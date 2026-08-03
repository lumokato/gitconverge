// 本文件提供带硬超时的外部命令执行器，并把平台相关的子进程清理由独立文件实现。
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type Result struct {
	Name     string
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
	Err      error
}

func (result Result) OK() bool {
	return result.Err == nil && result.ExitCode == 0 && !result.TimedOut
}

type Runner struct {
	HideWindow bool
}

func (runner Runner) Run(ctx context.Context, directory string, name string, arguments ...string) Result {
	started := time.Now()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.WaitDelay = 2 * time.Second
	configureCommand(command, runner.HideWindow)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	result := Result{Name: name, Args: append([]string(nil), arguments...), ExitCode: -1}
	if err := command.Start(); err != nil {
		result.Err = err
		result.Duration = time.Since(started)
		return result
	}
	release, err := attachProcessTree(command)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		result.Err = err
		result.Duration = time.Since(started)
		return result
	}
	defer release()
	err = command.Wait()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Duration = time.Since(started)
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if err != nil && !result.TimedOut {
		result.Err = err
	}
	return result
}
