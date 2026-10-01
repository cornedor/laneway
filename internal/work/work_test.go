package work

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestPropose: events close together are one session, the time up to
// an event goes to its issue, a session's first event counts a quarter,
// and what is logged comes off.
func TestPropose(t *testing.T) {
	at := func(hm string) time.Time {
		v, _ := time.ParseInLocation("15:04", hm, time.Local)
		return v
	}
	events := []Event{
		{At: at("09:00"), Key: "A-1", Source: "commit"},
		{At: at("09:20"), Key: "A-1", Source: "claude"},
		{At: at("09:50"), Key: "A-2", Source: "git"},    // 30m: still the session
		{At: at("11:00"), Key: "A-2", Source: "commit"}, // after a gap: a quarter
		{At: at("11:05"), Key: "", Source: "claude"},    // no key: gone
		{At: at("14:00"), Key: "A-3", Source: "commit"}, // logged already
	}
	got := Propose(events, map[string]int{"A-3": 3600})
	if len(got) != 2 {
		t.Fatalf("proposals %+v", got)
	}
	if p := got[0]; p.Key != "A-1" || p.Seconds != 30*60 || !p.Start.Equal(at("08:45")) || p.SourcesText() != "1 claude, 1 commit" {
		t.Errorf("A-1: %+v", p)
	}
	if p := got[1]; p.Key != "A-2" || p.Seconds != 45*60 {
		t.Errorf("A-2: %d, want 45m", p.Seconds)
	}
}

func TestReflog(t *testing.T) {
	log := "100\tcheckout: moving from main to issue/ABC-3-login\n200\tcommit: wip\n300\tcheckout: moving from issue/ABC-3-login to main\n400\tcommit: ABC-9 tidy\n"
	var keys []string
	for _, e := range reflog(log, time.Unix(1000, 0)) {
		keys = append(keys, e.Key)
	}
	if strings.Join(keys, " ") != "ABC-3 ABC-3  ABC-9" {
		t.Errorf("keys %q", keys)
	}
}

// TestCommits: your commits since the day, keyed by their subject;
// others' and older ones left out.
func TestCommits(t *testing.T) {
	repo := t.TempDir()
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skip("git:", err, string(out))
		}
	}
	git(nil, "init", "-q")
	git(nil, "config", "user.email", "me@x.test")
	git(nil, "config", "user.name", "Me")
	old := []string{"GIT_AUTHOR_DATE=2020-01-01T10:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T10:00:00Z"}
	git(old, "commit", "-q", "--allow-empty", "-m", "ABC-1 long ago")
	git(nil, "commit", "-q", "--allow-empty", "-m", "Fix login ABC-2")
	git(nil, "commit", "-q", "--allow-empty", "-m", "utf-8 names in paths")
	git([]string{"GIT_AUTHOR_EMAIL=bob@x.test"}, "commit", "-q", "--allow-empty", "-m", "ABC-3 bob's")
	got := Commits([]string{repo, repo, t.TempDir()}, time.Now().Add(-time.Hour))
	var rows []string
	for _, e := range got {
		rows = append(rows, e.Key+"|"+e.What)
	}
	if strings.Join(rows, "\n") != "|commit: utf-8 names in paths\nABC-2|commit: Fix login ABC-2" {
		t.Errorf("commits:\n%s", strings.Join(rows, "\n"))
	}
}
