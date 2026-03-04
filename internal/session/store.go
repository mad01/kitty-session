package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	dir      string
	trashDir string
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
	trashDir := filepath.Join(dir, "trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create trash directory: %w", err)
	}
	return &Store{dir: dir, trashDir: trashDir}, nil
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
	src := s.path(name)
	dst := filepath.Join(s.trashDir, name+".json")
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot move session to trash: %w", err)
	}
	return nil
}

func (s *Store) ListTrashed() ([]*Session, error) {
	entries, err := os.ReadDir(s.trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read trash directory: %w", err)
	}
	var sessions []*Session
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(s.trashDir, e.Name()))
		if err != nil {
			continue
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		sess.Name = name
		sessions = append(sessions, &sess)
	}
	return sessions, nil
}

func (s *Store) Restore(name string) error {
	src := filepath.Join(s.trashDir, name+".json")
	dst := s.path(name)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("cannot restore session: %w", err)
	}
	return nil
}

// Rename changes a session's name, returning the updated session.
func (s *Store) Rename(oldName, newName string) (*Session, error) {
	sess, err := s.Load(oldName)
	if err != nil {
		return nil, err
	}
	if s.Exists(newName) {
		return nil, fmt.Errorf("session %q already exists", newName)
	}
	sess.Name = newName
	if err := s.Save(sess); err != nil {
		return nil, err
	}
	if err := os.Remove(s.path(oldName)); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot remove old session file: %w", err)
	}
	return sess, nil
}

func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.path(name))
	return err == nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+".json")
}
