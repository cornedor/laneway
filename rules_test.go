package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesCmd(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(p, []byte(`
rules:
  - name: done-bugs
    on: status
    match: {type: Bug, status: Done, from_status: "In *"}
    actions: [{type: log}, {type: notify, title: "{{.Key}}"}]
  - name: any
    actions: [{type: exec, command: [echo, "{{.Key}}"]}]
  - name: broken
    on: moved
    actions: [{type: log}]
`), 0o600)
	var out, errOut bytes.Buffer
	if code := rulesCmd([]string{"list", "-config", p}, &out, &errOut); code != 0 {
		t.Fatalf("list exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "2 rules") || !strings.Contains(out.String(), "on status      log, notify") ||
		!strings.Contains(errOut.String(), "broken") {
		t.Errorf("list:\n%s\n%s", out.String(), errOut.String())
	}
	out.Reset()
	rulesCmd([]string{"test", "-config", p, "-on", "status", "-key", "A-1", "-type", "bug", "-status", "Done", "-from-status", "In review"}, &out, &errOut)
	for _, want := range []string{"✓ done-bugs  log     A-1 status In review → Done", "✓ done-bugs  notify  A-1: A-1 status", "✓ any        exec    echo A-1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("test lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	rulesCmd([]string{"test", "-config", p}, &out, &errOut)
	if !strings.Contains(out.String(), "✗ done-bugs  on: not new") {
		t.Errorf("test new:\n%s", out.String())
	}
	out.Reset()
	p2 := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p2, []byte("rules:\n  - {name: others, match: {by_me: false}, actions: [{type: log}]}\n"), 0o600)
	rulesCmd([]string{"test", "-config", p2, "-by-me", "true"}, &out, &errOut)
	if !strings.Contains(out.String(), "✗ others  by_me: true") {
		t.Errorf("by-me:\n%s", out.String())
	}
	out.Reset()
	_ = os.WriteFile(p2, []byte("rules:\n  - {name: mine, watch: assignee = currentUser(), actions: [{type: log}]}\n"), 0o600)
	rulesCmd([]string{"list", "-config", p2}, &out, &errOut)
	rulesCmd([]string{"test", "-config", p2}, &out, &errOut)
	rulesCmd([]string{"test", "-config", p2, "-watch", "assignee = currentUser()"}, &out, &errOut)
	for _, want := range []string{"on any change of assignee = currentUser() every 5m", "✗ mine  watch: not from", "✓ mine  log"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("watch lacks %q:\n%s", want, out.String())
		}
	}
	if rulesCmd([]string{"bogus"}, &out, &errOut) != 2 {
		t.Error("bad subcommand exit")
	}
	out.Reset()
	p3 := filepath.Join(t.TempDir(), "j.yaml")
	_ = os.WriteFile(p3, []byte("rules:\n  - {name: close, actions: [{type: transition, to: Done}]}\n"), 0o600)
	rulesCmd([]string{"test", "-config", p3}, &out, &errOut)
	if !strings.Contains(out.String(), "only on others' changes") {
		t.Errorf("jira action note:\n%s", out.String())
	}
	out.Reset()
	rulesCmd([]string{"test", "-config", p3, "-by-me", "false"}, &out, &errOut)
	if !strings.Contains(out.String(), "✓ close  transition  → Done") {
		t.Errorf("jira action:\n%s", out.String())
	}
}
