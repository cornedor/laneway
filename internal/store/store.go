// Package store keeps the app's remembered choices and cached boards: a flat
// key/value map persisted as JSON.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recheck is how often a read looks whether another laneway (the TUI beside
// laneway web, a second terminal) wrote the file.
var recheck = time.Second

type Store struct {
	path    string
	mu      sync.Mutex
	meta    map[string]string
	seen    os.FileInfo // the file as last read or written; nil when there was none
	checked time.Time
}

// Open loads path, starting empty when it does not exist yet.
func Open(path string) (*Store, error) {
	s := &Store{path: path, meta: map[string]string{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the file into the map; the caller holds mu (or owns s).
func (s *Store) load() error {
	fi, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	meta := map[string]string{}
	if err := json.Unmarshal(raw, &meta); err != nil {
		meta = map[string]string{}
	}
	s.meta, s.seen, s.checked = meta, fi, time.Now()
	return nil
}

// refresh rereads the file when another process replaced it since; force
// skips the recheck interval. Every write replaces the file through a
// rename, so a new inode, size or time means another writer. The caller
// holds mu.
func (s *Store) refresh(force bool) {
	if !force && time.Since(s.checked) < recheck {
		return
	}
	s.checked = time.Now()
	fi, err := os.Stat(s.path)
	if err != nil || (s.seen != nil && os.SameFile(fi, s.seen) && fi.ModTime().Equal(s.seen.ModTime()) && fi.Size() == s.seen.Size()) {
		return
	}
	_ = s.load()
}

func (s *Store) GetMeta(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(false)
	v, ok := s.meta[key]
	return v, ok, nil
}

// Prefixed is every key starting with prefix, with its value.
func (s *Store) Prefixed(prefix string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(false)
	out := map[string]string{}
	for k, v := range s.meta {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out
}

// SetMeta stores key and writes the file through a rename, so a crash never
// leaves it half written. What another process wrote since is read first,
// so its keys stay.
func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(true)
	if old, ok := s.meta[key]; ok && old == value {
		return nil
	}
	s.meta[key] = value
	return s.write()
}

// DeleteMeta forgets key.
func (s *Store) DeleteMeta(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh(true)
	if _, ok := s.meta[key]; !ok {
		return nil
	}
	delete(s.meta, key)
	return s.write()
}

// write persists the map; the caller holds mu.
func (s *Store) write() error {
	raw, err := json.Marshal(s.meta)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// A tmp name of its own, so a second laneway writing the same file
	// cannot truncate this one before the rename.
	f, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	if fi, err := os.Stat(s.path); err == nil {
		s.seen = fi
	}
	return nil
}

// Path is the file the store keeps its state in.
func (s *Store) Path() string { return s.path }
