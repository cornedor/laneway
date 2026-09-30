package web

import (
	"bufio"
	"bytes"
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// Git and ui.activity as sources of work: the standup's commits and the
// timesheet's proposals. Mirrors internal/ui/standup_git.go and propose.go.

const noTicket = "no ticket"

var gitKeyRe = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*-[0-9]+\b`)

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// repos are jira.repos' paths, each once.
func (s *Server) repos() []string {
	var out []string
	for _, p := range s.opt.Jira.Repos {
		if p = expandHome(p); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// gitCommits are your commits in repos since since, as standup entries.
func gitCommits(repos []string, since time.Time) []jira.InboxEntry {
	seen := map[string]bool{}
	var out []jira.InboxEntry
	for _, repo := range repos {
		email, err := exec.Command("git", "-C", repo, "config", "user.email").Output()
		if err != nil || strings.TrimSpace(string(email)) == "" {
			continue
		}
		log, err := exec.Command("git", "-C", repo, "log", "--all", "--no-merges",
			"--since="+strconv.FormatInt(since.Unix(), 10), "--author="+strings.TrimSpace(string(email)),
			"--format=%H%x09%at%x09%s").Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
			hash, rest, ok := strings.Cut(line, "\t")
			at, subject, ok2 := strings.Cut(rest, "\t")
			sec, err := strconv.ParseInt(at, 10, 64)
			if !ok || !ok2 || err != nil || seen[hash] {
				continue
			}
			seen[hash] = true
			out = append(out, jira.InboxEntry{Key: gitKeyRe.FindString(subject), When: time.Unix(sec, 0), What: "commit: " + subject})
		}
	}
	return out
}

// withCommits merges commits into entries, oldest first.
func withCommits(entries, commits []jira.InboxEntry) []jira.InboxEntry {
	summary := map[string]string{}
	for _, e := range entries {
		if e.Summary != "" {
			summary[e.Key] = e.Summary
		}
	}
	for _, c := range commits {
		c.Summary = summary[c.Key]
		entries = append(entries, c)
	}
	slices.SortStableFunc(entries, func(a, b jira.InboxEntry) int { return a.When.Compare(b.When) })
	return entries
}

type workEvent struct {
	at     time.Time
	key    string
	source string
}

// Proposal is a worklog the day view suggests.
type Proposal struct {
	Key     string
	Start   time.Time
	Seconds int
	Sources map[string]int
}

const (
	proposeIdle  = 30 * time.Minute
	proposeLead  = 15 * time.Minute
	proposeRound = 15 * 60
)

func proposeWork(events []workEvent, logged map[string]int) []Proposal {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b workEvent) int { return a.at.Compare(b.at) })
	by := map[string]*Proposal{}
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
			p = &Proposal{Key: e.key, Start: e.at.Add(-gap), Sources: map[string]int{}}
			by[e.key] = p
		}
		p.Seconds += int(gap.Seconds())
		p.Sources[e.source]++
	}
	out := []Proposal{}
	for _, p := range by {
		secs := p.Seconds - logged[p.Key]
		secs = (secs + proposeRound/2) / proposeRound * proposeRound
		if secs >= proposeRound {
			p.Seconds = secs
			out = append(out, *p)
		}
	}
	slices.SortFunc(out, func(a, b Proposal) int { return a.Start.Compare(b.Start) })
	return out
}

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
				branchKey = gitKeyRe.FindString(b)
			}
			key = branchKey
		} else {
			key = cmp.Or(branchKey, gitKeyRe.FindString(subject))
		}
		when := time.Unix(sec, 0)
		if !when.Before(to) {
			break
		}
		out = append(out, workEvent{at: when, key: key, source: "git"})
	}
	return out
}

// activityWork runs the ui.activity commands for day; a failing one is skipped.
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

func parseActivity(raw []byte, source string, from, to time.Time) []workEvent {
	var out []workEvent
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		at, text, ok := strings.Cut(sc.Text(), "\t")
		when, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
		if !ok || err != nil || when.Before(from) || !when.Before(to) {
			continue
		}
		out = append(out, workEvent{at: when, key: gitKeyRe.FindString(text), source: source})
	}
	return out
}
