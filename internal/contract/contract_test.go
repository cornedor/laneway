package contract

import (
	"context"
	"flag"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

var (
	record  = flag.String("record", "", "re-record testdata/jira.shape from this site in the laneway config (read-only)")
	project = flag.String("project", "LAN", "the project -record reads")
)

const shapeFile = "testdata/jira.shape"

// probeThrough runs the probe on project through a proxy to target.
func probeThrough(t *testing.T, target, email, token, project string) *Proxy {
	t.Helper()
	p, err := NewProxy(target)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p)
	defer srv.Close()
	c := jira.New(jira.Config{BaseURL: srv.URL, Email: email, APIToken: token, Projects: []string{project}, Timeout: time.Minute})
	for _, err := range Probe(context.Background(), c, project) {
		t.Log(err)
	}
	if r := p.Refused(); len(r) > 0 {
		t.Errorf("the probe tried to write: %v", r)
	}
	return p
}

// TestRecord: with -record <site>, re-records Jira's outline into
// testdata. Only reads leave; the proxy refuses the rest.
//
//	go test ./internal/contract -run TestRecord -record corne-team -project LAN -v
func TestRecord(t *testing.T) {
	if *record == "" {
		t.Skip("-record <site> re-records " + shapeFile)
	}
	cfg, _, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	site, err := cfg.Site(*record)
	if err != nil {
		t.Fatal(err)
	}
	p := probeThrough(t, site.BaseURL, site.Email, site.APIToken, *project)
	f, err := os.Create(shapeFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := "Jira Cloud's answers to the app's reads: key paths and types, no values.\n" +
		"Re-record: go test ./internal/contract -run TestRecord -record <site> -project <key>\n" +
		"Recorded " + time.Now().Format(time.DateOnly) + "."
	if err := p.Doc().Write(f, header); err != nil {
		t.Fatal(err)
	}
}

// TestDemoMatchesJira: the demo answers the app's reads only with keys and
// types Jira's recording has. A difference that is fine goes in
// testdata/allow.txt with its reason.
func TestDemoMatchesJira(t *testing.T) {
	f, err := os.Open(shapeFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recorded, err := ReadDoc(f)
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.Open("testdata/allow.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	allow, err := ReadAllow(a)
	if err != nil {
		t.Fatal(err)
	}

	url, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	p := probeThrough(t, url, "demo@example.com", "demo", "DEMO")

	r := Compare(recorded, p.Doc(), allow)
	if len(r.Unverified) > 0 {
		t.Logf("unverified, Jira's recording can't tell:\n\t%s", strings.Join(r.Unverified, "\n\t"))
	}
	if len(r.Missing) > 0 {
		t.Logf("Jira answered, the demo didn't:\n\t%s", strings.Join(r.Missing, "\n\t"))
	}
	if len(r.Wrong) > 0 {
		t.Errorf("the demo answers what Jira doesn't:\n\t%s", strings.Join(r.Wrong, "\n\t"))
	}
}
