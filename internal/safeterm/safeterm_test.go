package safeterm

import "testing"

const (
	esc = "\x1b"
	bel = "\x07"
)

func TestText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"line one\nline two\tindented", "line one\nline two\tindented"},
		{esc + "[2Jwiped", "[2Jwiped"},
		{"clip" + esc + "]52;c;cHduZWQ=" + bel + "here", "clip]52;c;cHduZWQ=here"},
		{"del\x7fbs\bcr\r", "delbscr"},
		{"nul\x00sentinel", "nulsentinel"},
		{"c1mA", "c1mA"},         // single-byte CSI
		{"c152;c;x", "c152;c;x"}, // single-byte OSC
		{"émoji \U0001f389 ok", "émoji \U0001f389 ok"},
	}
	for _, c := range cases {
		if got := Text(c.in); got != c.want {
			t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLine(t *testing.T) {
	if got, want := Line("a\nb\tc"), "abc"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
	if got, want := Line(esc+"]52;c;x"+bel+"evil"), "]52;c;xevil"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
}

func TestCleanInputRoundTrips(t *testing.T) {
	s := "nothing to strip"
	if Text(s) != s || Line(s) != s {
		t.Fatal("clean input must round-trip")
	}
}

func TestJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":"plain"}`, `{"a":"plain"}`},
		{`{"a":"\u001b]52;c;x\u0007hi"}`, `{"a":"]52;c;xhi"}`},
		{`{"a":"\u001B[2J"}`, `{"a":"[2J"}`},
		{`{"a":"\u009b31m"}`, `{"a":"31m"}`},
		{"{\"a\":\"\u009b31m\"}", `{"a":"31m"}`},
		{`{"a":"one\ntwo\u000athree\ttab"}`, `{"a":"one\ntwo\u000athree\ttab"}`},
		{`{"a":"\\u001b stays text"}`, `{"a":"\\u001b stays text"}`},
		{`{"a":"q\"\u001b"}`, `{"a":"q\""}`},
		{`{"a":"é é"}`, `{"a":"é é"}`},
		{`{"a":"cut\u00`, `{"a":"cut\u00`},
	}
	for _, c := range cases {
		if got := string(JSON([]byte(c.in))); got != c.want {
			t.Errorf("JSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
