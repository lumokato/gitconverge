// 本文件把 Linux worker 安装到当前用户目录，并通过 systemd user timer 周期运行。
//go:build linux

package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/execx"
)

const (
	TaskName           = "gitconverge.timer"
	TrayExecutableName = ""
	serviceName        = "gitconverge.service"
)

type Result struct {
	Executable     string
	TrayExecutable string
	Config         string
	Task           string
	Notice         string
}

type linuxInstallPaths struct {
	executable  string
	config      string
	environment string
	service     string
	timer       string
}

func TrayRequired() bool { return false }

func Install(ctx context.Context, sourceExecutable, _ string, sourceConfig string, interval, runTimeout time.Duration, runner execx.Runner) (Result, error) {
	paths, err := defaultLinuxInstallPaths()
	if err != nil {
		return Result{}, err
	}
	if err := validateLinuxSchedule(interval, runTimeout); err != nil {
		return Result{}, err
	}
	if err := requireUserSystemd(ctx, runner); err != nil {
		return Result{}, err
	}
	configuration, err := config.Load(sourceConfig)
	if err != nil {
		return Result{}, fmt.Errorf("load source configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.executable), 0o755); err != nil {
		return Result{}, err
	}
	if !sameLinuxPath(sourceExecutable, paths.executable) {
		if err := copyLinuxFile(sourceExecutable, paths.executable, 0o755); err != nil {
			return Result{}, fmt.Errorf("install executable: %w", err)
		}
	}
	installedConfiguration, err := config.Save(paths.config, configuration)
	if err != nil {
		return Result{}, fmt.Errorf("install configuration: %w", err)
	}
	if err := Update(ctx, "", paths.executable, installedConfiguration.SourcePath, interval, runTimeout, runner); err != nil {
		return Result{}, err
	}
	return Result{
		Executable: paths.executable,
		Config:     installedConfiguration.SourcePath,
		Task:       TaskName,
		Notice:     linuxLingerNotice(ctx, runner),
	}, nil
}

func Update(ctx context.Context, _ string, workerExecutable, configPath string, interval, runTimeout time.Duration, runner execx.Runner) error {
	if err := validateLinuxSchedule(interval, runTimeout); err != nil {
		return err
	}
	if err := requireUserSystemd(ctx, runner); err != nil {
		return err
	}
	workerExecutable, err := filepath.Abs(workerExecutable)
	if err != nil {
		return err
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return err
	}
	paths, err := defaultLinuxInstallPaths()
	if err != nil {
		return err
	}
	service, err := buildLinuxService(workerExecutable, configPath, paths.environment, runTimeout)
	if err != nil {
		return err
	}
	timer, err := buildLinuxTimer(interval)
	if err != nil {
		return err
	}
	if err := writeLinuxFileAtomic(paths.service, []byte(service), 0o644); err != nil {
		return fmt.Errorf("write systemd service: %w", err)
	}
	if err := writeLinuxFileAtomic(paths.timer, []byte(timer), 0o644); err != nil {
		return fmt.Errorf("write systemd timer: %w", err)
	}
	reload := runner.Run(ctx, "", "systemctl", "--user", "daemon-reload")
	if !reload.OK() {
		return fmt.Errorf("reload systemd user units: %s", linuxCommandDetail(reload))
	}
	enable := runner.Run(ctx, "", "systemctl", "--user", "enable", "--now", TaskName)
	if !enable.OK() {
		return fmt.Errorf("enable systemd timer: %s", linuxCommandDetail(enable))
	}
	return nil
}

func Uninstall(ctx context.Context, runner execx.Runner) error {
	paths, err := defaultLinuxInstallPaths()
	if err != nil {
		return err
	}
	if err := requireUserSystemd(ctx, runner); err != nil {
		return err
	}
	disable := runner.Run(ctx, "", "systemctl", "--user", "disable", "--now", TaskName)
	if !disable.OK() && linuxUnitExists(paths) {
		return fmt.Errorf("disable systemd timer: %s", linuxCommandDetail(disable))
	}
	stop := runner.Run(ctx, "", "systemctl", "--user", "stop", serviceName)
	if !stop.OK() && linuxUnitExists(paths) {
		return fmt.Errorf("stop systemd service: %s", linuxCommandDetail(stop))
	}
	var removeErr error
	for _, path := range []string{paths.timer, paths.service, paths.executable} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			removeErr = errors.Join(removeErr, err)
		}
	}
	if removeErr != nil {
		return removeErr
	}
	reload := runner.Run(ctx, "", "systemctl", "--user", "daemon-reload")
	if !reload.OK() {
		return fmt.Errorf("reload systemd user units: %s", linuxCommandDetail(reload))
	}
	_ = runner.Run(ctx, "", "systemctl", "--user", "reset-failed", serviceName)
	return nil
}

func WorkerInstalled(ctx context.Context, runner execx.Runner) bool {
	result := runner.Run(ctx, "", "systemctl", "--user", "is-enabled", "--quiet", TaskName)
	return result.OK()
}

func TrayAutostartEnabled() bool { return false }

func ManagedInstallation(_ string, workerExecutable string) bool {
	paths, err := defaultLinuxInstallPaths()
	return err == nil && sameLinuxPath(workerExecutable, paths.executable)
}

func defaultLinuxInstallPaths() (linuxInstallPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return linuxInstallPaths{}, err
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return linuxInstallPaths{}, err
	}
	configPath, err := config.DefaultPath()
	if err != nil {
		return linuxInstallPaths{}, err
	}
	unitRoot := filepath.Join(configRoot, "systemd", "user")
	return linuxInstallPaths{
		executable:  filepath.Join(home, ".local", "bin", "gitconverge"),
		config:      configPath,
		environment: filepath.Join(filepath.Dir(configPath), "environment"),
		service:     filepath.Join(unitRoot, serviceName),
		timer:       filepath.Join(unitRoot, TaskName),
	}, nil
}

func requireUserSystemd(ctx context.Context, runner execx.Runner) error {
	result := runner.Run(ctx, "", "systemctl", "--user", "show-environment")
	if !result.OK() {
		return fmt.Errorf("systemd user manager is unavailable: %s", linuxCommandDetail(result))
	}
	return nil
}

func linuxLingerNotice(ctx context.Context, runner execx.Runner) string {
	result := runner.Run(ctx, "", "loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger", "--value")
	if result.OK() && strings.EqualFold(strings.TrimSpace(result.Stdout), "yes") {
		return ""
	}
	return `user lingering is not confirmed; for unattended runs after logout, enable it once with: sudo loginctl enable-linger "$USER"`
}

func linuxUnitExists(paths linuxInstallPaths) bool {
	for _, path := range []string{paths.service, paths.timer} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func copyLinuxFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
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
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func writeLinuxFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "unit-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func sameLinuxPath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func linuxCommandDetail(result execx.Result) string {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" && result.Err != nil {
		detail = result.Err.Error()
	}
	if detail == "" {
		detail = "command failed without output"
	}
	return detail
}
