// Package sheettest is an in-memory server speaking laneway's timesheet API
// (docs/reference/timesheet-api.md), for tests.
package sheettest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Entry is a stored entry; End nil runs.
type Entry struct {
	ID, Key, Description string
	Start                time.Time
	End                  *time.Time
}

// Server is the fake. Its fields are safe to read under Lock.
type Server struct {
	*httptest.Server
	sync.Mutex
	Entries []*Entry
	// Caps are what /v1/info reports.
	ServerTimer, Move, Keyless bool
	// Login, when set, answers every call 401 with it as the hint.
	Login string
	// Now is the clock; time.Now when nil.
	Now  func() time.Time
	next int
}

// New starts a server with every cap on.
func New() *Server {
	s := &Server{ServerTimer: true, Move: true, Keyless: true}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", s.info)
	mux.HandleFunc("GET /v1/entries", s.list)
	mux.HandleFunc("GET /v1/running", s.running)
	mux.HandleFunc("POST /v1/entries", s.create)
	mux.HandleFunc("PATCH /v1/entries/{id}", s.patch)
	mux.HandleFunc("DELETE /v1/entries/{id}", s.delete)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Lock()
		defer s.Unlock()
		if s.Login != "" {
			fail(w, http.StatusUnauthorized, "sign in first", s.Login)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return s
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Running is the running entry, nil for none. Call under Lock.
func (s *Server) Running() *Entry {
	for _, e := range s.Entries {
		if e.End == nil {
			return e
		}
	}
	return nil
}

func fail(w http.ResponseWriter, status int, msg, hint string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "hint": hint})
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) wire(e *Entry) map[string]any {
	end := s.now()
	if e.End != nil {
		end = *e.End
	}
	return map[string]any{"id": e.ID, "key": e.Key, "summary": "", "description": e.Description,
		"start": e.Start, "seconds": int(end.Sub(e.Start).Seconds()), "running": e.End == nil, "pending": false}
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]any{"name": "fake", "caps": map[string]bool{"serverTimer": s.ServerTimer, "move": s.Move, "keyless": s.Keyless}})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		fail(w, http.StatusBadRequest, "bad from or to", "")
		return
	}
	out := []map[string]any{}
	sorted := slices.Clone(s.Entries)
	slices.SortFunc(sorted, func(a, b *Entry) int { return a.Start.Compare(b.Start) })
	for _, e := range sorted {
		if !e.Start.Before(from) && e.Start.Before(to) {
			out = append(out, s.wire(e))
		}
	}
	reply(w, http.StatusOK, map[string]any{"entries": out})
}

func (s *Server) running(w http.ResponseWriter, _ *http.Request) {
	if e := s.Running(); e != nil {
		reply(w, http.StatusOK, s.wire(e))
		return
	}
	reply(w, http.StatusOK, nil)
}

type body struct {
	Key         *string    `json:"key"`
	Description *string    `json:"description"`
	Start       *time.Time `json:"start"`
	Seconds     *int       `json:"seconds"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var b body
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Start == nil || b.Key == nil {
		fail(w, http.StatusBadRequest, "want key and start", "")
		return
	}
	if *b.Key == "" && !s.Keyless {
		fail(w, http.StatusUnprocessableEntity, "an entry needs an issue", "")
		return
	}
	s.next++
	e := &Entry{ID: strconv.Itoa(s.next), Key: *b.Key, Start: *b.Start}
	if b.Description != nil {
		e.Description = *b.Description
	}
	if b.Seconds != nil {
		end := e.Start.Add(time.Duration(*b.Seconds) * time.Second)
		e.End = &end
	} else {
		if !s.ServerTimer {
			fail(w, http.StatusBadRequest, "want seconds", "")
			return
		}
		if run := s.Running(); run != nil {
			end := e.Start
			run.End = &end
		}
	}
	s.Entries = append(s.Entries, e)
	reply(w, http.StatusCreated, s.wire(e))
}

func (s *Server) find(id string) *Entry {
	for _, e := range s.Entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	e := s.find(r.PathValue("id"))
	if e == nil {
		fail(w, http.StatusNotFound, "no such entry", "")
		return
	}
	var b body
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, http.StatusBadRequest, "bad body", "")
		return
	}
	if b.Key != nil {
		if !s.Move {
			fail(w, http.StatusUnprocessableEntity, "entries don't move", "")
			return
		}
		e.Key = *b.Key
	}
	if b.Description != nil {
		e.Description = *b.Description
	}
	if b.Start != nil {
		e.Start = *b.Start
	}
	if b.Seconds != nil {
		end := e.Start.Add(time.Duration(*b.Seconds) * time.Second)
		e.End = &end
	}
	reply(w, http.StatusOK, s.wire(e))
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	i := slices.IndexFunc(s.Entries, func(e *Entry) bool { return e.ID == r.PathValue("id") })
	if i < 0 {
		fail(w, http.StatusNotFound, "no such entry", "")
		return
	}
	s.Entries = slices.Delete(s.Entries, i, i+1)
	w.WriteHeader(http.StatusNoContent)
}
