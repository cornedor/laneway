package demo

import (
	"context"
	"testing"
)

// TestDemoDevInfo: a few issues have pull requests, builds, deployments,
// branches and commits; their cards carry the PR state and environment.
func TestDemoDevInfo(t *testing.T) {
	c, s := demoClient(t)
	ctx := context.Background()
	got, err := c.DevInfo(ctx, "DEMO-11")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, d := range got {
		kinds[d.Kind]++
	}
	if kinds["pr"] != 1 || kinds["branch"] != 1 || kinds["commit"] != 5 || kinds["build"] != 1 || kinds["deploy"] != 2 {
		t.Errorf("kinds = %v", kinds)
	}
	if pr := got[0]; pr.Status != "MERGED" || pr.Author == "" || len(pr.Reviewers) != 2 || pr.Updated.IsZero() {
		t.Errorf("pr = %+v", pr)
	}
	if none, err := c.DevInfo(ctx, "DEMO-3"); err != nil || len(none) != 0 {
		t.Errorf("DEMO-3 = %+v, %v", none, err)
	}
	cards, err := c.SearchCards(ctx, "key in (DEMO-11, DEMO-7, DEMO-3)")
	if err != nil || len(cards) != 3 {
		t.Fatalf("cards = %d, %v", len(cards), err)
	}
	for _, cd := range cards {
		want := map[string][2]string{"DEMO-11": {"MERGED", "production"}, "DEMO-7": {"OPEN", "staging"}, "DEMO-3": {"", ""}}[cd.Key]
		if cd.PR != want[0] || cd.Deploy != want[1] {
			t.Errorf("%s: PR %q deploy %q, want %v", cd.Key, cd.PR, cd.Deploy, want)
		}
	}
	if len(s.Unhandled) > 0 {
		t.Errorf("unanswered: %v", s.Unhandled)
	}
}
