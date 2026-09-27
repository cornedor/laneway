package ui

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/jira"
)

// TestOfflineWrites: an edit offline is kept (⇡1), sent when Jira is back;
// one whose issue changed since waits for the palette's send.
func TestOfflineWrites(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := "http://" + l.Addr().String()
	l.Close()
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: dead, Email: "me@x.test", APIToken: "tok"})
	m.jiraClient.SetQueue(queueTo(m.store))
	c := m.jiraClient
	out, _ := m.Update(jiraMutateCmd("ABC-1", "summary", func() error { return c.SetSummary(m.ctx, "ABC-1", "New") })())
	m = out.(Model)
	if m.queued != 1 || !strings.Contains(m.status, "offline: ABC-1 summary kept for later") || !strings.Contains(ansi.Strip(m.View().Content), "⇡1") {
		t.Fatalf("queued %d, status %q", m.queued, m.status)
	}
	updated := "2000-01-01T00:00:00.000+0000"
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"fields":{"updated":"`+updated+`"}}`)
			return
		}
		sent = append(sent, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m.jiraClient = jira.New(jira.Config{BaseURL: srv.URL, Email: "me@x.test", APIToken: "tok"})
	updated = "2999-01-01T00:00:00.000+0000" // changed in Jira since
	out, _ = m.Update(m.replayQueue(false)())
	if m = out.(Model); len(sent) != 0 || m.queued != 1 || !strings.Contains(m.status, "ABC-1 changed in Jira since") {
		t.Fatalf("conflict: sent %v, status %q", sent, m.status)
	}
	m.openPalette()
	if !strings.Contains(strings.Join(paletteLabels(m), "\n"), "queue  1 write waiting") {
		t.Errorf("palette: %q", paletteLabels(m))
	}
	m.closeJiraPicker()
	m.openQueue()
	out, cmd := m.applyQueuePick(m.jiraPicker.items[0]) // send them now
	m = out.(Model)
	out, _ = m.Update(cmd())
	if m = out.(Model); len(sent) != 1 || sent[0] != "PUT /rest/api/3/issue/ABC-1" || m.queued != 0 || m.queueBadge() != "" {
		t.Errorf("forced: sent %v, queued %d, status %q", sent, m.queued, m.status)
	}
}
