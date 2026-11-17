// Package mirror keeps every issue of a project in the local index, so a
// view of all of them sorts and filters locally, offline too, over the whole
// project rather than the first page Jira answers.
//
// The first time, Run reads the project page by page, newest first. After
// that Refresh reads only what changed since the last sync, and now and then
// the project's keys, to drop the issues deleted or moved out of it.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cornedor/laneway/internal/i18n"
	"github.com/cornedor/laneway/internal/index"
	"github.com/cornedor/laneway/internal/jira"
)

// overlap reads a little before the last sync, for clock skew and writes in
// flight while it ran.
const overlap = 2 * time.Minute

// keysEvery is how often Refresh reads the project's keys to drop the issues
// gone from it: issues are seldom deleted, and a key pass reads them all.
const keysEvery = time.Hour

// errNoIndex is a mirror asked for without an index (it failed to open).
func errNoIndex() error { return errors.New(i18n.T("the issue index is not available")) }

// projectJQL is project's issues, quoted as JQL.
func projectJQL(project string) string { return fmt.Sprintf("project = %q", project) }

// Run is a whole read of a project into the index, a page at a time, so the
// caller can show the issues as they arrive.
type Run struct {
	Project string
	// Started is when the run began: issues changed after it are left to the
	// next Refresh, and those not read by the end are gone from the project.
	Started time.Time
	// Done is how many issues were read, Total how many Jira counted.
	Done, Total int
	token       string
	finished    bool
}

// Start begins a run of project, counting its issues.
func Start(ctx context.Context, c *jira.Client, ix *index.Index, project string) (*Run, error) {
	if ix == nil {
		return nil, errNoIndex()
	}
	r := &Run{Project: project, Started: time.Now()}
	n, err := c.Count(ctx, projectJQL(project))
	if err != nil {
		return nil, err
	}
	r.Total = n
	return r, nil
}

// Finished is whether the run read the last page.
func (r *Run) Finished() bool { return r.finished }

// Next reads the next page into the index. After the last it drops the
// project's issues the run didn't read and marks the mirror complete.
// Newest created first, an order changes don't move issues in.
func (r *Run) Next(ctx context.Context, c *jira.Client, ix *index.Index) error {
	if r.finished {
		return nil
	}
	cards, next, err := c.SearchCardsPage(ctx, projectJQL(r.Project)+" ORDER BY created DESC, key DESC", r.token)
	if err != nil {
		return err
	}
	ix.PutCards(cards)
	r.Done += len(cards)
	r.Total = max(r.Total, r.Done)
	if r.token = next; next != "" {
		return nil
	}
	r.finished = true
	if _, err := ix.DropUnread(r.Project, r.Started); err != nil {
		return err
	}
	return ix.SetProjectSync(r.Project, index.ProjectSync{At: r.Started, KeysAt: r.Started, Complete: true})
}

// Refresh brings a complete mirror of project up to date: the issues updated
// since its last sync, and past keysEvery the keys, dropping the gone ones.
// A mirror never completed is left alone: that is Run's.
func Refresh(ctx context.Context, c *jira.Client, ix *index.Index, project string) error {
	if ix == nil {
		return errNoIndex()
	}
	s := ix.ProjectSync(project)
	if !s.Complete {
		return nil
	}
	started := time.Now()
	mins := int(started.Sub(s.At.Add(-overlap)).Minutes()) + 1
	jql := fmt.Sprintf("%s AND updated >= -%dm ORDER BY updated DESC", projectJQL(project), mins)
	for token := ""; ; {
		cards, next, err := c.SearchCardsPage(ctx, jql, token)
		if err != nil {
			return err
		}
		ix.PutCards(cards)
		if token = next; token == "" {
			break
		}
	}
	s.At = started
	if started.Sub(s.KeysAt) >= keysEvery {
		keys, err := c.Keys(ctx, projectJQL(project))
		if err != nil {
			return err
		}
		if _, err := ix.KeepKeys(project, keys); err != nil {
			return err
		}
		s.KeysAt = started
	}
	return ix.SetProjectSync(project, s)
}

// Cards is project's mirrored issues, most recently updated first, narrowed
// by filter (JQL, "" for none): Jira says which keys it finds, so filter
// means what it means on the board. Offline the index answers the parts of
// filter it can; note then says what it left out.
func Cards(ctx context.Context, c *jira.Client, ix *index.Index, project, filter, me string) (cards []jira.Card, note string, err error) {
	if ix == nil {
		return nil, "", errNoIndex()
	}
	all, err := ix.ProjectCards(project)
	if err != nil || filter == "" {
		return all, "", err
	}
	q := projectJQL(project) + " AND (" + filter + ")"
	keys, err := c.Keys(ctx, q)
	if err != nil {
		if !jira.Offline(err) {
			return nil, "", err
		}
		hits, dropped, ierr := ix.JQL(q, me, len(all)+1)
		if ierr != nil {
			return nil, "", ierr
		}
		keys = make([]string, len(hits))
		for i, h := range hits {
			keys[i] = h.Card.Key
		}
		if len(dropped) > 0 {
			note = i18n.Tf("offline: left out %s", strings.Join(dropped, ", "))
		}
	}
	in := make(map[string]bool, len(keys))
	for _, k := range keys {
		in[k] = true
	}
	return slices.DeleteFunc(all, func(c jira.Card) bool { return !in[c.Key] }), note, nil
}
