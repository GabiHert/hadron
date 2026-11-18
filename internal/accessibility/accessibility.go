// Package accessibility reads the macOS display settings the window follows.
package accessibility

// Display is what macOS asks apps to do with the display.
type Display struct {
	// ReduceTransparency is System Settings, Accessibility, Display, Reduce transparency.
	ReduceTransparency bool `json:"reduce_transparency"`
}
