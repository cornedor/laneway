package contract

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Report is what Compare found.
type Report struct {
	// Wrong: the demo says what Jira does not, a key Jira's object never
	// had or a type Jira never sent there.
	Wrong []string
	// Unverified: what the recording can't speak to, where Jira only sent
	// null or an empty list, or an endpoint it never answered.
	Unverified []string
	// Missing: endpoints Jira answered and the demo did not.
	Missing []string
}

// Compare holds demo's outline against jira's. allow lists "endpoint | path"
// pairs to let through, each for a reason the file gives.
func Compare(jira, demo Doc, allow map[string]bool) Report {
	var r Report
	for _, ep := range slices.Sorted(maps.Keys(demo)) {
		js, ok := jira[ep]
		if !ok {
			r.Unverified = append(r.Unverified, ep+": Jira never answered it")
			continue
		}
		ds := demo[ep]
		// Objects Jira sent with something in them; one always empty may be
		// a map, whose keys it can't rule out.
		filled := map[string]bool{}
		for p := range js {
			filled[parent(p)] = true
		}
		for _, p := range slices.Sorted(maps.Keys(ds)) {
			if allow[ep+" | "+p] {
				continue
			}
			if jk, ok := js[p]; ok {
				dk, jk := ds[p]&^kNull, jk&^kNull
				switch {
				case jk == 0 && dk != 0:
					r.Unverified = append(r.Unverified, fmt.Sprintf("%s: %s is %s, Jira only sent null", ep, p, dk))
				case dk&^jk != 0:
					r.Wrong = append(r.Wrong, fmt.Sprintf("%s: %s is %s, Jira sends %s", ep, p, dk, jk))
				}
				continue
			}
			par := parent(p)
			jp, ok := js[par]
			switch {
			case !ok:
				// The parent is new too, and reported itself.
			case jp&kObject != 0 && filled[par] && !strings.HasSuffix(p, "[]"):
				r.Wrong = append(r.Wrong, fmt.Sprintf("%s: %s, Jira never sent it", ep, p))
			default:
				r.Unverified = append(r.Unverified, fmt.Sprintf("%s: %s, Jira's %s was always empty", ep, p, par))
			}
		}
	}
	for _, ep := range slices.Sorted(maps.Keys(jira)) {
		if _, ok := demo[ep]; !ok {
			r.Missing = append(r.Missing, ep)
		}
	}
	return r
}

// ReadAllow parses the allow file: "endpoint | path" per line, "#" starting
// a comment.
func ReadAllow(rd io.Reader) (map[string]bool, error) {
	allow := map[string]bool{}
	sc := bufio.NewScanner(rd)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ep, path, ok := strings.Cut(line, " | ")
		if !ok {
			return nil, fmt.Errorf("allow: %q is not \"endpoint | path\"", line)
		}
		allow[strings.TrimSpace(ep)+" | "+strings.TrimSpace(path)] = true
	}
	return allow, sc.Err()
}
