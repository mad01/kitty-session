// Package cmux opens workspaces in cmux, the Ghostty-based agent terminal, so
// ks import --to cmux can hand herdr's claude agents to cmux instead of the
// ks kitty instance. Client is the only place in ks that runs the cmux CLI.
// cmux keeps and restores its own workspaces, so ks writes no records for
// them.
package cmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// appBinary is where the cmux app ships its CLI; the app does not put it on
// PATH.
const appBinary = "/Applications/cmux.app/Contents/Resources/bin/cmux"

// shellSafe is the set of characters a shell word may hold unquoted.
const shellSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@%+,"

// ErrNotInstalled means no cmux CLI was found on PATH or in the app bundle.
var ErrNotInstalled = errors.New("cmux: not installed")

// Workspace is one cmux workspace to create: a named terminal started in Dir
// running Command.
type Workspace struct {
	Name    string
	Dir     string
	Command []string
}

// Client runs the cmux CLI at Binary.
type Client struct {
	Binary string
}

// New returns a Client for the cmux CLI on PATH, else the one in the app
// bundle.
func New() (*Client, error) {
	if path, err := exec.LookPath("cmux"); err == nil {
		return &Client{Binary: path}, nil
	}
	if _, err := os.Stat(appBinary); err == nil {
		return &Client{Binary: appBinary}, nil
	}
	return nil, ErrNotInstalled
}

// NewWorkspace creates ws in the cmux window of the calling terminal without
// focusing it. cmux only accepts CLI calls from processes started inside
// cmux, so this fails when run from any other terminal.
func (c *Client) NewWorkspace(ws Workspace) error {
	args := []string{
		"new-workspace",
		"--name", ws.Name,
		"--cwd", ws.Dir,
		"--command", ShellJoin(ws.Command),
	}
	var stderr bytes.Buffer
	cmd := exec.Command(c.Binary, args...)
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("cmux new-workspace %s: %w", ws.Name, err)
		}
		return fmt.Errorf("cmux new-workspace %s: %s: %w", ws.Name, msg, err)
	}
	return nil
}

// InsideCmux reports whether the calling process runs in a cmux terminal,
// the only place the cmux socket accepts commands from.
func InsideCmux() bool {
	return os.Getenv("CMUX_SURFACE_ID") != "" || os.Getenv("CMUX_WORKSPACE_ID") != ""
}

// ShellJoin quotes argv as one POSIX shell command line, which is the form
// cmux new-workspace --command takes.
func ShellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// shellQuote leaves words made only of safe characters bare and wraps
// anything else in single quotes.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, shellSafe) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
