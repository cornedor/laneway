package ui

import (
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// The standup's git side: your commits in jira.repos since the day it
// starts, under the issue their subject names; keyless ones in a "no
// ticket" group.

// noTicket names the keyless commits' group.
const noTicket = "no ticket"

// commitIssueRe finds an issue key in a commit subject: upper case only,
// so "utf-8" names none.
var commitIssueRe = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*-[0-9]+\b`)

// standupRepos are jira.repos' paths, each once.
func (m *Model) standupRepos() []string {
	var out []string
	for _, p := range m.jiraRepos {
		if p = expandUserPath(p); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// gitCommits are your commits in repos since since, on every branch and
// without merges, as standup entries; a commit in two repos (a fork) once.
// A repo git can't read is left out.
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
			out = append(out, jira.InboxEntry{Key: commitIssueRe.FindString(subject), When: time.Unix(sec, 0), What: "commit: " + subject})
		}
	}
	return out
}

// withCommits merges commits into the standup's entries, oldest first; a
// commit takes its issue's summary from the other entries.
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
