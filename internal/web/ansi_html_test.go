package web

import (
	"strings"
	"testing"
)

// TestAnsiHTML: a line a div, colours as classes, 256 and RGB as CSS, a
// carriage return keeping a progress bar's last state, GitLab's section
// markers and other escapes left out, text escaped.
func TestAnsiHTML(t *testing.T) {
	log := "section_start:1:step_script\r\x1b[0K\x1b[36;1mExecuting\x1b[0;m\n" +
		"Pulling 10%\rPulling done\n" +
		"\x1b[38;5;196mred\x1b[0m \x1b[38;2;1;2;3mrgb\x1b[0m <b>\n" +
		"\x1b]8;;https://x\x07link\x1b]8;;\x07\x1b[2K\n" +
		"section_end:2:step_script\r\x1b[0K\x1b[32;1mJob succeeded\x1b[0;m"
	got := ansiHTML(log)
	for _, want := range []string{
		`<div class="jl-l"><span class="a-c6 a-bold">Executing</span></div>`,
		`<div class="jl-l">Pulling done</div>`,
		`<span style="color:#ff0000">red</span>`, `<span style="color:#010203">rgb</span>`, `&lt;b&gt;`,
		`<div class="jl-l">link</div>`,
		`<div class="jl-l"><span class="a-c2 a-bold">Job succeeded</span></div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "section_") || strings.Contains(got, "10%") || strings.Count(got, `class="jl-l"`) != 5 {
		t.Errorf("markers, an overwritten state or the line count:\n%s", got)
	}
}
