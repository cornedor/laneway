package ui

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// p in the timesheet proposes the day's worklogs from what you did: commits
// and branch switches in jira.repos, and the ui.activity commands' lines
// (agent logs, shell history, herdr: anything that prints "time<TAB>key").
// Events a little apart are one session; the time up to an event goes to
// its issue. What is logged already comes off; enter logs one.

// workEvent is a moment you worked on key ("" for none named).
type workEvent struct {
	at     time.Time
	key    string
	source string // "commit", "git", or the activity command's name
}

// proposal is a worklog the timesheet suggests.
type proposal struct {
	key     string
	start   time.Time
	seconds int
	sources map[string]int // events per source
}

const (
	proposeIdle  = 30 * time.Minute // a longer gap ends a session
	proposeLead  = 15 * time.Minute // what a session's first event counts
	proposeRound = 15 * 60          // proposals round to a quarter
)

// proposeWork sums each key's sessions, less what logged has for it, into
// proposals by start; under a quarter is left out.
func proposeWork(events []workEvent, logged map[string]int) []proposal {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b workEvent) int { return a.at.Compare(b.at) })
	by := map[string]*proposal{}
	var prev time.Time
	for _, e := range events {
		gap := proposeLead
		if !prev.IsZero() && e.at.Sub(prev) <= proposeIdle {
			gap = e.at.Sub(prev)
		}
		prev = e.at
		if e.key == "" {
			continue
		}
		p, ok := by[e.key]
		if !ok {
			p = &proposal{key: e.key, start: e.at.Add(-gap), sources: map[string]int{}}
			by[e.key] = p
		}
		p.seconds += int(gap.Seconds())
		p.sources[e.source]++
	}
	var out []proposal
	for _, p := range by {
		secs := p.seconds - logged[p.key]
		secs = (secs + proposeRound/2) / proposeRound * proposeRound
		if secs >= proposeRound {
			p.seconds = secs
			out = append(out, *p)
		}
	}
	slices.SortFunc(out, func(a, b proposal) int { return a.start.Compare(b.start) })
	return out
}

// sourcesText is "3 commits, 2 claude".
func (p proposal) sourcesText() string {
	names := make([]string, 0, len(p.sources))
	for s := range p.sources {
		names = append(names, s)
	}
	slices.Sort(names)
	parts := make([]string, len(names))
	for i, s := range names {
		parts[i] = strconv.Itoa(p.sources[s]) + " " + s
	}
	return strings.Join(parts, ", ")
}

// gitWork is the day's events in repos: your commits (the key their
// subject names) and HEAD's reflog (the key the branch then names).
func gitWork(repos []string, from, to time.Time) []workEvent {
	var out []workEvent
	for _, c := range gitCommits(repos, from) {
		if c.When.Before(to) {
			out = append(out, workEvent{at: c.When, key: c.Key, source: "commit"})
		}
	}
	for _, repo := range repos {
		log, err := exec.Command("git", "-C", repo, "log", "-g", "--reverse", "--format=%at%x09%gs",
			"--since="+strconv.FormatInt(from.Unix(), 10), "HEAD").Output()
		if err != nil {
			continue
		}
		out = append(out, reflogWork(string(log), to)...)
	}
	return out
}

// reflogWork reads "unix<TAB>reflog subject" lines, oldest first: a
// checkout moves onto its branch, and every entry counts for the branch's
// key, else the subject's.
func reflogWork(log string, to time.Time) []workEvent {
	var out []workEvent
	branchKey := ""
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		at, subject, ok := strings.Cut(line, "\t")
		sec, err := strconv.ParseInt(at, 10, 64)
		if !ok || err != nil {
			continue
		}
		key := ""
		if _, onto, ok := strings.Cut(subject, "checkout: moving from "); ok {
			if _, b, ok := strings.Cut(onto, " to "); ok {
				branchKey = commitIssueRe.FindString(b)
			}
			key = branchKey // the branch left names the other
		} else {
			key = cmp.Or(branchKey, commitIssueRe.FindString(subject))
		}
		when := time.Unix(sec, 0)
		if !when.Before(to) {
			break
		}
		out = append(out, workEvent{at: when, key: key, source: "git"})
	}
	return out
}

// activityWork runs the ui.activity commands for day and reads their
// lines; a command that fails is skipped, its name in failed.
func activityWork(commands []string, day time.Time) (events []workEvent, failed []string) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	to := from.AddDate(0, 0, 1)
	for _, command := range commands {
		f := strings.Fields(command)
		if len(f) == 0 {
			continue
		}
		raw, err := exec.Command(f[0], append(f[1:], from.Format(time.DateOnly))...).Output()
		if err != nil {
			failed = append(failed, f[0])
			continue
		}
		events = append(events, parseActivity(raw, filepath.Base(f[0]), from, to)...)
	}
	return events, failed
}

// parseActivity reads "time<TAB>key or text" lines inside [from, to).
func parseActivity(raw []byte, source string, from, to time.Time) []workEvent {
	var out []workEvent
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		at, text, ok := strings.Cut(sc.Text(), "\t")
		when, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
		if !ok || err != nil || when.Before(from) || !when.Before(to) {
			continue
		}
		out = append(out, workEvent{at: when, key: commitIssueRe.FindString(text), source: source})
	}
	return out
}

// proposalItems are the timesheet's rows for ps, after a heading.
func proposalItems(ps []proposal) []jiraPickerItem {
	if len(ps) == 0 {
		return []jiraPickerItem{{label: "── nothing to propose: every session is logged"}}
	}
	items := []jiraPickerItem{{label: "── proposed · enter logs one"}}
	for _, p := range ps {
		items = append(items, jiraPickerItem{
			id:    proposalID + p.key + "/" + strconv.FormatInt(p.start.Unix(), 10),
			label: fmt.Sprintf("≈ %s  %6s  %s — %s", p.start.Local().Format("15:04"), jira.FormatDuration(p.seconds), p.key, p.sourcesText()),
			value: jira.FormatDuration(p.seconds),
		})
	}
	return items
}

type proposalsMsg struct {
	gen    int
	day    time.Time
	items  []jiraPickerItem
	failed []string
	err    error
}

// loadProposals reads the day's work and what is logged, for p.
func (m *Model) loadProposals() tea.Cmd {
	p := m.jiraPicker
	gen, day, c, ctx, repos, acts := p.gen, p.day, m.jiraClient, m.ctx, m.standupRepos(), m.uiConfig.Activity
	m.status = "reading git and ui.activity…"
	return func() tea.Msg {
		logs, err := c.MyWorklogs(ctx, day)
		if err != nil {
			return proposalsMsg{gen: gen, day: day, err: err}
		}
		logged := map[string]int{}
		for _, w := range logs {
			logged[w.Key] += w.Seconds
		}
		from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		events := gitWork(repos, from, from.AddDate(0, 0, 1))
		more, failed := activityWork(acts, day)
		return proposalsMsg{gen: gen, day: day, items: proposalItems(proposeWork(append(events, more...), logged)), failed: failed}
	}
}

// handleProposals puts the proposals under the day's worklogs, in place
// of any before.
func (m Model) handleProposals(msg proposalsMsg) (tea.Model, tea.Cmd) {
	p := &m.jiraPicker
	if !p.active || p.kind != jiraPickTimesheet || p.gen != msg.gen || !p.day.Equal(msg.day) {
		return m, nil
	}
	if msg.err != nil {
		m.fail("proposals: " + msg.err.Error())
		return m, nil
	}
	i := slices.IndexFunc(p.items, func(it jiraPickerItem) bool {
		return strings.HasPrefix(it.label, "── proposed") || strings.HasPrefix(it.label, "── nothing to propose")
	})
	if i < 0 {
		i = len(p.items)
	}
	p.items = append(p.items[:i:i], msg.items...)
	p.idx = min(i+1, len(p.items)-1)
	m.status = fmt.Sprintf("%s proposed", plural(len(msg.items)-1, "worklog"))
	if len(msg.failed) > 0 {
		m.status += " · ui.activity failed: " + strings.Join(msg.failed, ", ")
	}
	return m, nil
}

// proposalID prefixes a proposal row's id: "propose:KEY/unix".
const proposalID = "propose:"
