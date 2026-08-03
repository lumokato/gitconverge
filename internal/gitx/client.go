// 本文件封装系统 Git CLI，并把所有命令限制在仓库级硬超时内。
package gitx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lumokato/gitconverge/internal/execx"
)

type Client struct {
	GitPath string
	Runner  execx.Runner
	Timeout time.Duration
}

type CommandError struct {
	Operation string
	Result    execx.Result
}

func (err CommandError) Error() string {
	detail := strings.TrimSpace(err.Result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(err.Result.Stdout)
	}
	if detail == "" && err.Result.Err != nil {
		detail = err.Result.Err.Error()
	}
	if err.Result.TimedOut {
		return fmt.Sprintf("%s timed out after %s", err.Operation, err.Result.Duration.Round(time.Millisecond))
	}
	return fmt.Sprintf("%s failed with exit code %d: %s", err.Operation, err.Result.ExitCode, detail)
}

func (client Client) Run(ctx context.Context, directory string, operation string, arguments ...string) (string, error) {
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	gitPath := client.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	result := client.Runner.Run(commandContext, directory, gitPath, arguments...)
	if !result.OK() {
		return "", CommandError{Operation: operation, Result: result}
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (client Client) RunAllowFailure(ctx context.Context, directory string, arguments ...string) execx.Result {
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	gitPath := client.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	return client.Runner.Run(commandContext, directory, gitPath, arguments...)
}
