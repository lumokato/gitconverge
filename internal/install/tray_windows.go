// 本文件管理 Windows 当前用户的托盘自启项，并在安装后无窗口启动托盘进程。
//go:build windows

package install

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func enableTrayAutostart(trayExecutable, configPath, workerExecutable string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(trayRunValueName, trayCommand(trayExecutable, configPath, workerExecutable))
}

func disableTrayAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	err = key.DeleteValue(trayRunValueName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func TrayAutostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue(trayRunValueName)
	return err == nil && strings.Contains(strings.ToLower(value), strings.ToLower(TrayExecutableName))
}

func startTray(trayExecutable, configPath, workerExecutable string) error {
	command := exec.Command(trayExecutable, "--config", configPath, "--worker", workerExecutable)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func trayCommand(trayExecutable, configPath, workerExecutable string) string {
	return fmt.Sprintf(`"%s" --config "%s" --worker "%s"`, trayExecutable, configPath, workerExecutable)
}
