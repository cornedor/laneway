package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
