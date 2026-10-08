package web

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/ui"
)

// The visual editor (js/lib/rte.js) edits ADF; its markdown mode is the
// markdown one (lib/mdedit.js). These turn one into the other as the editor
// switches, and draw the pictures its media nodes name.

// mediaFileID is a Media Services file id, as an ADF media node names it.
var mediaFileID = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

func init() {
	// POST {Doc} → the document as markdown to edit (editable; Editable false when markdown can't carry it)
	// and as markdown to read (Text: what markdown can't carry as its text), for a form that sends markdown.
	post("/adf/markdown", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Doc json.RawMessage }](r)
		if err != nil {
			return nil, err
		}
		if _, err := jira.CheckDoc(b.Doc); err != nil {
			return nil, badRequest(err.Error())
		}
		return struct {
			editable
			Text string
		}{editableOf(b.Doc), jira.DocMarkdown(b.Doc)}, nil
	})
	// POST {Markdown, Kept, People} → {Doc}: the markdown as Jira would save it, its
	// placeholders put back from Kept, each "@Name" of People a mention, each
	// pasted image line its picture.
	post("/adf/doc", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct {
			Markdown string
			Kept     []json.RawMessage
			People   []jira.User
		}](r)
		if err != nil {
			return nil, err
		}
		kept := append(b.Kept, jira.MentionNodes(b.People)...)
		return map[string]any{"Doc": jira.MarkdownToADFKept(s.Client().EmbedImages(ctx, b.Markdown), kept)}, nil
	})
	// POST {Lang, Code} → {Spans: [[from, to, class], …]}: a code block's tokens (ui.HighlightSpans), offsets in UTF-16 units.
	post("/highlight", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		b, err := Body[struct{ Lang, Code string }](r)
		if err != nil {
			return nil, err
		}
		spans := [][3]any{}
		if len(b.Code) <= 256<<10 && len(b.Lang) <= 40 {
			for _, sp := range ui.HighlightSpans(b.Lang, b.Code) {
				spans = append(spans, [3]any{sp.From, sp.To, sp.Class})
			}
		}
		return map[string]any{"Spans": spans}, nil
	})
	handle("GET /api/issues/{key}/media/{id}", media)
	// The Media Services file id of an attachment, for a media node of it.
	get("/attachments/{id}/media", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		id := r.PathValue("id")
		if !attachmentID.MatchString(id) {
			return nil, badRequest(i18n.T("bad attachment id"))
		}
		m, err := s.Client().MediaID(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]string{"MediaID": m}, nil
	})
}

// media answers the content of the issue's attachment that is Media Services
// file id: the picture a media node names (?name= as serveFile's).
func media(s *Server, w http.ResponseWriter, r *http.Request) {
	key, err := issueKey(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	if !mediaFileID.MatchString(id) {
		writeErr(w, badRequest(i18n.T("bad media id")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	at, err := s.Client().MediaAttachment(ctx, key, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	body, err := s.Client().AttachmentContent(ctx, at.ID)
	serveFile(w, r, body, err, at.Filename)
}

// given reports whether a request carried a document (not none, not null).
func given(doc json.RawMessage) bool { return len(doc) > 0 && string(doc) != "null" }
