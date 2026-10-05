package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := map[string]string{
		"Kitty Session":       "kitty-session",
		"feat/sidebar_ui":     "feat-sidebar-ui",
		"--weird--  name--":   "weird-name",
		"ALLCAPS123":          "allcaps123",
		"":                    "",
		"日本語":                 "",
		"release/v1.2.3":      "release-v1-2-3",
		"a---b":               "a-b",
		"trailing.dots...":    "trailing-dots",
		"  spaces  all over ": "spaces-all-over",
	}
	for in, want := range tests {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestName(t *testing.T) {
	if got := SuggestName(""); got != "" {
		t.Errorf("SuggestName(\"\") = %q, want empty", got)
	}

	plain := filepath.Join(t.TempDir(), "My Project")
	mkdir(t, plain)
	if got, want := SuggestName(plain), "my-project"; got != want {
		t.Errorf("outside git: %q, want %q", got, want)
	}

	repo := filepath.Join(t.TempDir(), "kitty-session")
	mkdir(t, repo)
	git(t, repo, "init", "-q", "-b", "feat/Sidebar_UI")
	if got, want := SuggestName(repo), "kitty-session-feat-sidebar-ui"; got != want {
		t.Errorf("inside git: %q, want %q", got, want)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
