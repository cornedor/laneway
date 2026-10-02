package ui

import (
	"context"
	"strconv"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
	"github.com/cornedor/laneway/internal/store"
)

// The merge requests waiting on you, across GitLab instances and projects,
// Jira key or not: review asked, assigned, and yours with comments since you
// last opened them. The TUI's merge requests screen and the web's share it.

// MR inbox groups, in order.
const (
	MRReview   = "Review requested"
	MRAssigned = "Assigned to you"
	MRYours    = "Yours, new comments"
)

// MRRow is one merge request on the screen.
type MRRow struct {
	Group string
	Host  string
	MR    *forge.Change
}

// mrSeenMeta keeps the comment count a merge request had when last opened,
// by its link.
const mrSeenMeta = "gitlab.seen."

// MRInbox reads every instance's waiting merge requests; errs are the
// instances that failed, by host.
func MRInbox(ctx context.Context, s *gitlab.Sites, st *store.Store) (rows []MRRow, errs []string) {
	if s == nil {
		return nil, nil
	}
	sites := s.Waiting(ctx)
	add := func(group string, pick func(gitlab.Waiting) []*forge.Change, keep func(*forge.Change) bool) {
		for _, w := range sites {
			for _, mr := range pick(w.Waiting) {
				if keep(mr) {
					rows = append(rows, MRRow{Group: group, Host: w.Host, MR: mr})
				}
			}
		}
	}
	all := func(*forge.Change) bool { return true }
	add(MRReview, func(w gitlab.Waiting) []*forge.Change { return w.Review }, all)
	add(MRAssigned, func(w gitlab.Waiting) []*forge.Change { return w.Assigned }, all)
	add(MRYours, func(w gitlab.Waiting) []*forge.Change { return w.Mine }, func(mr *forge.Change) bool { return mr.Notes > mrSeen(st, mr.WebURL) })
	for _, w := range sites {
		if w.Err != nil {
			errs = append(errs, w.Host+": "+w.Err.Error())
		}
	}
	return rows, errs
}

// mrSeen is the comment count link had when last opened, 0 for never.
func mrSeen(st *store.Store, link string) int {
	if st == nil {
		return 0
	}
	v, _, _ := st.GetMeta(mrSeenMeta + link)
	n, _ := strconv.Atoi(v)
	return n
}

// MRSeen notes that mr was opened with its comments read.
func MRSeen(st *store.Store, mr *forge.Change) {
	if st != nil && mr != nil && mr.WebURL != "" {
		_ = st.SetMeta(mrSeenMeta+mr.WebURL, strconv.Itoa(mr.Notes))
	}
}
