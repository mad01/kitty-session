// Package instance owns the lifecycle of the ks kitty instance: the one kitty
// process, listening on the configured socket, that holds every session tab.
// The user's own kitty is never touched; every command reaches kitty through
// the client this package hands out.
package instance

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
)

const (
	// startPoll is how often Ensure probes the socket while kitty starts.
	startPoll = 100 * time.Millisecond
	// startTimeout bounds the wait for a freshly started instance to answer.
	startTimeout = 10 * time.Second
	// windowTitle is the OS window title of the instance.
	windowTitle = "ks"
	// sidebarCommand is the ks subcommand the home tab runs: a sidebar with
	// no session, so the instance always has one window to anchor tabs on.
	sidebarCommand = "sidebar"
	// agentFlag makes that home sidebar run the Haiku state monitor.
	agentFlag = "--agent"
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

// Ensure returns a client for the instance, starting it when it is not
// running. A new instance opens with the home tab running `ks sidebar` and is
// ready once its socket answers.
func Ensure(cfg *config.Config, opts Options) (*kitty.Client, error) {
	c, err := Client(cfg)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the ks binary: %w", err)
	}
	command := []string{exe, sidebarCommand}
	if opts.Agent {
		command = append(command, agentFlag)
	}
	err = ensure(c, boot{
		socketPath: config.SocketPath(c.Socket()),
		start: kitty.StartOptions{
			Overrides: cfg.Overrides(),
			Command:   command,
			Title:     windowTitle,
		},
		sleep:   time.Sleep,
		timeout: startTimeout,
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func ensure(k kittyInstance, b boot) error {
	if k.Ping() == nil {
		return nil
	}
	// The instance is not answering, so whatever socket file is left is stale.
	if err := os.Remove(b.socketPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove stale socket %s: %w", b.socketPath, err)
	}
	if err := k.Start(b.start); err != nil {
		return fmt.Errorf("cannot start ks instance: %w", err)
	}
	for waited := time.Duration(0); waited < b.timeout; waited += startPoll {
		b.sleep(startPoll)
		if k.Ping() == nil {
			return nil
		}
	}
	return fmt.Errorf("ks instance did not answer on %s within %s", b.socketPath, b.timeout)
}

// Shutdown closes every window in the instance, which ends it. Session
// records stay active and come back on the next attach.
func Shutdown(c *kitty.Client) error {
	if err := c.CloseAll(); err != nil {
		return fmt.Errorf("cannot close ks instance: %w", err)
	}
	return nil
}
