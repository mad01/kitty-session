// Package herdr reads the layout file of herdr, another Claude session
// manager, so ks import can turn its running claude agents into ks sessions.
// It parses the file herdr writes, flattens it to the agents ks can host, and
// tells whether a herdr daemon still owns them. It never writes anything.
package herdr

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const (
	// Version is the session.json format this package understands. herdr
	// calls it SNAPSHOT_VERSION; the file carries it as "version".
	Version = 3
	// socketName is the daemon's control socket, beside session.json.
	socketName = "herdr.sock"
	// dialTimeout bounds the Running check. A live daemon answers a unix
	// connect at once; a stale socket file refuses just as fast.
	dialTimeout = 200 * time.Millisecond
	// agentClaude is the agent name herdr records for Claude Code panes.
	agentClaude = "claude"
	// kindID marks a session reference that is a Claude session id, the
	// only kind claude --resume accepts.
	kindID = "id"
)

// Snapshot is the part of herdr's session.json ks reads. The fields mirror
// herdr's SessionSnapshot; layout, focus and numbering are left out.
type Snapshot struct {
	Version    int         `json:"version"`
	Workspaces []Workspace `json:"workspaces"`
}

// Workspace is one herdr workspace: a named group of tabs rooted at a
// directory.
type Workspace struct {
	CustomName  string `json:"custom_name"`
	IdentityCwd string `json:"identity_cwd"`
	Tabs        []Tab  `json:"tabs"`
}

// Tab is one tab of a workspace. Panes are keyed by herdr's pane number,
// serialized as a string.
type Tab struct {
	CustomName string          `json:"custom_name"`
	Panes      map[string]Pane `json:"panes"`
}

// Pane is one terminal pane. A pane without AgentSession is a plain shell.
type Pane struct {
	Cwd          string        `json:"cwd"`
	AgentSession *AgentSession `json:"agent_session"`
}

// AgentSession is what herdr knows about the agent running in a pane: which
// agent, and how to resume it. Kind is "id" or "path".
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// Agent is a claude pane ks can import: the Claude session id herdr would
// pass to claude --resume, the directory it runs in, and the name herdr's
// user gave the workspace or tab, "" when they gave none.
type Agent struct {
	NameHint  string
	Dir       string
	SessionID string
}

// Skip is a pane ks import leaves out, with the reason to show the user.
type Skip struct {
	Dir    string
	Reason string
}

// Load reads and parses the herdr session file at path. A file of another
// format version is rejected with an error that names the version found.
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read herdr session file: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("cannot parse herdr session file %s: %w", path, err)
	}
	if snap.Version != Version {
		return nil, fmt.Errorf(
			"herdr session file %s is format version %d; ks import understands version %d",
			path,
			snap.Version,
			Version,
		)
	}
	return &snap, nil
}

// Agents flattens the snapshot into the claude panes ks can import and the
// panes it skips, both in file order: workspace, tab, then pane number.
func (s *Snapshot) Agents() ([]Agent, []Skip) {
	var agents []Agent
	var skipped []Skip
	for _, ws := range s.Workspaces {
		for _, tab := range ws.Tabs {
			hint := firstNonEmpty(ws.CustomName, tab.CustomName)
			for _, key := range sortedPaneKeys(tab.Panes) {
				pane := tab.Panes[key]
				dir := firstNonEmpty(pane.Cwd, ws.IdentityCwd)
				if reason := skipReason(pane.AgentSession); reason != "" {
					skipped = append(skipped, Skip{Dir: dir, Reason: reason})
					continue
				}
				agents = append(agents, Agent{
					NameHint:  hint,
					Dir:       dir,
					SessionID: pane.AgentSession.Value,
				})
			}
		}
	}
	return agents, skipped
}

// skipReason says why a pane cannot become a ks session, "" when it can.
func skipReason(as *AgentSession) string {
	switch {
	case as == nil:
		return "shell, no agent"
	case as.Agent != agentClaude:
		return as.Agent + " is not claude"
	case as.Kind != kindID:
		return fmt.Sprintf("claude session kind %q, ks needs an id", as.Kind)
	case as.Value == "":
		return "claude session has no id"
	}
	return ""
}

// sortedPaneKeys orders herdr's pane numbers numerically, so the import
// prints the same order every run; the JSON object has none.
func sortedPaneKeys(panes map[string]Pane) []string {
	keys := make([]string, 0, len(panes))
	for k := range panes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, errA := strconv.Atoi(keys[i])
		b, errB := strconv.Atoi(keys[j])
		if errA != nil || errB != nil {
			return keys[i] < keys[j]
		}
		return a < b
	})
	return keys
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// DefaultPath returns the session file of herdr's default session:
// $XDG_CONFIG_HOME/herdr/session.json when that variable is set, else
// ~/.config/herdr/session.json. Named herdr sessions live in
// sessions/<name>/ beneath that directory and are not looked at.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr", "session.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "herdr", "session.json"), nil
}

// Running reports whether a herdr daemon answers on the control socket in
// dir, the directory holding its session.json. A missing socket file, or
// a stale one nothing listens on, means not running.
func Running(dir string) bool {
	conn, err := net.DialTimeout("unix", filepath.Join(dir, socketName), dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close() // the connect succeeding is the answer
	return true
}
