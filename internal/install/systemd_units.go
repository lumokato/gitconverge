// 本文件生成可在各平台测试的 Linux systemd user service 与 timer 内容。
package install

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func validateLinuxSchedule(interval, runTimeout time.Duration) error {
	if interval < time.Minute {
		return fmt.Errorf("interval must be at least one minute")
	}
	if runTimeout < time.Minute {
		return fmt.Errorf("run timeout must be at least one minute")
	}
	return nil
}

func buildLinuxService(workerExecutable, configPath, environmentPath string, runTimeout time.Duration) (string, error) {
	worker, err := systemdQuote(workerExecutable)
	if err != nil {
		return "", fmt.Errorf("encode worker path: %w", err)
	}
	configuration, err := systemdQuote(configPath)
	if err != nil {
		return "", fmt.Errorf("encode config path: %w", err)
	}
	environment, err := systemdEnvironmentPath(environmentPath)
	if err != nil {
		return "", fmt.Errorf("encode environment path: %w", err)
	}
	timeoutSeconds := int64((runTimeout + time.Second - 1) / time.Second)
	return strings.Join([]string{
		"[Unit]",
		"Description=Converge configured Git repositories",
		"Wants=network-online.target",
		"After=network-online.target",
		"",
		"[Service]",
		"Type=oneshot",
		"Environment=GIT_TERMINAL_PROMPT=0",
		"Environment=GCM_INTERACTIVE=Never",
		"EnvironmentFile=-" + environment,
		"ExecStart=" + worker + " run --config " + configuration,
		"TimeoutStartSec=" + strconv.FormatInt(timeoutSeconds, 10) + "s",
		"TimeoutStopSec=5s",
		"KillMode=control-group",
		"UMask=0077",
		"NoNewPrivileges=true",
		"",
	}, "\n"), nil
}

func systemdEnvironmentPath(value string) (string, error) {
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	const hexadecimal = "0123456789abcdef"
	var encoded strings.Builder
	for index := 0; index < len(value); index++ {
		current := value[index]
		switch {
		case current == 0 || current == '\r' || current == '\n':
			return "", fmt.Errorf("value contains a forbidden control character")
		case current == '%':
			encoded.WriteString("%%")
		case current <= ' ' || current == 0x7f || current == '\\' || current == '"' || current == '\'':
			encoded.WriteString(`\x`)
			encoded.WriteByte(hexadecimal[current>>4])
			encoded.WriteByte(hexadecimal[current&0x0f])
		default:
			encoded.WriteByte(current)
		}
	}
	return encoded.String(), nil
}

func buildLinuxTimer(interval time.Duration) (string, error) {
	if interval < time.Minute {
		return "", fmt.Errorf("interval must be at least one minute")
	}
	intervalSeconds := int64((interval + time.Second - 1) / time.Second)
	return strings.Join([]string{
		"[Unit]",
		"Description=Run GitConverge periodically",
		"",
		"[Timer]",
		"OnStartupSec=30s",
		"OnUnitInactiveSec=" + strconv.FormatInt(intervalSeconds, 10) + "s",
		"AccuracySec=10s",
		"Unit=gitconverge.service",
		"",
		"[Install]",
		"WantedBy=timers.target",
		"",
	}, "\n"), nil
}

func systemdQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("value contains a forbidden control character")
	}
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "%", "%%")
	return "\"" + value + "\"", nil
}
