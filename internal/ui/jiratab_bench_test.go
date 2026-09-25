package ui

import (
	"fmt"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// bigJiraModel is jiraTabModel with n cards spread over the lanes.
func bigJiraModel(b *testing.B, n int) Model {
	m := jiraTabModel(&testing.T{})
	cards := make([]jira.Card, n)
	ids := []string{"1", "3", "5"}
	for i := range cards {
		cards[i] = jira.Card{Key: fmt.Sprintf("ABC-%d", i), Summary: fmt.Sprintf("Summary of issue number %d with some words", i),
			StatusID: ids[i%3], Status: "Status", Assignee: "Ada", Points: "3", Type: "Story"}
	}
	m.installJiraCards(cards, n, nil, "")
	return m
}

func BenchmarkRenderJiraLanes(b *testing.B) {
	m := bigJiraModel(b, 600)
	b.ResetTimer()
	for b.Loop() {
		m.moveJiraCursor(1)
	}
}

func BenchmarkRenderJiraList(b *testing.B) {
	m := bigJiraModel(b, 600)
	m.jiraTab.wantLanes = false
	m.buildJiraLanes()
	b.ResetTimer()
	for b.Loop() {
		m.moveJiraCursor(1)
	}
}

func BenchmarkJiraView(b *testing.B) {
	m := bigJiraModel(b, 600)
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}
