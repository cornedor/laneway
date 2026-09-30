// Package llm asks ui.llm about an issue: the TUI's ctrl+a and the web's
// ask dialog share the questions, the input and the command.
package llm

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/cornedor/laneway/internal/jira"
)

// Ask is a canned question.
type Ask struct{ ID, Label, Prompt string }

// Asks are the questions offered, by id.
var Asks = []Ask{
	{"summary", "Summarise the thread", "Summarise this Jira issue and its discussion in a few lines: where it stands, what was decided, what is open."},
	{"criteria", "Draft acceptance criteria", "Draft acceptance criteria for this Jira issue as a short markdown checklist."},
	{"subtasks", "Split into subtasks", "Split this Jira issue into subtasks: a markdown list, one line each, in the order to do them."},
	{"points", "Suggest story points", "Suggest story points (Fibonacci) for this Jira issue, with one line of reasoning."},
}

// Command is ui.llm's words, else claude -p when claude is on the PATH.
var Command = func(conf []string) []string {
	if len(conf) > 0 {
		return conf
	}
	if _, err := exec.LookPath("claude"); err == nil {
		return []string{"claude", "-p"}
	}
	return nil
}

// Stream runs command with prompt appended and input on stdin, copying its
// output to w as it comes. A failure carries the command's stderr.
func Stream(ctx context.Context, command []string, prompt, input string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, command[0], append(command[1:], prompt)...)
	cmd.Stdin = strings.NewReader(input)
	var errOut strings.Builder
	cmd.Stdout, cmd.Stderr = w, &errOut
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// Input is the issue as markdown: fields, description, comments, history.
func Input(iss jira.Issue, hist []jira.InboxEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\n\n", iss.Key, iss.Summary)
	for _, f := range [][2]string{{"Type", iss.Type}, {"Status", iss.Status}, {"Priority", iss.Priority},
		{"Assignee", iss.Assignee}, {"Reporter", iss.Reporter}, {"Points", iss.StoryPoints}, {"Labels", strings.Join(iss.Labels, ", ")}} {
		if f[1] != "" {
			fmt.Fprintf(&b, "- %s: %s\n", f[0], f[1])
		}
	}
	if d := strings.TrimSpace(iss.Description); d != "" {
		b.WriteString("\n## Description\n\n" + d + "\n")
	}
	if len(iss.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range iss.Comments {
			fmt.Fprintf(&b, "\n### %s, %s\n\n%s\n", c.Author, c.Created.Format("2006-01-02"), strings.TrimSpace(c.Body))
		}
	}
	if len(hist) > 0 {
		b.WriteString("\n## History\n\n")
		for _, e := range hist {
			fmt.Fprintf(&b, "- %s %s: %s\n", e.When.Format("2006-01-02 15:04"), e.Who, e.What)
		}
	}
	return b.String()
}
