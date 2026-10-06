package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/herdr"
	"github.com/cornedor/laneway/internal/jira"
)

func secReq(s *Server, method, target, host string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestHostCheck(t *testing.T) {
	s := New(context.Background(), Options{AllowedHosts: []string{"lan.example:8484", "box"}})
	for host, want := range map[string]int{
		"127.0.0.1:8484": 200, "localhost:1": 200, "[::1]:8484": 200, "a.localhost:80": 200, "127.9.9.9": 200,
		"evil.example:8484": 403, "evil.example": 403, "lan.example:8484": 200, "lan.example:9": 403,
		"box:1234": 200, "box": 200, "10.0.0.5:8484": 403,
	} {
		if got := secReq(s, "GET", "/api/actions", host, nil).Code; (got == 403) != (want == 403) {
			t.Errorf("Host %q = %d, want %d", host, got, want)
		}
	}
	// the rebinding attack: same Host as Origin, but not ours
	if c := secReq(s, "PUT", "/api/settings/actions", "evil.example", map[string]string{"Origin": "http://evil.example"}).Code; c != 403 {
		t.Errorf("rebound PUT = %d", c)
	}
}

func TestAPIGetCrossSite(t *testing.T) {
	s := New(context.Background(), Options{})
	for _, h := range []map[string]string{{"Origin": "http://evil.example"}, {"Sec-Fetch-Site": "cross-site"}, {"Sec-Fetch-Site": "same-site"}} {
		if c := secReq(s, "GET", "/api/actions", "localhost:8484", h).Code; c != 403 {
			t.Errorf("%v = %d", h, c)
		}
	}
	for _, h := range []map[string]string{nil, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "none"}, {"Origin": "http://localhost:8484"}} {
		if c := secReq(s, "GET", "/api/actions", "localhost:8484", h).Code; c == 403 {
			t.Errorf("%v refused", h)
		}
	}
}

func TestLaunchToken(t *testing.T) {
	s := New(context.Background(), Options{Token: "sekret"})
	if c := secReq(s, "GET", "/", "localhost", nil).Code; c != 401 {
		t.Errorf("no token = %d", c)
	}
	if c := secReq(s, "GET", "/?token=nope", "localhost", nil).Code; c != 401 {
		t.Errorf("wrong token = %d", c)
	}
	if c := secReq(s, "GET", "/api/actions", "localhost", map[string]string{"Cookie": "laneway_token=nope"}).Code; c != 401 {
		t.Errorf("wrong cookie = %d", c)
	}
	w := secReq(s, "GET", "/?token=sekret&x=1", "localhost", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/?x=1" {
		t.Fatalf("exchange = %d %q", w.Code, w.Header().Get("Location"))
	}
	ck := w.Result().Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v", ck)
	}
	if c := secReq(s, "GET", "/api/actions", "localhost", map[string]string{"Cookie": ck[0].Name + "=" + ck[0].Value}).Code; c != 200 {
		t.Errorf("with cookie = %d", c)
	}
}

func TestSSEWriterSplitRune(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &sseWriter{w: rec}
	b := []byte("é€")
	for i := range b { // one byte per write
		if _, err := sw.Write(b[i : i+1]); err != nil {
			t.Fatal(err)
		}
	}
	_ = sw.Flush()
	body := rec.Body.String()
	if strings.Contains(body, "\\ufffd") || !utf8.ValidString(body) || !strings.Contains(body, `data: "é"`) || !strings.Contains(body, `data: "€"`) {
		t.Errorf("body = %q", body)
	}
}

func TestAskTextRules(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, UI: config.UIConfig{LLM: "echo"}}))
	defer ts.Close()
	for _, text := range []string{"--dangerously-skip-permissions", " -p x", strings.Repeat("a", maxQuestion+1)} {
		if c := issueCall(t, "POST", ts.URL+"/api/issues/DEMO-1/ask", map[string]string{"Text": text}, nil); c != 400 {
			t.Errorf("%.20q = %d", text, c)
		}
	}
	if c := issueCall(t, "POST", ts.URL+"/api/issues/DEMO-1/ask", map[string]string{"Text": "what is it?"}, nil); c != 200 {
		t.Errorf("fine question = %d", c)
	}
}

func TestPaneAndWorkInputs(t *testing.T) {
	f, c := newFakeHerdr(t)
	old := herdrClient
	herdrClient = func() *herdr.Client { return c }
	defer func() { herdrClient = old }()
	ts := agentsServer(t, gitRepo(t))
	for _, p := range []string{"--help", "nope", "p1%20--x"} {
		for _, m := range []string{"prompt", "focus", "stop", "new"} {
			if code := workCall(t, "POST", ts.URL+"/api/agents/"+p+"/"+m, `{"Text":"x"}`, nil); code != 400 && code != 404 {
				t.Errorf("%s %s = %d", m, p, code)
			}
		}
		if code := workCall(t, "GET", ts.URL+"/api/agents/"+p+"/output", "", nil); code != 400 && code != 404 {
			t.Errorf("output %s = %d", p, code)
		}
	}
	for _, body := range []string{`{"Branch":"-x"}`, `{"Branch":"a b"}`, `{"Branch":"a..b"}`, `{"Agent":"rm"}`, `{"Agent":"--foo"}`} {
		if code := workCall(t, "POST", ts.URL+"/api/issues/DEMO-5/work", body, nil); code != 400 {
			t.Errorf("%s = %d", body, code)
		}
	}
	if code := workCall(t, "POST", ts.URL+"/api/agents/p1/new", `{"Agent":"rm"}`, nil); code != 400 {
		t.Errorf("new with bad agent = %d", code)
	}
	if f.called("worktree.create") || f.called("agent.start") {
		t.Error("herdr was reached")
	}
}

func TestServeRemoteNeedsTLS(t *testing.T) {
	h := http.NotFoundHandler()
	if err := Serve(t.Context(), "0.0.0.0:0", false, "", "", h, nil); err == nil || !strings.Contains(err.Error(), "-remote") {
		t.Errorf("public without -remote: %v", err)
	}
	if err := Serve(t.Context(), "0.0.0.0:0", true, "", "", h, nil); err == nil || !strings.Contains(err.Error(), "-cert") {
		t.Errorf("-remote without TLS: %v", err)
	}
	if err := Serve(t.Context(), "0.0.0.0:0", true, "missing.pem", "missing.key", h, nil); err == nil {
		t.Error("-remote with a missing certificate served")
	}
}
