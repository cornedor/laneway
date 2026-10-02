package web

import (
	"context"
	"html"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/ui"
)

// A merge request's diff for the browser's diff view (TUI: d on one in the
// panel): every file's lines parsed and highlighted here, and the inline
// threads to draw under them.

// DiffFile is one changed file.
type DiffFile struct {
	Path, OldPath                            string
	New, Deleted, Renamed, Binary, Generated bool
	TooLarge                                 bool
	Add, Del                                 int
	Lines                                    []DiffLine
}

// DiffLine is one row: K is " " context, "+", "-", "@" a hunk header, "\\"
// a note; O and N its numbers (0 for none), H its HTML.
type DiffLine struct {
	K    string
	O, N int
	H    string
}

// DiffThread is an inline conversation.
type DiffThread struct {
	ID, Path         string
	OldLine, NewLine int
	Resolved         bool
	Notes            []DiffNote
}

// DiffNote is one message of it.
type DiffNote struct {
	Author, Body string
	Created      time.Time
}

func init() {
	get("/gitlab/diff", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		if r.URL.Query().Get("fresh") == "1" {
			c.Invalidate(ref.Repo, ref.Number)
		}
		// &version= an older push (as v in the TUI): its own diff, without the
		// threads, which sit on the newest's lines.
		version, _ := strconv.Atoi(r.URL.Query().Get("version"))
		var d *forge.Diff
		var threads []forge.Thread
		if version != 0 {
			d, err = c.VersionDiff(ctx, ref.Repo, ref.Number, version)
		} else if d, err = c.Diff(ctx, ref.Repo, ref.Number); err == nil {
			threads, _ = c.Threads(ctx, ref.Repo, ref.Number) // best-effort, as in the TUI
		}
		if err != nil {
			return nil, err
		}
		versions, _ := c.Versions(ctx, ref.Repo, ref.Number)
		out := map[string]any{"Label": ref.Repo + "!" + strconv.Itoa(ref.Number), "WebURL": c.WebURL(ref.Repo, ref.Number),
			"Truncated": d.Truncated, "Files": diffFiles(d.Files), "Threads": diffThreads(threads), "Version": version, "Versions": diffVersions(versions)}
		if mr, err := c.Get(ctx, ref.Repo, ref.Number); err == nil {
			out["Title"] = mr.Title
		}
		return out, nil
	})
}

func init() {
	// &path= one file of the diff (&version= as above) with its unchanged
	// lines filled in from its text at the head: e in the diff view.
	get("/gitlab/diff/file", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		var d *forge.Diff
		if version, _ := strconv.Atoi(r.URL.Query().Get("version")); version != 0 {
			d, err = c.VersionDiff(ctx, ref.Repo, ref.Number, version)
		} else {
			d, err = c.Diff(ctx, ref.Repo, ref.Number)
		}
		if err != nil {
			return nil, err
		}
		path := r.URL.Query().Get("path")
		i := slices.IndexFunc(d.Files, func(f forge.FileDiff) bool { return f.Path() == path })
		if i < 0 {
			return nil, httpError{http.StatusNotFound, path + " is not in this diff"}
		}
		f := d.Files[i]
		if f.Deleted || f.Binary || f.TooLarge {
			return nil, httpError{http.StatusBadRequest, path + ": nothing more to show"}
		}
		text, err := c.File(ctx, ref.Repo, f.NewPath, d.Refs.HeadSHA)
		if err != nil {
			return nil, err
		}
		return diffFile(f, forge.ExpandDiff(forge.ParseUnifiedDiff(f.Diff), text)), nil
	})
}

var diffKinds = map[forge.DiffLineKind]string{forge.DiffContext: " ", forge.DiffAdd: "+", forge.DiffDel: "-", forge.DiffHunk: "@", forge.DiffMeta: "\\"}

func diffFiles(files []forge.FileDiff) []DiffFile {
	out := make([]DiffFile, 0, len(files))
	for _, f := range files {
		out = append(out, diffFile(f, forge.ParseUnifiedDiff(f.Diff)))
	}
	return out
}

// diffFile is f's lines (parsed, or expanded) numbered and highlighted.
func diffFile(f forge.FileDiff, lines []forge.DiffLine) DiffFile {
	var code []string
	var at []int
	for i, l := range lines {
		if l.Kind != forge.DiffHunk && l.Kind != forge.DiffMeta {
			code, at = append(code, l.Text), append(at, i)
		}
	}
	lit := ui.HighlightHTML(f.Path(), code)
	df := DiffFile{Path: f.Path(), OldPath: f.OldPath, New: f.New, Deleted: f.Deleted, Renamed: f.Renamed, Binary: f.Binary, Generated: f.Generated, TooLarge: f.TooLarge,
		Lines: make([]DiffLine, len(lines))}
	for i, l := range lines {
		df.Lines[i] = DiffLine{K: diffKinds[l.Kind], O: l.OldLine, N: l.NewLine}
		switch l.Kind {
		case forge.DiffAdd:
			df.Add++
		case forge.DiffDel:
			df.Del++
		}
	}
	for j, i := range at {
		df.Lines[i].H = lit[j]
	}
	for i, l := range lines {
		if l.Kind == forge.DiffHunk || l.Kind == forge.DiffMeta {
			df.Lines[i].H = html.EscapeString(l.Text)
		}
	}
	return df
}

// DiffVersion is one push, newest first.
type DiffVersion struct {
	ID      int
	HeadSHA string
	Created time.Time
}

func diffVersions(vs []forge.Version) []DiffVersion {
	out := make([]DiffVersion, len(vs))
	for i, v := range vs {
		out[i] = DiffVersion{ID: v.ID, HeadSHA: v.Refs.HeadSHA, Created: v.Created}
	}
	return out
}

func diffThreads(ts []forge.Thread) []DiffThread {
	out := []DiffThread{}
	for _, t := range forge.InlineThreads(ts) {
		dt := DiffThread{ID: t.ID, Path: t.Path, OldLine: t.OldLine, NewLine: t.NewLine, Resolved: t.Resolved}
		for _, n := range t.Notes {
			dt.Notes = append(dt.Notes, DiffNote{Author: n.Author, Body: n.Body, Created: n.Created})
		}
		out = append(out, dt)
	}
	return out
}
