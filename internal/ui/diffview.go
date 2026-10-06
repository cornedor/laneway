package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// d on a merge request in the panel: its full diff over the screen,
// syntax-highlighted, with the inline conversations already on it drawn
// under the lines they belong to, and the changed files in a tree beside it.
// It takes the whole body rather than the panel because a diff needs the
// width: code is not prose that can be reflowed into a third of the screen.
//
// Performance shape: the expensive, width-independent work (parsing the
// hunks, running chroma over each file, laying the conversations out as
// rows) happens once, when the diff lands. Drawing a frame touches only the
// rows on screen, so a ten-thousand-line diff costs the same per keystroke as
// a ten-line one.
//
// Ported from matterbox's internal/ui/diffview.go.

// diffRowKind is what one drawn row of the view is.
type diffRowKind uint8

const (
	diffRowFile    diffRowKind = iota // the per-file header
	diffRowHunk                       // an @@ … @@ header
	diffRowContext                    // unchanged code
	diffRowAdd                        // added code
	diffRowDel                        // removed code
	diffRowMeta                       // "\ No newline…", "binary file", the truncation notice
	diffRowNote                       // one line of an existing inline conversation
)

// commentable reports whether a note can be anchored to this row — only the
// code rows have a position the forge will accept.
func (k diffRowKind) commentable() bool {
	return k == diffRowContext || k == diffRowAdd || k == diffRowDel
}

// diffRow is one row of the view. For a code row text is already
// syntax-highlighted (done once, at load), so drawing it is truncate-and-tint.
type diffRow struct {
	kind     diffRowKind
	file     int // index into diffState.diff.Files, -1 where there is none
	old      int // line number on the old side, 0 where it has none
	new      int // line number on the new side, 0 where it has none
	text     string
	thread   int  // index into diffState.threads for a note row, -1 otherwise
	noteHead bool // the first row of a conversation — the one naming an author
	draft    int  // 1 + the index into diffState.drafts for a pending note's row, 0 otherwise
	// raw is a code row's text as it is, and oldPos, newPos its counters
	// (forge.DiffLine's): what a suggestion and a range note are made of.
	raw            string
	oldPos, newPos int
}

// diffState is the whole view, hung off Model by pointer: it is far too big to
// copy on every event, and nil is the closed state.
type diffState struct {
	c      *gitlab.Client
	repo   string
	number int
	label  string // "group/project!42"
	title  string
	webURL string

	gen     int // drops a fetch the user has already closed or refreshed past
	loading bool
	err     error

	diff    *forge.Diff
	threads []forge.Thread

	rows    []diffRow
	gutter  int // width of one line-number column, sized to the biggest number
	cursor  int // selected row, an index into rows
	top     int // first drawn row, a position in visible
	hscroll int // horizontal pan, in cells
	viewH   int // rows the last frame had room for — paging and clamping need it

	// Folding. collapsed is per file; visible lists the rows a collapsed file
	// leaves behind (its header, nothing else) and visPos maps back, so the
	// cursor can stay a row index while the window walks what is on screen.
	collapsed []bool
	visible   []int
	visPos    []int

	// The file tree down the left side, and which panel has the keys. See
	// difftree.go; fileHead and treeOf are the two directions of "this file" —
	// its header row in the diff, its row in the tree — so moving in one panel
	// moves the other.
	tree        []diffTreeRow
	fileHead    []int
	treeOf      []int
	treeIdx     int
	treeTop     int
	treeHScroll int
	treeFocus   bool
	treeH       int // rows the tree column had in the last frame

	// versions are the pushes, newest first (read with the first diff);
	// version the one shown, 0 for the newest. picking is v's list open,
	// pick its cursor.
	versions []forge.Version
	version  int
	picking  bool
	pick     int

	// full are the files e expanded, by index: their text at the head, which
	// fills in the lines around the hunks.
	full map[int][]string

	note diffNoteState // the composer (diffnote.go)
	// mark is where V began a range of lines for c or s, 0 for none (1 + the
	// row).
	mark int
	// drafts are your pending review's notes; verdicts S's list open, verdict
	// its cursor.
	drafts   []forge.Draft
	verdicts bool
	verdict  int
	// merges are M's list while it is open, mergeAt its cursor.
	merges     []string
	mergeAt    int
	mergeTitle string
}

// diffHScrollStep is how far ←/→ pan a wide diff: a tab's worth of columns,
// enough to make progress across an indented line without losing your place.
const diffHScrollStep = 8

// diffScrollOff keeps this many rows visible past the cursor, so you can see
// what you are about to scroll into.
const diffScrollOff = 2

var (
	// The diff tints. Backgrounds, not foregrounds: a review reads as blocks of
	// added and removed code, and tinting the text instead would fight the
	// syntax highlighting for the same channel. Each is a pair resolved against
	// the terminal's own background like every other tinted surface here (see
	// theme.go) — these dark greens are invisible on a light terminal, and the
	// light ones wash out on a dark one.
	diffAddBg = adaptiveColor{light: lipgloss.Color("#d7f5dd"), dark: lipgloss.Color("#13301d")}
	diffDelBg = adaptiveColor{light: lipgloss.Color("#ffdcd8"), dark: lipgloss.Color("#3a1519")}
	// The cursor row keeps its hue but a step stronger, so "which line am I on"
	// and "what kind of line is it" stay two separate questions.
	diffAddCursorBg = adaptiveColor{light: lipgloss.Color("#a9e8bb"), dark: lipgloss.Color("#1f5230")}
	diffDelCursorBg = adaptiveColor{light: lipgloss.Color("#ffb8b2"), dark: lipgloss.Color("#63222a")}
	diffCursorBg    = adaptiveColor{light: lipgloss.Color("252"), dark: lipgloss.Color("238")}
	diffFileBg      = adaptiveColor{light: lipgloss.Color("254"), dark: lipgloss.Color("236")}
	// A file header carries the cursor too — and has to, because with every
	// file folded (Z) the header rows are the only rows there are, and a view
	// with no visible cursor is a view you cannot steer.
	diffFileCursorBg = adaptiveColor{light: lipgloss.Color("249"), dark: lipgloss.Color("241")}

	diffGutterStyle = lipgloss.NewStyle().Foreground(dimColor)
	diffAddMark     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	diffDelMark     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	diffHunkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	diffFileStyle   = lipgloss.NewStyle().Bold(true)
	diffMetaStyle   = lipgloss.NewStyle().Foreground(dimColor).Italic(true)
	diffNoteHead    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	diffNoteBody    = lipgloss.NewStyle().Foreground(dimColor)
)

// diffLoadedMsg carries a finished diff fetch. gen guards a result the user has
// already closed or refreshed past.
type diffLoadedMsg struct {
	gen      int
	diff     *forge.Diff
	threads  []forge.Thread
	drafts   []forge.Draft
	versions []forge.Version // nil when not asked
	built    diffBuild
	err      error
}

// openDiffView raises the review view for the merge request the panel is
// showing (d in its view).
func (m Model) openDiffView() (tea.Model, tea.Cmd) {
	p := m.mr
	if p == nil || p.mr == nil {
		return m, nil
	}
	m.diff = &diffState{
		c:       p.c,
		repo:    p.ref.Repo,
		number:  p.ref.Number,
		label:   p.ref.Repo + "!" + strconv.Itoa(p.ref.Number),
		title:   p.mr.Title,
		webURL:  p.mr.WebURL,
		loading: true,
		gen:     1,
	}
	m.status = "loading diff for " + m.diff.label + "…"
	return m, m.fetchDiff()
}

// closeDiffView tears the view down. The generation bump is what makes an
// in-flight fetch land on nothing.
//
// Pending notes are easy to forget: only you see them until the review is
// submitted, so leaving with some says so, and the panel's merge request
// takes them over.
func (m *Model) closeDiffView() {
	d := m.diff
	if d == nil {
		return
	}
	d.gen++
	m.diff = nil
	m.status = ""
	if p := m.mr; p != nil && p.ref.Repo == d.repo && p.ref.Number == d.number && d.diff != nil {
		p.drafts = d.drafts
		m.renderRef()
	}
	if n := len(d.drafts); n > 0 && d.version == 0 {
		m.status = plural(n, "pending note") + " unpublished: d, then S submits your review"
	}
}

// fetchDiff fetches the diff and the inline conversations in the background.
// The diff is what can fail the view; the conversations are best-effort, like
// the approvals in the panel — a token that can read a diff but not
// the discussions still gets a reviewable view, minus what others said.
func (m Model) fetchDiff() tea.Cmd {
	d := m.diff
	if d == nil {
		return nil
	}
	c, ctx, gen := d.c, m.ctx, d.gen
	repo, number, version, askVersions, full := d.repo, d.number, d.version, d.versions == nil, maps.Clone(d.full)
	return func() tea.Msg {
		msg := diffLoadedMsg{gen: gen}
		if version != 0 { // an older push: its threads are on the newest's lines
			msg.diff, msg.err = c.VersionDiff(ctx, repo, number, version)
		} else {
			msg.diff, msg.err = c.Diff(ctx, repo, number)
		}
		if msg.err != nil {
			return msg
		}
		if version == 0 {
			msg.threads, _ = c.Threads(ctx, repo, number)
			msg.drafts, _ = c.Drafts(ctx, repo, number)
		}
		if askVersions {
			msg.versions, _ = c.Versions(ctx, repo, number)
			if msg.versions == nil {
				msg.versions = []forge.Version{}
			}
		}
		// Parsed and highlighted here, off the UI goroutine: chroma costs
		// milliseconds per file, and a merge request touching two hundred files
		// would otherwise freeze the app for as long as it takes.
		msg.built = buildDiff(msg.diff, msg.threads, msg.drafts, full)
		return msg
	}
}

// handleDiffLoaded installs a finished fetch and rebuilds the rows, putting the
// cursor back on the line it was on when this is a reload.
func (m Model) handleDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || msg.gen != d.gen {
		return m, nil
	}
	d.loading = false
	d.err = msg.err
	if msg.err != nil {
		m.status = "diff failed: " + msg.err.Error()
		return m, nil
	}
	anchor := d.cursorAnchor()
	d.diff, d.threads, d.drafts = msg.diff, msg.threads, msg.drafts
	if msg.versions != nil {
		d.versions = msg.versions
	}
	d.install(msg.built)
	d.restoreCursor(anchor)
	m.status = d.summary()
	return m, nil
}

// summary is the status line for a loaded diff: what it holds, and whether the
// forge handed all of it over.
func (d *diffState) summary() string {
	if d.diff == nil {
		return ""
	}
	s := d.label + " · " + plural(len(d.diff.Files), "file")
	if n := len(forge.InlineThreads(d.threads)); n > 0 {
		s += " · " + plural(n, "inline thread")
	}
	if d.diff.Truncated {
		s += " · diff truncated by the forge"
	}
	return s
}

// --- building the rows -----------------------------------------------------

// diffBuild is everything loading a diff produces: the rows the code panel
// draws, the tree the file panel draws, and the two indexes that keep the panels
// pointing at the same file.
type diffBuild struct {
	rows     []diffRow
	tree     []diffTreeRow
	gutter   int
	fileHead []int // diff row of each file's header
	treeOf   []int // tree row of each file
}

// install adopts a finished build, keeping whatever was folded when the file
// count hasn't changed — a reload after a note must not unfold the diff you
// were working through.
func (d *diffState) install(b diffBuild) {
	d.rows, d.tree, d.gutter = b.rows, b.tree, b.gutter
	d.fileHead, d.treeOf = b.fileHead, b.treeOf
	if len(d.collapsed) != len(b.fileHead) {
		d.collapsed = make([]bool, len(b.fileHead))
	}
	d.rebuildVisible()
}

// rebuildVisible recomputes which rows are on screen. Called on load and on
// every fold; O(rows), which at a keystroke is free.
func (d *diffState) rebuildVisible() {
	d.visible = d.visible[:0]
	if cap(d.visPos) < len(d.rows) {
		d.visPos = make([]int, len(d.rows))
	}
	d.visPos = d.visPos[:len(d.rows)]
	for i, r := range d.rows {
		if r.kind != diffRowFile && r.file >= 0 && r.file < len(d.collapsed) && d.collapsed[r.file] {
			d.visPos[i] = -1
			continue
		}
		d.visPos[i] = len(d.visible)
		d.visible = append(d.visible, i)
	}
}

// pos is where the cursor sits in the visible rows, or -1 when its row is
// folded away.
func (d *diffState) pos() int {
	if d.cursor < 0 || d.cursor >= len(d.visPos) {
		return -1
	}
	return d.visPos[d.cursor]
}

// setPos puts the cursor on the p'th visible row, clamped.
func (d *diffState) setPos(p int) {
	if len(d.visible) == 0 {
		return
	}
	p = min(max(p, 0), len(d.visible)-1)
	d.cursor = d.visible[p]
	d.clampCursor()
}

// toggleCollapse folds or unfolds one file. The cursor comes up to the file
// header when it was standing on a line that is about to disappear.
func (d *diffState) toggleCollapse(fi int) {
	if fi < 0 || fi >= len(d.collapsed) {
		return
	}
	d.collapsed[fi] = !d.collapsed[fi]
	if d.collapsed[fi] && d.cursor < len(d.rows) && d.rows[d.cursor].file == fi {
		d.cursor = d.fileHead[fi]
	}
	d.rebuildVisible()
	d.clampCursor()
}

// setAllCollapsed folds or unfolds every file — the "show me the shape of this
// merge request" key, and the way back out of it.
func (d *diffState) setAllCollapsed(v bool) {
	for i := range d.collapsed {
		d.collapsed[i] = v
	}
	if v && d.cursor < len(d.rows) {
		if fi := d.rows[d.cursor].file; fi >= 0 && fi < len(d.fileHead) {
			d.cursor = d.fileHead[fi]
		}
	}
	d.rebuildVisible()
	d.clampCursor()
}

// allCollapsed reports whether every file is folded, which is what makes the
// fold-all key a toggle.
func (d *diffState) allCollapsed() bool {
	for _, c := range d.collapsed {
		if !c {
			return false
		}
	}
	return len(d.collapsed) > 0
}

// cursorFile is the file the cursor is in, or -1.
func (d *diffState) cursorFile() int {
	if d.cursor < 0 || d.cursor >= len(d.rows) {
		return -1
	}
	return d.rows[d.cursor].file
}

// hiddenRows is how many rows a folded file is keeping out of sight, for its
// header to say so.
func (d *diffState) hiddenRows(fi int) int {
	if fi < 0 || fi >= len(d.fileHead) {
		return 0
	}
	n := 0
	for i := d.fileHead[fi] + 1; i < len(d.rows) && d.rows[i].file == fi && d.rows[i].kind != diffRowFile; i++ {
		n++
	}
	return n
}

// buildDiff turns a fetched diff into everything the view draws: a header per
// file, its hunks, every existing conversation under the line it hangs off, and
// the file tree beside them.
//
// A pure function rather than a method because it runs on the fetch goroutine —
// it is the expensive half of loading a diff (chroma over every file), and the
// UI thread must not wait on it. Everything it produces is width-independent,
// so it survives every resize.
func buildDiff(d *forge.Diff, threads []forge.Thread, drafts []forge.Draft, full map[int][]string) diffBuild {
	b := diffBuild{gutter: 3}
	if d == nil {
		return b
	}
	b.fileHead = make([]int, len(d.Files))
	stats := make([]diffFileStat, len(d.Files))
	// On the lines: the inline conversations on this head. The others (on the
	// change request as a whole, or outdated) go to Discussions at the end.
	var onLines []forge.Thread
	var rest []int
	for i, t := range threads {
		if t.Inline() && !t.Outdated(d.Refs.HeadSHA) {
			onLines = append(onLines, t)
		} else {
			onLines = append(onLines, forge.Thread{}) // keeps the indexes
			rest = append(rest, i)
		}
	}
	byLine := diffThreadIndex(onLines)
	for fi, f := range d.Files {
		b.fileHead[fi] = len(b.rows)
		b.rows = append(b.rows, diffRow{kind: diffRowFile, file: fi, thread: -1, text: fileHeaderText(f)})
		if f.Binary {
			b.rows = append(b.rows, diffRow{kind: diffRowMeta, file: fi, thread: -1, text: "binary file — not shown"})
			continue
		}
		if f.TooLarge {
			b.rows = append(b.rows, diffRow{kind: diffRowMeta, file: fi, thread: -1, text: "too large for GitLab to send — o opens it there"})
			continue
		}
		lines := forge.ParseUnifiedDiff(f.Diff)
		if text, ok := full[fi]; ok {
			lines = forge.ExpandDiff(lines, text)
		}
		code := highlightDiffFile(f, lines)
		for i, l := range lines {
			b.rows = append(b.rows, diffRow{
				kind: rowKindFor(l.Kind), file: fi, old: l.OldLine, new: l.NewLine,
				text: code[i], thread: -1, raw: l.Text, oldPos: l.OldPos, newPos: l.NewPos,
			})
			switch l.Kind {
			case forge.DiffAdd:
				stats[fi].adds++
			case forge.DiffDel:
				stats[fi].dels++
			}
			b.gutter = max(b.gutter, diffDigits(max(l.OldLine, l.NewLine)))
			notes := noteRowsFor(byLine, threads, f, l, fi)
			notes = withDraftReplies(notes, threads, drafts)
			notes = append(notes, draftRowsOn(drafts, f, l, fi)...)
			open, done := countThreadHeads(notes, threads)
			stats[fi].threads += open
			stats[fi].resolved += done
			b.rows = append(b.rows, notes...)
		}
	}
	if len(rest) > 0 {
		b.rows = append(b.rows, diffRow{kind: diffRowFile, file: -1, thread: -1, text: "Discussions"})
		for _, ti := range rest {
			t := threads[ti]
			if t.Inline() {
				n, old := t.Line()
				where := t.Path + ":" + strconv.Itoa(n)
				if old {
					where += " (removed)"
				}
				b.rows = append(b.rows, diffRow{kind: diffRowMeta, file: -1, thread: -1, text: "outdated · " + where})
			}
			b.rows = append(b.rows, withDraftReplies(diffThreadRows(t, ti, -1), threads, drafts)...)
		}
	}
	var general []diffRow // pending notes on the merge request as a whole
	for di, dr := range drafts {
		if dr.ReplyTo == "" && dr.Path == "" {
			general = append(general, draftRows(dr, di, -1)...)
		}
	}
	if len(general) > 0 {
		if len(rest) == 0 {
			b.rows = append(b.rows, diffRow{kind: diffRowFile, file: -1, thread: -1, text: "Discussions"})
		}
		b.rows = append(b.rows, general...)
	}
	if d.Truncated {
		b.rows = append(b.rows, diffRow{kind: diffRowMeta, file: -1, thread: -1,
			text: "the forge truncated this diff — the rest is only on the web"})
	}
	b.tree, b.treeOf = buildDiffTree(d, stats)
	return b
}

// countThreadHeads counts the conversations in a run of note rows — the head
// row of each, which is the one that names an author — split into the ones
// still open and the ones already resolved. The tree shows the first number:
// what is left to answer is the useful one.
func countThreadHeads(rows []diffRow, threads []forge.Thread) (open, resolved int) {
	for _, r := range rows {
		if !r.noteHead {
			continue
		}
		if r.thread >= 0 && r.thread < len(threads) && threads[r.thread].Resolved {
			resolved++
			continue
		}
		open++
	}
	return open, resolved
}

// rowKindFor maps a parsed diff line onto the row kind that draws it.
func rowKindFor(k forge.DiffLineKind) diffRowKind {
	switch k {
	case forge.DiffHunk:
		return diffRowHunk
	case forge.DiffAdd:
		return diffRowAdd
	case forge.DiffDel:
		return diffRowDel
	case forge.DiffMeta:
		return diffRowMeta
	}
	return diffRowContext
}

// fileHeaderText is the per-file header: the path, plus a word for what
// happened to it when it is not a plain edit.
func fileHeaderText(f forge.FileDiff) string {
	path := f.Path()
	switch {
	case f.Renamed && f.OldPath != "" && f.OldPath != f.NewPath:
		path = f.OldPath + " → " + f.NewPath
	case f.New:
		path += "  (new)"
	case f.Deleted:
		path += "  (deleted)"
	}
	if f.Generated {
		path += "  (generated)"
	}
	return path
}

// threadIndex keys the inline conversations by the line they hang off, so build
// attaches them in one pass instead of scanning the whole list per line.
func diffThreadIndex(threads []forge.Thread) map[string][]int {
	idx := map[string][]int{}
	for i, t := range threads {
		switch {
		case t.NewLine > 0:
			k := diffThreadKey(t.Path, true, t.NewLine)
			idx[k] = append(idx[k], i)
		case t.OldLine > 0:
			p := t.OldPath
			if p == "" {
				p = t.Path
			}
			k := diffThreadKey(p, false, t.OldLine)
			idx[k] = append(idx[k], i)
		}
	}
	return idx
}

// threadKey identifies a line of a file on one side of the diff. The side is
// part of the key because a removed line 12 and an added line 12 of the same
// file are different places.
func diffThreadKey(path string, newSide bool, line int) string {
	side := "o"
	if newSide {
		side = "n"
	}
	return path + "\x00" + side + fmt.Sprint(line)
}

// noteRows lays the conversations anchored to one diff line out as rows — one
// row per line of note text, so the view's "a row is a line" arithmetic (and
// therefore its scrolling) holds for notes as well as code.
func noteRowsFor(idx map[string][]int, threads []forge.Thread, f forge.FileDiff, l forge.DiffLine, fi int) []diffRow {
	// The overwhelmingly common case, and this runs per line of the diff: no
	// conversations at all, or a line (a hunk header) that cannot carry one.
	if len(idx) == 0 || (l.NewLine == 0 && l.OldLine == 0) {
		return nil
	}
	var out []diffRow
	var taken []int // which threads this line already drew — at most a couple
	add := func(key string) {
		for _, ti := range idx[key] {
			if slices.Contains(taken, ti) {
				continue
			}
			taken = append(taken, ti)
			out = append(out, diffThreadRows(threads[ti], ti, fi)...)
		}
	}
	if l.NewLine > 0 {
		add(diffThreadKey(f.Path(), true, l.NewLine))
	}
	if l.OldLine > 0 {
		old := f.OldPath
		if old == "" {
			old = f.Path()
		}
		add(diffThreadKey(old, false, l.OldLine))
	}
	return out
}

// draftRows draws a pending note, marked as such: only you see it until the
// review is submitted.
func draftRows(dr forge.Draft, di, fi int) []diffRow {
	out := []diffRow{{kind: diffRowNote, file: fi, thread: -1, draft: di + 1, text: "✎ you (pending)", noteHead: true}}
	for _, ln := range strings.Split(strings.TrimRight(dr.Body, "\n"), "\n") {
		out = append(out, diffRow{kind: diffRowNote, file: fi, thread: -1, draft: di + 1, text: "  " + ln})
	}
	return out
}

// draftRowsOn are the pending notes that start a thread on line l.
func draftRowsOn(drafts []forge.Draft, f forge.FileDiff, l forge.DiffLine, fi int) []diffRow {
	var out []diffRow
	for di, dr := range drafts {
		switch {
		case dr.ReplyTo != "":
		case l.NewLine > 0 && dr.NewLine == l.NewLine && dr.Path == f.Path(),
			l.NewLine == 0 && l.OldLine > 0 && dr.NewLine == 0 && dr.OldLine == l.OldLine && cmp.Or(dr.OldPath, dr.Path) == cmp.Or(f.OldPath, f.Path()):
			out = append(out, draftRows(dr, di, fi)...)
		}
	}
	return out
}

// withDraftReplies is a run of thread rows with each thread's pending
// replies after it.
func withDraftReplies(rows []diffRow, threads []forge.Thread, drafts []forge.Draft) []diffRow {
	if len(drafts) == 0 || len(rows) == 0 {
		return rows
	}
	var out []diffRow
	for i, r := range rows {
		out = append(out, r)
		if last := i == len(rows)-1 || rows[i+1].thread != r.thread; last && r.thread >= 0 && r.thread < len(threads) {
			for di, dr := range drafts {
				if dr.ReplyTo == threads[r.thread].ID {
					out = append(out, draftRows(dr, di, r.file)...)
				}
			}
		}
	}
	return out
}

// threadRows draws one conversation: a head line per note (author, and a ✓ when
// the thread is resolved), then its body one row per line.
func diffThreadRows(t forge.Thread, ti, fi int) []diffRow {
	var out []diffRow
	for i, n := range t.Notes {
		head := "💬 " + n.Author
		if i > 0 {
			head = "↳ " + n.Author
		}
		if !n.Created.IsZero() {
			head += " · " + age(n.Created) + " ago"
		}
		if t.Resolved && i == 0 {
			head += " · resolved"
		}
		out = append(out, diffRow{kind: diffRowNote, file: fi, thread: ti, text: head, noteHead: true})
		for _, ln := range strings.Split(strings.TrimRight(n.Body, "\n"), "\n") {
			out = append(out, diffRow{kind: diffRowNote, file: fi, thread: ti, text: "  " + ln})
		}
	}
	return out
}

// digits is how many columns a line number needs.
func diffDigits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// diffHighlightMaxLines is the per-file cap on syntax highlighting. See
// highlightDiffFile.
const diffHighlightMaxLines = 3000

// diffTabWidth is what a tab expands to before highlighting. Fixed, like the
// file previews next door: the alternative is tracking tab stops through a line
// we are about to truncate to the pane width anyway.
const diffTabWidth = 4

// highlightDiffFile syntax-highlights one file's diff lines, returning exactly
// one rendered string per input line.
//
// The file's code goes through chroma in a single pass rather than line by
// line, so a lexer's multi-line state (block comments, string literals) mostly
// survives. Added and removed lines are interleaved in that pass, which no
// lexer can make complete sense of, but it still beats restarting the lexer on
// every row — which is what per-line highlighting is.
func highlightDiffFile(f forge.FileDiff, lines []forge.DiffLine) []string {
	lexer := lexerForFilename(f.Path())
	// Chroma costs tens of microseconds a line, which is nothing on a normal
	// file and seconds on a generated one. Past the cap the file renders flat:
	// a twenty-thousand-line lockfile is the diff where highlighting helps
	// least and costs most.
	if len(lines) > diffHighlightMaxLines {
		lexer = nil
	}
	code := make([]string, len(lines))
	idx := make([]int, 0, len(lines))
	body := make([]string, 0, len(lines))
	for i, l := range lines {
		switch l.Kind {
		case forge.DiffHunk, forge.DiffMeta:
			code[i] = l.Text
		default:
			idx = append(idx, i)
			body = append(body, expandTabs(l.Text, diffTabWidth))
		}
	}
	for j, styled := range highlightWithLexer(body, lexer) {
		code[idx[j]] = styled
	}
	return code
}

// --- cursor and scrolling --------------------------------------------------

// diffAnchor identifies the line under the cursor across a rebuild: rows shift
// when a note is added above them, so the row index alone is not a position.
type diffAnchor struct {
	file, old, new int
	// thread is set instead when the cursor was in an existing conversation:
	// replying reloads the view, and landing back at the top of the diff after
	// a reply is not where anyone wants to be.
	thread string
	ok     bool
}

func (d *diffState) cursorAnchor() diffAnchor {
	if d.cursor < 0 || d.cursor >= len(d.rows) {
		return diffAnchor{}
	}
	r := d.rows[d.cursor]
	if r.kind == diffRowNote && r.thread >= 0 && r.thread < len(d.threads) {
		return diffAnchor{thread: d.threads[r.thread].ID, ok: true}
	}
	return diffAnchor{file: r.file, old: r.old, new: r.new, ok: r.kind.commentable()}
}

func (d *diffState) restoreCursor(a diffAnchor) {
	defer d.syncTreeToCursor()
	if a.ok {
		for i, r := range d.rows {
			if a.thread != "" {
				if r.kind == diffRowNote && r.thread >= 0 && r.thread < len(d.threads) &&
					d.threads[r.thread].ID == a.thread {
					d.cursor = i
					d.clampCursor()
					return
				}
				continue
			}
			if r.file == a.file && r.old == a.old && r.new == a.new && r.kind.commentable() {
				d.cursor = i
				d.clampCursor()
				return
			}
		}
	}
	d.cursorToFirstCode()
}

// cursorToFirstCode parks the cursor on the first line a note could hang off,
// so the comment key works without having to move first.
func (d *diffState) cursorToFirstCode() {
	for i, r := range d.rows {
		if r.kind.commentable() {
			d.cursor = i
			d.top = 0
			d.clampCursor()
			return
		}
	}
	d.cursor, d.top = 0, 0
}

// clampCursor keeps the cursor inside the rows and the window around the
// cursor.
func (d *diffState) clampCursor() {
	if len(d.rows) == 0 || len(d.visible) == 0 {
		d.cursor, d.top = 0, 0
		return
	}
	d.cursor = min(max(d.cursor, 0), len(d.rows)-1)
	// A cursor on a folded row rides up to its file header, which is never
	// folded away.
	if d.pos() < 0 {
		if fi := d.cursorFile(); fi >= 0 && fi < len(d.fileHead) {
			d.cursor = d.fileHead[fi]
		} else {
			d.cursor = d.visible[0]
		}
	}
	h := d.viewH
	if h <= 0 {
		return
	}
	p := d.pos()
	if p < d.top+diffScrollOff {
		d.top = p - diffScrollOff
	}
	if p > d.top+h-1-diffScrollOff {
		d.top = p - h + 1 + diffScrollOff
	}
	d.top = min(max(d.top, 0), max(0, len(d.visible)-h))
}

// move steps the cursor by n rows, taking the tree's highlight along to
// whichever file that lands in.
func (d *diffState) move(n int) {
	d.setPos(d.pos() + n)
	d.syncTreeToCursor()
}

// moveFile jumps to the next/previous file header, landing it at the top of the
// window: a file is read from its first line down, not from its middle.
func (d *diffState) moveFile(dir int) {
	for p := d.pos() + dir; p >= 0 && p < len(d.visible); p += dir {
		if d.rows[d.visible[p]].kind == diffRowFile {
			d.cursor = d.visible[p]
			d.top = p
			d.clampCursor()
			d.syncTreeToCursor()
			return
		}
	}
	if dir > 0 {
		d.setPos(len(d.visible) - 1)
	} else {
		d.setPos(0)
	}
	d.syncTreeToCursor()
}

// moveThread jumps to the next/previous existing conversation, reporting
// whether there was one.
// A conversation inside a folded file is not on screen, so n/N walk past it —
// the same rule the window itself follows.
func (d *diffState) moveThread(dir int) bool {
	for p := d.pos() + dir; p >= 0 && p < len(d.visible); p += dir {
		if d.rows[d.visible[p]].kind == diffRowNote {
			d.setPos(p)
			d.syncTreeToCursor()
			return true
		}
	}
	return false
}

// --- keys ------------------------------------------------------------------

// handleDiffKey owns every keystroke while the review view is up. The keys are
// hardwired rather than registry actions: the view is a modal surface of its
// own, and its vocabulary (pan, next file, next thread) exists nowhere else in
// the app to collide with.
//
// Tab moves between the two panels, and the arrows then mean whatever that
// panel means by them — which is why the tree can pan horizontally like the
// diff does.
func (m Model) handleDiffKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil {
		return m, nil
	}
	if d.picking {
		return m.handleDiffVersionKey(msg)
	}
	if d.note.active {
		return m.handleDiffNoteKey(msg)
	}
	if d.verdicts {
		return m.handleDiffVerdictKey(msg)
	}
	if d.merges != nil {
		return m.handleDiffMergeKey(msg)
	}
	if key.Matches(msg, m.keys.Help) {
		m.openHelp("Diff review")
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q":
		if d.mark > 0 { // the range first
			d.mark = 0
			m.status = ""
			return m, nil
		}
		m.closeDiffView()
		return m, nil
	case "tab", "shift+tab":
		if m.diffTreeShown() {
			d.treeFocus = !d.treeFocus
			if d.treeFocus {
				d.syncTreeToCursor()
			}
		}
		return m, nil
	case "r":
		return m.refreshDiffView()
	case "S":
		d.verdicts, d.verdict = true, 0
		return m, nil
	case "A":
		return m, m.approveMR(d.c, d.repo, d.number)
	case "M":
		return m.openDiffMerge()
	case "C":
		if m.mr == nil || m.mr.mr == nil {
			return m, nil
		}
		return m, m.startMRReview(m.mr.mr, m.mr.ref)
	case "v":
		if len(d.versions) < 2 {
			m.status = "one version: nothing pushed since it opened"
			return m, nil
		}
		d.picking = true
		d.pick = max(slices.IndexFunc(d.versions, func(v forge.Version) bool { return v.ID == d.version }), 0)
		return m, nil
	case "o":
		if d.webURL == "" {
			return m, nil
		}
		m.status = "opening " + d.webURL + "…"
		return m, m.openOpenable(openable{name: d.label, url: d.webURL})
	}
	if d.treeFocus {
		return m.handleDiffTreeKey(msg)
	}
	return m.handleDiffCodeKey(msg)
}

// handleDiffVersionKey is v's list of versions: enter shows the one under
// the cursor.
func (m Model) handleDiffVersionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "v", "q":
		d.picking = false
	case "up", "k":
		d.pick = max(d.pick-1, 0)
	case "down", "j":
		d.pick = min(d.pick+1, len(d.versions)-1)
	case "enter":
		d.picking = false
		id := 0
		if d.pick > 0 {
			id = d.versions[d.pick].ID
		}
		if id == d.version {
			return m, nil
		}
		d.version, d.gen, d.loading, d.err = id, d.gen+1, true, nil
		d.collapsed, d.full = nil, nil
		m.status = "loading " + d.versionName(d.pick) + "…"
		return m, m.fetchDiff()
	}
	return m, nil
}

// versionName is version i as the list and the title name it: v3, the
// newest the highest.
func (d *diffState) versionName(i int) string {
	return "version " + strconv.Itoa(len(d.versions)-i)
}

// versionLines are v's list, the cursor's row marked.
func (d *diffState) versionLines(width int) []string {
	lines := []string{refKeyStyle.Render("Versions") + refDimStyle.Render("  ↵ show · esc close")}
	for i, v := range d.versions {
		line := d.versionName(i)
		if i == 0 {
			line += " (newest)"
		}
		if !v.Created.IsZero() {
			line += refDimStyle.Render(" · " + age(v.Created) + " ago")
		}
		if sha := v.Refs.HeadSHA; len(sha) >= 8 {
			line += refDimStyle.Render(" · " + sha[:8])
		}
		mark := "  "
		if v.ID == d.version || i == 0 && d.version == 0 {
			mark = "✓ "
		}
		line = truncate(mark+line, width)
		if i == d.pick {
			line = selectedRow.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return lines
}

// handleDiffTreeKey is the file panel: ↑/↓ walk the files (the diff follows),
// ←/→ pan a path too long for the column, ↵ hands the keys back to the diff.
func (m Model) handleDiffTreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	switch msg.String() {
	case "up", "k":
		d.treeMove(-1)
	case "down", "j":
		d.treeMove(1)
	case "pgup", "ctrl+u":
		d.treeMove(-max(1, d.treeH-1))
	case "pgdown", "ctrl+d":
		d.treeMove(max(1, d.treeH-1))
	case "home", "g":
		d.treeHome()
	case "end", "G":
		d.treeEnd()
	case "left", "h":
		d.treeHScroll = max(0, d.treeHScroll-diffHScrollStep)
	case "right", "l":
		d.treeHScroll += diffHScrollStep
	case "]":
		d.treeMove(1)
	case "[":
		d.treeMove(-1)
	case "enter":
		d.treeFocus = false
	case "z":
		if d.treeIdx < len(d.tree) {
			d.toggleCollapse(d.tree[d.treeIdx].file)
		}
	case "Z":
		d.setAllCollapsed(!d.allCollapsed())
	case "R":
		return m.toggleDiffResolve()
	}
	return m, nil
}

// handleDiffCodeKey is the diff panel.
func (m Model) handleDiffCodeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	switch msg.String() {
	case "up", "k":
		d.move(-1)
	case "down", "j":
		d.move(1)
	case "pgup", "ctrl+u":
		d.move(-max(1, d.viewH-1))
	case "pgdown", "ctrl+d":
		d.move(max(1, d.viewH-1))
	case "home", "g":
		d.top = 0
		d.setPos(0)
		d.syncTreeToCursor()
	case "end", "G":
		d.setPos(len(d.visible) - 1)
		d.syncTreeToCursor()
	case "left", "h":
		d.hscroll = max(0, d.hscroll-diffHScrollStep)
	case "right", "l":
		d.hscroll += diffHScrollStep
	case "]":
		d.moveFile(1)
	case "[":
		d.moveFile(-1)
	case "n":
		if !d.moveThread(1) {
			m.status = "no further inline threads"
		}
	case "N":
		if !d.moveThread(-1) {
			m.status = "no earlier inline threads"
		}
	case "e":
		return m, m.expandDiffFile(d.cursorFile())
	case "c":
		return m.openDiffNote()
	case "s":
		return m.openDiffSuggestion()
	case "V":
		return m.toggleDiffMark()
	case "R":
		return m.toggleDiffResolve()
	case "x":
		return m.deleteDiffDraft()
	case "E":
		return m.editDiffDraft()
	case "z":
		d.toggleCollapse(d.cursorFile())
		d.syncTreeToCursor()
	case "Z":
		d.setAllCollapsed(!d.allCollapsed())
		d.syncTreeToCursor()
	}
	return m, nil
}

// diffExpandedMsg is a file's lines filled in, or taken back out.
type diffExpandedMsg struct {
	gen   int
	full  map[int][]string
	built diffBuild
	err   error
}

// expandDiffFile fills in file fi's unchanged lines from its text at the
// head, or, expanded already, folds it back to its hunks. The text is read
// and the rows rebuilt off the UI goroutine, as a load is.
func (m *Model) expandDiffFile(fi int) tea.Cmd {
	d := m.diff
	if d == nil || d.diff == nil || fi < 0 || fi >= len(d.diff.Files) {
		return nil
	}
	f := d.diff.Files[fi]
	full := maps.Clone(d.full)
	if full == nil {
		full = map[int][]string{}
	}
	_, expanded := full[fi]
	switch {
	case expanded:
		delete(full, fi)
		m.status = f.Path() + ": the changes only"
	case f.Deleted, f.New, f.Binary, f.TooLarge:
		m.status = f.Path() + ": nothing more to show here"
		return nil
	default:
		m.status = "reading " + f.Path() + "…"
	}
	c, ctx, gen, diff, threads, drafts, repo, ref := d.c, m.ctx, d.gen, d.diff, d.threads, d.drafts, d.repo, d.diff.Refs.HeadSHA
	return func() tea.Msg {
		if !expanded {
			text, err := c.File(ctx, repo, f.NewPath, ref)
			if err != nil {
				return diffExpandedMsg{gen: gen, err: err}
			}
			full[fi] = text
		}
		return diffExpandedMsg{gen: gen, full: full, built: buildDiff(diff, threads, drafts, full)}
	}
}

func (m Model) handleDiffExpanded(msg diffExpandedMsg) (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil || msg.gen != d.gen {
		return m, nil
	}
	if msg.err != nil {
		m.status = "expand failed: " + msg.err.Error()
		return m, nil
	}
	anchor := d.cursorAnchor()
	d.full = msg.full
	d.install(msg.built)
	d.restoreCursor(anchor)
	m.status = d.summary()
	return m, nil
}

// refreshDiffView drops the client's cached diff and refetches it along with
// the conversations.
func (m Model) refreshDiffView() (tea.Model, tea.Cmd) {
	d := m.diff
	if d == nil {
		return m, nil
	}
	d.c.Invalidate(d.repo, d.number)
	d.gen++
	d.loading = true
	d.err = nil
	m.status = "reloading diff…"
	return m, m.fetchDiff()
}

// reloadDiffThreads refetches without dropping the client's cached diff: after
// a note the diff is unchanged, the conversation on it is not. The diff call
// is a cache hit, so this costs one request.
func (m Model) reloadDiffThreads() tea.Cmd {
	if m.diff == nil {
		return nil
	}
	m.diff.gen++
	return m.fetchDiff()
}

// --- rendering -------------------------------------------------------------

// renderDiffView draws the overlay: the framed, scrolled diff with a key hint
// at its foot.
func (m *Model) renderDiffView(bodyH int) string {
	d := m.diff
	if d == nil {
		return ""
	}
	// The whole terminal, less a column either side — a diff is why this view
	// is not a sheet. The inner width has to match renderModalFrame's own
	// arithmetic exactly, or the frame truncates rows that are already exact.
	outerW := max(1, m.width-2)
	inner := max(1, outerW-4) // border (2) + the frame's one-cell padding either side
	// Body height: the frame's border (2), its title row and rule (2), and the
	// hint row at the foot of the body.
	h := max(3, bodyH-5)
	// Under the composer: the line it is about stays in sight above it.
	d.viewH = h
	if note := m.renderDiffNote(); note != "" {
		d.viewH = max(h-lipgloss.Height(note), 1)
	}
	d.clampCursor()
	d.viewH = h

	// The tree takes a column off the left, when there is room for one. Its
	// rows and the code's are rendered to exact widths and stitched per row, so
	// the frame never has to wrap anything.
	treeW := diffTreeWidth(inner)
	codeW := inner
	var treeCol []string
	if treeW > 0 {
		codeW = inner - treeW - 1 // the divider column
		treeCol = d.renderTreeColumn(treeW, h)
	} else {
		d.treeH = 0
		d.treeFocus = false
	}

	body := make([]string, 0, h+1)
	switch {
	case d.err != nil:
		body = append(body, refErrStyle.Render(truncate(d.err.Error(), codeW)))
	case d.loading && d.diff == nil:
		body = append(body, refDimStyle.Render("loading diff…"))
	case len(d.rows) == 0:
		body = append(body, refDimStyle.Render("this change request has no diff"))
	default:
		for p := d.top; p < len(d.visible) && p < d.top+h; p++ {
			i := d.visible[p]
			body = append(body, d.renderRow(i, codeW, i == d.cursor))
		}
	}
	for len(body) < h {
		body = append(body, "")
	}
	if treeW > 0 {
		for i := range body {
			body[i] = treeCol[i] + diffTreeDivider + body[i]
		}
	}
	over := []string(nil)
	switch {
	case d.picking:
		over = d.versionLines(codeW)
	case d.verdicts:
		over = d.verdictLines(codeW)
	case d.merges != nil:
		over = d.mergeLines(codeW)
	}
	if over != nil { // over the diff's first rows
		for i, l := range over {
			if i < len(body) {
				l = diffPaint(l, codeW, diffFileBg)
				if treeW > 0 {
					l = treeCol[i] + diffTreeDivider + l
				}
				body[i] = l
			}
		}
	}
	body = append(body, refDimStyle.Render(truncate(m.diffHint(), inner)))
	return m.renderModalFrame(outerW, d.frameTitle(), d.scrollHint(), strings.Join(body, "\n"))
}

// renderModalFrame frames body at outerW wide: the title and hint, a rule,
// the rows, every one cut to the inner width.
func (m *Model) renderModalFrame(outerW int, title, hint, body string) string {
	inner := max(outerW-4, 1)
	dim := lipgloss.NewStyle().Foreground(dimColor)
	head := titleStyle.Render(title)
	if hint != "" {
		head += "  " + dim.Render(hint)
	}
	edge := lipgloss.NewStyle().Foreground(focusedColor)
	left, right := edge.Render(border.Left)+" ", " "+edge.Render(border.Right)
	var b strings.Builder
	b.WriteString(edge.Render(border.TopLeft + strings.Repeat(border.Top, outerW-2) + border.TopRight))
	line := func(s string) {
		s = truncate(s, inner)
		b.WriteString("\n" + left + s + strings.Repeat(" ", max(inner-lipgloss.Width(s), 0)) + right)
	}
	line(head)
	line(dim.Render(strings.Repeat("─", inner)))
	for _, l := range strings.Split(body, "\n") {
		line(l)
	}
	b.WriteString("\n" + edge.Render(border.BottomLeft+strings.Repeat(border.Bottom, outerW-2)+border.BottomRight))
	return b.String()
}

// frameTitle names the change request under review.
func (d *diffState) frameTitle() string {
	t := d.label
	if d.title != "" {
		t += " · " + d.title
	}
	if i := slices.IndexFunc(d.versions, func(v forge.Version) bool { return v.ID == d.version }); d.version != 0 && i >= 0 {
		t += " · " + d.versionName(i) + " of " + strconv.Itoa(len(d.versions))
	}
	return t
}

// scrollHint is the position counter beside the title: which file the cursor is
// in, and how far into the diff it is.
func (d *diffState) scrollHint() string {
	if len(d.rows) == 0 || d.diff == nil {
		return ""
	}
	r := d.rows[min(d.cursor, len(d.rows)-1)]
	pos := fmt.Sprintf("%d/%d", max(d.pos(), 0)+1, len(d.visible))
	if r.file >= 0 && r.file < len(d.diff.Files) {
		return fmt.Sprintf("%s · file %d/%d · %s",
			truncate(d.diff.Files[r.file].Path(), 40), r.file+1, len(d.diff.Files), pos)
	}
	return pos
}

// diffHint is the key line at the foot of the view: the keys of the panel that
// has them, and only the ones that do something at the cursor. The rest
// (whole file, fold, pan, versions, reload, browser) are on ?: this line has
// to fit on a laptop terminal, and it is the review keys people need in
// front of them.
func (m *Model) diffHint() string {
	d := m.diff
	if d == nil {
		return ""
	}
	if d.note.active { // the composer has its own
		return ""
	}
	keys := helpKey(m.keys.Help) + " keys · esc close"
	tab := ""
	if m.diffTreeShown() {
		tab = "tab files · "
		if d.treeFocus {
			tab = "tab diff · "
		}
	}
	if d.treeFocus {
		return "↑/↓ file · z/Z fold · " + tab + keys
	}
	note, resolve := "c note · s suggest · V range · ", ""
	if d.mark > 0 {
		note = "c note the range · s suggest · esc drop it · "
	}
	if d.cursor < len(d.rows) && d.rows[d.cursor].draft > 0 {
		note, resolve = "", "E edit · x drop · "
	}
	review := "S submit · A approve · "
	if n := len(d.drafts); n > 0 {
		review = "S submit " + plural(n, "pending note") + " · A approve · "
	}
	if ti := d.threadAtCursor(); ti >= 0 {
		if d.rows[d.cursor].kind == diffRowNote {
			note = "c reply · "
		}
		switch t := d.threads[ti]; {
		case t.Resolved:
			resolve = "R reopen · "
		case t.Resolvable:
			resolve = "R resolve · "
		}
	}
	return note + resolve + review + "]/[ file · n/N thread · " + tab + keys
}

// renderRow draws one row to exactly width cells: the line-number gutter, the
// change marker, then the already-highlighted code, panned by the horizontal
// scroll and tinted by what kind of line it is.
func (d *diffState) renderRow(i, width int, cursor bool) string {
	r := d.rows[i]
	bg := diffRowBG(r.kind, cursor || d.inMark(i))
	switch r.kind {
	case diffRowFile:
		// The caret is the fold state, and a folded file says what it is
		// keeping back so the row is not a dead end.
		text := "▾ " + r.text
		if r.file >= 0 && r.file < len(d.collapsed) && d.collapsed[r.file] {
			text = "▸ " + r.text + refDimStyle.Render(fmt.Sprintf("  (%d lines folded)", d.hiddenRows(r.file)))
		}
		return diffPaint(diffPiece(diffFileStyle, " ")+keepBG(ansi.Truncate(text, width-1, "…"), ""), width, bg)
	case diffRowNote:
		return diffPaint(diffPiece(diffNoteRowStyle(r.text), diffNoteIndent+truncate(r.text, width-len(diffNoteIndent))), width, bg)
	}
	gutter := d.renderGutter(r)
	avail := width - visualWidth(gutter)
	if avail < 1 {
		return diffPaint(gutter, width, bg)
	}
	code := r.text
	switch r.kind {
	case diffRowHunk:
		code = diffPiece(diffHunkStyle, truncate(code, avail))
	case diffRowMeta:
		code = diffPiece(diffMetaStyle, truncate(code, avail))
	default:
		if d.hscroll > 0 {
			code = ansi.Cut(code, d.hscroll, d.hscroll+avail)
		} else {
			code = ansi.Truncate(code, avail, "")
		}
		code = keepBG(code, "")
	}
	return diffPaint(gutter+code, width, bg)
}

// diffNoteIndent lines an existing conversation up under the code it is about,
// clear of the gutter.
const diffNoteIndent = "    "

// noteStyle picks the colour for a note row: the author head line stands out,
// the body is quiet.
func diffNoteRowStyle(text string) lipgloss.Style {
	if strings.HasPrefix(text, "💬") || strings.HasPrefix(text, "↳") {
		return diffNoteHead
	}
	return diffNoteBody
}

// renderGutter draws the two line-number columns and the change marker. Both
// sides are shown — which line of the old file, which of the new — because that
// is exactly what an inline note is anchored to, and getting it wrong puts the
// comment on the wrong line.
func (d *diffState) renderGutter(r diffRow) string {
	w := d.gutter
	old, new := strings.Repeat(" ", w), strings.Repeat(" ", w)
	if r.old > 0 {
		old = fmt.Sprintf("%*d", w, r.old)
	}
	if r.new > 0 {
		new = fmt.Sprintf("%*d", w, r.new)
	}
	mark, markStyle := " ", diffGutterStyle
	switch r.kind {
	case diffRowAdd:
		mark, markStyle = "+", diffAddMark
	case diffRowDel:
		mark, markStyle = "-", diffDelMark
	}
	return diffPiece(diffGutterStyle, old+" "+new+" ") + diffPiece(markStyle, mark) + " "
}

// diffRowBG is the tint behind a row, or nil for none. A colourless terminal
// (NO_COLOR) gets no tints at all — the +/- markers and the line numbers still
// say which side a line is on, which is what that terminal asked for.
func diffRowBG(k diffRowKind, cursor bool) color.Color {
	if !codeColorEnabled {
		return nil
	}
	switch k {
	case diffRowAdd:
		if cursor {
			return diffAddCursorBg
		}
		return diffAddBg
	case diffRowDel:
		if cursor {
			return diffDelCursorBg
		}
		return diffDelBg
	case diffRowFile:
		if cursor {
			return diffFileCursorBg
		}
		return diffFileBg
	}
	if cursor {
		return diffCursorBg
	}
	return nil
}

// diffPiece renders text in st and neutralises the reset lipgloss ends it with,
// so the row's background survives the styled span.
func diffPiece(st lipgloss.Style, text string) string {
	return keepBG(st.Render(text), "")
}

// adaptiveColor is one tint for a light and a dark terminal, resolved at
// render time against termBG (dark until the terminal says).
type adaptiveColor struct{ light, dark color.Color }

// RGBA satisfies color.Color.
func (c adaptiveColor) RGBA() (uint32, uint32, uint32, uint32) {
	if termBG != nil && !isDark(termBG) {
		return c.light.RGBA()
	}
	return c.dark.RGBA()
}

// diffPaint pads inner out to width and lays the row's background under the
// whole thing, so the tint runs to the right edge rather than stopping at the
// end of the code.
func diffPaint(inner string, width int, bg color.Color) string {
	if pad := width - visualWidth(inner); pad > 0 {
		inner += strings.Repeat(" ", pad)
	}
	if bg == nil {
		return inner
	}
	return lipgloss.NewStyle().Background(bg).Render(inner)
}
