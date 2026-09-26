package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		m.rememberRecent(msg.issue.Key, msg.issue.Summary)
	}
	m.activity = activityState{} // a refetch reloads the history too
	activity := m.loadActivity()
	m.renderRef()
	return m, tea.Batch(m.fetchIssueImages(m.jiraIssue), m.fetchPanelExtra(), activity)
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
	m.panelFieldLine = m.panelFieldLine[:0]
	m.pickerLine = -1
	line := func() { m.panelFieldLine = append(m.panelFieldLine, strings.Count(b.String(), "\n")) }
	line() // Summary
	sel := m.panelFieldIs
	switch {
	case m.fieldInlineOn("Summary"):
		b.WriteString(m.fieldInlineView(0, width) + "\n")
	case sel("Summary"):
		b.WriteString(selectedRow.Render(orDash(iss.Summary)) + "\n")
	case iss.Summary != "":
		b.WriteString(titleStyle.Render(iss.Summary) + "\n")
	}
	b.WriteString("\n")

	line()
	if st, ok := statusLozenge[iss.StatusCategory]; ok && iss.Status != "" && !sel("Status") {
		b.WriteString(refLabelStyle.Render(refMetaLabel("Status", 10)) + st.Render(" "+strings.ToUpper(iss.Status)+" ") + "\n")
	} else {
		refField(&b, "Status", iss.Status, 10, sel("Status"))
	}
	m.inlinePickerUnder(&b, "Status", 10, width)
	line()
	refField(&b, "Priority", iss.Priority, 10, sel("Priority"))
	m.inlinePickerUnder(&b, "Priority", 10, width)
	line()
	m.refFieldEdit(&b, "Points", iss.StoryPoints, 10, width)
	line()
	refField(&b, "Assignee", iss.Assignee, 10, sel("Assignee"))
	m.inlinePickerUnder(&b, "Assignee", 10, width)
	refMeta(&b, "Reporter", iss.Reporter, 10)
	line()
	m.refFieldEdit(&b, "Labels", strings.Join(iss.Labels, ", "), 10, width)
	if !iss.Updated.IsZero() {
		refMeta(&b, "Updated", m.when(iss.Updated), 10)
	}
	// The deployment rides on the board's card (its Development field).
	if i := slices.IndexFunc(m.jiraTab.cards, func(c jira.Card) bool { return c.Key == iss.Key }); i >= 0 && m.jiraTab.cards[i].Deploy != "" {
		refMeta(&b, "Deployed", m.jiraTab.cards[i].Deploy, 10)
	}
	if extra := m.extraFields(); len(extra) > 0 {
		w := 10
		for _, ff := range extra {
			w = max(w, len(ff.Name)+2)
		}
		b.WriteString("\n")
		for i, ff := range extra {
			line()
			val := jiraValueText(ff.val)
			if richField(ff) {
				val = "↓ below"
			}
			if m.fieldInlineOn(ff.ID) {
				b.WriteString(refLabelStyle.Render(refMetaLabel(ff.Name, w)) + m.fieldInlineView(w, width) + "\n")
				continue
			}
			refField(&b, ff.Name, val, w, m.panelFieldIdx() == len(panelFields)+i)
			m.inlinePickerUnder(&b, ff.ID, w, width)
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
		b.WriteString(sectionHead("Description", "", divW))
		b.WriteString(renderMarkdown(desc, m.emojiImg, nil, ""))
	}
	// Rich-text fields read like the description, under their own heads.
	for _, ff := range m.extraFields() {
		if richField(ff) {
			b.WriteString(sectionHead(ff.Name, "", max(width, 1)))
			b.WriteString(renderMarkdown(ff.val.Text, m.emojiImg, nil, ""))
		}
	}

	m.renderJiraLinks(&b, iss, width)
	m.renderJiraAttachments(&b, iss, width)
	m.renderJiraActivity(&b, iss, width)
	return b.String()
}

// refFieldEdit writes the panel's own field row: the inline input while it
// is edited, else refField.
func (m *Model) refFieldEdit(b *strings.Builder, name, value string, labelW, width int) {
	if m.fieldInlineOn(name) {
		b.WriteString(refLabelStyle.Render(refMetaLabel(name, labelW)) + m.fieldInlineView(labelW, width) + "\n")
		return
	}
	refField(b, name, value, labelW, m.panelFieldIs(name))
}

// inlinePickerUnder drops the inline picker under row name when it is open
// there.
func (m *Model) inlinePickerUnder(b *strings.Builder, name string, indent, width int) {
	if m.pickerInlineOn(name) {
		m.renderInlinePicker(b, indent, width)
	}
}

// fieldInlineView is the field input fitted to the row after a label
// labelW wide.
func (m *Model) fieldInlineView(labelW, width int) string {
	m.jiraFieldInput.SetWidth(max(width-labelW-lipgloss.Width(m.jiraFieldInput.Prompt)-1, 4))
	return m.jiraFieldInput.View()
}

// richField is a filled rich-text (ADF) field, drawn as its own section
// rather than squeezed onto its row.
func richField(ff jiraFormField) bool {
	return ff.Kind == jira.KindDoc && strings.TrimSpace(ff.val.Text) != ""
}

// statusLozenge colours a status by its category, as Jira's lozenges do:
// grey to do, blue in progress, green done. Set by applyTheme.
var statusLozenge map[string]lipgloss.Style

// sectionHead opens a panel section: a shaded bar with its label and hint,
// or without shading a rule above the label.
func sectionHead(label, hint string, width int) string {
	head := refLabelStyle.Render(label) + refDimStyle.Render(hint)
	if shadeOn {
		return "\n" + bar(" "+head, max(width, 1)) + "\n"
	}
	return "\n" + refDimStyle.Render(strings.Repeat("─", max(width, 1))) + "\n" + head + "\n"
}

// renderJiraLinks lists the parent, linked issues and subtasks; L picks one
// to open.
func (m *Model) renderJiraLinks(b *strings.Builder, iss *jira.Issue, width int) {
	if len(iss.Links) == 0 {
		return
	}
	b.WriteString(sectionHead(fmt.Sprintf("Links (%d)", len(iss.Links)), "  L open", width))
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
	b.WriteString(sectionHead(fmt.Sprintf("Attachments (%d)", len(rest)), "", width))
	for _, a := range rest {
		name := attachmentStyle.Render("📎 " + a.Filename)
		if u := m.jiraClient.AttachmentURL(a.ID); u != "" {
			name = osc8Link(u, name)
		}
		b.WriteString(name + refDimStyle.Render("  "+byteSize(a.Size)) + "\n")
	}
}

// threadedComment is a comment's place in the thread: its index into the
// issue's comments and how deep a reply it is.
type threadedComment struct{ i, depth int }

// replyDepthMax caps the indent; deeper replies line up with it.
const replyDepthMax = 3

// commentThread orders comments as a thread: each top-level comment by
// date, its replies (parentId) under it, theirs under them. parentId is
// undocumented, so anything odd falls back to the flat list: a parent not
// among the loaded comments (older than the page, deleted) makes a comment
// top-level, and a loop is broken where it's found.
func commentThread(cs []jira.Comment) []threadedComment {
	at := map[string]int{}
	for i, c := range cs {
		if c.ID != "" {
			at[c.ID] = i
		}
	}
	kids := map[int][]int{}
	var roots []int
	for i, c := range cs {
		if p, ok := at[c.ParentID]; ok && c.ParentID != "" && p != i {
			kids[p] = append(kids[p], i)
		} else {
			roots = append(roots, i)
		}
	}
	out := make([]threadedComment, 0, len(cs))
	seen := make([]bool, len(cs))
	var walk func(i, depth int)
	walk = func(i, depth int) {
		if seen[i] {
			return
		}
		seen[i] = true
		out = append(out, threadedComment{i, depth})
		for _, k := range kids[i] {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	for i := range cs { // in a loop, unreachable from any root: flat, in order
		walk(i, 0)
	}
	return out
}

// indentReply puts a reply's lines behind a bar per level.
func indentReply(s string, depth int) string {
	if depth == 0 {
		return s
	}
	bar := refDimStyle.Render(strings.Repeat("│ ", min(depth, replyDepthMax)))
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = bar + l
	}
	return strings.Join(lines, "\n") + "\n"
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

// renderJiraComments writes the Comments tab: the thread oldest first (the
// order the API returns), each a dim author·timestamp line and its markdown
// body. When the issue has more comments than the inline field returned, a
// trailing note points at the browser.
func (m *Model) renderJiraComments(b *strings.Builder, iss *jira.Issue) {
	if len(iss.Comments) == 0 && iss.CommentTotal == 0 {
		b.WriteString(refDimStyle.Render("no comments yet") + "\n")
		return
	}
	thread := commentThread(iss.Comments)
	for n, tc := range thread {
		var cb strings.Builder
		m.renderComment(&cb, iss.Comments[tc.i])
		m.commentHeads = append(m.commentHeads, commentHead{i: tc.i,
			text: strings.Repeat("│ ", min(tc.depth, replyDepthMax)) + m.commentByline(iss.Comments[tc.i])})
		b.WriteString(indentReply(cb.String(), tc.depth))
		if n < len(thread)-1 {
			b.WriteString("\n")
		}
	}

	if extra := iss.CommentTotal - len(iss.Comments); extra > 0 {
		b.WriteString("\n" + refDimStyle.Render(fmt.Sprintf("…and %d more — o opens in browser", extra)) + "\n")
	}
}

// commentByline is a comment's first line: its author and time.
func (m *Model) commentByline(c jira.Comment) string {
	author := c.Author
	if author == "" {
		author = "Unknown"
	}
	if c.Created.IsZero() {
		return author
	}
	return author + " · " + m.when(c.Created)
}

// renderComment writes one comment: author and time, its body.
func (m *Model) renderComment(b *strings.Builder, c jira.Comment) {
	b.WriteString(refDimStyle.Render(m.commentByline(c)) + "\n")
	if body := strings.TrimSpace(c.Body); body != "" {
		b.WriteString(renderMarkdown(body, m.emojiImg, nil, ""))
	}
}
