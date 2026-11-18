// Package vcs answers laneway's questions about a checkout from git or, in
// a jj repository (colocated or not), from jj: the branch you're on, the
// repo's branches and checkouts, your commits, pushing and fetching. jj's
// bookmarks are the branches; its workspaces the worktrees. A command that
// fails reads as "nothing".
package vcs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// IsJJ reports whether dir is in a jj workspace: a .jj directory in it or
// above it, found before a .git one is.
func IsJJ(dir string) bool {
	dir, err := filepath.Abs(dir)
	for err == nil {
		if fi, err := os.Stat(filepath.Join(dir, ".jj")); err == nil && fi.IsDir() {
			return true
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

// jjRead runs a jj query in dir without snapshotting the working copy, so
// it stays quick and writes nothing.
func jjRead(ctx context.Context, dir string, args ...string) (string, error) {
	return jjRun(ctx, dir, append([]string{"--ignore-working-copy"}, args...)...)
}

// jjRun runs jj in dir, its output trimmed; an error carries jj's message.
func jjRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "jj", append([]string{"--no-pager", "--color=never", "-R", dir}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New("jj: " + msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs name in dir for what reaches outside: the remote, pushing,
// fetching. Its output is trimmed; an error carries what the command said.
// Tests swap it.
var Run = func(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(name + ": " + strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func lines(s string) []string {
	return slices.DeleteFunc(strings.Split(s, "\n"), func(l string) bool { return strings.TrimSpace(l) == "" })
}

// IsRepo reports whether dir is in a git or jj repository.
func IsRepo(dir string) bool {
	if IsJJ(dir) {
		return true
	}
	_, err := git(context.Background(), dir, "rev-parse", "--git-dir")
	return err == nil
}

// bookmarksTmpl prints a commit's local bookmarks, one a line.
const bookmarksTmpl = `local_bookmarks.map(|b| b.name() ++ "\n").join("")`

// trunkNames are the names of trunk()'s remote bookmarks, the ones every
// change sits on: main, master. A new branch's bookmark on the same commit
// isn't one, nor its copy in a colocated repo's git refs (name@git).
func trunkNames(ctx context.Context, dir string) []string {
	out, _ := jjRead(ctx, dir, "log", "--no-graph", "-r", "trunk()", "-T", `remote_bookmarks.map(|b| if(b.remote() != "git", b.name() ++ "\n")).join("")`)
	return lines(out)
}

// nearestBookmark is the bookmark on rev or its closest ancestor that isn't
// trunk's, "" when there is none. Bookmarks below where rev leaves trunk
// are history, branches merged long ago; one right there is a branch that
// started there and hasn't moved yet.
func nearestBookmark(ctx context.Context, dir, rev string, trunk []string) string {
	fork := "heads(::(" + rev + ") & ::trunk())"
	out, err := jjRead(ctx, dir, "log", "--no-graph", "-r", "heads((::("+rev+") & bookmarks()) ~ ::("+fork+")-)", "-T", bookmarksTmpl)
	if err != nil {
		return ""
	}
	for _, b := range lines(out) {
		if !slices.Contains(trunk, b) {
			return b
		}
	}
	return ""
}

// Branch is the branch dir is on: git's checked out branch, or in jj the
// bookmark on @ or its closest ancestor (not trunk's, main names no work).
// "" outside a repo, detached, or without one.
func Branch(dir string) string {
	ctx := context.Background()
	if IsJJ(dir) {
		return nearestBookmark(ctx, dir, "@", trunkNames(ctx, dir))
	}
	b, _ := git(ctx, dir, "branch", "--show-current")
	return b
}

// Branches are repo's local branches (jj: bookmarks).
func Branches(repo string) []string {
	ctx := context.Background()
	if IsJJ(repo) {
		out, _ := jjRead(ctx, repo, "bookmark", "list", "-T", `if(remote, "", if(present, name ++ "\n"))`)
		return lines(out)
	}
	out, _ := git(ctx, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	return strings.Fields(out)
}

// HasBranch reports whether repo has the local branch (jj: bookmark).
func HasBranch(repo, branch string) bool {
	if repo == "" || branch == "" {
		return false
	}
	if IsJJ(repo) {
		return slices.Contains(Branches(repo), branch)
	}
	// Listed and compared rather than asked for by name: the branch can
	// come from a request, and so never reaches git's command line.
	out, _ := git(context.Background(), repo, "for-each-ref", "--format=%(refname)", "refs/heads/")
	return slices.Contains(strings.Fields(out), "refs/heads/"+branch)
}

// DefaultBase is what new work starts from: the remote's default branch
// (origin/main), in jj trunk()'s remote bookmark (main@origin). "" when
// there is none.
func DefaultBase(repo string) string {
	ctx := context.Background()
	if !IsJJ(repo) {
		b, err := git(ctx, repo, "rev-parse", "--abbrev-ref", "origin/HEAD")
		if err != nil {
			return ""
		}
		return b
	}
	out, _ := jjRead(ctx, repo, "log", "--no-graph", "-r", "trunk()", "-T",
		`remote_bookmarks.map(|b| if(b.remote() != "git", b.name() ++ "@" ++ b.remote() ++ "\n")).join("")`)
	refs := lines(out)
	for _, r := range refs {
		if strings.HasSuffix(r, "@origin") {
			return r
		}
	}
	if len(refs) > 0 {
		return refs[0]
	}
	return ""
}

// root is the repo's top directory, cleaned.
func root(ctx context.Context, repo string) string {
	if IsJJ(repo) {
		r, _ := jjRead(ctx, repo, "root")
		return filepath.Clean(r)
	}
	r, _ := git(ctx, repo, "rev-parse", "--show-toplevel")
	return filepath.Clean(r)
}

// Checkouts are repo's other checkouts on a branch, not the one at repo
// itself: git's linked worktrees, jj's workspaces on a bookmark of their
// own. path → branch.
func Checkouts(repo string) map[string]string {
	if IsJJ(repo) {
		return workspaces(context.Background(), repo)
	}
	out, err := git(context.Background(), repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	wts := map[string]string{}
	for i, block := range strings.Split(out, "\n\n") {
		if i == 0 {
			continue // the main checkout
		}
		var p, b string
		for _, l := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(l, "worktree "); ok {
				p = v
			}
			if v, ok := strings.CutPrefix(l, "branch refs/heads/"); ok {
				b = v
			}
		}
		if b != "" {
			wts[p] = b
		}
	}
	return wts
}

// workspace is one of a jj repo's workspaces.
type workspace struct {
	name, path, commit string
}

// workspaceList is repo's workspaces but the one at repo.
func workspaceList(ctx context.Context, repo string) []workspace {
	out, err := jjRead(ctx, repo, "workspace", "list", "-T", `name ++ "\t" ++ root ++ "\t" ++ target.commit_id() ++ "\n"`)
	if err != nil {
		return nil
	}
	here := root(ctx, repo)
	var wss []workspace
	for _, l := range lines(out) {
		f := strings.Split(l, "\t")
		if len(f) != 3 {
			continue
		}
		ws := workspace{name: f[0], path: f[1], commit: f[2]}
		if ws.path == "" { // made before jj recorded roots
			ws.path, _ = jjRead(ctx, repo, "workspace", "root", "--name", ws.name)
		}
		if ws.path == "" || filepath.Clean(ws.path) == here {
			continue
		}
		wss = append(wss, ws)
	}
	return wss
}

func workspaces(ctx context.Context, repo string) map[string]string {
	trunk := trunkNames(ctx, repo)
	wss := map[string]string{}
	for _, ws := range workspaceList(ctx, repo) {
		if b := nearestBookmark(ctx, repo, ws.commit, trunk); b != "" {
			wss[ws.path] = b
		}
	}
	return wss
}

// DefaultMerged is what Kept checks a branch is merged into when given no
// base: HEAD in git, trunk() in jj.
func DefaultMerged(repo string) string {
	if IsJJ(repo) {
		return "trunk()"
	}
	return "HEAD"
}

// ErrUncommitted and ErrUnmerged are why a checkout must stay.
var (
	ErrUncommitted = errors.New("it has uncommitted changes")
	ErrUnmerged    = errors.New("not merged")
)

// Kept is why the checkout at path on branch must stay, nil when it can
// go: ErrUncommitted, ErrUnmerged (branch is not in base, "" for the
// default), or the command that couldn't tell. In jj, uncommitted is a
// working-copy commit with changes, and unmerged any change in the
// workspace's history that base lacks, also one past the bookmark.
func Kept(repo, path, branch, base string) error {
	ctx := context.Background()
	if IsJJ(path) {
		// No --ignore-working-copy: the snapshot picks up edits made since
		// jj last ran there.
		dirty, err := jjRun(ctx, path, "log", "--no-graph", "-r", "@ & ~empty()", "-T", "change_id")
		if err != nil {
			return err
		}
		if dirty != "" {
			return ErrUncommitted
		}
		out, err := jjRun(ctx, path, "log", "--no-graph", "-r", "(::@ ~ ::("+cmp.Or(base, DefaultMerged(path))+")) ~ empty()", "-T", `change_id ++ "\n"`)
		if err != nil {
			return err
		}
		if out != "" {
			return ErrUnmerged
		}
		return nil
	}
	out, err := git(ctx, path, "status", "--porcelain")
	if err != nil {
		return errors.New("git status: " + err.Error())
	}
	if out != "" {
		return ErrUncommitted
	}
	if _, err := git(ctx, repo, "merge-base", "--is-ancestor", branch, cmp.Or(base, DefaultMerged(repo))); err != nil {
		return ErrUnmerged
	}
	return nil
}

// AddWorkspace makes a jj workspace for branch beside repo
// (../<repo>.<branch slug>), on the bookmark, or on a new one made at base
// (trunk() for ""). Its path.
func AddWorkspace(ctx context.Context, repo, branch, base string) (string, error) {
	top := root(ctx, repo)
	name := slug(branch)
	path := filepath.Join(filepath.Dir(top), filepath.Base(top)+"."+name)
	if !HasBranch(repo, branch) {
		// At base, where git's new branch would point; it moves on when
		// you set it before pushing.
		if _, err := jjRun(ctx, repo, "bookmark", "create", branch, "-r", cmp.Or(base, "trunk()")); err != nil {
			return "", err
		}
	}
	// A colocated repo's workspace gets a git worktree as well.
	if _, err := jjRun(ctx, repo, "workspace", "add", "--name", name, "-r", "bookmarks(exact:"+strconv.Quote(branch)+")", path); err != nil {
		return "", err
	}
	return path, nil
}

// slug makes a workspace name of a branch: issue/ABC-1-fix → issue-ABC-1-fix.
func slug(branch string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, branch)
}

// ForgetWorkspace drops the jj workspace at path and deletes its directory.
// Its changes stay in the repo: jj snapshots them first.
func ForgetWorkspace(ctx context.Context, repo, path string) error {
	for _, ws := range workspaceList(ctx, repo) {
		if filepath.Clean(ws.path) != filepath.Clean(path) {
			continue
		}
		if _, err := jjRun(ctx, path, "workspace", "forget", ws.name); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		_, _ = git(ctx, repo, "worktree", "prune") // a colocated workspace's
		return nil
	}
	return errors.New("no jj workspace at " + path)
}

// RemoteURL is the origin remote's URL.
func RemoteURL(ctx context.Context, repo string) (string, error) {
	if !IsJJ(repo) {
		return Run(ctx, repo, "git", "remote", "get-url", "origin")
	}
	out, err := Run(ctx, repo, "jj", "--no-pager", "--color=never", "--ignore-working-copy", "git", "remote", "list")
	if err != nil {
		return "", err
	}
	for _, l := range lines(out) {
		if url, ok := strings.CutPrefix(l, "origin "); ok {
			return strings.TrimSpace(url), nil
		}
	}
	return "", errors.New("no origin remote in " + repo)
}

// Push pushes branch to origin and tracks it there. jj pushes the bookmark
// where it is: changes after it stay local until you move it.
func Push(ctx context.Context, repo, branch string) (string, error) {
	if !IsJJ(repo) {
		return Run(ctx, repo, "git", "push", "-u", "origin", branch)
	}
	if _, err := Run(ctx, repo, "jj", "--no-pager", "--color=never", "bookmark", "track", "exact:"+branch, "--remote", "origin"); err != nil &&
		!strings.Contains(err.Error(), "already tracked") {
		return "", err
	}
	return Run(ctx, repo, "jj", "--no-pager", "--color=never", "git", "push", "--remote", "origin", "-b", "exact:"+branch)
}

// Fetch fetches branches from origin.
func Fetch(ctx context.Context, repo string, branches ...string) error {
	if !IsJJ(repo) {
		_, err := Run(ctx, repo, "git", append([]string{"fetch", "origin"}, branches...)...)
		return err
	}
	args := []string{"--no-pager", "--color=never", "git", "fetch", "--remote", "origin"}
	for _, b := range branches {
		args = append(args, "-b", "exact:"+b)
	}
	_, err := Run(ctx, repo, "jj", args...)
	return err
}

// Commit is one of your commits.
type Commit struct {
	ID      string
	At      time.Time
	Subject string
}

// Commits are your commits in repo since since, on every branch, without
// merges; ok is false when the repo can't be read. jj's are the visible
// changes with content, not the versions rewriting left behind.
func Commits(repo string, since time.Time) (commits []Commit, ok bool) {
	ctx := context.Background()
	var out string
	if IsJJ(repo) {
		var err error
		out, err = jjRead(ctx, repo, "log", "--no-graph", "-r",
			`mine() & ~empty() & ~merges() & author_date(after:"`+since.Format(time.RFC3339)+`")`,
			"-T", `commit_id ++ "\t" ++ author.timestamp().format("%s") ++ "\t" ++ description.first_line() ++ "\n"`)
		if err != nil {
			return nil, false
		}
	} else {
		email, err := git(ctx, repo, "config", "user.email")
		if err != nil || email == "" {
			return nil, false
		}
		out, err = git(ctx, repo, "log", "--all", "--no-merges",
			"--since="+strconv.FormatInt(since.Unix(), 10), "--author="+email, "--format=%H%x09%at%x09%s")
		if err != nil {
			return nil, false
		}
	}
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		sec, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		commits = append(commits, Commit{ID: f[0], At: time.Unix(sec, 0), Subject: f[2]})
	}
	return commits, true
}

// Snapshot is a moment jj recorded your work on a change: a snapshot of
// your edits, a describe, a new change. Bookmark is its branch's: the
// change's, else the nearest one above it, else below it.
type Snapshot struct {
	At       time.Time
	Subject  string
	Bookmark string
}

// fetchOps are operations that rewrite changes for you, not by you.
var fetchOps = []string{"import git refs", "fetch from git remote", "push ", "export ", "update working copy"}

// Snapshots are the moments since from, oldest first, that jj recorded
// work on your changes in repo; nil outside a jj repo. git keeps none: its
// reflog has the branch switches instead.
func Snapshots(repo string, from time.Time) []Snapshot {
	ctx := context.Background()
	if !IsJJ(repo) {
		return nil
	}
	revs := `mine() & committer_date(after:"` + from.Format(time.RFC3339) + `")`
	out, err := jjRead(ctx, repo, "evolog", "--no-graph", "-r", revs, "-T",
		`if(operation, operation.time().start().format("%s") ++ "\t" ++ commit.change_id() ++ "\t" ++ operation.description() ++ "\t" ++ commit.description().first_line() ++ "\n")`)
	if err != nil {
		return nil
	}
	bookmarkOf := changeBookmarks(ctx, repo, revs)
	var snaps []Snapshot
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 4)
		if len(f) != 4 || slices.ContainsFunc(fetchOps, func(op string) bool { return strings.HasPrefix(f[2], op) }) {
			continue
		}
		sec, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil || time.Unix(sec, 0).Before(from) {
			continue
		}
		snaps = append(snaps, Snapshot{At: time.Unix(sec, 0), Subject: f[3], Bookmark: bookmarkOf[f[1]]})
	}
	slices.SortStableFunc(snaps, func(a, b Snapshot) int { return a.At.Compare(b.At) })
	return snaps
}

// changeBookmarks maps the changes in revs to their branch's bookmark: the
// one on the change, else the nearest above it, else below it, that isn't
// trunk's.
func changeBookmarks(ctx context.Context, repo, revs string) map[string]string {
	trunk := trunkNames(ctx, repo)
	out, err := jjRead(ctx, repo, "log", "--no-graph", "-r", "bookmarks() & (("+revs+")::) ~ ::trunk()", "-T", bookmarksTmpl)
	if err != nil {
		return nil
	}
	of := map[string]string{}
	for _, b := range lines(out) {
		if slices.Contains(trunk, b) {
			continue
		}
		// The bookmark's own branch: its ancestors trunk lacks, nearest
		// first, so a change below two bookmarks takes the closer one.
		ids, err := jjRead(ctx, repo, "log", "--no-graph", "-r", "(("+revs+") & ::bookmarks(exact:"+strconv.Quote(b)+")) ~ ::trunk()", "-T", `change_id ++ "\n"`)
		if err != nil {
			continue
		}
		for _, id := range lines(ids) {
			if _, ok := of[id]; !ok {
				of[id] = b
			}
		}
	}
	// A change past every bookmark, as work is until you move the bookmark
	// to push it: the branch it was started on.
	ids, err := jjRead(ctx, repo, "log", "--no-graph", "-r", revs, "-T", `change_id ++ "\n"`)
	if err != nil {
		return of
	}
	for _, id := range lines(ids) {
		if _, ok := of[id]; !ok {
			of[id] = nearestBookmark(ctx, repo, "change_id("+id+")", trunk)
		}
	}
	return of
}

// Change is one of the changes on a jj branch.
type Change struct {
	ID          string // its short change id
	Description string
}

// BranchChanges are the changes of the branch dir is on in jj: the mutable
// ones from trunk to @ with content, nearest first.
func BranchChanges(ctx context.Context, dir string) ([]Change, error) {
	out, err := jjRun(ctx, dir, "log", "--no-graph", "-r", "(trunk()..@) & mutable() & ~empty()",
		"-T", `json(change_id.short()) ++ "\t" ++ json(description) ++ "\n"`)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, l := range lines(out) {
		id, desc, _ := strings.Cut(l, "\t")
		var c Change
		if json.Unmarshal([]byte(id), &c.ID) != nil || json.Unmarshal([]byte(desc), &c.Description) != nil {
			continue
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// Describe sets a change's description.
func Describe(ctx context.Context, dir, change, msg string) error {
	_, err := jjRun(ctx, dir, "describe", "-r", "change_id("+change+")", "-m", msg)
	return err
}
