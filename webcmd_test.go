package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/web"
)

// TestWebSetupWhen: the setup screen shows without a config, without
// jira:, and for a site missing its token (filled in but that).
func TestWebSetupWhen(t *testing.T) {
	if _, ok := webSetup(config.Config{}, "c.yaml", "", true); !ok {
		t.Error("no config: no setup")
	}
	if _, ok := webSetup(config.Config{}, "c.yaml", "", false); !ok {
		t.Error("no jira: no setup")
	}
	full := config.Config{Jira: config.JiraConfig{BaseURL: "https://acme.atlassian.net", Email: "a@acme.io", APIToken: "x"}}
	if _, ok := webSetup(full, "c.yaml", "", false); ok {
		t.Error("a full site: setup")
	}
	half := config.Config{Jira: config.JiraConfig{BaseURL: "https://acme.atlassian.net", Email: "a@acme.io"}}
	st, ok := webSetup(half, "c.yaml", "", false)
	if !ok || st.Prefill.Site != "https://acme.atlassian.net" || st.Prefill.Email != "a@acme.io" || st.ConfigPath != "c.yaml" {
		t.Errorf("no token: %v %+v", ok, st)
	}
	if _, ok := webSetup(full, "c.yaml", "nope", false); ok {
		t.Error("an unknown -site: setup instead of its error")
	}
}

// TestWebSetupSave: each refusal names its field; a sign-in writes jira:.
func TestWebSetupSave(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "")
	jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, tok, _ := r.BasicAuth(); r.URL.Path != "/rest/api/3/myself" || tok != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"accountId":"1","displayName":"Ada Lovelace"}`))
	}))
	defer jiraSrv.Close()
	path := filepath.Join(t.TempDir(), "laneway", "config.yaml")
	ctx := context.Background()
	for _, c := range []struct {
		f     web.SetupForm
		field string
	}{
		{web.SetupForm{Site: " ", Email: "a@b.c", Token: "good"}, "site"},
		{web.SetupForm{Site: jiraSrv.URL, Email: "ada", Token: "good"}, "email"},
		{web.SetupForm{Site: jiraSrv.URL, Email: "a@b.c"}, "token"},
		{web.SetupForm{Site: jiraSrv.URL, Email: "a@b.c", Token: "bad"}, "token"},
		{web.SetupForm{Site: "http://127.0.0.1:1", Email: "a@b.c", Token: "good"}, "site"},
	} {
		var fe web.FieldError
		if _, err := webSetupSave(ctx, path, "", c.f); !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%+v: %v, want a %s error", c.f, err, c.field)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a refused form wrote the config")
	}
	who, err := webSetupSave(ctx, path, "", web.SetupForm{Site: jiraSrv.URL + "/", Email: " ada@acme.io ", Token: "good"})
	if err != nil || who != "Ada Lovelace" {
		t.Fatalf("save = %q %v", who, err)
	}
	cfg, _, err := config.Load(path)
	if err != nil || cfg.Jira.BaseURL != jiraSrv.URL || cfg.Jira.Email != "ada@acme.io" || cfg.Jira.APIToken != "good" {
		t.Errorf("config = %+v %v", cfg.Jira, err)
	}
}

// TestWebAddSite: a taken or bad name and an address set up already are
// refused before signing in; the added site is named after its address
// and becomes the last site.
func TestWebAddSite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("JIRA_API_TOKEN", "")
	jiraSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accountId":"1","displayName":"Ada Lovelace"}`))
	}))
	defer jiraSrv.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("jira:\n  base_url: https://acme.atlassian.net\n  email: a@acme.io\n  api_token: x\nsites:\n  work:\n    base_url: https://work.atlassian.net\n    email: a@work.io\n    api_token: y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, c := range []struct {
		name, site, field string
	}{
		{"", "acme", "site"},
		{"x", "https://work.atlassian.net/", "site"},
		{"Big Club", jiraSrv.URL, "name"},
		{"work", jiraSrv.URL, "name"},
	} {
		var fe web.FieldError
		if _, _, err := webAddSite(ctx, cfg, path, c.name, web.SetupForm{Site: c.site, Email: "a@b.c", Token: "t"}); !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s %s: %v, want a %s error", c.name, c.site, err, c.field)
		}
	}
	who, name, err := webAddSite(ctx, cfg, path, " club ", web.SetupForm{Site: jiraSrv.URL, Email: "ada@club.io", Token: "t"})
	if err != nil || who != "Ada Lovelace" || name != "club" {
		t.Fatalf("add = %q %q %v", who, name, err)
	}
	cfg, _, _ = config.Load(path)
	if j, err := cfg.Site("club"); err != nil || j.BaseURL != jiraSrv.URL || cfg.Jira.BaseURL != "https://acme.atlassian.net" {
		t.Errorf("club = %+v %v", j, err)
	}
	if got := config.LastSite(cfg.SiteNames()); got != "club" {
		t.Errorf("last site = %q", got)
	}
}

// TestRunningWeb: only a laneway answering /api/session counts.
func TestRunningWeb(t *testing.T) {
	lw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"dev"}`))
	}))
	defer lw.Close()
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	if !runningWeb(strings.TrimPrefix(lw.URL, "http://")) || runningWeb(strings.TrimPrefix(other.URL, "http://")) {
		t.Error("runningWeb wrong")
	}
}

// TestWebAutostartArgv: the same address and an absolute -config.
func TestWebAutostartArgv(t *testing.T) {
	a := webAutostart("c.yaml", "127.0.0.1:9000")
	if a == nil || len(a.Argv) != 7 || !filepath.IsAbs(a.Argv[0]) || strings.Join(a.Argv[1:5], " ") != "web -no-open -addr 127.0.0.1:9000" || !filepath.IsAbs(a.Argv[6]) {
		t.Errorf("argv = %+v", a)
	}
}
