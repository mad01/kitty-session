package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sourcegraph/zoekt"
	zoektsearch "github.com/sourcegraph/zoekt/search"
	"google.golang.org/grpc"

	pb "github.com/mad01/kitty-session/internal/daemon/proto"
	"github.com/mad01/kitty-session/internal/search"
)

type searchServer struct {
	pb.UnimplementedSearchDaemonServer
	searcher zoekt.Searcher
	indexDir string
	cancel   context.CancelFunc
	idle     *time.Timer
	idleMu   sync.Mutex
	idleDur  time.Duration
}

func (s *searchServer) resetIdle() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idle != nil {
		s.idle.Reset(s.idleDur)
	}
}

func (s *searchServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	s.resetIdle()

	opts := search.SearchOptions{
		Pattern:       req.GetPattern(),
		RepoFilter:    req.GetRepoFilter(),
		FileFilter:    req.GetFileFilter(),
		Lang:          req.GetLang(),
		CaseSensitive: req.GetCaseSensitive(),
		Limit:         int(req.GetLimit()),
		ContextLines:  int(req.GetContextLines()),
		OutputMode:    req.GetOutputMode(),
	}

	matches, err := search.SearchWith(ctx, s.searcher, opts, req.GetRepoNames())
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	resp := &pb.SearchResponse{
		Matches: make([]*pb.Match, len(matches)),
	}
	for i, m := range matches {
		resp.Matches[i] = &pb.Match{
			Repo:     m.Repo,
			RepoPath: m.RepoPath,
			File:     m.File,
			Line:     int32(m.Line),
			Column:   int32(m.Column),
			Text:     m.Text,
			Before:   m.Before,
			After:    m.After,
		}
	}
	return resp, nil
}

func (s *searchServer) Count(ctx context.Context, req *pb.CountRequest) (*pb.CountResponse, error) {
	s.resetIdle()

	opts := search.CountOptions{
		Pattern:    req.GetPattern(),
		RepoFilter: req.GetRepoFilter(),
		Lang:       req.GetLang(),
		GroupBy:    req.GetGroupBy(),
	}

	results, total, err := search.CountWith(ctx, s.searcher, opts)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	resp := &pb.CountResponse{
		Total:   int32(total),
		Results: make([]*pb.CountResult, len(results)),
	}
	for i, r := range results {
		resp.Results[i] = &pb.CountResult{
			Group: r.Group,
			Count: int32(r.Count),
		}
	}
	return resp, nil
}

func (s *searchServer) Validate(_ context.Context, req *pb.ValidateRequest) (*pb.ValidateResponse, error) {
	s.resetIdle()

	info := search.ValidateQuery(req.GetPattern())
	return &pb.ValidateResponse{
		Valid:  info.Valid,
		Parsed: info.Parsed,
		Error:  info.Error,
		Hint:   info.Hint,
	}, nil
}

func (s *searchServer) Ping(_ context.Context, _ *pb.PingRequest) (*pb.PingResponse, error) {
	s.resetIdle()
	return &pb.PingResponse{}, nil
}

func (s *searchServer) Shutdown(_ context.Context, _ *pb.ShutdownRequest) (*pb.ShutdownResponse, error) {
	s.resetIdle()
	s.cancel()
	return &pb.ShutdownResponse{}, nil
}

// Serve starts the gRPC daemon on a Unix socket.
func Serve(indexDir, socketPath string, idleTimeout time.Duration) error {
	searcher, err := zoektsearch.NewDirectorySearcher(indexDir)
	if err != nil {
		return fmt.Errorf("open index at %s: %w", indexDir, err)
	}

	pidPath := DefaultPIDPath()
	if err := RemoveStale(socketPath, pidPath); err != nil {
		searcher.Close()
		return fmt.Errorf("remove stale files: %w", err)
	}

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		searcher.Close()
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	if err := WritePID(pidPath); err != nil {
		lis.Close()
		searcher.Close()
		return fmt.Errorf("write pid: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	srv := &searchServer{
		searcher: searcher,
		indexDir: indexDir,
		cancel:   cancel,
		idleDur:  idleTimeout,
	}

	srv.idle = time.AfterFunc(idleTimeout, cancel)

	grpcServer := grpc.NewServer()
	pb.RegisterSearchDaemonServer(grpcServer, srv)

	// Handle SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	go grpcServer.Serve(lis)

	<-ctx.Done()

	grpcServer.GracefulStop()
	searcher.Close()

	// Cleanup socket and PID files.
	os.Remove(socketPath)
	os.Remove(pidPath)

	return nil
}
