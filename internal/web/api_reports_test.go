package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func reportsPost(t *testing.T, url string, body any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestReportsAndPlanning(t *testing.T) {
	ts := searchDemo(t)
	var boards []struct{ ID int }
	if searchGet(t, ts.URL+"/api/projects/DEMO/boards", &boards) != 200 || len(boards) == 0 {
		t.Fatal("no boards")
	}
	b := boards[0].ID
	at := func(p string) string { return ts.URL + "/api" + p }

	var plan struct {
		Sprints []struct {
			ID    int
			Cards []struct{ Key string }
		}
		Backlog struct{ Cards []struct{ Key string } }
	}
	if c := searchGet(t, at("/plan/"+itoa(b)), &plan); c != 200 || len(plan.Sprints) == 0 || len(plan.Backlog.Cards) == 0 {
		t.Fatalf("plan: %d %+v", c, plan)
	}

	var sp struct {
		Sprint  struct{ ID int }
		Issues  []struct{ Key string }
		Columns []struct{ Name string }
	}
	if c := searchGet(t, at("/reports/sprint/"+itoa(b)), &sp); c != 200 || len(sp.Issues) == 0 || len(sp.Columns) == 0 {
		t.Fatalf("sprint report: %d %+v", c, sp)
	}
	var vel []struct{ Name string }
	if c := searchGet(t, at("/reports/velocity/"+itoa(b)+"?n=3"), &vel); c != 200 || len(vel) == 0 {
		t.Fatalf("velocity: %d %v", c, vel)
	}
	var retro struct {
		Sprints []struct{ Name string }
		Cards   map[string]struct{ Summary string }
	}
	if c := searchGet(t, at("/reports/retro/"+itoa(b)), &retro); c != 200 || len(retro.Sprints) == 0 {
		t.Fatalf("retro: %d %+v", c, retro)
	}
	var cycle []struct{ Key string }
	if c := searchGet(t, at("/reports/cycle/DEMO?weeks=26"), &cycle); c != 200 || len(cycle) == 0 {
		t.Fatalf("cycle: %d %v", c, cycle)
	}
	var vers []struct{ Name string }
	if c := searchGet(t, at("/reports/versions/DEMO"), &vers); c != 200 || len(vers) == 0 {
		t.Fatalf("versions: %d %v", c, vers)
	}
	var rm struct{ Epics []struct{ Key string } }
	if c := searchGet(t, at("/roadmap/DEMO"), &rm); c != 200 || len(rm.Epics) == 0 {
		t.Fatalf("roadmap: %d %+v", c, rm)
	}

	key := plan.Backlog.Cards[0].Key
	if c := reportsPost(t, at("/plan/move"), map[string]any{"Keys": []string{key}, "Sprint": plan.Sprints[0].ID}); c != 200 {
		t.Fatalf("move: %d", c)
	}
	if c := reportsPost(t, at("/plan/move"), map[string]any{"Keys": []string{}}); c != 400 {
		t.Fatalf("empty move: %d", c)
	}
	if c := reportsPost(t, at("/plan/sprints"), map[string]any{"Board": b, "Name": "DEMO Sprint X"}); c != 200 {
		t.Fatalf("create: %d", c)
	}
	if c := reportsPost(t, at("/plan/sprints"), map[string]any{"Board": b}); c != 400 {
		t.Fatalf("nameless create: %d", c)
	}
	sid := itoa(plan.Sprints[len(plan.Sprints)-1].ID)
	if c := reportsPost(t, at("/plan/sprints/"+sid+"/start"), map[string]any{"End": "2001-01-01"}); c != 400 {
		t.Fatalf("past end: %d", c)
	}
	if c := reportsPost(t, at("/plan/sprints/"+sid+"/update"), map[string]any{"Name": "Renamed", "Goal": "ship"}); c != 200 {
		t.Fatalf("update: %d", c)
	}
	if c := reportsPost(t, at("/plan/sprints/"+sid+"/close"), map[string]any{"Board": b}); c != 200 {
		t.Fatalf("close: %d", c)
	}
	if c := reportsPost(t, at("/roadmap/"+rm.Epics[0].Key+"/dates"), map[string]any{"Start": "2030-01-01", "End": "2030-02-01"}); c != 200 {
		t.Fatalf("dates: %d", c)
	}
	var after struct {
		Epics []struct{ Key, Start, End string }
	}
	if c := searchGet(t, at("/roadmap/DEMO?fresh=1"), &after); c != 200 || !strings.HasPrefix(after.Epics[0].Start, "2030-01-01") || !strings.HasPrefix(after.Epics[0].End, "2030-02-01") {
		t.Errorf("dates after: %d %+v", c, after.Epics[0])
	}
	if c := reportsPost(t, at("/roadmap/"+rm.Epics[0].Key+"/dates"), map[string]any{"End": "soon"}); c != 400 {
		t.Fatalf("bad date: %d", c)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
