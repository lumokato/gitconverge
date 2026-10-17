//go:build darwin

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/dashboard"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/install"
	"github.com/lumokato/gitconverge/internal/lock"
)

func scheduledPause() bool { return install.SchedulePaused() }
func desktopCommand() error {
	l, ok, err := lock.Acquire("GitConvergeDesktop")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("menu bar is already running")
	}
	defer l.Release()
	cfg, err := config.DefaultPath()
	if err != nil {
		return err
	}
	worker, err := os.Executable()
	if err != nil {
		return err
	}
	// Installed desktop controls the canonical worker; a development preview
	// remains unmanaged and cannot silently rewrite the installed schedule.
	home, _ := os.UserHomeDir()
	appWorker := filepath.Join(home, "Applications", "GitConverge.app", "Contents", "MacOS", "gitconverge")
	if worker == appWorker {
		worker = filepath.Join(home, ".local", "bin", "gitconverge")
	}
	server, err := dashboard.Start(cfg, worker)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			Action string `json:"action"`
		}
		response := map[string]any{"version": version}
		err = json.Unmarshal(scanner.Bytes(), &request)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			switch request.Action {
			case "status":
				var snapshot dashboard.Snapshot
				snapshot, err = server.Snapshot()
				if err == nil {
					response["snapshot"] = snapshot
				}
				response["schedule"] = install.ScheduleStatus(ctx)
				if logs, logErr := dashboard.LoadLogs(cfg, 1); logErr == nil && len(logs.Entries) > 0 {
					response["lastRun"] = logs.Entries[0]
				} else if logErr != nil {
					err = errors.Join(err, logErr)
				}
			case "open":
				response["url"] = server.URL
			case "logs":
				response["url"] = server.URL + "#logs"
			case "run":
				err = server.RunNow()
			case "pause":
				err = install.Pause(ctx, execx.Runner{})
			case "resume":
				err = install.Resume(ctx, execx.Runner{})
			default:
				err = fmt.Errorf("unknown desktop action")
			}
			cancel()
		}
		if err != nil {
			response["error"] = err.Error()
		}
		if err = encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
