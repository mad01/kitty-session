// Package instance owns the lifecycle of the ks kitty instance: the one kitty
// process, listening on the configured socket, that holds every session tab.
// The user's own kitty is never touched; every command reaches kitty through
// the client this package hands out.
package instance

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"golang.org/x/sys/unix"
)

const (
	// startPoll is how often Ensure probes the socket while kitty starts.
	startPoll = 100 * time.Millisecond
	// startTimeout bounds the wait for a freshly started instance to answer.
	startTimeout = 10 * time.Second
	// dialTimeout bounds the stale-socket probe.
	dialTimeout = 500 * time.Millisecond
	// windowTitle is the OS window title of the instance.
	windowTitle = "ks"
	// sidebarCommand is the ks subcommand the home tab runs: a sidebar with
	// no session, so the instance always has one window to anchor tabs on.
	sidebarCommand = "sidebar"
	// agentFlag makes that home sidebar run the Haiku state monitor.
	agentFlag = "--agent"
	// lockFile guards the start sequence so two ks invocations cannot start
	// two instances on the same socket.
	lockFile = "instance.lock"
)

// ErrNotRunning is returned by Connect when the instance does not answer.
var ErrNotRunning = errors.New("ks instance not running")

// Options tune how Ensure starts the instance when it has to.
type Options struct {
	// Agent starts the home sidebar with --agent, so the Haiku state monitor
	// lives as long as the instance.
	Agent bool
}

// kittyInstance is the slice of kitty.Client that ensure drives; tests fake it.
type kittyInstance interface {
	Ping() error
	Start(kitty.StartOptions) error
}

// boot is everything ensure needs besides the kitty surface.
type boot struct {
	socketPath string // the file a dead instance leaves behind
	start      kitty.StartOptions
	sleep      func(time.Duration)
	timeout    time.Duration
	// stale reports whether the socket file is safe to remove (gone or
	// refused, not a live or wedged listener). A nil stale means "assume
	// stale", which is what the hermetic tests want.
	stale func(socketPath string) bool
}

// Client returns a client for the configured socket without checking that
// the instance answers. Commands that must work while the instance is down
// (close, rename, list) use it; their kitty calls then fail fast.
func Client(cfg *config.Config) (*kitty.Client, error) {
	socket, err := cfg.Socket()
	if err != nil {
		return nil, err
	}
	return kitty.New(socket), nil
}

// Connect returns a client for the running instance, or ErrNotRunning.
func Connect(cfg *config.Config) (*kitty.Client, error) {
	c, err := Client(cfg)
	if err != nil {
		return nil, err
	}
	if err := c.Ping(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	return c, nil
}

// Await returns a client once the instance answers, polling for up to
// startTimeout. The sidebars inside the instance use it: kitty runs the home
// sidebar as its first window, and the socket may answer a moment later.
func Await(cfg *config.Config) (*kitty.Client, error) {
	c, err := Client(cfg)
	if err != nil {
		return nil, err
	}
	if err := c.Ping(); err == nil {
		return c, nil
	}
	if err := awaitPing(c, time.Sleep, startTimeout); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	return c, nil
}

// Ensure returns a client for the instance, starting it when it is not
// running, and reports whether it did start one, so the caller can bring the
// sessions back before adding its own. A new instance opens with the home tab
// running `ks sidebar` and is ready once its socket answers.
func Ensure(cfg *config.Config, opts Options) (*kitty.Client, bool, error) {
	c, err := Client(cfg)
	if err != nil {
		return nil, false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, false, fmt.Errorf("cannot locate the ks binary: %w", err)
	}
	command := []string{exe, sidebarCommand}
	if opts.Agent {
		command = append(command, agentFlag)
	}
	b := boot{
		socketPath: config.SocketPath(c.Socket()),
		start: kitty.StartOptions{
			Overrides: cfg.Overrides(),
			Command:   command,
			Title:     windowTitle,
		},
		sleep:   time.Sleep,
		timeout: startTimeout,
		stale:   staleSocket,
	}
	var started bool
	err = withInstanceLock(b.socketPath, func() error {
		var err error
		started, err = ensure(c, b)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return c, started, nil
}

// ensure pings, starts kitty when nothing answers, and reports whether it did.
func ensure(k kittyInstance, b boot) (bool, error) {
	if k.Ping() == nil {
		return false, nil
	}
	if b.stale == nil || b.stale(b.socketPath) {
		if err := os.Remove(b.socketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("cannot remove stale socket %s: %w", b.socketPath, err)
		}
		// A concurrent ks may have finished starting the instance while we held
		// the lock; one more ping before we spend a kitty launch.
		if k.Ping() == nil {
			return false, nil
		}
	}
	if err := k.Start(b.start); err != nil {
		return false, fmt.Errorf("cannot start ks instance: %w", err)
	}
	if err := awaitPing(k, b.sleep, b.timeout); err != nil {
		return false, fmt.Errorf("ks instance did not answer on %s within %s: %w",
			b.socketPath, b.timeout, err)
	}
	return true, nil
}

// staleSocket reports whether the socket file is safe to remove: it is gone
// (ENOENT) or nothing listens (ECONNREFUSED). A dial that connects means a
// live instance; a dial that times out means a wedged one. Neither is
// removed, so ks never clobbers an instance that might still be serving.
func staleSocket(path string) bool {
	conn, err := net.DialTimeout("unix", path, dialTimeout)
	if err == nil {
		_ = conn.Close()
		return false
	}
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

// withInstanceLock runs fn while holding an exclusive flock on the instance
// lock file beside the socket, so two ks invocations serialize their start
// sequence instead of racing to launch two instances.
func withInstanceLock(socketPath string, fn func() error) error {
	path := filepath.Join(filepath.Dir(socketPath), lockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("cannot open instance lock %s: %w", path, err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("cannot lock the ks instance: %w", err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()
	return fn()
}

// awaitPing sleeps startPoll and pings, until k answers or timeout has
// passed; it returns the last ping error.
func awaitPing(k kittyInstance, sleep func(time.Duration), timeout time.Duration) error {
	var err error
	for waited := time.Duration(0); waited < timeout; waited += startPoll {
		sleep(startPoll)
		if err = k.Ping(); err == nil {
			return nil
		}
	}
	return err
}

// Shutdown closes every window in the instance, which ends it. Session
// records stay active and come back on the next attach.
func Shutdown(c *kitty.Client) error {
	if err := c.CloseAll(); err != nil {
		return fmt.Errorf("cannot close ks instance: %w", err)
	}
	return nil
}
