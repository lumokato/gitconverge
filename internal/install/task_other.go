// 本文件明确非 Windows 平台尚未实现安装器，核心同步命令仍可直接运行。
//go:build !windows

package install

import (
	"context"
	"fmt"
	"time"

	"github.com/lumokato/gitconverge/internal/execx"
)

const TaskName = "GitConverge"
const TrayExecutableName = "gitconverge-tray"

type Result struct{ Executable, TrayExecutable, Config, Task string }

func Install(context.Context, string, string, string, time.Duration, time.Duration, execx.Runner) (Result, error) {
	return Result{}, fmt.Errorf("automatic installation is currently implemented only on Windows")
}
func Uninstall(context.Context, execx.Runner) error {
	return fmt.Errorf("automatic installation is currently implemented only on Windows")
}
func WorkerInstalled(context.Context, execx.Runner) bool { return false }
func TrayAutostartEnabled() bool                         { return false }
func ManagedInstallation(string, string) bool            { return false }
func Update(context.Context, string, string, string, time.Duration, time.Duration, execx.Runner) error {
	return fmt.Errorf("automatic installation is currently implemented only on Windows")
}
