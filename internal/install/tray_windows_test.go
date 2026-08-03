// 本文件验证托盘自启命令完整引用可执行文件、配置和 worker 路径。
//go:build windows

package install

import "testing"

func TestTrayCommandQuotesAllPaths(t *testing.T) {
	got := trayCommand(`C:\Program Files\GitConverge\gitconverge-tray.exe`, `C:\Profile\test\AppData\Roaming\GitConverge\config.toml`, `C:\Program Files\GitConverge\gitconverge.exe`)
	want := `"C:\Program Files\GitConverge\gitconverge-tray.exe" --config "C:\Profile\test\AppData\Roaming\GitConverge\config.toml" --worker "C:\Program Files\GitConverge\gitconverge.exe"`
	if got != want {
		t.Fatalf("command = %q", got)
	}
}
