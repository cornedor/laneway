package ui

import (
	"bytes"
	"context"
	"errors"
	"github.com/cornedor/laneway/internal/i18n"
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
	if m.demo {
		m.status = i18n.Tf("asking: %s", errOffInDemo.Error())
		return
	}
	if llmCommand(m.opts.llm) == nil {
		m.status = i18n.T("asking needs ui.llm (claude -p, llm, ollama run …) or claude on the PATH")
		return
	}
	m.startJiraPicker(jiraPickAsk, i18n.Tf("Ask about %s", m.jiraIssue.Key), false)
	items := make([]jiraPickerItem, len(asks))
	for i, a := range asks {
		items[i] = jiraPickerItem{id: a.ID, label: a.Label}
	}
	m.setJiraPickerItems(items)
}

// askLLM runs the question id about the panel's issue.
func (m *Model) askLLM(id string) tea.Cmd {
	iss, command := m.jiraIssue, llmCommand(m.opts.llm)
	if iss == nil || command == nil || m.demo {
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
	m.status = i18n.Tf("asking %s: %s…", command[0], strings.ToLower(label))
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
		m.fail(i18n.Tf("ask: %s", msg.err.Error()))
	case msg.out == "":
		m.status = i18n.T("the answer came back empty")
	case m.jiraIssue == nil || m.jiraIssue.Key != msg.key:
		m.status = i18n.Tf("the answer about %s came after you moved on", msg.key)
	default:
		m.openJiraCommentInput()
		m.jiraCommentInput.SetValue(msg.out)
		m.jiraCommentInput.CursorEnd()
		m.status = i18n.Tf("%s · ctrl+s posts it as a comment · ctrl+e $EDITOR · esc drops it", strings.ToLower(msg.label))
		m.renderRef()
	}
	return m, nil
}
