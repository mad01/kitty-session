package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	dir string
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".config", "ks", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create sessions directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Save(sess *Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal session: %w", err)
	}
	path := s.path(sess.Name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("cannot write session file: %w", err)
	}
	return nil
}

func (s *Store) Load(name string) (*Session, error) {
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return nil, fmt.Errorf("session %q not found: %w", name, err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("cannot parse session file: %w", err)
	}
	return &sess, nil
}

func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read sessions directory: %w", err)
	}
	var sessions []*Session
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		sess, err := s.Load(name)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *Store) Delete(name string) error {
	path := s.path(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot delete session file: %w", err)
	}
	return nil
}

func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.path(name))
	return err == nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}
