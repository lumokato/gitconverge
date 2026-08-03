// 本文件验证计划任务包装器启动 worker 时使用真正的无窗口进程标志。
//go:build windows

package execx

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigureCommandHidesWindowsConsole(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	configureCommand(command, true)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow || command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("sys proc attributes = %+v", command.SysProcAttr)
	}
}

func TestConfigureCommandLeavesVisibleProcessUnchanged(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	configureCommand(command, false)
	if command.SysProcAttr != nil {
		t.Fatalf("sys proc attributes = %+v", command.SysProcAttr)
	}
}
