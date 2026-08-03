// 本文件提供轻量 Windows 托盘入口，仅展示状态并触发短命同步 worker。
//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getlantern/systray"
	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/dashboard"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/lock"
	"github.com/lumokato/gitconverge/internal/runlog"
)

var (
	configPath    string
	workerPath    string
	server        *dashboard.Server
	trayLock      *lock.Lock
	statusItem    *systray.MenuItem
	workerItem    *systray.MenuItem
	autostartItem *systray.MenuItem
	runItem       *systray.MenuItem
)

func main() {
	flags := flag.NewFlagSet("gitconverge-tray", flag.ExitOnError)
	configFlag := flags.String("config", "", "configuration file")
	workerFlag := flags.String("worker", "", "GitConverge worker executable")
	runOnce := flags.Bool("run-once", false, "run the worker once without showing a console window")
	_ = flags.Parse(os.Args[1:])
	var err error
	configPath = *configFlag
	if configPath == "" {
		configPath, err = defaultConfigPath()
	} else {
		configPath, err = filepath.Abs(configPath)
	}
	if err != nil {
		return
	}
	workerPath = *workerFlag
	if workerPath == "" {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return
		}
		workerPath = filepath.Join(filepath.Dir(executable), "gitconverge.exe")
	}
	if *runOnce {
		os.Exit(runWorkerOnce(configPath, workerPath))
	}
	var acquired bool
	trayLock, acquired, err = lock.Acquire("GitConvergeTray")
	if err != nil || !acquired {
		return
	}
	server, err = dashboard.Start(configPath, workerPath)
	if err != nil {
		_ = trayLock.Release()
		return
	}
	_ = server.EnsureInstallation()
	systray.Run(onReady, onExit)
}

func runWorkerOnce(configurationPath, executablePath string) int {
	startedAt := time.Now()
	configuration, err := config.Load(configurationPath)
	if err != nil {
		_ = writeBootstrapFailure(startedAt, time.Now(), err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), configuration.RunTimeout.Duration)
	defer cancel()
	result := (execx.Runner{HideWindow: true}).Run(ctx, "", executablePath, "run", "--config", configuration.SourcePath)
	if result.OK() {
		return 0
	}
	if result.TimedOut || result.ExitCode < 0 {
		completedAt := time.Now()
		message := "worker failed to start"
		if result.TimedOut {
			message = fmt.Sprintf("worker timed out after %s", result.Duration.Round(time.Millisecond))
		} else if result.Err != nil {
			message = result.Err.Error()
		}
		entry := runlog.Entry{
			StartedAt: startedAt, CompletedAt: completedAt, DurationMS: completedAt.Sub(startedAt).Milliseconds(),
			Status: "failed", Error: runlog.LimitMessage(message),
		}
		path := filepath.Join(configuration.StateDir, runlog.FileName)
		policy := runlog.Policy{MaxAge: configuration.Logging.MaxAge.Duration, MaxBytes: configuration.Logging.MaxBytes()}
		if configuration.Logging.Enabled {
			_ = runlog.Append(path, entry, policy)
		}
	}
	if result.ExitCode > 0 {
		return result.ExitCode
	}
	return 1
}

func writeBootstrapFailure(startedAt, completedAt time.Time, operationErr error) error {
	stateDir, err := config.DefaultStateDir()
	if err != nil {
		return err
	}
	logging := config.DefaultLogging()
	entry := runlog.Entry{
		StartedAt: startedAt, CompletedAt: completedAt, DurationMS: completedAt.Sub(startedAt).Milliseconds(),
		Status: "failed", Error: runlog.LimitMessage(operationErr.Error()),
	}
	path := filepath.Join(stateDir, runlog.FileName)
	return runlog.Append(path, entry, runlog.Policy{MaxAge: logging.MaxAge.Duration, MaxBytes: logging.MaxBytes()})
}

func defaultConfigPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "GitConverge", "config.toml"), nil
}

func onReady() {
	systray.SetIcon(trayIcon())
	systray.SetTitle("GitConverge")
	systray.SetTooltip("GitConverge")
	statusItem = systray.AddMenuItem("正在读取状态", "打开状态页查看详细信息")
	statusItem.Disable()
	openItem := systray.AddMenuItem("打开 GitConverge", "管理仓库、设置和运行记录")
	runItem = systray.AddMenuItem("立即同步", "启动一次同步")
	systray.AddSeparator()
	workerItem = systray.AddMenuItem("计划任务：检测中", "同步 worker 安装状态")
	workerItem.Disable()
	autostartItem = systray.AddMenuItem("托盘自启：检测中", "托盘登录自启状态")
	autostartItem.Disable()
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出托盘", "仅退出状态托盘，不影响计划任务")
	refresh()
	if snapshot, snapshotErr := server.Snapshot(); snapshotErr == nil && !snapshot.Configured {
		go func() {
			time.Sleep(350 * time.Millisecond)
			setTooltip("打开首次配置", server.Open())
		}()
	}
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-openItem.ClickedCh:
				setTooltip("状态页", server.Open())
			case <-runItem.ClickedCh:
				setTooltip("立即同步", server.RunNow())
				refresh()
			case <-ticker.C:
				refresh()
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func refresh() {
	if server == nil || statusItem == nil {
		return
	}
	snapshot, err := server.Snapshot()
	if err != nil {
		statusItem.SetTitle("状态不可用")
		systray.SetTooltip("GitConverge: " + err.Error())
		if runItem != nil {
			runItem.Disable()
		}
		return
	}
	if runItem != nil {
		if snapshot.Configured && !snapshot.Runtime.SyncActive {
			runItem.Enable()
		} else {
			runItem.Disable()
		}
	}
	if !snapshot.Configured {
		statusItem.SetTitle("等待首次配置")
		systray.SetTooltip("GitConverge: 请完成首次配置")
		workerItem.SetTitle(enabledTitle("计划任务", snapshot.Runtime.WorkerInstalled))
		autostartItem.SetTitle(enabledTitle("托盘自启", snapshot.Runtime.TrayAutostart))
		return
	}
	stateText := "空闲"
	if snapshot.Runtime.SyncActive {
		stateText = "同步中"
		if len(snapshot.Runtime.ActiveRepositories) > 0 {
			stateText += " · " + strings.Join(snapshot.Runtime.ActiveRepositories, ", ")
		}
	}
	statusItem.SetTitle(fmt.Sprintf("%s · %d 个仓库 · %d 项需处理", stateText, snapshot.Summary.Repositories, snapshot.Summary.NeedsAttention))
	workerItem.SetTitle(enabledTitle("计划任务", snapshot.Runtime.WorkerInstalled))
	autostartItem.SetTitle(enabledTitle("托盘自启", snapshot.Runtime.TrayAutostart))
	systray.SetTooltip(fmt.Sprintf("GitConverge: %s，%d 个仓库", stateText, snapshot.Summary.Repositories))
}

func enabledTitle(label string, enabled bool) string {
	if enabled {
		return label + "：开"
	}
	return label + "：关"
}

func setTooltip(action string, err error) {
	if err != nil {
		systray.SetTooltip(action + ": " + err.Error())
		return
	}
	systray.SetTooltip(action + ": 成功")
}

func onExit() {
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(ctx)
		cancel()
	}
	if trayLock != nil {
		_ = trayLock.Release()
	}
}
