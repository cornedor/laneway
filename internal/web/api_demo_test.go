package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

func TestDemoRefusesMachineRoutes(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, UI: config.UIConfig{LLM: "echo"}, Demo: true}))
	defer ts.Close()
	do := func(method, path string) (int, string) {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if c, b := do("GET", "/api/agents/status"); c != 200 || !strings.Contains(b, `"Available":false`) {
		t.Errorf("status: %d %s", c, b)
	}
	if c, b := do("GET", "/api/worklog/proposals"); c != 200 || !strings.Contains(b, `"Items":[]`) {
		t.Errorf("proposals: %d %s", c, b)
	}
	for _, rt := range [][2]string{{"POST", "/api/agents/x/prompt"}, {"POST", "/api/actions/0/run"}, {"POST", "/api/issues/DEMO-1/ask"}, {"POST", "/api/issues/DEMO-1/work"}, {"POST", "/api/issues/DEMO-1/pr"}} {
		if c, b := do(rt[0], rt[1]); c != 503 || !strings.Contains(b, "not available in demo") {
			t.Errorf("%v: %d %s", rt, c, b)
		}
	}
}
