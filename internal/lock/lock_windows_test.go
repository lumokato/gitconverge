// 本文件验证 Windows 已存在命名 Mutex 不会被误判为创建失败。
//go:build windows

package lock

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAcquireReportsExistingHeldMutexAsBusy(t *testing.T) {
	name := fmt.Sprintf("GitConvergeTest-%d", time.Now().UnixNano())
	first, acquired, err := Acquire(name)
	if err != nil || !acquired {
		t.Fatalf("first acquire = acquired %v, err %v", acquired, err)
	}
	t.Cleanup(func() { _ = first.Release() })

	command := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	command.Env = append(os.Environ(), "GITCONVERGE_LOCK_HELPER=1", "GITCONVERGE_LOCK_NAME="+name)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper process: %v\n%s", err, output)
	}
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("GITCONVERGE_LOCK_HELPER") != "1" {
		return
	}
	active, err := Active(os.Getenv("GITCONVERGE_LOCK_NAME"))
	if err != nil || !active {
		t.Fatalf("active = %v, err %v", active, err)
	}
}
