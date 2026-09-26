package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Boards, sprints and board issues, for the Jira tab (internal/ui/jiratab.go).
// These come from the Agile API (/rest/agile/1.0), which returns issues in
// rank order.

// DefaultCardLimit caps one board fetch; a backlog can run to thousands.
const DefaultCardLimit = 500

// cardFields is what a card shows. The points field is appended per board.
const cardFields = "summary,status,assignee,issuetype,priority,parent,subtasks,duedate,statuscategorychangedate"

// boardMetaCache keeps what a board is made of — a project's boards, a
// board's columns and quick filters — for the session: they change about as
// often as a team reorganises, and fetching them made every board open wait.
type boardMetaCache struct {
	mu     sync.Mutex
	boards map[string][]Board
	cfg    map[int]*BoardConfig
	quick  map[int][]QuickFilter
	status map[string]map[string]string // "" → status id → name
	saved  map[string][]QuickFilter     // "" → your favourite filters
	jql    map[string][]JQLWords        // "" → the JQL autocomplete data
}

// StatusNames maps every status id to its name, cached for the session. A
// board's columns name their statuses by id only.
func (c *Client) StatusNames(ctx context.Context) (map[string]string, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	bm := &c.boardMeta
	return cached(&bm.mu, &bm.status, "", func() (map[string]string, error) {
		var resp []named
		if err := c.do(ctx, http.MethodGet, "/rest/api/3/status", "statuses", nil, &resp); err != nil {
			return nil, err
		}
		out := make(map[string]string, len(resp))
		for _, s := range resp {
			out[s.ID] = s.Name
		}
		return out, nil
	})
}

// cached returns m[k] under the cache lock, or fetches and stores it.
func cached[K comparable, V any](mu *sync.Mutex, m *map[K]V, k K, fetch func() (V, error)) (V, error) {
	mu.Lock()
	v, ok := (*m)[k]
	mu.Unlock()
	if ok {
		return v, nil
	}
	v, err := fetch()
	if err != nil {
		return v, err
	}
	mu.Lock()
	if *m == nil {
		*m = map[K]V{}
	}
	(*m)[k] = v
	mu.Unlock()
	return v, nil
}

// Board is a Jira Software board: Type is "scrum" or "kanban".
type Board struct {
	ID   int
	Name string
	Type string
}

// Column is a board column and the statuses mapped to it.
type Column struct {
	Name      string
	StatusIDs []string
	// Max is the column's work-in-progress limit, 0 when it has none.
	Max int
}

// BoardConfig is a board's columns, left to right, and its estimation field
// ("" when the board doesn't estimate with a field).
type BoardConfig struct {
	Columns     []Column
	PointsField string
}

// Sprint is an active or future sprint.
type Sprint struct {
	ID         int
	Name       string
	State      string
	Start, End time.Time // zero until planned
	Goal       string
}

// Card is an issue as a board shows it.
type Card struct {
	Key      string
	Summary  string
	Type     string
	TypeID   string
	Status   string
	StatusID string
	Priority string
	Assignee string
	// AssigneeID is the assignee's accountId, "" when unassigned.
	AssigneeID string
	Points     string
	// Parent is the parent issue (an epic, or a subtask's story), "" for none.
	ParentKey, ParentSummary string
	// PR is the state of its pull requests, from the Development field:
	// OPEN, MERGED or DECLINED; "" for none (or no such field).
	PR string
	// Subtasks and SubtasksDone count its subtasks, all and done.
	Subtasks, SubtasksDone int
	// Due is its due date, zero for none; Done whether its status is in the
	// done category.
	Due  time.Time
	Done bool
	// Flagged marks an impediment (the Flagged field is set).
	Flagged bool
	// InProgress is whether its status is in the in-progress category, and
	// Since when its status category last changed (zero when unknown).
	InProgress bool
	Since      time.Time
}

// QuickFilter is a board's saved filter: a name and the JQL behind it.
type QuickFilter struct {
	ID   int
	Name string
	JQL  string
}

// QuickFilters lists a board's quick filters, cached for the session.
func (c *Client) QuickFilters(ctx context.Context, board int) ([]QuickFilter, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	bm := &c.boardMeta
	return cached(&bm.mu, &bm.quick, board, func() ([]QuickFilter, error) { return c.fetchQuickFilters(ctx, board) })
}

func (c *Client) fetchQuickFilters(ctx context.Context, board int) ([]QuickFilter, error) {
	var resp struct {
		Values []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			JQL  string `json:"jql"`
		} `json:"values"`
	}
	path := "/rest/agile/1.0/board/" + strconv.Itoa(board) + "/quickfilter?maxResults=50"
	if err := c.do(ctx, http.MethodGet, path, "quick filters", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]QuickFilter, 0, len(resp.Values))
	for _, q := range resp.Values {
		out = append(out, QuickFilter{ID: q.ID, Name: q.Name, JQL: q.JQL})
	}
	return out, nil
}

// FavouriteFilters lists the saved filters you starred, cached for the
// session.
func (c *Client) FavouriteFilters(ctx context.Context) ([]QuickFilter, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	bm := &c.boardMeta
	return cached(&bm.mu, &bm.saved, "", func() ([]QuickFilter, error) {
		var resp []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			JQL  string `json:"jql"`
		}
		if err := c.do(ctx, http.MethodGet, "/rest/api/3/filter/favourite", "saved filters", nil, &resp); err != nil {
			return nil, err
		}
		out := make([]QuickFilter, 0, len(resp))
		for _, f := range resp {
			id, _ := strconv.Atoi(f.ID)
			out = append(out, QuickFilter{ID: id, Name: f.Name, JQL: f.JQL})
		}
		return out, nil
	})
}

// ListProjects returns every project the user can browse, by name.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var out []Project
	for start := 0; start < 1000; {
		var resp struct {
			Values []struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"values"`
			IsLast bool `json:"isLast"`
		}
		path := "/rest/api/3/project/search?orderBy=name&maxResults=100&startAt=" + strconv.Itoa(start)
		if err := c.do(ctx, http.MethodGet, path, "projects", nil, &resp); err != nil {
			return nil, err
		}
		for _, p := range resp.Values {
			out = append(out, Project{Key: p.Key, Name: p.Name})
		}
		if resp.IsLast || len(resp.Values) == 0 {
			break
		}
		start += len(resp.Values)
	}
	return out, nil
}

// Boards lists the boards of a project, cached for the session.
func (c *Client) Boards(ctx context.Context, project string) ([]Board, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	bm := &c.boardMeta
	return cached(&bm.mu, &bm.boards, project, func() ([]Board, error) { return c.fetchBoards(ctx, project) })
}

func (c *Client) fetchBoards(ctx context.Context, project string) ([]Board, error) {
	var resp struct {
		Values []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"values"`
	}
	path := "/rest/agile/1.0/board?maxResults=50&projectKeyOrId=" + url.QueryEscape(project)
	if err := c.do(ctx, http.MethodGet, path, "boards of "+project, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Board, 0, len(resp.Values))
	for _, b := range resp.Values {
		out = append(out, Board{ID: b.ID, Name: b.Name, Type: b.Type})
	}
	return out, nil
}

// BoardConfiguration returns a board's columns and estimation field, cached
// for the session.
func (c *Client) BoardConfiguration(ctx context.Context, board int) (*BoardConfig, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	bm := &c.boardMeta
	return cached(&bm.mu, &bm.cfg, board, func() (*BoardConfig, error) { return c.fetchBoardConfiguration(ctx, board) })
}

func (c *Client) fetchBoardConfiguration(ctx context.Context, board int) (*BoardConfig, error) {
	var resp struct {
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Max      int    `json:"max"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
		Estimation struct {
			Field struct {
				FieldID string `json:"fieldId"`
			} `json:"field"`
		} `json:"estimation"`
	}
	path := "/rest/agile/1.0/board/" + strconv.Itoa(board) + "/configuration"
	if err := c.do(ctx, http.MethodGet, path, "board configuration", nil, &resp); err != nil {
		return nil, err
	}
	cfg := &BoardConfig{PointsField: resp.Estimation.Field.FieldID}
	for _, col := range resp.ColumnConfig.Columns {
		column := Column{Name: col.Name, Max: col.Max}
		for _, s := range col.Statuses {
			column.StatusIDs = append(column.StatusIDs, s.ID)
		}
		cfg.Columns = append(cfg.Columns, column)
	}
	return cfg, nil
}

// Sprints lists a scrum board's active and future sprints, active first.
func (c *Client) Sprints(ctx context.Context, board int) ([]Sprint, error) {
	if !c.Enabled() {
		return nil, errNotConfigured
	}
	var resp struct {
		Values []struct {
			ID        int       `json:"id"`
			Name      string    `json:"name"`
			State     string    `json:"state"`
			StartDate time.Time `json:"startDate"`
			EndDate   time.Time `json:"endDate"`
			Goal      string    `json:"goal"`
		} `json:"values"`
	}
	path := "/rest/agile/1.0/board/" + strconv.Itoa(board) + "/sprint?state=active,future&maxResults=50"
	if err := c.do(ctx, http.MethodGet, path, "sprints", nil, &resp); err != nil {
		return nil, err
	}
	var active, future []Sprint
	for _, s := range resp.Values {
		sp := Sprint{ID: s.ID, Name: s.Name, State: s.State, Start: s.StartDate, End: s.EndDate, Goal: strings.TrimSpace(s.Goal)}
		if s.State == "active" {
			active = append(active, sp)
		} else {
			future = append(future, sp)
		}
	}
	return append(active, future...), nil
}

// SprintIssues returns a sprint's issues in rank order, narrowed by jql when
// non-empty.
func (c *Client) SprintIssues(ctx context.Context, board, sprint int, jql, pointsField string) ([]Card, int, error) {
	return c.cards(ctx, "/rest/agile/1.0/board/"+strconv.Itoa(board)+"/sprint/"+strconv.Itoa(sprint)+"/issue", jql, pointsField)
}

// BacklogIssues returns a board's backlog in rank order, narrowed by jql when
// non-empty.
func (c *Client) BacklogIssues(ctx context.Context, board int, jql, pointsField string) ([]Card, int, error) {
	return c.cards(ctx, "/rest/agile/1.0/board/"+strconv.Itoa(board)+"/backlog", jql, pointsField)
}

// BoardIssues returns a board's issues, narrowed by jql when non-empty.
func (c *Client) BoardIssues(ctx context.Context, board int, jql, pointsField string) ([]Card, int, error) {
	return c.cards(ctx, "/rest/agile/1.0/board/"+strconv.Itoa(board)+"/issue", jql, pointsField)
}

// cardPage is how many issues one request asks for; the Agile API caps it.
const cardPage = 100

// cards pages through an Agile issue list up to the card limit, returning the cards
// and the server's total. The first page says how many there are; the rest
// are fetched at once.
func (c *Client) cards(ctx context.Context, path, jql, pointsField string) ([]Card, int, error) {
	if !c.Enabled() {
		return nil, 0, errNotConfigured
	}
	fields := cardFields
	if pointsField != "" {
		fields += "," + pointsField
	}
	dev, flag := c.devField(ctx), c.flagField(ctx)
	for _, id := range []string{dev, flag} {
		if id != "" {
			fields += "," + id
		}
	}
	page := func(start int) ([]Card, int, error) {
		q := url.Values{}
		q.Set("fields", fields)
		q.Set("maxResults", strconv.Itoa(cardPage))
		q.Set("startAt", strconv.Itoa(start))
		if jql != "" {
			q.Set("jql", jql)
		}
		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Key    string                     `json:"key"`
				Fields map[string]json.RawMessage `json:"fields"`
			} `json:"issues"`
		}
		if err := c.do(ctx, http.MethodGet, path+"?"+q.Encode(), "board issues", nil, &resp); err != nil {
			return nil, 0, err
		}
		out := make([]Card, 0, len(resp.Issues))
		for _, is := range resp.Issues {
			card := toCard(is.Key, is.Fields, pointsField)
			card.PR = prState(is.Fields[dev])
			card.Flagged = flagSet(is.Fields[flag])
			out = append(out, card)
		}
		return out, resp.Total, nil
	}
	out, total, err := page(0)
	if err != nil {
		return nil, 0, err
	}
	// The server may cap a page below cardPage; step by what it gave.
	step := len(out)
	if step == 0 || len(out) >= total {
		return out, total, nil
	}
	var starts []int
	for s := step; s < min(total, c.cardLimit); s += step {
		starts = append(starts, s)
	}
	pages := make([][]Card, len(starts))
	errs := make([]error, len(starts))
	var wg sync.WaitGroup
	for i, s := range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pages[i], _, errs[i] = page(s)
		}()
	}
	wg.Wait()
	for i := range pages {
		if errs[i] != nil {
			return nil, 0, errs[i]
		}
		out = append(out, pages[i]...)
	}
	if len(out) > c.cardLimit {
		out = out[:c.cardLimit]
	}
	return out, total, nil
}

func toCard(key string, f map[string]json.RawMessage, pointsField string) Card {
	card := Card{Key: key}
	str := func(name string) string {
		var s string
		_ = json.Unmarshal(f[name], &s)
		return s
	}
	obj := func(name string) (id, label string) {
		var v struct {
			ID          string `json:"id"`
			AccountID   string `json:"accountId"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		}
		_ = json.Unmarshal(f[name], &v)
		if v.DisplayName != "" {
			return v.AccountID, v.DisplayName
		}
		return v.ID, v.Name
	}
	card.Summary = str("summary")
	card.StatusID, card.Status = obj("status")
	card.Due = dateField(f["duedate"])
	var st struct {
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	_ = json.Unmarshal(f["status"], &st)
	card.Done = st.Category.Key == "done"
	card.InProgress = st.Category.Key == "indeterminate"
	var since string
	if json.Unmarshal(f["statuscategorychangedate"], &since) == nil {
		card.Since, _ = time.Parse(jiraTime, since)
	}
	card.TypeID, card.Type = obj("issuetype")
	_, card.Priority = obj("priority")
	card.AssigneeID, card.Assignee = obj("assignee")
	var subs []struct {
		Fields struct {
			Status struct {
				Category struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
		} `json:"fields"`
	}
	if json.Unmarshal(f["subtasks"], &subs) == nil {
		for _, s := range subs {
			card.Subtasks++
			if s.Fields.Status.Category.Key == "done" {
				card.SubtasksDone++
			}
		}
	}
	var parent apiLinked
	if json.Unmarshal(f["parent"], &parent) == nil {
		card.ParentKey, card.ParentSummary = parent.Key, parent.Fields.Summary
	}
	if raw, ok := f[pointsField]; ok && pointsField != "" {
		var v float64
		if json.Unmarshal(raw, &v) == nil && strings.TrimSpace(string(raw)) != "null" {
			card.Points = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return card
}

// MoveToBacklog takes issues out of their sprint.
func (c *Client) MoveToBacklog(ctx context.Context, keys ...string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	return c.moveChunks(ctx, "/rest/agile/1.0/backlog/issue", "backlog", keys)
}

// agileMoveMax is how many issues one Agile move request takes.
const agileMoveMax = 50

// moveChunks posts keys to an Agile move endpoint, 50 at a time.
func (c *Client) moveChunks(ctx context.Context, path, what string, keys []string) error {
	for chunk := range slices.Chunk(keys, agileMoveMax) {
		if err := c.do(ctx, http.MethodPost, path, what, map[string]any{"issues": chunk}, nil); err != nil {
			return err
		}
	}
	return nil
}

// MoveToSprint puts issues in a sprint.
func (c *Client) MoveToSprint(ctx context.Context, sprint int, keys ...string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	return c.moveChunks(ctx, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint)+"/issue", "sprint", keys)
}

// Rank puts key just before (after false) or after other in rank order.
func (c *Client) Rank(ctx context.Context, key, other string, after bool) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{"issues": []string{key}}
	if after {
		body["rankAfterIssue"] = other
	} else {
		body["rankBeforeIssue"] = other
	}
	return c.do(ctx, http.MethodPut, "/rest/agile/1.0/issue/rank", key, body, nil)
}

// StartSprint makes a future sprint active from start to end.
func (c *Client) StartSprint(ctx context.Context, sprint int, start, end time.Time) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{"state": "active", "startDate": start.Format(time.RFC3339), "endDate": end.Format(time.RFC3339)}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint), "sprint", body, nil)
}

// CloseSprint completes an active sprint. Move its unfinished issues first:
// what Jira does with them on its own differs between instances.
func (c *Client) CloseSprint(ctx context.Context, sprint int) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint), "sprint", map[string]any{"state": "closed"}, nil)
}

// devField is the Development field's id, "" when the instance has none or
// the field list can't be read.
func (c *Client) devField(ctx context.Context) string {
	ids, err := c.resolveRoadmapFields(ctx)
	if err != nil {
		return ""
	}
	return ids.dev
}

// prState reads the Development field's summary for its pull requests'
// state. The field is a string like
// "{pullrequest={dataType=pullrequest, state=OPEN, stateCount=1}, …}".
func prState(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	_, pr, ok := strings.Cut(s, "pullrequest={")
	if !ok {
		return ""
	}
	pr, _, _ = strings.Cut(pr, "}")
	for _, kv := range strings.Split(pr, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(kv), "state="); ok {
			return v
		}
	}
	return ""
}

// CreateSprint adds a future sprint named name to board.
func (c *Client) CreateSprint(ctx context.Context, board int, name string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{"name": name, "originBoardId": board}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint", "sprint", body, nil)
}

// SetSprintGoal replaces a sprint's goal; "" clears it.
func (c *Client) SetSprintGoal(ctx context.Context, sprint int, goal string) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint), "sprint", map[string]any{"goal": goal}, nil)
}

// flagField is the Flagged field's id, "" when none.
func (c *Client) flagField(ctx context.Context) string {
	ids, err := c.resolveRoadmapFields(ctx)
	if err != nil {
		return ""
	}
	return ids.flagged
}

// flagSet reads the Flagged field: a non-empty list of options.
func flagSet(raw json.RawMessage) bool {
	var opts []json.RawMessage
	return json.Unmarshal(raw, &opts) == nil && len(opts) > 0
}

// SetFlagged flags key as an impediment, or clears the flag.
func (c *Client) SetFlagged(ctx context.Context, key string, on bool) error {
	id := c.flagField(ctx)
	if id == "" {
		return fmt.Errorf("jira: no Flagged field on this instance")
	}
	var v any
	if on {
		v = []map[string]string{{"value": "Impediment"}}
	}
	return c.SetField(ctx, key, id, v)
}

// UpdateSprint changes a sprint's name and end date; "" and zero leave them.
func (c *Client) UpdateSprint(ctx context.Context, sprint int, name string, end time.Time) error {
	if !c.Enabled() {
		return errNotConfigured
	}
	body := map[string]any{}
	if name != "" {
		body["name"] = name
	}
	if !end.IsZero() {
		body["endDate"] = end.Format(time.RFC3339)
	}
	return c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint), "sprint", body, nil)
}
