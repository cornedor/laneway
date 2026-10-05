package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStatePathMigratesOldState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(d, "jiratui", "state.json")
	_ = os.MkdirAll(filepath.Dir(old), 0o700)
	_ = os.WriteFile(old, []byte(`{"meta":{}}`), 0o600)
	p, err := StatePath()
	if err != nil || p != filepath.Join(d, "laneway", "state.json") {
		t.Fatalf("path = %q, %v", p, err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != `{"meta":{}}` {
		t.Errorf("migrated = %q, %v", b, err)
	}
}

func TestLoadFallsBackToJiratui(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, _ := os.UserConfigDir()
	p := filepath.Join(d, "jiratui", "config.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("jira:\n  base_url: https://x.test\n"), 0o600)
	c, got, err := Load("")
	if err != nil || got != p || c.Jira.BaseURL != "https://x.test" {
		t.Errorf("Load = %+v %q %v", c.Jira, got, err)
	}
}

// TestMatterboxRulesIgnored: matterbox's rules: are chat rules, not ours.
func TestMatterboxRulesIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	d, _ := os.UserConfigDir()
	p := filepath.Join(d, "matterbox", "config.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("jira:\n  base_url: https://x.test\nrules:\n  - match: {channel: Ops, frequency: {count: 3}}\n    actions: [{type: send, text: hi}]\n"), 0o600)
	c, _, err := Load("")
	if err != nil || c.Jira.BaseURL != "https://x.test" || c.Rules != nil {
		t.Errorf("Load = %+v rules %v, %v", c.Jira, c.Rules, err)
	}
}

func TestRequestTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "45s": 45 * time.Second, " 1m ": time.Minute} {
		if got, err := (JiraConfig{Timeout: in}).RequestTimeout(); err != nil || got != want {
			t.Errorf("%q = %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"soon", "10ms"} {
		if _, err := (JiraConfig{Timeout: bad}).RequestTimeout(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// TestUnknownKeys: a key no option reads warns, with the nearest known one.
func TestUnknownKeys(t *testing.T) {
	raw := []byte("jira:\n  base_url: x\n  emial: a@b\nui:\n  panel_widht: 40\n  card_colors: off\n  keys: {search: f}\nsites:\n  work:\n    api_tokn: t\ngitlab:\n  - base_url: x\n    tokn: t\nbogus: 1\n")
	got := unknownKeys(raw)
	want := []string{
		"bogus: unknown option",
		"jira.emial: unknown option, did you mean email?",
		"ui.panel_widht: unknown option, did you mean panel_width?",
		"sites.work.api_tokn: unknown option, did you mean api_token?",
		"gitlab[0].tokn: unknown option, did you mean token?",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestCheck: a site's config names what it lacks, and a base_url without
// its scheme.
func TestCheck(t *testing.T) {
	for _, c := range []struct {
		j    JiraConfig
		site string
		want string
	}{
		{JiraConfig{BaseURL: "https://x.atlassian.net", Email: "a@b", APIToken: "t"}, "jira", ""},
		{JiraConfig{BaseURL: "https://x.atlassian.net"}, "jira", "jira: set email, api_token (or api_token_cmd, JIRA_API_TOKEN"},
		{JiraConfig{Email: "a@b", APIToken: "t"}, "sites.club", "sites.club: set base_url"},
		{JiraConfig{BaseURL: "x.atlassian.net", Email: "a@b", APIToken: "t"}, "jira", `jira.base_url: "x.atlassian.net" needs its scheme, e.g. https://x.atlassian.net`},
	} {
		err := c.j.Check(c.site)
		if got := fmt.Sprint(err); c.want == "" && err != nil || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%+v: %v, want %q", c.j, err, c.want)
		}
	}
}

// TestNoConfig: no config file says what to write and where a token is.
func TestNoConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	_, _, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "base_url: https://") || !strings.Contains(err.Error(), TokenURL) {
		t.Errorf("got %v", err)
	}
}

// TestAPITokenCmd: without api_token, the command's output is the token.
func TestAPITokenCmd(t *testing.T) {
	c := Config{Sites: map[string]JiraConfig{
		"club": {BaseURL: "https://club.test", APITokenCmd: []string{"echo", " tok-1 "}},
		"set":  {APIToken: "kept", APITokenCmd: []string{"false"}},
		"bad":  {APITokenCmd: []string{"false"}},
	}}
	if j, err := c.Site("club"); err != nil || j.APIToken != "tok-1" {
		t.Errorf("club: %q %v", j.APIToken, err)
	}
	if j, err := c.Site("set"); err != nil || j.APIToken != "kept" {
		t.Errorf("an api_token wins: %q %v", j.APIToken, err)
	}
	if _, err := c.Site("bad"); err == nil || !strings.Contains(err.Error(), "api_token_cmd false") {
		t.Errorf("a failing command: %v", err)
	}
	if g, err := (GitLabConfig{TokenCmd: []string{"echo", "gl-1"}}).WithToken(); err != nil || g.Token != "gl-1" {
		t.Errorf("gitlab token_cmd: %q %v", g.Token, err)
	}
}

// TestBadValues: a value of the wrong type is skipped with a warning, the
// rest still loads, and a lone value where a list goes is a list of one.
func TestBadValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	raw := "jira:\n  base_url: https://x.test\n  projects: ABC\nui:\n  home: work\n  card_limit: lots\n  panel_width: 40\n  keys: {search: [f, {x: 1}]}\n  card_styles:\n    - when: a\n      fade: maybe\ngitlab:\n  base_url: https://g.test\nsites:\n  w:\n    projects: [A, B]\n    timeout: [1]\nrules:\n  - name: r\n    on: status\n"
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Jira.Projects, []string{"ABC"}) || !slices.Equal(c.UI.Home, []string{"work"}) || !slices.Equal(c.Sites["w"].Projects, []string{"A", "B"}) {
		t.Errorf("lone values: %q %q %q", c.Jira.Projects, c.UI.Home, c.Sites["w"].Projects)
	}
	if c.Jira.BaseURL != "https://x.test" || c.UI.PanelWidth != 40 || c.UI.CardLimit != 0 || len(c.GitLab) != 0 || len(c.Rules) != 1 || len(c.UI.CardStyles) != 1 || c.UI.CardStyles[0].When != "a" {
		t.Errorf("the rest: %+v %+v %+v", c.Jira, c.UI, c.GitLab)
	}
	want := []string{
		`ui.card_limit: wants a number, not "lots"; ignored`,
		`ui.keys.search: wants a list of text, not a list; ignored`,
		`ui.card_styles[0].fade: wants true or false, not "maybe"; ignored`,
		`gitlab: wants a list, not key: value pairs; ignored`,
		`sites.w.timeout: wants text, not a list; ignored`,
	}
	if !slices.Equal(c.Warnings, want) {
		t.Errorf("got %q\nwant %q", c.Warnings, want)
	}
	if err := os.WriteFile(p, []byte("ui: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil {
		t.Error("YAML that does not parse loaded")
	}
}
