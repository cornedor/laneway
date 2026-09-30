package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

func TestEmptySlicesHelper(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	writeJSON(rec, req, jira.Issue{})
	var iss jira.Issue
	if err := json.Unmarshal(rec.Body.Bytes(), &iss); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Links", "Attachments", "Comments", "Labels"} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"`+k+`":[]`)) {
			t.Errorf("%s not []: %s", k, rec.Body.String())
		}
	}
}

func TestDemoIssuesNoNullSlices(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	var cards struct{ Cards []jira.Card }
	if issueCall(t, "GET", ts.URL+"/api/search?jql=project%20%3D%20DEMO", nil, &cards) != 200 {
		t.Fatal("search")
	}
	bad := regexp.MustCompile(`"(Links|Attachments|Comments|Labels|Watches|Worklogs|Transitions)":null`)
	for _, c := range cards.Cards {
		for _, p := range []string{"", "/comments", "/worklogs", "/transitions"} {
			res, err := http.Get(ts.URL + "/api/issues/" + c.Key + p)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if m := bad.Find(b); m != nil {
				t.Errorf("%s%s: %s", c.Key, p, m)
			}
		}
	}
	if m := bad.Find(mustGet(t, ts.URL+"/api/search?jql=project%20%3D%20DEMO")); m != nil {
		t.Errorf("search: %s", m)
	}
}

func mustGet(t *testing.T, u string) []byte {
	t.Helper()
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return b
}
