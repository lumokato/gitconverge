// 本文件为非 Windows 平台提供基于文件锁的单实例保护。
//go:build !windows

package lock

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type Lock struct {
	file *os.File
}

func Acquire(name string) (*Lock, bool, error) {
	path := filepath.Join(os.TempDir(), strings.ToLower(name)+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if err == unix.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Lock{file: file}, true, nil
}

func (lock *Lock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
