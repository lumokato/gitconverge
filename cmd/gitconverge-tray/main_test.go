// 本文件验证托盘状态标题保持简短且明确。
//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lumokato/gitconverge/internal/runlog"
)

func TestEnabledTitle(t *testing.T) {
	if enabledTitle("计划任务", true) != "计划任务：开" || enabledTitle("计划任务", false) != "计划任务：关" {
		t.Fatal("unexpected enabled title")
	}
}

func TestDefaultConfigPathUsesGitConvergeDirectory(t *testing.T) {
	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "config.toml" || filepath.Base(filepath.Dir(path)) != "GitConverge" {
		t.Fatalf("path = %q", path)
	}
}

func TestRunWorkerOnceLogsInvalidConfiguration(t *testing.T) {
	localAppData := filepath.Join(t.TempDir(), "local")
	t.Setenv("LOCALAPPDATA", localAppData)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("not valid toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if exitCode := runWorkerOnce(configPath, filepath.Join(t.TempDir(), "missing.exe")); exitCode != 1 {
		t.Fatalf("exit code = %d", exitCode)
	}
	entries, err := runlog.Read(filepath.Join(localAppData, "GitConverge", runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != "failed" || !strings.Contains(entries[0].Error, "toml") {
		t.Fatalf("entries = %+v", entries)
	}
}
