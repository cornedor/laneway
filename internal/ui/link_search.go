package ui

import (
	"cmp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// A → Link to another issue: the input finds the issue as you type, a key
// (ABC-12, or 12 in the issue's project) looked up, words searched. The
// hits list under it, the picked one with its type, status and assignee,
// so ↵ links what you saw.

// linkFind is the link input's search.
type linkFind struct {
	seq     int
	query   string // what the hits are for
	hits    []jira.Card
	idx     int
	loading bool
	err     string
}

const (
	linkSearchDelay = 250 * time.Millisecond
	linkSearchHits  = 6
)

type linkSearchMsg struct{ seq int }

type linkFoundMsg struct {
	seq   int
	query string
	hits  []jira.Card
	err   error
}

// searchLinkTarget searches after a pause in typing; nil outside the link
// input.
func (m *Model) searchLinkTarget() tea.Cmd {
	if !m.jiraFieldActive || m.jiraFieldName != "link" {
		return nil
	}
	f := &m.linkFind
	f.seq++
	q := strings.TrimSpace(m.jiraFieldInput.Value())
	if q == "" {
		*f = linkFind{seq: f.seq}
		return nil
	}
	f.loading = true
	seq := f.seq
	return tea.Tick(linkSearchDelay, func(time.Time) tea.Msg { return linkSearchMsg{seq} })
}

func (m Model) handleLinkSearch(msg linkSearchMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.linkFind.seq || !m.jiraFieldActive || m.jiraFieldName != "link" {
		return m, nil
	}
	c, ctx, q := m.jiraClient, m.ctx, strings.TrimSpace(m.jiraFieldInput.Value())
	key := jiraGotoKey(q, issueProject(m.jiraFieldKey))
	return m, func() tea.Msg {
		var hits []jira.Card
		var err error
		if key != "" {
			hits, err = c.SearchCards(ctx, "key = "+key)
			if err != nil {
				hits, err = nil, nil // a key that doesn't exist finds nothing
			}
		}
		if len(hits) == 0 && len([]rune(q)) >= 2 {
			hits, err = c.FindIssues(ctx, q, linkSearchHits)
		}
		return linkFoundMsg{seq: msg.seq, query: q, hits: hits, err: err}
	}
}

func (m Model) handleLinkFound(msg linkFoundMsg) (tea.Model, tea.Cmd) {
	f := &m.linkFind
	if msg.seq != f.seq {
		return m, nil
	}
	f.loading, f.query, f.idx, f.err = false, msg.query, 0, ""
	f.hits = f.hits[:0]
	for _, c := range msg.hits {
		if c.Key != m.jiraFieldKey { // not the issue itself
			f.hits = append(f.hits, c)
		}
	}
	if msg.err != nil {
		f.err = msg.err.Error()
	}
	return m, nil
}

// linkKey moves through the hits with ↑ ↓; false for other keys.
func (m *Model) linkKey(msg tea.KeyPressMsg) bool {
	f := &m.linkFind
	if m.jiraFieldName != "link" || len(f.hits) == 0 {
		return false
	}
	switch msg.String() {
	case "up", "ctrl+p":
		f.idx = (f.idx + len(f.hits) - 1) % len(f.hits)
	case "down", "ctrl+n":
		f.idx = (f.idx + 1) % len(f.hits)
	default:
		return false
	}
	return true
}

// linkTarget is the picked hit's key, false while none is found for what
// is typed.
func (m *Model) linkTarget() (string, bool) {
	f := m.linkFind
	if f.loading || len(f.hits) == 0 || f.query != strings.TrimSpace(m.jiraFieldInput.Value()) {
		return "", false
	}
	return f.hits[f.idx].Key, true
}

// linkLines are the hits under the input, the picked one with its details.
func (m *Model) linkLines(width int) []string {
	if m.jiraFieldName != "link" {
		return nil
	}
	f := m.linkFind
	dim := refDimStyle.Render
	switch {
	case strings.TrimSpace(m.jiraFieldInput.Value()) == "":
		return nil
	case f.loading && len(f.hits) == 0:
		return []string{"", dim("looking…")}
	case f.err != "":
		return []string{"", refErrStyle.Render(truncate(f.err, width))}
	case len(f.hits) == 0:
		return []string{"", dim("no issue found")}
	}
	out := []string{""}
	for i, c := range f.hits {
		line := c.Key + "  " + c.Summary
		if i != f.idx {
			out = append(out, dim(truncate("  "+line, width)))
			continue
		}
		out = append(out, jiraKeyStyle.Render(truncate("▸ "+line, width)))
		details := []string{cmp.Or(c.Type, "issue"), cmp.Or(c.Status, "no status"), cmp.Or(c.Assignee, "unassigned")}
		if c.ParentKey != "" {
			details = append(details, "in "+c.ParentKey)
		}
		out = append(out, lipgloss.NewStyle().Italic(true).Render(dim(truncate("    "+strings.Join(details, " · "), width))))
	}
	return out
}
