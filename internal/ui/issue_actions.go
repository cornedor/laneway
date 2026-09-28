package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// A in the panel: what else can be done with the issue — a subtask (or, on
// an epic, a child), a link to another issue, a clone, watching it, its
// herdr agents (agents.go).

// jiraWatchMsg is a watch toggled.
type jiraWatchMsg struct {
	key string
	on  bool
	err error
}

// openIssueActions lists the actions for the panel issue.
func (m *Model) openIssueActions() {
	if m.jiraIssue == nil {
		return
	}
	iss := m.jiraIssue
	m.startJiraPicker(jiraPickIssueActions, iss.Key, false)
	items := []jiraPickerItem{{id: "subtask", label: "New subtask"}}
	if strings.EqualFold(iss.Type, "epic") {
		items = []jiraPickerItem{{id: "child", label: "New issue in this epic"}}
	}
	items = append(items,
		jiraPickerItem{id: "link", label: "Link to another issue"},
		jiraPickerItem{id: "weblink", label: "Add a web link"},
		jiraPickerItem{id: "estimate", label: "Set the original estimate"},
		jiraPickerItem{id: "status-time", label: "Time in each status"},
		jiraPickerItem{id: "deps", label: "Dependency tree: what holds it up"},
		jiraPickerItem{id: "clone", label: "Clone"},
		jiraPickerItem{id: "type", label: "Change the issue type"},
		jiraPickerItem{id: "move", label: "Move to another project"},
		jiraPickerItem{id: "delete", label: "Delete the issue"},
		jiraPickerItem{id: "watch", label: "Watch / stop watching"},
		jiraPickerItem{id: "watchers", label: "Add or remove watchers"},
		jiraPickerItem{id: "vote", label: "Vote / take back the vote"},
		jiraPickerItem{id: "flag", label: "Flag as an impediment / clear the flag"},
		jiraPickerItem{id: "upload", label: "Upload a file"},
		jiraPickerItem{id: "paste", label: "Upload the image on the clipboard"},
		jiraPickerItem{id: "screenshot", label: "Screenshot a region and attach it"},
	)
	if slices.ContainsFunc(iss.Links, func(l jira.Link) bool { return l.LinkID != "" }) {
		items = append(items, jiraPickerItem{id: "unlink", label: "Remove a link"})
	}
	items = append(items, m.agentActions(iss.Key)...)
	if m.canOpenPullRequest(iss.Key) {
		items = append(items, jiraPickerItem{id: "pr", label: "Open a pull request (draft)"})
	}
	if len(iss.Comments) > 0 {
		items = append(items, jiraPickerItem{id: "edit-comment", label: "Edit a comment of yours"},
			jiraPickerItem{id: "delete-comment", label: "Delete a comment of yours"})
	}
	if m.notes(iss.Key) != "" {
		items = append(items, jiraPickerItem{id: "post-notes", label: "Post your local notes as a comment"})
	}
	if len(iss.Attachments) > 0 {
		items = append(items, jiraPickerItem{id: "download", label: "Download an attachment"},
			jiraPickerItem{id: "delete-attachment", label: "Delete an attachment"})
	}
	m.setJiraPickerItems(items)
}

// applyIssueAction runs the picked action on key.
func (m *Model) applyIssueAction(key, id string) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	if what, pane, ok := strings.Cut(id, ":"); ok && strings.HasPrefix(what, "agent-") {
		return m.applyAgentAction(key, what, pane)
	}
	switch id {
	case "subtask", "child":
		return m.openJiraCreateChild(key, id)
	case "pr":
		return m.openPullRequest(key)
	case "status-time":
		return m.openStatusTime(key)
	case "deps":
		return m.openDependencies(key)
	case "link":
		gen := m.startJiraPicker(jiraPickLinkType, "Link "+key, true)
		m.jiraPicker.issueKey = key
		seq := m.jiraPicker.fetchSeq
		return func() tea.Msg {
			types, err := c.LinkTypes(ctx)
			var items []jiraPickerItem
			for _, t := range types {
				items = append(items, jiraPickerItem{id: "out|" + t.Name, label: key + " " + t.Outward + " …"})
				if t.Inward != t.Outward {
					items = append(items, jiraPickerItem{id: "in|" + t.Name, label: key + " " + t.Inward + " …"})
				}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickLinkType, items: items, err: err}
		}
	case "type":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		current, project := m.jiraIssue.Type, issueProject(key)
		gen := m.startJiraPicker(jiraPickChangeType, key+" is a "+current+": change to", false)
		m.jiraPicker.issueKey = key
		seq := m.jiraPicker.fetchSeq
		return func() tea.Msg {
			types, err := c.TypesLike(ctx, project, current)
			items := make([]jiraPickerItem, 0, len(types))
			for _, t := range types {
				items = append(items, jiraPickerItem{id: t.ID, label: jiraTypeIcon(t.Name) + " " + t.Name, value: t.Name})
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickChangeType, items: items, err: err}
		}
	case "watchers":
		gen := m.startJiraPicker(jiraPickWatchers, "Watchers of "+key+" · ↵ adds or removes", true)
		m.jiraPicker.issueKey = key
		return m.fetchWatchers(gen, m.jiraPicker.fetchSeq, key, "")
	case "delete":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		m.startJiraPicker(jiraPickDeleteIssue, "Delete "+key+"?", false)
		it := jiraPickerItem{id: key, label: "Delete " + key + "  " + m.jiraIssue.Summary}
		switch n := countSubtasks(m.jiraIssue.Links); {
		case n == 1:
			it.label, it.value = "Delete "+key+" and its subtask", "subtasks"
		case n > 1:
			it.label, it.value = fmt.Sprintf("Delete %s and its %d subtasks", key, n), "subtasks"
		}
		m.setJiraPickerItems([]jiraPickerItem{it})
	case "move":
		gen := m.startJiraPicker(jiraPickMoveProject, "Move "+key+" to", true)
		m.jiraPicker.issueKey = key
		seq, from := m.jiraPicker.fetchSeq, issueProject(key)
		return func() tea.Msg {
			ps, err := c.Projects(ctx) // the ones you can create in
			var items []jiraPickerItem
			for _, p := range ps {
				if p.Key != from {
					items = append(items, jiraPickerItem{id: p.Key, label: p.Key + "  " + p.Name})
				}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickMoveProject, items: items, err: err}
		}
	case "clone":
		return m.openJiraClone(key)
	case "estimate":
		m.openBulkInput("estimate", "2d 4h")
		m.jiraFieldKey = key
	case "weblink":
		m.openBulkInput("weblink", "https://… and a title")
		m.jiraFieldKey = key
	case "upload":
		m.openBulkInput("upload", "file path (~ works)")
		m.jiraFieldKey = key
	case "paste":
		m.status = "uploading the clipboard image to " + key + "…"
		name, command := time.Now().Format("pasted-20060102-150405.png"), m.opts.clipboardImage
		return jiraMutateCmd(key, "attachments", func() error {
			img, err := clipboardImage(command)
			if err != nil {
				return err
			}
			return c.UploadAttachmentFrom(ctx, key, name, bytes.NewReader(img))
		})
	case "screenshot":
		m.status = "pick a region to attach to " + key + "…"
		name := time.Now().Format("screenshot-20060102-150405.png")
		return jiraMutateCmd(key, "attachments", func() error {
			img, err := screenshot()
			if err != nil {
				return err
			}
			return c.UploadAttachmentFrom(ctx, key, name, bytes.NewReader(img))
		})
	case "download":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		m.startJiraPicker(jiraPickAttachment, "Download to "+downloadDir(strings.TrimSpace(m.uiConfig.DownloadDir)), true)
		var items []jiraPickerItem
		for _, a := range m.jiraIssue.Attachments {
			items = append(items, jiraPickerItem{id: a.ID + "/" + a.Filename, label: fmt.Sprintf("%s  %s", a.Filename, byteSize(a.Size))})
		}
		m.setJiraPickerItems(items)
	case "delete-attachment":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		m.startJiraPicker(jiraPickDeleteAttachment, "Delete an attachment from "+key, true)
		var items []jiraPickerItem
		for _, a := range m.jiraIssue.Attachments {
			items = append(items, jiraPickerItem{id: a.ID, label: fmt.Sprintf("%s  %s", a.Filename, byteSize(a.Size)), value: a.Filename})
		}
		m.setJiraPickerItems(items)
	case "unlink":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		m.startJiraPicker(jiraPickUnlink, "Remove a link from "+key, true)
		m.jiraPicker.issueKey = key
		var items []jiraPickerItem
		for _, l := range m.jiraIssue.Links {
			if l.LinkID != "" {
				items = append(items, jiraPickerItem{id: l.LinkID, label: l.Rel + " " + l.Key + " " + l.Summary})
			}
		}
		m.setJiraPickerItems(items)
	case "edit-comment":
		return m.openCommentPicker(jiraPickEditComment)
	case "delete-comment":
		return m.openCommentPicker(jiraPickDeleteComment)
	case "post-notes":
		m.postNotes()
		return nil
	case "flag":
		// The board's card tells the flag; an issue off it is asked.
		i := slices.IndexFunc(m.jiraTab.cards, func(cd jira.Card) bool { return cd.Key == key })
		onBoard := i >= 0 && m.jiraTab.cards[i].Flagged
		m.status = "setting the flag on " + key + "…"
		return func() tea.Msg {
			flagged := onBoard
			if i < 0 {
				var err error
				if flagged, err = c.Flagged(ctx, key); err != nil {
					return jiraMutatedMsg{key: key, field: "flag", err: err}
				}
			}
			what := "flagged"
			if flagged {
				what = "flag cleared"
			}
			return jiraMutatedMsg{key: key, field: what, err: c.SetFlagged(ctx, key, !flagged)}
		}
	case "vote":
		return func() tea.Msg {
			on, err := c.ToggleVote(ctx, key)
			return jiraVoteMsg{key: key, on: on, err: err}
		}
	case "watch":
		return func() tea.Msg {
			on, err := c.ToggleWatch(ctx, key)
			return jiraWatchMsg{key: key, on: on, err: err}
		}
	}
	return nil
}

// openMoveTypes asks which of project's issue types key becomes, its own
// first.
func (m *Model) openMoveTypes(key, project string) tea.Cmd {
	if m.jiraIssue == nil || m.jiraIssue.Key != key {
		return nil
	}
	current := m.jiraIssue.Type
	gen := m.startJiraPicker(jiraPickMoveType, "Move "+key+" to "+project+" as", false)
	m.jiraPicker.issueKey = key
	seq, c, ctx := m.jiraPicker.fetchSeq, m.jiraClient, m.ctx
	return func() tea.Msg {
		types, err := c.MoveTypes(ctx, issueProject(key), current, project)
		var items []jiraPickerItem
		for _, t := range types {
			items = append(items, jiraPickerItem{id: project + "," + t.ID, label: jiraTypeIcon(t.Name) + " " + t.Name,
				focus: strings.EqualFold(t.Name, current)})
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickMoveType, items: items, err: err}
	}
}

// fetchWatchers lists key's watchers (✓) matching query, then the people
// who can see it and don't watch it yet.
func (m *Model) fetchWatchers(gen, seq int, key, query string) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	return func() tea.Msg {
		watchers, err := c.Watchers(ctx, key)
		if err != nil {
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickWatchers, err: err}
		}
		others, err := c.ViewUsers(ctx, key, query)
		q := strings.ToLower(strings.TrimSpace(query))
		var items []jiraPickerItem
		for _, u := range watchers {
			if strings.Contains(strings.ToLower(u.DisplayName), q) {
				items = append(items, jiraPickerItem{id: u.AccountID, label: u.DisplayName, current: true})
			}
		}
		for _, u := range others {
			if !slices.ContainsFunc(watchers, func(w jira.User) bool { return w.AccountID == u.AccountID }) {
				items = append(items, jiraPickerItem{id: u.AccountID, label: u.DisplayName})
			}
		}
		return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickWatchers, items: items, err: err}
	}
}

// countSubtasks is how many of links are subtasks.
func countSubtasks(links []jira.Link) int {
	n := 0
	for _, l := range links {
		if l.Rel == "subtask" {
			n++
		}
	}
	return n
}

// jiraDeletedMsg is key deleted.
type jiraDeletedMsg struct {
	key string
	err error
}

// deleteIssue deletes key, its subtasks too when subtasks is set.
func (m *Model) deleteIssue(key string, subtasks bool) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	m.status = "deleting " + key + "…"
	return func() tea.Msg { return jiraDeletedMsg{key: key, err: c.DeleteIssue(ctx, key, subtasks)} }
}

// handleJiraDeleted closes the panel on the deleted issue and reloads the
// board without it.
func (m Model) handleJiraDeleted(msg jiraDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + " not deleted: " + msg.err.Error())
		return m, nil
	}
	if r := m.currentRef(); r != nil && r.jiraKey == msg.key {
		m.closeRef()
	}
	m.status = "deleted " + msg.key
	return m, m.refreshJiraAfterEdit()
}

// jiraRelocatedMsg is key moved to another project, now next.
type jiraRelocatedMsg struct {
	key, next string
	err       error
}

// moveIssue moves key to the picked project and type.
func (m *Model) moveIssue(key string, it jiraPickerItem) tea.Cmd {
	project, typeID, _ := strings.Cut(it.id, ",")
	c, ctx := m.jiraClient, m.ctx
	m.status = "moving " + key + " to " + project + "…"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, c.Scaled(2*time.Minute))
		defer cancel()
		next, err := c.MoveIssue(ctx, key, project, typeID)
		return jiraRelocatedMsg{key: key, next: next, err: err}
	}
}

// handleJiraRelocated opens the moved issue under its new key.
func (m Model) handleJiraRelocated(msg jiraRelocatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail(msg.key + " not moved: " + msg.err.Error())
		return m, nil
	}
	board := m.refreshJiraAfterEdit()
	out, cmd := m.openJiraKey(msg.next)
	m = out.(Model)
	m.status = "moved " + msg.key + " to " + msg.next
	return m, tea.Batch(cmd, board)
}

// applyEstimate sets the typed original estimate on the panel issue.
func (m Model) applyEstimate(raw string) (tea.Model, tea.Cmd) {
	key, raw := m.jiraFieldKey, strings.TrimSpace(raw)
	if secs, extra, err := jira.ParseDuration(raw); err != nil || secs == 0 || extra != "" {
		m.status = raw + " is not a time: 2d 4h, 90m"
		return m, nil
	}
	m.closeJiraField()
	c, ctx := m.jiraClient, m.ctx
	m.status = "estimating " + key + " at " + raw + "…"
	return m, jiraMutateCmd(key, "estimate", func() error { return c.SetEstimate(ctx, key, raw) })
}

// applyWebLink adds the typed "URL title" to the panel issue.
func (m Model) applyWebLink(raw string) (tea.Model, tea.Cmd) {
	key := m.jiraFieldKey
	u, title, _ := strings.Cut(strings.TrimSpace(raw), " ")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		m.status = "a web link starts with http:// or https://"
		return m, nil
	}
	m.closeJiraField()
	c, ctx := m.jiraClient, m.ctx
	m.status = "linking " + key + " to " + u + "…"
	return m, jiraMutateCmd(key, "links", func() error { return c.AddWebLink(ctx, key, u, strings.TrimSpace(title)) })
}

// openLinkTarget asks which issue the picked link goes to.
func (m *Model) openLinkTarget(key string, it jiraPickerItem) {
	m.openBulkInput("link", "a key, a number in "+issueProject(key)+", or words to search")
	m.jiraFieldKey = key
	m.jiraLinkChoice = it
	m.linkFind = linkFind{seq: m.linkFind.seq + 1}
}

// applyLink links the panel issue and the typed key.
func (m Model) applyLink(raw string) (tea.Model, tea.Cmd) {
	key := m.jiraFieldKey
	target, ok := m.linkTarget()
	if !ok {
		switch {
		case strings.TrimSpace(raw) == "":
			m.status = "type a key or words to find the issue"
		case m.linkFind.loading:
			m.status = "still looking…"
		default:
			m.status = "no issue to link: " + raw
		}
		return m, nil
	}
	dir, typ, _ := strings.Cut(m.jiraLinkChoice.id, "|")
	out, in := key, target
	if dir == "in" {
		out, in = target, key
	}
	m.closeJiraField()
	c, ctx := m.jiraClient, m.ctx
	m.status = "linking " + key + " and " + target + "…"
	return m, jiraMutateCmd(key, "links", func() error { return c.LinkIssues(ctx, typ, out, in) })
}

// jiraVoteMsg is a vote toggled.
type jiraVoteMsg struct {
	key string
	on  bool
	err error
}

func (m Model) handleJiraVote(msg jiraVoteMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.fail(msg.key + " vote: " + msg.err.Error())
	case msg.on:
		m.status = "voted for " + msg.key
	default:
		m.status = "took back the vote on " + msg.key
	}
	return m, nil
}

// unlinkJira removes the picked link from key.
func (m *Model) unlinkJira(key string, it jiraPickerItem) tea.Cmd {
	c, ctx, id := m.jiraClient, m.ctx, it.id
	m.status = "removing link " + it.label + "…"
	return jiraMutateCmd(key, "links", func() error { return c.DeleteLink(ctx, key, id) })
}

func (m Model) handleJiraWatch(msg jiraWatchMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.fail(msg.key + " watch: " + msg.err.Error())
	case msg.on:
		m.status = "watching " + msg.key
	default:
		m.status = "stopped watching " + msg.key
	}
	return m, nil
}

// jiraDownloadedMsg is an attachment saved, or why not.
type jiraDownloadedMsg struct {
	path string
	err  error
}

// downloadDir is where attachments are saved: ui.download_dir (~ is your
// home), else $XDG_DOWNLOAD_DIR, else ~/Downloads.
func downloadDir(dir string) string {
	home, _ := os.UserHomeDir()
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return filepath.Join(home, rest)
	}
	if dir != "" {
		return dir
	}
	if d := os.Getenv("XDG_DOWNLOAD_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, "Downloads")
}

// downloadAttachment saves the picked "id/name" to the download dir.
func (m *Model) downloadAttachment(pick string) tea.Cmd {
	id, name, _ := strings.Cut(pick, "/")
	c, ctx, dir := m.jiraClient, m.ctx, downloadDir(strings.TrimSpace(m.uiConfig.DownloadDir))
	m.status = "downloading " + name + "…"
	return func() tea.Msg {
		path, err := c.DownloadAttachment(ctx, id, name, dir)
		return jiraDownloadedMsg{path: path, err: err}
	}
}

func (m Model) handleJiraDownloaded(msg jiraDownloadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.fail("download: " + msg.err.Error())
	} else {
		m.lastDownload = msg.path
		m.status = "saved " + msg.path + " · " + helpKey(m.keys.Palette) + " open download"
	}
	return m, nil
}

// applyUpload attaches the typed file to the panel issue.
func (m Model) applyUpload(raw string) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(raw)
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, rest)
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		m.status = "no such file: " + raw
		return m, nil
	}
	key, c, ctx := m.jiraFieldKey, m.jiraClient, m.ctx
	m.closeJiraField()
	m.status = "uploading " + filepath.Base(path) + " to " + key + "…"
	return m, jiraMutateCmd(key, "attachments", func() error { return c.UploadAttachment(ctx, key, path) })
}

// issueProject is the project part of an issue key.
func issueProject(key string) string {
	if i := strings.LastIndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return key
}

// completePath extends in to the longest prefix every match shares (a
// lone directory gets its /) and returns the matches' names.
func completePath(in string) (string, []string) {
	dir, base := filepath.Split(in)
	read := dir
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		home, _ := os.UserHomeDir()
		read = filepath.Join(home, rest)
	}
	if read == "" {
		read = "."
	}
	entries, err := os.ReadDir(read)
	if err != nil {
		return in, nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), base) && (strings.HasPrefix(base, ".") || !strings.HasPrefix(e.Name(), ".")) {
			n := e.Name()
			if e.IsDir() {
				n += "/"
			}
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return in, nil
	}
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	return dir + common, names
}
