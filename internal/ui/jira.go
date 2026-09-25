package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// Jira-specific half of the reference panel: fetching an issue and rendering it.
// The shared panel machinery (open/close/cycle/keys/layout) lives in ref.go;
// the inline field editors live in jira_edit.go.

// jiraLoadedMsg carries a finished background fetch. gen guards a stale result
// the user already cycled or closed past (see Model.refGen); key records which
// issue it was for.
type jiraLoadedMsg struct {
	gen   int
	key   string
	issue *jira.Issue
	err   error
}

// fetchJira fetches (and caches) an issue in the background, returning a
// jiraLoadedMsg tagged with gen.
func (m Model) fetchJira(gen int, key string) tea.Cmd {
	client := m.jiraClient
	ctx := m.ctx
	return func() tea.Msg {
		issue, err := client.Get(ctx, key)
		return jiraLoadedMsg{gen: gen, key: key, issue: issue, err: err}
	}
}

// handleJiraLoaded installs a finished fetch, unless the user has since cycled
// or closed the panel (stale gen) or moved to a non-Jira reference.
func (m Model) handleJiraLoaded(msg jiraLoadedMsg) (tea.Model, tea.Cmd) {
	r := m.currentRef()
	if !m.refOpen || msg.gen != m.refGen || r == nil {
		return m, nil
	}
	m.refLoading = false
	if msg.err != nil {
		m.refErr = msg.err
		m.jiraIssue = nil
	} else {
		m.refErr = nil
		m.jiraIssue = msg.issue
	}
	m.renderRef()
	return m, tea.Batch(m.fetchIssueImages(m.jiraIssue), m.fetchPanelExtra())
}

// renderJiraIssue formats one issue for the viewport: a key + type header, the
// summary, an aligned meta block, then the description rendered through the
// shared markdown renderer (so links are clickable and inline styling matches
// the message pane).
func (m *Model) renderJiraIssue(iss *jira.Issue, width int) string {
	var b strings.Builder

	header := refKeyStyle.Render(iss.Key)
	if iss.Type != "" {
		header += "  " + refDimStyle.Render(iss.Type)
	}
	b.WriteString(header + "\n")
	sel := m.panelFieldIs
	switch {
	case sel("Summary"):
		b.WriteString(selectedRow.Render(orDash(iss.Summary)) + "\n")
	case iss.Summary != "":
		b.WriteString(titleStyle.Render(iss.Summary) + "\n")
	}
	b.WriteString("\n")

	refField(&b, "Status", iss.Status, 10, sel("Status"))
	refField(&b, "Priority", iss.Priority, 10, sel("Priority"))
	refField(&b, "Points", iss.StoryPoints, 10, sel("Points"))
	refField(&b, "Assignee", iss.Assignee, 10, sel("Assignee"))
	refMeta(&b, "Reporter", iss.Reporter, 10)
	refField(&b, "Labels", strings.Join(iss.Labels, ", "), 10, sel("Labels"))
	if !iss.Updated.IsZero() {
		refMeta(&b, "Updated", iss.Updated.Format(m.opts.dateFormat), 10)
	}
	if extra := m.extraFields(); len(extra) > 0 {
		w := 10
		for _, ff := range extra {
			w = max(w, len(ff.Name)+2)
		}
		b.WriteString("\n")
		for i, ff := range extra {
			refField(&b, ff.Name, jiraValueText(ff.val), w, m.panelFieldIdx() == len(panelFields)+i)
		}
	}

	// Edit affordances: the field cursor (panel_fields.go), comments
	// (jira_comment.go).
	b.WriteString("\n" + refDimStyle.Render("tab fields · ↵ edit · c comment · R reply · S start work · ? keys") + "\n")

	if desc := strings.TrimSpace(iss.Description); desc != "" {
		divW := width
		if divW < 1 {
			divW = 1
		}
		b.WriteString("\n" + refDimStyle.Render(strings.Repeat("─", divW)) + "\n")
		b.WriteString(renderMarkdown(desc, m.emojiImg, nil, ""))
	}

	m.renderJiraLinks(&b, iss, width)
	m.renderJiraAttachments(&b, iss, width)
	m.renderJiraComments(&b, iss, width)
	return b.String()
}

// renderJiraLinks lists the parent, linked issues and subtasks; L picks one
// to open.
func (m *Model) renderJiraLinks(b *strings.Builder, iss *jira.Issue, width int) {
	if len(iss.Links) == 0 {
		return
	}
	b.WriteString("\n" + refDimStyle.Render(strings.Repeat("─", max(width, 1))) + "\n")
	b.WriteString(refLabelStyle.Render(fmt.Sprintf("Links (%d)", len(iss.Links))) + refDimStyle.Render("  L open") + "\n")
	for _, l := range iss.Links {
		line := refDimStyle.Render(l.Rel+" ") + jiraKeyStyle.Render(l.Key) + " " + l.Summary
		if l.Status != "" {
			line += refDimStyle.Render(" · " + l.Status)
		}
		b.WriteString(ansi.Truncate(line, max(width, 1), "…") + "\n")
	}
}

// openJiraLinkPicker lists the shown issue's links to jump to.
func (m *Model) openJiraLinkPicker() {
	if m.jiraIssue == nil || len(m.jiraIssue.Links) == 0 {
		m.status = "no linked issues"
		return
	}
	m.startJiraPicker(jiraPickLink, "Go to linked issue", false)
	items := make([]jiraPickerItem, len(m.jiraIssue.Links))
	for i, l := range m.jiraIssue.Links {
		items[i] = jiraPickerItem{id: l.Key, label: l.Rel + " " + l.Key + " " + l.Summary}
	}
	m.setJiraPickerItems(items)
}

// renderJiraAttachments lists the files the description and comments don't
// already show, each a link that downloads it.
func (m *Model) renderJiraAttachments(b *strings.Builder, iss *jira.Issue, width int) {
	var rest []jira.Attachment
	for _, a := range iss.Attachments {
		if !issueShowsAttachment(iss, a.ID) {
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		return
	}
	b.WriteString("\n" + refDimStyle.Render(strings.Repeat("─", max(width, 1))) + "\n")
	b.WriteString(refLabelStyle.Render(fmt.Sprintf("Attachments (%d)", len(rest))) + "\n")
	for _, a := range rest {
		name := attachmentStyle.Render("📎 " + a.Filename)
		if u := m.jiraClient.AttachmentURL(a.ID); u != "" {
			name = osc8Link(u, name)
		}
		b.WriteString(name + refDimStyle.Render("  "+byteSize(a.Size)) + "\n")
	}
}

// byteSize is n bytes for people: 512 B, 3.4 KB, 12 MB.
func byteSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 10<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// renderJiraComments appends the issue's comment thread under the description: a
// divider, a "Comments (N)" heading, then each comment (oldest first, the order
// the API returns) as a dim author·timestamp line and its markdown body. When
// the issue has more comments than the inline field returned, a trailing note
// points at the browser.
func (m *Model) renderJiraComments(b *strings.Builder, iss *jira.Issue, width int) {
	if len(iss.Comments) == 0 && iss.CommentTotal == 0 {
		return
	}
	divW := width
	if divW < 1 {
		divW = 1
	}
	b.WriteString("\n" + refDimStyle.Render(strings.Repeat("─", divW)) + "\n")

	count := iss.CommentTotal
	if count < len(iss.Comments) {
		count = len(iss.Comments)
	}
	b.WriteString(refLabelStyle.Render(fmt.Sprintf("Comments (%d)", count)) + "\n\n")

	for i, c := range iss.Comments {
		author := c.Author
		if author == "" {
			author = "Unknown"
		}
		when := ""
		if !c.Created.IsZero() {
			when = " · " + c.Created.Format(m.opts.dateFormat)
		}
		b.WriteString(refDimStyle.Render(author+when) + "\n")
		if body := strings.TrimSpace(c.Body); body != "" {
			b.WriteString(renderMarkdown(body, m.emojiImg, nil, ""))
		}
		if i < len(iss.Comments)-1 {
			b.WriteString("\n")
		}
	}

	if extra := iss.CommentTotal - len(iss.Comments); extra > 0 {
		b.WriteString("\n" + refDimStyle.Render(fmt.Sprintf("…and %d more — o opens in browser", extra)) + "\n")
	}
}
