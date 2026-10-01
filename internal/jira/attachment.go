package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	reqCtx, moved, stop := c.stallGuard(ctx)
	defer stop()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+"/rest/api/3/attachment/content/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.transfer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call jira: %w", stallCause(reqCtx, err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(progressReader{resp.Body, moved}, maxAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", stallCause(reqCtx, err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError(resp.StatusCode, "attachment "+id, body, resp.Header.Get("Retry-After"))
	}
	if len(body) > maxAttachmentBytes {
		return nil, fmt.Errorf("attachment %s is over %d MB", id, maxAttachmentBytes>>20)
	}
	return body, nil
}

// AttachmentURL is where a browser signed in to Jira downloads attachment id.
func (c *Client) AttachmentURL(id string) string {
	if c == nil || c.baseURL == "" {
		return ""
	}
	return c.baseURL + "/rest/api/3/attachment/content/" + url.PathEscape(id)
}

// DeleteAttachment deletes attachment id from key.
func (c *Client) DeleteAttachment(ctx context.Context, key, id string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	if err := c.do(ctx, http.MethodDelete, "/rest/api/3/attachment/"+url.PathEscape(id), key, nil, nil); err != nil {
		return err
	}
	c.Invalidate(key)
	return nil
}

// UploadAttachment attaches the file at path to key, streamed from disk.
func (c *Client) UploadAttachment(ctx context.Context, key, path string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = c.UploadAttachmentFrom(ctx, key, filepath.Base(path), f)
	return err
}

// UploadAttachmentFrom attaches what f holds to key as name and returns the
// attachment Jira made (zero when it names none).
func (c *Client) UploadAttachmentFrom(ctx context.Context, key, name string, f io.Reader) (Attachment, error) {
	if !c.Enabled() {
		return Attachment{}, errNotConfigured
	}
	reqCtx, moved, stop := c.stallGuard(ctx)
	defer stop()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, progressReader{f, moved})
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/rest/api/3/issue/"+url.PathEscape(key)+"/attachments", pr)
	if err != nil {
		return Attachment{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check") // Jira's CSRF guard for uploads
	c.writing.Add(1)
	defer c.writing.Add(-1)
	resp, err := c.transfer.Do(req)
	if err != nil {
		return Attachment{}, fmt.Errorf("call jira: %w", stallCause(reqCtx, err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Attachment{}, statusError(resp.StatusCode, "upload to "+key, body, resp.Header.Get("Retry-After"))
	}
	c.Invalidate(key)
	var made []apiAttachment
	if json.Unmarshal(body, &made) != nil || len(made) == 0 {
		return Attachment{}, nil
	}
	a := made[0]
	return Attachment{ID: a.ID, Filename: a.Filename, MimeType: a.MimeType, Size: a.Size}, nil
}

// mediaFile finds the Media Services file id in where Jira sends an
// attachment's content: …/file/<uuid>/binary?….
var mediaFile = regexp.MustCompile(`/file/([0-9a-fA-F-]{36})/`)

// MediaID is the Media Services file id an ADF media node needs to show
// attachment id. Jira has no API for it; its content redirect names it.
func (c *Client) MediaID(ctx context.Context, id string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/rest/api/3/attachment/content/"+url.PathEscape(id), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	hc := *c.transfer
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("call jira: %w", err)
	}
	resp.Body.Close()
	if m := mediaFile.FindStringSubmatch(resp.Header.Get("Location")); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("attachment %s: no media id (%s)", id, resp.Status)
}

// imageLine is a markdown image alone on its line pointing at an attachment.
var imageLine = regexp.MustCompile(`^( *)!\[([^\]]*)\]\(` + AttachmentScheme + `(\d+)\)\s*$`)

// EmbedImages points each image line at an attachment (![name](attachment:ID),
// the web editor's pasted image) at its media file instead, so
// MarkdownToADF makes it a picture in the document. One whose media id
// can't be had stays as it is.
func (c *Client) EmbedImages(ctx context.Context, md string) string {
	if !strings.Contains(md, "]("+AttachmentScheme) {
		return md
	}
	lines := strings.Split(md, "\n")
	fence := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fence = !fence
		}
		m := imageLine.FindStringSubmatch(ln)
		if fence || m == nil {
			continue
		}
		if id, err := c.MediaID(ctx, m[3]); err == nil {
			lines[i] = m[1] + "![" + m[2] + "](" + mediaScheme + id + ")"
		}
	}
	return strings.Join(lines, "\n")
}

// mediaScheme prefixes a Media Services file id in a markdown image target;
// MarkdownToADF makes such a line a mediaSingle.
const mediaScheme = "media:"

// DownloadAttachment saves attachment id as name in dir, never over an
// existing file ("a (1).png"), and returns the path written.
func (c *Client) DownloadAttachment(ctx context.Context, id, name, dir string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	reqCtx, moved, stop := c.stallGuard(ctx)
	defer stop()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.AttachmentURL(id), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.transfer.Do(req)
	if err != nil {
		return "", fmt.Errorf("call jira: %w", stallCause(reqCtx, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", statusError(resp.StatusCode, "attachment "+id, body, resp.Header.Get("Retry-After"))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, path, err := createFree(dir, filepath.Base(name))
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(f, progressReader{resp.Body, moved}); err == nil {
		err = f.Close()
	} else {
		f.Close()
		err = stallCause(reqCtx, err)
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// errStalled is a transfer that moved nothing for the client's timeout.
var errStalled = errors.New("transfer stalled")

// stallGuard bounds a transfer by its progress, not its length: the context
// ends once moved went uncalled for c.timeout. stop releases it.
func (c *Client) stallGuard(ctx context.Context) (context.Context, func(), func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	t := time.AfterFunc(c.timeout, func() { cancel(fmt.Errorf("%w: nothing moved for %s", errStalled, c.timeout)) })
	return ctx, func() { t.Reset(c.timeout) }, func() { t.Stop(); cancel(nil) }
}

// stallCause is err, or why stallGuard ended ctx.
func stallCause(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
		return cause
	}
	return err
}

// progressReader calls moved on every read that got bytes.
type progressReader struct {
	r     io.Reader
	moved func()
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.moved()
	}
	return n, err
}

// createFree creates name in dir, or "name (n).ext" when taken.
func createFree(dir, name string) (*os.File, string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 0; n < 1000; n++ {
		try := name
		if n > 0 {
			try = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		path := filepath.Join(dir, try)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("no free name for %s in %s", name, dir)
}
