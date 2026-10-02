package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveApp: the app goes to dst, the one there to the Trash, and the
// quarantine is cleared on the moved one.
func TestMoveApp(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	home := filepath.Join(dir, "home")
	for _, d := range []string{bin, filepath.Join(home, ".Trash"), filepath.Join(dir, "Downloads", "Laneway.app"), filepath.Join(dir, "Applications", "Laneway.app")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "Downloads", "Laneway.app", "new"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Applications", "Laneway.app", "old"), nil, 0o644)
	cleared := filepath.Join(dir, "xattr-args")
	os.WriteFile(filepath.Join(bin, "xattr"), []byte("#!/bin/sh\necho \"$@\" > "+cleared+"\n"), 0o755)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)

	orig, dst := filepath.Join(dir, "Downloads", "Laneway.app"), filepath.Join(dir, "Applications", "Laneway.app")
	if err := moveApp(orig, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "new")); err != nil {
		t.Error("the new app is not in Applications")
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Error("the original is still in Downloads")
	}
	if trashed, _ := filepath.Glob(filepath.Join(home, ".Trash", "Laneway *.app", "old")); len(trashed) != 1 {
		t.Errorf("the old app is not in the Trash: %v", trashed)
	}
	if b, _ := os.ReadFile(cleared); string(b) != "-dr com.apple.quarantine "+dst+"\n" {
		t.Errorf("xattr ran with %q", b)
	}
}
