package demo

import (
	"context"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// demoClient is the app's Jira client on a fresh demo.
func demoClient(t *testing.T) (*jira.Client, *Server) {
	t.Helper()
	s := New(time.Now())
	url, stop, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return jira.New(jira.Config{BaseURL: url, Email: "demo@example.com", APIToken: "demo", Projects: []string{project}}), s
}

// TestDemoBoard: the board, its sprints and a sprint's cards read as from
// Jira, with nothing unanswered.
func TestDemoBoard(t *testing.T) {
	c, s := demoClient(t)
	ctx := context.Background()
	boards, err := c.Boards(ctx, project)
	if err != nil || len(boards) != 1 {
		t.Fatalf("boards = %v, %v", boards, err)
	}
	cfg, err := c.BoardConfiguration(ctx, boards[0].ID)
	if err != nil || len(cfg.Columns) != 4 {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	sprints, err := c.Sprints(ctx, boards[0].ID)
	if err != nil || len(sprints) != 2 || sprints[0].State != "active" {
		t.Fatalf("sprints = %+v, %v", sprints, err)
	}
	cards, _, err := c.SprintIssues(ctx, boards[0].ID, sprints[0].ID, "", pointsField)
	if err != nil || len(cards) != 10 {
		t.Fatalf("sprint cards = %d, %v", len(cards), err)
	}
	mine, _, err := c.SprintIssues(ctx, boards[0].ID, sprints[0].ID, "assignee = currentUser()", pointsField)
	if err != nil || len(mine) != 3 {
		t.Errorf("my sprint cards = %d, %v", len(mine), err)
	}
	if len(s.Unhandled) > 0 {
		t.Errorf("unanswered: %v", s.Unhandled)
	}
}

// TestDemoWrites: a move, a comment and a worklog stay, in memory.
func TestDemoWrites(t *testing.T) {
	c, s := demoClient(t)
	ctx := context.Background()
	trs, err := c.Transitions(ctx, "DEMO-5")
	if err != nil || len(trs) == 0 {
		t.Fatalf("transitions = %v, %v", trs, err)
	}
	var toDone string
	for _, tr := range trs {
		if tr.Name == "Done" {
			toDone = tr.ID
		}
	}
	if err := c.TransitionWith(ctx, "DEMO-5", toDone, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.AddComment(ctx, "DEMO-5", "shipped", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.AddWorklog(ctx, "DEMO-5", 1800, time.Now(), "", ""); err != nil {
		t.Fatal(err)
	}
	iss, err := c.Get(ctx, "DEMO-5")
	if err != nil {
		t.Fatal(err)
	}
	if iss.Status != "Done" || len(iss.Comments) != 1 || iss.Comments[0].Body != "shipped" {
		t.Errorf("DEMO-5 = %s, %+v", iss.Status, iss.Comments)
	}
	if wl, err := c.IssueWorklogs(ctx, "DEMO-5"); err != nil || len(wl) != 1 {
		t.Errorf("worklogs = %v, %v", wl, err)
	}
	key, err := c.CreateIssue(ctx, jira.NewIssue{Project: project, Type: "Task", Summary: "Try the demo"})
	if err != nil || s.issues[key] == nil || s.issues[key].summary != "Try the demo" {
		t.Errorf("created %q, %v", key, err)
	}
}

func TestDemoJQL(t *testing.T) {
	s := New(time.Now())
	for jql, want := range map[string]int{
		"issuetype = Epic":   3,
		"parent in (DEMO-4)": 3,
		`text ~ "invoice*"`:  4,
		"sprint = 8":         len(s.search("sprint = 8")),
		"statusCategory != Done AND labels = backend":           4,
		`key in (DEMO-1, DEMO-2) OR (assignee = currentUser())`: 7,
	} {
		if got := len(s.search(jql)); got != want || got == 0 {
			t.Errorf("%s: %d issues, want %d", jql, got, want)
		}
	}
}

// TestDemoReleases: V reads the versions with their progress, a release's
// issues by fixVersion, and releasing one sticks.
func TestDemoReleases(t *testing.T) {
	c, s := demoClient(t)
	ctx := context.Background()
	vs, err := c.Versions(ctx, project)
	if err != nil || len(vs) != 3 || vs[0].Name != "2.5" {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	cur := vs[1]
	if cur.Name != "2.4" || cur.Released || cur.Total != 5 || cur.Done != 0 {
		t.Errorf("2.4 = %+v", cur)
	}
	if got := s.search("fixVersion = " + cur.ID); len(got) != 5 {
		t.Errorf("fixVersion = %s: %d issues, want 5", cur.ID, len(got))
	}
	if err := c.ReleaseVersion(ctx, cur.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if vs, _ = c.Versions(ctx, project); !vs[1].Released {
		t.Errorf("2.4 after releasing: %+v", vs[1])
	}
	if len(s.Unhandled) > 0 {
		t.Errorf("unanswered: %v", s.Unhandled)
	}
}
