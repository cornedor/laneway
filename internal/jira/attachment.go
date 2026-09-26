package jira

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
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

// AttachmentURL is where a browser signed in to Jira downloads attachment id.
func (c *Client) AttachmentURL(id string) string {
	if c == nil || c.baseURL == "" {
		return ""
	}
	return c.baseURL + "/rest/api/3/attachment/content/" + url.PathEscape(id)
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
	return c.UploadAttachmentFrom(ctx, key, filepath.Base(path), f)
}

// UploadAttachmentFrom attaches what f holds to key as name.
func (c *Client) UploadAttachmentFrom(ctx context.Context, key, name string, f io.Reader) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rest/api/3/issue/"+url.PathEscape(key)+"/attachments", pr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check") // Jira's CSRF guard for uploads
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call jira: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return statusError(resp.StatusCode, "upload to "+key, body)
	}
	c.Invalidate(key)
	return nil
}

// DownloadAttachment saves attachment id as name in dir, never over an
// existing file ("a (1).png"), and returns the path written.
func (c *Client) DownloadAttachment(ctx context.Context, id, name, dir string) (string, error) {
	if !c.Enabled() {
		return "", errNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.AttachmentURL(id), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call jira: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", statusError(resp.StatusCode, "attachment "+id, body)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, path, err := createFree(dir, filepath.Base(name))
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(f, resp.Body); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
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
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("no free name for %s in %s", name, dir)
}
