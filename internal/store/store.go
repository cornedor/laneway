// Package store keeps the app's remembered choices and cached boards: a flat
// key/value map persisted as JSON.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
	meta map[string]string
}

// Open loads path, starting empty when it does not exist yet.
func Open(path string) (*Store, error) {
	s := &Store{path: path, meta: map[string]string{}}
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.meta); err != nil {
		s.meta = map[string]string{}
	}
	return s, nil
}

func (s *Store) GetMeta(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.meta[key]
	return v, ok, nil
}

// Prefixed is every value whose key starts with prefix, in no order.
func (s *Store) Prefixed(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, v := range s.meta {
		if strings.HasPrefix(k, prefix) {
			out = append(out, v)
		}
	}
	return out
}

// SetMeta stores key and writes the file through a rename, so a crash never
// leaves it half written.
func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return os.Rename(f.Name(), s.path)
}

// Path is the file the store keeps its state in.
func (s *Store) Path() string { return s.path }
