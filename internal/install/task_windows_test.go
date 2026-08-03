// 本文件验证 Windows 计划任务 XML，不注册或启动真实任务。
//go:build windows

package install

import (
	"encoding/binary"
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestManagedInstallationRequiresInstalledExecutableNames(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	programDir := filepath.Join(localAppData, "Programs", "GitConverge")
	tray := filepath.Join(programDir, TrayExecutableName)
	worker := filepath.Join(programDir, "gitconverge.exe")
	if !ManagedInstallation(tray, worker) {
		t.Fatal("installed tray and worker were not recognized")
	}
	if ManagedInstallation(filepath.Join(programDir, "dashboard.test.exe"), worker) {
		t.Fatal("test executable was recognized as the installed tray")
	}
	if ManagedInstallation(tray, filepath.Join(t.TempDir(), "gitconverge.exe")) {
		t.Fatal("worker outside the installation directory was recognized")
	}
}

func TestCurrentWindowsUserIDIgnoresMisleadingEnvironment(t *testing.T) {
	t.Setenv("USERDOMAIN", "WORKGROUP")
	t.Setenv("USERNAME", "wrong-user")
	userID, err := currentWindowsUserID()
	if err != nil {
		t.Fatal(err)
	}
	if userID == "" || userID == `WORKGROUP\wrong-user` {
		t.Fatalf("user ID = %q", userID)
	}
}

func TestBuildTaskXMLUsesSeparateLogonAndTimeTriggers(t *testing.T) {
	started := time.Date(2026, time.August, 2, 9, 30, 0, 0, time.Local)
	content, err := buildTaskXML(`C:\Program Files\GitConverge\gitconverge-tray.exe`, `C:\Program Files\GitConverge\gitconverge.exe`, `C:\Users\test\AppData\Roaming\GitConverge\config.toml`, `DOMAIN\test`, 10*time.Minute, 8*time.Minute, started)
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeTaskXMLUTF16(t, content)
	parts := strings.SplitN(decoded, "\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("task XML is missing declaration separator")
	}
	var document taskDocument
	if err := xml.Unmarshal([]byte(parts[1]), &document); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(decoded, `<?xml version="1.0" encoding="UTF-16"?>`) {
		t.Fatalf("header = %q", parts[0])
	}
	if !document.Triggers.Logon.Enabled || document.Triggers.Logon.UserID != `DOMAIN\test` {
		t.Fatalf("logon trigger = %+v", document.Triggers.Logon)
	}
	if document.Triggers.Time.Repetition.Interval != "PT10M" || document.Triggers.Time.StartBoundary != started.Format(time.RFC3339) {
		t.Fatalf("time trigger = %+v", document.Triggers.Time)
	}
	if document.Settings.MultipleInstancesPolicy != "IgnoreNew" || document.Settings.ExecutionTimeLimit != "PT8M" {
		t.Fatalf("settings = %+v", document.Settings)
	}
	if document.Actions.Exec.Command != `C:\Program Files\GitConverge\gitconverge-tray.exe` {
		t.Fatalf("command = %q", document.Actions.Exec.Command)
	}
	if document.Actions.Exec.Arguments != `--run-once --config "C:\Users\test\AppData\Roaming\GitConverge\config.toml" --worker "C:\Program Files\GitConverge\gitconverge.exe"` {
		t.Fatalf("arguments = %q", document.Actions.Exec.Arguments)
	}
}

func decodeTaskXMLUTF16(t *testing.T, content []byte) string {
	t.Helper()
	if len(content) < 2 || content[0] != 0xff || content[1] != 0xfe || (len(content)-2)%2 != 0 {
		t.Fatalf("task XML is not UTF-16LE with BOM: %v", content[:min(len(content), 8)])
	}
	values := make([]uint16, (len(content)-2)/2)
	for index := range values {
		values[index] = binary.LittleEndian.Uint16(content[2+index*2:])
	}
	return string(utf16.Decode(values))
}

func TestDurationISO8601PreservesSeconds(t *testing.T) {
	if got := durationISO8601(90 * time.Second); got != "PT1M30S" {
		t.Fatalf("duration = %q", got)
	}
}
