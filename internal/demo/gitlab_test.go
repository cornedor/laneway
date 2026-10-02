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
	if ds, _ := c.Drafts(ctx, ref.Repo, ref.Number); len(ds) != 1 {
		t.Fatalf("Drafts = %+v", ds)
	}
	if err := c.SubmitReview(ctx, ref.Repo, ref.Number, "", forge.VerdictApprove); err != nil {
		t.Fatal(err)
	}
	threads, _ = c.Threads(ctx, ref.Repo, ref.Number)
	ds, _ := c.Drafts(ctx, ref.Repo, ref.Number)
	mr, _ = c.Get(ctx, ref.Repo, ref.Number)
	if len(threads) != 2 || len(threads[0].Notes) != 2 || len(ds) != 0 || !mr.Approvals.Approved {
		t.Errorf("after the review: threads %+v, drafts %d, approvals %+v", threads, len(ds), mr.Approvals)
	}
}
