// 本文件通过临时真实 Git 仓库验证 check 与 run 命令的完整入口。
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckAndSyncTemporaryRepository(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	local := filepath.Join(root, "local")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "clone", remote, local)
	runGit(t, local, "config", "user.name", "GitConverge CLI Test")
	runGit(t, local, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(local, "README.md"), []byte("cli acceptance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "add", "README.md")
	runGit(t, local, "commit", "-m", "docs: add cli acceptance")
	runGit(t, local, "branch", "-M", "main")

	configPath := filepath.Join(root, "config.toml")
	content := strings.Join([]string{
		"version = 1",
		"interval = \"10m\"",
		"run_timeout = \"2m\"",
		"command_timeout = \"30s\"",
		"max_parallel = 1",
		"state_dir = \"" + tomlPath(filepath.Join(root, "state")) + "\"",
		"notification_cooldown = \"1h\"",
		"",
		"[[repositories]]",
		"id = \"cli-e2e\"",
		"path = \"" + tomlPath(local) + "\"",
		"remote = \"origin\"",
		"branch = \"main\"",
		"divergence = \"notify\"",
		"fetch_interval = \"10m\"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "--config", configPath}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"run", "--config", configPath, "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"status", "--config", configPath, "--json"}); err != nil {
		t.Fatal(err)
	}
	logContent, err := os.ReadFile(filepath.Join(root, "state", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logContent), `"repositories"`) {
		t.Fatalf("run log missing repository results: %s", logContent)
	}
	localHead := runGit(t, local, "rev-parse", "HEAD")
	remoteHead := runGit(t, remote, "rev-parse", "refs/heads/main")
	if localHead != remoteHead {
		t.Fatalf("remote head %s != local head %s", remoteHead, localHead)
	}
}

func TestRunLogsInvalidConfiguration(t *testing.T) {
	localAppData := filepath.Join(t.TempDir(), "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("not valid toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"run", "--config", configPath}); err == nil {
		t.Fatal("expected invalid configuration error")
	}
	content, err := os.ReadFile(filepath.Join(localAppData, "GitConverge", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"status":"failed"`) {
		t.Fatalf("bootstrap failure was not logged: %s", content)
	}
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func tomlPath(path string) string {
	return strings.ReplaceAll(path, "\\", "\\\\")
}
