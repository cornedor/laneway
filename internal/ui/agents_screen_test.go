package ui

import (
	"path/filepath"
	"testing"

	"github.com/cornedor/laneway/internal/herdr"
)

// TestDemoLeavesHerdrOff: the demo neither lists nor drives your agents.
func TestDemoLeavesHerdrOff(t *testing.T) {
	m := jiraTabModel(t)
	m.herdr = herdr.New(filepath.Join(t.TempDir(), "herdr.sock"))
	m = m.WithDemo()
	if m.herdr != nil {
		t.Fatal("WithDemo kept the herdr client")
	}
	m.openAgents()
	if m.status != "herdr is off in the demo" {
		t.Errorf("ctrl+g in the demo: status %q", m.status)
	}
}
