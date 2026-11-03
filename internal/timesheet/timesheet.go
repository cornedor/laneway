// Package timesheet is the provider-neutral half of laneway's time
// tracking: the entries the timesheet, week and proposals show, and the
// Provider interface a backend implements. Jira's worklogs are one backend
// (internal/timesheet/jirasheet); a timesheet server speaking laneway's HTTP
// contract (docs/reference/timesheet-api.md) is another
// (internal/timesheet/httpsheet). One provider is active at a time.
package timesheet

import (
	"context"
	"errors"
	"time"
)

// Entry is one stretch of logged work.
type Entry struct {
	ID string
	// Key is the issue it is logged on; "" for an entry without one, which
	// a backend with Caps.Keyless allows.
	Key     string
	Summary string
	Started time.Time
	// Seconds is the time logged; a running entry's is the time so far.
	Seconds int
	Comment string
	// Running is the backend's running timer (Caps.ServerTimer).
	Running bool `json:",omitempty"`
	// Pending is an entry the backend took but has not stored yet: shown,
	// but its ID may still change.
	Pending bool `json:",omitempty"`
}

// Caps are what a provider can do beyond logging, reading, editing and
// deleting entries.
type Caps struct {
	// ServerTimer: the backend runs the timer (Running, Start); laneway's
	// own timer then shows and stops that one.
	ServerTimer bool
	// Move: entries move to another issue (Move).
	Move bool
	// Remaining: a new entry's Left sets the issue's remaining estimate.
	Remaining bool
	// Keyless: entries may lack an issue.
	Keyless bool
}

// New is an entry to log.
type New struct {
	Key     string
	Seconds int
	Started time.Time
	Comment string
	// Left is what the remaining estimate becomes (Caps.Remaining): "" takes
	// the time off it, "keep" leaves it, else a duration ("2h").
	Left string
}

// ErrUnsupported is a call the provider's Caps rule out, for errors.Is.
var ErrUnsupported = errors.New("timesheet: not supported by this provider")

// Provider is one timesheet backend. Implementations are safe for
// concurrent use: the UI calls them from background goroutines. Calls that
// name an entry pass its issue key too: Jira's worklog API needs it, a
// backend with its own ids ignores it.
type Provider interface {
	// Name is the backend's display name ("Jira", "easyreg").
	Name() string
	// Caps are what it can do; a provider that reads them from its backend
	// reports zero Caps until a call has.
	Caps() Caps
	// Entries are yours that start in [from, to), by start.
	Entries(ctx context.Context, from, to time.Time) ([]Entry, error)
	// Add logs a new entry.
	Add(ctx context.Context, e New) error
	// Update sets entry id's time, its comment unless comment is nil, and
	// its start unless started is zero. On a running entry it stops it,
	// seconds after its start.
	Update(ctx context.Context, key, id string, seconds int, started time.Time, comment *string) error
	// Delete removes entries ids, all on key.
	Delete(ctx context.Context, key string, ids []string) error
	// Move puts entries ids, all on key, on the issue to (Caps.Move).
	Move(ctx context.Context, key string, ids []string, to string) error
	// Running is the backend's running entry, nil when none runs or the
	// backend has no timer (Caps.ServerTimer).
	Running(ctx context.Context) (*Entry, error)
	// Start starts the backend's timer on key at at, ending a running one
	// there (Caps.ServerTimer).
	Start(ctx context.Context, key, comment string, at time.Time) error
}

// Day is p's entries on day (local midnight to midnight).
func Day(ctx context.Context, p Provider, day time.Time) ([]Entry, error) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return p.Entries(ctx, from, from.AddDate(0, 0, 1))
}
