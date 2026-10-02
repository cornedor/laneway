package joblog

import "testing"

// TestParse: colours carried across spans, a carriage return keeping the
// last state, section markers and control characters gone, tabs spaced.
func TestParse(t *testing.T) {
	ls := Parse("section_start:1:x\r\x1b[0K\x1b[31;1mred\x1b[0m plain\n10%\r\x1b[32mdone\x07\n\ta")
	if len(ls) != 3 {
		t.Fatalf("%d lines: %+v", len(ls), ls)
	}
	if ls[0][0] != (Span{"red", Style{FG: "1", Bold: true}}) || ls[0][1] != (Span{" plain", Style{}}) {
		t.Errorf("line 1: %+v", ls[0])
	}
	if ls[1].Plain() != "done" || ls[1][0].Style.FG != "2" {
		t.Errorf("line 2: %+v", ls[1])
	}
	if ls[2].Plain() != "    a" || ls[2][0].Style.FG != "2" { // the green runs on: no reset came
		t.Errorf("line 3: %+v", ls[2])
	}
}

// TestSGR: a style as the terminal sequence that sets it.
func TestSGR(t *testing.T) {
	for _, c := range []struct {
		s    Style
		want string
	}{
		{Style{}, ""},
		{Style{FG: "1", Bold: true}, "\x1b[1;31m"},
		{Style{FG: "9", BG: "4"}, "\x1b[91;44m"},
		{Style{FG: "#ff0000"}, "\x1b[38;2;255;0;0m"},
	} {
		if got := c.s.SGR(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.s, got, c.want)
		}
	}
	if got := (Line{{"a", Style{FG: "2"}}, {"b", Style{}}}).ANSI(); got != "\x1b[32ma\x1b[0mb" {
		t.Errorf("ANSI = %q", got)
	}
}
