package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/home"
	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
)

// ~ is the start screen (ui.home, internal/home, as laneway web's): each
// widget a heading and its rows. Enter on an issue opens it, on a heading
// its own screen, on a saved search the search as a view; esc is the board.
// With ui.home set it opens once the first board is in.

// A home row's id: an issue, a saved search's JQL, or the screen a heading
// opens; none for a row that opens nothing.
const (
	homeIssue = "issue:"
	homeURL   = "url:" // another site's issue, opened in the browser
	homeJQL   = "jql:"
	homeGo    = "go:"
)

// homeShown caps a list widget's rows; its heading says how many there are.
const homeShown = 8

// openHome loads the widgets into a picker.
func (m *Model) openHome() tea.Cmd {
	widgets := m.opts.home
	if len(widgets) == 0 {
		widgets = home.Widgets
	}
	gen := m.startJiraPicker(jiraPickHome, i18n.T("Home  ·  enter opens · esc the board"), false)
	seq := m.jiraPicker.fetchSeq
	c, ctx, o := m.jiraClient, m.ctx, m.opts
	board := 0
	if t := m.jiraTab; t != nil && t.board < len(t.boards) {
		board = t.boards[t.board].ID
	}
	// Read on the model now: the inbox the app keeps, the timer, the starred searches.
	now := time.Now()
	var inbox []inboxThread
	for _, t := range m.inboxData().threads {
		if st := m.inboxStateOf(t, now); len(t.entries) > 0 && st.unread && !st.done && !st.snoozed {
			inbox = append(inbox, t)
		}
	}
	slices.SortFunc(inbox, func(a, b inboxThread) int { return b.latest().Compare(a.latest()) })
	timer, starred, timerKey, site := m.timerLabel(), m.jqlList(jqlSavedMeta), m.keys.Timer, m.site
	if i := slices.IndexFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == m.timer.key }); timer != "" && i >= 0 {
		timer += "  " + m.jiraTab.cards[i].Summary // the timed card's, when the board has it
	}
	return func() tea.Msg {
		parts := map[string][]jiraPickerItem{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, w := range widgets {
			wg.Go(func() {
				var items []jiraPickerItem
				switch w {
				case "work":
					cards, err := c.SearchCards(ctx, o.myWorkJQL)
					cards = slices.DeleteFunc(cards, func(c jira.Card) bool { return c.Done })
					items = homeList(i18n.T("My work"), homeGo+"work", i18n.Tf("%d open", len(cards)), homeCards(cards), err, i18n.T("nothing open is yours"))
				case "inbox":
					rows := make([]jiraPickerItem, len(inbox))
					for i, t := range inbox {
						rows[i] = jiraPickerItem{id: homeIssue + t.Key, label: "  " + t.Key + "  " + t.Summary}
						if t.site != site {
							rows[i].id, rows[i].value = homeURL+t.url, t.Key
						}
					}
					items = homeList(i18n.T("Inbox"), homeGo+"inbox", i18n.Tf("%d unread", len(inbox)), rows, nil, i18n.T("all caught up"))
				case "sprint":
					items = homeSprint(ctx, c, board, now)
				case "timer":
					row := jiraPickerItem{label: "  " + i18n.Tf("no timer running · %s starts one on the selected card", helpKey(timerKey))}
					if timer != "" {
						key, _, _ := strings.Cut(strings.TrimPrefix(timer, "⏱ "), " ")
						row = jiraPickerItem{id: homeIssue + key, label: "  " + timer}
					}
					items = []jiraPickerItem{{label: i18n.T("Timer")}, row}
				case "filters":
					fs, err := home.Filters(ctx, c, o.savedFilters, starred)
					rows := make([]jiraPickerItem, len(fs))
					for i, f := range fs {
						n := fmt.Sprint(f.Count)
						if f.Err != "" {
							n = "! " + f.Err
						}
						rows[i] = jiraPickerItem{id: homeJQL + f.JQL, value: f.Name, label: "  " + f.Name + "  ·  " + n}
					}
					items = homeList(i18n.T("Saved searches"), "", "", rows, err, i18n.T("none · star a Jira filter, or a search with ctrl+s in Q"))
				}
				mu.Lock()
				parts[w] = items
				mu.Unlock()
			})
		}
		wg.Wait()
		var items []jiraPickerItem
		for _, w := range widgets {
			items = append(items, parts[w]...)
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickHome, items: items}
	}
}

// homeList is a widget's heading ("My work · 4 open") and its first rows,
// or the error, or empty when there are none.
func homeList(title, id, count string, rows []jiraPickerItem, err error, empty string) []jiraPickerItem {
	head := title
	if count != "" && err == nil && len(rows) > 0 {
		head += "  ·  " + count
	}
	out := []jiraPickerItem{{id: id, label: head}}
	switch {
	case err != nil:
		return append(out, jiraPickerItem{label: "  ! " + err.Error()})
	case len(rows) == 0:
		return append(out, jiraPickerItem{label: "  " + empty})
	}
	return append(out, rows[:min(len(rows), homeShown)]...)
}

// homeCards are cards as home rows: key, summary, status.
func homeCards(cards []jira.Card) []jiraPickerItem {
	out := make([]jiraPickerItem, len(cards))
	for i, c := range cards {
		out[i] = jiraPickerItem{id: homeIssue + c.Key, label: "  " + c.Key + "  " + c.Summary + "  ·  " + c.Status}
	}
	return out
}

// homeSprint is the board's active sprint: done against the time gone.
func homeSprint(ctx context.Context, c *jira.Client, board int, now time.Time) []jiraPickerItem {
	head := jiraPickerItem{id: homeGo + "board", label: i18n.T("Sprint")}
	if board == 0 {
		return []jiraPickerItem{head, {label: "  " + i18n.T("open a scrum board first")}}
	}
	fail := func(err error) []jiraPickerItem { return []jiraPickerItem{head, {label: "  ! " + err.Error()}} }
	sprints, err := c.Sprints(ctx, board)
	if err != nil {
		return fail(err)
	}
	i := slices.IndexFunc(sprints, func(s jira.Sprint) bool { return s.State == "active" })
	if i < 0 {
		return []jiraPickerItem{head, {label: "  " + i18n.T("no active sprint on this board")}}
	}
	cfg, err := c.BoardConfiguration(ctx, board)
	if err != nil {
		return fail(err)
	}
	issues, err := c.SprintBurn(ctx, sprints[i].ID, cfg.PointsField)
	if err != nil {
		return fail(err)
	}
	s := home.Health(sprints[i], issues, now)
	days := i18n.Tn(s.DaysLeft, "%d day left", "%d days left", s.DaysLeft)
	switch {
	case s.DaysLeft == 0:
		days = i18n.T("last day")
	case s.DaysLeft < 0:
		days = i18n.Tn(-s.DaysLeft, "%d day over", "%d days over", -s.DaysLeft)
	}
	head.label = i18n.Tf("Sprint  ·  %s · %s", s.Name, days)
	done := i18n.Tf("%d of %d issues", s.Done, s.Issues)
	if s.Points > 0 {
		pts := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
		done = i18n.Tf("%s of %s points", pts(s.DonePoints), pts(s.Points))
	}
	line := i18n.Tf("  %s %s done  ·  %d%% of the time gone", homeBar(s.Progress(), s.Elapsed), done, int(s.Elapsed*100+0.5))
	if s.Behind() {
		line += i18n.T("  ·  behind")
	}
	out := []jiraPickerItem{head, {id: head.id, label: line}}
	if g := strings.TrimSpace(s.Goal); g != "" {
		out = append(out, jiraPickerItem{id: head.id, label: "  " + strings.ReplaceAll(g, "\n", " ")})
	}
	return out
}

// homeBar is done as sixteen cells, a │ where the time has got to.
func homeBar(done, elapsed float64) string {
	const cells = 16
	n, at := int(done*cells+0.5), min(int(elapsed*cells), cells-1)
	var b strings.Builder
	for i := range cells {
		switch {
		case i == at && elapsed > 0:
			b.WriteString("│")
		case i < n:
			b.WriteString("█")
		default:
			b.WriteString("░")
		}
	}
	return b.String()
}

// applyHomePick opens what the row is.
func (m Model) applyHomePick(it jiraPickerItem) (tea.Model, tea.Cmd) {
	switch {
	case strings.HasPrefix(it.id, homeIssue):
		m.closeJiraPicker()
		return m.openJiraKey(strings.TrimPrefix(it.id, homeIssue))
	case strings.HasPrefix(it.id, homeURL):
		m.closeJiraPicker()
		url := strings.TrimPrefix(it.id, homeURL)
		m.status = i18n.Tf("opening %s…", url)
		return m, m.openOpenable(openable{name: it.value, url: url})
	case strings.HasPrefix(it.id, homeJQL):
		m.closeJiraPicker()
		return m, m.runNamedJQLView(cmp.Or(it.value, i18n.T("Search")), strings.TrimPrefix(it.id, homeJQL))
	case it.id == "":
		m.status = i18n.T("nothing to open on this row") // the screen stays
		return m, nil
	}
	m.closeJiraPicker()
	switch strings.TrimPrefix(it.id, homeGo) {
	case "work":
		return m, m.openMyWork()
	case "inbox":
		return m, m.openInbox()
	}
	return m, nil // the board
}

// startHome opens ui.home's screen once, when the first board is in.
func (m *Model) startHome() tea.Cmd {
	if m.homeShown || len(m.opts.home) == 0 {
		return nil
	}
	m.homeShown = true // the first board only: later ones must not open it over what you do
	if m.jiraPicker.active || m.modalOpen() {
		return nil
	}
	return m.openHome()
}
