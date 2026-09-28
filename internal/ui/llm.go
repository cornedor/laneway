package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// ctrl+a in the panel asks ui.llm about the issue: the issue, its comments
// and history go in on stdin, the question as the last argument. The answer
// lands in the comment composer, so nothing is written until you post it
// (ctrl+e hands it to $EDITOR, esc drops it).

// asks are the questions ctrl+a offers, by id.
var asks = []struct{ id, label, prompt string }{
	{"summary", "Summarise the thread", "Summarise this Jira issue and its discussion in a few lines: where it stands, what was decided, what is open."},
	{"criteria", "Draft acceptance criteria", "Draft acceptance criteria for this Jira issue as a short markdown checklist."},
	{"subtasks", "Split into subtasks", "Split this Jira issue into subtasks: a markdown list, one line each, in the order to do them."},
	{"points", "Suggest story points", "Suggest story points (Fibonacci) for this Jira issue, with one line of reasoning."},
}

type llmDoneMsg struct {
	key, label, out string
	err             error
}

// llmCommand is ui.llm, else claude -p when claude is on the PATH.
var llmCommand = func(conf []string) []string {
	if len(conf) > 0 {
		return conf
	}
	if _, err := exec.LookPath("claude"); err == nil {
		return []string{"claude", "-p"}
	}
	return nil
}

// runLLM runs command with prompt appended and input on stdin. Tests swap it.
var runLLM = func(ctx context.Context, command []string, prompt, input string) (string, error) {
	cmd := exec.CommandContext(ctx, command[0], append(command[1:], prompt)...)
	cmd.Stdin = strings.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// openAsk lists the questions.
func (m *Model) openAsk() {
	if llmCommand(m.opts.llm) == nil {
		m.status = "asking needs ui.llm (claude -p, llm, ollama run …) or claude on the PATH"
		return
	}
	m.startJiraPicker(jiraPickAsk, "Ask about "+m.jiraIssue.Key, false)
	items := make([]jiraPickerItem, len(asks))
	for i, a := range asks {
		items[i] = jiraPickerItem{id: a.id, label: a.label}
	}
	m.setJiraPickerItems(items)
}

// askLLM runs the question id about the panel's issue.
func (m *Model) askLLM(id string) tea.Cmd {
	iss, command := m.jiraIssue, llmCommand(m.opts.llm)
	if iss == nil || command == nil {
		return nil
	}
	var label, prompt string
	for _, a := range asks {
		if a.id == id {
			label, prompt = a.label, a.prompt
		}
	}
	if prompt == "" {
		return nil
	}
	m.status = "asking " + command[0] + ": " + strings.ToLower(label) + "…"
	c, ctx, is := m.jiraClient, m.ctx, *iss
	return func() tea.Msg {
		hist, _ := c.History(ctx, is.Key) // the issue alone still answers
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		out, err := runLLM(ctx, command, prompt, llmInput(is, hist))
		return llmDoneMsg{key: is.Key, label: label, out: out, err: err}
	}
}

// llmInput is the issue as markdown: fields, description, comments, history.
func llmInput(iss jira.Issue, hist []jira.InboxEntry) string {
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

// handleLLMDone puts the answer in the comment composer, when the panel
// still shows the issue.
func (m Model) handleLLMDone(msg llmDoneMsg) (tea.Model, tea.Cmd) {
	msg.out = strings.TrimSpace(msg.out)
	switch {
	case msg.err != nil:
		m.fail("ask: " + msg.err.Error())
	case msg.out == "":
		m.status = "the answer came back empty"
	case m.jiraIssue == nil || m.jiraIssue.Key != msg.key:
		m.status = "the answer about " + msg.key + " came after you moved on"
	default:
		m.openJiraCommentInput()
		m.jiraCommentInput.SetValue(msg.out)
		m.jiraCommentInput.CursorEnd()
		m.status = strings.ToLower(msg.label) + " · ctrl+s posts it as a comment · ctrl+e $EDITOR · esc drops it"
		m.renderRef()
	}
	return m, nil
}
