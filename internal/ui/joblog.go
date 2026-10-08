package ui

import (
	"fmt"
	"github.com/cornedor/laneway/internal/i18n"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/joblog"
)

// p on a merge request in the panel: its pipeline's jobs in a list, the ones
// that want a look first; enter reads one's log over the screen, as the diff
// view takes it, in the log's own colours. A running job's log is read again
// every jobLogEvery and followed to its end (G follows, scrolling up stops
// it); the panel's pipeline is read again every mrPipelineEvery while it
// runs. o opens the job in GitLab, r reads it again, esc closes it.

const (
	jobLogEvery     = 2 * time.Second
	mrPipelineEvery = 5 * time.Second
)

// jobLogState is the log view, nil closed.
type jobLogState struct {
	c       *gitlab.Client
	repo    string
	id      int
	name    string
	gen     int
	job     *forge.JobLog
	lines   []string // the log's lines as terminal text
	loading bool
	err     error
	top     int // the first line drawn
	hscroll int
	follow  bool // the end stays in sight as lines come
	viewH   int
}

type jobLogMsg struct {
	gen   int
	job   *forge.JobLog
	lines []string
	err   error
}

type jobLogTickMsg struct{ gen int }

// mrPipelineTickMsg reads the panel's merge request again for its pipeline.
type mrPipelineTickMsg struct{ gen int }

// openJobPicker lists the panel's merge request's jobs (p).
func (m *Model) openJobPicker() tea.Cmd {
	p := m.mr
	if p == nil || p.mr == nil || p.mr.Checks == nil {
		m.status = i18n.T("no pipeline on this merge request")
		return nil
	}
	m.startJiraPicker(jiraPickJob, i18n.Tf("Jobs — %s!%d", p.ref.Repo, p.ref.Number), true)
	var look, passed []jiraPickerItem
	for _, g := range p.mr.Checks.Groups {
		for _, j := range g.Jobs {
			if j.ID == 0 {
				continue
			}
			it := jiraPickerItem{id: strconv.Itoa(j.ID), value: j.Name, label: checkGlyph(j.Status) + " " + fmt.Sprintf("%-8s", g.Name) + "  " + j.Name, search: g.Name}
			if j.Status == forge.StatusSuccess || j.Status == forge.StatusSkipped {
				passed = append(passed, it)
				continue
			}
			look = append(look, it)
		}
	}
	m.jiraPicker.items, m.jiraPicker.all = append(look, passed...), append(look, passed...)
	m.jiraPicker.loading = false
	if len(m.jiraPicker.items) == 0 {
		m.jiraPicker.items = []jiraPickerItem{{label: i18n.T("GitLab sent no jobs")}}
	}
	return nil
}

// openJobLog reads job id's log over the screen.
func (m *Model) openJobLog(c *gitlab.Client, repo string, id int, name string) tea.Cmd {
	m.jobLogGen++
	m.jobLog = &jobLogState{c: c, repo: repo, id: id, name: name, gen: m.jobLogGen, loading: true, follow: true}
	m.status = i18n.Tf("reading %s…", name)
	return m.fetchJobLog()
}

func (m *Model) fetchJobLog() tea.Cmd {
	j := m.jobLog
	if j == nil {
		return nil
	}
	c, ctx, repo, id, gen := j.c, m.ctx, j.repo, j.id, j.gen
	return func() tea.Msg {
		job, err := c.JobLog(ctx, repo, id)
		if err != nil {
			return jobLogMsg{gen: gen, err: err}
		}
		ls := joblog.Parse(job.Log)
		lines := make([]string, len(ls))
		for i, l := range ls {
			lines[i] = l.ANSI()
		}
		return jobLogMsg{gen: gen, job: job, lines: lines}
	}
}

func (m Model) handleJobLog(msg jobLogMsg) (tea.Model, tea.Cmd) {
	j := m.jobLog
	if j == nil || j.gen != msg.gen {
		return m, nil
	}
	j.loading = false
	if msg.err != nil {
		j.err = msg.err
		m.status = i18n.Tf("job log failed: %s", msg.err.Error())
		return m, nil
	}
	wasLive := j.job != nil && !j.job.Done()
	j.job, j.lines, j.err = msg.job, msg.lines, nil
	if j.follow {
		j.top = j.maxTop()
	}
	m.status = ""
	var cmds []tea.Cmd
	if !j.job.Done() {
		gen := j.gen
		cmds = append(cmds, tea.Tick(jobLogEvery, func(time.Time) tea.Msg { return jobLogTickMsg{gen: gen} }))
	} else if wasLive {
		m.status = j.name + " " + jobWord(j.job.Status)
		cmds = append(cmds, m.refreshMRPipeline()) // it just ended: the pipeline moved
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleJobLogTick(msg jobLogTickMsg) (tea.Model, tea.Cmd) {
	if m.jobLog == nil || m.jobLog.gen != msg.gen {
		return m, nil
	}
	return m, m.fetchJobLog()
}

// refreshMRPipeline reads the panel's merge request again, past the cache,
// keeping what the panel shows until it lands.
func (m *Model) refreshMRPipeline() tea.Cmd {
	p := m.mr
	if p == nil || p.mr == nil {
		return nil
	}
	c, ctx, r, gen := p.c, m.ctx, p.ref, p.gen
	return func() tea.Msg {
		c.Invalidate(r.Repo, r.Number)
		mr, err := c.Get(ctx, r.Repo, r.Number)
		return mrMsg{gen: gen, mr: mr, err: err, quiet: true}
	}
}

// watchMRPipeline reads the panel's pipeline again in a while when it runs.
func (m *Model) watchMRPipeline() tea.Cmd {
	p := m.mr
	if p == nil || p.mr == nil || p.mr.Checks == nil || !checksRunning(p.mr.Checks.Status) {
		return nil
	}
	gen := p.gen
	return tea.Tick(mrPipelineEvery, func(time.Time) tea.Msg { return mrPipelineTickMsg{gen: gen} })
}

func (m Model) handleMRPipelineTick(msg mrPipelineTickMsg) (tea.Model, tea.Cmd) {
	if m.mr == nil || m.mr.gen != msg.gen {
		return m, nil
	}
	return m, m.refreshMRPipeline()
}

// checksRunning is whether a pipeline or job status may still change.
func checksRunning(s string) bool { return s == forge.StatusRunning || s == forge.StatusPending }

// jobWord is a job status in words.
func jobWord(s string) string {
	switch s {
	case forge.StatusSuccess:
		return i18n.T("passed")
	case forge.StatusWarning:
		return i18n.T("failed, allowed to")
	}
	return s
}

func (j *jobLogState) maxTop() int { return max(len(j.lines)-max(j.viewH, 1), 0) }

// scroll moves the view by n lines; reaching the end follows it again.
func (j *jobLogState) scroll(n int) {
	j.top = min(max(j.top+n, 0), j.maxTop())
	j.follow = j.top == j.maxTop()
}

func (m Model) handleJobLogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	j := m.jobLog
	if key.Matches(msg, m.keys.Help) {
		m.openHelp(i18n.T("Job log"))
		return m, nil
	}
	page := max(j.viewH-1, 1)
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q":
		m.jobLog, m.status = nil, ""
		return m, nil
	case "up", "k":
		j.scroll(-1)
	case "down", "j":
		j.scroll(1)
	case "pgup", "ctrl+u":
		j.scroll(-page)
	case "pgdown", "ctrl+d", "space":
		j.scroll(page)
	case "home", "g":
		j.top, j.follow = 0, false
	case "end", "G":
		j.top, j.follow = j.maxTop(), true
	case "left", "h":
		j.hscroll = max(0, j.hscroll-diffHScrollStep)
	case "right", "l":
		j.hscroll += diffHScrollStep
	case "r":
		j.loading = true
		return m, m.fetchJobLog()
	case "o":
		if j.job != nil && j.job.WebURL != "" {
			m.status = i18n.Tf("opening %s…", j.job.WebURL)
			return m, m.openOpenable(openable{name: j.name, url: j.job.WebURL})
		}
	}
	return m, nil
}

// renderJobLog draws the log over the body, as the diff view is drawn.
func (m *Model) renderJobLog(bodyH int) string {
	j := m.jobLog
	outerW := max(1, m.width-2)
	inner := max(1, outerW-4)
	h := max(3, bodyH-5)
	j.viewH = h
	if j.follow {
		j.top = j.maxTop()
	}
	j.top = min(j.top, j.maxTop())
	gutter := len(strconv.Itoa(max(len(j.lines), 1)))
	avail := max(inner-gutter-2, 1)
	body := make([]string, 0, h+1)
	switch {
	case j.err != nil && j.job == nil:
		body = append(body, refErrStyle.Render(truncate(j.err.Error(), inner)))
	case j.job == nil:
		body = append(body, refDimStyle.Render(i18n.T("reading the job's log…")))
	case len(j.lines) == 0:
		body = append(body, refDimStyle.Render(i18n.T("no output yet")))
	default:
		for i := j.top; i < len(j.lines) && i < j.top+h; i++ {
			text := j.lines[i]
			if j.hscroll > 0 {
				text = ansi.Cut(text, j.hscroll, j.hscroll+avail)
			} else {
				text = ansi.Truncate(text, avail, "")
			}
			body = append(body, diffGutterStyle.Render(fmt.Sprintf("%*d", gutter, i+1))+"  "+text+"\x1b[0m")
		}
	}
	for len(body) < h {
		body = append(body, "")
	}
	hint := i18n.Tf("↑/↓ scroll · G the end, following · ←/→ pan · o GitLab · r reload · %s keys · esc close", helpKey(m.keys.Help))
	if j.job != nil && j.job.Truncated {
		hint = i18n.T("the log's start is cut: o has all of it · ") + hint
	}
	body = append(body, refDimStyle.Render(truncate(hint, inner)))
	return m.renderModalFrame(outerW, j.title(), j.scrollHint(), strings.Join(body, "\n"))
}

// title names the job and its state.
func (j *jobLogState) title() string {
	t := j.name
	if j.job == nil {
		return t
	}
	t = checkGlyph(j.job.Status) + " " + t + " · " + j.job.Stage + " · " + jobWord(j.job.Status)
	switch d := j.job.Duration; {
	case d >= 60:
		t += " · " + spanText(time.Duration(d)*time.Second)
	case d > 0:
		t += " · " + strconv.Itoa(d) + "s"
	}
	return t
}

// scrollHint is where the view is, and whether it follows.
func (j *jobLogState) scrollHint() string {
	if len(j.lines) == 0 {
		return ""
	}
	s := fmt.Sprintf("%d–%d/%d", j.top+1, min(j.top+j.viewH, len(j.lines)), len(j.lines))
	if j.job != nil && !j.job.Done() {
		if j.follow {
			s += " · " + lipgloss.NewStyle().Foreground(focusedColor).Render(i18n.T("● following"))
		} else {
			s += i18n.T(" · G follows")
		}
	}
	return s
}
