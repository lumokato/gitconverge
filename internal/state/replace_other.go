// 本文件在非 Windows 平台使用原子 rename 替换状态文件。
//go:build !windows

package state

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }
