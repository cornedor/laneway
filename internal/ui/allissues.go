package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/mirror"
)

// The all issues view: every issue of the board's project, open and closed,
// from a mirror of the project in the index (internal/mirror). Search, the
// filter builder and sort work locally on all of them; the assignee and
// quick filters ask Jira which keys they find, so they mean what they mean
// on the board. The first time a project is opened here a run reads it
// whole in the background, the issues showing as they arrive.

// AllIssues is the all issues view's cards: project's mirror brought up to
// date (once complete) and narrowed by filter, the JQL of the assignee and
// quick filters. note says what an offline answer left out.
func AllIssues(ctx context.Context, c *jira.Client, ix *index.Index, project, filter string) (cards []jira.Card, note string, err error) {
	if err := mirror.Refresh(ctx, c, ix, project); err != nil {
		if !jira.Offline(err) {
			return nil, "", err
		}
		note = i18n.Tf("offline: %s", err.Error())
	}
	me := ""
	if filter != "" {
		if u, err := c.Myself(ctx); err == nil {
			me = u.AccountID
		}
	}
	cards, fnote, err := mirror.Cards(ctx, c, ix, project, filter, me)
	if fnote != "" {
		note = fnote
	}
	return cards, note, err
}

// allIssuesCached is the all issues view as the index holds it, to show
// while the fresh copy loads; not when filtered, as only Jira can say which
// issues the filters find.
func allIssuesCached(ix *index.Index, project, filter string) ([]jira.Card, bool) {
	if ix == nil || filter != "" {
		return nil, false
	}
	cards, err := ix.ProjectCards(project)
	return cards, err == nil && len(cards) > 0
}

// allIssuesView is the all issues view of a board.
func allIssuesView() jiraView {
	return jiraView{kind: jiraViewAll, name: i18n.T("All issues")} // a list only: thousands of issues make no lanes
}

// inAllIssues is whether the all issues view shows.
func (m *Model) inAllIssues() bool {
	v, ok := m.jiraCurrentView()
	return ok && v.kind == jiraViewAll
}

// allIssuesSort is the all issues view's first order: most recently updated
// first, as rank means nothing across a project.
var allIssuesSort = viewSort{by: jiraSortUpdated}

// seedAllIssuesSort gives the all issues view its first order, once.
func (m *Model) seedAllIssuesSort() {
	t := m.jiraTab
	i := slices.IndexFunc(t.views, func(v jiraView) bool { return v.kind == jiraViewAll })
	if i < 0 {
		return
	}
	if _, ok := t.sorts[t.views[i].name]; !ok {
		m.setViewSort(i, allIssuesSort)
	}
}

// mirrorProgress is a mirror run under way, as its last page left it.
type mirrorProgress struct {
	project     string
	done, total int
	// shown is when the view last showed what the run has read.
	shown time.Time
}

// mirrorMsg is a mirror run's page read (or its start).
type mirrorMsg struct {
	run         *mirror.Run
	done, total int
	finished    bool
	err         error
}

// mirrorShowEvery is how often a run under way shows the issues read since.
const mirrorShowEvery = 2 * time.Second

// startMirror starts reading the shown project whole, when the all issues
// view shows and its mirror was never completed.
func (m *Model) startMirror() tea.Cmd {
	t := m.jiraTab
	if !m.inAllIssues() || m.index == nil || t.mirror != nil || t.project == "" || m.index.ProjectSync(t.project).Complete {
		return nil
	}
	t.mirror = &mirrorProgress{project: t.project, shown: time.Now()}
	ctx, c, ix, project := m.ctx, m.jiraClient, m.index, t.project
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(60*time.Second))
		defer cancel()
		run, err := mirror.Start(ctx, c, ix, project)
		if err != nil {
			return mirrorMsg{err: err}
		}
		return mirrorMsg{run: run, total: run.Total}
	}
}

// nextMirrorPage reads run's next page.
func (m *Model) nextMirrorPage(run *mirror.Run) tea.Cmd {
	ctx, c, ix := m.ctx, m.jiraClient, m.index
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(60*time.Second))
		defer cancel()
		err := run.Next(ctx, c, ix)
		return mirrorMsg{run: run, done: run.Done, total: run.Total, finished: run.Finished(), err: err}
	}
}

func (m Model) handleMirror(msg mirrorMsg) (tea.Model, tea.Cmd) {
	t := m.jiraTab
	if t.mirror == nil {
		return m, nil
	}
	if msg.err != nil {
		t.mirror = nil
		m.fail(i18n.Tf("all issues: %s", msg.err.Error()))
		m.renderJira()
		return m, nil
	}
	t.mirror.done, t.mirror.total = msg.done, msg.total
	project := t.mirror.project
	show := m.inAllIssues() && t.project == project
	if msg.finished {
		t.mirror = nil
		if show {
			return m, m.loadJiraCards(t.viewIdx, false)
		}
		return m, nil
	}
	var reload tea.Cmd
	if show && time.Since(t.mirror.shown) >= mirrorShowEvery {
		t.mirror.shown = time.Now()
		reload = m.loadJiraCards(t.viewIdx, false)
	}
	m.renderJira()
	return m, tea.Batch(reload, m.nextMirrorPage(msg.run))
}

// allIssuesCounts is the all issues view's counts for the title row: the
// issues, the open ones, and those the search leaves when it narrows them;
// while a run reads the project, how far it got.
func (m *Model) allIssuesCounts() string {
	t := m.jiraTab
	if t.mirror != nil && t.mirror.project == t.project {
		return i18n.Tf("reading all issues: %d of %d", t.mirror.done, t.mirror.total)
	}
	open := 0
	for _, c := range t.cards {
		if !c.Done {
			open++
		}
	}
	s := i18n.Tf("%d issues · %d open", len(t.cards), open)
	if t.jiraSearchQuery() != "" {
		s += i18n.Tf(" · %d shown", len(t.order))
	}
	return s
}
