package ui

import (
	"encoding/json"
	"testing"

	"github.com/cornedor/laneway/internal/jira"
)

// TestJiraCacheRoundTrip: a stored board comes back as the load it was.
func TestJiraCacheRoundTrip(t *testing.T) {
	in := jiraBoardMsg{project: "ABC", boards: []jira.Board{{ID: 1, Name: "B", Type: "scrum"}},
		cfg:     &jira.BoardConfig{Columns: []jira.Column{{Name: "To do", StatusIDs: []string{"1"}}}},
		views:   []jiraView{{kind: jiraViewSprint, name: "S", sprint: 9, lanes: true}, {kind: jiraViewBacklog, name: "Backlog"}},
		viewIdx: 1, quickOn: map[int]bool{7: true}, assignee: jiraAssignee{id: "me", label: "Me"},
		cards: []jira.Card{{Key: "ABC-1", StatusID: "1"}}, total: 3}
	raw, err := json.Marshal(cacheOf(in, "x = 1"))
	if err != nil {
		t.Fatal(err)
	}
	var c jiraCache
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	out := c.boardMsg(4)
	if !out.cached || out.seq != 4 || out.project != "ABC" || out.views[0] != in.views[0] || out.viewIdx != 1 ||
		!out.quickOn[7] || out.assignee != in.assignee || out.cards[0].Key != "ABC-1" || out.total != 3 || c.Filter != "x = 1" {
		t.Errorf("round trip = %+v", out)
	}
}

// TestJiraCachedAfterFresh: a stored copy arriving after the network's is
// dropped; one arriving first shows while the load goes on.
func TestJiraCachedAfterFresh(t *testing.T) {
	m := jiraTabModel(t)
	cmd := m.loadJiraCards(1, true)
	if cmd == nil {
		t.Fatal("no load")
	}
	seq := m.jiraTab.seq
	out, _ := m.handleJiraCards(jiraCardsMsg{seq: seq, cached: true, viewIdx: 1, cards: []jira.Card{{Key: "OLD-1"}}})
	m = out.(Model)
	if !m.jiraTab.loading || m.jiraTab.cards[0].Key != "OLD-1" {
		t.Fatalf("cached copy not shown while loading: loading=%v", m.jiraTab.loading)
	}
	out, _ = m.handleJiraCards(jiraCardsMsg{seq: seq, viewIdx: 1, cards: []jira.Card{{Key: "NEW-1"}}})
	m = out.(Model)
	out, _ = m.handleJiraCards(jiraCardsMsg{seq: seq, cached: true, viewIdx: 1, cards: []jira.Card{{Key: "OLD-1"}}})
	m = out.(Model)
	if m.jiraTab.loading || m.jiraTab.cards[0].Key != "NEW-1" {
		t.Errorf("late cached copy won: cards=%v loading=%v", m.jiraTab.cards, m.jiraTab.loading)
	}
}
