//go:build !darwin

package accessibility

// Read has nothing to read outside macOS.
func Read() Display { return Display{} }
