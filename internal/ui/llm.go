package ui

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/llm"
)

// ctrl+a in the panel asks ui.llm about the issue: the issue, its comments
// and history go in on stdin, the question as the last argument. The answer
// lands in the comment composer, so nothing is written until you post it
// (ctrl+e hands it to $EDITOR, esc drops it).

var asks = llm.Asks

type llmDoneMsg struct {
	key, label, out string
	err             error
}

// llmCommand is ui.llm, else claude -p when claude is on the PATH.
var llmCommand = llm.Command

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
		items[i] = jiraPickerItem{id: a.ID, label: a.Label}
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
		if a.ID == id {
			label, prompt = a.Label, a.Prompt
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

var llmInput = llm.Input

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
