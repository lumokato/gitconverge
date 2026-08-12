//go:build !windows

package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireUsesExclusiveUnixFileLock(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	first, acquired, err := Acquire("GitConvergeTest")
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%v err=%v", acquired, err)
	}
	defer first.Release()

	second, acquired, err := Acquire("GitConvergeTest")
	if err != nil || acquired || second != nil {
		t.Fatalf("second acquire: lock=%v acquired=%v err=%v", second, acquired, err)
	}
}

func TestAcquireRejectsUnsafeUnixLockName(t *testing.T) {
	if _, _, err := Acquire("../shared"); err == nil {
		t.Fatal("unsafe lock name was accepted")
	}
}

func TestAcquireRestrictsExistingUnixLockDirectory(t *testing.T) {
	runtimeRoot := t.TempDir()
	lockRoot := filepath.Join(runtimeRoot, "gitconverge")
	if err := os.Mkdir(lockRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeRoot)

	current, acquired, err := Acquire("GitConvergePermissions")
	if err != nil || !acquired {
		t.Fatalf("acquire: acquired=%v err=%v", acquired, err)
	}
	defer current.Release()
	information, err := os.Stat(lockRoot)
	if err != nil {
		t.Fatal(err)
	}
	if information.Mode().Perm() != 0o700 {
		t.Fatalf("lock directory mode = %o", information.Mode().Perm())
	}
}
