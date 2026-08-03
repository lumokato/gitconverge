// 本文件为非 Windows 开发测试提供基础浏览器打开能力。
//go:build !windows

package dashboard

import (
	"fmt"
	"os/exec"
	"runtime"
)

func configureCommand(command *exec.Cmd) {}

func openURL(value string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, value).Start()
}

func chooseFolder() (string, error) {
	return "", fmt.Errorf("folder picker is available only on Windows")
}
