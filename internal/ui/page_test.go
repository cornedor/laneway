package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/demo"
	"github.com/cornedor/laneway/internal/jira"
)

// TestConfluencePage: L on DEMO-4's Confluence link reads the page in the
// panel; esc goes back to the issue.
func TestConfluencePage(t *testing.T) {
	url, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	m := jiraTabModel(t)
	m.jiraClient = jira.New(jira.Config{BaseURL: url, Email: "demo@example.com", APIToken: "demo"})
	out, cmd := openRefFor(m, "DEMO-4")
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if m.jiraIssue == nil {
		t.Fatal("DEMO-4 did not load")
	}
	links, err := m.jiraClient.WebLinks(m.ctx, "DEMO-4")
	if err != nil {
		t.Fatal(err)
	}
	m.webLinks, m.webLinksKey = links, "DEMO-4"
	m.images = &panelImages{on: true, live: true, maxRows: 16, cell: defaultCell, byAtt: map[string]*panelImage{}, avatars: map[string]*panelImage{}}

	out, _ = m.handleKey(keyMsg(t, "L"))
	m = out.(Model)
	at := -1
	for i, it := range m.jiraPicker.items {
		if strings.HasPrefix(it.label, "page ") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no page row in %+v", m.jiraPicker.items)
	}
	m.jiraPicker.idx = at
	out, cmd = m.applyJiraPick()
	m = out.(Model)
	if m.page == nil || cmd == nil {
		t.Fatal("picking the page should read it")
	}
	out, cmd = m.Update(cmd())
	m = out.(Model)
	e := m.images.byAtt[pageImageKey("40962")]
	if e == nil || cmd == nil {
		t.Fatalf("the page's image is not fetched: %v", m.images.byAtt)
	}
	for _, msg := range drain(cmd) {
		if l, ok := msg.(imageLoadedMsg); ok {
			out, _ = m.Update(l)
			m = out.(Model)
		}
	}
	if e.state != imgReady || !strings.Contains(m.View().Content, string(rune(0x10EEEE))) {
		t.Errorf("the page's image is not drawn: state %v", e.state)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Guest checkout: design", "[toc macro]", "Flow", "checkout-flow.png"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q in:\n%s", want, view)
		}
	}
	out, _ = m.handleKey(keyMsg(t, "esc"))
	m = out.(Model)
	if m.page != nil || !strings.Contains(ansi.Strip(m.View().Content), "Checkout without an account") {
		t.Error("esc should go back to the issue")
	}
}

// drain runs cmd and the batches it returns, for their messages.
func drain(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	if cmd == nil {
		return out
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, drain(c)...)
		}
	default:
		out = append(out, msg)
	}
	return out
}
