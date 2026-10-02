package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// version is set by build-mac.sh: the release's, without the v.
var version = "dev"

// The release assets build-mac.sh makes and release.yml uploads.
const (
	appAsset = "Laneway-mac.zip"
	sumAsset = appAsset + ".sha256"
)

const releasesPage = "https://github.com/cornedor/laneway/releases/latest"

// initUpdates sets up the updater and looks for a release now and daily.
// Only macOS has an app to update; a dev build never looks.
func (d *desktop) initUpdates() {
	if runtime.GOOS != "darwin" || version == "dev" {
		return
	}
	gh, err := github.New(github.Config{
		Repository:    "cornedor/laneway",
		ChecksumAsset: sumAsset,
		AssetMatcher:  appAssetIndex,
	})
	if err == nil {
		err = d.app.Updater.Init(updater.Config{
			CurrentVersion: version,
			Providers:      []updater.Provider{gh},
			Window:         updater.WindowNone,
		})
	}
	if err != nil {
		log.Print("updates: ", err)
		return
	}
	d.updates = true
	if b, err := bundlePath(); err != nil || translocatedFrom(b) != "" {
		return // offerMove asks first; Check for Updates… still works
	}
	go func() {
		for {
			d.checkUpdate(false)
			time.Sleep(24 * time.Hour)
		}
	}()
}

// checkUpdate asks to install a newer release. manual (the menu, the page's
// ↑) also says when there is none or the check failed; a daily check that
// fails only logs.
func (d *desktop) checkUpdate(manual bool) {
	if !d.updates {
		if manual {
			d.openReleases()
		}
		return
	}
	if !d.checking.CompareAndSwap(false, true) {
		return
	}
	defer d.checking.Store(false)
	if s := d.app.Updater.State(); s == updater.StateDownloading || s == updater.StateVerifying || s == updater.StateInstalling {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rel, err := d.app.Updater.Check(ctx)
	if err == nil && rel != nil && rel.Verification == nil {
		err = fmt.Errorf("release %s has no %s", rel.Version, sumAsset) // unverified: not installed
	}
	switch {
	case err != nil:
		log.Print("updates: ", err)
		if manual {
			d.app.Dialog.Error().SetTitle("Laneway could not look for updates").SetMessage(err.Error()).Show()
		}
	case rel == nil:
		if manual {
			d.app.Dialog.Info().SetTitle("Laneway is up to date").SetMessage("Version " + version + " is the latest.").Show()
		}
	case manual || d.declined.Load() != rel.Version:
		d.offerUpdate(rel)
	}
}

// offerUpdate asks to install rel. Not Now keeps the daily check quiet about
// it until the app starts again.
func (d *desktop) offerUpdate(rel *updater.Release) {
	log.Printf("updates: %s is out", rel.Version)
	dlg := d.app.Dialog.Question().SetTitle("Laneway " + rel.Version + " is out").
		SetMessage("You have " + version + ". Laneway restarts to update.")
	install := dlg.AddButton("Update and Restart")
	install.OnClick(func() { go d.installUpdate() })
	notes := dlg.AddButton("Release Notes")
	notes.OnClick(d.openReleases)
	later := dlg.AddButton("Not Now")
	later.OnClick(func() { d.declined.Store(rel.Version) })
	dlg.SetDefaultButton(install).SetCancelButton(later).Show()
}

// installUpdate downloads the pending release, checks it and restarts into
// it. Laneway.app is replaced where it is, so that place must be writable
// and not a temporary copy.
func (d *desktop) installUpdate() {
	fail := func(err error) {
		log.Print("updates: ", err)
		dlg := d.app.Dialog.Error().SetTitle("Laneway was not updated").
			SetMessage(err.Error() + "\n\nDownload it from the release page instead.")
		page := dlg.AddButton("Open Release Page")
		page.OnClick(d.openReleases)
		ok := dlg.AddButton("OK")
		dlg.SetDefaultButton(page).SetCancelButton(ok).Show()
	}
	bundle, err := bundlePath()
	if err != nil {
		fail(err)
		return
	}
	if translocatedFrom(bundle) != "" {
		fail(errors.New("Laneway runs from a temporary copy: move it to Applications first."))
		return
	}
	if err := writable(filepath.Dir(bundle)); err != nil {
		fail(fmt.Errorf("Laneway cannot replace itself in %s: %w", filepath.Dir(bundle), err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := d.app.Updater.DownloadAndInstall(ctx); err != nil {
		fail(err)
		return
	}
	// The digest says it is the release's zip; this, that it unpacked whole.
	if out, err := exec.Command("codesign", "--verify", "--deep", d.app.Updater.DownloadedPath()).CombinedOutput(); err != nil {
		fail(fmt.Errorf("the download's signature: %s", strings.TrimSpace(string(out))))
		return
	}
	log.Printf("updates: restarting into %s", d.app.Updater.DownloadedPath())
	if err := d.app.Updater.Restart(ctx); err != nil {
		fail(err)
	}
}

// appAssetIndex is Laneway-mac.zip among a release's assets. The default
// matcher wants darwin and the arch in the name: it would pick the CLI's
// tarball.
func appAssetIndex(_ updater.CheckRequest, assets []github.ReleaseAsset) int {
	for i, a := range assets {
		if a.Name == appAsset {
			return i
		}
	}
	return -1
}

func (d *desktop) openReleases() {
	if err := d.app.Browser.OpenURL(releasesPage); err != nil {
		log.Print("open: ", err)
	}
}

// bundlePath is the Laneway.app this runs from.
func bundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	b := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // Laneway.app/Contents/MacOS/laneway-desktop
	if filepath.Ext(b) != ".app" {
		return "", fmt.Errorf("%s is not in an app bundle", exe)
	}
	return b, nil
}

// writable is whether a file can be created in dir: the swap renames the
// bundle there.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".laneway-update-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}
