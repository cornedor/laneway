package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxAvatarBytes caps an avatar download; they are small.
const maxAvatarBytes = 1 << 20

// Avatar downloads an avatar image. The API credentials go only to the
// instance itself, never to another host (Gravatar, Atlassian's avatar
// CDN); Go drops them on a redirect elsewhere too.
func (c *Client) Avatar(ctx context.Context, avatarURL string) ([]byte, error) {
	u, err := url.Parse(avatarURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("avatar: bad url %q", avatarURL)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if base, err := url.Parse(c.baseURL); err == nil && c.auth != "" && u.Host == base.Host {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("avatar: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes))
}
