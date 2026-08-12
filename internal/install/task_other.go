// 本文件明确尚未支持的平台仍可直接运行核心同步命令。
//go:build !windows && !linux

package install

import (
	"context"
	"fmt"
	"time"

	"github.com/lumokato/gitconverge/internal/execx"
)

const TaskName = "GitConverge"
const TrayExecutableName = ""

type Result struct {
	Executable     string
	TrayExecutable string
	Config         string
	Task           string
	Notice         string
}

func TrayRequired() bool { return false }

func Install(context.Context, string, string, string, time.Duration, time.Duration, execx.Runner) (Result, error) {
	return Result{}, fmt.Errorf("automatic installation is currently implemented only on Windows and Linux")
}
func Uninstall(context.Context, execx.Runner) error {
	return fmt.Errorf("automatic installation is currently implemented only on Windows and Linux")
}
func WorkerInstalled(context.Context, execx.Runner) bool { return false }
func TrayAutostartEnabled() bool                         { return false }
func ManagedInstallation(string, string) bool            { return false }
func Update(context.Context, string, string, string, time.Duration, time.Duration, execx.Runner) error {
	return fmt.Errorf("automatic installation is currently implemented only on Windows and Linux")
}
