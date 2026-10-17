//go:build darwin

package dashboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func configureCommand(command *exec.Cmd) {}
func openURL(value string) error         { return exec.Command("/usr/bin/open", value).Run() }
func chooseFolder() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", `POSIX path of (choose folder with prompt "选择现有 Git 仓库")`).Output()
	if err != nil {
		return "", fmt.Errorf("未选择文件夹或系统拒绝访问")
	}
	return strings.TrimSpace(string(output)), nil
}
