package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/i18n"
)

// The settings overlay's GitLab group: each instance (the gitlab: config's,
// then the hosts glab is logged in to) and whether its token signs in.

// WithGitLab gives the model its GitLab instances.
func (m Model) WithGitLab(s *gitlab.Sites) Model {
	m.gitlab = s
	return m
}

// gitlabCheckMsg is every instance signed in to.
type gitlabCheckMsg struct{ sites []gitlab.Status }

// checkGitLab signs in to every instance, for the settings rows.
func (m *Model) checkGitLab() tea.Cmd {
	if m.gitlab == nil {
		return nil
	}
	s, ctx := m.gitlab, m.ctx
	return func() tea.Msg { return gitlabCheckMsg{s.Check(ctx)} }
}

// gitlabRows are the GitLab group's rows for the instances checked.
func gitlabRows(sites []gitlab.Status) []settingRow {
	var rows []settingRow
	for _, st := range sites {
		v := i18n.T("no token")
		switch {
		case st.Err == nil:
			v = st.User.Username + " (" + st.From + ")"
		case st.From != "":
			v = i18n.T("fails")
		}
		rows = append(rows, settingRow{name: st.Host, value: v, def: st.BaseURL, group: "GitLab", info: true, doc: st.Summary()})
	}
	return rows
}

func (m Model) handleGitLabCheck(msg gitlabCheckMsg) (tea.Model, tea.Cmd) {
	if s := m.settings; s != nil {
		s.gitlab = gitlabRows(msg.sites)
		s.all = append(settingRows(m.uiConfig), s.gitlab...)
		s.applySettingsFilter()
	}
	return m, nil
}
