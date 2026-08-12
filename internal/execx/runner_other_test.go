//go:build !windows

package execx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerKillsUnixProcessGroupOnTimeout(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-finished")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	result := (Runner{}).Run(ctx, "", os.Args[0], "-test.run=^TestRunnerProcessGroupHelper$", "--", "parent", marker)
	if !result.TimedOut {
		t.Fatalf("result = %+v", result)
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child survived process-group cleanup: %v", err)
	}
}

func TestRunnerProcessGroupHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+3 {
		return
	}
	mode, marker := os.Args[separator+1], os.Args[separator+2]
	if mode == "child" {
		time.Sleep(750 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("survived\n"), 0o600)
		os.Exit(0)
	}
	if mode != "parent" {
		os.Exit(2)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestRunnerProcessGroupHelper$", "--", "child", marker)
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	time.Sleep(10 * time.Second)
	os.Exit(0)
}
