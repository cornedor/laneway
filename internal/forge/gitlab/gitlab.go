// Package gitlab is the GitLab implementation of forge.Provider: merge
// requests, their pipelines, approvals, diffs and discussions from a GitLab
// instance. Like internal/jira it has no dependency on the UI, so it is tested
// against httptest servers.
//
// Authentication is a personal access token sent in the PRIVATE-TOKEN header
// (read_api to read, api to approve, merge and comment). Descriptions come
// back as GitLab-flavored markdown.
//
// Ported from matterbox's internal/forge/gitlab (Corné Dorrestijn, Jasper
// Kuiper).
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/forge"
)

// mrFields is the field list requested from the API. The default response is
// already compact, but naming fields keeps the decode predictable.
const mrFields = "title,state,draft,author,source_branch,target_branch,assignees," +
	"reviewers,labels,changes_count,detailed_merge_status,has_conflicts,description," +
	"web_url,updated_at,head_pipeline,project_id"

// Config is one instance: BaseURL its root (https://git.example.com), Token
// a personal access token.
type Config struct {
	BaseURL string
	Token   string
}

// Client fetches and caches merge requests for one instance. The zero value is
// not usable; use New. Safe for concurrent use.
type Client struct {
	baseURL string // trimmed of any trailing slash
	token   string
	rest    *forge.REST
	cache   forge.Cache
	// diffs memoises the review diff separately from the change itself: it is a
	// different (and much larger) fetch, made only when the diff view is opened.
	diffs forge.Store[*forge.Diff]
}

var _ forge.Provider = (*Client)(nil)

// New builds a Client from cfg. The returned client is always non-nil; call
// Enabled to see whether it has enough configuration to actually fetch.
func New(cfg Config) *Client {
	base := forge.NormalizeBaseURL(cfg.BaseURL)
	token := strings.TrimSpace(cfg.Token)
	return &Client{
		baseURL: base,
		token:   token,
		rest: forge.NewREST(base+"/api/v4", "gitlab", func(r *http.Request) {
			r.Header.Set("PRIVATE-TOKEN", token)
		}),
	}
}

// Name is the forge's display name.
func (c *Client) Name() string { return "GitLab" }

// Noun is what GitLab calls a change request.
func (c *Client) Noun() string { return "merge request" }

// Enabled reports whether the client has a base URL and token — i.e. whether a
// fetch can succeed.
func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

// BaseURL returns the instance root (no trailing slash).
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// mrURLRe pulls the project path and iid out of a merge request link:
// https://host/group/.../project/-/merge_requests/123, nested groups whole.
var mrURLRe = regexp.MustCompile(`^https?://([^/\s]+)/(\S+?)/-/merge_requests/(\d+)`)

// Parse reads a merge request link on this instance.
func (c *Client) Parse(link string) (forge.Ref, bool) {
	m := mrURLRe.FindStringSubmatch(strings.TrimSpace(link))
	if m == nil || !strings.EqualFold(m[1], forge.HostOf(c.BaseURL())) {
		return forge.Ref{}, false
	}
	iid, err := strconv.Atoi(m[3])
	project := strings.Trim(m[2], "/")
	if err != nil || project == "" {
		return forge.Ref{}, false
	}
	return forge.Ref{Repo: project, Number: iid}, true
}

// WebURL returns the human merge request URL.
func (c *Client) WebURL(project string, iid int) string {
	if c == nil || c.baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/-/merge_requests/%d", c.baseURL, project, iid)
}

// label is a merge request as GitLab writes it: group/project!12.
func label(project string, iid int) string { return project + "!" + strconv.Itoa(iid) }

// User is the token's account: the connection check.
type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Me returns the account the token belongs to.
func (c *Client) Me(ctx context.Context) (User, error) {
	if !c.Enabled() {
		return User{}, forge.ErrNotConfigured
	}
	var u User
	err := c.rest.Do(ctx, http.MethodGet, "/user", "user", nil, &u)
	return u, err
}

// apiMR mirrors the slice of the REST response we read.
type apiMR struct {
	IID                 int       `json:"iid"`
	ProjectID           int       `json:"project_id"`
	Title               string    `json:"title"`
	State               string    `json:"state"`
	Draft               bool      `json:"draft"`
	SourceBranch        string    `json:"source_branch"`
	TargetBranch        string    `json:"target_branch"`
	Labels              []string  `json:"labels"`
	ChangesCount        string    `json:"changes_count"`
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	HasConflicts        bool      `json:"has_conflicts"`
	Description         string    `json:"description"`
	WebURL              string    `json:"web_url"`
	UpdatedAt           string    `json:"updated_at"`
	Author              *apiUser  `json:"author"`
	Assignees           []apiUser `json:"assignees"`
	Reviewers           []apiUser `json:"reviewers"`
	HeadPipeline        *struct {
		ID             int    `json:"id"`
		Status         string `json:"status"`
		WebURL         string `json:"web_url"`
		Duration       int    `json:"duration"`
		DetailedStatus *struct {
			Label string `json:"label"`
		} `json:"detailed_status"`
	} `json:"head_pipeline"`
}

type apiUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

// Get returns the merge request, serving a cached copy when present. It makes
// up to three calls: the MR itself (required), its pipeline jobs and its
// approvals (both best-effort — a failure leaves those sections empty rather
// than failing the whole fetch). Use Invalidate (then Get) to force a refetch.
func (c *Client) Get(ctx context.Context, project string, iid int) (*forge.Change, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	if hit, ok := c.cache.Get(project, iid); ok {
		return hit, nil
	}
	mr, err := c.fetch(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	c.cache.Put(project, iid, mr)
	return mr, nil
}

// Invalidate drops any cached copy of the MR — and of its diff — so the next
// Get / Diff refetches.
func (c *Client) Invalidate(project string, iid int) {
	if c == nil {
		return
	}
	c.cache.Invalidate(project, iid)
	c.diffs.Invalidate(project, iid)
}

func (c *Client) fetch(ctx context.Context, project string, iid int) (*forge.Change, error) {
	base := fmt.Sprintf("/projects/%s/merge_requests/%d", encodePath(project), iid)
	body, err := c.rest.DoRaw(ctx, http.MethodGet, base+"?fields="+mrFields, label(project, iid), nil)
	if err != nil {
		return nil, err
	}
	var a apiMR
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("decode merge request: %w", err)
	}
	mr := toChange(a, project)
	mr.WebURL = c.WebURL(project, iid)

	// Pipeline jobs (best-effort): a per-job breakdown.
	if a.HeadPipeline != nil && a.HeadPipeline.ID != 0 && a.ProjectID != 0 {
		if groups, jerr := c.pipelineGroups(ctx, a.ProjectID, a.HeadPipeline.ID); jerr == nil {
			mr.Checks.Groups = groups
		}
	}
	// Approvals (best-effort): may be disabled on the instance/project.
	if ap, aerr := c.approvals(ctx, project, iid); aerr == nil {
		mr.Approvals = ap
	}
	return mr, nil
}

// toChange flattens the API response into the render-ready change (minus the
// URL and the best-effort pipeline jobs / approvals the caller fills in).
func toChange(a apiMR, project string) *forge.Change {
	ch := &forge.Change{
		Repo:         project,
		Number:       a.IID,
		Title:        a.Title,
		State:        a.State,
		Draft:        a.Draft,
		SourceBranch: a.SourceBranch,
		TargetBranch: a.TargetBranch,
		Labels:       a.Labels,
		ChangesCount: a.ChangesCount,
		HasConflicts: a.HasConflicts,
		Description:  a.Description,
		Mergeable:    a.DetailedMergeStatus == "mergeable",
	}
	ch.MergeStatus = mergeStatusText(a)
	if a.Author != nil {
		ch.Author = a.Author.Name
	}
	for _, u := range a.Assignees {
		ch.Assignees = append(ch.Assignees, u.Name)
	}
	for _, u := range a.Reviewers {
		ch.Reviewers = append(ch.Reviewers, u.Name)
	}
	if t, err := time.Parse(time.RFC3339, a.UpdatedAt); err == nil {
		ch.UpdatedAt = t
	}
	if hp := a.HeadPipeline; hp != nil {
		checks := &forge.Checks{
			Status:   normStatus(hp.Status),
			WebURL:   hp.WebURL,
			Duration: hp.Duration,
		}
		if hp.DetailedStatus != nil {
			checks.Label = hp.DetailedStatus.Label
		}
		if checks.Label == "" {
			checks.Label = hp.Status
		}
		ch.Checks = checks
	}
	return ch
}

// mergeStatusText is the merge-readiness phrase: "mergeable", or the humanized
// blocking reason (ci_still_running → "ci still running") plus an explicit
// conflicts note when GitLab flags one that the reason doesn't mention.
func mergeStatusText(a apiMR) string {
	if a.DetailedMergeStatus == "mergeable" {
		return "mergeable"
	}
	txt := strings.ReplaceAll(a.DetailedMergeStatus, "_", " ")
	if txt == "" {
		txt = "not mergeable"
	}
	if a.HasConflicts && !strings.Contains(txt, "conflict") {
		txt += " · conflicts"
	}
	return txt
}

// pipelineGroups fetches the pipeline's jobs and groups them by stage, in the
// order the stages first appear in the response.
func (c *Client) pipelineGroups(ctx context.Context, projectID, pipelineID int) ([]forge.Group, error) {
	path := fmt.Sprintf("/projects/%d/pipelines/%d/jobs?per_page=100", projectID, pipelineID)
	var jobs []struct {
		Name   string `json:"name"`
		Stage  string `json:"stage"`
		Status string `json:"status"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, "pipeline jobs", nil, &jobs); err != nil {
		return nil, err
	}
	var order []string
	byStage := map[string][]forge.Job{}
	for _, j := range jobs {
		if _, ok := byStage[j.Stage]; !ok {
			order = append(order, j.Stage)
		}
		byStage[j.Stage] = append(byStage[j.Stage], forge.Job{Name: j.Name, Status: normStatus(j.Status)})
	}
	groups := make([]forge.Group, 0, len(order))
	for _, name := range order {
		groups = append(groups, forge.Group{Name: name, Jobs: byStage[name]})
	}
	return groups, nil
}

// normStatus maps a GitLab job/pipeline status onto the shared vocabulary.
func normStatus(s string) string {
	switch s {
	case "success", "passed":
		return forge.StatusSuccess
	case "failed":
		return forge.StatusFailed
	case "running":
		return forge.StatusRunning
	case "canceled", "canceling", "cancelled":
		return forge.StatusCanceled
	case "skipped":
		return forge.StatusSkipped
	case "manual":
		return forge.StatusManual
	default: // created / pending / scheduled / preparing / waiting_for_resource
		return forge.StatusPending
	}
}

// approvals fetches the MR's approval summary.
func (c *Client) approvals(ctx context.Context, project string, iid int) (*forge.Approvals, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/approvals", encodePath(project), iid)
	var resp struct {
		Approved          bool `json:"approved"`
		ApprovalsRequired int  `json:"approvals_required"`
		ApprovalsLeft     int  `json:"approvals_left"`
		ApprovedBy        []struct {
			User apiUser `json:"user"`
		} `json:"approved_by"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, "approvals", nil, &resp); err != nil {
		return nil, err
	}
	ap := &forge.Approvals{Approved: resp.Approved, Required: resp.ApprovalsRequired, Left: resp.ApprovalsLeft}
	for _, a := range resp.ApprovedBy {
		ap.By = append(ap.By, a.User.Name)
	}
	return ap, nil
}

// Approve approves the merge request, then invalidates the cache so the next
// Get reflects the new approval state.
func (c *Client) Approve(ctx context.Context, project string, iid int) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/approve", encodePath(project), iid)
	if err := c.rest.Do(ctx, http.MethodPost, path, "approve", nil, nil); err != nil {
		return err
	}
	c.Invalidate(project, iid)
	return nil
}

// Merge merges the merge request and asks GitLab to delete the source branch,
// then invalidates the cache.
func (c *Client) Merge(ctx context.Context, project string, iid int) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/merge", encodePath(project), iid)
	body := map[string]any{"should_remove_source_branch": true}
	if err := c.rest.Do(ctx, http.MethodPut, path, "merge", body, nil); err != nil {
		return err
	}
	c.Invalidate(project, iid)
	return nil
}

// encodePath URL-encodes a project path for the API, turning the namespace
// slashes into %2F (e.g. group/project → group%2Fproject) as GitLab requires.
func encodePath(project string) string {
	return strings.ReplaceAll(project, "/", "%2F")
}

// Search returns the merge requests whose title names key (an issue key),
// on every project the token sees, newest first. GitLab's search matches
// words loosely, so a title must hold key itself: ABC-1 is not ABC-12.
func (c *Client) Search(ctx context.Context, key string) ([]*forge.Change, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	path := "/merge_requests?scope=all&state=all&in=title&per_page=20&order_by=updated_at&search=" + url.QueryEscape(key)
	var found []apiMR
	if err := c.rest.Do(ctx, http.MethodGet, path, "merge request search", nil, &found); err != nil {
		return nil, err
	}
	named := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(key) + `($|[^0-9])`)
	var out []*forge.Change
	for _, a := range found {
		r, ok := c.Parse(a.WebURL)
		if !ok || !named.MatchString(a.Title) && !named.MatchString(a.SourceBranch) {
			continue
		}
		ch := toChange(a, r.Repo)
		ch.WebURL = a.WebURL
		out = append(out, ch)
	}
	return out, nil
}
