package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestConfig creates ~/.config/ks/config.yaml under a fake HOME
// and returns a cleanup function that restores HOME.
func setupTestConfig(t *testing.T, cfgContent string) (home string, cleanup func()) {
	t.Helper()
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".config", "ks")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	return tmp, func() { os.Setenv("HOME", origHome) }
}

func TestRepoListFlag(t *testing.T) {
	// Create temp dir with a git repo
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "org", "myrepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, repoDir)
	gitSetRemote(t, repoDir, "git@github.com:testorg/myrepo.git")

	// Set up config under fake HOME
	_, cleanup := setupTestConfig(t, "dirs:\n  - "+tmp+"\n")
	defer cleanup()

	// Capture output
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"repo", "--list"})

	// Reset flag for test isolation
	repoListFlag = false
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "testorg/myrepo") {
		t.Errorf("expected output to contain testorg/myrepo, got: %s", output)
	}
	if !strings.Contains(output, repoDir) {
		t.Errorf("expected output to contain repo path %s, got: %s", repoDir, output)
	}

	// Reset for other tests
	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	repoListFlag = false
}

func TestRepoListFlagNoConfig(t *testing.T) {
	tmp := t.TempDir()

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"repo", "--list"})
	repoListFlag = false

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no config found")
	}

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	repoListFlag = false
}

func TestRepoListFlagEmptyDirs(t *testing.T) {
	tmp := t.TempDir()
	emptyDir := filepath.Join(tmp, "empty")
	os.MkdirAll(emptyDir, 0o755)

	_, cleanup := setupTestConfig(t, "dirs:\n  - "+emptyDir+"\n")
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"repo", "--list"})
	repoListFlag = false

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no repos found")
	}

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	repoListFlag = false
}

func TestRepoListMultipleRepos(t *testing.T) {
	tmp := t.TempDir()

	// Create multiple repos
	for _, name := range []string{"repo-a", "repo-b", "repo-c"} {
		repoDir := filepath.Join(tmp, name)
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitInit(t, repoDir)
		gitSetRemote(t, repoDir, "git@github.com:org/"+name+".git")
	}

	_, cleanup := setupTestConfig(t, "dirs:\n  - "+tmp+"\n")
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"repo", "--list"})
	repoListFlag = false

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %s", len(lines), output)
	}

	for _, name := range []string{"org/repo-a", "org/repo-b", "org/repo-c"} {
		if !strings.Contains(output, name) {
			t.Errorf("expected output to contain %s", name)
		}
	}

	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	repoListFlag = false
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
