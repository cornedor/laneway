package ui

import (
	"testing"

	"github.com/cornedor/laneway/internal/forge"
)

// TestRemoteIsProject: ssh and https remotes name their project, nested
// groups whole.
func TestRemoteIsProject(t *testing.T) {
	for url, want := range map[string]bool{
		"git@git.example.com:g/sub/p.git":       true,
		"https://git.example.com/g/sub/p.git":   true,
		"ssh://git@git.example.com:22/g/sub/p":  true,
		"git@git.example.com:g/sub/other.git":   false,
		"https://git.example.com/g/sub/p/extra": false,
	} {
		if got := remoteIsProject(url, "g/sub/p"); got != want {
			t.Errorf("remoteIsProject(%q) = %v", url, got)
		}
	}
}

// TestMRReviewNeeds: C says what it lacks: herdr, an open merge request.
func TestMRReviewNeeds(t *testing.T) {
	m := loadedJiraModel(t)
	m.herdr = nil
	if cmd := m.startMRReview(&forge.Change{State: forge.StateOpen}, forge.Ref{Repo: "g/p", Number: 1}); cmd != nil || m.status == "" {
		t.Errorf("no herdr: cmd %v, status %q", cmd != nil, m.status)
	}
}
