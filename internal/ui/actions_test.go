package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/config"
)

// TestActions: an action runs on its key with the card as JSON and
// LANEWAY_KEY; a key a built-in has keeps it to the palette.
func TestActions(t *testing.T) {
	k := defaultKeys()
	acts, warn := actionsFrom([]config.Action{
		{Name: "echo", Key: "!", Command: []string{"sh", "-c", `read in; echo "$LANEWAY_KEY $in" | cut -c1-40`}},
		{Name: "taken", Key: "q", Command: []string{"true"}},
		{Name: "pager", Command: []string{"printf", "one\ntwo"}, Show: "pager", Where: "panel"},
		{Name: "bad"},
	}, &k)
	if len(acts) != 3 || acts[1].Key != "" || len(warn) != 2 || !strings.Contains(warn[0], `"q" is quit already`) {
		t.Fatalf("actions %v, warnings %q", acts, warn)
	}
	m := jiraTabModel(t)
	m.actions = acts
	out, cmd := m.handleJiraKey(keyStr("!"))
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("! should run the action: %q", m.status)
	}
	out, _ = m.Update(cmd())
	if m = out.(Model); !strings.HasPrefix(m.status, `echo: ABC-1 {"key":"ABC-1","summary":"First"`) {
		t.Errorf("status %q", m.status)
	}
	m.openPalette()
	if labels := strings.Join(paletteLabels(m), "\n"); !strings.Contains(labels, "action  echo  !") || strings.Contains(labels, "action  pager") {
		t.Errorf("palette:\n%s", labels)
	}
	m.closeJiraPicker()
	out, _ = m.Update(actionDoneMsg{i: 2, out: "one\ntwo"})
	m = out.(Model)
	if v := ansi.Strip(m.View().Content); !m.jiraPicker.active || !strings.Contains(v, "two") {
		t.Errorf("pager:\n%s", v)
	}
}
