//go:build darwin

package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/lock"
)

// A private marker persists a deliberate pause across UI restarts and logins.
// The worker also checks it, so an already-enqueued launch cannot sync after pause.
func pausePath() (string, error) {
	p, err := defaultDarwinPaths()
	return filepath.Join(filepath.Dir(p.config), "paused"), err
}
func SchedulePaused() bool {
	p, err := pausePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
func Pause(ctx context.Context, runner execx.Runner) error {
	p, err := pausePath()
	if err != nil {
		return err
	}
	if err = atomicDarwinFile(p, []byte("Automatic sync paused; manual sync remains available.\n"), 0600); err != nil {
		return err
	}
	// Do not bootout an active worker: it may be finishing a Git operation.
	return nil
}
func Resume(ctx context.Context, runner execx.Runner) error {
	p, err := pausePath()
	if err != nil {
		return err
	}
	paths, err := defaultDarwinPaths()
	if err != nil {
		return err
	}
	if _, err = os.Stat(paths.plist); err != nil {
		return fmt.Errorf("install the worker first: %w", err)
	}
	if !WorkerInstalled(ctx, runner) {
		if err = launchctl(ctx, runner, "bootstrap", darwinDomain(), paths.plist); err != nil {
			return err
		}
	}
	if err = os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Before installer bootout, use the same global lock as all sync invocations.
func idleDarwin() (*lock.Lock, error) {
	l, acquired, err := lock.Acquire("GitConverge")
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("sync is active; retry installation after it completes")
	}
	return l, nil
}

// Return observable scheduler state; never install or resume from UI startup.
func ScheduleStatus(ctx context.Context) map[string]any {
	return map[string]any{"paused": SchedulePaused(), "loaded": WorkerInstalled(ctx, execx.Runner{}), "checkedAt": time.Now()}
}
