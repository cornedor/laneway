package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/cornedor/laneway/internal/emoji"
	"github.com/cornedor/laneway/internal/jira"
)

// Issue actions (the TUI's A menu), attachments uploads and emoji completion.

const (
	emojiUsageMeta = "emoji_usage" // the TUI keeps the same counts
	emojiShown     = 8
)

func init() {
	get("/emoji", emojiSearch)
	post("/emoji/used", emojiUsed)
	get("/issues/{key}/watchers", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		us, err := s.Client().Watchers(ctx, key)
		if us == nil {
			us = []jira.User{}
		}
		return us, err
	})
	get("/issues/{key}/viewusers", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		us, err := s.Client().ViewUsers(ctx, key, Q(r, "q"))
		if us == nil {
			us = []jira.User{}
		}
		return us, err
	})
	put("/issues/{key}/watchers/{account}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ Watch bool }](r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().SetWatcher(ctx, key, r.PathValue("account"), b.Watch)
	})
	post("/issues/{key}/watch", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		on, err := s.Client().ToggleWatch(ctx, key)
		return map[string]bool{"On": on}, err
	})
	post("/issues/{key}/vote", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		on, err := s.Client().ToggleVote(ctx, key)
		return map[string]bool{"On": on}, err
	})
	get("/issues/{key}/types", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		ts, err := s.Client().TypesLike(ctx, projectOf(key), Q(r, "current"))
		if ts == nil {
			ts = []jira.Option{}
		}
		return uniqueByID(ts), err
	})
	post("/issues/{key}/type", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ ID string }](r)
		if err != nil || b.ID == "" {
			return nil, badRequest("need a type id")
		}
		return nil, s.Client().SetIssueType(ctx, key, b.ID)
	})
	get("/issues/{key}/movetypes", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		to := Q(r, "project")
		if !jira.ValidKey(to + "-1") {
			return nil, badRequest("bad project")
		}
		ts, err := s.Client().MoveTypes(ctx, projectOf(key), Q(r, "current"), to)
		if ts == nil {
			ts = []jira.Option{}
		}
		return uniqueByID(ts), err
	})
	post("/issues/{key}/move", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ Project, TypeID string }](r)
		if err != nil || b.TypeID == "" || !jira.ValidKey(b.Project+"-1") {
			return nil, badRequest("need a project and a type id")
		}
		nk, err := s.Client().MoveIssue(ctx, key, b.Project, b.TypeID)
		return map[string]string{"Key": nk}, err
	})
	// POST multipart, field "file": attaches it to the issue.
	post("/issues/{key}/attachments", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		r.Body = http.MaxBytesReader(nil, r.Body, 32<<20)
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, badRequest("need a file: " + err.Error())
		}
		defer f.Close()
		name := strings.NewReplacer("/", "_", "\\", "_").Replace(hdr.Filename)
		if name == "" {
			name = "file"
		}
		return nil, s.Client().UploadAttachmentFrom(ctx, key, name, f)
	})
}

func projectOf(key string) string { return key[:strings.LastIndex(key, "-")] }

type emojiHit struct{ Name, Glyph string }

// emojiSearch ranks shortcodes for ?q=, the way the TUI's ":" completion does.
func emojiSearch(ctx context.Context, s *Server, r *http.Request) (any, error) {
	q := strings.ToLower(strings.TrimSpace(Q(r, "q")))
	out := []emojiHit{}
	if len(q) < 2 || strings.IndexFunc(q, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || unicode.IsDigit(c) || c == '_' || c == '+' || c == '-')
	}) >= 0 {
		return out, nil
	}
	type cand struct {
		name        string
		band, score int
	}
	var cands []cand
	near := false
	tones := strings.Contains(q, "skin") || strings.Contains(q, "tone")
	for _, n := range emoji.Names() {
		if !tones && strings.HasSuffix(n, "skin_tone") {
			continue
		}
		if band, score, ok := fuzzyScore(n, q); ok {
			cands = append(cands, cand{n, band, score})
			near = near || band < 3
		}
	}
	if near {
		cands = slices.DeleteFunc(cands, func(c cand) bool { return c.band == 3 })
	}
	use := emojiUsage(s)
	slices.SortStableFunc(cands, func(a, b cand) int {
		switch {
		case a.band != b.band:
			return a.band - b.band
		case use[a.name] != use[b.name]:
			return use[b.name] - use[a.name]
		case a.score != b.score:
			return a.score - b.score
		}
		return strings.Compare(a.name, b.name)
	})
	for _, c := range cands[:min(len(cands), emojiShown)] {
		out = append(out, emojiHit{c.name, emoji.Glyph(c.name)})
	}
	return out, nil
}

// fuzzyScore: band 0 exact, 1 prefix, 2 inside, 3 letters in order.
func fuzzyScore(haystack, needle string) (band, score int, ok bool) {
	if i := strings.Index(haystack, needle); i >= 0 {
		switch {
		case len(haystack) == len(needle):
			band = 0
		case i == 0:
			band = 1
		default:
			band = 2
		}
		return band, i*2 + len(haystack) - len(needle), true
	}
	hi, gaps := 0, 0
	for _, c := range []byte(needle) {
		for hi < len(haystack) && haystack[hi] != c {
			hi, gaps = hi+1, gaps+1
		}
		if hi >= len(haystack) {
			return 0, 0, false
		}
		hi++
	}
	return 3, gaps, true
}

func emojiUsage(s *Server) map[string]int {
	use := map[string]int{}
	if v, ok, _ := s.opt.Store.GetMeta(emojiUsageMeta); ok {
		_ = json.Unmarshal([]byte(v), &use)
	}
	return use
}

func emojiUsed(ctx context.Context, s *Server, r *http.Request) (any, error) {
	b, err := Body[struct{ Name string }](r)
	if err != nil || emoji.Glyph(b.Name) == "" {
		return nil, badRequest("unknown emoji")
	}
	inboxMu.Lock()
	defer inboxMu.Unlock()
	use := emojiUsage(s)
	use[b.Name]++
	raw, _ := json.Marshal(use)
	return nil, s.opt.Store.SetMeta(emojiUsageMeta, string(raw))
}
