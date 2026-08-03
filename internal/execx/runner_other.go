// 本文件为非 Windows 平台提供基础命令执行实现，跨平台进程组清理将在后续里程碑补强。
//go:build !windows

package execx

import "os/exec"

func configureCommand(command *exec.Cmd, hideWindow bool) {}

func attachProcessTree(command *exec.Cmd) (func(), error) {
	return func() {}, nil
}
