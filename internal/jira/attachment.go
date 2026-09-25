package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Attachment is one file on an issue.
type Attachment struct {
	ID       string
	Filename string
	MimeType string
	Size     int64
}

// IsImage reports whether the attachment is a picture the panel can draw.
func (a Attachment) IsImage() bool {
	return strings.HasPrefix(a.MimeType, "image/")
}

type apiAttachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// mediaRef is the placeholder target adfToMarkdown writes for a media node;
// resolveMedia swaps it for attachment:<id>.
const mediaRef = "attachment"

// AttachmentScheme prefixes an attachment id in a markdown image target.
const AttachmentScheme = "attachment:"

// escapeMediaAlt keeps a file name from closing the markdown image early.
func escapeMediaAlt(s string) string {
	return strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(s)
}

// resolveMedia points each ![name](attachment) at the attachment of that
// name. Names that match none are left as they are.
func resolveMedia(md string, atts []Attachment) string {
	if len(atts) == 0 || !strings.Contains(md, "]("+mediaRef+")") {
		return md
	}
	for _, a := range atts {
		name := escapeMediaAlt(a.Filename)
		md = strings.ReplaceAll(md, "!["+name+"]("+mediaRef+")", "!["+name+"]("+AttachmentScheme+a.ID+")")
	}
	return md
}

// maxAttachmentBytes caps a download; bigger files are not worth drawing.
const maxAttachmentBytes = 16 << 20

// AttachmentContent downloads an attachment's bytes. Jira redirects to its
// media service, which the http client follows.
func (c *Client) AttachmentContent(ctx context.Context, id string) ([]byte, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+"/rest/api/3/attachment/content/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call jira: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError(resp.StatusCode, "attachment "+id, body)
	}
	if len(body) > maxAttachmentBytes {
		return nil, fmt.Errorf("attachment %s is over %d MB", id, maxAttachmentBytes>>20)
	}
	return body, nil
}
