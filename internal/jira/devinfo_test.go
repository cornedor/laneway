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
			io.WriteString(w, `{"summary":{"pullrequest":{"byInstanceType":{"GitLab":{"count":2}}},"branch":{"byInstanceType":{"GitLab":{"count":1},"GitHub":{"count":0}}},"repository":{"byInstanceType":{"GitLab":{"count":1}}},"build":{"byInstanceType":{"cloud-providers":{"count":1}}},"deployment-environment":{"byInstanceType":{"cloud-providers":{"count":1}}}}}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("dataType") == "repository":
			io.WriteString(w, `{"detail":[{"repositories":[{"name":"web","commits":[{"displayId":"a1b2c3d","message":"Fix login\n\nlonger text","url":"https://g/c","author":{"name":"Ada"}}]}]}]}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("applicationType") == "GitLab" && q.Get("dataType") == "pullrequest":
			io.WriteString(w, `{"detail":[{"pullRequests":[
			  {"name":"Old fix","status":"MERGED","url":"https://g/1","source":{"branch":"a"},"destination":{"branch":"main"}},
			  {"name":"Fix login","status":"OPEN","url":"https://g/2","repositoryName":"web","source":{"branch":"issue/A-1"},"destination":{"branch":"main"}}]}]}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("applicationType") == "cloud-providers" && q.Get("dataType") == "build":
			io.WriteString(w, `{"detail":[{"builds":[{"displayName":"CI","buildNumber":42,"state":"failed","url":"https://g/p/42","references":[{"ref":{"name":"issue/A-1"}}]}]}]}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/detail" && q.Get("dataType") == "deployment-environment":
			io.WriteString(w, `{"detail":[{"deployments":[{"displayName":"Deploy 7","state":"successful","url":"https://g/d/7","environment":{"displayName":"production","type":"production"},"pipeline":{"displayName":"web deploy"}}]}]}`)
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
	if len(got) != 6 || got[0].Name != "Fix login" || got[0].Branch != "issue/A-1 → main" || got[1].Status != "MERGED" ||
		got[2].Kind != "build" || got[2].Name != "CI #42" || got[2].Status != "FAILED" || got[2].Branch != "issue/A-1" ||
		got[3].Kind != "deploy" || got[3].Name != "web deploy" || got[3].Status != "SUCCESSFUL" || got[3].Branch != "production" ||
		got[4].Kind != "branch" || got[5].Kind != "commit" || got[5].Name != "a1b2c3d Fix login" || got[5].Status != "Ada" {
		t.Errorf("items = %+v", got)
	}
}
