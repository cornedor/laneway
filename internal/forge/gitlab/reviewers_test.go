package gitlab

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/forge"
)

// fakeGitLab answers each request by its decoded path and query, else its
// path alone, else 404, and counts the paths asked.
func fakeGitLab(t *testing.T, routes map[string]string) (*httptest.Server, map[string]int) {
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		body, ok := routes[r.URL.Path+"?"+r.URL.RawQuery]
		if !ok {
			body, ok = routes[r.URL.Path]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

const candidateMR = `{"iid": 7, "state": "opened", "target_branch": "main",
  "author": {"username": "me", "name": "Me Author"},
  "diff_refs": {"base_sha": "base1", "start_sha": "start1", "head_sha": "head1"}}`

// pinNow fixes blame's clock at 2026-01-01.
func pinNow(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
}

// byKey is the candidates by username, else email.
func byKey(cs []forge.Candidate) map[string]forge.Candidate {
	out := map[string]forge.Candidate{}
	for _, c := range cs {
		out[cmp.Or(c.Username, c.Email)] = c
	}
	return out
}

// TestCodeowners: anchoring, directories, **, sections each giving their
// last match, section default owners, groups and comments left out.
func TestCodeowners(t *testing.T) {
	file := `# owners
* @all
/docs/ @docs
*.go @gopher dev@example.com
internal/ @group/sub @inner # a comment @not
/internal/**/gen_*.go @gen
README.md @readme
/root.txt @root
my\ file.txt @spaced

[Frontend] @fe-lead
web/
web/legacy/ @legacy

^[optional][2]
*.go @opt
[FRONTEND]
*.css @css
`
	sections := parseCodeowners(strings.Split(file, "\n"))
	for path, want := range map[string]string{
		"main.go":             "@gopher dev@example.com @opt",
		"docs/a.md":           "@docs",
		"x/docs/a.md":         "@all",
		"internal/a.txt":      "@inner",
		"internal/gen_a.go":   "@gen @opt",
		"internal/x/gen_a.go": "@gen @opt",
		"sub/README.md":       "@readme",
		"root.txt":            "@root",
		"sub/root.txt":        "@all",
		"a/my file.txt":       "@spaced",
		"web/a.js":            "@all @fe-lead",
		"web/legacy/b.js":     "@all @legacy",
		"web/a.css":           "@all @css",
	} {
		if got := strings.Join(codeOwners(sections, path), " "); got != want {
			t.Errorf("%s: owners %q, want %q", path, got, want)
		}
	}
}

// TestCandidatesBlame: the lines a change removes or rewrites, and the line
// before a pure insertion, count for their author, halved per year of age;
// new, binary and generated files are not blamed; the author is left out. Only the
// span touched is asked for, and a decomposed name comes back composed.
func TestCandidatesBlame(t *testing.T) {
	pinNow(t)
	srv, hits := fakeGitLab(t, map[string]string{
		"/api/v4/projects/g/p/merge_requests/7": candidateMR,
		"/api/v4/projects/g/p/merge_requests/7/diffs": `[
  {"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1,5 +1,5 @@\n a\n-b\n+B\n c\n+new\n d\n-e\n"},
  {"old_path": "n.go", "new_path": "n.go", "new_file": true, "diff": "@@ -0,0 +1 @@\n+x\n"},
  {"old_path": "i.png", "new_path": "i.png", "diff": "Binary files a/i.png and b/i.png differ\n"},
  {"old_path": "s.generated.ts", "new_path": "s.generated.ts", "diff": "@@ -1 +1 @@\n-x\n+y\n"},
  {"old_path": "web/yarn.lock", "new_path": "web/yarn.lock", "diff": "@@ -1 +1 @@\n-x\n+y\n"}]`,
		"/api/v4/projects/g/p/repository/files/a.go/blame?ref=base1&range[start]=2&range[end]=5": `[
  {"commit": {"author_name": "Bo\u0301b", "author_email": "bob@x", "committed_date": "2025-01-01T00:00:00Z"}, "lines": ["b"]},
  {"commit": {"author_name": "Carol", "author_email": "carol@x", "committed_date": "2026-01-01T00:00:00Z"}, "lines": ["c", "d"]},
  {"commit": {"author_name": "me author", "author_email": "me@x", "committed_date": "2026-01-01T00:00:00Z"}, "lines": ["e"]}]`,
	})
	got, err := newTestClient(srv).Candidates(context.Background(), "g/p", 7, []string{"blame"})
	if err != nil {
		t.Fatal(err)
	}
	want := []forge.Candidate{
		{Name: "Carol", Email: "carol@x", Scores: map[string]float64{"blame": 1}},
		{Name: "Bób", Email: "bob@x", Scores: map[string]float64{"blame": 0.5}}, // as one character
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("candidates\n got %v\nwant %v", got, want)
	}
	if hits["/api/v4/projects/g/p/repository/files/n.go/blame"]+hits["/api/v4/projects/g/p/repository/files/i.png/blame"]+
		hits["/api/v4/projects/g/p/repository/files/s.generated.ts/blame"]+hits["/api/v4/projects/g/p/repository/files/web/yarn.lock/blame"] != 0 {
		t.Errorf("blamed a new, binary or generated file: %v", hits)
	}
}

// TestCandidatesReviewers: each merged merge request in the changed files'
// history counts once for its reviewers; open ones and this one do not.
func TestCandidatesReviewers(t *testing.T) {
	routes := map[string]string{
		"/api/v4/projects/g/p/merge_requests/7":                                       candidateMR,
		"/api/v4/projects/g/p/merge_requests/7/diffs":                                 `[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@\n-x\n+y\n"}]`,
		"/api/v4/projects/g/p/repository/commits?path=a.go&ref_name=main&per_page=10": `[{"id": "c1"}, {"id": "c2"}, {"id": "c1"}]`,
		"/api/v4/projects/g/p/repository/commits/c1/merge_requests": `[
  {"iid": 3, "project_id": 1, "state": "merged", "reviewers": [{"username": "rev", "name": "Rev"}, {"username": "me", "name": "Me Author"}]},
  {"iid": 7, "project_id": 1, "state": "merged", "web_url": "SELF", "reviewers": [{"username": "self", "name": "Self"}]}]`,
		"/api/v4/projects/g/p/repository/commits/c2/merge_requests": `[
  {"iid": 3, "project_id": 1, "state": "merged", "reviewers": [{"username": "rev", "name": "Rev"}]},
  {"iid": 4, "project_id": 1, "state": "opened", "reviewers": [{"username": "open", "name": "Open"}]},
  {"iid": 5, "project_id": 1, "state": "merged", "reviewers": [{"username": "rev", "name": "Rev"}, {"username": "other", "name": "Other"}]}]`,
	}
	srv, _ := fakeGitLab(t, routes)
	c := newTestClient(srv)
	p := "/api/v4/projects/g/p/repository/commits/c1/merge_requests"
	routes[p] = strings.Replace(routes[p], "SELF", c.WebURL("g/p", 7), 1)
	got, err := c.Candidates(context.Background(), "g/p", 7, []string{"reviewers"})
	if err != nil {
		t.Fatal(err)
	}
	m := byKey(got)
	if len(got) != 2 || m["rev"].Scores["reviewers"] != 2 || m["rev"].Name != "Rev" || m["other"].Scores["reviewers"] != 1 {
		t.Errorf("candidates %+v", got)
	}
}

// TestCandidatesOwners: the first CODEOWNERS found counts each owner's
// changed files; blame's authors fold into them by name or email; a
// username GitLab has no user for is dropped.
func TestCandidatesOwners(t *testing.T) {
	pinNow(t)
	srv, hits := fakeGitLab(t, map[string]string{
		"/api/v4/projects/g/p/merge_requests/7": candidateMR,
		"/api/v4/projects/g/p/merge_requests/7/diffs": `[
  {"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1 +1 @@\n-x\n+y\n"},
  {"old_path": "b.go", "new_path": "b.go", "diff": "@@ -1 +1 @@\n-x\n+y\n"}]`,
		"/api/v4/projects/g/p/repository/files/.gitlab/CODEOWNERS/raw?ref=main": "* @ada team@example.com @ghost\n/docs/ @group/docs\n",
		"/api/v4/projects/g/p/repository/files/docs/CODEOWNERS/raw?ref=main":    "* @nobody\n",
		"/api/v4/users?username=ada":                                            `[{"username": "ada", "name": "Ada Lovelace"}]`,
		"/api/v4/users?username=ghost":                                          `[]`,
		"/api/v4/projects/g/p/repository/files/a.go/blame?ref=base1&range[start]=1&range[end]=1": `[
  {"commit": {"author_name": "ada lovelace", "author_email": "ada@home", "committed_date": "2026-01-01T00:00:00Z"}, "lines": ["x"]}]`,
		"/api/v4/projects/g/p/repository/files/b.go/blame?ref=base1&range[start]=1&range[end]=1": `[
  {"commit": {"author_name": "Team Bot", "author_email": "TEAM@example.com", "committed_date": "2026-01-01T00:00:00Z"}, "lines": ["x"]}]`,
	})
	got, err := newTestClient(srv).Candidates(context.Background(), "g/p", 7, []string{"blame", "codeowners", "nonsense"})
	if err != nil {
		t.Fatal(err)
	}
	m := byKey(got)
	ada, team := m["ada"], m["team@example.com"]
	if len(got) != 2 || ada.Name != "Ada Lovelace" || ada.Email != "ada@home" || ada.Scores["codeowners"] != 2 || ada.Scores["blame"] != 1 ||
		team.Name != "Team Bot" || team.Scores["codeowners"] != 2 || team.Scores["blame"] != 1 {
		t.Errorf("candidates %+v", got)
	}
	if hits["/api/v4/projects/g/p/repository/files/CODEOWNERS/raw"] != 1 || hits["/api/v4/projects/g/p/repository/files/docs/CODEOWNERS/raw"] != 0 {
		t.Errorf("CODEOWNERS lookup %v", hits)
	}
}

// TestSitesCandidates: the newest three merge requests naming the key that
// are not closed, their candidates summed.
func TestSitesCandidates(t *testing.T) {
	var base string
	mr := func(iid int, state, updated string) string {
		return fmt.Sprintf(`{"iid": %d, "title": "ABC-1 part %d", "state": %q, "updated_at": %q, "web_url": "%s/g/p/-/merge_requests/%d"}`,
			iid, iid, state, updated, base, iid)
	}
	routes := map[string]string{
		"/api/v4/projects/g/p/repository/files/CODEOWNERS/raw?ref=main": "* @ada\n/web/ @bob\n",
		"/api/v4/users?username=ada":                                    `[{"username": "ada", "name": "Ada"}]`,
		"/api/v4/users?username=bob":                                    `[{"username": "bob", "name": "Bob"}]`,
	}
	file := func(p string) string {
		return fmt.Sprintf(`{"old_path": %q, "new_path": %q, "diff": "@@ -1 +1 @@\n-x\n+y\n"}`, p, p)
	}
	for iid, files := range map[int][]string{1: {"a.go", "b.go"}, 2: {"c.go"}, 3: {"d.go"}, 4: {"web/x.js"}, 5: {"e.go"}} {
		p := fmt.Sprintf("/api/v4/projects/g/p/merge_requests/%d", iid)
		routes[p] = fmt.Sprintf(`{"iid": %d, "target_branch": "main", "author": {"username": "zed"}}`, iid)
		var fs []string
		for _, f := range files {
			fs = append(fs, file(f))
		}
		routes[p+"/diffs"] = "[" + strings.Join(fs, ",") + "]"
	}
	srv, hits := fakeGitLab(t, routes)
	base = srv.URL
	routes["/api/v4/merge_requests"] = "[" + strings.Join([]string{
		mr(5, "opened", "2025-01-01T00:00:00Z"),
		mr(1, "opened", "2026-04-01T00:00:00Z"),
		mr(3, "closed", "2026-05-01T00:00:00Z"),
		mr(2, "merged", "2026-03-01T00:00:00Z"),
		mr(4, "merged", "2026-02-01T00:00:00Z"),
	}, ",") + "]"

	s := NewSites([]Config{{BaseURL: srv.URL, Token: "tok"}}).WithoutGlab()
	got := s.Candidates(context.Background(), "ABC-1", []string{"codeowners"})
	if len(got) != 2 || got[0].Username != "ada" || got[0].Scores["codeowners"] != 3 || got[1].Username != "bob" || got[1].Scores["codeowners"] != 1 {
		t.Errorf("candidates %+v", got)
	}
	if hits["/api/v4/projects/g/p/merge_requests/3"]+hits["/api/v4/projects/g/p/merge_requests/5"] != 0 {
		t.Errorf("read a closed or older merge request: %v", hits)
	}
	if !slices.ContainsFunc(got, func(c forge.Candidate) bool { return c.Name == "Bob" }) {
		t.Errorf("names not filled: %+v", got)
	}
}

// Touched lines close together are one blame request; at most blameSpans.
func TestSpans(t *testing.T) {
	lines := map[int]bool{3: true, 10: true, 40: true, 41: true}
	for i := range 10 {
		lines[100+i*100] = true
	}
	got := spans(lines)
	if len(got) != blameSpans || got[0] != [2]int{3, 10} || got[1] != [2]int{40, 41} || got[2] != [2]int{100, 100} {
		t.Errorf("spans = %v", got)
	}
}
