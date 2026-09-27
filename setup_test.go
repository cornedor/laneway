package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
)

// scripted answers setup's questions from lines, the token ones too, and
// records the pages it opened and the logins it checked.
type scripted struct {
	out    bytes.Buffer
	opened []string
	tried  []config.JiraConfig
	fail   int // how many sign-ins fail first
}

func (s *scripted) prompter(lines ...string) prompter {
	in := bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	return prompter{in: in, out: &s.out,
		secret: func() (string, error) {
			line, err := in.ReadString('\n')
			if err != nil {
				return "", errCancelled
			}
			return strings.TrimSpace(line), nil
		},
		open: func(u string) error { s.opened = append(s.opened, u); return nil },
	}
}

func (s *scripted) verify(_ context.Context, j config.JiraConfig) (string, error) {
	s.tried = append(s.tried, j)
	if len(s.tried) <= s.fail {
		return "", errors.New("jira: not authorized")
	}
	return "Ada Lovelace", nil
}

func TestSetupFirstThenAnother(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	path := filepath.Join(t.TempDir(), "laneway", "config.yaml")
	s := &scripted{}
	name, err := setup(context.Background(), path, s.prompter("acme", "ada@acme.io", "tok1"), s.verify)
	if err != nil || name != "" {
		t.Fatalf("first = %q %v\n%s", name, err, s.out.String())
	}
	// The second one goes under sites:, the email offered from jira:.
	name, err = setup(context.Background(), path, s.prompter("club.atlassian.net", "", "tok2", ""), s.verify)
	if err != nil || name != "club" {
		t.Fatalf("second = %q %v\n%s", name, err, s.out.String())
	}
	c, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := config.JiraConfig{BaseURL: "https://club.atlassian.net", Email: "ada@acme.io", APIToken: "tok2"}
	if c.Jira.BaseURL != "https://acme.atlassian.net" || c.Jira.APIToken != "tok1" || c.Sites["club"].BaseURL != want.BaseURL ||
		c.Sites["club"].Email != want.Email || c.Sites["club"].APIToken != want.APIToken {
		t.Errorf("config = %+v", c)
	}
	if !strings.Contains(s.out.String(), "laneway -site club") {
		t.Errorf("no hint to start on it:\n%s", s.out.String())
	}
}

// TestSetupRetry: a failed sign-in asks again, enter keeping the answers;
// a taken or bad name is asked again too.
func TestSetupRetry(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira: {base_url: https://a.atlassian.net, email: a@a, api_token: x}\nsites:\n  b: {base_url: https://b.atlassian.net}\n"), 0o600)
	s := &scripted{fail: 1}
	name, err := setup(context.Background(), path, s.prompter("https://b2.atlassian.net", "me@b", "wrong", "", "", "right", "B 2", "b", "b2"), s.verify)
	if err != nil || name != "b2" {
		t.Fatalf("setup = %q %v\n%s", name, err, s.out.String())
	}
	if len(s.tried) != 2 || s.tried[1].Email != "me@b" || s.tried[1].APIToken != "right" {
		t.Errorf("tried %+v", s.tried)
	}
	if out := s.out.String(); !strings.Contains(out, "✗ jira: not authorized") || !strings.Contains(out, "there is a site b already") {
		t.Errorf("out:\n%s", out)
	}
}

// TestSetupReplacesToken: a site already there keeps its name and gets
// the new email and token, its other keys kept.
func TestSetupReplacesToken(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira: {base_url: https://a.atlassian.net, email: a@a, api_token: x}\nsites:\n  b: {base_url: https://b.atlassian.net, email: me@b, api_token: expired, timeout: 30s}\n"), 0o600)
	s := &scripted{}
	name, err := setup(context.Background(), path, s.prompter("b", "", "fresh"), s.verify)
	if err != nil || name != "b" {
		t.Fatalf("setup = %q %v\n%s", name, err, s.out.String())
	}
	c, _, _ := config.Load(path)
	if b := c.Sites["b"]; b.APIToken != "fresh" || b.Email != "me@b" || b.Timeout != "30s" || len(c.Sites) != 1 {
		t.Errorf("sites = %+v", c.Sites)
	}
}

// TestSetupEnvToken: with JIRA_API_TOKEN, enter at the token uses it and
// the file gets none; enter without one opens the token page first.
func TestSetupEnvToken(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "from-env")
	path := filepath.Join(t.TempDir(), "config.yaml")
	s := &scripted{}
	if _, err := setup(context.Background(), path, s.prompter("acme", "a@a", ""), s.verify); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); len(s.tried) != 1 || s.tried[0].APIToken != "from-env" || strings.Contains(string(raw), "api_token") {
		t.Errorf("tried %+v, file:\n%s", s.tried, raw)
	}

	t.Setenv("JIRA_API_TOKEN", "")
	path = filepath.Join(t.TempDir(), "config.yaml")
	s = &scripted{}
	if _, err := setup(context.Background(), path, s.prompter("acme", "a@a", "", "tok"), s.verify); err != nil {
		t.Fatal(err)
	}
	if len(s.opened) != 1 || s.opened[0] != config.TokenURL || s.tried[0].APIToken != "tok" {
		t.Errorf("opened %v tried %+v", s.opened, s.tried)
	}
}

// TestSetupCancel: ctrl+d (end of input) writes nothing.
func TestSetupCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	s := &scripted{}
	in := bufio.NewReader(strings.NewReader("acme\n"))
	p := s.prompter()
	p.in = in
	if _, err := setup(context.Background(), path, p, s.verify); !errors.Is(err, errCancelled) {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a cancelled setup wrote the config")
	}
}
