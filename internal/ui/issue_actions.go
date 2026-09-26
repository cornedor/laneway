package ui

import (
	"bytes"
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
// an epic, a child), a link to another issue, a clone, watching it.

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
		jiraPickerItem{id: "clone", label: "Clone"},
		jiraPickerItem{id: "watch", label: "Watch / stop watching"},
		jiraPickerItem{id: "vote", label: "Vote / take back the vote"},
		jiraPickerItem{id: "flag", label: "Flag as an impediment / clear the flag"},
		jiraPickerItem{id: "upload", label: "Upload a file"},
		jiraPickerItem{id: "paste", label: "Upload the image on the clipboard"},
	)
	if slices.ContainsFunc(iss.Links, func(l jira.Link) bool { return l.LinkID != "" }) {
		items = append(items, jiraPickerItem{id: "unlink", label: "Remove a link"})
	}
	if len(iss.Comments) > 0 {
		items = append(items, jiraPickerItem{id: "edit-comment", label: "Edit a comment of yours"})
	}
	if len(iss.Attachments) > 0 {
		items = append(items, jiraPickerItem{id: "download", label: "Download an attachment"})
	}
	m.setJiraPickerItems(items)
}

// applyIssueAction runs the picked action on key.
func (m *Model) applyIssueAction(key, id string) tea.Cmd {
	c, ctx := m.jiraClient, m.ctx
	switch id {
	case "subtask", "child":
		project := issueProject(key)
		gen := m.startJiraPicker(jiraPickCreateType, "New "+id+" of "+key, false)
		m.jiraCreateParent, m.jiraCreateProject = key, project
		seq := m.jiraPicker.fetchSeq
		return func() tea.Msg {
			types, err := c.IssueTypes(ctx, project)
			if id == "subtask" {
				types, err = c.SubtaskTypes(ctx, project)
			}
			items := make([]jiraPickerItem, 0, len(types))
			for _, t := range types {
				if !strings.EqualFold(t.Name, "epic") {
					items = append(items, jiraPickerItem{id: t.Name, label: jiraTypeIcon(t.Name) + " " + t.Name})
				}
			}
			return jiraPickerLoadedMsg{gen: gen, seq: seq, kind: jiraPickCreateType, items: items, err: err}
		}
	case "link":
		gen := m.startJiraPicker(jiraPickLinkType, "Link "+key, false)
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
	case "clone":
		m.status = "cloning " + key + "…"
		return func() tea.Msg {
			nk, err := c.Clone(ctx, key)
			return jiraCreatedMsg{key: nk, err: err}
		}
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
	case "download":
		if m.jiraIssue == nil || m.jiraIssue.Key != key {
			return nil
		}
		m.startJiraPicker(jiraPickAttachment, "Download to "+downloadDir(), true)
		var items []jiraPickerItem
		for _, a := range m.jiraIssue.Attachments {
			items = append(items, jiraPickerItem{id: a.ID + "/" + a.Filename, label: fmt.Sprintf("%s  %s", a.Filename, byteSize(a.Size))})
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
		return m.openCommentPicker()
	case "flag":
		on := true
		if i := slices.IndexFunc(m.jiraTab.cards, func(cd jira.Card) bool { return cd.Key == key }); i >= 0 {
			on = !m.jiraTab.cards[i].Flagged
		}
		what := "flagged"
		if !on {
			what = "flag cleared"
		}
		m.status = "setting the flag on " + key + "…"
		return jiraMutateCmd(key, what, func() error { return c.SetFlagged(ctx, key, on) })
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

// openLinkTarget asks which issue the picked link goes to.
func (m *Model) openLinkTarget(key string, it jiraPickerItem) {
	m.openBulkInput("link", "issue key, or a number in "+issueProject(key))
	m.jiraFieldKey = key
	m.jiraLinkChoice = it
}

// applyLink links the panel issue and the typed key.
func (m Model) applyLink(raw string) (tea.Model, tea.Cmd) {
	key := m.jiraFieldKey
	target := jiraGotoKey(raw, issueProject(key))
	if target == "" || target == key {
		m.status = "not an issue key: " + raw
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
		m.status = msg.key + " vote: " + msg.err.Error()
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
		m.status = msg.key + " watch: " + msg.err.Error()
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

// downloadDir is where attachments are saved: $XDG_DOWNLOAD_DIR, else
// ~/Downloads.
func downloadDir() string {
	if d := os.Getenv("XDG_DOWNLOAD_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// downloadAttachment saves the picked "id/name" to the download dir.
func (m *Model) downloadAttachment(pick string) tea.Cmd {
	id, name, _ := strings.Cut(pick, "/")
	c, ctx, dir := m.jiraClient, m.ctx, downloadDir()
	m.status = "downloading " + name + "…"
	return func() tea.Msg {
		path, err := c.DownloadAttachment(ctx, id, name, dir)
		return jiraDownloadedMsg{path: path, err: err}
	}
}

func (m Model) handleJiraDownloaded(msg jiraDownloadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "download: " + msg.err.Error()
	} else {
		m.status = "saved " + msg.path
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
