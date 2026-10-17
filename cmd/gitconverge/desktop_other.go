//go:build !darwin

package main

import "fmt"

func scheduledPause() bool  { return false }
func desktopCommand() error { return fmt.Errorf("native desktop bridge is available on macOS only") }
