package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/editor"
)

// TestQuickInsert: "/" at a line's start lists what it inserts, narrowed
// by label words and aliases; taking one writes it over "/query", the
// cursor where it is filled in. Mid-line or in code it stays text.
func TestQuickInsert(t *testing.T) {
	items := quickItems(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	for q, want := range map[string]string{"warn": "Warning panel", "todo": "Action item", "green": "Status: green", "spoiler": "Expand", "h2": "Heading 2", "today": "Date: today"} {
		if got := quickMatches(items, q); len(got) == 0 || got[0].label != want {
			t.Errorf("/%s: %v", q, got)
		}
	}
	if got := quickMatches(items, "zzz"); len(got) != 0 {
		t.Errorf("/zzz: %v", got)
	}
	m := &Model{descEdit: &descEdit{input: editor.New()}}
	in := &m.descEdit.input
	in.SetValue("intro\n/warn")
	in.SetCursorOffset(len([]rune(in.Value())))
	if !m.scheduleQuick() || m.jiraMention.quick[0].label != "Warning panel" {
		t.Fatalf("no list: %+v", m.jiraMention)
	}
	m.mentionKey("tab")
	if in.Value() != "intro\n<!-- panel:warning -->\n\n\n\n<!-- /panel -->" || in.CursorOffset() != len([]rune("intro\n<!-- panel:warning -->\n\n")) {
		t.Errorf("%q at %d", in.Value(), in.CursorOffset())
	}
	for _, text := range []string{"a /warn", "```\n/warn"} {
		in.SetValue(text)
		in.SetCursorOffset(len([]rune(text)))
		if m.scheduleQuick() {
			t.Errorf("%q opened the list", text)
		}
	}
	if !strings.Contains(items[0].text, "‸") {
		t.Error("no cursor mark")
	}
}
