package ui

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/editor"
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
		return quickItem{"Status: " + label, `<status color="` + color + `">‸</status>`, []string{"lozenge", "label"}}
	}
	colour := func(label, hex string) quickItem {
		return quickItem{"Text colour: " + label, `<span style="color:` + hex + `">‸</span>`, []string{"color", "colour"}}
	}
	return []quickItem{
		{"Action item", "- [ ] ‸", []string{"task", "todo", "checkbox"}},
		{"Decision", "<> ‸", []string{"decided"}},
		panel("Info panel", "info"), panel("Note panel", "note"), panel("Success panel", "success"),
		panel("Warning panel", "warning"), panel("Error panel", "error"),
		{"Expand", "<!-- expand: ‸ -->\n\n\n\n<!-- /expand -->", []string{"spoiler", "collapse", "details"}},
		{"Table", "| ‸ |  |  |\n| --- | --- | --- |\n|  |  |  |", nil},
		{"Code block", "```\n‸\n```", []string{"snippet"}},
		{"Quote", "> ‸", []string{"blockquote"}},
		{"Divider", "---\n‸", []string{"rule", "hr", "line"}},
		{"Heading 1", "# ‸", []string{"h1", "title"}},
		{"Heading 2", "## ‸", []string{"h2"}},
		{"Heading 3", "### ‸", []string{"h3"}},
		{"Bullet list", "- ‸", []string{"ul", "unordered"}},
		{"Numbered list", "1. ‸", []string{"ol", "ordered"}},
		{"Date: today", "<date>" + today.Format(time.DateOnly) + "</date>‸", []string{"now"}},
		status("grey", "neutral"), status("purple", "purple"), status("blue", "blue"),
		status("red", "red"), status("yellow", "yellow"), status("green", "green"),
		{"Mention", "@‸", []string{"person", "user"}},
		{"Link", "[‸](https://)", []string{"url"}},
		{"Underline", "<u>‸</u>", nil},
		colour("blue", "#0747a6"), colour("teal", "#008da6"), colour("green", "#006644"),
		colour("orange", "#ff991f"), colour("red", "#bf2600"), colour("purple", "#403294"), colour("grey", "#97a0af"),
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
