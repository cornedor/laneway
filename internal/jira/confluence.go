package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cornedor/laneway/internal/safeterm"
)

// Confluence pages linked from an issue, read as markdown with the site's
// own login: Confluence Cloud lives on the same host as Jira, under /wiki.

// Page is a Confluence page as the panel shows it.
type Page struct {
	ID, Title, URL string
	Markdown       string
}

var pagePathRe = regexp.MustCompile(`/wiki/spaces/[^/]+/pages/(\d+)`)

// PageID is the id of the Confluence page u links on this site
// (…/wiki/spaces/KEY/pages/123/Title, …/viewpage.action?pageId=123); ""
// for anything else, a page on another site too.
func (c *Client) PageID(u string) string {
	pu, err := url.Parse(u)
	base, err2 := url.Parse(c.baseURL)
	if err != nil || err2 != nil || !strings.EqualFold(pu.Host, base.Host) || !strings.HasPrefix(pu.Path, "/wiki/") {
		return ""
	}
	if m := pagePathRe.FindStringSubmatch(pu.Path); m != nil {
		return m[1]
	}
	if id := pu.Query().Get("pageId"); id != "" && strings.Trim(id, "0123456789") == "" {
		return id
	}
	return ""
}

// ConfluencePage reads page id, its body as markdown. Its images stand as
// their names: they are the page's attachments, not the issue's.
func (c *Client) ConfluencePage(ctx context.Context, id string) (Page, error) {
	if !c.Enabled() {
		return Page{}, errNotConfigured
	}
	var resp struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  struct {
			ADF struct {
				Value string `json:"value"` // the document as a JSON string
			} `json:"atlas_doc_format"`
		} `json:"body"`
		Links struct {
			WebUI string `json:"webui"`
			Base  string `json:"base"`
		} `json:"_links"`
	}
	path := "/wiki/api/v2/pages/" + url.PathEscape(id) + "?body-format=atlas_doc_format"
	if err := c.do(ctx, http.MethodGet, path, "page "+id, nil, &resp); err != nil {
		return Page{}, err
	}
	md := pageImageRe.ReplaceAllString(adfToMarkdown(json.RawMessage(resp.Body.ADF.Value)), "_[image: $1]_")
	base := strings.TrimSuffix(resp.Links.Base, "/")
	if base == "" {
		base = c.baseURL + "/wiki"
	}
	return Page{ID: resp.ID, Title: safeterm.Line(resp.Title), URL: base + resp.Links.WebUI, Markdown: safeterm.Text(md)}, nil
}

// pageImageRe is an image of the page's: its name only.
var pageImageRe = regexp.MustCompile(`!\[([^\]\n]*)\]\(` + mediaRef + `\)`)
