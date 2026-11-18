//go:build !darwin

package quitguard

// Install does nothing outside macOS: ⌘Q reaches the menu, and BeforeClose asks.
func Install(*Guard) {}

// SystemQuitting is never true outside macOS.
func SystemQuitting() bool { return false }
