package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/store"
)

func issueServer(t *testing.T, base string) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := jira.New(jira.Config{BaseURL: base, Email: "d@example.com", APIToken: "x", Projects: []string{"DEMO"}})
	ts := httptest.NewServer(New(context.Background(), Options{Client: cl, Jira: config.JiraConfig{}, Store: st, Site: "demo", Demo: true}))
	t.Cleanup(ts.Close)
	return ts
}

func issueCall(t *testing.T, method, url string, body, out any) int {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func TestIssuePanelRoutes(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	var cards struct{ Cards []jira.Card }
	if issueCall(t, "GET", ts.URL+"/api/search?jql=project%20%3D%20DEMO", nil, &cards) != 200 || len(cards.Cards) == 0 {
		t.Fatal("no demo cards")
	}
	key := cards.Cards[0].Key
	iu := ts.URL + "/api/issues/" + key

	var card jira.Card
	if code := issueCall(t, "GET", iu+"/card", nil, &card); code != 200 || card.Key != key {
		t.Fatalf("card: %d %q", code, card.Key)
	}
	var ed editable
	if code := issueCall(t, "GET", iu+"/description", nil, &ed); code != 200 || !ed.Editable {
		t.Fatalf("description: %d %+v", code, ed)
	}
	if code := issueCall(t, "PUT", iu+"/description", mdBody{Markdown: "hello **world**", Kept: ed.Kept}, nil); code != 200 {
		t.Fatalf("set description: %d", code)
	}

	if code := issueCall(t, "POST", iu+"/comments", map[string]any{"Markdown": "  "}, nil); code != 400 {
		t.Errorf("empty comment: %d, want 400", code)
	}
	if code := issueCall(t, "POST", iu+"/comments", map[string]any{"Markdown": "hi @Mira Jansen", "Mentions": []jira.Mention{{AccountID: "demo-0002", DisplayName: "Mira Jansen"}}}, nil); code != 200 {
		t.Fatalf("add comment: %d", code)
	}
	var is jira.Issue
	issueCall(t, "GET", iu+"?fresh=1", nil, &is)
	if len(is.Comments) == 0 {
		t.Fatal("comment not listed")
	}
	id := is.Comments[len(is.Comments)-1].ID
	if code := issueCall(t, "GET", iu+"/comments/"+id+"/edit", nil, &ed); code != 200 || !ed.Editable {
		t.Fatalf("comment edit: %d %+v", code, ed)
	}
	if code := issueCall(t, "PUT", iu+"/comments/"+id, mdBody{Markdown: "edited", Kept: ed.Kept}, nil); code != 200 {
		t.Fatalf("edit comment: %d", code)
	}
	if code := issueCall(t, "DELETE", iu+"/comments/"+id, nil, nil); code != 200 {
		t.Fatalf("delete comment: %d", code)
	}
	for _, p := range []string{"/history", "/children", "/weblinks", "/timeinstatus", "/dev"} {
		if code := issueCall(t, "GET", iu+p, nil, nil); code != 200 {
			t.Errorf("%s: %d", p, code)
		}
	}
	if code := issueCall(t, "GET", ts.URL+"/api/issues/nope/history", nil, nil); code != 400 {
		t.Errorf("bad key: %d, want 400", code)
	}
}

func TestAttachmentProxy(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	files := map[string][]byte{"1": png, "2": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/rest/api/3/attachment/content/"):]
		if b, ok := files[id]; ok && r.Header.Get("Authorization") != "" {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer fake.Close()
	ts := issueServer(t, fake.URL)

	res, err := http.Get(ts.URL + "/api/attachments/1")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" || res.Header.Get("Content-Disposition") != "" {
		t.Errorf("png: %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
	}
	res, _ = http.Get(ts.URL + "/api/attachments/2?name=a/b.svg")
	res.Body.Close()
	if res.Header.Get("Content-Type") != "application/octet-stream" || res.Header.Get("Content-Disposition") == "" {
		t.Errorf("svg must download: %q %q", res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
	}
	if code := issueCall(t, "GET", ts.URL+"/api/attachments/9", nil, nil); code == 200 {
		t.Error("missing attachment answered 200")
	}
	if code := issueCall(t, "GET", ts.URL+"/api/attachments/a.b", nil, nil); code != 400 {
		t.Errorf("bad id: %d, want 400", code)
	}
}
