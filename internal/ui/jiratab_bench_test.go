package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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

// BenchmarkPanelWheel is a wheel notch over a long issue in the panel and
// the frame drawn after it.
func BenchmarkPanelWheel(b *testing.B) {
	m := configuredJiraModel(&testing.T{}, "ABC")
	out, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	out, _ = openRefFor(out.(Model), "ABC-1")
	m = out.(Model)
	var desc strings.Builder
	for i := range 200 {
		fmt.Fprintf(&desc, "Paragraph %d with **bold** and `code` and a long line of words that wraps around the panel a couple of times.\n\n", i)
	}
	var comments []jira.Comment
	for i := range 60 {
		comments = append(comments, jira.Comment{ID: fmt.Sprint(i), Author: "Ann", Created: time.Now(), Body: strings.Repeat("comment words ", 40)})
	}
	out, _ = m.handleJiraLoaded(jiraLoadedMsg{gen: m.refGen, key: "ABC-1", issue: &jira.Issue{Key: "ABC-1", Description: desc.String(), Comments: comments}})
	m = out.(Model)
	listW, _ := m.jiraListWidth(m.width)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		btn := tea.MouseWheelDown
		if i/50%2 == 1 {
			btn = tea.MouseWheelUp
		}
		out, _ := m.Update(tea.MouseWheelMsg{X: listW + 5, Y: 20, Button: btn})
		m = out.(Model)
		_ = m.View()
	}
}
