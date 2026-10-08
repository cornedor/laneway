package ui

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/editor"
	"github.com/cornedor/laneway/internal/i18n"
)

// "/" quick insert, after Jira's: "/" at a line's start in the description
// editor (and the create form's) lists what Jira content can go there,
// narrowed as you type ("/warn", "/todo", "/green"); tab or enter writes
// its markdown, the cursor where you fill it in.

// quickItem is one thing "/" inserts: text with ‸ where the cursor goes,
// found by its label's words or its aliases.
type quickItem struct {
	label, text string
	aliases     []string
}

// quickShown caps the list.
const quickShown = 8

// quickItems are the "/" menu, in its order when nothing is typed.
func quickItems(today time.Time) []quickItem {
	panel := func(label, typ string) quickItem {
		return quickItem{label, "<!-- panel:" + typ + " -->\n\n‸\n\n<!-- /panel -->", []string{"panel", typ}}
	}
	status := func(label, color string) quickItem {
		return quickItem{i18n.Tf("Status: %s", label), `<status color="` + color + `">‸</status>`, []string{"lozenge", "label"}}
	}
	colour := func(label, hex string) quickItem {
		return quickItem{i18n.Tf("Text colour: %s", label), `<span style="color:` + hex + `">‸</span>`, []string{"color", "colour"}}
	}
	return []quickItem{
		{i18n.T("Action item"), "- [ ] ‸", []string{"task", "todo", "checkbox"}},
		{i18n.T("Decision"), "<> ‸", []string{"decided"}},
		panel(i18n.T("Info panel"), "info"), panel(i18n.T("Note panel"), "note"), panel(i18n.T("Success panel"), "success"),
		panel(i18n.T("Warning panel"), "warning"), panel(i18n.T("Error panel"), "error"),
		{i18n.T("Expand"), "<!-- expand: ‸ -->\n\n\n\n<!-- /expand -->", []string{"spoiler", "collapse", "details"}},
		{i18n.T("Table"), "| ‸ |  |  |\n| --- | --- | --- |\n|  |  |  |", nil},
		{i18n.T("Code block"), "```\n‸\n```", []string{"snippet"}},
		{i18n.T("Quote"), "> ‸", []string{"blockquote"}},
		{i18n.T("Divider"), "---\n‸", []string{"rule", "hr", "line"}},
		{i18n.T("Heading 1"), "# ‸", []string{"h1", "title"}},
		{i18n.T("Heading 2"), "## ‸", []string{"h2"}},
		{i18n.T("Heading 3"), "### ‸", []string{"h3"}},
		{i18n.T("Bullet list"), "- ‸", []string{"ul", "unordered"}},
		{i18n.T("Numbered list"), "1. ‸", []string{"ol", "ordered"}},
		{i18n.T("Date: today"), "<date>" + today.Format(time.DateOnly) + "</date>‸", []string{"now"}},
		status(i18n.T("grey"), "neutral"), status(i18n.T("purple"), "purple"), status(i18n.T("blue"), "blue"),
		status(i18n.T("red"), "red"), status(i18n.T("yellow"), "yellow"), status(i18n.T("green"), "green"),
		{i18n.T("Mention"), "@‸", []string{"person", "user"}},
		{i18n.T("Link"), "[‸](https://)", []string{"url"}},
		{i18n.T("Underline"), "<u>‸</u>", nil},
		colour(i18n.T("blue"), "#0747a6"), colour(i18n.T("teal"), "#008da6"), colour(i18n.T("green"), "#006644"),
		colour(i18n.T("orange"), "#ff991f"), colour(i18n.T("red"), "#bf2600"), colour(i18n.T("purple"), "#403294"), colour(i18n.T("grey"), "#97a0af"),
	}
}

// quickQuery matches "/" and a word at a line's start, ending at the cursor.
var quickQuery = regexp.MustCompile(`(?:^|\n) */([a-zA-Z0-9]*)$`)

// quickEditor is the editor "/" inserts in: one whose text saves as
// markdown (the description, a comment or field being edited, the create
// form's description); nil for none.
func (m *Model) quickEditor() *editor.Model {
	if m.descEdit != nil {
		return &m.descEdit.input
	}
	if ed, _ := m.mentionEditor(); ed != nil && !m.jiraCommentActive {
		return ed
	}
	return nil
}

// scheduleQuick opens, updates or closes the "/" list for the text before
// the cursor; true while it shows.
func (m *Model) scheduleQuick() bool {
	ed := m.quickEditor()
	var sub []string
	if ed != nil && !ed.InCodeBlock() {
		r := []rune(ed.Value())
		sub = quickQuery.FindStringSubmatch(string(r[:min(ed.CursorOffset(), len(r))]))
	}
	if sub == nil {
		if len(m.jiraMention.quick) > 0 {
			m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
		}
		return false
	}
	q := strings.ToLower(sub[1])
	if ms := m.jiraMention; len(ms.quick) > 0 && ms.query == q {
		return true // the same query: keep the choice
	}
	items := quickMatches(quickItems(time.Now()), q)
	start := ed.CursorOffset() - len([]rune(q)) - 1
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1, start: start, quick: items, query: q}
	return len(items) > 0
}

// quickMatches are the items q finds, best first: a word of the label or
// an alias it is, then one it starts, then one it is inside of.
func quickMatches(items []quickItem, q string) []quickItem {
	type cand struct {
		it   quickItem
		band int
	}
	var cands []cand
	for _, it := range items {
		band := 3
		words := append(strings.Fields(strings.ToLower(strings.NewReplacer(":", "").Replace(it.label))), it.aliases...)
		words = append(words, strings.ReplaceAll(strings.ToLower(it.label), " ", ""))
		for _, w := range words {
			if b, _, ok := fuzzyScore(w, q); ok && b < band {
				band = b
			}
		}
		if q == "" {
			band = 0
		}
		if band < 3 {
			cands = append(cands, cand{it, band})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return a.band - b.band })
	out := make([]quickItem, 0, min(len(cands), quickShown))
	for _, c := range cands[:min(len(cands), quickShown)] {
		out = append(out, c.it)
	}
	return out
}

// acceptQuick writes it over the typed "/query", the cursor at its ‸.
func (m *Model) acceptQuick(it quickItem) {
	in := m.quickEditor()
	if in == nil {
		return
	}
	r := []rune(in.Value())
	cur := min(in.CursorOffset(), len(r))
	start := min(m.jiraMention.start, cur)
	before, after, _ := strings.Cut(it.text, "‸")
	in.SetValue(string(r[:start]) + before + after + string(r[cur:]))
	in.SetCursorOffset(start + len([]rune(before)))
	m.jiraMention = mentionState{seq: m.jiraMention.seq + 1}
}
