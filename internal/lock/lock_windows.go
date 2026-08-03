// 本文件在 Windows 上使用命名 Mutex，防止登录任务和周期任务同时运行。
//go:build windows

package lock

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

type Lock struct {
	handle windows.Handle
}

func Acquire(name string) (*Lock, bool, error) {
	pointer, err := windows.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateMutex(nil, false, pointer)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create mutex: %w", err)
	}
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, false, fmt.Errorf("wait for mutex: %w", err)
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		windows.CloseHandle(handle)
		return nil, false, nil
	}
	if result != uint32(windows.WAIT_OBJECT_0) && result != uint32(windows.WAIT_ABANDONED) {
		windows.CloseHandle(handle)
		return nil, false, errors.New("unexpected mutex wait result")
	}
	return &Lock{handle: handle}, true, nil
}

func (lock *Lock) Release() error {
	if lock == nil || lock.handle == 0 {
		return nil
	}
	releaseErr := windows.ReleaseMutex(lock.handle)
	closeErr := windows.CloseHandle(lock.handle)
	lock.handle = 0
	return errors.Join(releaseErr, closeErr)
}
