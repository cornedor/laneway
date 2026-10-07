// Package index mirrors the issues laneway has read, per site, in a SQLite
// file under the user's cache directory: every board, search and panel read
// writes its issues, so they outlive the session and can be read without
// Jira.
package index

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/cornedor/laneway/internal/jira"
)

// schema is the index's layout; version bumps drop and rebuild it, since
// everything in it comes back from Jira.
const (
	schema = `
CREATE TABLE IF NOT EXISTS issues (
	key      TEXT PRIMARY KEY,
	project  TEXT NOT NULL,
	summary  TEXT NOT NULL,
	status   TEXT NOT NULL,
	assignee TEXT NOT NULL,
	done     INTEGER NOT NULL,
	updated  INTEGER NOT NULL,
	synced   INTEGER NOT NULL,
	card     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS issues_project ON issues(project, updated);
CREATE TABLE IF NOT EXISTS users (
	project    TEXT NOT NULL,
	account_id TEXT NOT NULL,
	name       TEXT NOT NULL,
	assignable INTEGER NOT NULL,
	seen       INTEGER NOT NULL,
	PRIMARY KEY (project, account_id)
);
CREATE TABLE IF NOT EXISTS user_syncs (
	project TEXT PRIMARY KEY,
	at      INTEGER NOT NULL
);
`
	version = 2

	peopleKept = 90 * 24 * time.Hour
)

// Index is one site's mirror. Safe for concurrent use; a nil *Index does
// nothing.
type Index struct {
	db *sql.DB
	mu sync.Mutex // one write at a time: SQLite has one writer
}

// Path is where site's index lives: ~/.cache/laneway/index-<site>.db, with
// jira: as "jira".
func Path(site string) (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if site == "" {
		site = "jira"
	}
	return filepath.Join(d, "laneway", "index-"+site+".db"), nil
}

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// WAL lets a second laneway on the same site read while one writes.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	ix := &Index{db: db}
	if err := ix.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return ix, nil
}

func (ix *Index) migrate() error {
	var v int
	if err := ix.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v != version {
		for _, t := range []string{"issues", "users", "user_syncs"} {
			if _, err := ix.db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
				return err
			}
		}
	}
	if _, err := ix.db.Exec(schema); err != nil {
		return err
	}
	_, err := ix.db.Exec("PRAGMA user_version = " + strconv.Itoa(version))
	return err
}

// Close closes the file.
func (ix *Index) Close() error {
	if ix == nil {
		return nil
	}
	return ix.db.Close()
}

// PutCards stores cards as read now, replacing what the index had for them.
func (ix *Index) PutCards(cards []jira.Card) {
	if ix == nil || len(cards) == 0 {
		return
	}
	_ = ix.put(cards, time.Now())
}

// PutIssue stores a panel read. An issue carries fewer fields than a card
// (no parent, sprint or PR), so it updates the ones it has on a card
// already indexed.
func (ix *Index) PutIssue(iss *jira.Issue) {
	if ix == nil || iss == nil {
		return
	}
	c, _ := ix.Get(iss.Key)
	c.Key, c.Summary, c.Type, c.Status = iss.Key, iss.Summary, iss.Type, iss.Status
	c.TypeKind, c.TypeAvatar = iss.TypeKind, iss.TypeAvatar
	c.Priority, c.Assignee, c.AssigneeID, c.Reporter = iss.Priority, iss.Assignee, iss.AssigneeAccountID, iss.Reporter
	c.Points, c.Labels, c.Updated = iss.StoryPoints, strings.Join(iss.Labels, " "), iss.Updated
	c.Done = iss.StatusCategory == "done"
	c.InProgress = iss.StatusCategory == "indeterminate"
	_ = ix.put([]jira.Card{c}, time.Now())
	project, _, _ := strings.Cut(iss.Key, "-")
	seen := []jira.User{{AccountID: iss.ReporterAccountID, DisplayName: iss.Reporter}}
	for _, cm := range iss.Comments {
		seen = append(seen, jira.User{AccountID: cm.AuthorID, DisplayName: cm.Author})
	}
	ix.PutUsers(project, seen, false)
}

func (ix *Index) put(cards []jira.Card, at time.Time) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`INSERT OR REPLACE INTO issues (key, project, summary, status, assignee, done, updated, synced, card)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, c := range cards {
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		project, _, _ := strings.Cut(c.Key, "-")
		if _, err := st.Exec(c.Key, project, c.Summary, c.Status, c.Assignee, c.Done, unix(c.Updated), at.UnixMilli(), string(raw)); err != nil {
			return err
		}
		if err := putUser(tx, project, jira.User{AccountID: c.AssigneeID, DisplayName: c.Assignee}, false, at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// Get is key's card as last read; ok is false when the index lacks it.
func (ix *Index) Get(key string) (c jira.Card, ok bool) {
	if ix == nil {
		return c, false
	}
	var raw string
	if err := ix.db.QueryRow("SELECT card FROM issues WHERE key = ?", key).Scan(&raw); err != nil {
		return c, false
	}
	return c, json.Unmarshal([]byte(raw), &c) == nil
}

// Synced is when key was last read from Jira, zero when never.
func (ix *Index) Synced(key string) time.Time {
	if ix == nil {
		return time.Time{}
	}
	var ms int64
	if err := ix.db.QueryRow("SELECT synced FROM issues WHERE key = ?", key).Scan(&ms); err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Hit is an indexed card and when it was last read from Jira.
type Hit struct {
	Card   jira.Card
	Synced time.Time
}

// Search finds up to n issues in every indexed project whose key or summary
// holds each word of text, most recently updated first.
func (ix *Index) Search(text string, n int) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(text))
	if ix == nil || len(words) == 0 {
		return nil, nil
	}
	q := "SELECT card, synced FROM issues WHERE 1"
	var args []any
	for _, w := range words {
		q += ` AND (lower(key) LIKE ? ESCAPE '\' OR lower(summary) LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(w) + "%"
		args = append(args, like, like)
	}
	q += " ORDER BY updated DESC LIMIT ?"
	rows, err := ix.db.Query(q, append(args, n)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var raw string
		var ms int64
		if err := rows.Scan(&raw, &ms); err != nil {
			return nil, err
		}
		h := Hit{Synced: time.UnixMilli(ms)}
		if json.Unmarshal([]byte(raw), &h.Card) == nil {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

// Stats counts the indexed issues per project.
func (ix *Index) Stats() (map[string]int, error) {
	rows, err := ix.db.Query("SELECT project, count(*) FROM issues GROUP BY project")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, err
		}
		out[p] = n
	}
	return out, rows.Err()
}

// PutUsers keeps the people seen in project, their names as now; assignable
// marks them so (a search for assignees found them), else it is left as
// it was.
func (ix *Index) PutUsers(project string, us []jira.User, assignable bool) {
	if ix == nil || project == "" || len(us) == 0 {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	at := time.Now()
	for _, u := range us {
		if putUser(tx, project, u, assignable, at) != nil {
			return
		}
	}
	_ = tx.Commit()
}

func putUser(tx *sql.Tx, project string, u jira.User, assignable bool, at time.Time) error {
	if u.AccountID == "" || u.DisplayName == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO users (project, account_id, name, assignable, seen) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (project, account_id) DO UPDATE SET name = excluded.name, seen = excluded.seen,
		assignable = max(assignable, excluded.assignable)`, project, u.AccountID, u.DisplayName, assignable, at.UnixMilli())
	return err
}

// SyncAssignable replaces project's assignable people with us, the full
// list from Jira, and notes when; others not seen for peopleKept go.
func (ix *Index) SyncAssignable(project string, us []jira.User) {
	if ix == nil || project == "" {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tx, err := ix.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	at := time.Now()
	if _, err := tx.Exec("UPDATE users SET assignable = 0 WHERE project = ?", project); err != nil {
		return
	}
	for _, u := range us {
		if putUser(tx, project, u, true, at) != nil {
			return
		}
	}
	if _, err := tx.Exec("DELETE FROM users WHERE project = ? AND assignable = 0 AND seen < ?", project, at.Add(-peopleKept).UnixMilli()); err != nil {
		return
	}
	if _, err := tx.Exec("INSERT OR REPLACE INTO user_syncs (project, at) VALUES (?, ?)", project, at.UnixMilli()); err != nil {
		return
	}
	_ = tx.Commit()
}

// UsersSynced is when project's assignable people were last read in full,
// zero when never.
func (ix *Index) UsersSynced(project string) time.Time {
	if ix == nil {
		return time.Time{}
	}
	var ms int64
	if err := ix.db.QueryRow("SELECT at FROM user_syncs WHERE project = ?", project).Scan(&ms); err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Users are project's people matching query, assignable ones only when
// asked: each word of query starts a word of the name. Names starting with
// query come first, then by name.
func (ix *Index) Users(project, query string, assignable bool) []jira.User {
	if ix == nil {
		return nil
	}
	rows, err := ix.db.Query("SELECT account_id, name FROM users WHERE project = ? AND assignable >= ? ORDER BY name COLLATE NOCASE", project, assignable)
	if err != nil {
		return nil
	}
	defer rows.Close()
	words := strings.Fields(strings.ToLower(query))
	var head, rest []jira.User
	for rows.Next() {
		var u jira.User
		if rows.Scan(&u.AccountID, &u.DisplayName) != nil {
			return nil
		}
		name := strings.ToLower(u.DisplayName)
		switch {
		case len(words) == 0 || strings.HasPrefix(name, strings.Join(words, " ")):
			head = append(head, u)
		case wordStarts(strings.Fields(name), words):
			rest = append(rest, u)
		}
	}
	return append(head, rest...)
}

// wordStarts is whether each of words starts one of name's words.
func wordStarts(name, words []string) bool {
	for _, w := range words {
		if !slices.ContainsFunc(name, func(n string) bool { return strings.HasPrefix(n, w) }) {
			return false
		}
	}
	return true
}

// PeopleStats counts the people kept per project.
func (ix *Index) PeopleStats() (map[string]int, error) {
	rows, err := ix.db.Query("SELECT project, count(*) FROM users GROUP BY project")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, err
		}
		out[p] = n
	}
	return out, rows.Err()
}

// Clear deletes the index file at path; one that isn't there is cleared.
func Clear(path string) error {
	var errs []error
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
