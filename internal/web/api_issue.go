package web

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// Routes for the issue panel: description and comment editing, history,
// links, children, time in status, dev info and the attachment proxy.

func init() {
	get("/issues/{key}/card", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		cards, err := s.Client().SearchCards(ctx, "key = "+key)
		if err != nil || len(cards) == 0 {
			return nil, err
		}
		return cards[0], nil
	})
	get("/issues/{key}/description", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		raw, err := s.Client().Description(ctx, key)
		if err != nil {
			return nil, err
		}
		return editableOf(raw), nil
	})
	put("/issues/{key}/description", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[mdBody](r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().SetDescription(ctx, key, b.Markdown, b.Kept)
	})
	post("/issues/{key}/description/task", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct {
			N, Total int
			Done     bool
		}](r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().ToggleTask(ctx, key, b.N, b.Total, b.Done)
	})

	post("/issues/{key}/comments", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct {
			Markdown string
			Mentions []jira.Mention
		}](r)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(b.Markdown) == "" {
			return nil, badRequest("empty comment")
		}
		var kept []json.RawMessage
		for _, m := range b.Mentions {
			if m.AccountID != "" && m.DisplayName != "" {
				kept = append(kept, jira.MentionNode("mention", map[string]any{"id": m.AccountID, "text": "@" + m.DisplayName}))
			}
		}
		doc, _ := json.Marshal(jira.MarkdownToADFKept(b.Markdown, kept))
		return nil, s.Client().AddCommentADF(ctx, key, doc)
	})
	get("/issues/{key}/comments/{id}/edit", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		iss, err := s.Client().Get(ctx, key)
		if err != nil {
			return nil, err
		}
		for _, c := range iss.Comments {
			if c.ID == r.PathValue("id") {
				return editableOf(c.Raw), nil
			}
		}
		return nil, jira.ErrNotFound
	})
	put("/issues/{key}/comments/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[mdBody](r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().SetComment(ctx, key, r.PathValue("id"), b.Markdown, b.Kept)
	})
	del("/issues/{key}/comments/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().DeleteComment(ctx, key, r.PathValue("id"))
	})

	get("/issues/{key}/history", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return s.Client().Changelog(ctx, key)
	})
	get("/issues/{key}/timeinstatus", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return s.Client().TimeInStatus(ctx, key, time.Now())
	})
	get("/issues/{key}/dev", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return s.Client().DevInfo(ctx, key)
	})
	get("/issues/{key}/children", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return s.Client().Children(ctx, key)
	})
	get("/issues/{key}/weblinks", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return s.Client().WebLinks(ctx, key)
	})
	get("/linktypes", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		return s.Client().LinkTypes(ctx)
	})
	post("/issues/{key}/links", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		// Outward: key does the type's outward verb to Other; else Other does it to key.
		b, err := Body[struct {
			Type, Other string
			Outward     bool
		}](r)
		if err != nil {
			return nil, err
		}
		if !jira.ValidKey(b.Other) || b.Type == "" {
			return nil, badRequest("need a type and an issue key")
		}
		from, to := key, b.Other
		if !b.Outward {
			from, to = to, from
		}
		return nil, s.Client().LinkIssues(ctx, b.Type, from, to)
	})
	del("/issues/{key}/links/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().DeleteLink(ctx, key, r.PathValue("id"))
	})
	post("/issues/{key}/weblinks", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ URL, Title string }](r)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(b.URL, "https://") && !strings.HasPrefix(b.URL, "http://") {
			return nil, badRequest("a link needs an http(s) URL")
		}
		return nil, s.Client().AddWebLink(ctx, key, b.URL, b.Title)
	})
	del("/issues/{key}/attachments/{id}", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		key, err := issueKey(r)
		if err != nil {
			return nil, err
		}
		return nil, s.Client().DeleteAttachment(ctx, key, r.PathValue("id"))
	})

	handle("GET /api/attachments/{id}", attachment)
}

type mdBody struct {
	Markdown string
	Kept     []json.RawMessage
}

func issueKey(r *http.Request) (string, error) {
	key := r.PathValue("key")
	if !jira.ValidKey(key) {
		return "", badRequest("bad issue key")
	}
	return key, nil
}

// editable is a document as markdown to edit. When markdown can't carry it,
// Editable is false, Reason says why and Markdown is empty.
type editable struct {
	Markdown string
	Kept     []json.RawMessage
	Editable bool
	Reason   string
}

func editableOf(raw json.RawMessage) editable {
	ed, err := jira.EditableDescription(raw)
	if err != nil {
		return editable{Reason: err.Error()}
	}
	if ed.Kept == nil {
		ed.Kept = []json.RawMessage{}
	}
	return editable{Markdown: ed.Markdown, Kept: ed.Kept, Editable: true}
}

var attachmentID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// inlineTypes are the content types sniffed from the bytes that may show in
// the browser; everything else downloads. SVG is never among them: it runs
// script.
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true}

// attachment streams a Jira attachment with the client's credentials. The
// type comes from the bytes, not from Jira or the query.
func attachment(s *Server, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !attachmentID.MatchString(id) {
		writeErr(w, badRequest("bad attachment id"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	body, err := s.Client().AttachmentContent(ctx, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	typ := http.DetectContentType(body)
	if i := strings.IndexByte(typ, ';'); i >= 0 {
		typ = typ[:i]
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, max-age=86400")
	if inlineTypes[typ] && Q(r, "download") == "" {
		h.Set("Content-Type", typ)
	} else {
		h.Set("Content-Type", "application/octet-stream")
		name := strings.NewReplacer("/", "_", "\\", "_", "\r", "", "\n", "").Replace(Q(r, "name"))
		if name == "" {
			name = "attachment-" + id
		}
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	_, _ = w.Write(body)
}
