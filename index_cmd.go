package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/cornedor/laneway/internal/i18n"
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
		fmt.Fprintln(out, i18n.Tf("cleared %s", path))
		return 0
	case len(args) > 0:
		fmt.Fprintln(errOut, i18n.T("usage: laneway [-site NAME] index [clear]"))
		return 2
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(out, path, i18n.T("(empty)"))
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
	people, err := ix.PeopleStats()
	if err != nil {
		fmt.Fprintln(errOut, "laneway:", err)
		return 1
	}
	fmt.Fprintln(out, path)
	projects := slices.Collect(maps.Keys(n))
	for p := range people {
		if _, ok := n[p]; !ok {
			projects = append(projects, p)
		}
	}
	slices.Sort(projects)
	for _, p := range projects {
		fmt.Fprintf(out, "%s\t%s\t%s\n", p, i18n.Tf("%d issues", n[p]), i18n.Tf("%d people", people[p]))
	}
	return 0
}
