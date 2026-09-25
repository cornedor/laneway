package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/safeterm"
)

// ExecTimeout bounds one exec action.
const ExecTimeout = 30 * time.Second

// LogLine is f as a rules.log line, stamped now.
func LogLine(now time.Time, f Firing) string {
	return fmt.Sprintf("%s %s: %s\n", now.Format("2006-01-02 15:04:05"), orUnnamed(f.Rule), safeterm.Line(f.Text))
}

// AppendLog appends lines to the log at path.
func AppendLog(path string, lines []string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strings.Join(lines, ""))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// NotifySeq is a desktop notification by OSC 777, which kitty, Ghostty,
// WezTerm and foot show; other terminals ignore it.
func NotifySeq(title, body string) string {
	clean := func(s string) string { return strings.ReplaceAll(safeterm.Line(s), ";", ",") }
	return "\x1b]777;notify;" + clean(title) + ";" + clean(body) + "\x1b\\"
}

// Exec runs an exec action's argv with the issue as JSON on stdin and as
// LANEWAY_* variables (LANEWAY_KEY, LANEWAY_OLD_STATUS, …).
func Exec(ctx context.Context, f Firing) error {
	ctx, cancel := context.WithTimeout(ctx, ExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.Argv[0], f.Argv[1:]...)
	in, _ := json.Marshal(f.Vars)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = os.Environ()
	for k, v := range f.Vars {
		cmd.Env = append(cmd.Env, "LANEWAY_"+envName(k)+"="+v)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s: %v %s", orUnnamed(f.Rule), f.Argv[0], err, bytes.TrimSpace(out))
	}
	return nil
}

// JiraAct runs a transition or comment action on the event's issue. A
// transition to the status the issue already has does nothing.
func JiraAct(ctx context.Context, c *jira.Client, f Firing) error {
	ctx, cancel := context.WithTimeout(ctx, ExecTimeout)
	defer cancel()
	key := f.Vars["Key"]
	switch f.Action {
	case "transition":
		if strings.EqualFold(f.Vars["Status"], f.To) {
			return nil
		}
		opts, err := c.Transitions(ctx, key)
		if err != nil {
			return fmt.Errorf("%s: %s: %v", orUnnamed(f.Rule), key, err)
		}
		for _, o := range opts {
			if strings.EqualFold(o.Name, f.To) {
				if err := c.DoTransition(ctx, key, o.ID); err != nil {
					return fmt.Errorf("%s: %s → %s: %v", orUnnamed(f.Rule), key, f.To, err)
				}
				return nil
			}
		}
		return fmt.Errorf("%s: %s has no move to %s", orUnnamed(f.Rule), key, f.To)
	case "comment":
		if err := c.AddComment(ctx, key, f.Text, nil); err != nil {
			return fmt.Errorf("%s: %s comment: %v", orUnnamed(f.Rule), key, err)
		}
	}
	return nil
}

// envName turns OldStatus into OLD_STATUS.
func envName(k string) string {
	var b strings.Builder
	for i, r := range k {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func orUnnamed(name string) string {
	if name == "" {
		return "rule"
	}
	return name
}

// ResolveByMe sets each event's ByMe from the Jira changelog, leaving it
// nil where the lookup fails; the first failure is returned.
func ResolveByMe(ctx context.Context, c *jira.Client, events []Event, pointsField string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	me, err := c.Myself(ctx)
	if err != nil {
		return err
	}
	fields := map[string]string{New: "", Status: "status", Assignee: "assignee",
		Priority: "priority", Summary: "summary", Points: pointsField}
	var firstFail error
	for i := range events {
		ev := &events[i]
		field := fields[ev.Kind]
		if ev.Kind == Points && field == "" {
			continue
		}
		who, err := c.ChangeAuthor(ctx, ev.Card.Key, field)
		if err != nil {
			if firstFail == nil {
				firstFail = err
			}
			continue
		}
		byMe := who == me.AccountID
		ev.ByMe = &byMe
	}
	return firstFail
}
