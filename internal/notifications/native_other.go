//go:build !darwin

package notifications

import "errors"

// PostNative is only available on macOS.
func PostNative(Note) error { return errors.New("notifications need macOS") }

// NativeStatus is only available on macOS.
func NativeStatus() Status { return Status{Permission: PermUnavailable} }
