// 本文件提供 GitConverge 命令行入口：运行同步、检查配置以及安装或卸载周期任务。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lumokato/gitconverge/internal/app"
	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/dashboard"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/gitx"
	"github.com/lumokato/gitconverge/internal/install"
	"github.com/lumokato/gitconverge/internal/notify"
	"github.com/lumokato/gitconverge/internal/runlog"
	"github.com/lumokato/gitconverge/internal/syncer"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitconverge:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	command := "run"
	if len(arguments) > 0 && arguments[0][0] != '-' {
		command, arguments = arguments[0], arguments[1:]
	}
	switch command {
	case "run":
		return runSync(arguments)
	case "check":
		return checkConfig(arguments)
	case "status":
		return showStatus(arguments)
	case "dashboard":
		return showDashboard(arguments)
	case "install":
		return installTask(arguments)
	case "uninstall":
		return uninstallTask(arguments)
	case "version":
		fmt.Println("GitConverge " + version)
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runSync(arguments []string) error {
	startedAt := time.Now()
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	asJSON := flags.Bool("json", false, "write JSON results")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	configuration, err := config.Load(*configPath)
	if err != nil {
		completedAt := time.Now()
		return errors.Join(err, writeBootstrapFailure(startedAt, completedAt, err))
	}
	notifier, err := buildNotifier(configuration.Notification)
	if err != nil {
		completedAt := time.Now()
		return errors.Join(err, writeRunLog(configuration, startedAt, completedAt, nil, err))
	}
	application := app.Application{Git: gitx.Client{Runner: execx.Runner{}, Timeout: configuration.CommandTimeout.Duration}, Notifier: notifier}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalContext, configuration.RunTimeout.Duration)
	defer cancel()
	results, runErr := application.Run(ctx, configuration)
	completedAt := time.Now()
	writeErr := app.WriteResults(results, os.Stdout, *asJSON)
	var resultErr error
	for _, result := range results {
		if result.Failed() {
			resultErr = fmt.Errorf("one or more repositories require attention")
			break
		}
	}
	operationErr := errors.Join(runErr, writeErr, resultErr)
	logErr := writeRunLog(configuration, startedAt, completedAt, results, operationErr)
	return errors.Join(operationErr, logErr)
}

func writeBootstrapFailure(startedAt, completedAt time.Time, operationErr error) error {
	stateDir, err := config.DefaultStateDir()
	if err != nil {
		return fmt.Errorf("resolve default state directory for run log: %w", err)
	}
	logging := config.DefaultLogging()
	entry := runlog.Entry{
		StartedAt: startedAt, CompletedAt: completedAt, DurationMS: completedAt.Sub(startedAt).Milliseconds(),
		Status: "failed", Error: limitedLogMessage(operationErr.Error()),
	}
	path := filepath.Join(stateDir, runlog.FileName)
	if err := runlog.Append(path, entry, runlog.Policy{MaxAge: logging.MaxAge.Duration, MaxBytes: logging.MaxBytes()}); err != nil {
		return fmt.Errorf("write bootstrap run log: %w", err)
	}
	return nil
}

func writeRunLog(configuration config.Config, startedAt, completedAt time.Time, results []syncer.Result, operationErr error) error {
	if !configuration.Logging.Enabled {
		return nil
	}
	status := "synced"
	repositories := make([]runlog.Repository, 0, len(results))
	for _, result := range results {
		if result.Status != syncer.StatusSynced {
			status = "attention"
		}
		repositories = append(repositories, runlog.Repository{
			ID: result.Repository, Status: string(result.Status), Action: result.Action, Head: result.Head,
			Dirty: result.Dirty, Message: limitedLogMessage(result.Message),
		})
	}
	entry := runlog.Entry{
		StartedAt: startedAt, CompletedAt: completedAt, DurationMS: completedAt.Sub(startedAt).Milliseconds(),
		Status: status, Repositories: repositories,
	}
	if operationErr != nil {
		entry.Status = "failed"
		entry.Error = limitedLogMessage(operationErr.Error())
	}
	path := filepath.Join(configuration.StateDir, runlog.FileName)
	policy := runlog.Policy{MaxAge: configuration.Logging.MaxAge.Duration, MaxBytes: configuration.Logging.MaxBytes()}
	if err := runlog.Append(path, entry, policy); err != nil {
		return fmt.Errorf("write run log: %w", err)
	}
	return nil
}

func limitedLogMessage(message string) string {
	return runlog.LimitMessage(message)
}

func checkConfig(arguments []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	configuration, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("Configuration valid: %d repositories, interval %s, max parallel %d\n", len(configuration.Repositories), configuration.Interval, configuration.MaxParallel)
	for _, repository := range configuration.Repositories {
		fmt.Printf("- %s: %s -> %s/%s\n", repository.ID, repository.Path, repository.Remote, repository.Branch)
	}
	return nil
}

func showStatus(arguments []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	asJSON := flags.Bool("json", false, "write JSON status")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot, err := dashboard.Load(*configPath, dashboard.Runtime{WorkerInstalled: install.WorkerInstalled(ctx, execx.Runner{}), TrayAutostart: install.TrayAutostartEnabled()})
	if err != nil {
		return err
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(snapshot)
	}
	stateText := "idle"
	if snapshot.Runtime.SyncActive {
		stateText = "running"
	}
	fmt.Printf("GitConverge %s: %d repositories, %d healthy, %d need attention, %d dirty\n", stateText, snapshot.Summary.Repositories, snapshot.Summary.Healthy, snapshot.Summary.NeedsAttention, snapshot.Summary.Dirty)
	for _, repository := range snapshot.Repositories {
		statusText := repository.LastStatus
		if statusText == "" {
			statusText = "never"
		}
		actionText := repository.LastAction
		if actionText == "" {
			actionText = "none"
		}
		fmt.Printf("- %-20s %-10s %-22s %s/%s\n", repository.ID, statusText, actionText, repository.Remote, repository.Branch)
	}
	return nil
}

func showDashboard(arguments []string) error {
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	openBrowser := flags.Bool("open", true, "open the dashboard in the default browser")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	path := *configPath
	if path == "" {
		path, err = config.DefaultPath()
		if err != nil {
			return err
		}
	} else {
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
	}
	server, err := dashboard.Start(path, executable)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(ctx)
		cancel()
	}()
	fmt.Println(server.URL)
	if *openBrowser {
		if err := server.Open(); err != nil {
			return err
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	<-stop
	return nil
}

func installTask(arguments []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	configuration, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	trayExecutable := ""
	if install.TrayRequired() {
		trayExecutable = filepath.Join(filepath.Dir(executable), install.TrayExecutableName)
		if _, err := os.Stat(trayExecutable); err != nil {
			return fmt.Errorf("tray executable is unavailable: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := install.Install(ctx, executable, trayExecutable, configuration.SourcePath, configuration.Interval.Duration, configuration.RunTimeout.Duration, execx.Runner{})
	if err != nil {
		return err
	}
	fmt.Printf("Installed executable: %s\n", result.Executable)
	if result.TrayExecutable != "" {
		fmt.Printf("Installed tray: %s\n", result.TrayExecutable)
	}
	fmt.Printf("Installed config: %s\nScheduled task: %s\n", result.Config, result.Task)
	if result.Notice != "" {
		fmt.Printf("Notice: %s\n", result.Notice)
	}
	return nil
}

func uninstallTask(arguments []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return install.Uninstall(ctx, execx.Runner{})
}

func buildNotifier(configuration config.Notification) (notify.Notifier, error) {
	if configuration.Type == "" || configuration.Type == "none" {
		return notify.Noop{}, nil
	}
	if configuration.Type != "dingtalk" {
		return nil, fmt.Errorf("unsupported notification type %q", configuration.Type)
	}
	webhook := configuration.Webhook
	if configuration.WebhookEnv != "" {
		webhook = os.Getenv(configuration.WebhookEnv)
	}
	if webhook == "" {
		return nil, fmt.Errorf("dingtalk webhook is empty")
	}
	secret := configuration.Secret
	if configuration.SecretEnv != "" {
		secret = os.Getenv(configuration.SecretEnv)
	}
	return notify.DingTalk{Webhook: webhook, Secret: secret}, nil
}
