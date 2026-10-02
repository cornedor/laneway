package ui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// enter on a GitLab merge request in D reads it in the panel, in place of
// the issue: its state, branches, pipeline by stage, approvals, reviewers
// and description. o opens it in GitLab, r refetches, esc or backspace go
// back to the issue. A link no GitLab instance has a token for opens in the
// browser, as before.

// panelMR is the merge request the panel shows over its issue.
type panelMR struct {
	link, title string
	c           *gitlab.Client
	ref         forge.Ref
	mr          *forge.Change
	threads     []forge.Thread
	loading     bool
	err         string
	gen         int
}

// mrMsg is a merge request read.
type mrMsg struct {
	gen     int
	mr      *forge.Change
	threads []forge.Thread
	err     error
}

// gitlabMR is the client and reference for a merge request link, ok false
// for one no GitLab instance with a token has.
func (m *Model) gitlabMR(link string) (*gitlab.Client, forge.Ref, bool) {
	if m.gitlab == nil {
		return nil, forge.Ref{}, false
	}
	c := m.gitlab.For(link)
	if c == nil {
		return nil, forge.Ref{}, false
	}
	r, ok := c.Parse(link)
	return c, r, ok
}

// openMR shows the merge request at link in the panel; fresh refetches
// past the client's cache.
func (m *Model) openMR(c *gitlab.Client, r forge.Ref, link, title string, fresh bool) tea.Cmd {
	m.mrGen++
	m.mr = &panelMR{link: link, title: title, c: c, ref: r, loading: true, gen: m.mrGen}
	m.refView.GotoTop()
	m.renderRef()
	m.status = helpKey(m.keys.OpenAttach) + " GitLab · " + helpKey(m.keys.Refresh) + " reload · esc back to the issue"
	ctx, gen := m.ctx, m.mrGen
	if fresh {
		c.Invalidate(r.Repo, r.Number)
	}
	return func() tea.Msg {
		mr, err := c.Get(ctx, r.Repo, r.Number)
		var threads []forge.Thread
		if err == nil {
			threads, _ = c.Threads(ctx, r.Repo, r.Number) // best-effort: the counts
		}
		return mrMsg{gen: gen, mr: mr, threads: threads, err: err}
	}
}

// showMR opens the panel on the merge request alone, no issue under it
// (the merge requests screen).
func (m *Model) showMR(c *gitlab.Client, r forge.Ref, link, title string) tea.Cmd {
	m.closeAgentPanel()
	m.refOpen, m.refs, m.refIdx, m.refBack = true, nil, 0, nil
	m.jiraIssue = nil
	m.focus = focusRef
	m.resize()
	return m.openMR(c, r, link, title, false)
}

func (m Model) handleMR(msg mrMsg) (tea.Model, tea.Cmd) {
	if m.mr == nil || m.mr.gen != msg.gen {
		return m, nil
	}
	m.mr.loading = false
	if msg.err != nil {
		m.mr.err = msg.err.Error()
	} else {
		m.mr.mr, m.mr.threads = msg.mr, msg.threads
		MRSeen(m.store, msg.mr)
	}
	m.renderRef()
	return m, nil
}

// checkGlyph is a job or pipeline status as a mark, coloured.
func checkGlyph(status string) string {
	switch status {
	case forge.StatusSuccess:
		return laneMark["done"].Render("✓")
	case forge.StatusFailed:
		return refErrStyle.Render("✗")
	case forge.StatusRunning:
		return laneMark["indeterminate"].Render("●")
	case forge.StatusPending:
		return laneMark["indeterminate"].Render("○")
	case forge.StatusWarning:
		return mdPanelStyles["warning"].Render("!")
	case forge.StatusManual:
		return refDimStyle.Render("▶")
	case forge.StatusCanceled:
		return refDimStyle.Render("⊘")
	}
	return refDimStyle.Render("»") // skipped
}

// renderMR is the merge request as the panel draws it, w wide.
func (m *Model) renderMR(w int) string {
	p := m.mr
	var b strings.Builder
	title := p.title
	if p.mr != nil {
		title = p.mr.Title
	}
	b.WriteString(refKeyStyle.Render(title) + "\n")
	b.WriteString(refDimStyle.Render("GitLab "+p.ref.Repo+"!"+strconv.Itoa(p.ref.Number)+" · d diff and review · A approve · "+helpKey(m.keys.OpenAttach)+" opens it · esc back") + "\n\n")
	switch {
	case p.err != "":
		b.WriteString(refErrStyle.Render(p.err))
	case p.loading:
		b.WriteString(refDimStyle.Render("loading the merge request…"))
	default:
		b.WriteString(m.mrBody(p.mr))
	}
	return wrapPanel(expandTables(b.String(), w), w)
}

func (m *Model) mrBody(mr *forge.Change) string {
	var b strings.Builder
	row := func(label, v string) {
		if v != "" {
			b.WriteString(refLabelStyle.Render(fmt.Sprintf("%-10s", label)) + " " + v + "\n")
		}
	}
	state := mr.State
	if mr.Draft {
		state = "draft"
	}
	if !mr.UpdatedAt.IsZero() {
		state += refDimStyle.Render(" · updated " + age(mr.UpdatedAt) + " ago")
	}
	row("State", state)
	row("Branches", mr.SourceBranch+" → "+mr.TargetBranch)
	if mr.ChangesCount != "" {
		row("Changes", mr.ChangesCount+" files")
	}
	merge := mr.MergeStatus
	if mr.HasConflicts {
		merge = refErrStyle.Render(merge)
	}
	if mr.State == forge.StateOpen {
		row("Merge", merge)
	}
	row("Author", mr.Author)
	row("Assignees", strings.Join(mr.Assignees, ", "))
	row("Reviewers", strings.Join(mr.Reviewers, ", "))
	if a := mr.Approvals; a != nil && (a.Approved || a.Required > 0 || len(a.By) > 0) { // none: no rules, no one
		v := "none"
		switch {
		case a.Required > 0:
			v = fmt.Sprintf("%d of %d", a.Required-a.Left, a.Required)
		case len(a.By) > 0:
			v = strconv.Itoa(len(a.By))
		}
		if a.Approved {
			v = laneMark["done"].Render("approved") + " · " + v
		}
		if len(a.By) > 0 {
			v += refDimStyle.Render(" · " + strings.Join(a.By, ", "))
		}
		row("Approvals", v)
	}
	row("Labels", strings.Join(mr.Labels, ", "))
	if ts := m.mr.threads; len(ts) > 0 {
		open, done := 0, 0
		for _, t := range ts {
			switch {
			case t.Resolved:
				done++
			case t.Resolvable:
				open++
			}
		}
		v := plural(len(ts), "thread")
		if open+done > 0 {
			v = fmt.Sprintf("%d open · %d resolved", open, done)
			if n := len(ts) - open - done; n > 0 {
				v += " · " + plural(n, "comment")
			}
		}
		row("Threads", v+refDimStyle.Render(" · d to read and reply"))
	}
	if c := mr.Checks; c != nil {
		v := checkGlyph(c.Status) + " " + c.Label
		if c.Duration > 0 {
			v += refDimStyle.Render(" · " + spanText(time.Duration(c.Duration)*time.Second))
		}
		row("Pipeline", v)
		// A stage's jobs that want a look, by name; the passed ones counted, as a
		// big pipeline's names (docker/build:branch: [...]) fill the panel.
		stageW := 0
		for _, g := range c.Groups {
			stageW = max(stageW, ansi.StringWidth(g.Name))
		}
		for _, g := range c.Groups {
			var jobs []string
			passed := 0
			for _, j := range g.Jobs {
				if j.Status == forge.StatusSuccess {
					passed++
					continue
				}
				jobs = append(jobs, checkGlyph(j.Status)+" "+j.Name)
			}
			switch {
			case passed > 0 && len(jobs) == 0:
				jobs = append(jobs, checkGlyph(forge.StatusSuccess)+" "+plural(passed, "job")+" passed")
			case passed > 0:
				jobs = append(jobs, checkGlyph(forge.StatusSuccess)+" "+strconv.Itoa(passed)+" more passed")
			}
			b.WriteString("  " + refDimStyle.Render(g.Name+strings.Repeat(" ", stageW-ansi.StringWidth(g.Name))) + " " + strings.Join(jobs, "  ") + "\n")
		}
	}
	if d := strings.TrimSpace(mr.Description); d != "" {
		b.WriteString("\n" + renderMarkdown(d, m.emojiImg, nil, ""))
	}
	return b.String()
}

// mrKey handles a key while a merge request shows; false leaves it to the
// panel.
func (m Model) mrKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case msg.String() == "esc", key.Matches(msg, m.keys.Back):
		m.mr = nil
		m.status = ""
		if len(m.refs) == 0 { // opened alone: back to the screen
			m.closeRef()
			m.focus = focusJira
			m.resize()
			return m, nil, true
		}
		m.refView.GotoTop()
		m.renderRef()
		return m, nil, true
	case key.Matches(msg, m.keys.OpenAttach):
		u := m.mr.link
		if m.mr.mr != nil {
			u = cmp.Or(m.mr.mr.WebURL, u)
		}
		m.status = "opening " + u + "…"
		return m, m.openOpenable(openable{name: m.mr.title, url: u}), true
	case key.Matches(msg, m.keys.Refresh):
		p := m.mr
		return m, m.openMR(p.c, p.ref, p.link, p.title, true), true
	case msg.String() == "A" && m.mr.mr != nil:
		return m, m.approveMR(m.mr.c, m.mr.ref.Repo, m.mr.ref.Number), true
	case msg.String() == "d" && m.mr.mr != nil:
		out, cmd := m.openDiffView()
		return out, cmd, true
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Help):
		return m, nil, false
	}
	var cmd tea.Cmd
	m.refView, cmd = m.refView.Update(msg) // every other key reads on
	return m, cmd, true
}
