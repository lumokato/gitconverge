//go:build !windows

package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunNowRejectsPendingBeforeWorkerLock(t *testing.T) {
	root := t.TempDir()
	worker := filepath.Join(root, "worker")
	// No global lock: reproduce the startup gap before a child acquires it.
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nsleep 0.3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "config.toml")
	content := "version = 1\nstate_dir = " + `"` + filepath.Join(root, "state") + `"` + "\n[[repositories]]\nid = \"example\"\npath = " + `"` + root + `"` + "\nremote = \"origin\"\nbranch = \"main\"\n"
	if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := Start(cfg, worker)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(t.Context())
	if err := server.RunNow(); err != nil {
		t.Fatal(err)
	}
	if err := server.RunNow(); err == nil || !strings.Contains(err.Error(), "已在运行") {
		t.Fatalf("duplicate launch: %v", err)
	}
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		server.runMu.Lock()
		pending := server.runPending
		server.runMu.Unlock()
		if !pending {
			return
		}
		select {
		case <-deadline:
			t.Fatal("child was not reaped")
		case <-tick.C:
		}
	}
}
