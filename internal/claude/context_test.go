package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{name: "absolute path", dir: "/Users/foo/bar", want: "Users-foo-bar"},
		{name: "nested path", dir: "/home/user/code/project", want: "home-user-code-project"},
		{name: "root", dir: "/", want: ""},
		{name: "no leading slash", dir: "relative/path", want: "relative-path"},
		{name: "single component", dir: "/usr", want: "usr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodePath(tt.dir)
			if got != tt.want {
				t.Errorf("encodePath(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestLatestPromptFromJSON(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "single entry",
			json: `{"version":1,"entries":[{"firstPrompt":"Fix the login bug","modified":"2026-01-16T15:31:47.156Z","isSidechain":false}]}`,
			want: "Fix the login bug",
		},
		{
			name: "picks most recent",
			json: `{"version":1,"entries":[
				{"firstPrompt":"Old prompt","modified":"2026-01-10T10:00:00Z","isSidechain":false},
				{"firstPrompt":"Newest prompt","modified":"2026-01-20T10:00:00Z","isSidechain":false},
				{"firstPrompt":"Middle prompt","modified":"2026-01-15T10:00:00Z","isSidechain":false}
			]}`,
			want: "Newest prompt",
		},
		{
			name: "skips sidechain entries",
			json: `{"version":1,"entries":[
				{"firstPrompt":"Main session","modified":"2026-01-10T10:00:00Z","isSidechain":false},
				{"firstPrompt":"Sidechain newer","modified":"2026-01-20T10:00:00Z","isSidechain":true}
			]}`,
			want: "Main session",
		},
		{
			name: "all sidechain returns empty",
			json: `{"version":1,"entries":[
				{"firstPrompt":"Only sidechain","modified":"2026-01-10T10:00:00Z","isSidechain":true}
			]}`,
			want: "",
		},
		{
			name: "empty entries",
			json: `{"version":1,"entries":[]}`,
			want: "",
		},
		{
			name: "truncates long prompt at 60 chars",
			json: `{"version":1,"entries":[{"firstPrompt":"` + strings.Repeat("a", 80) + `","modified":"2026-01-16T15:00:00Z","isSidechain":false}]}`,
			want: strings.Repeat("a", 60) + "\u2026",
		},
		{
			name: "exactly 60 chars not truncated",
			json: `{"version":1,"entries":[{"firstPrompt":"` + strings.Repeat("b", 60) + `","modified":"2026-01-16T15:00:00Z","isSidechain":false}]}`,
			want: strings.Repeat("b", 60),
		},
		{
			name: "invalid json returns empty",
			json: `not json at all`,
			want: "",
		},
		{
			name: "empty input",
			json: ``,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestPromptFromJSON([]byte(tt.json))
			if got != tt.want {
				t.Errorf("latestPromptFromJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLatestPromptFromFile(t *testing.T) {
	t.Run("reads valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sessions-index.json")
		data := `{"version":1,"entries":[{"firstPrompt":"Hello from file","modified":"2026-01-16T15:00:00Z","isSidechain":false}]}`
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}

		got := latestPromptFromFile(path)
		if got != "Hello from file" {
			t.Errorf("latestPromptFromFile() = %q, want %q", got, "Hello from file")
		}
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		got := latestPromptFromFile("/nonexistent/path/sessions-index.json")
		if got != "" {
			t.Errorf("latestPromptFromFile() = %q, want empty", got)
		}
	})
}
