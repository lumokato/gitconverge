// 本文件在 Unix 平台把 Git 及其 SSH 子进程放入独立进程组，确保超时后完整清理。
//go:build !windows

package execx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCommand(command *exec.Cmd, hideWindow bool) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return killProcessGroup(command.Process.Pid)
	}
}

func attachProcessTree(command *exec.Cmd) (func(), error) {
	return func() {}, nil
}

func killProcessGroup(processID int) error {
	err := syscall.Kill(-processID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
