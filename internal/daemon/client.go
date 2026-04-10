package daemon

import (
	"context"
	"errors"
	"time"

	pb "github.com/mad01/kitty-session/internal/daemon/proto"
	"github.com/mad01/kitty-session/internal/search"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ErrDaemonNotRunning indicates the search daemon is unreachable.
var ErrDaemonNotRunning = errors.New("search daemon is not running")

// dial connects to the daemon's Unix socket with a timeout.
func dial(socketPath string) (pb.SearchDaemonClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}
	return pb.NewSearchDaemonClient(conn), conn, nil
}

// SearchVia sends a search request to the daemon.
func SearchVia(ctx context.Context, socketPath string, opts search.SearchOptions, repoNames map[string]string) ([]search.Match, error) {
	client, conn, err := dial(socketPath)
	if err != nil {
		return nil, ErrDaemonNotRunning
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.Search(ctx, &pb.SearchRequest{
		Pattern:       opts.Pattern,
		RepoFilter:    opts.RepoFilter,
		FileFilter:    opts.FileFilter,
		Lang:          opts.Lang,
		CaseSensitive: opts.CaseSensitive,
		Limit:         int32(opts.Limit),
		ContextLines:  int32(opts.ContextLines),
		OutputMode:    opts.OutputMode,
		RepoNames:     repoNames,
	})
	if err != nil {
		return nil, ErrDaemonNotRunning
	}

	matches := make([]search.Match, len(resp.Matches))
	for i, m := range resp.Matches {
		matches[i] = search.Match{
			Repo:     m.Repo,
			RepoPath: m.RepoPath,
			File:     m.File,
			Line:     int(m.Line),
			Column:   int(m.Column),
			Text:     m.Text,
			Before:   m.Before,
			After:    m.After,
		}
	}
	return matches, nil
}

// CountVia sends a count request to the daemon.
func CountVia(ctx context.Context, socketPath string, opts search.CountOptions) ([]search.CountResult, int, error) {
	client, conn, err := dial(socketPath)
	if err != nil {
		return nil, 0, ErrDaemonNotRunning
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.Count(ctx, &pb.CountRequest{
		Pattern:    opts.Pattern,
		RepoFilter: opts.RepoFilter,
		Lang:       opts.Lang,
		GroupBy:    opts.GroupBy,
	})
	if err != nil {
		return nil, 0, ErrDaemonNotRunning
	}

	results := make([]search.CountResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = search.CountResult{
			Group: r.Group,
			Count: int(r.Count),
		}
	}
	return results, int(resp.Total), nil
}

// ValidateVia sends a validate request to the daemon.
func ValidateVia(socketPath string, pattern string) (search.QueryInfo, error) {
	client, conn, err := dial(socketPath)
	if err != nil {
		return search.QueryInfo{}, ErrDaemonNotRunning
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Validate(ctx, &pb.ValidateRequest{
		Pattern: pattern,
	})
	if err != nil {
		return search.QueryInfo{}, ErrDaemonNotRunning
	}

	return search.QueryInfo{
		Valid:  resp.Valid,
		Parsed: resp.Parsed,
		Error:  resp.Error,
		Hint:   resp.Hint,
	}, nil
}

// Ping checks if the daemon is alive.
func Ping(socketPath string) error {
	client, conn, err := dial(socketPath)
	if err != nil {
		return ErrDaemonNotRunning
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Ping(ctx, &pb.PingRequest{})
	if err != nil {
		return ErrDaemonNotRunning
	}
	return nil
}

// Shutdown sends a shutdown request to the daemon.
func Shutdown(socketPath string) error {
	client, conn, err := dial(socketPath)
	if err != nil {
		return ErrDaemonNotRunning
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Shutdown(ctx, &pb.ShutdownRequest{})
	if err != nil {
		return ErrDaemonNotRunning
	}
	return nil
}

// EnsureDaemon ensures the daemon is running, starting it if needed.
// Returns nil if the daemon is ready, ErrDaemonNotRunning if it cannot be started.
func EnsureDaemon(indexDir, socketPath string) error {
	if err := Ping(socketPath); err == nil {
		return nil
	}

	if err := StartBackground(); err != nil {
		return ErrDaemonNotRunning
	}

	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := Ping(socketPath); err == nil {
			return nil
		}
	}
	return ErrDaemonNotRunning
}
