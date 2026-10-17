//go:build darwin

package install

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLaunchAgentArgumentsAndSchedule(t *testing.T) {
	content, err := buildLaunchAgent(`/Users/test & user/bin/worker`, `/Users/test & user/config.toml`, `/Users/test & user`, 10*time.Minute, 8*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(content))
	for {
		_, err = decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{`<integer>600</integer>`, `<key>RunAtLoad</key><true/>`, `test &amp; user`, `<string>run</string><string>--config</string>`, `GIT_TERMINAL_PROMPT`, `BatchMode=yes`, `StrictHostKeyChecking=yes`, `<key>Umask</key><integer>63</integer>`} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %s in %s", want, content)
		}
	}
	if strings.Contains(content, "KeepAlive") || strings.Contains(content, "sh -c") {
		t.Fatal("must not create a daemon or shell command")
	}
}
func TestLaunchAgentRejectsInvalidPathsAndSchedule(t *testing.T) {
	for _, p := range []string{"relative", "/bad\npath", "/bad\x00path"} {
		if _, err := buildLaunchAgent(p, "/config", "/home", time.Minute, time.Minute); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	for _, interval := range []time.Duration{time.Second, time.Minute + 1, time.Duration(1<<32) * time.Second} {
		if _, err := buildLaunchAgent("/worker", "/config", "/home", interval, time.Minute); err == nil {
			t.Fatalf("accepted %s", interval)
		}
	}
}
func TestDarwinBackupRestoresAndPreservesModes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	existing := filepath.Join(home, "config")
	absent := filepath.Join(home, "worker")
	if err := os.WriteFile(existing, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	backups, root, err := snapshotDarwin(existing, absent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "manifest.txt")); err != nil {
		t.Fatal(err)
	}
	if err = atomicDarwinFile(existing, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = atomicDarwinFile(absent, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = restoreDarwin(backups); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "original" {
		t.Fatal(string(got))
	}
	st, _ := os.Stat(existing)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if _, err = os.Stat(absent); !os.IsNotExist(err) {
		t.Fatal("new file not removed during rollback")
	}
}
func TestDarwinBackupRefusesSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, "target")
	link := filepath.Join(home, "link")
	if err := os.WriteFile(target, []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := snapshotDarwin(link); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestDarwinDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := defaultDarwinPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.config != filepath.Join(home, "Library", "Application Support", "GitConverge", "config.toml") {
		t.Fatal(p.config)
	}
	if p.plist != filepath.Join(home, "Library", "LaunchAgents", TaskName+".plist") {
		t.Fatal(p.plist)
	}
	if !ManagedInstallation("", p.executable) || TrayRequired() {
		t.Fatal("unexpected installation type")
	}
}
