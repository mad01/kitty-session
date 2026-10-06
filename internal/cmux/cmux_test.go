package cmux

import "testing"

func TestShellJoin(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "plain words stay bare",
			argv: []string{"claude", "--resume", "b75ee90c-9382-4d0a-a09b-99b7fd5f34ef"},
			want: "claude --resume b75ee90c-9382-4d0a-a09b-99b7fd5f34ef",
		},
		{name: "space is quoted", argv: []string{"echo", "a b"}, want: "echo 'a b'"},
		{name: "single quote is escaped", argv: []string{"echo", "it's"}, want: `echo 'it'\''s'`},
		{
			name: "shell metacharacters are quoted",
			argv: []string{"echo", "$HOME;rm"},
			want: "echo '$HOME;rm'",
		},
		{name: "empty argument is kept", argv: []string{"echo", ""}, want: "echo ''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShellJoin(tt.argv); got != tt.want {
				t.Errorf("ShellJoin(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}

func TestInsideCmux(t *testing.T) {
	t.Setenv("CMUX_SURFACE_ID", "")
	t.Setenv("CMUX_WORKSPACE_ID", "")
	if InsideCmux() {
		t.Fatal("InsideCmux() = true with no cmux variables set")
	}
	t.Setenv("CMUX_SURFACE_ID", "surface:1")
	if !InsideCmux() {
		t.Fatal("InsideCmux() = false with CMUX_SURFACE_ID set")
	}
}
