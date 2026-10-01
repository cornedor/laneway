// Package work reads what you did from outside Jira, for both front ends:
// your commits in jira.repos (the standup's), and the day's sessions from
// commits, branch switches and the ui.activity commands' lines (agent logs,
// shell history, herdr: anything that prints "time<TAB>key"), proposed as
// worklogs. Events a little apart are one session; the time up to an event
// goes to its issue; what is logged already comes off.
package work

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

// NoTicket names the keyless commits' group.
const NoTicket = "no ticket"

// KeyRe finds an issue key in a commit subject: upper case only, so
// "utf-8" names none.
var KeyRe = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*-[0-9]+\b`)

// branchRe finds an issue key in a branch name: issue/ABC-12-fix, abc-12,
// feature/ABC-12.
var branchRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([a-z][a-z0-9_]*-[0-9]+)`)

// BranchKey is the issue key branch names, "" for none.
func BranchKey(branch string) string {
	if m := branchRe.FindStringSubmatch(branch); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// ExpandHome is p with a leading ~ as the home directory.
func ExpandHome(p string) string {
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

// Repos are jira.repos' paths, each once, sorted.
func Repos(paths map[string]string) []string {
	var out []string
	for _, p := range paths {
		if p = ExpandHome(p); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// Commits are your commits in repos since since, on every branch and
// without merges, as standup entries; a commit in two repos (a fork) once.
// A repo git can't read is left out.
func Commits(repos []string, since time.Time) []jira.InboxEntry {
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
			out = append(out, jira.InboxEntry{Key: KeyRe.FindString(subject), When: time.Unix(sec, 0), What: "commit: " + subject})
		}
	}
	return out
}

// WithCommits merges commits into the standup's entries, oldest first; a
// commit takes its issue's summary from the other entries.
func WithCommits(entries, commits []jira.InboxEntry) []jira.InboxEntry {
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

// Event is a moment you worked on Key ("" for none named).
type Event struct {
	At     time.Time
	Key    string
	Source string // "commit", "git", or the activity command's name
}

// Proposal is a worklog the timesheet suggests.
type Proposal struct {
	Key     string
	Start   time.Time
	Seconds int
	Sources map[string]int // events per source
	Comment string         // what the worklog says: a meeting's title
}

const (
	proposeIdle  = 30 * time.Minute // a longer gap ends a session
	proposeLead  = 15 * time.Minute // what a session's first event counts
	proposeRound = 15 * 60          // proposals round to a quarter
)

// Propose sums each key's sessions, less what logged has for it, into
// proposals by start; under a quarter is left out.
func Propose(events []Event, logged map[string]int) []Proposal {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b Event) int { return a.At.Compare(b.At) })
	by := map[string]*Proposal{}
	var prev time.Time
	for _, e := range events {
		gap := proposeLead
		if !prev.IsZero() && e.At.Sub(prev) <= proposeIdle {
			gap = e.At.Sub(prev)
		}
		prev = e.At
		if e.Key == "" {
			continue
		}
		p, ok := by[e.Key]
		if !ok {
			p = &Proposal{Key: e.Key, Start: e.At.Add(-gap), Sources: map[string]int{}}
			by[e.Key] = p
		}
		p.Seconds += int(gap.Seconds())
		p.Sources[e.Source]++
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

// Day is day's proposals: git in repos and the activity commands' sessions,
// less what logs has, and extra (a calendar's meetings), by start; failed
// are the activity commands that failed.
func Day(repos, activity []string, day time.Time, logs []jira.Worklog, extra []Proposal) (ps []Proposal, failed []string) {
	logged := map[string]int{}
	for _, w := range logs {
		logged[w.Key] += w.Seconds
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	events := Git(repos, from, from.AddDate(0, 0, 1))
	more, failed := Activity(activity, day)
	ps = append(Propose(append(events, more...), logged), extra...)
	slices.SortStableFunc(ps, func(a, b Proposal) int { return a.Start.Compare(b.Start) })
	return ps, failed
}

// SourcesText is "3 commits, 2 claude".
func (p Proposal) SourcesText() string {
	names := make([]string, 0, len(p.Sources))
	for s := range p.Sources {
		names = append(names, s)
	}
	slices.Sort(names)
	parts := make([]string, len(names))
	for i, s := range names {
		parts[i] = strconv.Itoa(p.Sources[s]) + " " + s
	}
	return strings.Join(parts, ", ")
}

// Git is the events in repos between from and to: your commits (the key
// their subject names) and HEAD's reflog (the key the branch then names).
func Git(repos []string, from, to time.Time) []Event {
	var out []Event
	for _, c := range Commits(repos, from) {
		if c.When.Before(to) {
			out = append(out, Event{At: c.When, Key: c.Key, Source: "commit"})
		}
	}
	for _, repo := range repos {
		log, err := exec.Command("git", "-C", repo, "log", "-g", "--reverse", "--format=%at%x09%gs",
			"--since="+strconv.FormatInt(from.Unix(), 10), "HEAD").Output()
		if err != nil {
			continue
		}
		out = append(out, reflog(string(log), to)...)
	}
	return out
}

// reflog reads "unix<TAB>reflog subject" lines, oldest first: a checkout
// moves onto its branch, and every entry counts for the branch's key, else
// the subject's.
func reflog(log string, to time.Time) []Event {
	var out []Event
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
				branchKey = KeyRe.FindString(b)
			}
			key = branchKey // the branch left names the other
		} else {
			key = cmp.Or(branchKey, KeyRe.FindString(subject))
		}
		when := time.Unix(sec, 0)
		if !when.Before(to) {
			break
		}
		out = append(out, Event{At: when, Key: key, Source: "git"})
	}
	return out
}

// Activity runs the ui.activity commands for day and reads their lines; a
// command that fails is skipped, its name in failed.
func Activity(commands []string, day time.Time) (events []Event, failed []string) {
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
func parseActivity(raw []byte, source string, from, to time.Time) []Event {
	var out []Event
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		at, text, ok := strings.Cut(sc.Text(), "\t")
		when, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
		if !ok || err != nil || when.Before(from) || !when.Before(to) {
			continue
		}
		out = append(out, Event{At: when, Key: KeyRe.FindString(text), Source: source})
	}
	return out
}
