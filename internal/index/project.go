package index

import (
	"encoding/json"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// A project mirrored whole: every issue of it in the index, kept up to date
// by what changed since (the all issues view, internal/ui/allissues.go).

// ProjectSync is where a project's mirror stands.
type ProjectSync struct {
	// At is when the last sync started: what changed after it is not in yet.
	At time.Time
	// KeysAt is when the issues gone from the project were last dropped.
	KeysAt time.Time
	// Complete is whether every issue of the project was read once; until
	// then the index holds only the ones read so far.
	Complete bool
}

// ProjectSync is project's mirror state, zero when it was never mirrored.
func (ix *Index) ProjectSync(project string) ProjectSync {
	var s ProjectSync
	if ix == nil {
		return s
	}
	var at, keysAt int64
	var complete bool
	if err := ix.db.QueryRow("SELECT at, keys_at, complete FROM project_syncs WHERE project = ?", project).Scan(&at, &keysAt, &complete); err != nil {
		return s
	}
	return ProjectSync{At: time.UnixMilli(at), KeysAt: time.UnixMilli(keysAt), Complete: complete}
}

// SetProjectSync records where project's mirror stands.
func (ix *Index) SetProjectSync(project string, s ProjectSync) error {
	if ix == nil {
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	_, err := ix.db.Exec("INSERT OR REPLACE INTO project_syncs (project, at, keys_at, complete) VALUES (?, ?, ?, ?)",
		project, s.At.UnixMilli(), s.KeysAt.UnixMilli(), s.Complete)
	return err
}

// ProjectCards is every indexed card of project, most recently updated
// first.
func (ix *Index) ProjectCards(project string) ([]jira.Card, error) {
	if ix == nil {
		return nil, nil
	}
	rows, err := ix.db.Query("SELECT card FROM issues WHERE project = ? ORDER BY updated DESC", project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []jira.Card
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c jira.Card
		if json.Unmarshal([]byte(raw), &c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// DropUnread drops project's issues last read before since: after a sync
// that read every issue of the project from since on, the ones it didn't
// read are gone from it (deleted, or moved to another project).
func (ix *Index) DropUnread(project string, since time.Time) (int, error) {
	if ix == nil {
		return 0, nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	res, err := ix.db.Exec("DELETE FROM issues WHERE project = ? AND synced < ?", project, since.UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// KeepKeys drops project's issues whose key is not in keys: the project's
// every key, as Jira has them now.
func (ix *Index) KeepKeys(project string, keys []string) (int, error) {
	if ix == nil {
		return 0, nil
	}
	keep := make(map[string]bool, len(keys))
	for _, k := range keys {
		keep[k] = true
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	rows, err := ix.db.Query("SELECT key FROM issues WHERE project = ?", project)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[k] {
			gone = append(gone, k)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(gone) == 0 {
		return 0, err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, k := range gone {
		if _, err := tx.Exec("DELETE FROM issues WHERE key = ?", k); err != nil {
			return 0, err
		}
	}
	return len(gone), tx.Commit()
}
