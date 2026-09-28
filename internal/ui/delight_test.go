package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestConfetti: a card moved into the done lane bursts over its head, the
// ticks thin it out and stop; ui.delight off skips it.
func TestConfetti(t *testing.T) {
	m := jiraTabModel(t)
	m.jiraTab.cards[3].Done = true
	m.moveJiraCard("ABC-1", 2, "")
	out, _ := m.handleJiraMoved(jiraMovedMsg{key: "ABC-1", lane: "Done"})
	m = out.(Model)
	c := m.jiraTab.confetti
	if !c.on("Done") {
		t.Fatalf("no burst on Done: %+v", c)
	}
	if !strings.ContainsAny(ansi.Strip(m.View().Content), string(confettiDots)) {
		t.Error("no dots on the lane head")
	}
	for range confettiFrames {
		out, _ = m.handleConfetti(confettiMsg{seq: c.seq})
		m = out.(Model)
	}
	if m.jiraTab.confetti.lane != "" {
		t.Errorf("burst still on after %d frames", confettiFrames)
	}

	m.moveJiraCard("ABC-3", 1, "")
	out, _ = m.handleJiraMoved(jiraMovedMsg{key: "ABC-3", lane: "In progress"})
	if m = out.(Model); m.jiraTab.confetti.lane != "" {
		t.Error("burst on a lane that isn't done")
	}
	m.opts.delight = false
	m.moveJiraCard("ABC-3", 2, "")
	out, _ = m.handleJiraMoved(jiraMovedMsg{key: "ABC-3", lane: "Done"})
	if m = out.(Model); m.jiraTab.confetti.lane != "" {
		t.Error("burst with ui.delight off")
	}
}

func TestSprintCheer(t *testing.T) {
	v := func(done ...float64) []jira.SprintVelocity {
		var out []jira.SprintVelocity
		for _, d := range done {
			out = append(out, jira.SprintVelocity{Done: d})
		}
		return out
	}
	for _, tc := range []struct {
		vel  []jira.SprintVelocity
		want string
	}{
		{v(20, 30, 34), "34p, best of the last 3"},
		{v(40, 30, 34), "34p, #2 of the last 3"},
		{v(40, 50, 60, 10.5), "10.5p done"},
		{v(34), ""},
		{v(20, 0), ""},
	} {
		if got := sprintCheer(tc.vel); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.vel, got, tc.want)
		}
	}
}
