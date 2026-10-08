package ui

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// The inbox and its ✉ count take in every configured site, not only the
// one shown: another site's threads say which, and open in the browser.

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

// inboxThreads are every site's threads, newest news first: prev's
// reused where the issue was not updated since, the rest read again.
// Another site failing only leaves it out.
func inboxThreads(ctx context.Context, sites map[string]*jira.Client, shown string, since time.Time, prev []inboxThread) ([]inboxThread, error) {
	known := map[string]inboxThread{}
	for _, t := range prev {
		known[t.id()] = t
	}
	var (
		out     []inboxThread
		mu      sync.Mutex
		wg      sync.WaitGroup
		shownEr error
	)
	for site, c := range sites {
		wg.Go(func() {
			ts, err := siteThreads(ctx, c, site, since, known)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && site == shown {
				shownEr = err
			}
			out = append(out, ts...)
		})
	}
	wg.Wait()
	if shownEr != nil {
		return nil, shownEr
	}
	slices.SortStableFunc(out, func(a, b inboxThread) int { return b.latest().Compare(a.latest()) })
	return out, nil
}

// siteThreads are one site's threads, the issues without news by others
// too, so they are not read again while unchanged. An issue that fails to
// read keeps its last thread, or is left out, and is read again next time;
// only when every read fails does the site fail.
func siteThreads(ctx context.Context, c *jira.Client, site string, since time.Time, known map[string]inboxThread) ([]inboxThread, error) {
	issues, err := c.InboxIssues(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]inboxThread, len(issues))
	errs := make([]error, len(issues))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	read := 0
	for i, is := range issues {
		out[i] = inboxThread{InboxIssue: is, site: site, url: c.BrowseURL(is.Key)}
		if k, ok := known[out[i].id()]; ok && k.Updated.Equal(is.Updated) {
			out[i].entries = slices.DeleteFunc(slices.Clone(k.entries), func(e jira.InboxEntry) bool { return !e.When.After(since) })
			continue
		}
		read++
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i].entries, errs[i] = c.IssueInbox(ctx, is.Key, is.Summary, since)
		})
	}
	wg.Wait()
	failed := 0
	for i, err := range errs {
		if err == nil {
			continue
		}
		failed++
		if k, ok := known[out[i].id()]; ok {
			out[i] = k // its old Updated: read again next time
		} else {
			out[i] = inboxThread{}
		}
	}
	if failed > 0 && failed == read {
		return nil, errors.Join(errs...)
	}
	return slices.DeleteFunc(out, func(t inboxThread) bool { return t.Key == "" }), nil
}
