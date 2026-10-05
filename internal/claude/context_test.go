package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncodePath(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{name: "absolute path", dir: "/Users/foo/bar", want: "-Users-foo-bar"},
		{name: "nested path", dir: "/home/user/code/project", want: "-home-user-code-project"},
		{name: "root", dir: "/", want: "-"},
		{name: "no leading slash", dir: "relative/path", want: "relative-path"},
		{name: "single component", dir: "/usr", want: "-usr"},
		{
			name: "dot and underscore",
			dir:  "/Users/u/.config/ks_tmp",
			want: "-Users-u--config-ks-tmp",
		},
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

func TestTranscriptPath(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	got, err := TranscriptPath("/work/demo", "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/u/.claude/projects/-work-demo/abc-123.jsonl"; got != want {
		t.Errorf("TranscriptPath = %q, want %q", got, want)
	}
}

func TestHasTranscripts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, ".claude", "projects", "-work-demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if HasTranscripts("/work/demo") {
		t.Error("empty project dir reported transcripts")
	}
	if err := os.WriteFile(filepath.Join(project, "sessions-index.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasTranscripts("/work/demo") {
		t.Error("an index without transcripts counted")
	}
	if err := os.WriteFile(filepath.Join(project, "abc.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasTranscripts("/work/demo") {
		t.Error("a transcript was not found")
	}
	if HasTranscripts("/work/never") {
		t.Error("a dir without a project dir reported transcripts")
	}
}
