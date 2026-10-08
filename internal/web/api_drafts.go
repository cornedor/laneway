package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/ui"
)

// Drafts: a comment or description being written, kept in the state file
// where the TUI keeps its own (internal/ui/drafts.go), so a reload, or the
// terminal, brings it back. Ids are comment:KEY, desc:KEY,
// desc:KEY:comment:ID and desc:KEY:field:ID; create is the browser's
// create form, its text the form as JSON.

const draftPrefix = "jira_tab:draft:"

// docBase is a draft's Base: none, or a jira.DocBase.
var docBase = regexp.MustCompile(`^[0-9a-f]{0,64}$`)

var draftID = regexp.MustCompile(`^(comment|desc):([A-Z][A-Z0-9_]*-[0-9]+)(:comment:[0-9]+|:field:[A-Za-z0-9_]+)?$`)

func draftKey(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if id == "create" {
		return draftPrefix + id, nil
	}
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
		v, _, _ := s.opt.Store.GetMeta(k)
		d, ok := ui.DecodeDraft(v)
		if !ok {
			return map[string]any{"Text": ""}, nil
		}
		return map[string]any{"Text": d.Text, "When": d.At, "Base": d.Base}, nil
	})
	put("/drafts/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		k, err := draftKey(r)
		if err != nil {
			return nil, err
		}
		// Base is the jira.DocBase of the document a desc: draft edits.
		b, err := Body[struct{ Text, Base string }](r)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(b.Text) == "" {
			return nil, s.opt.Store.DeleteMeta(k)
		}
		if !docBase.MatchString(b.Base) {
			return nil, badRequest(i18n.T("bad draft base"))
		}
		return nil, s.opt.Store.SetMeta(k, ui.EncodeDraft(ui.Draft{Text: b.Text, Base: b.Base, At: time.Now()}))
	})
	del("/drafts/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		k, err := draftKey(r)
		if err != nil {
			return nil, err
		}
		return nil, s.opt.Store.DeleteMeta(k)
	})
}
