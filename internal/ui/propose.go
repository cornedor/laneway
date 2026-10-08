package ui

import (
	"cmp"
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/calendar"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// p in the timesheet proposes the day's worklogs from what you did
// (internal/work): commits and branch switches in jira.repos, and the
// ui.activity commands' lines. Enter logs one.

// proposalItems are the timesheet's rows for ps, after a heading.
func proposalItems(ps []work.Proposal) []jiraPickerItem {
	if len(ps) == 0 {
		return []jiraPickerItem{{label: i18n.T("── nothing to propose: every session is logged")}}
	}
	items := []jiraPickerItem{{label: i18n.T("── proposed · enter logs one")}}
	for _, p := range ps {
		items = append(items, jiraPickerItem{
			id:    proposalID + p.Key + "/" + strconv.FormatInt(p.Start.Unix(), 10),
			label: fmt.Sprintf("≈ %s  %6s  %s — %s", p.Start.Local().Format("15:04"), jira.FormatDuration(p.Seconds), p.Key, cmp.Or(p.Comment, p.SourcesText())),
			value: strings.TrimSpace(jira.FormatDuration(p.Seconds) + " " + proposalComment(p.Comment)),
		})
	}
	return items
}

type proposalsMsg struct {
	gen    int
	day    time.Time
	items  []jiraPickerItem
	failed []string
	calErr error // ui.calendar could not be read
	err    error
}

// loadProposals reads the day's work and what is logged, for p.
func (m *Model) loadProposals() tea.Cmd {
	p := m.jiraPicker
	gen, day, c, ctx, repos, acts := p.gen, p.day, m.jiraClient, m.ctx, work.Repos(m.jiraRepos), m.uiConfig.Activity
	cal, meetingKey := m.uiConfig.Calendar, strings.TrimSpace(m.uiConfig.MeetingKey)
	m.status = i18n.T("reading git and ui.activity…")
	return func() tea.Msg {
		logs, err := c.MyWorklogs(ctx, day)
		if err != nil {
			return proposalsMsg{gen: gen, day: day, err: err}
		}
		meetings, calErr := calendar.Day(ctx, cal, meetingKey, day, logs)
		ps, failed := work.Day(repos, acts, day, logs, meetings)
		return proposalsMsg{gen: gen, day: day, items: proposalItems(ps), failed: failed, calErr: calErr}
	}
}

// handleProposals puts the proposals under the day's worklogs, in place
// of any before.
func (m Model) handleProposals(msg proposalsMsg) (tea.Model, tea.Cmd) {
	p := &m.jiraPicker
	if !p.active || p.kind != jiraPickTimesheet || p.gen != msg.gen || !p.day.Equal(msg.day) {
		return m, nil
	}
	if msg.err != nil {
		m.fail(i18n.Tf("proposals: %s", msg.err.Error()))
		return m, nil
	}
	i := slices.IndexFunc(p.items, func(it jiraPickerItem) bool {
		return strings.HasPrefix(it.label, "──")
	})
	if i < 0 {
		i = len(p.items)
	}
	p.items = append(p.items[:i:i], msg.items...)
	p.idx = min(i+1, len(p.items)-1)
	m.status = i18n.Tf("%s proposed", i18n.Tn(len(msg.items)-1, "%d worklog", "%d worklogs", len(msg.items)-1))
	if len(msg.failed) > 0 {
		m.status += i18n.Tf(" · ui.activity failed: %s", strings.Join(msg.failed, ", "))
	}
	if msg.calErr != nil {
		m.status += i18n.Tf(" · ui.calendar: %s", msg.calErr.Error())
	}
	return m, nil
}

// proposalID prefixes a proposal row's id: "propose:KEY/unix".
const proposalID = "propose:"

// proposalComment is a proposal's comment as the worklog input reads it
// after the time: one that starts like a time or left: ("2d offsite") is
// set off with a dash, so it stays the comment.
func proposalComment(c string) string {
	if _, _, err := jira.ParseDuration(c); err == nil || strings.HasPrefix(strings.ToLower(c), "left:") {
		return "– " + c
	}
	return c
}
