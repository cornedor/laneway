package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestAppAssetIndex(t *testing.T) {
	req := updater.CheckRequest{Platform: "darwin", Arch: "arm64"}
	assets := []github.ReleaseAsset{{Name: "laneway_0.5.0_darwin_arm64.tar.gz"}, {Name: sumAsset}, {Name: appAsset}}
	if i := appAssetIndex(req, assets); i != 2 {
		t.Errorf("picked %d, want the app's zip", i)
	}
	if i := github.DefaultAssetMatcher(req, assets); i != 0 {
		t.Errorf("default matcher picked %d; the custom one may not be needed", i)
	}
	if i := appAssetIndex(req, assets[:2]); i != -1 {
		t.Errorf("a release without the app: %d, want -1", i)
	}
}

func TestChannel(t *testing.T) {
	rel := func(tag string, pre bool) string {
		return fmt.Sprintf(`{"tag_name":%q,"prerelease":%t,"assets":[{"name":%q,"browser_download_url":"http://x/zip"}]}`, tag, pre, appAsset)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/latest") {
			fmt.Fprint(w, rel("v0.19.0", false))
		} else {
			fmt.Fprint(w, "["+rel("v0.20.0-beta.1", true)+","+rel("v0.19.0", false)+"]")
		}
	}))
	defer srv.Close()
	var beta atomic.Bool
	ch := &channel{beta: &beta}
	for _, p := range []struct {
		dst *(*github.Provider)
		pre bool
	}{{&ch.stable, false}, {&ch.pre, true}} {
		gh, err := github.New(github.Config{Repository: "cornedor/laneway", BaseURL: srv.URL, AssetMatcher: appAssetIndex, Prerelease: p.pre})
		if err != nil {
			t.Fatal(err)
		}
		*p.dst = gh
	}
	req := updater.CheckRequest{CurrentVersion: "0.18.0", Platform: "darwin", Arch: "arm64"}
	for _, c := range []struct {
		beta bool
		want string
	}{{false, "0.19.0"}, {true, "0.20.0-beta.1"}} {
		beta.Store(c.beta)
		r, err := ch.Check(context.Background(), req)
		if err != nil || r == nil || r.Version != c.want {
			t.Errorf("beta %t: %+v, %v; want %s", c.beta, r, err, c.want)
		}
	}
	// A beta is newer than the release before it, older than its own.
	beta.Store(true)
	req.CurrentVersion = "0.20.0"
	if r, err := ch.Check(context.Background(), req); r != nil || err != nil {
		t.Errorf("on 0.20.0, beta offered %+v, %v", r, err)
	}
}
