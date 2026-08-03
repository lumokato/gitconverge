// 本文件隐藏托盘触发的 Windows worker 与浏览器启动窗口。
//go:build windows

package dashboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/lumokato/gitconverge/internal/execx"
)

func configureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func openURL(value string) error {
	command := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", value)
	configureCommand(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func chooseFolder() (string, error) {
	script := "Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.FolderBrowserDialog; $dialog.Description = '选择 Git 仓库文件夹'; $dialog.ShowNewFolderButton = $true; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($dialog.SelectedPath) }"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result := (execx.Runner{HideWindow: true}).Run(ctx, "", "powershell.exe", "-NoProfile", "-STA", "-Command", script)
	if !result.OK() {
		return "", fmt.Errorf("open folder picker: %s", strings.TrimSpace(result.Stderr))
	}
	path := strings.TrimSpace(result.Stdout)
	if path == "" {
		return "", fmt.Errorf("未选择文件夹")
	}
	return path, nil
}
