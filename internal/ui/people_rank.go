package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/people"
)

// peopleRanksMsg is a person picker's ranking with the merge requests'
// part, for the picker of gen.
type peopleRanksMsg struct {
	gen   int
	ranks map[string]people.Rank
}

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
// "assignee" or "reporter", and name) by what was picked in it, and
// returns the read of its merge requests when the field's rule asks for
// one. Call it before the picker's fetch, which orders by the ranks.
func (m *Model) rankPeople(key, field, name string) tea.Cmd {
	pp := m.peoplePicker()
	project := people.Project(key)
	sources := pp.Sources(project, field, name)
	m.jiraPicker.field = field
	m.jiraPicker.ranks = pp.Local(sources, project, field)
	if pp.Forge == nil || !people.Remote(sources) || !strings.Contains(key, "-") {
		return nil
	}
	gen, c, ctx := m.jiraPicker.gen, m.jiraClient, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ranks := pp.Ranks(ctx, key, sources, field, func(ctx context.Context, q string) ([]jira.User, error) {
			return c.AssignableUsers(ctx, key, q)
		})
		return peopleRanksMsg{gen: gen, ranks: ranks}
	}
}

// handlePeopleRanks reorders the open picker by the full ranking, the
// cursor kept on its person.
func (m Model) handlePeopleRanks(msg peopleRanksMsg) (tea.Model, tea.Cmd) {
	p := &m.jiraPicker
	if !p.active || msg.gen != p.gen {
		return m, nil
	}
	p.ranks = msg.ranks
	chosen := ""
	if p.idx < len(p.items) {
		chosen = p.items[p.idx].id
	}
	rankItems(p.items, p.ranks)
	if !p.loading {
		for i, it := range p.items {
			if it.id == chosen {
				p.idx = i
				break
			}
		}
	}
	return m, nil
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
