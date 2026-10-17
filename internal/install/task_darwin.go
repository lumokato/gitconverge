//go:build darwin

// macOS uses a per-login LaunchAgent and the existing short-lived sync worker.
package install

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/execx"
)

const TaskName = "com.gitconverge.worker"
const TrayExecutableName = ""

type Result struct{ Executable, TrayExecutable, Config, Task, Notice string }
type darwinPaths struct{ executable, config, plist string }

func TrayRequired() bool         { return false }
func TrayAutostartEnabled() bool { return false }
func defaultDarwinPaths() (darwinPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return darwinPaths{}, err
	}
	cfg, err := config.DefaultPath()
	if err != nil {
		return darwinPaths{}, err
	}
	return darwinPaths{filepath.Join(home, ".local", "bin", "gitconverge"), cfg, filepath.Join(home, "Library", "LaunchAgents", TaskName+".plist")}, nil
}
func darwinDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func darwinTarget() string { return darwinDomain() + "/" + TaskName }
func launchctl(ctx context.Context, runner execx.Runner, args ...string) error {
	r := runner.Run(ctx, "", "/bin/launchctl", args...)
	if !r.OK() {
		return fmt.Errorf("launchctl %s: %s %v", strings.Join(args, " "), strings.TrimSpace(r.Stderr), r.Err)
	}
	return nil
}
func WorkerInstalled(ctx context.Context, runner execx.Runner) bool {
	return runner.Run(ctx, "", "/bin/launchctl", "print", darwinTarget()).OK()
}
func ManagedInstallation(_ string, executable string) bool {
	p, err := defaultDarwinPaths()
	if err != nil {
		return false
	}
	absolute, err := filepath.Abs(executable)
	return err == nil && absolute == p.executable
}
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func buildLaunchAgent(executable, cfg, home string, interval, timeout time.Duration) (string, error) {
	if interval < time.Minute || timeout < time.Minute || interval%time.Second != 0 || interval/time.Second > math.MaxInt32 {
		return "", fmt.Errorf("invalid LaunchAgent schedule")
	}
	for _, p := range []string{executable, cfg, home} {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
			return "", fmt.Errorf("LaunchAgent paths must be absolute and contain no control characters")
		}
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>run</string><string>--scheduled</string><string>--config</string><string>%s</string></array>
<key>RunAtLoad</key><true/>
<key>StartInterval</key><integer>%d</integer>
<key>ProcessType</key><string>Background</string>
<key>WorkingDirectory</key><string>%s</string>
<key>EnvironmentVariables</key><dict>
<key>HOME</key><string>%s</string>
<key>PATH</key><string>%s/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
<key>GIT_TERMINAL_PROMPT</key><string>0</string>
<key>GIT_SSH_COMMAND</key><string>/usr/bin/ssh -oBatchMode=yes -oStrictHostKeyChecking=yes</string>
</dict>
<!-- The worker owns bounded runs.jsonl logs and the run/command deadlines. -->
<key>StandardOutPath</key><string>/dev/null</string>
<key>StandardErrorPath</key><string>/dev/null</string>
<key>Umask</key><integer>63</integer>
</dict></plist>
`, TaskName, xmlText(executable), xmlText(cfg), int64(interval/time.Second), xmlText(home), xmlText(home), xmlText(home)), nil
}

// Back up only files this installer owns; never follow a symlink or overwrite a directory.
// Every replacement is atomic and rollback restores the exact previous bytes/mode.
type darwinBackup struct {
	path, saved string
	exists      bool
	mode        os.FileMode
}

func snapshotDarwin(paths ...string) ([]darwinBackup, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	root := filepath.Join(home, "Library", "Application Support", "GitConverge", "backups", time.Now().Format("20060102T150405.000000000"))
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, "", err
	}
	var snapshots []darwinBackup
	for i, p := range paths {
		b := darwinBackup{path: p, saved: filepath.Join(root, strconv.Itoa(i))}
		st, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return nil, root, e
		}
		if e == nil {
			if !st.Mode().IsRegular() {
				return nil, root, fmt.Errorf("refusing to replace non-regular file %s", p)
			}
			b.exists = true
			b.mode = st.Mode().Perm()
			if e = copyDarwinFile(p, b.saved, 0600); e != nil {
				return nil, root, e
			}
		}
		snapshots = append(snapshots, b)
	}
	var manifest strings.Builder
	for _, b := range snapshots {
		fmt.Fprintf(&manifest, "%s\texisted=%v\tmode=%o\tbackup=%s\n", b.path, b.exists, b.mode, b.saved)
	}
	if err = os.WriteFile(filepath.Join(root, "manifest.txt"), []byte(manifest.String()), 0600); err != nil {
		return nil, root, err
	}
	return snapshots, root, nil
}
func restoreDarwin(snapshots []darwinBackup) error {
	var result error
	for _, b := range snapshots {
		if b.exists {
			result = errors.Join(result, copyDarwinFile(b.saved, b.path, b.mode))
		} else if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	}
	return result
}
func atomicDarwinFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "gitconverge-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func copyDarwinFile(source, target string, mode os.FileMode) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return atomicDarwinFile(target, data, mode)
}
func Install(ctx context.Context, executable, _ string, cfg string, interval, timeout time.Duration, runner execx.Runner) (Result, error) {
	p, err := defaultDarwinPaths()
	if err != nil {
		return Result{}, err
	}
	configuration, err := config.Load(cfg)
	if err != nil {
		return Result{}, err
	}
	idle, err := idleDarwin()
	if err != nil {
		return Result{}, err
	}
	defer idle.Release()
	snapshots, backup, err := snapshotDarwin(p.executable, p.config, p.plist)
	if err != nil {
		return Result{}, err
	}
	loaded := WorkerInstalled(ctx, runner)
	if loaded {
		if err = launchctl(ctx, runner, "bootout", darwinTarget()); err != nil {
			return Result{}, err
		}
	}
	rollback := func(e error) (Result, error) {
		restoreErr := restoreDarwin(snapshots)
		if loaded && restoreErr == nil {
			recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			restoreErr = launchctl(recovery, runner, "bootstrap", darwinDomain(), p.plist)
		}
		return Result{}, errors.Join(e, restoreErr)
	}
	if err = copyDarwinFile(executable, p.executable, 0755); err != nil {
		return rollback(err)
	}
	if _, err = config.Save(p.config, configuration); err != nil {
		return rollback(err)
	}
	if err = updateDarwin(ctx, p.executable, p.config, interval, timeout, runner); err != nil {
		return rollback(err)
	}
	return Result{Executable: p.executable, Config: p.config, Task: TaskName, Notice: "Backups: " + backup + "; LaunchAgent runs only while logged in, not during sleep. Missed sleep intervals coalesce on wake; logout unloads it. Logs: configured state_dir/runs.jsonl. Menu bar runs independently of this worker."}, nil
}
func Update(ctx context.Context, _ string, executable, cfg string, interval, timeout time.Duration, runner execx.Runner) error {
	idle, err := idleDarwin()
	if err != nil {
		return err
	}
	defer idle.Release()
	return updateDarwin(ctx, executable, cfg, interval, timeout, runner)
}
func updateDarwin(ctx context.Context, executable, cfg string, interval, timeout time.Duration, runner execx.Runner) error {
	p, err := defaultDarwinPaths()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	cfg, err = filepath.Abs(cfg)
	if err != nil {
		return err
	}
	content, err := buildLaunchAgent(executable, cfg, home, interval, timeout)
	if err != nil {
		return err
	}
	snapshots, _, err := snapshotDarwin(p.plist)
	if err != nil {
		return err
	}
	loaded := WorkerInstalled(ctx, runner)
	if loaded {
		if err = launchctl(ctx, runner, "bootout", darwinTarget()); err != nil {
			return err
		}
	}
	rollback := func(e error) error {
		// Use a fresh bounded context for recovery if the caller timed out.
		recovery, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = runner.Run(recovery, "", "/bin/launchctl", "bootout", darwinTarget())
		restoreErr := restoreDarwin(snapshots)
		if loaded && restoreErr == nil {
			restoreErr = launchctl(recovery, runner, "bootstrap", darwinDomain(), p.plist)
		}
		return errors.Join(e, restoreErr)
	}
	if err = atomicDarwinFile(p.plist, []byte(content), 0600); err != nil {
		return rollback(err)
	}
	if err = launchctl(ctx, runner, "bootstrap", darwinDomain(), p.plist); err != nil {
		return rollback(err)
	}
	return nil
}
func Uninstall(ctx context.Context, runner execx.Runner) error {
	idle, err := idleDarwin()
	if err != nil {
		return err
	}
	defer idle.Release()
	p, err := defaultDarwinPaths()
	if err != nil {
		return err
	}
	if WorkerInstalled(ctx, runner) {
		if err = launchctl(ctx, runner, "bootout", darwinTarget()); err != nil {
			return err
		}
	}
	if _, _, err = snapshotDarwin(p.plist, p.executable); err != nil {
		return err
	}
	for _, path := range []string{p.plist, p.executable} {
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil // Deliberately preserve config, logs, backups and every managed repository.
}
