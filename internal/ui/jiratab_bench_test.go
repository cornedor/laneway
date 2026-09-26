package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// bigJiraModel is jiraTabModel with n cards spread over the lanes.
func bigJiraModel(b *testing.B, n int) Model {
	m := jiraTabModel(&testing.T{})
	cards := make([]jira.Card, n)
	ids := []string{"1", "3", "5"}
	for i := range cards {
		cards[i] = jira.Card{Key: fmt.Sprintf("ABC-%d", i), Summary: fmt.Sprintf("Summary of issue number %d with some words", i),
			StatusID: ids[i%3], Status: "Status", Assignee: "Ada", Points: "3", Type: "Story",
			PR: "OPEN", Subtasks: 4, SubtasksDone: i % 5, Due: time.Now().AddDate(0, 0, i%9-3), Flagged: i%7 == 0}
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

func BenchmarkRenderJiraSwimlanes(b *testing.B) {
	m := bigJiraModel(b, 600)
	for i := range m.jiraTab.cards {
		m.jiraTab.cards[i].Assignee = fmt.Sprintf("Person %d", i%8)
		m.jiraTab.cards[i].AssigneeID = fmt.Sprintf("p%d", i%8)
	}
	m.jiraTab.swim = jiraSortAssignee
	m.buildJiraLanes()
	b.ResetTimer()
	for b.Loop() {
		m.moveJiraCursor(1)
	}
}
