package ui

import (
	"context"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/store"
)

// configuredJiraModel is a sized app on a throwaway store.
func configuredJiraModel(t *testing.T, projects ...string) Model {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), config.JiraConfig{
		BaseURL:  "https://example.atlassian.net",
		Email:    "me@x.test",
		APIToken: "tok",
		Projects: projects,
	}, config.UIConfig{}, nil, "", st)
	m.herdr = nil
	m.width, m.height = 120, 40
	m.resize()
	return m
}

// openRefFor opens the panel on key, as enter on its card does.
func openRefFor(m Model, key string) (tea.Model, tea.Cmd) {
	m.refOpen = true
	m.refs = []reference{{kind: refJira, jiraKey: key}}
	m.refIdx = 0
	m.focus = focusRef
	m.resize()
	return m, m.loadCurrentRef()
}

func keyStr(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	default:
		r := []rune(s)
		return tea.KeyPressMsg(tea.Key{Code: r[0], Text: s})
	}
}

func keyMsg(t *testing.T, name string) tea.KeyPressMsg {
	t.Helper()
	switch name {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+x":
		return tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
	case "ctrl+y":
		return tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	}
	if r := []rune(name); len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: name}
	}
	t.Fatalf("keyMsg: unknown key %q", name)
	return tea.KeyPressMsg{}
}
