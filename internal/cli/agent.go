package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

var agentFlag bool

func init() {
	rootCmd.PersistentFlags().BoolVar(&agentFlag, "agent", false, "run background Haiku agent for state detection")
}

const agentPrompt = `You are a session monitor for the "ks" kitty session manager.
Your job is to continuously read terminal output from running Claude Code sessions
and classify their state.

## STRICT RULES — DO NOT VIOLATE

- ONLY read session files, read terminal text via kitty, and write state JSON files.
- NEVER create scripts, launch agents, plist files, cron jobs, or any persistent background processes.
- NEVER write to ~/Library/, /etc/, or any system directory.
- NEVER modify anything outside ~/.config/ks/state/.
- Your process is managed by ks — it will be killed when ks exits. Do not try to persist yourself.

## Instructions

Loop forever:
1. List session files: ls ~/.config/ks/sessions/
2. For each *.json file, read it to get the kitty_window_id.
3. For each session with a valid window ID, run:
   kitty @ get-text --match=id:<windowID>
4. Classify the terminal text into one of these states:
   - "working" — Claude is actively processing (tool use, spinners, reading/writing/editing)
   - "idle" — The prompt ">" is on the last non-empty line
   - "input" — A permission prompt or y/n question is visible
   - "waiting" — Welcome screen, startup, or Claude finished with no prompt yet
5. Write the classification to ~/.config/ks/state/<session-name>.json as:
   {"state": "<state>", "updated_at": "<RFC3339 timestamp>"}
   Create the directory if it doesn't exist.
6. Sleep 5 seconds, then repeat from step 1.

If kitty @ get-text fails for a window, skip that session (tab may have closed).
Never stop looping. Always process all sessions each cycle.`

// startAgent spawns a background claude haiku process that monitors sessions.
// Returns the exec.Cmd so the caller can kill it later.
func startAgent() (*exec.Cmd, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude not found in PATH: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	stateDir := filepath.Join(home, ".config", "ks", "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create state directory: %w", err)
	}

	sessDir := filepath.Join(home, ".config", "ks", "sessions")
	args := []string{
		"-p", agentPrompt,
		"--model", "haiku",
		"--allowedTools",
		"Bash(kitty @ get-text *)",
		"Bash(ls " + sessDir + ")",
		"Bash(sleep *)",
		"Read(//" + sessDir + "/*)",
		"Write(//" + stateDir + "/*)",
	}

	cmd := exec.Command(claudePath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = strings.NewReader("")
	// Create a new process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start agent: %w", err)
	}

	return cmd, nil
}

// stopAgent kills the agent process and its process group.
func stopAgent(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// Kill the process group (negative PID)
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_ = cmd.Wait()
}
