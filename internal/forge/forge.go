// Package forge is the provider-neutral half of laneway's merge request
// support: the render-ready types the UI draws and the Provider interface a
// forge implements. A "change request" is whatever the forge calls the thing
// you open to get a branch merged: a GitLab merge request, a GitHub pull
// request.
//
// Subpackages own the wire format (internal/forge/gitlab). They flatten their
// API into the types here, including normalizing check/job status onto the
// small vocabulary below, so the UI has one set of glyphs to know about rather
// than one per forge.
//
// Ported from matterbox's internal/forge (Corné Dorrestijn, Jasper Kuiper).
package forge

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The change request's lifecycle state. The vocabulary is GitLab's (every forge
// maps onto it): a GitHub pull request that is closed-and-merged reports
// StateMerged, not StateClosed.
const (
	StateOpen   = "opened"
	StateMerged = "merged"
	StateClosed = "closed"
	StateLocked = "locked"
)

// The job/check status vocabulary. Providers map their own tokens onto these, so
// the UI has one glyph table and one severity ranking. Anything a provider can't
// classify becomes StatusPending — surfaced as in-progress rather than silently
// reported as success.
const (
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusRunning  = "running"
	StatusPending  = "pending"
	StatusCanceled = "canceled"
	StatusSkipped  = "skipped"
	StatusManual   = "manual"
	StatusWarning  = "warning" // failed, but allowed to
)

// Provider is one forge instance laneway reads change requests from.
// Implementations are safe for concurrent use: the UI fetches from background
// goroutines.
type Provider interface {
	// Name is the forge's display name ("GitLab"), for headings and errors.
	Name() string
	// Noun is what this forge calls a change request ("merge request").
	Noun() string
	// Enabled reports whether the provider has enough configuration to fetch.
	Enabled() bool
	// BaseURL is the instance root (no trailing slash).
	BaseURL() string
	// Parse reads a change request's web link, as Jira's development panel
	// has it; ok is false for a link that is not one on this instance.
	Parse(link string) (Ref, bool)
	// WebURL is the human page for a change request.
	WebURL(repo string, number int) string
	// Get returns the change request, serving a cached copy when it has one.
	Get(ctx context.Context, repo string, number int) (*Change, error)
	// Invalidate drops any cached copy so the next Get refetches.
	Invalidate(repo string, number int)
	// Approve records an approval.
	Approve(ctx context.Context, repo string, number int) error
	// Merge merges the change request.
	Merge(ctx context.Context, repo string, number int) error
}

// Ref names a change request: the repository it lives in (a GitLab project
// path) and its number there.
type Ref struct {
	Repo   string
	Number int
}

// Change is the flattened, render-ready change request. Description is the
// forge's own markdown flavour.
type Change struct {
	Repo         string
	Number       int
	Title        string
	State        string // one of the State* constants
	Draft        bool
	Author       string
	SourceBranch string
	TargetBranch string
	Assignees    []string
	Reviewers    []string
	Labels       []string
	ChangesCount string // "44", or "44+" when the forge caps it
	Notes        int    // the people's comments, system notes left out
	// Mergeable is whether the forge would accept a merge right now — the gate
	// for offering the merge action. MergeStatus is the human phrase shown
	// either way ("mergeable", "ci still running", "conflicts").
	Mergeable    bool
	MergeStatus  string
	HasConflicts bool
	Description  string
	WebURL       string
	UpdatedAt    time.Time

	Checks    *Checks    // nil when the change request has no CI
	Approvals *Approvals // nil when approvals couldn't be read (best-effort)
}

// Checks is the CI verdict for the change request's head commit: an overall
// status plus the per-job breakdown, grouped the way the forge groups it
// (GitLab pipeline stages).
type Checks struct {
	Status   string // normalized: one of the Status* constants
	Label    string // human label, e.g. "passed"
	WebURL   string
	Duration int // seconds, 0 when unknown
	Groups   []Group
}

// Group is a named run of jobs — one pipeline stage.
type Group struct {
	Name string
	Jobs []Job
}

// Job is one CI job's name and normalized status.
type Job struct {
	ID     int // the forge's, for its log (0 when unknown)
	Name   string
	Status string
}

// JobLog is one CI job as its log view shows it: its state and its log so
// far, ANSI colours and all. Truncated: only the log's end is here.
type JobLog struct {
	Job
	Stage     string
	Duration  int // seconds, 0 when unknown
	WebURL    string
	Log       string
	Truncated bool
}

// Done reports whether the job has stopped: nothing more will come.
func (j *JobLog) Done() bool {
	return j.Status != StatusRunning && j.Status != StatusPending
}

// Approvals summarizes who has signed off. Required/Left are GitLab's approval
// rules; forges without a readable requirement leave them 0 and the UI falls
// back to listing names.
type Approvals struct {
	Approved bool
	Required int
	Left     int
	By       []string
}

// ErrNotConfigured is what every call returns when a provider lacks the base
// URL or token it needs (Enabled reports false).
var ErrNotConfigured = errors.New("forge: not configured (need a base URL and a token)")

// NormalizeBaseURL trims a base URL and supplies a default https:// scheme when
// it's missing, so a bare host like "git.example.com" still parses to a host
// (url.Parse treats a scheme-less string as a path) and produces working
// API/browse URLs.
func NormalizeBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return raw
}

// HostOf returns the lowercased host of a URL, or "" if it doesn't parse to one.
func HostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Host)
}

// Cache memoises fetched change requests per provider, keyed case-insensitively
// by repo and number. The zero value is ready to use.
type Cache struct {
	mu sync.Mutex
	m  map[string]*Change
}

func cacheKey(repo string, number int) string {
	return strings.ToLower(repo) + "#" + strconv.Itoa(number)
}

// Get returns the cached change request, if any.
func (c *Cache) Get(repo string, number int) (*Change, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.m[cacheKey(repo, number)]
	return ch, ok
}

// Put stores a fetched change request.
func (c *Cache) Put(repo string, number int, ch *Change) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*Change{}
	}
	c.m[cacheKey(repo, number)] = ch
}

// Invalidate drops one entry so the next fetch goes to the forge.
func (c *Cache) Invalidate(repo string, number int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, cacheKey(repo, number))
}
