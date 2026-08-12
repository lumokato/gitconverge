// 本文件把程序安装到当前用户目录，并注册登录立即运行、之后周期执行的 Windows 计划任务。
//go:build windows

package install

import (
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/lumokato/gitconverge/internal/execx"
	"golang.org/x/sys/windows"
)

const (
	TaskName           = "GitConverge"
	TrayExecutableName = "gitconverge-tray.exe"
	trayRunValueName   = "GitConverge"
)

type Result struct {
	Executable     string
	TrayExecutable string
	Config         string
	Task           string
	Notice         string
}

func TrayRequired() bool { return true }

type taskDocument struct {
	XMLName      xml.Name     `xml:"Task"`
	Version      string       `xml:"version,attr"`
	XMLNS        string       `xml:"xmlns,attr"`
	Registration registration `xml:"RegistrationInfo"`
	Triggers     triggers     `xml:"Triggers"`
	Principals   principals   `xml:"Principals"`
	Settings     taskSettings `xml:"Settings"`
	Actions      taskActions  `xml:"Actions"`
}
type registration struct {
	Description string `xml:"Description"`
}
type triggers struct {
	Logon logonTrigger `xml:"LogonTrigger"`
	Time  timeTrigger  `xml:"TimeTrigger"`
}
type logonTrigger struct {
	Enabled bool   `xml:"Enabled"`
	UserID  string `xml:"UserId"`
}
type timeTrigger struct {
	StartBoundary string     `xml:"StartBoundary"`
	Enabled       bool       `xml:"Enabled"`
	Repetition    repetition `xml:"Repetition"`
}
type repetition struct {
	Interval          string `xml:"Interval"`
	StopAtDurationEnd bool   `xml:"StopAtDurationEnd"`
}
type principals struct {
	Principal principal `xml:"Principal"`
}
type principal struct {
	ID        string `xml:"id,attr"`
	UserID    string `xml:"UserId"`
	LogonType string `xml:"LogonType"`
	RunLevel  string `xml:"RunLevel"`
}
type taskSettings struct {
	MultipleInstancesPolicy   string `xml:"MultipleInstancesPolicy"`
	DisallowStartOnBatteries  bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries    bool   `xml:"StopIfGoingOnBatteries"`
	AllowHardTerminate        bool   `xml:"AllowHardTerminate"`
	StartWhenAvailable        bool   `xml:"StartWhenAvailable"`
	RunOnlyIfNetworkAvailable bool   `xml:"RunOnlyIfNetworkAvailable"`
	ExecutionTimeLimit        string `xml:"ExecutionTimeLimit"`
	Priority                  int    `xml:"Priority"`
}
type taskActions struct {
	Context string      `xml:"Context,attr"`
	Exec    executeTask `xml:"Exec"`
}
type executeTask struct {
	Command   string `xml:"Command"`
	Arguments string `xml:"Arguments"`
}

func Install(ctx context.Context, sourceExecutable, sourceTray, sourceConfig string, interval, runTimeout time.Duration, runner execx.Runner) (Result, error) {
	intervalMinutes := int(interval / time.Minute)
	if intervalMinutes < 1 {
		return Result{}, fmt.Errorf("interval must be at least one minute")
	}
	if runTimeout < time.Minute {
		return Result{}, fmt.Errorf("run timeout must be at least one minute")
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return Result{}, fmt.Errorf("LOCALAPPDATA is not set")
	}
	roamingAppData := os.Getenv("APPDATA")
	if roamingAppData == "" {
		return Result{}, fmt.Errorf("APPDATA is not set")
	}
	programDir := filepath.Join(localAppData, "Programs", "GitConverge")
	configDir := filepath.Join(roamingAppData, "GitConverge")
	installedExecutable := filepath.Join(programDir, "gitconverge.exe")
	installedTray := filepath.Join(programDir, TrayExecutableName)
	installedConfig := filepath.Join(configDir, "config.toml")
	if err := os.MkdirAll(programDir, 0o755); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return Result{}, err
	}
	if !samePath(sourceTray, installedTray) {
		_ = runner.Run(ctx, "", "taskkill.exe", "/F", "/IM", TrayExecutableName)
	}
	if !samePath(sourceExecutable, installedExecutable) {
		if err := copyFile(sourceExecutable, installedExecutable, 0o755); err != nil {
			return Result{}, fmt.Errorf("install executable: %w", err)
		}
	}
	if !samePath(sourceTray, installedTray) {
		if err := copyFile(sourceTray, installedTray, 0o755); err != nil {
			return Result{}, fmt.Errorf("install tray executable: %w", err)
		}
	}
	if !samePath(sourceConfig, installedConfig) {
		if err := copyFile(sourceConfig, installedConfig, 0o600); err != nil {
			return Result{}, fmt.Errorf("install config: %w", err)
		}
	}
	if err := Update(ctx, installedTray, installedExecutable, installedConfig, interval, runTimeout, runner); err != nil {
		return Result{}, err
	}
	_ = runner.Run(ctx, "", "taskkill.exe", "/F", "/IM", TrayExecutableName)
	if err := startTray(installedTray, installedConfig, installedExecutable); err != nil {
		return Result{}, fmt.Errorf("start tray: %w", err)
	}
	result := runner.Run(ctx, "", "schtasks.exe", "/Run", "/TN", TaskName)
	if !result.OK() {
		return Result{}, fmt.Errorf("start scheduled task: %s", commandDetail(result))
	}
	return Result{Executable: installedExecutable, TrayExecutable: installedTray, Config: installedConfig, Task: TaskName}, nil
}

func Update(ctx context.Context, trayExecutable, workerExecutable, configPath string, interval, runTimeout time.Duration, runner execx.Runner) error {
	userID, err := currentWindowsUserID()
	if err != nil {
		return err
	}
	content, err := buildTaskXML(trayExecutable, workerExecutable, configPath, userID, interval, runTimeout, time.Now())
	if err != nil {
		return err
	}
	taskFile, err := os.CreateTemp("", "gitconverge-task-*.xml")
	if err != nil {
		return err
	}
	taskPath := taskFile.Name()
	defer os.Remove(taskPath)
	if _, err := taskFile.Write(content); err != nil {
		taskFile.Close()
		return err
	}
	if err := taskFile.Close(); err != nil {
		return err
	}
	result := runner.Run(ctx, "", "schtasks.exe", "/Create", "/F", "/TN", TaskName, "/XML", taskPath)
	if !result.OK() {
		return fmt.Errorf("create scheduled task: %s", commandDetail(result))
	}
	if err := enableTrayAutostart(trayExecutable, configPath, workerExecutable); err != nil {
		return fmt.Errorf("enable tray autostart: %w", err)
	}
	return nil
}

func currentWindowsUserID() (string, error) {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read current Windows token user: %w", err)
	}
	account, domain, _, err := tokenUser.User.Sid.LookupAccount("")
	if err != nil {
		return "", fmt.Errorf("resolve current Windows token user: %w", err)
	}
	if account == "" {
		return "", fmt.Errorf("current Windows account name is empty")
	}
	if domain == "" {
		return account, nil
	}
	return domain + "\\" + account, nil
}

func Uninstall(ctx context.Context, runner execx.Runner) error {
	result := runner.Run(ctx, "", "schtasks.exe", "/Delete", "/F", "/TN", TaskName)
	if !result.OK() && !strings.Contains(strings.ToLower(result.Stderr+result.Stdout), "cannot find") {
		return fmt.Errorf("delete scheduled task: %s", commandDetail(result))
	}
	if err := disableTrayAutostart(); err != nil {
		return fmt.Errorf("disable tray autostart: %w", err)
	}
	_ = runner.Run(ctx, "", "taskkill.exe", "/F", "/IM", TrayExecutableName)
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return fmt.Errorf("LOCALAPPDATA is not set")
	}
	return os.RemoveAll(filepath.Join(localAppData, "Programs", "GitConverge"))
}

func WorkerInstalled(ctx context.Context, runner execx.Runner) bool {
	result := runner.Run(ctx, "", "schtasks.exe", "/Query", "/TN", TaskName)
	return result.OK()
}

func ManagedInstallation(trayExecutable, workerExecutable string) bool {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return false
	}
	programDir := filepath.Join(localAppData, "Programs", "GitConverge")
	return strings.EqualFold(filepath.Base(trayExecutable), TrayExecutableName) &&
		strings.EqualFold(filepath.Base(workerExecutable), "gitconverge.exe") &&
		samePath(filepath.Dir(trayExecutable), programDir) && samePath(filepath.Dir(workerExecutable), programDir)
}

func buildTaskXML(trayExecutable, workerExecutable, configPath, userID string, interval, runTimeout time.Duration, start time.Time) ([]byte, error) {
	document := taskDocument{
		Version: "1.4", XMLNS: "http://schemas.microsoft.com/windows/2004/02/mit/task",
		Registration: registration{Description: "Run GitConverge at logon and every configured interval."},
		Triggers: triggers{
			Logon: logonTrigger{Enabled: true, UserID: userID},
			Time:  timeTrigger{StartBoundary: start.Format(time.RFC3339), Enabled: true, Repetition: repetition{Interval: durationISO8601(interval)}},
		},
		Principals: principals{Principal: principal{ID: "Author", UserID: userID, LogonType: "InteractiveToken", RunLevel: "LeastPrivilege"}},
		Settings:   taskSettings{MultipleInstancesPolicy: "IgnoreNew", DisallowStartOnBatteries: false, StopIfGoingOnBatteries: false, AllowHardTerminate: true, StartWhenAvailable: true, RunOnlyIfNetworkAvailable: false, ExecutionTimeLimit: durationISO8601(runTimeout), Priority: 7},
		Actions: taskActions{Context: "Author", Exec: executeTask{
			Command:   trayExecutable,
			Arguments: "--run-once --config \"" + configPath + "\" --worker \"" + workerExecutable + "\"",
		}},
	}
	content, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return encodeTaskXMLUTF16(`<?xml version="1.0" encoding="UTF-16"?>` + "\r\n" + string(content)), nil
}

func encodeTaskXMLUTF16(value string) []byte {
	values := utf16.Encode([]rune(value))
	encoded := make([]byte, 2+len(values)*2)
	encoded[0], encoded[1] = 0xff, 0xfe
	for index, current := range values {
		binary.LittleEndian.PutUint16(encoded[2+index*2:], current)
	}
	return encoded
}

func copyFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), "install-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func samePath(left, right string) bool {
	leftPath, _ := filepath.Abs(left)
	rightPath, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftPath), filepath.Clean(rightPath))
}
func durationISO8601(duration time.Duration) string {
	totalSeconds := int64((duration + time.Second - 1) / time.Second)
	hours := totalSeconds / 3600
	minutes := totalSeconds % 3600 / 60
	seconds := totalSeconds % 60
	value := "PT"
	if hours > 0 {
		value += strconv.FormatInt(hours, 10) + "H"
	}
	if minutes > 0 {
		value += strconv.FormatInt(minutes, 10) + "M"
	}
	if seconds > 0 || value == "PT" {
		value += strconv.FormatInt(seconds, 10) + "S"
	}
	return value
}
func commandDetail(result execx.Result) string {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" && result.Err != nil {
		detail = result.Err.Error()
	}
	return detail
}
