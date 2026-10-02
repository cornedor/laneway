package web

import (
	"context"
	"html"
	"net/http"
	"slices"
	"strconv"
	"strings"
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

// DiffThread is a conversation: Inline on a line of this head's diff, or
// on the merge request as a whole, or Outdated (on a line of an earlier push).
type DiffThread struct {
	ID, Path, OldPath                      string
	OldLine, NewLine                       int
	Inline, Outdated, Resolved, Resolvable bool
	Notes                                  []DiffNote
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
		drafts := []forge.Draft{}
		if version != 0 {
			d, err = c.VersionDiff(ctx, ref.Repo, ref.Number, version)
		} else if d, err = c.Diff(ctx, ref.Repo, ref.Number); err == nil {
			threads, _ = c.Threads(ctx, ref.Repo, ref.Number) // best-effort, as in the TUI
			if ds, err := c.Drafts(ctx, ref.Repo, ref.Number); err == nil {
				drafts = ds
			}
		}
		if err != nil {
			return nil, err
		}
		versions, _ := c.Versions(ctx, ref.Repo, ref.Number)
		out := map[string]any{"Label": ref.Repo + "!" + strconv.Itoa(ref.Number), "WebURL": c.WebURL(ref.Repo, ref.Number),
			"Truncated": d.Truncated, "Files": diffFiles(d.Files), "Threads": diffThreads(threads, d.Refs.HeadSHA), "Drafts": drafts, "Version": version, "Versions": diffVersions(versions)}
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

func diffThreads(ts []forge.Thread, head string) []DiffThread {
	out := []DiffThread{}
	for _, t := range ts {
		dt := DiffThread{ID: t.ID, Path: t.Path, OldPath: t.OldPath, OldLine: t.OldLine, NewLine: t.NewLine, Resolved: t.Resolved, Resolvable: t.Resolvable,
			Outdated: t.Outdated(head)}
		dt.Inline = t.Inline() && !dt.Outdated
		for _, n := range t.Notes {
			dt.Notes = append(dt.Notes, DiffNote{Author: n.Author, Body: n.Body, Created: n.Created})
		}
		out = append(out, dt)
	}
	return out
}

// NoteForm is a note to post: a reply to ReplyTo, else a new conversation on
// the line OldLine / NewLine of OldPath / NewPath (0: not on that side).
type NoteForm struct {
	Body, ReplyTo    string
	OldPath, NewPath string
	OldLine, NewLine int
}

func init() {
	// ?url= the merge request: into your pending review (TUI: c in its diff).
	post("/gitlab/note", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		f, err := Body[NoteForm](r)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(f.Body) == "" {
			return nil, FieldError{Field: "Body", Msg: "a note needs text"}
		}
		n := forge.NewNote{Body: f.Body, ReplyTo: f.ReplyTo, OldPath: f.OldPath, NewPath: f.NewPath, OldLine: f.OldLine, NewLine: f.NewLine}
		if f.ReplyTo == "" { // a position is anchored to the diff's commits
			d, err := c.Diff(ctx, ref.Repo, ref.Number)
			if err != nil {
				return nil, err
			}
			n.Refs = d.Refs
		}
		return map[string]bool{"OK": true}, c.AddDraft(ctx, ref.Repo, ref.Number, n)
	})
	// ?url= and &draft=: drops a pending note (TUI: x).
	del("/gitlab/draft", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		id, _ := strconv.Atoi(r.URL.Query().Get("draft"))
		return map[string]bool{"OK": true}, c.DeleteDraft(ctx, ref.Repo, ref.Number, id)
	})
	// ?url=: {Verdict, Summary} publishes the pending review (TUI: S); a
	// Verdict of "approve" alone, with Only, approves without it (TUI: A).
	post("/gitlab/review", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct {
			Verdict, Summary string
			Only             bool
		}](r)
		if err != nil {
			return nil, err
		}
		switch {
		case b.Only && b.Verdict == forge.VerdictApprove:
			err = c.Approve(ctx, ref.Repo, ref.Number)
		case b.Verdict == forge.VerdictComment, b.Verdict == forge.VerdictApprove, b.Verdict == forge.VerdictChanges:
			err = c.SubmitReview(ctx, ref.Repo, ref.Number, b.Summary, b.Verdict)
		default:
			return nil, FieldError{Field: "Verdict", Msg: "comment, approve or changes"}
		}
		return map[string]bool{"OK": true}, err
	})
	// ?url= and &thread=: {Resolved} resolves it or reopens it (TUI: R).
	put("/gitlab/resolve", func(ctx context.Context, s *Server, r *http.Request) (any, error) {
		c, ref, err := gitlabLink(s, r)
		if err != nil {
			return nil, err
		}
		b, err := Body[struct{ Resolved bool }](r)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"OK": true}, c.ResolveThread(ctx, ref.Repo, ref.Number, r.URL.Query().Get("thread"), b.Resolved)
	})
}
