package kitty

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// lsFixture is a trimmed `kitty @ ls` of an instance with the home tab and
// one session tab (sidebar 2, claude 3) tagged with the session id. Claude
// holds the keyboard focus.
const lsFixture = `[{"id":1,"tabs":[
  {"id":1,"title":"ks","is_active":false,"windows":[
    {"id":1,"title":"ks","columns":158,"lines":39,"is_focused":false,
     "user_vars":{"KS_HOME_AGENT":"1"}}]},
  {"id":2,"title":"demo","is_active":true,"windows":[
    {"id":2,"title":"ks","columns":36,"lines":39,"is_focused":false,
     "user_vars":{"KS_SESSION_ID":"abc"}},
    {"id":3,"title":"✳ claude","columns":119,"lines":39,"is_focused":true,
     "user_vars":{"KS_SESSION_ID":"abc"}}]}
]}]`

// spawned records one Start.
type spawned struct {
	env  []string
	args []string
}

// newFake returns a client whose kitty binary is a recorder answering with
// out, or failing with err. Start is recorded in the returned spawned.
func newFake(out string, err error) (*Client, *[][]string) {
	c, calls, _ := newFakeWithSpawn(out, err)
	return c, calls
}

func newFakeWithSpawn(out string, err error) (*Client, *[][]string, *spawned) {
	var calls [][]string
	var sp spawned
	c := New("unix:/tmp/t.sock")
	c.run = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	c.spawn = func(env []string, args ...string) error {
		sp = spawned{env: env, args: args}
		return err
	}
	return c, &calls, &sp
}

// unsetArgs is what every launch passes to drop the agent-session markers.
var unsetArgs = []string{
	"--env", "CLAUDECODE",
	"--env", "CLAUDE_CODE_CHILD_SESSION",
	"--env", "CLAUDE_CODE_SESSION_ID",
	"--env", "CLAUDE_PID",
	"--env", "CLAUDE_CODE_ENTRYPOINT",
	"--env", "CLAUDE_CODE_SESSION_ATTENDED",
	"--env", "CLAUDE_CODE_MESSAGING_SOCKET",
	"--env", "CLAUDE_CODE_MESSAGING_TOKEN",
	"--env", "CLAUDE_EFFORT",
}

func TestEveryCallTargetsTheSocket(t *testing.T) {
	tests := []struct {
		name string
		call func(c *Client) error
		want []string // the subcommand and its arguments after `@ --to <sock>`
	}{
		{"Ping", func(c *Client) error { return c.Ping() }, []string{"ls"}},
		{
			"GotoLayout matches the tab by window_id",
			func(c *Client) error { return c.GotoLayout(2, LayoutSplits) },
			[]string{"goto-layout", "--match=window_id:2", "splits"},
		},
		{
			"ResizeWindow shrinks",
			func(c *Client) error { return c.ResizeWindow(2, AxisHorizontal, -9) },
			[]string{"resize-window", "--match=id:2", "--axis=horizontal", "--increment=-9"},
		},
		{
			"FocusWindow",
			func(c *Client) error { return c.FocusWindow(3) },
			[]string{"focus-window", "--match=id:3"},
		},
		{
			"SetTabTitleForWindow matches the tab by window_id",
			func(c *Client) error { return c.SetTabTitleForWindow("demo", 2) },
			[]string{"set-tab-title", "--match=window_id:2", "demo"},
		},
		{
			"CloseTab",
			func(c *Client) error { return c.CloseTab(2) },
			[]string{"close-tab", "--match=id:2"},
		},
		{
			"CloseAll",
			func(c *Client) error { return c.CloseAll() },
			[]string{"close-window", "--match=all"},
		},
		{
			"GetText",
			func(c *Client) error { _, err := c.GetText(3); return err },
			[]string{"get-text", "--match=id:3"},
		},
		{
			"LaunchTab",
			func(c *Client) error {
				_, err := c.LaunchTab(Launch{
					Match: 1, Dir: "/work", Env: []string{"A=1", "B=2"},
					Vars: []string{"KS_SESSION_ID=abc"}, Command: []string{"ks", "sidebar"},
					KeepFocus: true,
				})
				return err
			},
			slices.Concat(
				[]string{"launch", "--type=tab", "--match=id:1", "--keep-focus", "--cwd=/work"},
				unsetArgs,
				[]string{
					"--env", "A=1", "--env", "B=2", "--var", "KS_SESSION_ID=abc",
					"--", "ks", "sidebar",
				},
			),
		},
		{
			"LaunchVSplit",
			func(c *Client) error {
				_, err := c.LaunchVSplit(Launch{
					Match: 2, Dir: "/work", Bias: 77, Command: []string{"claude", "--continue"},
				})
				return err
			},
			slices.Concat(
				[]string{
					"launch", "--type=window", "--location=vsplit", "--bias=77",
					"--match=id:2", "--cwd=/work",
				},
				unsetArgs,
				[]string{"--", "claude", "--continue"},
			),
		},
		{
			"LaunchHSplit names the window to split",
			func(c *Client) error {
				_, err := c.LaunchHSplit(Launch{
					Match: 3, Dir: "/work", Bias: 30, Vars: []string{"KS_SESSION_ID=abc"},
				})
				return err
			},
			slices.Concat(
				[]string{
					"launch", "--type=window", "--location=hsplit", "--bias=30",
					"--match=id:3", "--next-to=id:3", "--cwd=/work",
				},
				unsetArgs,
				[]string{"--var", "KS_SESSION_ID=abc", "--"},
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := newFake("7\n", nil)
			if err := tc.call(c); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(*calls) != 1 {
				t.Fatalf("kitty run %d times, want 1", len(*calls))
			}
			got := (*calls)[0]
			prefix := []string{"@", "--to", "unix:/tmp/t.sock"}
			if len(got) < len(prefix) || !slices.Equal(got[:3], prefix) {
				t.Fatalf("args = %q, want prefix %q", got, prefix)
			}
			if !slices.Equal(got[3:], tc.want) {
				t.Errorf("args = %q, want %q", got[3:], tc.want)
			}
		})
	}
}

func TestLaunchParsesTheWindowID(t *testing.T) {
	c, _ := newFake("42\n", nil)
	id, err := c.LaunchTab(Launch{Match: 1, Command: []string{"sh"}})
	if err != nil || id != 42 {
		t.Fatalf("LaunchTab = %d, %v; want 42", id, err)
	}
	c, _ = newFake("not a number", nil)
	if _, err := c.LaunchTab(Launch{Match: 1, Command: []string{"sh"}}); err == nil {
		t.Error("garbage output accepted as a window id")
	}
}

func TestErrorsNameTheSubcommand(t *testing.T) {
	c, _ := newFake("", errors.New("exit status 1: no such window"))
	err := c.FocusWindow(9)
	if err == nil || !strings.Contains(err.Error(), "kitty @ focus-window") ||
		!strings.Contains(err.Error(), "no such window") {
		t.Fatalf("err = %v, want the subcommand and kitty's message", err)
	}
}

func TestStart(t *testing.T) {
	t.Setenv("PATH", "/opt/bin")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/me/.claude")
	t.Setenv("KS_SESSION_NAME", "demo")
	t.Setenv("KITTY_WINDOW_ID", "4")
	t.Setenv("KITTY_CONFIG_DIRECTORY", "/cfg")
	c, calls, sp := newFakeWithSpawn("", nil)
	err := c.Start(StartOptions{
		Overrides: []string{"font_size=13"},
		Command:   []string{"/bin/ks", "sidebar"},
		Title:     "ks",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("Start went through the remote-control protocol: %q", *calls)
	}
	for _, want := range []string{
		"PATH=/opt/bin", "KITTY_CONFIG_DIRECTORY=/cfg", "CLAUDE_CONFIG_DIR=/home/me/.claude",
	} {
		if !slices.Contains(sp.env, want) {
			t.Errorf("instance env lacks %s", want)
		}
	}
	for _, kv := range sp.env {
		name, _, _ := strings.Cut(kv, "=")
		if scrubbed(name) {
			t.Errorf("instance env leaks %s", kv)
		}
	}
	got := sp.args
	want := []string{
		"--detach", "--listen-on", "unix:/tmp/t.sock",
		"-o", "allow_remote_control=yes",
		"-o", "tab_bar_style=hidden",
		"-o", "window_border_width=0",
		"-o", "window_margin_width=0",
		"-o", "window_padding_width=3",
		"-o", "macos_quit_when_last_window_closed=yes",
		"-o", "font_size=13",
		"--title", "ks",
		"--", "/bin/ks", "sidebar",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Start args = %q, want %q", got, want)
	}
	// The package-level lists must not grow with a caller's extras.
	if len(instanceOverrides) != 6 {
		t.Errorf("instance settings mutated: %v", instanceOverrides)
	}
}

func TestScrubEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HOME=/Users/me",
		"CLAUDECODE=1",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_PID=7",
		"CLAUDE_CODE_MESSAGING_TOKEN=secret",
		"KS_SESSION_ID=abc",
		"KS_SESSION_NAME=demo",
		"KITTY_LISTEN_ON=unix:/tmp/k",
		"KITTY_PID=9",
		"KITTY_CONFIG_DIRECTORY=/cfg",
		// Kept: user configuration, not a session marker.
		"CLAUDE_CONFIG_DIR=/home/me/.claude",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"TERM=xterm-kitty",
		"NOEQUALS",
	}
	want := []string{
		"PATH=/usr/bin", "HOME=/Users/me", "KITTY_CONFIG_DIRECTORY=/cfg",
		"CLAUDE_CONFIG_DIR=/home/me/.claude", "CLAUDE_CODE_USE_BEDROCK=1",
		"TERM=xterm-kitty", "NOEQUALS",
	}
	if got := scrubEnv(in); !slices.Equal(got, want) {
		t.Errorf("scrubEnv = %q, want %q", got, want)
	}
}

func TestWindows(t *testing.T) {
	c, _ := newFake(lsFixture, nil)
	windows, err := c.Windows()
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	want := []Window{
		{ID: 1, TabID: 1, TabTitle: "ks", Title: "ks", Columns: 158, HomeAgent: true},
		{
			ID: 2, TabID: 2, TabTitle: "demo", TabActive: true,
			Title: "ks", Columns: 36, SessionID: "abc",
		},
		{
			ID: 3, TabID: 2, TabTitle: "demo", TabActive: true,
			Title: "✳ claude", Columns: 119, SessionID: "abc", Focused: true,
		},
	}
	if !slices.Equal(windows, want) {
		t.Errorf("Windows() = %+v, want %+v", windows, want)
	}
}

func TestSetUserVars(t *testing.T) {
	c, calls := newFake("", nil)
	if err := c.SetUserVars(7, HomeAgentVar+"=1"); err != nil {
		t.Fatalf("SetUserVars: %v", err)
	}
	want := []string{
		"@",
		"--to",
		"unix:/tmp/t.sock",
		"set-user-vars",
		matchID(7),
		"KS_HOME_AGENT=1",
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}
