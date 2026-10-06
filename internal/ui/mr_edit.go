package ui

import (
	"cmp"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// e on a merge request in the panel edits it: its title, draft or ready,
// reviewers and assignees (the project's members), labels (the project's),
// target branch, and description in $EDITOR. M merges it, in the panel and
// the diff: a list of how, GitLab's defaults first; enter merges.

// mrWroteMsg is an edit or a merge written, or why not.
type mrWroteMsg struct {
	what string
	err  error
}

// mrDescEditedMsg is $EDITOR closed on the description's file.
type mrDescEditedMsg struct {
	c            *gitlab.Client
	ref          forge.Ref
	path, before string
	err          error
}

// mrLabel is a merge request as GitLab writes it: group/project!12.
func mrLabel(r forge.Ref) string { return r.Repo + "!" + strconv.Itoa(r.Number) }

// openMREdit lists what e can change, each with its value now.
func (m *Model) openMREdit() {
	p := m.mr
	mr := p.mr
	m.startJiraPicker(jiraPickMREdit, "Edit "+mrLabel(p.ref), false)
	now := func(v string) string { return refDimStyle.Render("  " + cmp.Or(v, "none")) }
	draft := jiraPickerItem{id: "draft", label: "Mark as draft"}
	if mr.Draft {
		draft.label = "Mark as ready"
	}
	m.setJiraPickerItems([]jiraPickerItem{
		{id: "title", label: "Title" + now(mr.Title)},
		draft,
		{id: "reviewers", label: "Reviewers" + now(strings.Join(mr.Reviewers, ", "))},
		{id: "assignees", label: "Assignees" + now(strings.Join(mr.Assignees, ", "))},
		{id: "labels", label: "Labels" + now(strings.Join(mr.Labels, ", "))},
		{id: "target", label: "Target branch" + now(mr.TargetBranch)},
		{id: "description", label: "Description, in $EDITOR"},
	})
}

// applyMRPick is enter in one of the merge request's lists.
func (m Model) applyMRPick(kind jiraPickerKind, it jiraPickerItem) (Model, tea.Cmd) {
	p := m.mr
	if p == nil || p.mr == nil {
		m.closeJiraPicker()
		return m, nil
	}
	switch kind {
	case jiraPickMRMerge:
		m.closeJiraPicker()
		i, _ := strconv.Atoi(it.id)
		return m, m.mergeMR(MergeChoices(p.mr)[i].MergeOptions)
	case jiraPickMRPeople, jiraPickMRLabels:
		ids := it.id // one pick: that one, "" nobody
		if m.jiraPicker.checked != nil {
			if !m.jiraPicker.ticked { // enter on a row: it alone changes
				m.toggleChecked()
			}
			ids, _ = m.jiraPicker.checkedPick()
		}
		field := m.jiraPicker.issueKey
		m.closeJiraPicker()
		var e gitlab.Edit
		var vals []string
		if ids != "" {
			vals = strings.Split(ids, ",")
		}
		if kind == jiraPickMRLabels {
			e.Labels = &vals
		} else {
			nums := []int{}
			for _, v := range vals {
				n, _ := strconv.Atoi(v)
				nums = append(nums, n)
			}
			if field == "reviewers" {
				e.ReviewerIDs = &nums
			} else {
				e.AssigneeIDs = &nums
			}
		}
		return m, m.updateMR(e, field+" changed")
	}
	m.closeJiraPicker()
	mr := p.mr
	switch it.id {
	case "title":
		m.openMRTextInput("mr-title", mr.Title)
	case "target":
		m.openMRTextInput("mr-target", mr.TargetBranch)
	case "draft":
		title, what := gitlab.DraftTitle(mr.Title, !mr.Draft), "marked as draft"
		if mr.Draft {
			what = "marked as ready"
		}
		return m, m.updateMR(gitlab.Edit{Title: &title}, what)
	case "reviewers", "assignees":
		return m, m.openMRPeople(it.id)
	case "labels":
		return m, m.openMRLabels()
	case "description":
		return m.editMRDescription()
	}
	return m, nil
}

// openMRPeople ticks field's people (reviewers or assignees) among the
// project's members; one of them, with Nobody, on a GitLab that takes one.
func (m *Model) openMRPeople(field string) tea.Cmd {
	p := m.mr
	names, ids := p.mr.Reviewers, p.mr.ReviewerIDs
	if field == "assignees" {
		names, ids = p.mr.Assignees, p.mr.AssigneeIDs
	}
	gen := m.startJiraPicker(jiraPickMRPeople, strings.ToUpper(field[:1])+field[1:]+" of "+mrLabel(p.ref), true)
	m.jiraPicker.issueKey = field // what the ticks are
	m.jiraPicker.checked = map[string]string{}
	for i, id := range ids {
		if i < len(names) {
			m.jiraPicker.checked[strconv.Itoa(id)] = names[i]
		}
	}
	c, ctx, ref := p.c, m.ctx, p.ref
	return func() tea.Msg {
		ms, err := c.Members(ctx, ref.Repo)
		single := false
		if a, r, merr := c.Multiple(ctx, ref.Repo, ref.Number); merr == nil { // unknown: several, as GitLab's paid tiers
			single = field == "assignees" && !a || field == "reviewers" && !r
		}
		var items []jiraPickerItem
		if single {
			items = append(items, jiraPickerItem{id: "", label: "Nobody", current: len(ids) == 0})
		}
		for _, mb := range ms {
			items = append(items, jiraPickerItem{id: strconv.Itoa(mb.ID), label: mb.Name, search: mb.Username, current: slices.Contains(ids, mb.ID)})
		}
		for i, id := range ids { // someone not a member any more stays listed
			if i < len(names) && !slices.ContainsFunc(ms, func(mb gitlab.Member) bool { return mb.ID == id }) {
				items = append(items, jiraPickerItem{id: strconv.Itoa(id), label: names[i], current: true})
			}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: 1, kind: jiraPickMRPeople, items: items, err: err, single: single}
	}
}

// openMRLabels ticks the merge request's labels among the project's.
func (m *Model) openMRLabels() tea.Cmd {
	p := m.mr
	gen := m.startJiraPicker(jiraPickMRLabels, "Labels of "+mrLabel(p.ref), true)
	m.jiraPicker.issueKey = "labels"
	m.jiraPicker.checked = map[string]string{}
	for _, l := range p.mr.Labels {
		m.jiraPicker.checked[l] = l
	}
	c, ctx, repo, have := p.c, m.ctx, p.ref.Repo, p.mr.Labels
	return func() tea.Msg {
		ls, err := c.Labels(ctx, repo)
		var items []jiraPickerItem
		for _, l := range ls {
			items = append(items, jiraPickerItem{id: l, label: l})
		}
		for _, l := range have {
			if !slices.Contains(ls, l) {
				items = append(items, jiraPickerItem{id: l, label: l})
			}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: 1, kind: jiraPickMRLabels, items: items, err: err}
	}
}

// openMRTextInput opens the field input on the panel's merge request's
// title or target branch.
func (m *Model) openMRTextInput(field, value string) {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.SetWidth(max(min(m.width-16, 72), 16))
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Focus()
	m.jiraFieldInput = ti
	m.jiraFieldActive = true
	m.jiraFieldName = field
	m.jiraFieldKey = mrLabel(m.mr.ref)
}

// applyMRText writes the title or target branch typed.
func (m Model) applyMRText(field, raw string) (Model, tea.Cmd) {
	raw = strings.TrimSpace(raw)
	m.closeJiraField()
	p := m.mr
	if p == nil || p.mr == nil {
		return m, nil
	}
	switch {
	case raw == "":
		m.status = "it can't be empty"
		return m, nil
	case field == "mr-title" && raw != p.mr.Title:
		return m, m.updateMR(gitlab.Edit{Title: &raw}, "title changed")
	case field == "mr-target" && raw != p.mr.TargetBranch:
		return m, m.updateMR(gitlab.Edit{TargetBranch: &raw}, "target branch changed")
	}
	return m, nil
}

// editMRDescription hands the description to $VISUAL / $EDITOR.
func (m Model) editMRDescription() (Model, tea.Cmd) {
	p := m.mr
	f, err := os.CreateTemp("", "laneway-mr-*.md")
	if err == nil {
		_, err = f.WriteString(p.mr.Description + "\n")
		err = firstErr(err, f.Close())
	}
	if err != nil {
		m.fail("description: " + err.Error())
		return m, nil
	}
	c, ref, path, before := p.c, p.ref, f.Name(), p.mr.Description
	m.status = "editing " + mrLabel(ref) + " description…"
	return m, tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
		return mrDescEditedMsg{c: c, ref: ref, path: path, before: before, err: err}
	})
}

// handleMRDescEdited writes the description back when it changed; the file
// goes once GitLab has it.
func (m Model) handleMRDescEdited(msg mrDescEditedMsg) (tea.Model, tea.Cmd) {
	b, err := os.ReadFile(msg.path)
	if err = firstErr(msg.err, err); err != nil {
		m.fail("description: " + err.Error())
		return m, nil
	}
	text := strings.TrimSpace(string(b))
	if text == strings.TrimSpace(msg.before) {
		os.Remove(msg.path)
		m.status = mrLabel(msg.ref) + " unchanged"
		return m, nil
	}
	ctx, c, ref, path := m.ctx, msg.c, msg.ref, msg.path
	m.status = "saving the description…"
	return m, func() tea.Msg {
		err := c.Update(ctx, ref.Repo, ref.Number, gitlab.Edit{Description: &text})
		if err == nil {
			os.Remove(path)
		} else {
			err = &keptErr{err: err, path: path}
		}
		return mrWroteMsg{what: "description saved", err: err}
	}
}

// keptErr is a write that failed, its text kept in path.
type keptErr struct {
	err  error
	path string
}

func (e *keptErr) Error() string { return e.err.Error() + " — your text is in " + e.path }

// updateMR writes e to the panel's merge request; what is the status after.
func (m *Model) updateMR(e gitlab.Edit, what string) tea.Cmd {
	p := m.mr
	c, ctx, ref := p.c, m.ctx, p.ref
	m.status = "saving " + mrLabel(ref) + "…"
	return func() tea.Msg {
		return mrWroteMsg{what: what, err: c.Update(ctx, ref.Repo, ref.Number, e)}
	}
}

// MergeChoice is one way to merge: its options and its row. The first of
// MergeChoices is GitLab's default.
type MergeChoice struct {
	forge.MergeOptions
	Label string
}

// MergeChoices are the ways to merge mr: GitLab's defaults for it first.
func MergeChoices(mr *forge.Change) []MergeChoice {
	def := forge.MergeOptions{Squash: mr.Squash, DeleteBranch: mr.DeleteBranch}
	out := []MergeChoice{}
	for _, o := range []forge.MergeOptions{def, {Squash: !def.Squash, DeleteBranch: def.DeleteBranch},
		{Squash: def.Squash, DeleteBranch: !def.DeleteBranch}, {Squash: !def.Squash, DeleteBranch: !def.DeleteBranch}} {
		how := "Its commits"
		if o.Squash {
			how = "Squashed"
		}
		branch := ", keep the branch"
		if o.DeleteBranch {
			branch = ", delete the branch"
		}
		out = append(out, MergeChoice{MergeOptions: o, Label: how + branch})
	}
	return out
}

// mergeRows are MergeChoices as a list's rows, the default marked.
func mergeRows(mr *forge.Change) []string {
	var rows []string
	for i, c := range MergeChoices(mr) {
		if i == 0 {
			c.Label += refDimStyle.Render("  GitLab's default")
		}
		rows = append(rows, c.Label)
	}
	return rows
}

// MergeTitle is the question over MergeChoices.
func MergeTitle(mr *forge.Change) string {
	return "Merge " + mr.SourceBranch + " into " + mr.TargetBranch + "?"
}

// MergeReady is why mr can't be merged now, "" when it can.
func MergeReady(mr *forge.Change) string {
	switch {
	case mr.State != forge.StateOpen:
		return "it is " + mrState(mr)
	case !mr.Mergeable:
		return "not ready to merge: " + mr.MergeStatus
	}
	return ""
}

// openMRMerge asks how to merge the panel's merge request.
func (m *Model) openMRMerge() {
	p := m.mr
	if why := MergeReady(p.mr); why != "" {
		m.status = why
		return
	}
	m.startJiraPicker(jiraPickMRMerge, MergeTitle(p.mr), false)
	var items []jiraPickerItem
	for i, row := range mergeRows(p.mr) {
		items = append(items, jiraPickerItem{id: strconv.Itoa(i), label: row})
	}
	m.setJiraPickerItems(items)
}

// mergeMR merges the panel's merge request as o says.
func (m *Model) mergeMR(o forge.MergeOptions) tea.Cmd {
	p := m.mr
	c, ctx, ref := p.c, m.ctx, p.ref
	m.status = "merging " + mrLabel(ref) + "…"
	return func() tea.Msg {
		return mrWroteMsg{what: "merged " + mrLabel(ref), err: c.Merge(ctx, ref.Repo, ref.Number, o)}
	}
}

// handleMRWrote reads the merge request again after a write.
func (m Model) handleMRWrote(msg mrWroteMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.err.Error())
		return m, nil
	}
	m.status = msg.what
	p := m.mr
	if p == nil || p.c == nil {
		return m, nil
	}
	cmd := m.openMR(p.c, p.ref, p.link, p.title, true)
	m.status = msg.what
	return m, cmd
}

// --- the diff's M ----------------------------------------------------------

// mergeLines are the diff's M list, the cursor's row marked.
func (d *diffState) mergeLines(width int) []string {
	lines := []string{refKeyStyle.Render(d.mergeTitle) + refDimStyle.Render("  ↵ merge · esc close")}
	for i, row := range d.merges {
		line := truncate("  "+row, width)
		if i == d.mergeAt {
			line = selectedRow.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return lines
}

// openDiffMerge opens the diff's M list.
func (m Model) openDiffMerge() (tea.Model, tea.Cmd) {
	if m.mr == nil || m.mr.mr == nil {
		return m, nil
	}
	if why := MergeReady(m.mr.mr); why != "" {
		m.status = why
		return m, nil
	}
	d := m.diff
	d.merges, d.mergeAt, d.mergeTitle = mergeRows(m.mr.mr), 0, MergeTitle(m.mr.mr)
	return m, nil
}

// handleDiffMergeKey is the diff's M list: enter merges.
func (m Model) handleDiffMergeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q", "M":
		d.merges = nil
	case "up", "k":
		d.mergeAt = max(d.mergeAt-1, 0)
	case "down", "j":
		d.mergeAt = min(d.mergeAt+1, len(d.merges)-1)
	case "enter":
		o := MergeChoices(m.mr.mr)[d.mergeAt].MergeOptions
		d.merges = nil
		return m, m.mergeMR(o)
	}
	return m, nil
}
