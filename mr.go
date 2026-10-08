package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/i18n"
)

var mrUsage = i18n.N(`usage: laneway mr note [-config FILE] LINK [FILE:LINE] TEXT

Adds TEXT to your pending review of the GitLab merge request at LINK, on
LINE of FILE as it is on the merge request's head (FILE:-LINE: a removed
line, numbered as it was), or on the merge request as a whole without one.
Only you see it until the review is submitted (S in its diff). An agent
reviewing the merge request (C) leaves its findings this way.`)

// mrCmd runs laneway mr.
func mrCmd(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] != "note" {
		fmt.Fprintln(errOut, i18n.T(mrUsage))
		return 2
	}
	fs := flag.NewFlagSet("mr note", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprintln(errOut, i18n.T(mrUsage)) }
	cfgPath := fs.String("config", "", i18n.T("config file"))
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		return 2
	}
	link, text := rest[0], strings.Join(rest[1:], " ")
	path, line, old, ok := mrLine(rest[1])
	if ok {
		text = strings.Join(rest[2:], " ")
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(errOut, "laneway mr note:", i18n.T("no text"))
		return 2
	}
	cfg, _, err := config.Load(*cfgPath)
	if err != nil && !errors.Is(err, config.ErrNoConfig) { // glab's login does without one
		fmt.Fprintln(errOut, "laneway mr note:", err)
		return 1
	}
	sites, warn := gitlabSites(cfg)
	for _, w := range warn {
		fmt.Fprintln(errOut, "laneway mr note:", w)
	}
	c := sites.For(link)
	if c == nil {
		fmt.Fprintln(errOut, "laneway mr note:", i18n.Tf("no GitLab token for %s", forge.HostOf(link)))
		return 1
	}
	ref, okRef := c.Parse(link)
	if !okRef {
		fmt.Fprintln(errOut, "laneway mr note:", i18n.Tf("not a merge request link: %s", link))
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	n := forge.NewNote{Body: text}
	if ok {
		d, err := c.Diff(ctx, ref.Repo, ref.Number)
		if err != nil {
			fmt.Fprintln(errOut, "laneway mr note:", err)
			return 1
		}
		f, err := mrNoteLine(d, path, line, old)
		if err != nil {
			fmt.Fprintln(errOut, "laneway mr note:", err)
			return 1
		}
		n.Refs, n.OldPath, n.NewPath, n.OldLine, n.NewLine = d.Refs, f.OldPath, f.NewPath, f.OldLine, f.NewLine
	}
	if err := c.AddDraft(ctx, ref.Repo, ref.Number, n); err != nil {
		fmt.Fprintln(errOut, "laneway mr note:", err)
		return 1
	}
	fmt.Fprintln(out, i18n.Tf("pending note added to %s", ref.Repo+"!"+strconv.Itoa(ref.Number)))
	return 0
}

// mrLine reads FILE:LINE, or FILE:-LINE for a removed line.
func mrLine(s string) (path string, line int, old, ok bool) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 {
		return "", 0, false, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil || n == 0 {
		return "", 0, false, false
	}
	if n < 0 {
		return s[:i], -n, true, true
	}
	return s[:i], n, false, true
}

// mrNoteLine is the position of LINE of FILE in d: a line the diff shows, on
// both sides when unchanged. GitLab only takes notes on those.
func mrNoteLine(d *forge.Diff, path string, line int, old bool) (forge.NewNote, error) {
	for _, f := range d.Files {
		if f.Path() != path && f.OldPath != path {
			continue
		}
		for _, l := range forge.ParseUnifiedDiff(f.Diff) {
			if old && l.Kind == forge.DiffDel && l.OldLine == line ||
				!old && (l.Kind == forge.DiffAdd || l.Kind == forge.DiffContext) && l.NewLine == line {
				return forge.NewNote{OldPath: cmp.Or(f.OldPath, f.Path()), NewPath: cmp.Or(f.NewPath, f.Path()), OldLine: l.OldLine, NewLine: l.NewLine}, nil
			}
		}
		return forge.NewNote{}, fmt.Errorf(i18n.T("%s:%d is not in the diff: note a changed line or one beside it, or the merge request as a whole (no FILE:LINE)"), path, line)
	}
	return forge.NewNote{}, fmt.Errorf(i18n.T("%s is not in the diff"), path)
}
