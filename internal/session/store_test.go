package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
				ID:                   "feedface00000000feedface00000000",
				Name:                 "stopped",
				Dir:                  "/tmp/stopped",
				KittyTabID:           3,
				Status:               StatusStopped,
				ClaudeSessionID:      "0b5c1d2e-aaaa-bbbb-cccc-000000000001",
				ClaudeTranscriptPath: "/tmp/stopped.jsonl",
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
			if got.ID != tc.sess.ID {
				t.Errorf("ID = %q, want %q", got.ID, tc.sess.ID)
			}
			if got.ClaudeTranscriptPath != tc.sess.ClaudeTranscriptPath {
				t.Errorf(
					"ClaudeTranscriptPath = %q, want %q",
					got.ClaudeTranscriptPath,
					tc.sess.ClaudeTranscriptPath,
				)
			}
		})
	}
}

func TestNewAssignsUniqueID(t *testing.T) {
	a, b := New("a", "/tmp/a", 0, 0), New("b", "/tmp/b", 0, 0)
	if len(a.ID) != 2*idBytes {
		t.Errorf("len(ID) = %d, want %d hex chars", len(a.ID), 2*idBytes)
	}
	if a.ID == b.ID {
		t.Errorf("two New calls share ID %q", a.ID)
	}
}

func TestFindByID(t *testing.T) {
	store := newTestStore(t)
	want := New("findme", "/tmp/findme", 1, 2)
	for _, sess := range []*Session{want, New("other", "/tmp/other", 3, 4)} {
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	// A rename changes the file name but not the ID.
	if _, err := store.Rename("findme", "renamed"); err != nil {
		t.Fatal(err)
	}

	got, err := store.FindByID(want.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "renamed" {
		t.Errorf("Name = %q, want renamed", got.Name)
	}
	for _, id := range []string{"", "no-such-id"} {
		if _, err := store.FindByID(id); err == nil {
			t.Errorf("FindByID(%q) = nil error, want not found", id)
		}
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

// TestFocusedAtRoundTrip: a record that was never focused serializes without
// the field, so old readers and hand-edits see no bogus zero time; a stamped
// one survives the trip.
func TestFocusedAtRoundTrip(t *testing.T) {
	store := newTestStore(t)
	fresh := New("fresh", "/tmp/fresh", 1, 2)
	fresh.KittySidebarWindowID = 3
	if err := store.Save(fresh); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.path("fresh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "focused_at") {
		t.Errorf("zero FocusedAt written: %s", raw)
	}

	stamp := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	fresh.FocusedAt = stamp
	if err := store.Save(fresh); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("fresh")
	if err != nil {
		t.Fatal(err)
	}
	if !got.FocusedAt.Equal(stamp) {
		t.Errorf("FocusedAt = %v, want %v", got.FocusedAt, stamp)
	}
	if got.KittySidebarWindowID != 3 {
		t.Errorf("KittySidebarWindowID = %d, want 3", got.KittySidebarWindowID)
	}
}
