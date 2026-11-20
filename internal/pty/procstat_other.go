//go:build !darwin

package pty

import "time"

// Descendants is not read on this platform.
func Descendants(pid int) []int { return nil }

// ProcessCPU is not read on this platform.
func ProcessCPU(pid int) time.Duration { return -1 }

// MemoryPressure is not read on this platform.
func MemoryPressure() int { return 0 }
