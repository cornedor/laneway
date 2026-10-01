// Package calendar reads your meetings from an iCalendar feed (ui.calendar):
// a file or an http(s)/webcal URL, as calendar apps export or publish them.
// Recurring meetings repeat by their rules, a moved or cancelled one as it
// was changed; all-day events and ones marked free are no meetings.
package calendar

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/cornedor/laneway/internal/jira"
	"github.com/cornedor/laneway/internal/work"
)

// Meeting is a stretch of your calendar.
type Meeting struct {
	Start, End time.Time
	Summary    string
}

// maxFeed bounds what a feed may hold.
const maxFeed = 20 << 20

// Read is source's meetings that overlap [from, to), by start.
func Read(ctx context.Context, source string, from, to time.Time) ([]Meeting, error) {
	source = strings.TrimSpace(source)
	var r io.Reader
	switch {
	case source == "":
		return nil, errors.New("no calendar")
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"), strings.HasPrefix(source, "webcal://"):
		if rest, ok := strings.CutPrefix(source, "webcal://"); ok {
			source = "https://" + rest
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("calendar: %s", resp.Status)
		}
		r = resp.Body
	default:
		f, err := os.Open(work.ExpandHome(source))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	return parse(io.LimitReader(r, maxFeed), from, to)
}

// parse reads a feed's meetings in [from, to).
func parse(r io.Reader, from, to time.Time) ([]Meeting, error) {
	dec := ical.NewDecoder(r)
	var out []Meeting
	for {
		cal, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("calendar: %w", err)
		}
		out = append(out, meetings(cal.Events(), from, to)...)
	}
	slices.SortFunc(out, func(a, b Meeting) int { return cmp.Or(a.Start.Compare(b.Start), strings.Compare(a.Summary, b.Summary)) })
	return out, nil
}

// meetings expands events into the meetings in [from, to). An event with a
// RECURRENCE-ID replaces the occurrence it names.
func meetings(events []ical.Event, from, to time.Time) []Meeting {
	moved := map[string]bool{} // uid + the replaced occurrence's start
	for i := range events {
		e := &events[i]
		localZones(e)
		if p := e.Props.Get(ical.PropRecurrenceID); p != nil {
			if at, err := p.DateTime(time.Local); err == nil {
				moved[uid(e)+at.UTC().Format(time.RFC3339)] = true
			}
		}
	}
	var out []Meeting
	for i := range events {
		e := &events[i]
		if skip(e) {
			continue
		}
		start, err := e.DateTimeStart(time.Local)
		if err != nil {
			continue
		}
		end, err := e.DateTimeEnd(time.Local)
		if err != nil || !end.After(start) {
			continue
		}
		summary := ""
		if p := e.Props.Get(ical.PropSummary); p != nil {
			summary, _ = p.Text()
		}
		long := end.Sub(start)
		add := func(at time.Time) {
			if at.Before(to) && at.Add(long).After(from) {
				out = append(out, Meeting{Start: at, End: at.Add(long), Summary: strings.TrimSpace(summary)})
			}
		}
		set, err := e.RecurrenceSet(time.Local)
		if err != nil || set == nil || e.Props.Get(ical.PropRecurrenceID) != nil {
			add(start)
			continue
		}
		for _, at := range set.Between(from.Add(-long), to, true) {
			if !moved[uid(e)+at.UTC().Format(time.RFC3339)] {
				add(at)
			}
		}
	}
	return out
}

// skip: all day, cancelled or marked free.
func skip(e *ical.Event) bool {
	p := e.Props.Get(ical.PropDateTimeStart)
	if p == nil || p.ValueType() == ical.ValueDate || len(p.Value) == len("20060102") {
		return true
	}
	if st, _ := e.Status(); st == ical.EventCancelled {
		return true
	}
	t := e.Props.Get(ical.PropTransparency)
	return t != nil && strings.EqualFold(t.Value, "TRANSPARENT")
}

func uid(e *ical.Event) string {
	if p := e.Props.Get(ical.PropUID); p != nil {
		return p.Value
	}
	return ""
}

// localZones drops a TZID Go does not know (Outlook writes Windows names,
// "W. Europe Standard Time"), so the time reads as local: right for your
// own calendar more often than not. An EXDATE of several dates becomes one
// per date, as the decoder reads one.
func localZones(e *ical.Event) {
	var ex []ical.Prop
	for _, p := range e.Props[ical.PropExceptionDates] {
		for _, v := range strings.Split(p.Value, ",") {
			q := p
			q.Params = maps.Clone(p.Params)
			q.Value = strings.TrimSpace(v)
			ex = append(ex, q)
		}
	}
	if ex != nil {
		e.Props[ical.PropExceptionDates] = ex
	}
	for _, name := range []string{ical.PropDateTimeStart, ical.PropDateTimeEnd, ical.PropRecurrenceID, ical.PropExceptionDates, ical.PropRecurrenceDates} {
		for i := range e.Props[name] {
			p := &e.Props[name][i]
			if tz := p.Params.Get(ical.PropTimezoneID); tz != "" {
				if _, err := time.LoadLocation(tz); err != nil {
					p.Params.Del(ical.PropTimezoneID)
				}
			}
		}
	}
}

// Proposals are the meetings as worklogs on key, each its own with its
// summary as the comment; one a worklog on key already starts in is left out.
func Proposals(ms []Meeting, key string, logs []jira.Worklog) []work.Proposal {
	out := []work.Proposal{}
	for _, m := range ms {
		if slices.ContainsFunc(logs, func(w jira.Worklog) bool {
			return w.Key == key && !w.Started.Before(m.Start) && w.Started.Before(m.End)
		}) {
			continue
		}
		out = append(out, work.Proposal{Key: key, Start: m.Start, Seconds: int(m.End.Sub(m.Start).Round(time.Minute).Seconds()),
			Sources: map[string]int{"calendar": 1}, Comment: m.Summary})
	}
	return out
}

// Day is source's meetings on day as proposals on key; none without either.
func Day(ctx context.Context, source, key string, day time.Time, logs []jira.Worklog) ([]work.Proposal, error) {
	if strings.TrimSpace(source) == "" || key == "" {
		return nil, nil
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	ms, err := Read(ctx, source, from, from.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	return Proposals(ms, key, logs), nil
}
