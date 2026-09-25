package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDevInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/rest/api/3/issue/A-1":
			io.WriteString(w, `{"id":"10042"}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/summary" && q.Get("issueId") == "10042":
			io.WriteString(w, `{"summary":{"pullrequest":{"byInstanceType":{"GitLab":{"count":2}}},"branch":{"byInstanceType":{"GitLab":{"count":1},"GitHub":{"count":0}}}}}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("applicationType") == "GitLab" && q.Get("dataType") == "pullrequest":
			io.WriteString(w, `{"detail":[{"pullRequests":[
			  {"name":"Old fix","status":"MERGED","url":"https://g/1","source":{"branch":"a"},"destination":{"branch":"main"}},
			  {"name":"Fix login","status":"OPEN","url":"https://g/2","repositoryName":"web","source":{"branch":"issue/A-1"},"destination":{"branch":"main"}}]}]}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("dataType") == "branch":
			io.WriteString(w, `{"detail":[{"branches":[{"name":"issue/A-1","url":"https://g/b","repository":{"name":"web"}}]}]}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.DevInfo(context.Background(), "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "Fix login" || got[0].Branch != "issue/A-1 → main" || got[1].Status != "MERGED" || got[2].Kind != "branch" {
		t.Errorf("items = %+v", got)
	}
}
