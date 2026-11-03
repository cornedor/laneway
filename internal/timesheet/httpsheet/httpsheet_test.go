package httpsheet

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/timesheet"
	"github.com/cornedor/laneway/internal/timesheet/sheettest"
)

func TestRoundTrip(t *testing.T) {
	srv := sheettest.New()
	defer srv.Close()
	ctx := context.Background()
	s := New(srv.URL+"/", "")
	if s.Caps() != (timesheet.Caps{}) {
		t.Fatalf("caps before a call = %+v, want zero", s.Caps())
	}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day.Add(9 * time.Hour)
	if err := s.Add(ctx, timesheet.New{Key: "ABC-1", Seconds: 1800, Started: at, Comment: "review"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, timesheet.New{Key: "", Seconds: 600, Started: at.Add(time.Hour), Comment: "standup"}); err != nil {
		t.Fatal(err)
	}
	got, err := timesheet.Day(ctx, s, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "ABC-1" || got[0].Seconds != 1800 || got[0].Comment != "review" || !got[0].Started.Equal(at) || got[1].Key != "" {
		t.Fatalf("entries = %+v", got)
	}
	if want := (timesheet.Caps{ServerTimer: true, Move: true, Keyless: true}); s.Caps() != want || s.Name() != "fake" {
		t.Fatalf("after a call: caps %+v name %q", s.Caps(), s.Name())
	}

	comment := "review, done"
	if err := s.Update(ctx, "ABC-1", got[0].ID, 3600, time.Time{}, &comment); err != nil {
		t.Fatal(err)
	}
	if err := s.Move(ctx, "ABC-1", []string{got[0].ID}, "ABC-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "", []string{got[1].ID}); err != nil {
		t.Fatal(err)
	}
	got, _ = timesheet.Day(ctx, s, day)
	if len(got) != 1 || got[0].Key != "ABC-2" || got[0].Seconds != 3600 || got[0].Comment != comment {
		t.Fatalf("after update, move and delete: %+v", got)
	}
}

func TestTimer(t *testing.T) {
	srv := sheettest.New()
	defer srv.Close()
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	srv.Now = func() time.Time { return now }
	ctx := context.Background()
	s := New(srv.URL, "")
	if run, err := s.Running(ctx); err != nil || run != nil {
		t.Fatalf("no timer: %+v %v", run, err)
	}
	if err := s.Start(ctx, "ABC-1", "", now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	run, err := s.Running(ctx)
	if err != nil || run == nil || run.Key != "ABC-1" || !run.Running || run.Seconds != 1200 {
		t.Fatalf("running = %+v %v", run, err)
	}
	// A second start ends the first where it begins.
	if err := s.Start(ctx, "ABC-2", "", now); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Entries(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if len(all) != 2 || all[0].Running || all[0].Seconds != 1200 || !all[1].Running {
		t.Fatalf("after a switch: %+v", all)
	}
	// Stopping is an update of the running entry.
	if err := s.Update(ctx, "ABC-2", all[1].ID, 900, time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	if run, _ := s.Running(ctx); run != nil {
		t.Fatalf("still running: %+v", run)
	}
}

func TestNoServerTimer(t *testing.T) {
	srv := sheettest.New()
	defer srv.Close()
	srv.ServerTimer, srv.Move = false, false
	ctx := context.Background()
	s := New(srv.URL, "")
	if run, err := s.Running(ctx); err != nil || run != nil {
		t.Fatalf("running without the cap = %+v %v", run, err)
	}
	if err := s.Start(ctx, "ABC-1", "", time.Now()); !errors.Is(err, timesheet.ErrUnsupported) {
		t.Fatalf("start = %v, want ErrUnsupported", err)
	}
	if err := s.Move(ctx, "ABC-1", []string{"1"}, "ABC-2"); !errors.Is(err, timesheet.ErrUnsupported) {
		t.Fatalf("move = %v, want ErrUnsupported", err)
	}
}

func TestErrorCarriesHint(t *testing.T) {
	srv := sheettest.New()
	defer srv.Close()
	srv.Login = "statz wants its 2FA code"
	_, err := New(srv.URL, "").Entries(context.Background(), time.Now(), time.Now().Add(time.Hour))
	var e *Error
	if !errors.As(err, &e) || e.Status != 401 || !strings.Contains(err.Error(), "sign in first — statz wants its 2FA code") {
		t.Fatalf("err = %v", err)
	}
}

func TestBearerToken(t *testing.T) {
	srv := sheettest.New()
	defer srv.Close()
	var got string
	inner := srv.Config.Handler
	srv.Config.Handler = httpHandlerFunc(func(auth string) { got = auth }, inner)
	if _, err := New(srv.URL, "s3cret").Running(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer s3cret" {
		t.Fatalf("Authorization = %q", got)
	}
}

// httpHandlerFunc hands see each request's Authorization, then serves it.
func httpHandlerFunc(see func(string), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		see(r.Header.Get("Authorization"))
		next.ServeHTTP(w, r)
	})
}
