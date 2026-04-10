package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/mad01/kitty-session/internal/daemon/proto"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/search"
)

// setupTestRepo creates a temp directory with known files and returns the path.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"main.go":        "package main\nfunc Hello() string { return \"hello\" }\n",
		"util/helper.go": "package util\nfunc Add(a, b int) int { return a + b }\n",
		"README.md":      "# Test Repo\nSome documentation\n",
	}
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return dir
}

// indexTestRepo indexes the test repo and returns the index directory.
func indexTestRepo(t *testing.T, repoPath string) string {
	t.Helper()
	indexDir := t.TempDir()
	repo := finder.Repo{Name: "test/repo", Path: repoPath}
	if err := search.IndexRepo(indexDir, repo); err != nil {
		t.Fatalf("IndexRepo: %v", err)
	}
	return indexDir
}

// startServer indexes a test repo, starts the gRPC daemon on a temp socket,
// and returns a connected client plus a cleanup function.
func startServer(t *testing.T, idleTimeout time.Duration) (pb.SearchDaemonClient, string) {
	t.Helper()

	repoPath := setupTestRepo(t)
	indexDir := indexTestRepo(t, repoPath)

	socketPath := filepath.Join(t.TempDir(), "test.sock")

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(indexDir, socketPath, idleTimeout)
	}()

	// Wait for the socket to appear.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial unix socket: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		// If server is still running, shut it down.
		select {
		case <-errCh:
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cl := pb.NewSearchDaemonClient(conn)
			cl.Shutdown(ctx, &pb.ShutdownRequest{})
			select {
			case <-errCh:
			case <-time.After(5 * time.Second):
			}
		}
	})

	client := pb.NewSearchDaemonClient(conn)

	// Verify the server is responsive before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx, &pb.PingRequest{}); err != nil {
		t.Fatalf("initial ping failed: %v", err)
	}

	return client, socketPath
}

func TestSearch(t *testing.T) {
	client, _ := startServer(t, 5*time.Minute)

	ctx := context.Background()
	resp, err := client.Search(ctx, &pb.SearchRequest{
		Pattern: "Hello",
		Limit:   50,
		RepoNames: map[string]string{
			"test/repo": "/some/path",
		},
	})
	if err != nil {
		t.Fatalf("Search RPC: %v", err)
	}
	if len(resp.GetMatches()) == 0 {
		t.Fatal("expected at least one match for 'Hello'")
	}

	m := resp.GetMatches()[0]
	if m.GetRepo() != "test/repo" {
		t.Errorf("Repo = %q, want %q", m.GetRepo(), "test/repo")
	}
	if m.GetFile() != "main.go" {
		t.Errorf("File = %q, want %q", m.GetFile(), "main.go")
	}
}

func TestCount(t *testing.T) {
	client, _ := startServer(t, 5*time.Minute)

	ctx := context.Background()
	resp, err := client.Count(ctx, &pb.CountRequest{
		Pattern: "func",
		GroupBy: "repo",
	})
	if err != nil {
		t.Fatalf("Count RPC: %v", err)
	}
	if resp.GetTotal() < 2 {
		t.Errorf("total = %d, want >= 2 (Hello + Add)", resp.GetTotal())
	}
	if len(resp.GetResults()) == 0 {
		t.Fatal("expected at least one group result")
	}
}

func TestValidate(t *testing.T) {
	client, _ := startServer(t, 5*time.Minute)

	ctx := context.Background()

	t.Run("valid query", func(t *testing.T) {
		resp, err := client.Validate(ctx, &pb.ValidateRequest{Pattern: "Hello"})
		if err != nil {
			t.Fatalf("Validate RPC: %v", err)
		}
		if !resp.GetValid() {
			t.Errorf("expected valid=true, got false (error: %q)", resp.GetError())
		}
		if resp.GetParsed() == "" {
			t.Error("expected non-empty Parsed for valid query")
		}
	})

	t.Run("invalid query", func(t *testing.T) {
		resp, err := client.Validate(ctx, &pb.ValidateRequest{Pattern: "func (Walk"})
		if err != nil {
			t.Fatalf("Validate RPC: %v", err)
		}
		if resp.GetValid() {
			t.Error("expected valid=false for unmatched paren")
		}
		if resp.GetError() == "" {
			t.Error("expected non-empty Error for invalid query")
		}
	})
}

func TestPing(t *testing.T) {
	client, _ := startServer(t, 5*time.Minute)

	ctx := context.Background()
	_, err := client.Ping(ctx, &pb.PingRequest{})
	if err != nil {
		t.Fatalf("Ping RPC: %v", err)
	}
}

func TestShutdown(t *testing.T) {
	client, socketPath := startServer(t, 5*time.Minute)

	ctx := context.Background()
	_, err := client.Shutdown(ctx, &pb.ShutdownRequest{})
	if err != nil {
		t.Fatalf("Shutdown RPC: %v", err)
	}

	// Wait for the socket to be cleaned up.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("socket file still exists after shutdown")
}

func TestIdleTimeout(t *testing.T) {
	_, socketPath := startServer(t, 1*time.Second)

	// The idle timer is 1 second. Wait for the server to exit.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("server did not exit after idle timeout")
}
