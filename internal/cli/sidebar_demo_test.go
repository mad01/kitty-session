package cli

import (
	"strconv"
	"testing"

	"github.com/mad01/kitty-session/internal/sidebar"
)

func TestSidebarDemoFlags(t *testing.T) {
	t.Cleanup(func() {
		sidebarDemoWidth = sidebar.DefaultWidth
		sidebarDemoSession = "kitty-session"
	})
	cmd, _, err := rootCmd.Find([]string{"_sidebar-demo"})
	if err != nil || cmd == nil || cmd.Name() != "_sidebar-demo" {
		t.Fatalf("Find(_sidebar-demo) = %v, %v", cmd, err)
	}
	if !cmd.Hidden {
		t.Error("_sidebar-demo should be hidden")
	}
	width := cmd.Flags().Lookup("width")
	if width == nil || width.DefValue != strconv.Itoa(sidebar.DefaultWidth) {
		t.Fatalf("--width default = %v, want %d", width, sidebar.DefaultWidth)
	}
	session := cmd.Flags().Lookup("session")
	if session == nil || session.DefValue != "kitty-session" {
		t.Fatalf("--session default = %v, want kitty-session", session)
	}
	if err := cmd.ParseFlags([]string{"--width", "40", "--session", "demo"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if sidebarDemoWidth != 40 || sidebarDemoSession != "demo" {
		t.Fatalf("flags wired to width=%d session=%q", sidebarDemoWidth, sidebarDemoSession)
	}
	if err := cmd.ValidateArgs([]string{"extra"}); err == nil {
		t.Error("positional arguments should be rejected")
	}
}
