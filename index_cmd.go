package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/cornedor/laneway/internal/index"
)

// indexCmd shows the site's issue index (where it is, what it holds), or
// with clear drops it; the app fills it again as it reads.
func indexCmd(args []string, site string, out, errOut io.Writer) int {
	path, err := index.Path(site)
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	switch {
	case len(args) == 1 && args[0] == "clear":
		if err := index.Clear(path); err != nil {
			fmt.Fprintln(errOut, "laneway:", err)
			return 1
		}
		fmt.Fprintln(out, "cleared", path)
		return 0
	case len(args) > 0:
		fmt.Fprintln(errOut, "usage: laneway [-site NAME] index [clear]")
		return 2
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(out, path, "(empty)")
		return 0
	}
	ix, err := index.Open(path)
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	defer ix.Close()
	n, err := ix.Stats()
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	fmt.Fprintln(out, path)
	for _, p := range slices.Sorted(maps.Keys(n)) {
		fmt.Fprintf(out, "%s\t%d\n", p, n[p])
	}
	return 0
}
