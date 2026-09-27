package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

func TestLabelWord(t *testing.T) {
	for in, want := range map[string][3]string{
		"ui ap":  {"", "ap", ""},
		"ui -ol": {"-", "ol", ""},
		"":       {"", "", ""},
		"ui ":    {"", "", ""},
	} {
		ti := textinput.New()
		ti.SetValue(in)
		ti.CursorEnd()
		if sign, word, _ := labelWord(&ti); sign != want[0] || word != want[1] {
			t.Errorf("%q = %q %q", in, sign, word)
		}
	}
}

// TestLabelSuggest: typing in the panel's labels lists Jira's matching
// labels under it, those set left out; ↓ and tab take one.
func TestLabelSuggest(t *testing.T) {
	m, _ := actionsModel(t, map[string]string{
		"/rest/api/3/jql/autocompletedata/suggestions": `{"results":[{"value":"ui"},{"value":"uikit"},{"value":"uiux"}]}`,
	})
	m.jiraIssue.Labels = []string{"ui"}
	out, _ := m.handleRefKey(keyMsg(t, "l"))
	m = out.(Model)
	for _, k := range []string{"space", "u"} {
		out, _ = m.handleKey(keyMsg(t, k))
		m = out.(Model)
	}
	out, cmd := m.handleLabelTick(labelTickMsg{m.labels.seq})
	m = out.(Model)
	out, _ = m.handleLabelsFound(cmd().(labelsFoundMsg))
	m = out.(Model)
	if got := m.labels.list; len(got) != 2 || got[0] != "uikit" {
		t.Fatalf("suggestions = %q", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "▸ uikit") {
		t.Error("the panel should list them")
	}
	out, _ = m.handleKey(keyMsg(t, "down"))
	out, _ = out.(Model).handleKey(keyMsg(t, "tab"))
	m = out.(Model)
	if got := m.jiraFieldInput.Value(); got != "ui uiux " || len(m.labels.list) != 0 {
		t.Errorf("value %q, list %q", got, m.labels.list)
	}
}
