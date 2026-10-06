package demo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/forge"
	"github.com/cornedor/laneway/internal/forge/gitlab"
)

// TestGitLab: the demo's GitLab answers what the merge request views read,
// its shipping.go diff is the one between the two texts it serves, and a
// review's notes go pending, then out.
func TestGitLab(t *testing.T) {
	s := New(time.Now())
	base, stop, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c := gitlab.New(gitlab.Config{BaseURL: base, Token: "demo"})
	ctx := context.Background()
	link := base + "/acme/shop-api/-/merge_requests/87"
	ref, ok := c.Parse(link)
	if !ok {
		t.Fatal("the demo's link is not its GitLab's")
	}
	mr, err := c.Get(ctx, ref.Repo, ref.Number)
	if err != nil || mr.Title == "" || mr.Checks == nil || len(mr.Checks.Groups) != 3 || mr.Checks.Groups[0].Name != "test" || mr.Approvals == nil || mr.Approvals.Left != 1 {
		t.Fatalf("Get = %+v, %v", mr, err)
	}
	w, err := c.Waiting(ctx)
	if err != nil || len(w.Review) != 1 || w.Review[0].Number != 87 {
		t.Errorf("Waiting = %+v, %v", w, err)
	}
	d, err := c.Diff(ctx, ref.Repo, ref.Number)
	if err != nil || len(d.Files) != 2 {
		t.Fatalf("Diff = %+v, %v", d, err)
	}
	text, err := c.File(ctx, ref.Repo, "internal/shipping/shipping.go", d.Refs.HeadSHA)
	if err != nil {
		t.Fatal(err)
	}
	var oldSide, newSide []string
	for _, l := range forge.ExpandDiff(forge.ParseUnifiedDiff(d.Files[1].Diff), text) {
		if l.Kind != forge.DiffAdd {
			oldSide = append(oldSide, l.Text)
		}
		if l.Kind != forge.DiffDel {
			newSide = append(newSide, l.Text)
		}
	}
	if got := strings.Join(oldSide, "\n") + "\n"; got != shippingOld {
		t.Errorf("old side:\n%s", got)
	}
	if got := strings.Join(newSide, "\n") + "\n"; got != shippingNew {
		t.Errorf("new side:\n%s", got)
	}
	threads, _ := c.Threads(ctx, ref.Repo, ref.Number)
	if len(threads) != 2 || !threads[0].Inline() || threads[1].Inline() {
		t.Fatalf("Threads = %+v", threads)
	}
	if err := c.AddDraft(ctx, ref.Repo, ref.Number, forge.NewNote{Body: "why?", ReplyTo: threads[0].ID}); err != nil {
		t.Fatal(err)
	}
	// One on a line, edited: it stays on its line, as GitLab keeps a position sent back.
	if err := c.AddDraft(ctx, ref.Repo, ref.Number, forge.NewNote{Body: "nit", NewPath: "internal/shipping/shipping.go", OldPath: "internal/shipping/shipping.go", NewLine: 3, Refs: d.Refs}); err != nil {
		t.Fatal(err)
	}
	ds, _ := c.Drafts(ctx, ref.Repo, ref.Number)
	if len(ds) != 2 || ds[1].NewLine != 3 {
		t.Fatalf("Drafts = %+v", ds)
	}
	if err := c.EditDraft(ctx, ref.Repo, ref.Number, ds[1].ID, "nit: rename"); err != nil {
		t.Fatal(err)
	}
	if ds, _ = c.Drafts(ctx, ref.Repo, ref.Number); len(ds) != 2 || ds[1].Body != "nit: rename" || ds[1].Path != "internal/shipping/shipping.go" || ds[1].NewLine != 3 {
		t.Fatalf("after the edit: Drafts = %+v", ds)
	}
	if err := c.SubmitReview(ctx, ref.Repo, ref.Number, "", forge.VerdictApprove); err != nil {
		t.Fatal(err)
	}
	threads, _ = c.Threads(ctx, ref.Repo, ref.Number)
	ds, _ = c.Drafts(ctx, ref.Repo, ref.Number)
	mr, _ = c.Get(ctx, ref.Repo, ref.Number)
	if len(threads) != 3 || len(threads[0].Notes) != 2 || len(ds) != 0 || !mr.Approvals.Approved {
		t.Errorf("after the review: threads %+v, drafts %d, approvals %+v", threads, len(ds), mr.Approvals)
	}
}

// TestGitLabJobs: !87's pipeline deploys for deployRun from its first read,
// deploy:staging's log growing; every job has an id and a log.
func TestGitLabJobs(t *testing.T) {
	s := New(time.Now())
	base, stop, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c := gitlab.New(gitlab.Config{BaseURL: base, Token: "demo"})
	ctx := context.Background()
	mr, err := c.Get(ctx, "acme/shop-api", 87)
	if err != nil || mr.Checks.Status != forge.StatusRunning || mr.Mergeable {
		t.Fatalf("a fresh pipeline: %+v, %v", mr.Checks, err)
	}
	var deploy forge.Job
	for _, g := range mr.Checks.Groups {
		for _, j := range g.Jobs {
			if j.ID == 0 {
				t.Errorf("job %s has no id", j.Name)
			}
			if l, err := c.JobLog(ctx, "acme/shop-api", j.ID); err != nil || !strings.Contains(l.Log, "Running with gitlab-runner") {
				t.Errorf("%s's log: %v", j.Name, err)
			}
			if j.Name == "deploy:staging" {
				deploy = j
			}
		}
	}
	l, _ := c.JobLog(ctx, "acme/shop-api", deploy.ID)
	if deploy.Status != forge.StatusRunning || l.Done() || strings.Contains(l.Log, "Job succeeded") {
		t.Errorf("deploy:staging while it runs: %s, %+v", deploy.Status, l.Job)
	}
	s.mu.Lock()
	s.git.deployAt = time.Now().Add(-deployRun)
	s.mu.Unlock()
	if l, _ = c.JobLog(ctx, "acme/shop-api", deploy.ID); !l.Done() || !strings.Contains(l.Log, "smoke: 4/4 passed") {
		t.Errorf("deploy:staging done: %+v", l.Job)
	}
	c.Invalidate("acme/shop-api", 87)
	if mr, _ = c.Get(ctx, "acme/shop-api", 87); mr.Checks.Status != forge.StatusSuccess || !mr.Mergeable {
		t.Errorf("the pipeline after: %+v", mr.Checks)
	}
}

// TestGitLabEditMerge: an edit changes what the next read says, draft by
// its title; a merge waits for the pipeline, then the merge request is merged.
func TestGitLabEditMerge(t *testing.T) {
	s := New(time.Now())
	base, stop, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c := gitlab.New(gitlab.Config{BaseURL: base, Token: "demo"})
	ctx := context.Background()
	const repo = "acme/shop-api"
	ms, err := c.Members(ctx, repo)
	if err != nil || len(ms) != len(users) {
		t.Fatalf("Members = %+v, %v", ms, err)
	}
	ls, _ := c.Labels(ctx, repo)
	title, revs, labels := gitlab.DraftTitle("Rate cache", true), []int{ms[0].ID}, []string{ls[1]}
	if err := c.Update(ctx, repo, 87, gitlab.Edit{Title: &title, ReviewerIDs: &revs, Labels: &labels}); err != nil {
		t.Fatal(err)
	}
	mr, _ := c.Get(ctx, repo, 87)
	if !mr.Draft || mr.Title != "Draft: Rate cache" || len(mr.ReviewerIDs) != 1 || mr.ReviewerIDs[0] != ms[0].ID || strings.Join(mr.Labels, ",") != ls[1] {
		t.Fatalf("after the edit: %+v", mr)
	}
	if err := c.Merge(ctx, repo, 87, forge.MergeOptions{}); err == nil {
		t.Error("merged a draft")
	}
	title = gitlab.DraftTitle(title, false)
	_ = c.Update(ctx, repo, 87, gitlab.Edit{Title: &title})
	s.mu.Lock()
	s.git.deployAt = time.Now().Add(-deployRun)
	s.mu.Unlock()
	if err := c.Merge(ctx, repo, 87, forge.MergeOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
	if mr, _ = c.Get(ctx, repo, 87); mr.State != forge.StateMerged {
		t.Errorf("after the merge: %s", mr.State)
	}
}
