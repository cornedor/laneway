package jira

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// TestDevInfoPartial: a tool whose detail fails leaves the others' items.
func TestDevInfoPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/rest/api/3/issue/A-1":
			io.WriteString(w, `{"id":"1"}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/summary":
			io.WriteString(w, `{"summary":{"branch":{"byInstanceType":{"GitHub":{"count":1},"GitLab":{"count":1}}}}}`)
		case q.Get("applicationType") == "GitHub":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			io.WriteString(w, `{"detail":[{"branches":[{"name":"issue/A-1","url":"https://g/b","repository":{"name":"web"}}]}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.DevInfo(context.Background(), "A-1")
	if err != nil || len(got) != 1 || got[0].Name != "issue/A-1" {
		t.Errorf("got %+v, %v", got, err)
	}
}

// TestDevInfoDetail: authors, reviewers, times, hashes and test summaries
// come through for the web's development panel.
func TestDevInfoDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/rest/api/3/issue/A-1":
			io.WriteString(w, `{"id":"1"}`)
		case r.URL.Path == "/rest/dev-status/latest/issue/summary":
			io.WriteString(w, `{"summary":{"pullrequest":{"byInstanceType":{"GitHub":{"count":1}}},"branch":{"byInstanceType":{"GitHub":{"count":1}}},"repository":{"byInstanceType":{"GitHub":{"count":1}}},"build":{"byInstanceType":{"cloud-providers":{"count":1}}},"deployment-environment":{"byInstanceType":{"cloud-providers":{"count":1}}}}}`)
		case q.Get("dataType") == "pullrequest":
			io.WriteString(w, `{"detail":[{"pullRequests":[{"name":"Fix","status":"OPEN","url":"u","repositoryName":"web","repositoryUrl":"https://g/web","source":{"branch":"b"},"destination":{"branch":"main"},
			  "author":{"name":"Ada","avatar":"https://a/ada"},"reviewers":[{"name":"Bo","approved":true},{"name":"Cy"}],"commentCount":3,"lastUpdate":"2026-03-18T12:14:00.000+0100"}]}]}`)
		case q.Get("dataType") == "branch":
			io.WriteString(w, `{"detail":[{"branches":[{"name":"b","url":"u","createPullRequestUrl":"https://g/new","repository":{"name":"web"},
			  "lastCommit":{"id":"abcdef1234","displayId":"abcdef1","message":"Tidy\n\nmore","authorTimestamp":1773832440000,"author":{"name":"Ada"}}}]}]}`)
		case q.Get("dataType") == "repository":
			io.WriteString(w, `{"detail":[{"repositories":[{"name":"web","url":"https://g/web","commits":[{"id":"abcdef1234","displayId":"abcdef1","message":"Tidy","url":"c","authorTimestamp":"2026-03-18T11:14:00Z","author":{"name":"Ada"}}]}]}]}`)
		case q.Get("dataType") == "build":
			io.WriteString(w, `{"detail":[{"builds":[{"displayName":"CI","state":"successful","lastUpdated":"2026-03-18T11:14:00Z","testSummary":{"totalNumber":10,"numberPassed":9,"numberFailed":0,"numberSkipped":1}}]}]}`)
		case q.Get("dataType") == "deployment-environment":
			io.WriteString(w, `{"detail":[{"deployments":[{"displayName":"D","state":"successful","duration":95,"lastUpdated":"2026-03-18T11:14:00Z","environment":{"displayName":"prod-eu","type":"production"}}]}]}`)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	got, err := c.DevInfo(context.Background(), "A-1")
	if err != nil || len(got) != 5 {
		t.Fatalf("got %+v, %v", got, err)
	}
	want := time.Date(2026, 3, 18, 11, 14, 0, 0, time.UTC)
	pr, build, dep, br, cm := got[0], got[1], got[2], got[3], got[4]
	if pr.Author != "Ada" || pr.AuthorAvatar != "https://a/ada" || len(pr.Reviewers) != 2 || !pr.Reviewers[0].Approved || pr.Reviewers[1].Approved ||
		pr.Comments != 3 || pr.Source != "b" || pr.Target != "main" || pr.RepoURL != "https://g/web" || !pr.Updated.Equal(want) {
		t.Errorf("pr = %+v", pr)
	}
	if build.Tests == nil || build.Tests.Passed != 9 || build.Tests.Skipped != 1 || !build.Updated.Equal(want) {
		t.Errorf("build = %+v", build)
	}
	if dep.Duration != 95 || dep.EnvType != "production" || dep.Branch != "prod-eu" || !dep.Updated.Equal(want) {
		t.Errorf("deploy = %+v", dep)
	}
	if br.Hash != "abcdef1234" || br.ShortHash != "abcdef1" || br.Message != "Tidy" || br.CreatePR != "https://g/new" || !br.Updated.Equal(want) {
		t.Errorf("branch = %+v", br)
	}
	if cm.Hash != "abcdef1234" || cm.Message != "Tidy" || cm.Author != "Ada" || !cm.Updated.Equal(want) {
		t.Errorf("commit = %+v", cm)
	}
}
