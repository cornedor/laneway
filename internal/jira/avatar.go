package jira

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxAvatarBytes caps an avatar download; they are small.
const maxAvatarBytes = 1 << 20

// Avatar downloads an avatar image. The API credentials go only to the
// instance itself, never to another host (Gravatar, Atlassian's avatar
// CDN); Go drops them on a redirect elsewhere too. The URL comes from a
// response, so only the instance and Atlassian's avatar hosts are fetched,
// redirects included: never localhost or a cloud metadata address.
func (c *Client) Avatar(ctx context.Context, avatarURL string) ([]byte, error) {
	u, err := url.Parse(avatarURL)
	if err != nil || !c.avatarHost(u) {
		return nil, fmt.Errorf("avatar: bad url %q", avatarURL)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Rebuilt from the parsed, checked parts: no userinfo, fragment or opaque.
	safe := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, safe.String(), nil)
	if err != nil {
		return nil, err
	}
	if base, err := url.Parse(c.baseURL); err == nil && c.auth != "" && u.Host == base.Host {
		req.Header.Set("Authorization", c.auth)
	}
	hc := *c.http
	hc.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !c.avatarHost(r.URL) {
			return fmt.Errorf("avatar: redirect to %s refused", r.URL.Host)
		}
		return nil
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("avatar: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes))
}

// avatarHosts are the hosts, and their subdomains, Jira Cloud's avatar URLs
// point at besides the instance: Gravatar, which falls back to Atlassian's
// initials images, and Atlassian's avatar CDN.
var avatarHosts = []string{"gravatar.com", "atl-paas.net", "atlassian.net", "atlassian.com"}

// avatarHost is whether u may be fetched as an avatar: the instance itself,
// or https to one of avatarHosts.
func (c *Client) avatarHost(u *url.URL) bool {
	if (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" {
		return false
	}
	if base, err := url.Parse(c.baseURL); err == nil && base.Host != "" && u.Host == base.Host {
		return true
	}
	if u.Scheme != "https" || u.Port() != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, d := range avatarHosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
