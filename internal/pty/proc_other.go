//go:build !darwin

package pty

// ProcessCwd is not read on this platform.
func ProcessCwd(pid int) string { return "" }

func foreground(pid int) (string, int) { return "", 0 }
