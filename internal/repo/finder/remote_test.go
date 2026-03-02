package finder

import "testing"

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "ssh format",
			url:  "git@github.com:mad01/dotfiles.git",
			want: "mad01/dotfiles",
		},
		{
			name: "ssh without .git",
			url:  "git@github.com:mad01/dotfiles",
			want: "mad01/dotfiles",
		},
		{
			name: "https format",
			url:  "https://github.com/mad01/dotfiles.git",
			want: "mad01/dotfiles",
		},
		{
			name: "https without .git",
			url:  "https://github.com/mad01/dotfiles",
			want: "mad01/dotfiles",
		},
		{
			name: "custom host ssh",
			url:  "git@git.example.com:team/service.git",
			want: "team/service",
		},
		{
			name: "custom host https",
			url:  "https://git.example.com/team/service.git",
			want: "team/service",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "whitespace",
			url:  "  git@github.com:org/repo.git  ",
			want: "org/repo",
		},
		{
			name: "deep path https",
			url:  "https://gitlab.com/group/subgroup/project.git",
			want: "subgroup/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRemote(tt.url)
			if got != tt.want {
				t.Errorf("ParseRemote(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
