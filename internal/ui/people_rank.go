package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/people"
)

// peoplePicker is m.pickers with the site's state and GitLab.
func (m Model) peoplePicker() people.Picker {
	p := m.pickers
	if m.store != nil {
		p.Store = m.store
	}
	if m.gitlab != nil {
		p.Forge = m.gitlab
	}
	return p
}

// rankPeople ranks the person picker just opened on key's field (its id,
// "assignee" or "reporter", and name): by what was picked in it and what
// warmPeople kept of key's merge requests. Call it before the picker's
// fetch, which orders by the ranks; the list never reorders after.
func (m *Model) rankPeople(key, field, name string) {
	pp := m.peoplePicker()
	m.jiraPicker.field = field
	m.jiraPicker.ranks = pp.Ranks(key, pp.Sources(people.Project(key), field, name), field)
}

// warmPeople reads key's merge requests for the person pickers' rules in
// the background, when the issue opens, so its pickers need not wait.
func (m Model) warmPeople(key string) tea.Cmd {
	pp, c, ctx := m.peoplePicker(), m.jiraClient, m.ctx
	if !pp.Wants(key) {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		pp.Warm(ctx, key, func(ctx context.Context, q string) ([]jira.User, error) {
			return c.AssignableUsers(ctx, key, q)
		})
		return nil
	}
}

// rankItems orders a person picker's rows by ranks, each ranked one saying
// why.
func rankItems(items []jiraPickerItem, ranks map[string]people.Rank) {
	if len(ranks) == 0 {
		return
	}
	for i := range items {
		if r, ok := ranks[items[i].id]; ok && items[i].id != "" {
			items[i].hint = whyText(r.Why)
		}
	}
	people.Order(items, func(it jiraPickerItem) string { return it.id }, ranks)
}

// whyText is a ranked row's reason.
func whyText(source string) string {
	switch source {
	case people.CodeOwners:
		return i18n.T("code owner")
	case people.Reviewers:
		return i18n.T("reviewed this code")
	case people.Blame:
		return i18n.T("wrote this code")
	}
	return i18n.T("picked often")
}

// recordPick counts a person picked in the open picker's field.
func (m Model) recordPick(key, account string) {
	if m.store == nil || m.jiraPicker.field == "" || account == "" {
		return
	}
	_ = people.Record(m.store, people.Project(key), m.jiraPicker.field, account, time.Now())
}
