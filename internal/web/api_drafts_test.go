package web

import (
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/demo"
)

func TestDrafts(t *testing.T) {
	base, stop, err := demo.New(time.Now()).Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ts := issueServer(t, base)
	u := ts.URL + "/api/drafts/"
	var d struct{ Text string }
	if issueCall(t, "GET", u+"comment:DEMO-1", nil, &d) != 200 || d.Text != "" {
		t.Fatalf("no draft = %+v", d)
	}
	for _, id := range []string{"desc:DEMO-1", "desc:DEMO-1:comment:10001", "comment:DEMO-2"} {
		if code := issueCall(t, "PUT", u+id, map[string]string{"Text": "half a\nthought"}, nil); code != 200 {
			t.Fatalf("put %s: %d", id, code)
		}
		if issueCall(t, "GET", u+id, nil, &d); d.Text != "half a\nthought" {
			t.Errorf("%s = %q", id, d.Text)
		}
	}
	issueCall(t, "DELETE", u+"desc:DEMO-1", nil, nil)
	if issueCall(t, "GET", u+"desc:DEMO-1", nil, &d); d.Text != "" {
		t.Errorf("deleted draft = %q", d.Text)
	}
	issueCall(t, "PUT", u+"comment:DEMO-2", map[string]string{"Text": "  "}, nil)
	if issueCall(t, "GET", u+"comment:DEMO-2", nil, &d); d.Text != "" {
		t.Errorf("blank draft kept: %q", d.Text)
	}
	for _, bad := range []string{"comment:nope", "other:DEMO-1", "comment:DEMO-1:comment:1", "desc:DEMO-1:comment:x"} {
		if code := issueCall(t, "PUT", u+bad, map[string]string{"Text": "x"}, nil); code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}
}
