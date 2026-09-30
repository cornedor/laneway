package web

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// Personal views: my work, time tracking, the inbox and the standup.

const defaultMyWorkJQL = "assignee = currentUser() AND (statusCategory != Done OR resolved >= -7d) ORDER BY updated DESC"

// The TUI keeps the same marks; sharing the keys keeps read state in step.
const (
	inboxMarksMeta = "jira_tab:inbox_marks"
	inboxFloorMeta = "jira_tab:inbox_seen"
)

func init() {
	get("/work", myWork)
	get("/worklogs", myWorklogs)
	post("/worklog/{key}", addWorklog)
	put("/worklog/{key}/{id}", updateWorklog)
	del("/worklog/{key}/{id}", deleteWorklog)
	get("/inbox", inbox)
	put("/inbox/state/{key}", putInboxState)
	get("/standup", standup)
	get("/standup/people", standupPeople)
}

func validKey(r *http.Request) (string, error) {
	key := r.PathValue("key")
	if !jira.ValidKey(key) {
		return "", badRequest("bad issue key")
	}
	return key, nil
}

func myWork(ctx context.Context, s *Server, r *http.Request) (any, error) {
	jql := Q(r, "jql")
	if jql == "" {
		jql = strings.TrimSpace(s.UIConfig().MyWorkJQL)
	}
	if jql == "" {
		jql = defaultMyWorkJQL
	}
	cards, err := s.Client().SearchCards(ctx, jql)
	return map[string]any{"cards": cards}, err
}

// day reads a YYYY-MM-DD query parameter as a local midnight.
func day(r *http.Request, name string) (time.Time, error) {
	t, err := time.ParseInLocation(time.DateOnly, Q(r, name), time.Local)
	if err != nil {
		return t, badRequest("bad " + name + ": want YYYY-MM-DD")
	}
	return t, nil
}

// myWorklogs: ?from=DAY&to=DAY, to exclusive.
func myWorklogs(ctx context.Context, s *Server, r *http.Request) (any, error) {
	from, err := day(r, "from")
	if err != nil {
		return nil, err
	}
	to, err := day(r, "to")
	if err != nil {
		return nil, err
	}
	if !to.After(from) || to.Sub(from) > 62*24*time.Hour {
		return nil, badRequest("to must follow from, within two months")
	}
	logs, err := s.Client().MyWorklogsBetween(ctx, from, to)
	if logs == nil {
		logs = []jira.Worklog{}
	}
	return map[string]any{"worklogs": logs}, err
}

type worklogBody struct {
	Seconds int
	Started string // RFC 3339; "" on an update keeps it
	Comment *string
	Left    string // new worklogs: "" takes it off the estimate, "keep", or "2h"
}

func (b worklogBody) start() (time.Time, error) {
	if b.Started == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, b.Started)
	if err != nil {
		return t, badRequest("bad Started: want RFC 3339")
	}
	return t, nil
}

func addWorklog(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	b, err := Body[worklogBody](r)
	if err != nil {
		return nil, err
	}
	started, err := b.start()
	if err != nil {
		return nil, err
	}
	if started.IsZero() {
		started = time.Now().Add(-time.Duration(b.Seconds) * time.Second)
	}
	comment := ""
	if b.Comment != nil {
		comment = *b.Comment
	}
	return nil, s.Client().AddWorklog(ctx, key, b.Seconds, started, comment, b.Left)
}

func updateWorklog(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	b, err := Body[worklogBody](r)
	if err != nil {
		return nil, err
	}
	started, err := b.start()
	if err != nil {
		return nil, err
	}
	return nil, s.Client().UpdateWorklog(ctx, key, r.PathValue("id"), b.Seconds, started, b.Comment)
}

func deleteWorklog(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	return nil, s.Client().DeleteWorklog(ctx, key, r.PathValue("id"))
}

// ---- inbox

// InboxThread is an issue and what others did on it.
type InboxThread struct {
	jira.InboxIssue
	Entries []jira.InboxEntry
}

// inboxMark mirrors the TUI's stored marks, Unix milliseconds.
type inboxMark struct {
	R int64 `json:"r,omitempty"`
	D int64 `json:"d,omitempty"`
	S int64 `json:"s,omitempty"`
}

// InboxMark is a thread's marks as the API speaks them.
type InboxMark struct{ Read, Done, Snooze int64 }

var (
	inboxMu    sync.Mutex
	inboxCache = map[string]InboxThread{} // by site/key, reused while the issue is not updated
)

// inbox: ?days=N (default the TUI's week). Threads without news are left out.
func inbox(ctx context.Context, s *Server, r *http.Request) (any, error) {
	days, _ := strconv.Atoi(Q(r, "days"))
	if days < 1 || days > 90 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	c := s.Client()
	issues, err := c.InboxIssues(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]InboxThread, len(issues))
	errs := make([]error, len(issues))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, is := range issues {
		out[i] = InboxThread{InboxIssue: is}
		id := s.opt.Site + "/" + is.Key
		inboxMu.Lock()
		k, ok := inboxCache[id]
		inboxMu.Unlock()
		if ok && k.Updated.Equal(is.Updated) {
			out[i].Entries = slices.DeleteFunc(slices.Clone(k.Entries), func(e jira.InboxEntry) bool { return !e.When.After(since) })
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i].Entries, errs[i] = c.IssueInbox(ctx, is.Key, is.Summary, since)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	inboxMu.Lock()
	for _, t := range out {
		inboxCache[s.opt.Site+"/"+t.Key] = t
	}
	inboxMu.Unlock()
	out = slices.DeleteFunc(out, func(t InboxThread) bool { return len(t.Entries) == 0 })
	slices.SortStableFunc(out, func(a, b InboxThread) int {
		return b.Entries[len(b.Entries)-1].When.Compare(a.Entries[len(a.Entries)-1].When)
	})
	marks := map[string]InboxMark{}
	for id, m := range readMarks(s) {
		if site, key, ok := strings.Cut(id, "/"); ok && site == s.opt.Site {
			marks[key] = InboxMark{m.R, m.D, m.S}
		}
	}
	return map[string]any{"threads": out, "marks": marks, "floor": inboxFloor(s), "lookback": days}, nil
}

func readMarks(s *Server) map[string]inboxMark {
	marks := map[string]inboxMark{}
	if raw, ok, _ := s.opt.Store.GetMeta(inboxMarksMeta); ok {
		_ = json.Unmarshal([]byte(raw), &marks)
	}
	return marks
}

// inboxFloor is when news starts counting as unread, in Unix milliseconds:
// the first time the inbox was read, a day before.
func inboxFloor(s *Server) int64 {
	if v, ok, _ := s.opt.Store.GetMeta(inboxFloorMeta); ok {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			return sec * 1000
		}
	}
	floor := time.Now().Add(-24 * time.Hour)
	_ = s.opt.Store.SetMeta(inboxFloorMeta, strconv.FormatInt(floor.Unix(), 10))
	return floor.UnixMilli()
}

func putInboxState(ctx context.Context, s *Server, r *http.Request) (any, error) {
	key, err := validKey(r)
	if err != nil {
		return nil, err
	}
	b, err := Body[InboxMark](r)
	if err != nil {
		return nil, err
	}
	inboxMu.Lock()
	defer inboxMu.Unlock()
	marks := readMarks(s)
	id := s.opt.Site + "/" + key
	marks[id] = inboxMark{b.Read, b.Done, b.Snooze}
	old := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	maps.DeleteFunc(marks, func(_ string, m inboxMark) bool { return max(m.R, m.D, m.S) < old })
	raw, _ := json.Marshal(marks)
	return nil, s.opt.Store.SetMeta(inboxMarksMeta, string(raw))
}

// ---- standup

const standupNext = "(assignee = currentUser() AND statusCategory != Done AND sprint in openSprints())"

// standup: ?since=DAY; ?ids=a,b for the team's. The client sorts the
// activity and cards into sections.
func standup(ctx context.Context, s *Server, r *http.Request) (any, error) {
	since, err := day(r, "since")
	if err != nil {
		return nil, err
	}
	c := s.Client()
	var entries []jira.InboxEntry
	if ids := Q(r, "ids"); ids != "" {
		entries, err = c.TeamStandup(ctx, since, strings.Split(ids, ","))
	} else {
		entries, err = c.Standup(ctx, since)
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range entries {
		if e.Key != "" && !slices.Contains(keys, e.Key) {
			keys = append(keys, e.Key)
		}
	}
	jql := standupNext + " ORDER BY Rank"
	if len(keys) > 0 {
		jql = "key in (" + strings.Join(keys, ",") + ") OR " + standupNext + " ORDER BY Rank"
	}
	cards, err := c.SearchCards(ctx, jql)
	if err != nil && len(keys) > 0 { // a deleted key fails the whole search
		cards, _ = c.SearchCards(ctx, standupNext+" ORDER BY Rank")
	}
	if entries == nil {
		entries = []jira.InboxEntry{}
	}
	if cards == nil {
		cards = []jira.Card{}
	}
	return map[string]any{"entries": entries, "cards": cards, "previous": jira.PreviousWorkday(time.Now(), workdays(s))}, nil
}

// standupPeople are who holds the project's issues in the open sprints.
func standupPeople(ctx context.Context, s *Server, r *http.Request) (any, error) {
	project := Q(r, "project")
	jql := "sprint in openSprints()"
	if project != "" {
		if !jira.ValidKey(project + "-1") {
			return nil, badRequest("bad project")
		}
		jql = "project = " + project + " AND " + jql
	}
	cards, err := s.Client().SearchCards(ctx, jql)
	if err != nil {
		return nil, err
	}
	type person struct{ ID, Name string }
	seen := map[string]bool{}
	out := []person{}
	for _, cd := range cards {
		if cd.AssigneeID != "" && !seen[cd.AssigneeID] {
			seen[cd.AssigneeID] = true
			out = append(out, person{cd.AssigneeID, cd.Assignee})
		}
	}
	return out, nil
}

// workdays are ui.workdays as weekdays, nil for the default.
func workdays(s *Server) []time.Weekday {
	var out []time.Weekday
	for _, d := range s.UIConfig().Workdays {
		for wd := time.Sunday; wd <= time.Saturday; wd++ {
			if len(d) >= 3 && strings.HasPrefix(strings.ToLower(wd.String()), strings.ToLower(d[:3])) {
				out = append(out, wd)
			}
		}
	}
	return out
}
