package main

import (
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
