package ui

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"slices"

	"github.com/cornedor/laneway/internal/i18n"
)

// clipboardImage reads a PNG off the system clipboard with command
// (ui.clipboard_image), else the first tool found: wl-paste (Wayland), xclip
// (X11), pngpaste (macOS). Tests swap it.
var clipboardImage = func(command []string) ([]byte, error) {
	tools := [][]string{
		{"wl-paste", "--no-newline", "--type", "image/png"},
		{"xclip", "-selection", "clipboard", "-target", "image/png", "-out"},
		{"pngpaste", "-"},
	}
	if len(command) > 0 {
		tools = [][]string{command}
	}
	for _, cmd := range tools {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		out, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil || !bytes.HasPrefix(out, []byte("\x89PNG")) {
			return nil, errors.New(i18n.T("no image on the clipboard"))
		}
		return out, nil
	}
	if len(command) > 0 {
		return nil, errors.New(i18n.Tf("ui.clipboard_image: no %s", command[0]))
	}
	return nil, errors.New(i18n.T("reading the clipboard needs wl-paste, xclip or pngpaste (or ui.clipboard_image)"))
}

// screenshot captures a region of the screen as a PNG, with the first tool
// found: grim and slurp (Wayland), gnome-screenshot, spectacle (KDE),
// screencapture (macOS). Tests swap it.
var screenshot = func() ([]byte, error) {
	f, err := os.CreateTemp("", "laneway-shot-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	tools := []struct {
		need []string
		cmd  []string
	}{
		{[]string{"grim", "slurp"}, []string{"sh", "-c", `grim -g "$(slurp)" "$1"`, "sh", path}},
		{[]string{"gnome-screenshot"}, []string{"gnome-screenshot", "-a", "-f", path}},
		{[]string{"spectacle"}, []string{"spectacle", "-r", "-b", "-n", "-o", path}},
		{[]string{"screencapture"}, []string{"screencapture", "-i", path}},
	}
	for _, t := range tools {
		if !slices.ContainsFunc(t.need, func(n string) bool { _, err := exec.LookPath(n); return err != nil }) {
			_ = exec.Command(t.cmd[0], t.cmd[1:]...).Run()
			img, _ := os.ReadFile(path)
			if !bytes.HasPrefix(img, []byte("\x89PNG")) {
				return nil, errors.New(i18n.T("no screenshot taken"))
			}
			return img, nil
		}
	}
	return nil, errors.New(i18n.T("a screenshot needs grim and slurp, gnome-screenshot, spectacle or screencapture"))
}
