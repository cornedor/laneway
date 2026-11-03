// Package httpsheet is the timesheet provider on a server that speaks
// laneway's timesheet API (docs/reference/timesheet-api.md).
package httpsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cornedor/laneway/internal/timesheet"
)

// Sheet is one timesheet server.
type Sheet struct {
	base  string // the API's root, without /v1 or a trailing slash
	token string
	hc    *http.Client

	mu     sync.Mutex
	name   string
	caps   timesheet.Caps
	loaded bool
}

// New is the provider on the server at base; token, when set, goes along
// as a bearer token.
func New(base, token string) *Sheet {
	return &Sheet{base: strings.TrimRight(strings.TrimSpace(base), "/"), token: token, hc: &http.Client{Timeout: 20 * time.Second}}
}

// Name is the server's own name once read, else its host.
func (s *Sheet) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name != "" {
		return s.name
	}
	if u, err := url.Parse(s.base); err == nil && u.Host != "" {
		return u.Host
	}
	return "timesheet"
}

// Caps are zero until a call has read them.
func (s *Sheet) Caps() timesheet.Caps {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps
}

// info reads the server's name and caps once.
func (s *Sheet) info(ctx context.Context) (timesheet.Caps, error) {
	s.mu.Lock()
	if s.loaded {
		defer s.mu.Unlock()
		return s.caps, nil
	}
	s.mu.Unlock()
	var in struct {
		Name string `json:"name"`
		Caps struct {
			ServerTimer bool `json:"serverTimer"`
			Move        bool `json:"move"`
			Keyless     bool `json:"keyless"`
		} `json:"caps"`
	}
	if err := s.do(ctx, http.MethodGet, "/v1/info", nil, &in); err != nil {
		return timesheet.Caps{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name, s.loaded = strings.TrimSpace(in.Name), true
	s.caps = timesheet.Caps{ServerTimer: in.Caps.ServerTimer, Move: in.Caps.Move, Keyless: in.Caps.Keyless}
	return s.caps, nil
}

// entry is an Entry on the wire.
type entry struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	Start       time.Time `json:"start"`
	Seconds     int       `json:"seconds"`
	Running     bool      `json:"running"`
	Pending     bool      `json:"pending"`
}

func (e entry) entry() timesheet.Entry {
	return timesheet.Entry{ID: e.ID, Key: e.Key, Summary: e.Summary, Started: e.Start, Seconds: e.Seconds,
		Comment: e.Description, Running: e.Running, Pending: e.Pending}
}

func (s *Sheet) Entries(ctx context.Context, from, to time.Time) ([]timesheet.Entry, error) {
	if _, err := s.info(ctx); err != nil {
		return nil, err
	}
	var in struct {
		Entries []entry `json:"entries"`
	}
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}}
	if err := s.do(ctx, http.MethodGet, "/v1/entries?"+q.Encode(), nil, &in); err != nil {
		return nil, err
	}
	out := make([]timesheet.Entry, len(in.Entries))
	for i, e := range in.Entries {
		out[i] = e.entry()
	}
	return out, nil
}

// newEntry is POST /v1/entries's body; no Seconds starts the timer.
type newEntry struct {
	Key         string    `json:"key"`
	Description string    `json:"description"`
	Start       time.Time `json:"start"`
	Seconds     *int      `json:"seconds,omitempty"`
}

func (s *Sheet) Add(ctx context.Context, e timesheet.New) error {
	if e.Seconds < 60 {
		return errors.New("timesheet: log at least a minute")
	}
	return s.do(ctx, http.MethodPost, "/v1/entries", newEntry{Key: e.Key, Description: e.Comment, Start: e.Started, Seconds: &e.Seconds}, nil)
}

// patch is PATCH /v1/entries/{id}'s body.
type patch struct {
	Key         *string    `json:"key,omitempty"`
	Description *string    `json:"description,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Seconds     *int       `json:"seconds,omitempty"`
}

func entryPath(id string) string { return "/v1/entries/" + url.PathEscape(id) }

func (s *Sheet) Update(ctx context.Context, _, id string, seconds int, started time.Time, comment *string) error {
	if seconds < 60 {
		return errors.New("timesheet: log at least a minute")
	}
	p := patch{Description: comment, Seconds: &seconds}
	if !started.IsZero() {
		p.Start = &started
	}
	return s.do(ctx, http.MethodPatch, entryPath(id), p, nil)
}

func (s *Sheet) Delete(ctx context.Context, _ string, ids []string) error {
	for _, id := range ids {
		if err := s.do(ctx, http.MethodDelete, entryPath(id), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sheet) Move(ctx context.Context, _ string, ids []string, to string) error {
	if caps, err := s.info(ctx); err != nil || !caps.Move {
		return orElse(err, timesheet.ErrUnsupported)
	}
	for _, id := range ids {
		if err := s.do(ctx, http.MethodPatch, entryPath(id), patch{Key: &to}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sheet) Running(ctx context.Context) (*timesheet.Entry, error) {
	if caps, err := s.info(ctx); err != nil || !caps.ServerTimer {
		return nil, err
	}
	var in *entry
	if err := s.do(ctx, http.MethodGet, "/v1/running", nil, &in); err != nil || in == nil {
		return nil, err
	}
	e := in.entry()
	return &e, nil
}

func (s *Sheet) Start(ctx context.Context, key, comment string, at time.Time) error {
	if caps, err := s.info(ctx); err != nil || !caps.ServerTimer {
		return orElse(err, timesheet.ErrUnsupported)
	}
	return s.do(ctx, http.MethodPost, "/v1/entries", newEntry{Key: key, Description: comment, Start: at}, nil)
}

// orElse is err, else fallback.
func orElse(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// Error is the server's refusal, with its hint and where to fix it.
type Error struct {
	Status int
	Msg    string
	Hint   string
	URL    string
}

func (e *Error) Error() string {
	out := e.Msg
	if out == "" {
		out = http.StatusText(e.Status)
	}
	if e.Hint != "" {
		out += " — " + e.Hint
	}
	if e.URL != "" {
		out += " (" + e.URL + ")"
	}
	return out
}

// maxBody bounds a response.
const maxBody = 8 << 20

func (s *Sheet) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", s.Name(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%s: %w", s.Name(), err)
	}
	if resp.StatusCode/100 != 2 {
		e := &Error{Status: resp.StatusCode}
		var in struct {
			Error string `json:"error"`
			Hint  string `json:"hint"`
			URL   string `json:"url"`
		}
		if json.Unmarshal(raw, &in) == nil {
			e.Msg, e.Hint, e.URL = in.Error, in.Hint, in.URL
		}
		return fmt.Errorf("%s: %w", s.Name(), e)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %s %s: %w", s.Name(), method, path, err)
	}
	return nil
}
