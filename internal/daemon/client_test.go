package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/search"
)

func TestSearchVia(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	repoNames := map[string]string{"test/repo": "/some/path"}
	matches, err := SearchVia(context.Background(), sockPath, search.SearchOptions{
		Pattern: "Hello",
		Limit:   10,
	}, repoNames)
	if err != nil {
		t.Fatalf("SearchVia: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}

	found := false
	for _, m := range matches {
		if m.Repo == "test/repo" && m.File == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected match in test/repo main.go, got: %+v", matches)
	}
}

func TestCountVia(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	results, total, err := CountVia(context.Background(), sockPath, search.CountOptions{
		Pattern: "func",
		GroupBy: "repo",
	})
	if err != nil {
		t.Fatalf("CountVia: %v", err)
	}
	if total == 0 {
		t.Fatal("expected total > 0")
	}
	if len(results) == 0 {
		t.Fatal("expected at least one count result")
	}

	found := false
	for _, r := range results {
		if r.Group == "test/repo" && r.Count > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected count for test/repo, got: %+v", results)
	}
}

func TestValidateVia_ValidQuery(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	info, err := ValidateVia(sockPath, "Hello")
	if err != nil {
		t.Fatalf("ValidateVia: %v", err)
	}
	if !info.Valid {
		t.Fatalf("expected valid query, got error: %s", info.Error)
	}
	if info.Parsed == "" {
		t.Fatal("expected non-empty parsed string")
	}
}

func TestValidateVia_InvalidQuery(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	info, err := ValidateVia(sockPath, "(unclosed")
	if err != nil {
		t.Fatalf("ValidateVia: %v", err)
	}
	if info.Valid {
		t.Fatal("expected invalid query")
	}
	if info.Error == "" {
		t.Fatal("expected error message for invalid query")
	}
}

func TestClientPing_Running(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	if err := Ping(sockPath); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClientPing_NotRunning(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	err := Ping(sockPath)
	if err == nil {
		t.Fatal("expected error for non-existent socket")
	}
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Fatalf("expected ErrDaemonNotRunning, got: %v", err)
	}
}

func TestClientShutdown(t *testing.T) {
	_, sockPath := startServer(t, 5*time.Minute)

	// Verify the server is up.
	if err := Ping(sockPath); err != nil {
		t.Fatalf("pre-shutdown Ping: %v", err)
	}

	if err := Shutdown(sockPath); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
