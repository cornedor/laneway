package ui

import (
	"encoding/json"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

// The Jira tab's board as last seen, kept in the store's meta table per board
// and view, so opening the tab or switching view shows it at once while the
// fresh copy loads. A cached message carries the load's seq like the network
// one; whichever lands second only wins if it is the network's.

// jiraCache is one board view, as stored.
type jiraCache struct {
	Project     string
	Boards      []jira.Board
	Board       int
	Cfg         *jira.BoardConfig
	Views       []jiraCacheView
	ViewIdx     int
	Quick       []jira.QuickFilter
	QuickOn     map[int]bool
	Assignee    [2]string // id, label
	StatusNames map[string]string
	Filter      string // the JQL the cards were narrowed by
	Cards       []jira.Card
	Total       int
}

type jiraCacheView struct {
	Kind   jiraViewKind
	Name   string
	Sprint int
	JQL    string `json:",omitempty"`
	Lanes  bool
}

func jiraCacheKey(board int, view string) string {
	return jiraMetaPrefix + "cache:" + strconv.Itoa(board) + ":" + view
}

// jiraViewKey remembers a board's last view, so the tab reopens on it.
func jiraViewKey(board int) string {
	return jiraMetaPrefix + "view:" + strconv.Itoa(board)
}

// cacheOf is what a successful board load stores.
func cacheOf(msg jiraBoardMsg, filter string) jiraCache {
	c := jiraCache{Project: msg.project, Boards: msg.boards, Board: msg.board, Cfg: msg.cfg, ViewIdx: msg.viewIdx,
		Quick: msg.quick, QuickOn: msg.quickOn, Assignee: [2]string{msg.assignee.id, msg.assignee.label},
		StatusNames: msg.statusNames, Filter: filter, Cards: msg.cards, Total: msg.total}
	for _, v := range msg.views {
		c.Views = append(c.Views, jiraCacheView{Kind: v.kind, Name: v.name, Sprint: v.sprint, JQL: v.jql, Lanes: v.lanes})
	}
	return c
}

// boardMsg turns a stored board back into the message a load delivers.
func (c jiraCache) boardMsg(seq int) jiraBoardMsg {
	msg := jiraBoardMsg{seq: seq, cached: true, project: c.Project, boards: c.Boards, board: c.Board, cfg: c.Cfg,
		viewIdx: c.ViewIdx, quick: c.Quick, quickOn: c.QuickOn, assignee: jiraAssignee{id: c.Assignee[0], label: c.Assignee[1]},
		statusNames: c.StatusNames, cards: c.Cards, total: c.Total}
	for _, v := range c.Views {
		msg.views = append(msg.views, jiraView{kind: v.Kind, name: v.Name, sprint: v.Sprint, jql: v.JQL, lanes: v.Lanes})
	}
	return msg
}

// saveJiraCache stores c as board's view, and that view as the board's last.
func saveJiraCache(st *store.Store, board int, view string, c jiraCache) {
	if raw, err := json.Marshal(c); err == nil {
		_ = st.SetMeta(jiraCacheKey(board, view), string(raw))
	}
	_ = st.SetMeta(jiraViewKey(board), view)
}

func readJiraCache(st *store.Store, board int, view string) (jiraCache, bool) {
	var c jiraCache
	raw, ok, _ := st.GetMeta(jiraCacheKey(board, view))
	if !ok || json.Unmarshal([]byte(raw), &c) != nil || c.Cfg == nil || c.ViewIdx >= len(c.Views) {
		return c, false
	}
	return c, true
}

// jiraBoardFromCache delivers the stored copy of the board loadJiraBoard is
// about to fetch, resolved the same way; nil-message when there is none.
func jiraBoardFromCache(st *store.Store, seq int, project string, board int, view string, configured []string, readMode bool) tea.Cmd {
	return func() tea.Msg {
		project, board = resolveJiraBoard(st, project, board, configured)
		if board == 0 {
			return nil
		}
		if view == "" {
			view, _, _ = st.GetMeta(jiraViewKey(board))
		}
		c, ok := readJiraCache(st, board, view)
		if !ok || c.Project != project {
			return nil
		}
		msg := c.boardMsg(seq)
		if readMode {
			msg.lanes = storedJiraMode(st)
		}
		return msg
	}
}

// resolveJiraBoard fills an empty project and a zero board from what the
// store remembers (then the first configured project); the board stays 0
// when nothing is remembered for the project.
func resolveJiraBoard(st *store.Store, project string, board int, configured []string) (string, int) {
	if project == "" {
		project, _, _ = st.GetMeta(jiraMetaPrefix + "project")
	}
	if project == "" && len(configured) > 0 {
		project = configured[0]
	}
	if board == 0 && project != "" {
		if v, ok, _ := st.GetMeta(jiraMetaPrefix + "board:" + project); ok {
			board, _ = strconv.Atoi(v)
		}
	}
	return project, board
}

func storedJiraMode(st *store.Store) *bool {
	v, ok, _ := st.GetMeta(jiraMetaPrefix + "mode")
	if !ok {
		return nil
	}
	lanes := v != "list"
	return &lanes
}
