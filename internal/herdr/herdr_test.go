package herdr

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture is a version 3 session.json in the shape herdr 0.9 writes: one
// workspace per repo, panes keyed by number. Workspace w2 is unnamed, wC
// carries a workspace name, wE a tab name; wE also holds every kind of pane
// the import must skip.
const fixture = `{
  "version": 3,
  "workspaces": [
    {
      "id": "w2",
      "custom_name": null,
      "identity_cwd": "/home/u/code/alpha",
      "tabs": [
        {
          "custom_name": null,
          "layout": {"Pane": 1},
          "panes": {
            "1": {
              "cwd": "/home/u/code/alpha",
              "agent_session": {
                "source": "herdr:claude", "agent": "claude",
                "kind": "id", "value": "b75ee90c-9382-4d0a-a09b-99b7fd5f34ef"
              }
            }
          },
          "zoomed": false, "focused": 1, "root_pane": 1
        }
      ],
      "active_tab": 0
    },
    {
      "id": "wC",
      "custom_name": "Beta Work",
      "identity_cwd": "/home/u/code/beta",
      "tabs": [
        {
          "custom_name": "ignored tab name",
          "layout": {"Pane": 2},
          "panes": {
            "2": {
              "cwd": "/home/u/code/beta",
              "agent_session": {
                "source": "herdr:claude", "agent": "claude",
                "kind": "id", "value": "211b8c21-c5a0-4fa1-864a-8d6c8d72f67c"
              }
            }
          },
          "zoomed": false, "focused": 2, "root_pane": 2
        }
      ],
      "active_tab": 0
    },
    {
      "id": "wE",
      "custom_name": null,
      "identity_cwd": "/home/u/code/gamma",
      "tabs": [
        {
          "custom_name": "gamma tab",
          "layout": {"Pane": 3},
          "panes": {
            "10": {
              "cwd": "/home/u/code/gamma",
              "agent_session": {
                "source": "herdr:claude", "agent": "claude",
                "kind": "id", "value": "0f0f0f0f-0000-4000-8000-000000000010"
              }
            },
            "3": {"cwd": "/home/u/code/gamma"},
            "4": {
              "cwd": "/home/u/code/gamma",
              "agent_session": {
                "source": "herdr:codex", "agent": "codex",
                "kind": "id", "value": "codex-thread-1"
              }
            },
            "5": {
              "cwd": "/home/u/code/gamma",
              "agent_session": {
                "source": "herdr:claude", "agent": "claude",
                "kind": "path", "value": "/home/u/.claude/projects/x/y.jsonl"
              }
            },
            "6": {
              "cwd": "",
              "agent_session": {
                "source": "herdr:claude", "agent": "claude",
                "kind": "id", "value": "0f0f0f0f-0000-4000-8000-000000000006"
              }
            }
          },
          "zoomed": false, "focused": 3, "root_pane": 3
        }
      ],
      "active_tab": 0
    }
  ],
  "active": 0,
  "selected": 0
}`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadParsesVersionThree(t *testing.T) {
	snap, err := Load(writeFixture(t, fixture))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Version != 3 || len(snap.Workspaces) != 3 {
		t.Fatalf("got version %d, %d workspaces", snap.Version, len(snap.Workspaces))
	}
}

func TestLoadRejectsOtherVersions(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"older", `{"version": 2, "workspaces": []}`, "format version 2"},
		{"newer", `{"version": 4, "workspaces": []}`, "format version 4"},
		{"missing", `{"workspaces": []}`, "format version 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFixture(t, tt.content)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), path) {
				t.Errorf("error %q should name %q and the path", err, tt.want)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("missing file: expected an error")
	}
	if _, err := Load(writeFixture(t, `{"version": 3, "workspaces": [`)); err == nil {
		t.Error("truncated JSON: expected an error")
	}
}

func TestAgentsFlattensClaudePanes(t *testing.T) {
	snap, err := Load(writeFixture(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	agents, skipped := snap.Agents()

	wantAgents := []Agent{
		{NameHint: "", Dir: "/home/u/code/alpha", SessionID: "b75ee90c-9382-4d0a-a09b-99b7fd5f34ef"},
		{NameHint: "Beta Work", Dir: "/home/u/code/beta", SessionID: "211b8c21-c5a0-4fa1-864a-8d6c8d72f67c"},
		// Pane 6 has no cwd of its own and takes the workspace's.
		{NameHint: "gamma tab", Dir: "/home/u/code/gamma", SessionID: "0f0f0f0f-0000-4000-8000-000000000006"},
		// Pane 10 sorts after 6 numerically, not before 3 as a string.
		{NameHint: "gamma tab", Dir: "/home/u/code/gamma", SessionID: "0f0f0f0f-0000-4000-8000-000000000010"},
	}
	if !reflect.DeepEqual(agents, wantAgents) {
		t.Errorf("agents:\n got %+v\nwant %+v", agents, wantAgents)
	}

	wantSkipped := []Skip{
		{Dir: "/home/u/code/gamma", Reason: "shell, no agent"},
		{Dir: "/home/u/code/gamma", Reason: "codex is not claude"},
		{Dir: "/home/u/code/gamma", Reason: `claude session kind "path", ks needs an id`},
	}
	if !reflect.DeepEqual(skipped, wantSkipped) {
		t.Errorf("skipped:\n got %+v\nwant %+v", skipped, wantSkipped)
	}
}

func TestAgentsEmptySnapshot(t *testing.T) {
	snap := &Snapshot{Version: Version}
	agents, skipped := snap.Agents()
	if len(agents) != 0 || len(skipped) != 0 {
		t.Errorf("got %d agents, %d skipped from an empty snapshot", len(agents), len(skipped))
	}
}

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "herdr", "session.json"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, err = DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(xdg, "herdr", "session.json"); got != want {
		t.Errorf("with XDG_CONFIG_HOME got %q, want %q", got, want)
	}
}

// shortTempDir returns a temp dir short enough for a unix socket path,
// which t.TempDir on macOS is not always.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ks")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRunning(t *testing.T) {
	dir := shortTempDir(t)
	if Running(dir) {
		t.Error("no socket file: expected not running")
	}

	l, err := net.Listen("unix", filepath.Join(dir, socketName))
	if err != nil {
		t.Fatal(err)
	}
	if !Running(dir) {
		t.Error("listening socket: expected running")
	}

	// Keep the file but stop listening, as a crashed daemon would.
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if Running(dir) {
		t.Error("stale socket file: expected not running")
	}
}
