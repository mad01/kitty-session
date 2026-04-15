package finder

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWalk(t *testing.T) {
	tmp := t.TempDir()

	// Create repo1 with git init and a remote
	repo1 := filepath.Join(tmp, "org1", "repo1")
	if err := os.MkdirAll(repo1, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo1)
	gitSetRemote(t, repo1, "git@github.com:testorg/repo1.git")

	// Create repo2
	repo2 := filepath.Join(tmp, "org2", "repo2")
	if err := os.MkdirAll(repo2, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repo2)
	gitSetRemote(t, repo2, "https://github.com/testorg/repo2.git")

	// Create a non-repo directory (should be skipped)
	nonRepo := filepath.Join(tmp, "not-a-repo")
	if err := os.MkdirAll(nonRepo, 0o755); err != nil {
		t.Fatal(err)
	}

	repos, err := Walk([]string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	byName := make(map[string]Repo)
	for _, r := range repos {
		byName[r.Name] = r
	}

	r1, ok := byName["testorg/repo1"]
	if !ok {
		t.Fatal("expected testorg/repo1 in results")
	}
	if r1.Remote != "git@github.com:testorg/repo1.git" {
		t.Errorf("repo1 Remote = %q, want %q", r1.Remote, "git@github.com:testorg/repo1.git")
	}
	if r1.Host != "github.com" {
		t.Errorf("repo1 Host = %q, want %q", r1.Host, "github.com")
	}

	r2, ok := byName["testorg/repo2"]
	if !ok {
		t.Fatal("expected testorg/repo2 in results")
	}
	if r2.Remote != "https://github.com/testorg/repo2.git" {
		t.Errorf("repo2 Remote = %q, want %q", r2.Remote, "https://github.com/testorg/repo2.git")
	}
	if r2.Host != "github.com" {
		t.Errorf("repo2 Host = %q, want %q", r2.Host, "github.com")
	}
}

func TestWalkSkipsMissingDirs(t *testing.T) {
	repos, err := Walk([]string{"/nonexistent/path/that/does/not/exist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}
}

func TestWalkNoRemoteFallback(t *testing.T) {
	tmp := t.TempDir()

	// Create a repo with no remote — should fallback to dir name
	repoDir := filepath.Join(tmp, "myorg", "myrepo")
	os.MkdirAll(repoDir, 0o755)
	gitInit(t, repoDir)

	repos, err := Walk([]string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0].Name != "myorg/myrepo" {
		t.Errorf("expected fallback name myorg/myrepo, got %s", repos[0].Name)
	}
	if repos[0].Remote != "" {
		t.Errorf("expected empty Remote for no-remote repo, got %q", repos[0].Remote)
	}
	if repos[0].Host != "" {
		t.Errorf("expected empty Host for no-remote repo, got %q", repos[0].Host)
	}
}

func TestReadOriginURL(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "ssh remote",
			content: `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:mad01/dotfiles.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`,
			want: "git@github.com:mad01/dotfiles.git",
		},
		{
			name: "https remote",
			content: `[remote "origin"]
	url = https://github.com/mad01/dropper.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`,
			want: "https://github.com/mad01/dropper.git",
		},
		{
			name: "no origin",
			content: `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:other/repo.git
`,
			want: "",
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
		},
		{
			name: "origin without url",
			content: `[remote "origin"]
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			cfgPath := filepath.Join(tmp, "config")
			os.WriteFile(cfgPath, []byte(tt.content), 0o644)

			got := readOriginURL(cfgPath)
			if got != tt.want {
				t.Errorf("readOriginURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadOriginURLMissingFile(t *testing.T) {
	got := readOriginURL("/nonexistent/config")
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in %s: %v\n%s", dir, err, out)
	}
}

func gitSetRemote(t *testing.T, dir, url string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", "origin", url)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add in %s: %v\n%s", dir, err, out)
	}
}
