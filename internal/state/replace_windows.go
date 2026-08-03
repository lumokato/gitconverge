// 本文件在 Windows 上原子替换状态文件，避免 os.Rename 无法覆盖既有目标。
//go:build windows

package state

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func replaceFile(source, target string) error {
	sourcePointer, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
	if err := windows.MoveFileEx(sourcePointer, targetPointer, flags); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return err
	}
	return nil
}
