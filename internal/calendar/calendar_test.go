package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cornedor/laneway/internal/jira"
)

// A week of a calendar as apps export it: a daily standup (not on the 2nd,
// moved on the 1st), a review, an all-day event, a free slot, a cancelled
// meeting and an Outlook zone name.
const feed = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:standup
DTSTAMP:20260901T000000Z
DTSTART;TZID=Europe/Amsterdam:20260928T093000
DTEND;TZID=Europe/Amsterdam:20260928T094500
RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR
EXDATE;TZID=Europe/Amsterdam:20261002T093000,20261005T093000
SUMMARY:Standup
END:VEVENT
BEGIN:VEVENT
UID:standup
DTSTAMP:20260901T000000Z
RECURRENCE-ID;TZID=Europe/Amsterdam:20261001T093000
DTSTART;TZID=Europe/Amsterdam:20261001T110000
DTEND;TZID=Europe/Amsterdam:20261001T111500
SUMMARY:Standup (moved)
END:VEVENT
BEGIN:VEVENT
UID:review
DTSTAMP:20260901T000000Z
DTSTART:20261001T130000Z
DTEND:20261001T140000Z
SUMMARY:Sprint review
END:VEVENT
BEGIN:VEVENT
UID:allday
DTSTAMP:20260901T000000Z
DTSTART;VALUE=DATE:20261001
DTEND;VALUE=DATE:20261002
SUMMARY:Holiday
END:VEVENT
BEGIN:VEVENT
UID:free
DTSTAMP:20260901T000000Z
DTSTART:20261001T150000Z
DTEND:20261001T160000Z
TRANSP:TRANSPARENT
SUMMARY:Focus
END:VEVENT
BEGIN:VEVENT
UID:gone
DTSTAMP:20260901T000000Z
DTSTART:20261001T080000Z
DTEND:20261001T083000Z
STATUS:CANCELLED
SUMMARY:Cancelled
END:VEVENT
BEGIN:VEVENT
UID:outlook
DTSTAMP:20260901T000000Z
DTSTART;TZID=W. Europe Standard Time:20261001T160000
DTEND;TZID=W. Europe Standard Time:20261001T163000
SUMMARY:1:1
END:VEVENT
END:VCALENDAR
`

func day(t *testing.T, s string) (time.Time, time.Time) {
	t.Helper()
	d, err := time.ParseInLocation(time.DateOnly, s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return d, d.AddDate(0, 0, 1)
}

func TestParse(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Skip("no zoneinfo")
	}
	from, to := day(t, "2026-10-01")
	ms, err := parse(strings.NewReader(feed), from.Add(-24*time.Hour), to.Add(24*time.Hour)) // the 30th to the 2nd
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		if m.Summary == "1:1" { // Outlook's zone name: read as local time
			if m.Start.Hour() != 16 || m.Start.Location() != time.Local {
				t.Errorf("1:1 at %v", m.Start)
			}
			continue
		}
		got = append(got, m.Start.In(ams).Format("01-02 15:04")+" "+m.End.Sub(m.Start).String()+" "+m.Summary)
	}
	want := []string{
		"09-30 09:30 15m0s Standup",
		"10-01 11:00 15m0s Standup (moved)",
		"10-01 15:00 1h0m0s Sprint review",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("meetings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(feed)) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "cal.ics")
	if err := os.WriteFile(path, []byte(feed), 0o600); err != nil {
		t.Fatal(err)
	}
	from, to := day(t, "2026-10-01")
	vdir := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "standup.ics"), []byte(feed), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{path, srv.URL, filepath.Dir(vdir)} {
		if ms, err := Read(context.Background(), src, from, to); err != nil || len(ms) < 2 {
			t.Errorf("%s: %d meetings, %v", src, len(ms), err)
		}
	}
	if _, err := Read(context.Background(), "", from, to); err == nil {
		t.Error("no source should fail")
	}
}

func TestProposals(t *testing.T) {
	at := time.Date(2026, 10, 1, 11, 0, 0, 0, time.Local)
	ms := []Meeting{{Start: at, End: at.Add(15 * time.Minute), Summary: "Standup"}, {Start: at.Add(2 * time.Hour), End: at.Add(3 * time.Hour), Summary: "Review"}}
	logs := []jira.Worklog{{Key: "OPS-1", Started: at.Add(5 * time.Minute)}, {Key: "ABC-1", Started: at.Add(2 * time.Hour)}}
	ps := Proposals(ms, "OPS-1", logs)
	if len(ps) != 1 || ps[0].Key != "OPS-1" || ps[0].Seconds != 3600 || ps[0].Comment != "Review" || !ps[0].Start.Equal(at.Add(2*time.Hour)) {
		t.Errorf("proposals %+v", ps)
	}
}

// TestCapacity: two weeks of 80 work hours, 12 of them in meetings (an
// overlap counted once, a Saturday and the time outside the sprint not at
// all), take 15% off 10 points.
func TestCapacity(t *testing.T) {
	mon := time.Date(2026, 9, 28, 9, 0, 0, 0, time.Local) // a Monday
	from, to := mon, mon.AddDate(0, 0, 14)
	var ms []Meeting
	for d := range 10 { // a 1h meeting each workday: 10h
		day := mon.AddDate(0, 0, d+d/5*2)
		ms = append(ms, Meeting{Start: day, End: day.Add(time.Hour)})
	}
	ms = append(ms,
		Meeting{Start: mon.Add(30 * time.Minute), End: mon.Add(2 * time.Hour)},             // 1h more past the first one's end
		Meeting{Start: mon.Add(26 * time.Hour), End: mon.Add(27 * time.Hour)},              // Tuesday 11:00: 1h
		Meeting{Start: mon.AddDate(0, 0, 5), End: mon.AddDate(0, 0, 5).Add(3 * time.Hour)}, // Saturday
		Meeting{Start: mon.Add(-2 * time.Hour), End: mon.Add(-time.Hour)},                  // before the sprint
	)
	left, busy := Capacity(10, ms, from, to, nil)
	if busy != 12*time.Hour || left != 8.5 {
		t.Errorf("Capacity = %v, %v; want 8.5, 12h", left, busy)
	}
	if left, _ := Capacity(10, nil, from, to, nil); left != 10 {
		t.Errorf("no meetings: %v", left)
	}
}

// TestParseHostile: a title can't carry terminal controls, and an RDATE of
// several dates keeps the series.
func TestParseHostile(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTAMP:20260101T000000Z\r\n" +
		"DTSTART:20260105T090000Z\r\nDTEND:20260105T093000Z\r\nRRULE:FREQ=WEEKLY;COUNT=3\r\nRDATE:20260107T090000Z,20260108T090000Z\r\n" +
		"SUMMARY:Stand\\nup \x1b]52;c;aGk=\x07x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ms, err := parse(strings.NewReader(ics), from, from.AddDate(0, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 5 {
		t.Errorf("%d meetings, want 5", len(ms))
	}
	for _, m := range ms {
		if strings.ContainsAny(m.Summary, "\x1b\x07\n") {
			t.Errorf("summary %q", m.Summary)
		}
	}
}
