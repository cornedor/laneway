package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/cornedor/laneway/internal/forge"
)

// Candidate reviewers: who a merge request's history suggests should look at
// it, from the CODEOWNERS file, earlier reviews of the same paths, and blame.
// Every source is best effort: one that fails adds nobody.

const (
	// candidateLimit is how many requests one Candidates call has in flight.
	candidateLimit = 6
	// reviewerFiles and reviewerCommits cap the history the reviewers source
	// reads: the most changed files, and their newest commits.
	reviewerFiles   = 5
	reviewerCommits = 20
	// blameFiles caps the files blamed, most changed first.
	blameFiles = 10
	// ownerLookups caps the CODEOWNERS usernames looked up for a name.
	ownerLookups = 20
	// candidateMRs is how many merge requests Sites.Candidates reads.
	candidateMRs = 3
)

// codeownersPaths are where GitLab looks for the file, in its order.
var codeownersPaths = []string{"CODEOWNERS", ".gitlab/CODEOWNERS", "docs/CODEOWNERS"}

// now is the clock blame ages lines by; tests pin it.
var now = time.Now

// limiter bounds the requests in flight and gives up when ctx does.
type limiter chan struct{}

func (l limiter) do(ctx context.Context, fn func() error) error {
	select {
	case l <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-l }()
	return fn()
}

// changed is a file of the diff with its parsed lines and how many it
// adds or removes.
type changed struct {
	forge.FileDiff
	lines []forge.DiffLine
	n     int
}

// Candidates are the people the merge request's history suggests as its
// reviewers, by sources ("codeowners", "reviewers", "blame"; only those
// asked run), highest total score first. Its author is left out.
func (c *Client) Candidates(ctx context.Context, project string, iid int, sources []string) ([]forge.Candidate, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	ch, err := c.Get(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	d, err := c.Diff(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	files := make([]changed, 0, len(d.Files))
	for _, f := range d.Files {
		cf := changed{FileDiff: f, lines: forge.ParseUnifiedDiff(f.Diff)}
		for _, l := range cf.lines {
			if l.Kind == forge.DiffAdd || l.Kind == forge.DiffDel {
				cf.n++
			}
		}
		files = append(files, cf)
	}
	slices.SortStableFunc(files, func(a, b changed) int { return b.n - a.n })

	lim := make(limiter, candidateLimit)
	// In this order, so the forge's accounts come before blame's authors.
	run := []struct {
		source string
		f      func() []forge.Candidate
	}{
		{"codeowners", func() []forge.Candidate { return c.ownerCandidates(ctx, lim, project, ch.TargetBranch, files) }},
		{"reviewers", func() []forge.Candidate { return c.reviewerCandidates(ctx, lim, project, iid, ch.TargetBranch, files) }},
		{"blame", func() []forge.Candidate { return c.blameCandidates(ctx, lim, project, d.Refs.BaseSHA, files) }},
	}
	found := make([][]forge.Candidate, len(run))
	var wg sync.WaitGroup
	for i, r := range run {
		if slices.Contains(sources, r.source) {
			wg.Go(func() { found[i] = r.f() })
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(mergeCandidates(slices.Concat(found...)), func(p forge.Candidate) bool {
		if p.Username != "" {
			return strings.EqualFold(p.Username, ch.AuthorUsername)
		}
		return p.Name != "" && strings.EqualFold(p.Name, ch.Author)
	}), nil
}

// ownerCandidates scores each owner by the changed files CODEOWNERS at ref
// gives them.
func (c *Client) ownerCandidates(ctx context.Context, lim limiter, project, ref string, files []changed) []forge.Candidate {
	var sections []ownerSection
	for _, p := range codeownersPaths {
		var lines []string
		err := lim.do(ctx, func() (err error) {
			lines, err = c.File(ctx, project, p, ref)
			return err
		})
		if err == nil {
			sections = parseCodeowners(lines)
			break
		}
	}
	var out []forge.Candidate
	for _, f := range files {
		for _, o := range codeOwners(sections, f.Path()) {
			i := slices.IndexFunc(out, func(p forge.Candidate) bool { return "@"+p.Username == o || p.Email == o })
			if i < 0 {
				p := forge.Candidate{Scores: map[string]float64{}}
				if u, ok := strings.CutPrefix(o, "@"); ok {
					p.Username = u
				} else {
					p.Email = o
				}
				out, i = append(out, p), len(out)
			}
			out[i].Scores["codeowners"]++
		}
	}
	return c.ownerNames(ctx, lim, out)
}

// ownerNames fills in the owners' names, so blame can find them, and drops
// the usernames GitLab has no user for: a top-level group's.
func (c *Client) ownerNames(ctx context.Context, lim limiter, owners []forge.Candidate) []forge.Candidate {
	gone := make([]bool, len(owners))
	var wg sync.WaitGroup
	looked := 0
	for i, p := range owners {
		if p.Username == "" || looked == ownerLookups {
			continue
		}
		looked++
		wg.Go(func() {
			var us []apiUser
			err := lim.do(ctx, func() error {
				return c.rest.Do(ctx, http.MethodGet, "/users?username="+url.QueryEscape(p.Username), "user", nil, &us)
			})
			switch {
			case err != nil:
			case len(us) == 0:
				gone[i] = true
			default:
				owners[i].Name = us[0].Name
			}
		})
	}
	wg.Wait()
	var out []forge.Candidate
	for i, p := range owners {
		if !gone[i] {
			out = append(out, p)
		}
	}
	return out
}

// reviewerCandidates scores each person by the merged merge requests they
// reviewed among the recent history of the most changed files.
func (c *Client) reviewerCandidates(ctx context.Context, lim limiter, project string, iid int, ref string, files []changed) []forge.Candidate {
	var paths []string
	for _, f := range files {
		if !f.New && len(paths) < reviewerFiles {
			paths = append(paths, f.OldPath)
		}
	}
	shas := make([][]string, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Go(func() {
			var cs []struct {
				ID string `json:"id"`
			}
			q := fmt.Sprintf("/projects/%s/repository/commits?path=%s&ref_name=%s&per_page=10",
				encodePath(project), url.QueryEscape(p), url.QueryEscape(ref))
			if lim.do(ctx, func() error { return c.rest.Do(ctx, http.MethodGet, q, "commits", nil, &cs) }) == nil {
				for _, cm := range cs {
					shas[i] = append(shas[i], cm.ID)
				}
			}
		})
	}
	wg.Wait()
	var uniq []string
	for _, s := range slices.Concat(shas...) {
		if len(uniq) < reviewerCommits && !slices.Contains(uniq, s) {
			uniq = append(uniq, s)
		}
	}
	mrs := make([][]apiMR, len(uniq))
	for i, sha := range uniq {
		wg.Go(func() {
			q := fmt.Sprintf("/projects/%s/repository/commits/%s/merge_requests", encodePath(project), sha)
			lim.do(ctx, func() error { return c.rest.Do(ctx, http.MethodGet, q, "commit merge requests", nil, &mrs[i]) })
		})
	}
	wg.Wait()
	seen := map[string]bool{}
	var out []forge.Candidate
	for _, a := range slices.Concat(mrs...) {
		key := fmt.Sprintf("%d!%d", a.ProjectID, a.IID)
		if a.State != forge.StateMerged || seen[key] || c.isMR(a, project, iid) {
			continue
		}
		seen[key] = true
		for _, u := range a.Reviewers {
			out = append(out, forge.Candidate{Username: u.Username, Name: u.Name, Scores: map[string]float64{"reviewers": 1}})
		}
	}
	return mergeCandidates(out)
}

// isMR is whether a is project's merge request iid.
func (c *Client) isMR(a apiMR, project string, iid int) bool {
	if r, ok := c.Parse(a.WebURL); ok {
		return strings.EqualFold(r.Repo, project) && r.Number == iid
	}
	return a.IID == iid
}

// blameCandidates scores each commit author by the lines the merge request
// touches that they last wrote at base, the older the less.
func (c *Client) blameCandidates(ctx context.Context, lim limiter, project, base string, files []changed) []forge.Candidate {
	var picked []changed
	for _, f := range files {
		if !f.New && !f.Binary && !generated(f.FileDiff) && f.n > 0 && len(picked) < blameFiles {
			picked = append(picked, f)
		}
	}
	found := make([][]forge.Candidate, len(picked))
	var wg sync.WaitGroup
	for i, f := range picked {
		wg.Go(func() { found[i] = c.blame(ctx, lim, project, base, f) })
	}
	wg.Wait()
	return mergeCandidates(slices.Concat(found...))
}

// blameSpanGap joins touched lines this close into one blame request;
// blameSpans caps the requests a file gets.
const (
	blameSpanGap = 20
	blameSpans   = 5
)

// blame reads f's blame at ref over the spans it touches (a generated file's
// whole blame runs to megabytes) and weighs its authors' touched lines.
func (c *Client) blame(ctx context.Context, lim limiter, project, ref string, f changed) []forge.Candidate {
	want := blamedLines(f.lines)
	if len(want) == 0 {
		return nil
	}
	t := now()
	var out []forge.Candidate
	for _, sp := range spans(want) {
		var ranges []struct {
			Commit struct {
				AuthorName    string `json:"author_name"`
				AuthorEmail   string `json:"author_email"`
				CommittedDate string `json:"committed_date"`
			} `json:"commit"`
			Lines []string `json:"lines"`
		}
		p := fmt.Sprintf("/projects/%s/repository/files/%s/blame?ref=%s&range[start]=%d&range[end]=%d", encodePath(project),
			strings.ReplaceAll(url.PathEscape(f.OldPath), "/", "%2F"), url.QueryEscape(ref), sp[0], sp[1])
		if lim.do(ctx, func() error { return c.rest.Do(ctx, http.MethodGet, p, "blame", nil, &ranges) }) != nil {
			continue
		}
		line := sp[0]
		for _, r := range ranges {
			n := 0
			for l := line; l < line+len(r.Lines); l++ {
				if want[l] {
					n++
				}
			}
			line += len(r.Lines)
			if n == 0 {
				continue
			}
			age := 0.0
			if at, err := time.Parse(time.RFC3339, r.Commit.CommittedDate); err == nil {
				age = max(0, t.Sub(at).Hours()/24)
			}
			// git keeps a name as typed: é may come as e and a combining accent.
			out = append(out, forge.Candidate{Name: norm.NFC.String(r.Commit.AuthorName), Email: r.Commit.AuthorEmail,
				Scores: map[string]float64{"blame": float64(n) * math.Pow(0.5, age/365)}})
		}
	}
	return out
}

// spans are the touched lines as [first, last] runs, lines blameSpanGap
// apart joined, the first blameSpans.
func spans(lines map[int]bool) [][2]int {
	ls := slices.Sorted(maps.Keys(lines))
	var out [][2]int
	for _, l := range ls {
		if n := len(out); n > 0 && l-out[n-1][1] <= blameSpanGap {
			out[n-1][1] = l
			continue
		}
		if len(out) == blameSpans {
			break
		}
		out = append(out, [2]int{l, l})
	}
	return out
}

// lockfiles are files no one writes by hand.
var lockfiles = []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "composer.lock", "go.sum", "Cargo.lock", "Gemfile.lock", "poetry.lock"}

// generated is whether f is written by a tool: its blame names whoever ran
// it, and GitLab blames a large one slowly (seconds for a schema).
func generated(f forge.FileDiff) bool {
	p := f.OldPath
	return f.Generated || strings.Contains(p, ".generated.") || strings.Contains(p, "/generated/") ||
		strings.HasPrefix(p, "generated/") || slices.Contains(lockfiles, path.Base(p))
}

// blamedLines are the old side's lines a diff touches: the ones it removes
// or rewrites, and for a pure insertion the line before it.
func blamedLines(lines []forge.DiffLine) map[int]bool {
	out := map[int]bool{}
	edit := func(l forge.DiffLine) bool { return l.Kind == forge.DiffAdd || l.Kind == forge.DiffDel }
	for i := 0; i < len(lines); {
		if !edit(lines[i]) {
			i++
			continue
		}
		j, del := i, false
		for ; j < len(lines) && edit(lines[j]); j++ {
			if lines[j].Kind == forge.DiffDel {
				out[lines[j].OldLine], del = true, true
			}
		}
		if !del && lines[i].OldPos > 1 {
			out[lines[i].OldPos-1] = true
		}
		i = j
	}
	return out
}

// mergeCandidates folds each person's entries into one, scores summed: the
// same username, else the same email or name where one has no username.
// Those with a username go first, so blame's authors fold into them.
func mergeCandidates(in []forge.Candidate) []forge.Candidate {
	in = slices.Clone(in)
	anon := func(p forge.Candidate) int {
		if p.Username == "" {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(in, func(a, b forge.Candidate) int { return cmp.Compare(anon(a), anon(b)) })
	var out []forge.Candidate
	for _, p := range in {
		i := slices.IndexFunc(out, func(o forge.Candidate) bool { return samePerson(o, p) })
		if i < 0 {
			out, i = append(out, forge.Candidate{Username: p.Username, Name: p.Name, Email: p.Email, Scores: map[string]float64{}}), len(out)
		}
		o := &out[i]
		o.Username, o.Name, o.Email = cmp.Or(o.Username, p.Username), cmp.Or(o.Name, p.Name), cmp.Or(o.Email, p.Email)
		for k, v := range p.Scores {
			o.Scores[k] += v
		}
	}
	slices.SortStableFunc(out, func(a, b forge.Candidate) int { return cmp.Compare(total(b), total(a)) })
	return out
}

// samePerson is whether a and b are one person, as far as can be told.
func samePerson(a, b forge.Candidate) bool {
	if a.Username != "" && b.Username != "" {
		return strings.EqualFold(a.Username, b.Username)
	}
	return a.Email != "" && strings.EqualFold(a.Email, b.Email) || a.Name != "" && strings.EqualFold(a.Name, b.Name)
}

// total is p's score over every source.
func total(p forge.Candidate) float64 {
	var t float64
	for _, v := range p.Scores {
		t += v
	}
	return t
}

// ownerSection is a CODEOWNERS section ("" the one before any header): the
// last of its rules matching a path gives that path's owners.
type ownerSection struct {
	name  string
	rules []ownerRule
}

type ownerRule struct {
	re     *regexp.Regexp
	owners []string // "@username" or an email
}

// sectionRe is a section header: [Name] or ^[Name] (optional), an approval
// count [2] perhaps, then its default owners.
var sectionRe = regexp.MustCompile(`^\^?\[([^\]]+)\](?:\[\d+\])?(.*)$`)

// parseCodeowners reads a CODEOWNERS file. Sections of one name, any case,
// are one; a rule without owners takes its section's default ones.
func parseCodeowners(lines []string) []ownerSection {
	out := []ownerSection{{}}
	cur := 0
	var defaults []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || ln[0] == '#' {
			continue
		}
		if m := sectionRe.FindStringSubmatch(ln); m != nil {
			defaults = ownerList(fields(m[2]))
			cur = slices.IndexFunc(out, func(s ownerSection) bool { return strings.EqualFold(s.name, m[1]) })
			if cur < 0 {
				out, cur = append(out, ownerSection{name: m[1]}), len(out)
			}
			continue
		}
		f := fields(ln)
		re, err := globRegexp(f[0])
		if err != nil {
			continue
		}
		owners := ownerList(f[1:])
		if len(owners) == 0 {
			owners = defaults
		}
		out[cur].rules = append(out[cur].rules, ownerRule{re: re, owners: owners})
	}
	return out
}

// codeOwners are path's owners: each section's last matching rule's.
func codeOwners(sections []ownerSection, path string) []string {
	p := "/" + strings.TrimPrefix(path, "/")
	var out []string
	for _, s := range sections {
		for _, r := range slices.Backward(s.rules) {
			if r.re.MatchString(p) {
				for _, o := range r.owners {
					if !slices.Contains(out, o) {
						out = append(out, o)
					}
				}
				break
			}
		}
	}
	return out
}

// fields splits a line on whitespace, "\ " kept as a space.
func fields(s string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '\\' && i+1 < len(s) && s[i+1] == ' ':
			b.WriteByte(' ')
			i++
		case ch == ' ' || ch == '\t':
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(ch)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// ownerList keeps the users and emails of a rule's owners, up to a comment:
// groups (@group/sub) and roles (@@developer) are left out.
func ownerList(tokens []string) []string {
	var out []string
	for _, t := range tokens {
		switch {
		case strings.HasPrefix(t, "#"):
			return out
		case strings.HasPrefix(t, "@@"):
		case strings.HasPrefix(t, "@"):
			if !strings.Contains(t, "/") {
				out = append(out, t)
			}
		case strings.Contains(t, "@"):
			out = append(out, t)
		}
	}
	return out
}

// globRegexp compiles a CODEOWNERS pattern as GitLab matches it (fnmatch,
// a path's slashes literal): unanchored is under any directory, a trailing
// slash is everything below, ** any directories.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern == "*" {
		pattern = "/**/*"
	}
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/**/" + pattern
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**/*"
	}
	var b strings.Builder
	b.WriteString("^")
	braces := 0
	for i := 0; i < len(pattern); i++ {
		switch ch := pattern[i]; ch {
		case '*':
			if strings.HasPrefix(pattern[i:], "**/") {
				b.WriteString("(?:.*/)?")
				i += 2
				continue
			}
			for i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := pattern[i+1 : i+1+end]
			if rest, ok := strings.CutPrefix(class, "!"); ok {
				class = "^" + rest
			}
			b.WriteString("[" + class + "]")
			i += end + 1
		case '{':
			braces++
			b.WriteString("(?:")
		case '}', ',':
			switch {
			case braces == 0:
				b.WriteString(regexp.QuoteMeta(string(ch)))
			case ch == ',':
				b.WriteString("|")
			default:
				braces--
				b.WriteString(")")
			}
		case '\\':
			if i+1 < len(pattern) {
				i++
				b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	if braces > 0 {
		return nil, fmt.Errorf("unclosed brace in %q", pattern)
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
