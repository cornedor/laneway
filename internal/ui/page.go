package ui

import (
	"cmp"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/cornedor/laneway/internal/jira"
)

// L on a Confluence page of this site reads it in the panel, in place of
// the issue: its markdown, macros named, images by name. The scroll keys
// read on; o opens it in Confluence, r reloads, esc or backspace go back to
// the issue.

// panelPage is the page the panel shows over its issue.
type panelPage struct {
	id, title string
	page      jira.Page
	loading   bool
	err       string
	gen       int
}

// pageMsg is a page read.
type pageMsg struct {
	gen  int
	page jira.Page
	err  error
}

// openPage shows Confluence page id in the panel.
func (m *Model) openPage(id, title string) tea.Cmd {
	m.pageGen++
	m.page = &panelPage{id: id, title: title, loading: true, gen: m.pageGen}
	m.refView.GotoTop()
	m.renderRef()
	m.status = helpKey(m.keys.OpenAttach) + " Confluence · " + helpKey(m.keys.Refresh) + " reload · esc back to the issue"
	c, ctx, gen := m.jiraClient, m.ctx, m.pageGen
	return func() tea.Msg {
		p, err := c.ConfluencePage(ctx, id)
		return pageMsg{gen: gen, page: p, err: err}
	}
}

func (m Model) handlePage(msg pageMsg) (tea.Model, tea.Cmd) {
	if m.page == nil || m.page.gen != msg.gen {
		return m, nil
	}
	m.page.loading = false
	if msg.err != nil {
		m.page.err = msg.err.Error()
		m.renderRef()
		return m, nil
	}
	m.page.page = msg.page
	m.renderRef()
	return m, m.fetchPageImages(msg.page.Markdown)
}

var pageImageRefRe = regexp.MustCompile(`\]\(` + jira.PageImageScheme + `(\d+)\)`)

// fetchPageImages downloads the page's images not held yet, and sends back
// those held but freed since (swapIssueImages frees all but the issue's).
func (m *Model) fetchPageImages(md string) tea.Cmd {
	ii := m.images
	if ii == nil || !ii.on {
		return nil
	}
	var cmds []tea.Cmd
	var back strings.Builder
	for _, sm := range pageImageRefRe.FindAllStringSubmatch(md, -1) {
		key, id := pageImageKey(sm[1]), sm[1]
		switch e := ii.byAtt[key]; {
		case e == nil:
			c, ctx := m.jiraClient, m.ctx
			cmds = append(cmds, m.loadImage(key, func() ([]byte, error) { return c.PageImage(ctx, id) }))
		case e.gone && e.state == imgReady:
			back.WriteString(e.transmit())
			e.gone = false
		}
	}
	return tea.Batch(append(cmds, ii.send(back.String()))...)
}

// renderPage is the page as the panel draws it, w wide.
func (m *Model) renderPage(w int) string {
	p := m.page
	var b strings.Builder
	b.WriteString(refKeyStyle.Render(cmp.Or(p.page.Title, p.title)) + "\n")
	b.WriteString(refDimStyle.Render("Confluence · "+helpKey(m.keys.OpenAttach)+" opens it · esc back") + "\n\n")
	switch {
	case p.err != "":
		b.WriteString(refErrStyle.Render(p.err))
	case p.loading:
		b.WriteString(refDimStyle.Render("loading the page…"))
	default:
		b.WriteString(renderMarkdown(p.page.Markdown, m.emojiImg, nil, ""))
	}
	return m.placeImages(wrapPanel(expandTables(b.String(), w), w))
}

// pageKey handles a key while a page shows; false leaves it to the panel.
func (m Model) pageKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case msg.String() == "esc", key.Matches(msg, m.keys.Back):
		m.page = nil
		m.status = ""
		m.refView.GotoTop()
		m.renderRef()
		return m, nil, true
	case key.Matches(msg, m.keys.OpenAttach):
		u := m.page.page.URL
		if u == "" {
			return m, nil, true
		}
		m.status = "opening " + u + "…"
		return m, m.openOpenable(openable{name: m.page.title, url: u}), true
	case key.Matches(msg, m.keys.Refresh):
		return m, m.openPage(m.page.id, m.page.title), true
	case msg.String() == "ctrl+c", key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Help):
		return m, nil, false
	}
	var cmd tea.Cmd
	m.refView, cmd = m.refView.Update(msg) // every other key reads on
	return m, cmd, true
}
