package ui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// The inbox and its ✉ count take in every configured site, not only the
// one shown: another site's entries say which, and open in the browser.

// siteClients makes a client for another configured site on first use.
type siteClients struct {
	mu   sync.Mutex
	make func(site string) (*jira.Client, error)
	got  map[string]*jira.Client
}

// WithSiteClients lets the inbox reach the other sites: make builds a
// site's client (running its api_token_cmd) the first time it is needed.
func (m Model) WithSiteClients(make func(site string) (*jira.Client, error)) Model {
	m.otherSites = &siteClients{make: make, got: map[string]*jira.Client{}}
	return m
}

// others are the clients of the configured sites besides the shown one;
// a site that fails to build is left out.
func (m *Model) others() map[string]*jira.Client {
	s := m.otherSites
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]*jira.Client{}
	for _, site := range m.sites {
		if site == m.site {
			continue
		}
		c, ok := s.got[site]
		if !ok {
			c, _ = s.make(site) // nil on a failure: not asked again
			s.got[site] = c
		}
		if c != nil && c.Enabled() {
			out[site] = c
		}
	}
	return out
}

// siteEntry is an inbox entry and the site it is from, "" for this one.
type siteEntry struct {
	jira.InboxEntry
	site string
	url  string
}

// inboxAll is the shown site's inbox and the others', mentions first,
// then newest; another site failing only leaves it out.
func inboxAll(ctx context.Context, c *jira.Client, others map[string]*jira.Client, since time.Time) ([]siteEntry, error) {
	entries, err := c.Inbox(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]siteEntry, len(entries))
	for i, e := range entries {
		out[i] = siteEntry{InboxEntry: e}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for site, oc := range others {
		wg.Add(1)
		go func() {
			defer wg.Done()
			es, err := oc.Inbox(ctx, since)
			if err != nil {
				return
			}
			mu.Lock()
			for _, e := range es {
				out = append(out, siteEntry{InboxEntry: e, site: site, url: oc.BrowseURL(e.Key)})
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.SortStableFunc(out, func(a, b siteEntry) int {
		if a.Mention != b.Mention {
			if a.Mention {
				return -1
			}
			return 1
		}
		return b.When.Compare(a.When)
	})
	return out, nil
}

// inboxCountAll adds the other sites' counts to this one's.
func inboxCountAll(ctx context.Context, c *jira.Client, others map[string]*jira.Client, since time.Time) (int, error) {
	n, err := c.InboxCount(ctx, since)
	if err != nil {
		return 0, err
	}
	for _, oc := range others {
		if k, err := oc.InboxCount(ctx, since); err == nil {
			n += k
		}
	}
	return n, nil
}

// siteEntryID is an inbox row's id for another site's entry: its URL.
const siteEntryPrefix = "\x00site "

func siteEntryURL(id string) (string, bool) {
	return strings.CutPrefix(id, siteEntryPrefix)
}
