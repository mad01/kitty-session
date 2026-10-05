package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		sess *Session
	}{
		{
			name: "new session is active with no claude id",
			sess: New("fresh", "/tmp/fresh", 1, 2),
		},
		{
			name: "stopped session keeps status and claude id",
			sess: &Session{
				Name:            "stopped",
				Dir:             "/tmp/stopped",
				KittyTabID:      3,
				Status:          StatusStopped,
				ClaudeSessionID: "0b5c1d2e-aaaa-bbbb-cccc-000000000001",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			if err := store.Save(tc.sess); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := store.Load(tc.sess.Name)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got.Status != tc.sess.Status {
				t.Errorf("Status = %q, want %q", got.Status, tc.sess.Status)
			}
			if got.ClaudeSessionID != tc.sess.ClaudeSessionID {
				t.Errorf(
					"ClaudeSessionID = %q, want %q",
					got.ClaudeSessionID,
					tc.sess.ClaudeSessionID,
				)
			}
			if got.KittyTabID != tc.sess.KittyTabID {
				t.Errorf("KittyTabID = %d, want %d", got.KittyTabID, tc.sess.KittyTabID)
			}
		})
	}
}

func TestSaveLeavesOnlyTheSessionFile(t *testing.T) {
	store := newTestStore(t)
	sess := New("atomic", "/tmp/atomic", 1, 2)
	for i := range 2 { // second Save replaces the first
		sess.KittyTabID = i + 10
		if err := store.Save(sess); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) != 1 || names[0] != "atomic.json" {
		t.Fatalf("sessions dir holds %v, want only atomic.json", names)
	}

	info, err := os.Stat(filepath.Join(store.dir, "atomic.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 644", perm)
	}
	got, err := store.Load("atomic")
	if err != nil {
		t.Fatal(err)
	}
	if got.KittyTabID != 11 {
		t.Errorf("KittyTabID = %d, want 11 (second write)", got.KittyTabID)
	}
}

func TestLegacyFileWithoutStatusIsActive(t *testing.T) {
	store := newTestStore(t)
	legacy := map[string]any{
		"name":            "legacy",
		"dir":             "/tmp/legacy",
		"created_at":      "2026-04-16T10:15:00Z",
		"kitty_tab_id":    42,
		"kitty_window_id": 87,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path("legacy"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("legacy")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Status != "" {
		t.Errorf("Status = %q, want empty for a legacy file", got.Status)
	}
	if !got.IsActive() {
		t.Error("IsActive() = false, want true for a legacy file")
	}
	if got.ClaudeSessionID != "" {
		t.Errorf("ClaudeSessionID = %q, want empty", got.ClaudeSessionID)
	}
}

func TestIsActive(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"", true},
		{StatusActive, true},
		{StatusStopped, false},
	}
	for _, tc := range tests {
		s := &Session{Status: tc.status}
		if got := s.IsActive(); got != tc.want {
			t.Errorf("IsActive() with Status %q = %v, want %v", tc.status, got, tc.want)
		}
	}
}
