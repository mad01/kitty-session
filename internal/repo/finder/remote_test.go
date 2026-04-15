package finder

import "testing"

func TestParseHost(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "ssh github", url: "git@github.com:org/repo.git", want: "github.com"},
		{name: "ssh custom ghe", url: "git@ghe.example.net:team/service.git", want: "ghe.example.net"},
		{name: "ssh user@ custom host", url: "deploy@git.corp.com:infra/admission.git", want: "git.corp.com"},
		{name: "ssh git@ custom host", url: "git@git.corp.com:infra/repo.git", want: "git.corp.com"},
		{name: "https github", url: "https://github.com/org/repo.git", want: "github.com"},
		{name: "https custom ghe", url: "https://ghe.example.net/team/service.git", want: "ghe.example.net"},
		{name: "https custom host", url: "https://git.corp.com/infra/repo", want: "git.corp.com"},
		{name: "empty", url: "", want: ""},
		{name: "whitespace", url: "  git@github.com:org/repo.git  ", want: "github.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseHost(tt.url)
			if got != tt.want {
				t.Errorf("ParseHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

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
