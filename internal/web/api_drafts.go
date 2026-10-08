package web

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// Drafts: a comment or description being written, kept in the state file
// where the TUI keeps its own (internal/ui/drafts.go), so a reload, or the
// terminal, brings it back. Ids are comment:KEY, desc:KEY,
// desc:KEY:comment:ID and desc:KEY:field:ID.

const draftPrefix = "jira_tab:draft:"

var draftID = regexp.MustCompile(`^(comment|desc):([A-Z][A-Z0-9_]*-[0-9]+)(:comment:[0-9]+|:field:[A-Za-z0-9_]+)?$`)

func draftKey(r *http.Request) (string, error) {
	id := r.PathValue("id")
	m := draftID.FindStringSubmatch(id)
	if m == nil || (m[1] == "comment" && m[3] != "") || !jira.ValidKey(m[2]) {
		return "", badRequest(i18n.T("bad draft id"))
	}
	return draftPrefix + id, nil
}

func init() {
	get("/drafts/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		k, err := draftKey(r)
		if err != nil {
			return nil, err
		}
		v, ok, _ := s.opt.Store.GetMeta(k)
		unix, text, _ := strings.Cut(v, "\n")
		sec, err := strconv.ParseInt(unix, 10, 64)
		if !ok || err != nil || text == "" {
			return map[string]any{"Text": ""}, nil
		}
		return map[string]any{"Text": text, "When": time.Unix(sec, 0)}, nil
	})
	put("/drafts/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		k, err := draftKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ Text string }](r)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(b.Text) == "" {
			return nil, s.opt.Store.DeleteMeta(k)
		}
		return nil, s.opt.Store.SetMeta(k, strconv.FormatInt(time.Now().Unix(), 10)+"\n"+b.Text)
	})
	del("/drafts/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		k, err := draftKey(r)
		if err != nil {
			return nil, err
		}
		return nil, s.opt.Store.DeleteMeta(k)
	})
}
