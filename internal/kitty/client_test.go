package kitty

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// lsFixture is a trimmed `kitty @ ls` of an instance with the home tab and
// one session tab (sidebar 2, claude 3) tagged with the session id.
const lsFixture = `[{"id":1,"tabs":[
  {"id":1,"title":"ks","windows":[
    {"id":1,"title":"ks","columns":158,"lines":39,"user_vars":{}}]},
  {"id":2,"title":"demo","windows":[
    {"id":2,"title":"ks","columns":36,"lines":39,"user_vars":{"KS_SESSION_ID":"abc"}},
    {"id":3,"title":"✳ claude","columns":119,"lines":39,"user_vars":{"KS_SESSION_ID":"abc"}}]}
]}]`

// newFake returns a client whose kitty binary is a recorder answering with
// out, or failing with err.
func newFake(out string, err error) (*Client, *[][]string) {
	var calls [][]string
	c := New("unix:/tmp/t.sock")
	c.run = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	return c, &calls
}

func TestEveryCallTargetsTheSocket(t *testing.T) {
	tests := []struct {
		name string
		call func(c *Client) error
		want []string // the subcommand and its arguments after `@ --to <sock>`
	}{
		{"Ping", func(c *Client) error { return c.Ping() }, []string{"ls"}},
		{
			"GotoLayout",
			func(c *Client) error { return c.GotoLayout(2, LayoutSplits) },
			[]string{"goto-layout", "--match=id:2", "splits"},
		},
		{
			"LayoutAction",
			func(c *Client) error { return c.LayoutAction(2, "move_to_screen_edge", "left") },
			[]string{"action", "--match=id:2", "layout_action", "move_to_screen_edge", "left"},
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
			"SetTabTitleForWindow",
			func(c *Client) error { return c.SetTabTitleForWindow("demo", 2) },
			[]string{"set-tab-title", "--match=id:2", "demo"},
		},
		{
			"CloseTab",
			func(c *Client) error { return c.CloseTab(2) },
			[]string{"close-tab", "--match=id:2"},
		},
		{
			"CloseWindow",
			func(c *Client) error { return c.CloseWindow(3) },
			[]string{"close-window", "--match=id:3"},
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
				})
				return err
			},
			[]string{
				"launch", "--type=tab", "--match=id:1", "--cwd=/work",
				"--env", "A=1", "--env", "B=2", "--var", "KS_SESSION_ID=abc",
				"--", "ks", "sidebar",
			},
		},
		{
			"LaunchVSplit",
			func(c *Client) error {
				_, err := c.LaunchVSplit(Launch{
					Match: 2, Dir: "/work", Bias: 77, Command: []string{"claude", "--continue"},
				})
				return err
			},
			[]string{
				"launch", "--type=window", "--location=vsplit", "--bias=77",
				"--match=id:2", "--cwd=/work", "--", "claude", "--continue",
			},
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
	c, calls := newFake("", nil)
	err := c.Start(StartOptions{
		Overrides: []string{"font_size=13"},
		Command:   []string{"/bin/ks", "sidebar"},
		Title:     "ks",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := (*calls)[0]
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
	if slices.Contains(got, "@") {
		t.Error("Start went through the remote-control protocol")
	}
	// The package-level override list must not grow with a caller's extras.
	if len(instanceOverrides) != 6 {
		t.Errorf("instanceOverrides mutated: %v", instanceOverrides)
	}
}

func TestWindows(t *testing.T) {
	c, _ := newFake(lsFixture, nil)
	windows, err := c.Windows()
	if err != nil {
		t.Fatalf("Windows: %v", err)
	}
	want := []Window{
		{ID: 1, TabID: 1, TabTitle: "ks", Title: "ks", Columns: 158},
		{ID: 2, TabID: 2, TabTitle: "demo", Title: "ks", Columns: 36, SessionID: "abc"},
		{ID: 3, TabID: 2, TabTitle: "demo", Title: "✳ claude", Columns: 119, SessionID: "abc"},
	}
	if !slices.Equal(windows, want) {
		t.Errorf("Windows() = %+v, want %+v", windows, want)
	}
}

func TestSnapshotQueries(t *testing.T) {
	c, _ := newFake(lsFixture, nil)
	if id, err := c.AnyWindow(); err != nil || id != 1 {
		t.Errorf("AnyWindow() = %d, %v; want 1", id, err)
	}
	if !c.TabExists(2) || c.TabExists(9) {
		t.Error("TabExists wrong for tab 2 / 9")
	}
	if !c.WindowExists(3) || c.WindowExists(9) {
		t.Error("WindowExists wrong for window 3 / 9")
	}
	if tab, err := c.FindTabForWindow(3); err != nil || tab != 2 {
		t.Errorf("FindTabForWindow(3) = %d, %v; want 2", tab, err)
	}
	if cols, err := c.WindowColumns(2); err != nil || cols != 36 {
		t.Errorf("WindowColumns(2) = %d, %v; want 36", cols, err)
	}
	if title, err := c.WindowTitle(3); err != nil || title != "✳ claude" {
		t.Errorf("WindowTitle(3) = %q, %v", title, err)
	}
	if _, err := c.WindowColumns(9); !errors.Is(err, ErrNotFound) {
		t.Errorf("WindowColumns(9) err = %v, want ErrNotFound", err)
	}

	down, _ := newFake("", errors.New("connection refused"))
	if down.TabExists(1) || down.WindowExists(1) {
		t.Error("an unreachable instance reported live windows")
	}
	if _, err := down.AnyWindow(); err == nil {
		t.Error("AnyWindow on an unreachable instance returned nil error")
	}
}
