// Package store keeps the app's remembered choices and cached boards: a flat
// key/value map persisted as JSON.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// SetMeta stores key and writes the file through a rename, so a crash never
// leaves it half written.
func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.meta[key]; ok && old == value {
		return nil
	}
	s.meta[key] = value
	raw, err := json.Marshal(s.meta)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
