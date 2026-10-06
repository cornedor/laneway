// Package gitlab is the GitLab implementation of forge.Provider: merge
// requests, their pipelines, approvals, diffs and discussions from a GitLab
// instance. Like internal/jira it has no dependency on the UI, so it is tested
// against httptest servers.
//
// Authentication is a personal access token sent in the PRIVATE-TOKEN header
// (read_api to read, api to approve, merge, edit and comment). Descriptions come
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/forge"
)

// mrFields is the field list requested from the API. The default response is
// already compact, but naming fields keeps the decode predictable.
const mrFields = "title,state,draft,author,source_branch,target_branch,assignees," +
	"reviewers,labels,changes_count,detailed_merge_status,has_conflicts,description," +
	"web_url,updated_at,head_pipeline,project_id,squash,force_remove_source_branch"

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
	gql     *forge.REST // the GraphQL API, for what REST doesn't say
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
		gql: forge.NewREST(base+"/api", "gitlab", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
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
	UserNotesCount      int       `json:"user_notes_count"`
	Squash              bool      `json:"squash"`
	RemoveSourceBranch  bool      `json:"force_remove_source_branch"`
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
	ID       int    `json:"id"`
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
		Notes:        a.UserNotesCount,
		HasConflicts: a.HasConflicts,
		Squash:       a.Squash,
		DeleteBranch: a.RemoveSourceBranch,
		Description:  a.Description,
		Mergeable:    a.DetailedMergeStatus == "mergeable",
	}
	ch.MergeStatus = mergeStatusText(a)
	if a.Author != nil {
		ch.Author = a.Author.Name
	}
	for _, u := range a.Assignees {
		ch.Assignees = append(ch.Assignees, u.Name)
		ch.AssigneeIDs = append(ch.AssigneeIDs, u.ID)
	}
	for _, u := range a.Reviewers {
		ch.Reviewers = append(ch.Reviewers, u.Name)
		ch.ReviewerIDs = append(ch.ReviewerIDs, u.ID)
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

// mergeStatusPhrase is GitLab's detailed_merge_status in words: what stands
// between the merge request and a merge.
var mergeStatusPhrase = map[string]string{
	"mergeable":                  "ready to merge",
	"not_approved":               "needs approval",
	"requested_changes":          "changes requested",
	"ci_must_pass":               "pipeline must pass",
	"ci_still_running":           "pipeline still running",
	"discussions_not_resolved":   "threads to resolve",
	"draft_status":               "draft",
	"conflict":                   "conflicts",
	"need_rebase":                "needs a rebase",
	"not_open":                   "not open",
	"merge_request_blocked":      "blocked by another merge request",
	"jira_association_missing":   "needs a Jira key",
	"status_checks_must_pass":    "status checks must pass",
	"security_policy_violations": "security policy violated",
	"checking":                   "checking",
	"unchecked":                  "checking",
	"preparing":                  "checking",
	"approvals_syncing":          "checking",
}

// mergeStatusText is the merge-readiness phrase: "ready to merge", or the
// blocking reason in words (ci_still_running → "pipeline still running"),
// plus an explicit conflicts note when GitLab flags one the reason doesn't
// mention.
func mergeStatusText(a apiMR) string {
	txt, ok := mergeStatusPhrase[a.DetailedMergeStatus]
	switch {
	case ok:
	case a.DetailedMergeStatus == "":
		txt = "not mergeable"
	default:
		txt = strings.ReplaceAll(a.DetailedMergeStatus, "_", " ")
	}
	if a.HasConflicts && !strings.Contains(txt, "conflict") {
		txt += " · conflicts"
	}
	return txt
}

// pipelineGroups fetches the pipeline's jobs and groups them by stage, in the
// pipeline's order: GitLab lists the newest job first, so the walk runs from
// the end. A failure the job is allowed is a warning, as GitLab shows it.
func (c *Client) pipelineGroups(ctx context.Context, projectID, pipelineID int) ([]forge.Group, error) {
	path := fmt.Sprintf("/projects/%d/pipelines/%d/jobs?per_page=100", projectID, pipelineID)
	var jobs []apiJob
	if err := c.rest.Do(ctx, http.MethodGet, path, "pipeline jobs", nil, &jobs); err != nil {
		return nil, err
	}
	var order []string
	byStage := map[string][]forge.Job{}
	for _, j := range slices.Backward(jobs) {
		if _, ok := byStage[j.Stage]; !ok {
			order = append(order, j.Stage)
		}
		byStage[j.Stage] = append(byStage[j.Stage], forge.Job{ID: j.ID, Name: j.Name, Status: j.status()})
	}
	groups := make([]forge.Group, 0, len(order))
	for _, name := range order {
		groups = append(groups, forge.Group{Name: name, Jobs: byStage[name]})
	}
	return groups, nil
}

// apiJob is a CI job as the API sends it.
type apiJob struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Stage        string  `json:"stage"`
	Status       string  `json:"status"`
	AllowFailure bool    `json:"allow_failure"`
	Duration     float64 `json:"duration"`
	WebURL       string  `json:"web_url"`
}

// status is the job's status in the shared vocabulary: a failure it is
// allowed a warning, as GitLab shows it.
func (j apiJob) status() string {
	st := normStatus(j.Status)
	if st == forge.StatusFailed && j.AllowFailure {
		st = forge.StatusWarning
	}
	return st
}

// jobLogMax is how much of a job's log JobLog keeps: its end, where a
// failure is.
const jobLogMax = 512 << 10

// JobLog reads job id of project: its state and its log so far, never
// cached, so a running job's grows on every call.
func (c *Client) JobLog(ctx context.Context, project string, id int) (*forge.JobLog, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	base := fmt.Sprintf("/projects/%s/jobs/%d", encodePath(project), id)
	var j apiJob
	if err := c.rest.Do(ctx, http.MethodGet, base, "job", nil, &j); err != nil {
		return nil, err
	}
	b, err := c.rest.DoRaw(ctx, http.MethodGet, base+"/trace", "job log", nil)
	if err != nil {
		return nil, err
	}
	out := &forge.JobLog{Job: forge.Job{ID: j.ID, Name: j.Name, Status: j.status()}, Stage: j.Stage,
		Duration: int(j.Duration), WebURL: j.WebURL, Log: string(b)}
	if len(b) > jobLogMax {
		cut := len(b) - jobLogMax
		if nl := strings.IndexByte(out.Log[cut:], '\n'); nl >= 0 {
			cut += nl + 1
		}
		out.Log, out.Truncated = out.Log[cut:], true
	}
	return out, nil
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

// Merge merges the merge request as o says, then invalidates the cache.
func (c *Client) Merge(ctx context.Context, project string, iid int, o forge.MergeOptions) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/merge", encodePath(project), iid)
	body := map[string]any{"squash": o.Squash, "should_remove_source_branch": o.DeleteBranch}
	if err := c.rest.Do(ctx, http.MethodPut, path, "merge", body, nil); err != nil {
		return err
	}
	c.Invalidate(project, iid)
	return nil
}

// Edit is a change to a merge request: what is set is sent, the rest stays.
// Empty AssigneeIDs or ReviewerIDs (not nil) clear them; a draft is the
// title's prefix (DraftTitle).
type Edit struct {
	Title, Description, TargetBranch *string
	AssigneeIDs, ReviewerIDs         *[]int
	Labels                           *[]string
}

// Update writes e to the merge request, then invalidates the cache.
func (c *Client) Update(ctx context.Context, project string, iid int, e Edit) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	body := map[string]any{}
	if e.Title != nil {
		body["title"] = *e.Title
	}
	if e.Description != nil {
		body["description"] = *e.Description
	}
	if e.TargetBranch != nil {
		body["target_branch"] = *e.TargetBranch
	}
	ids := func(v []int) []int {
		if len(v) == 0 {
			return []int{0} // GitLab's "nobody"
		}
		return v
	}
	if e.AssigneeIDs != nil {
		body["assignee_ids"] = ids(*e.AssigneeIDs)
	}
	if e.ReviewerIDs != nil {
		body["reviewer_ids"] = ids(*e.ReviewerIDs)
	}
	if e.Labels != nil {
		body["labels"] = strings.Join(*e.Labels, ",")
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", encodePath(project), iid)
	if err := c.rest.Do(ctx, http.MethodPut, path, "edit "+label(project, iid), body, nil); err != nil {
		return err
	}
	c.Invalidate(project, iid)
	return nil
}

// draftRe is the title prefixes GitLab reads as a draft.
var draftRe = regexp.MustCompile(`(?i)^\s*(\[draft\]|\(draft\)|draft:|draft\s+-|\[wip\]|wip:)\s*`)

// DraftTitle is title marked a draft, or not: GitLab has no draft field to
// write, only the title's prefix.
func DraftTitle(title string, draft bool) string {
	for draftRe.MatchString(title) {
		title = draftRe.ReplaceAllString(title, "")
	}
	if draft {
		return "Draft: " + title
	}
	return title
}

// Member is someone in a project, who can be assigned or asked to review.
type Member struct {
	ID       int
	Username string
	Name     string
}

// listMax is how many pages of a list Members and Labels read: a project
// with more than a thousand is searched in GitLab.
const listMax = 10

// Multiple is whether the merge request takes several assignees and
// several reviewers: GitLab's free tier takes one of each.
func (c *Client) Multiple(ctx context.Context, project string, iid int) (assignees, reviewers bool, err error) {
	if !c.Enabled() {
		return false, false, forge.ErrNotConfigured
	}
	body := map[string]any{
		"query":     `query($p: ID!, $iid: String!) { project(fullPath: $p) { mergeRequest(iid: $iid) { allowsMultipleAssignees allowsMultipleReviewers } } }`,
		"variables": map[string]any{"p": project, "iid": strconv.Itoa(iid)},
	}
	var resp struct {
		Data struct {
			Project *struct {
				MergeRequest *struct {
					AllowsMultipleAssignees bool `json:"allowsMultipleAssignees"`
					AllowsMultipleReviewers bool `json:"allowsMultipleReviewers"`
				} `json:"mergeRequest"`
			} `json:"project"`
		} `json:"data"`
	}
	if err := c.gql.Do(ctx, http.MethodPost, "/graphql", "graphql", body, &resp); err != nil {
		return false, false, err
	}
	if resp.Data.Project == nil || resp.Data.Project.MergeRequest == nil {
		return false, false, fmt.Errorf("gitlab: %s not found in GraphQL", label(project, iid))
	}
	mr := resp.Data.Project.MergeRequest
	return mr.AllowsMultipleAssignees, mr.AllowsMultipleReviewers, nil
}

// Members are the project's members, inherited ones too, by name.
func (c *Client) Members(ctx context.Context, project string) ([]Member, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	var out []Member
	for page := 1; page <= listMax; page++ {
		var us []apiUser
		path := fmt.Sprintf("/projects/%s/members/all?per_page=100&page=%d", encodePath(project), page)
		if err := c.rest.Do(ctx, http.MethodGet, path, "members", nil, &us); err != nil {
			return nil, err
		}
		for _, u := range us {
			if !slices.ContainsFunc(out, func(m Member) bool { return m.ID == u.ID }) {
				out = append(out, Member{ID: u.ID, Username: u.Username, Name: u.Name})
			}
		}
		if len(us) < 100 {
			break
		}
	}
	slices.SortFunc(out, func(a, b Member) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

// Labels are the names of the labels the project can use, its groups' too.
func (c *Client) Labels(ctx context.Context, project string) ([]string, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	var out []string
	for page := 1; page <= listMax; page++ {
		var ls []struct {
			Name string `json:"name"`
		}
		path := fmt.Sprintf("/projects/%s/labels?per_page=100&page=%d", encodePath(project), page)
		if err := c.rest.Do(ctx, http.MethodGet, path, "labels", nil, &ls); err != nil {
			return nil, err
		}
		for _, l := range ls {
			out = append(out, l.Name)
		}
		if len(ls) < 100 {
			break
		}
	}
	return out, nil
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

// Waiting are the open merge requests that wait on the token's account, by
// why: a review asked of it, assigned to it, or its own. One in several
// shows in the first.
type Waiting struct {
	Review, Assigned, Mine []*forge.Change
}

// Waiting lists the open merge requests waiting on the token's account,
// newest first, across every project.
func (c *Client) Waiting(ctx context.Context) (Waiting, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return Waiting{}, err
	}
	const open = "/merge_requests?state=opened&per_page=50&order_by=updated_at"
	var w Waiting
	seen := map[string]bool{}
	for _, q := range []struct {
		query string
		into  *[]*forge.Change
	}{
		{open + "&scope=all&reviewer_username=" + url.QueryEscape(me.Username), &w.Review},
		{open + "&scope=assigned_to_me", &w.Assigned},
		{open + "&scope=created_by_me", &w.Mine},
	} {
		var found []apiMR
		if err := c.rest.Do(ctx, http.MethodGet, q.query, "merge requests", nil, &found); err != nil {
			return Waiting{}, err
		}
		for _, a := range found {
			r, ok := c.Parse(a.WebURL)
			if !ok || seen[a.WebURL] {
				continue
			}
			seen[a.WebURL] = true
			ch := toChange(a, r.Repo)
			ch.WebURL = a.WebURL
			*q.into = append(*q.into, ch)
		}
	}
	return w, nil
}
