package gitlab

import (
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/forge"
)

// The review half of the GitLab provider: the merge request's full diff, the
// inline discussions already on it, and posting a new one. See internal/forge's
// diff.go for the shared types and the unified-diff parser; this file is the
// wire format and nothing else.

// Client implements the optional review half of the provider set.
var _ forge.Reviewer = (*Client)(nil)

const (
	// diffPerPage is the page size for the diffs endpoint (GitLab's maximum).
	diffPerPage = 100
	// diffMaxPages caps how much of a huge merge request we pull. A thousand
	// changed files is already past the point where reviewing it in a terminal
	// is the plan; the view says the diff was truncated rather than pretending
	// it is complete.
	diffMaxPages = 10
	// discussionMaxPages caps the inline-conversation fetch the same way.
	discussionMaxPages = 5
)

// Diff returns the merge request's full diff, serving a cached copy when it has
// one. Invalidate (which Approve/Merge already call) drops it along with the
// change itself.
func (c *Client) Diff(ctx context.Context, project string, iid int) (*forge.Diff, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	if hit, ok := c.diffs.Get(project, iid); ok {
		return hit, nil
	}
	d, err := c.fetchDiff(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	c.diffs.Put(project, iid, d)
	return d, nil
}

// fetchDiff reads the diff refs off the merge request, then pages the diffs
// endpoint. On an instance too old for that endpoint (it arrived in GitLab
// 15.7) it falls back to the deprecated /changes, which answers with the whole
// diff — capped by the server — in one call.
func (c *Client) fetchDiff(ctx context.Context, project string, iid int) (*forge.Diff, error) {
	refs, err := c.diffRefs(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	files, truncated, err := c.diffPages(ctx, project, iid)
	if err != nil {
		var se *forge.StatusErr
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return c.changesDiff(ctx, project, iid)
		}
		return nil, err
	}
	return &forge.Diff{Refs: refs, Files: files, Truncated: truncated}, nil
}

// apiDiffRefs is GitLab's diff_refs block: the three commits an inline note is
// positioned against.
type apiDiffRefs struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
}

func (r apiDiffRefs) toRefs() forge.DiffRefs {
	return forge.DiffRefs{BaseSHA: r.BaseSHA, StartSHA: r.StartSHA, HeadSHA: r.HeadSHA}
}

// diffRefs fetches just the commit refs from the merge request.
func (c *Client) diffRefs(ctx context.Context, project string, iid int) (forge.DiffRefs, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d", encodePath(project), iid)
	var resp struct {
		DiffRefs apiDiffRefs `json:"diff_refs"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, label(project, iid), nil, &resp); err != nil {
		return forge.DiffRefs{}, err
	}
	return resp.DiffRefs.toRefs(), nil
}

// apiFileDiff is one entry of the diffs / changes payload.
type apiFileDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Generated   bool   `json:"generated_file"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
}

func (a apiFileDiff) toFileDiff() forge.FileDiff {
	return forge.FileDiff{
		OldPath:   a.OldPath,
		NewPath:   a.NewPath,
		Diff:      a.Diff,
		New:       a.NewFile,
		Deleted:   a.DeletedFile,
		Renamed:   a.RenamedFile,
		Generated: a.Generated,
		TooLarge:  a.TooLarge || a.Collapsed && a.Diff == "",
		// GitLab has no binary flag: a binary file arrives as git's own notice
		// in place of a hunk, which is also exactly what we want to show.
		Binary: strings.HasPrefix(a.Diff, "Binary files") || strings.HasPrefix(a.Diff, "GIT binary patch"),
	}
}

// diffPages walks the paginated diffs endpoint. A short page ends the walk; a
// full page at the cap means there is more, which the caller reports as a
// truncated diff.
func (c *Client) diffPages(ctx context.Context, project string, iid int) ([]forge.FileDiff, bool, error) {
	var files []forge.FileDiff
	for page := 1; page <= diffMaxPages; page++ {
		path := fmt.Sprintf("/projects/%s/merge_requests/%d/diffs?per_page=%d&page=%d",
			encodePath(project), iid, diffPerPage, page)
		var batch []apiFileDiff
		if err := c.rest.Do(ctx, http.MethodGet, path, "diff", nil, &batch); err != nil {
			return nil, false, err
		}
		for _, a := range batch {
			files = append(files, a.toFileDiff())
		}
		if len(batch) < diffPerPage {
			return files, false, nil
		}
	}
	return files, true, nil
}

// changesDiff is the pre-15.7 path: /changes hands over the diff refs and every
// file in one response, flagging its own truncation as "overflow".
func (c *Client) changesDiff(ctx context.Context, project string, iid int) (*forge.Diff, error) {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/changes", encodePath(project), iid)
	var resp struct {
		DiffRefs apiDiffRefs   `json:"diff_refs"`
		Overflow bool          `json:"overflow"`
		Changes  []apiFileDiff `json:"changes"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, "diff", nil, &resp); err != nil {
		return nil, err
	}
	d := &forge.Diff{Refs: resp.DiffRefs.toRefs(), Truncated: resp.Overflow}
	for _, a := range resp.Changes {
		d.Files = append(d.Files, a.toFileDiff())
	}
	return d, nil
}

// apiPosition is the anchor GitLab stores on an inline note.
type apiPosition struct {
	PositionType string `json:"position_type"`
	OldPath      string `json:"old_path"`
	NewPath      string `json:"new_path"`
	OldLine      int    `json:"old_line"`
	NewLine      int    `json:"new_line"`
	HeadSHA      string `json:"head_sha"`
}

// apiNote is one note of a discussion.
type apiNote struct {
	ID         int64        `json:"id"`
	Body       string       `json:"body"`
	System     bool         `json:"system"`
	Resolved   bool         `json:"resolved"`
	Resolvable bool         `json:"resolvable"`
	CreatedAt  string       `json:"created_at"`
	Author     *apiUser     `json:"author"`
	Position   *apiPosition `json:"position"`
}

// Threads returns the merge request's conversations — the inline ones and the
// ones on the merge request as a whole, in GitLab's own order. System notes
// ("changed the title", "added 3 commits") are dropped: they are activity, not
// conversation, and nobody reviews them.
func (c *Client) Threads(ctx context.Context, project string, iid int) ([]forge.Thread, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	var out []forge.Thread
	for page := 1; page <= discussionMaxPages; page++ {
		path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions?per_page=100&page=%d",
			encodePath(project), iid, page)
		var batch []struct {
			ID    string    `json:"id"`
			Notes []apiNote `json:"notes"`
		}
		if err := c.rest.Do(ctx, http.MethodGet, path, "discussions", nil, &batch); err != nil {
			return nil, err
		}
		for _, d := range batch {
			if t, ok := toThread(d.ID, d.Notes); ok {
				out = append(out, t)
			}
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

// toThread flattens one discussion, reporting false for the ones that carry no
// conversation at all (a lone system note). A discussion with no text position
// is kept, unanchored: that is the merge request's own comment thread.
func toThread(id string, notes []apiNote) (forge.Thread, bool) {
	if len(notes) == 0 {
		return forge.Thread{}, false
	}
	t := forge.Thread{ID: id, Resolved: notes[0].Resolved, Resolvable: notes[0].Resolvable}
	if pos := notes[0].Position; pos != nil && pos.PositionType == "text" {
		t.Path, t.OldPath = pos.NewPath, pos.OldPath
		t.OldLine, t.NewLine, t.HeadSHA = pos.OldLine, pos.NewLine, pos.HeadSHA
		if t.Path == "" {
			t.Path = pos.OldPath
		}
	}
	for _, n := range notes {
		if n.System {
			continue
		}
		note := forge.Note{ID: strconv.FormatInt(n.ID, 10), Body: n.Body}
		if n.Author != nil {
			note.Author = n.Author.Name
		}
		if ts, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil {
			note.Created = ts
		}
		t.Notes = append(t.Notes, note)
	}
	if len(t.Notes) == 0 {
		return forge.Thread{}, false
	}
	return t, true
}

// AddNote posts an inline note: a reply when ReplyTo names a discussion,
// otherwise a new conversation anchored to the line. GitLab validates the
// position against the diff, so a note on a line the diff doesn't contain comes
// back as a 400 with its own message rather than landing somewhere wrong.
func (c *Client) AddNote(ctx context.Context, project string, iid int, n forge.NewNote) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	base := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", encodePath(project), iid)
	if n.ReplyTo != "" {
		body := map[string]any{"body": n.Body}
		return c.rest.Do(ctx, http.MethodPost, base+"/"+n.ReplyTo+"/notes", "reply", body, nil)
	}
	body := map[string]any{"body": n.Body, "position": position(n)}
	return c.rest.Do(ctx, http.MethodPost, base, "note", body, nil)
}

// position anchors n to its line of the diff at n.Refs.
func position(n forge.NewNote) map[string]any {
	pos := map[string]any{
		"base_sha":      n.Refs.BaseSHA,
		"start_sha":     n.Refs.StartSHA,
		"head_sha":      n.Refs.HeadSHA,
		"position_type": "text",
		"new_path":      n.NewPath,
		"old_path":      n.OldPath,
	}
	// Exactly the sides the line exists on: an added line has no old number and
	// sending 0 for it is rejected.
	if n.OldLine > 0 {
		pos["old_line"] = n.OldLine
	}
	if n.NewLine > 0 {
		pos["new_line"] = n.NewLine
	}
	if r := n.Range; r != nil {
		file := cmp.Or(n.NewPath, n.OldPath)
		pos["line_range"] = map[string]any{"start": rangeEnd(file, r.Start), "end": rangeEnd(file, r.End)}
	}
	return pos
}

// rangeEnd is one end of a line_range: GitLab names the line by its code,
// sha1(path)_old_new over the counters, and says which side it is on.
func rangeEnd(file string, p forge.LinePos) map[string]any {
	e := map[string]any{"line_code": fmt.Sprintf("%x_%d_%d", sha1.Sum([]byte(file)), p.OldPos, p.NewPos)}
	switch {
	case p.NewLine > 0 && p.OldLine == 0:
		e["type"] = "new"
	case p.OldLine > 0 && p.NewLine == 0:
		e["type"] = "old"
	}
	if p.OldLine > 0 {
		e["old_line"] = p.OldLine
	}
	if p.NewLine > 0 {
		e["new_line"] = p.NewLine
	}
	return e
}

// The pending review: GitLab's draft notes, which only their writer sees until
// SubmitReview publishes them together.

// Drafts are the token's account's pending notes on the merge request.
func (c *Client) Drafts(ctx context.Context, project string, iid int) ([]forge.Draft, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/draft_notes", encodePath(project), iid)
	var ds []struct {
		ID           int          `json:"id"`
		Note         string       `json:"note"`
		DiscussionID string       `json:"discussion_id"`
		Position     *apiPosition `json:"position"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, "pending review", nil, &ds); err != nil {
		return nil, err
	}
	out := make([]forge.Draft, 0, len(ds))
	for _, d := range ds {
		fd := forge.Draft{ID: d.ID, Body: d.Note, ReplyTo: d.DiscussionID}
		if p := d.Position; p != nil && p.PositionType == "text" {
			fd.Path, fd.OldPath, fd.OldLine, fd.NewLine = cmp.Or(p.NewPath, p.OldPath), p.OldPath, p.OldLine, p.NewLine
		}
		out = append(out, fd)
	}
	return out, nil
}

// AddDraft puts n in the pending review: a reply when ReplyTo names a
// discussion, else a new one on its line.
func (c *Client) AddDraft(ctx context.Context, project string, iid int, n forge.NewNote) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	body := map[string]any{"note": n.Body}
	if n.ReplyTo != "" {
		body["in_reply_to_discussion_id"] = n.ReplyTo
	} else if n.NewPath != "" || n.OldPath != "" {
		body["position"] = position(n)
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/draft_notes", encodePath(project), iid)
	return c.rest.Do(ctx, http.MethodPost, path, "pending note", body, nil)
}

// EditDraft replaces pending note id's text. GitLab's PUT clears a position
// it is not sent, so the note's own is read and sent back: it stays on its lines.
func (c *Client) EditDraft(ctx context.Context, project string, iid, id int, body string) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/draft_notes/%d", encodePath(project), iid, id)
	var d struct {
		Position json.RawMessage `json:"position"`
	}
	if err := c.rest.Do(ctx, http.MethodGet, path, "pending note", nil, &d); err != nil {
		return err
	}
	req := map[string]any{"note": body}
	if p := d.Position; len(p) > 0 && string(p) != "null" {
		req["position"] = p
	}
	return c.rest.Do(ctx, http.MethodPut, path, "pending note", req, nil)
}

// DeleteDraft takes pending note id out of the review.
func (c *Client) DeleteDraft(ctx context.Context, project string, iid, id int) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/draft_notes/%d", encodePath(project), iid, id)
	return c.rest.Do(ctx, http.MethodDelete, path, "pending note", nil, nil)
}

// SubmitReview publishes the pending notes with summary (may be empty) and
// the verdict: forge.VerdictComment, VerdictApprove or VerdictChanges. An
// approve is GitLab's approval too, after the notes.
func (c *Client) SubmitReview(ctx context.Context, project string, iid int, summary, verdict string) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	body := map[string]any{"reviewer_state": "reviewed"}
	if verdict == forge.VerdictChanges {
		body["reviewer_state"] = "requested_changes"
	}
	if strings.TrimSpace(summary) != "" {
		body["note"] = summary
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/draft_notes/bulk_publish", encodePath(project), iid)
	if err := c.rest.Do(ctx, http.MethodPost, path, "review", body, nil); err != nil {
		return err
	}
	c.Invalidate(project, iid)
	if verdict == forge.VerdictApprove {
		return c.Approve(ctx, project, iid)
	}
	return nil
}

// ResolveThread resolves or reopens an inline conversation. GitLab only accepts
// this for a resolvable discussion, which every diff note is; the overall
// discussion is not, and the diff view never offers it one.
func (c *Client) ResolveThread(ctx context.Context, project string, iid int, threadID string, resolved bool) error {
	if !c.Enabled() {
		return forge.ErrNotConfigured
	}
	if threadID == "" {
		return errors.New("gitlab: no discussion to resolve")
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions/%s",
		encodePath(project), iid, threadID)
	what := "resolve"
	if !resolved {
		what = "reopen"
	}
	return c.rest.Do(ctx, http.MethodPut, path, what, map[string]any{"resolved": resolved}, nil)
}

// Versions are the merge request's pushes, newest first: each its own diff,
// read with VersionDiff.
func (c *Client) Versions(ctx context.Context, project string, iid int) ([]forge.Version, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/versions?per_page=100", encodePath(project), iid)
	var vs []apiVersion
	if err := c.rest.Do(ctx, http.MethodGet, path, "versions", nil, &vs); err != nil {
		return nil, err
	}
	out := make([]forge.Version, len(vs))
	for i, v := range vs {
		out[i] = v.toVersion()
	}
	return out, nil
}

// apiVersion is one entry of the versions endpoint; read one by its id and
// it carries its diffs too.
type apiVersion struct {
	ID        int           `json:"id"`
	HeadSHA   string        `json:"head_commit_sha"`
	BaseSHA   string        `json:"base_commit_sha"`
	StartSHA  string        `json:"start_commit_sha"`
	CreatedAt string        `json:"created_at"`
	Diffs     []apiFileDiff `json:"diffs"`
}

func (v apiVersion) toVersion() forge.Version {
	fv := forge.Version{ID: v.ID, Refs: forge.DiffRefs{BaseSHA: v.BaseSHA, StartSHA: v.StartSHA, HeadSHA: v.HeadSHA}}
	if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil {
		fv.Created = t
	}
	return fv
}

// VersionDiff is version id's diff, as it was pushed.
func (c *Client) VersionDiff(ctx context.Context, project string, iid, id int) (*forge.Diff, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/versions/%d", encodePath(project), iid, id)
	var v apiVersion
	if err := c.rest.Do(ctx, http.MethodGet, path, "version", nil, &v); err != nil {
		return nil, err
	}
	d := &forge.Diff{Refs: v.toVersion().Refs}
	for _, a := range v.Diffs {
		d.Files = append(d.Files, a.toFileDiff())
	}
	return d, nil
}

// File is the file at path as it is at ref (a commit), a line each, for
// filling in a diff's unchanged lines (forge.ExpandDiff).
func (c *Client) File(ctx context.Context, project, path, ref string) ([]string, error) {
	if !c.Enabled() {
		return nil, forge.ErrNotConfigured
	}
	p := fmt.Sprintf("/projects/%s/repository/files/%s/raw?ref=%s", encodePath(project),
		strings.ReplaceAll(url.PathEscape(path), "/", "%2F"), url.QueryEscape(ref))
	b, err := c.rest.DoRaw(ctx, http.MethodGet, p, path, nil)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), nil
}
