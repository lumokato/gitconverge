package install

import (
	"strings"
	"testing"
	"time"
)

func TestBuildLinuxServiceUsesWorkerConfigAndHardTimeout(t *testing.T) {
	content, err := buildLinuxService(`/home/test user/.local/bin/gitconverge`, `/home/test user/.config/GitConverge/config.toml`, `/home/test user/.config/GitConverge/environment`, 8*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`ExecStart="/home/test user/.local/bin/gitconverge" run --config "/home/test user/.config/GitConverge/config.toml"`,
		`EnvironmentFile=-/home/test\x20user/.config/GitConverge/environment`,
		"TimeoutStartSec=480s",
		"KillMode=control-group",
		"UMask=0077",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("service is missing %q:\n%s", expected, content)
		}
	}
}

func TestSystemdEnvironmentPathEscapesWordsAndSpecifiers(t *testing.T) {
	got, err := systemdEnvironmentPath(`/home/100%/test user/a'b"c`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `/home/100%%/test\x20user/a\x27b\x22c` {
		t.Fatalf("encoded path = %q", got)
	}
	if _, err := systemdEnvironmentPath("relative/path"); err == nil {
		t.Fatal("relative path was accepted")
	}
	if _, err := systemdEnvironmentPath("/bad\npath"); err == nil {
		t.Fatal("newline was accepted")
	}
}

func TestBuildLinuxTimerUsesCompletionBasedInterval(t *testing.T) {
	content, err := buildLinuxTimer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "OnStartupSec=30s") || !strings.Contains(content, "OnUnitInactiveSec=600s") || !strings.Contains(content, "WantedBy=timers.target") {
		t.Fatalf("unexpected timer:\n%s", content)
	}
}

func TestSystemdQuoteEscapesSpecifiersAndQuotes(t *testing.T) {
	got, err := systemdQuote(`/home/100%/a "quoted" path`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `"/home/100%%/a \"quoted\" path"` {
		t.Fatalf("quoted value = %q", got)
	}
	if _, err := systemdQuote("bad\npath"); err == nil {
		t.Fatal("newline was accepted")
	}
}
