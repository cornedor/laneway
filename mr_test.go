package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMrNote: laneway mr note adds a pending note on a line of the diff, a
// removed one by FILE:-LINE, or on the merge request; a line the diff
// doesn't show is refused.
func TestMrNote(t *testing.T) {
	t.Setenv("GLAB_CONFIG_DIR", t.TempDir()) // no glab logins
	var writes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			writes = append(writes, r.URL.Path+" "+string(b))
			w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			w.Write([]byte(`[{"old_path": "a.go", "new_path": "a.go", "diff": "@@ -1,2 +1,2 @@\n keep\n-old\n+new\n"}]`))
		default:
			w.Write([]byte(`{"iid": 7, "diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}}`))
		}
	}))
	defer srv.Close()
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(cfg, []byte("gitlab:\n  - base_url: "+srv.URL+"\n    token: tok\n"), 0o600)
	link := srv.URL + "/g/p/-/merge_requests/7"
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := mrCmd(append([]string{"note", "-config", cfg, link}, args...), &out, &errOut)
		return code, out.String() + errOut.String()
	}
	for _, args := range [][]string{{"a.go:2", "why", "new?"}, {"a.go:-2", "gone?"}, {"a.go:1", "context"}, {"Overall", "fine"}} {
		if code, out := run(args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	if code, out := run("a.go:9", "far"); code == 0 || !strings.Contains(out, "a.go:9 is not in the diff") {
		t.Errorf("a line outside the diff: %d %s", code, out)
	}
	if len(writes) != 4 || !strings.Contains(writes[0], `"new_line":2`) || strings.Contains(writes[0], `"old_line"`) || !strings.Contains(writes[0], `"note":"why new?"`) ||
		!strings.Contains(writes[1], `"old_line":2`) || strings.Contains(writes[1], `"new_line"`) ||
		!strings.Contains(writes[2], `"new_line":1`) || !strings.Contains(writes[2], `"old_line":1`) ||
		strings.Contains(writes[3], "position") || !strings.Contains(writes[3], `"note":"Overall fine"`) {
		t.Errorf("writes:\n%s", strings.Join(writes, "\n"))
	}
}
