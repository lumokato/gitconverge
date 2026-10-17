//go:build darwin

package install

import (
	"context"
	"github.com/lumokato/gitconverge/internal/execx"
	"os"
	"path/filepath"
	"testing"
)

func TestPauseIsPersistentAndPrivate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if SchedulePaused() {
		t.Fatal("unexpected pause")
	}
	if err := Pause(context.Background(), execx.Runner{}); err != nil {
		t.Fatal(err)
	}
	if !SchedulePaused() {
		t.Fatal("pause not persisted")
	}
	p, _ := pausePath()
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("unsafe marker: %v %v", st, err)
	}
	if err := Resume(context.Background(), execx.Runner{}); err == nil {
		t.Fatal("resume accepted without installed worker")
	}
	if !SchedulePaused() {
		t.Fatal("failed resume lost pause")
	}
}
func TestInstallerRefusesActiveSync(t *testing.T) {
	// The global lock is intentionally the exact same name as the sync engine.
	first, err := idleDarwin()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if second, err := idleDarwin(); err == nil {
		second.Release()
		t.Fatal("installer accepted active sync")
	}
}
func TestSnapshotIncludesWorkerPlist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, _ := defaultDarwinPaths()
	if err := os.MkdirAll(filepath.Dir(p.plist), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.plist, []byte("old schedule"), 0600); err != nil {
		t.Fatal(err)
	}
	backups, _, err := snapshotDarwin(p.executable, p.config, p.plist)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicDarwinFile(p.plist, []byte("new schedule"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDarwin(backups); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p.plist)
	if string(data) != "old schedule" {
		t.Fatal(string(data))
	}
}
