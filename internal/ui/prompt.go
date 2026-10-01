package ui

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/work"
)

// laneway prompt: a segment for a shell prompt or tmux status line, read
// from the state file alone — no network, so it can run on every prompt.

// Prompt is what the segment shows; empty parts are unknown or off.
type Prompt struct {
	Key      string // the git branch's issue
	Status   string // its status on a board as last loaded
	TimerKey string // the issue the timer runs on
	Timer    string // how long it has run, "1h 20m"
	Inbox    int    // unread inbox threads, as the header last counted
}

// ReadPrompt fills a Prompt from st and the working directory's branch.
func ReadPrompt(st *store.Store, now time.Time) Prompt {
	p := Prompt{Key: BranchIssue()}
	if v, ok, _ := st.GetMeta(timerMeta); ok {
		key, unix, _ := strings.Cut(v, " ")
		if sec, err := strconv.ParseInt(unix, 10, 64); key != "" && err == nil {
			p.TimerKey = key
			p.Timer = jira.FormatDuration(max(int(now.Sub(time.Unix(sec, 0)).Seconds()), 0))
		}
	}
	if v, ok, _ := st.GetMeta(inboxUnreadMeta); ok {
		p.Inbox, _ = strconv.Atoi(v)
	}
	if p.Key != "" {
		p.Status = cachedStatus(st, p.Key)
	}
	return p
}

// BranchIssue is the issue key the working directory's git branch names,
// "" for none.
func BranchIssue() string {
	return work.BranchKey(headBranch())
}

// headBranch is the working directory's branch read from .git/HEAD, which
// is quicker than running git; "" outside a repo or detached.
var headBranch = func() string {
	dir, err := os.Getwd()
	for err == nil {
		dotGit := filepath.Join(dir, ".git")
		if fi, err := os.Stat(dotGit); err == nil {
			if !fi.IsDir() { // a worktree: "gitdir: /repo/.git/worktrees/x"
				b, _ := os.ReadFile(dotGit)
				gd, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(dir, gd)
				}
				dotGit = gd
			}
			b, _ := os.ReadFile(filepath.Join(dotGit, "HEAD"))
			ref, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
			if ref == strings.TrimSpace(string(b)) {
				return "" // detached: a commit id
			}
			return ref
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// CachedKeys are the issue keys on the stored boards, each once, sorted.
func CachedKeys(st *store.Store) []string {
	seen := map[string]bool{}
	for _, raw := range st.Prefixed(jiraMetaPrefix + "cache:") {
		var c struct {
			Cards []struct{ Key string }
		}
		if json.Unmarshal([]byte(raw), &c) != nil {
			continue
		}
		for _, card := range c.Cards {
			seen[card.Key] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// cachedStatus is key's status on any stored board, "" when on none.
func cachedStatus(st *store.Store, key string) string {
	for _, raw := range st.Prefixed(jiraMetaPrefix + "cache:") {
		var c struct {
			Cards []struct{ Key, Status string }
		}
		if json.Unmarshal([]byte(raw), &c) != nil {
			continue
		}
		for _, card := range c.Cards {
			if card.Key == key {
				return card.Status
			}
		}
	}
	return ""
}

// String is "ABC-12 · In review · ⏱ 1h 20m · ✉ 3", leaving out what's
// empty; the timer names its issue when that's not the branch's.
func (p Prompt) String() string {
	var parts []string
	for _, s := range []string{p.Key, p.Status} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if p.Timer != "" {
		t := "⏱ " + p.Timer
		if p.TimerKey != p.Key {
			t = "⏱ " + p.TimerKey + " " + p.Timer
		}
		parts = append(parts, t)
	}
	if p.Inbox > 0 {
		parts = append(parts, "✉ "+strconv.Itoa(p.Inbox))
	}
	return strings.Join(parts, " · ")
}
