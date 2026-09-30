package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmojiSearch(t *testing.T) {
	ts := issueServer(t, "http://127.0.0.1:1")
	var hits []emojiHit
	if code := issueCall(t, "GET", ts.URL+"/api/emoji?q=smil", nil, &hits); code != 200 || len(hits) == 0 || len(hits) > emojiShown {
		t.Fatalf("emoji: %d %v", code, hits)
	}
	if hits[0].Glyph == "" {
		t.Errorf("no glyph: %+v", hits[0])
	}
	hits = nil
	issueCall(t, "GET", ts.URL+"/api/emoji?q=a", nil, &hits)
	if len(hits) != 0 {
		t.Errorf("one letter answered %v", hits)
	}
	if code := issueCall(t, "POST", ts.URL+"/api/emoji/used", map[string]string{"Name": "smile"}, nil); code != 200 {
		t.Errorf("used: %d", code)
	}
	if code := issueCall(t, "POST", ts.URL+"/api/emoji/used", map[string]string{"Name": "nope_nope"}, nil); code != 400 {
		t.Errorf("unknown emoji: %d", code)
	}
}

func TestActionsRoutes(t *testing.T) {
	var got []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"watchers":[{"accountId":"a1","displayName":"Ann"}]}`))
		}
	}))
	defer fake.Close()
	ts := issueServer(t, fake.URL)
	var us []struct{ DisplayName string }
	if code := issueCall(t, "GET", ts.URL+"/api/issues/AB-1/watchers", nil, &us); code != 200 {
		t.Fatalf("watchers: %d", code)
	}
	if code := issueCall(t, "PUT", ts.URL+"/api/issues/AB-1/watchers/a1", map[string]bool{"Watch": false}, nil); code != 200 {
		t.Fatalf("unwatch: %d", code)
	}
	if code := issueCall(t, "POST", ts.URL+"/api/issues/AB-1/type", map[string]string{}, nil); code != 400 {
		t.Errorf("type without id: %d", code)
	}
	if code := issueCall(t, "GET", ts.URL+"/api/issues/AB-1/movetypes?project=x%20y", nil, nil); code != 400 {
		t.Errorf("bad project: %d", code)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "../a.png")
	_, _ = fw.Write([]byte("png"))
	mw.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "POST", ts.URL+"/api/issues/AB-1/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("upload: %d", res.StatusCode)
	}
	found := false
	for _, g := range got {
		found = found || g == "POST /rest/api/3/issue/AB-1/attachments"
	}
	if !found {
		t.Errorf("no upload reached jira: %v", got)
	}
}
