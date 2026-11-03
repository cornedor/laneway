package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/config"
	"github.com/cornedor/laneway/internal/store"
	"github.com/cornedor/laneway/internal/timesheet"
	"github.com/cornedor/laneway/internal/timesheet/httpsheet"
	"github.com/cornedor/laneway/internal/timesheet/sheettest"
)

// sheetServer is laneway web logging time to a fake timesheet server.
func sheetServer(t *testing.T) (*httptest.Server, *sheettest.Server) {
	t.Helper()
	sheet := sheettest.New()
	t.Cleanup(sheet.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(context.Background(), Options{Jira: config.JiraConfig{}, Store: st, Site: "s",
		Timesheet: httpsheet.New(sheet.URL, "")}))
	t.Cleanup(ts.Close)
	return ts, sheet
}

func TestWorklogsOnHTTPTimesheet(t *testing.T) {
	ts, sheet := sheetServer(t)
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	from, tomorrow := start.Format(time.DateOnly), time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	if c := workCall(t, "POST", ts.URL+"/api/worklog/ABC-1", `{"Seconds":1800,"Started":"`+start.Format(time.RFC3339)+`","Comment":"tests"}`, nil); c != 200 {
		t.Fatalf("add = %d", c)
	}
	if c := workCall(t, "POST", ts.URL+"/api/worklog/ABC-1", `{"Seconds":1800,"Comment":"x","Left":"2h"}`, nil); c != 400 {
		t.Fatalf("left: on a provider without estimates = %d, want 400", c)
	}
	var logs struct {
		Worklogs []timesheet.Entry
		CanMove  bool
		CanLeave bool
	}
	workCall(t, "GET", ts.URL+"/api/worklogs?from="+from+"&to="+tomorrow, "", &logs)
	if len(logs.Worklogs) != 1 || logs.Worklogs[0].Key != "ABC-1" || logs.Worklogs[0].Seconds != 1800 || !logs.CanMove || logs.CanLeave {
		t.Fatalf("worklogs = %+v", logs)
	}
	id := logs.Worklogs[0].ID
	if c := workCall(t, "POST", ts.URL+"/api/worklog/ABC-1/move", `{"IDs":["`+id+`"],"To":"ABC-2"}`, nil); c != 200 {
		t.Fatalf("move = %d", c)
	}
	if c := workCall(t, "DELETE", ts.URL+"/api/worklog/ABC-2", `{"IDs":["`+id+`"]}`, nil); c != 200 {
		t.Fatalf("delete = %d", c)
	}
	sheet.Lock()
	defer sheet.Unlock()
	if len(sheet.Entries) != 0 {
		t.Fatalf("left on the server: %+v", sheet.Entries)
	}
}

func TestSessionTimesheet(t *testing.T) {
	ts, _ := sheetServer(t)
	var s struct {
		Timesheet struct {
			Name                   string
			Remaining, ServerTimer bool
		}
	}
	workCall(t, "GET", ts.URL+"/api/session", "", &s)
	if s.Timesheet.Name != "fake" || s.Timesheet.Remaining || !s.Timesheet.ServerTimer {
		t.Fatalf("session timesheet = %+v", s.Timesheet)
	}
}

// TestServerTimer: on a timesheet that runs the timer, /timer is its
// running entry: started, moved and discarded there, mirrored for the TUI.
func TestServerTimer(t *testing.T) {
	ts, sheet := sheetServer(t)
	start := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	if c := workCall(t, "PUT", ts.URL+"/api/timer", `{"Key":"ABC-1","Start":"`+start.Format(time.RFC3339)+`"}`, nil); c != 200 {
		t.Fatalf("start = %d", c)
	}
	var got workTimer
	workCall(t, "GET", ts.URL+"/api/timer", "", &got)
	if got.Key != "ABC-1" || !got.Start.Equal(start) || got.ID == "" {
		t.Fatalf("timer = %+v", got)
	}
	// Started elsewhere: the server's switch shows.
	sheet.Lock()
	run := sheet.Running()
	end := start.Add(5 * time.Minute)
	run.End = &end
	sheet.Entries = append(sheet.Entries, &sheettest.Entry{ID: "99", Key: "ABC-2", Start: end})
	sheet.Unlock()
	workCall(t, "GET", ts.URL+"/api/timer", "", &got)
	if got.Key != "ABC-2" || got.ID != "99" {
		t.Fatalf("after a switch elsewhere: %+v", got)
	}
	workCall(t, "PUT", ts.URL+"/api/timer", `{"Key":"ABC-3","Start":"`+end.Format(time.RFC3339)+`"}`, nil)
	sheet.Lock()
	if r := sheet.Running(); r == nil || r.ID != "99" || r.Key != "ABC-3" {
		t.Fatalf("moved = %+v", r)
	}
	sheet.Unlock()
	// A clear after logging leaves the entry; a discard deletes it.
	workCall(t, "PUT", ts.URL+"/api/timer", `{"Key":""}`, nil)
	workCall(t, "PUT", ts.URL+"/api/timer", `{"Key":"","Drop":"ABC-1"}`, nil)
	sheet.Lock()
	if sheet.Running() == nil {
		t.Fatal("a clear or another issue's discard deleted the running entry")
	}
	sheet.Unlock()
	workCall(t, "PUT", ts.URL+"/api/timer", `{"Key":"","Drop":"ABC-3"}`, nil)
	sheet.Lock()
	defer sheet.Unlock()
	if sheet.Running() != nil || len(sheet.Entries) != 1 {
		t.Fatalf("after discard: %+v", sheet.Entries)
	}
}
