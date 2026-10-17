//go:build darwin

package dashboard

import (
	"context"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/install"
	"time"
)

func refreshRuntime(runtime Runtime) Runtime {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runtime.WorkerInstalled = install.WorkerInstalled(ctx, execx.Runner{})
	runtime.AutomaticPaused = install.SchedulePaused()
	return runtime
}
