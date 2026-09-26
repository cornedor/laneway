package ui

import (
	"bytes"
	"errors"
	"os/exec"
)

// clipboardImage reads a PNG off the system clipboard with the first tool
// found: wl-paste (Wayland), xclip (X11), pngpaste (macOS). Tests swap it.
var clipboardImage = func() ([]byte, error) {
	for _, cmd := range [][]string{
		{"wl-paste", "--no-newline", "--type", "image/png"},
		{"xclip", "-selection", "clipboard", "-target", "image/png", "-out"},
		{"pngpaste", "-"},
	} {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		out, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil || !bytes.HasPrefix(out, []byte("\x89PNG")) {
			return nil, errors.New("no image on the clipboard")
		}
		return out, nil
	}
	return nil, errors.New("reading the clipboard needs wl-paste, xclip or pngpaste")
}
