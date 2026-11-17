package notifications

import "os/exec"

// Permission values.
const (
	PermAllowed       = "allowed"
	PermDenied        = "denied"
	PermNotDetermined = "not_determined"
	PermUnavailable   = "unavailable"
)

// Status is what macOS allows Hadron.
type Status struct {
	// Permission is allowed, denied, not_determined (macOS has not asked yet) or unavailable
	// (not running as Hadron.app).
	Permission string `json:"permission"`
	// AlertStyle is none (allowed, but banners and alerts are switched off), banner or alert.
	AlertStyle string `json:"alert_style,omitempty"`
}

// Shows says whether a notification would appear on screen.
func (s Status) Shows() bool { return s.Permission == PermAllowed && s.AlertStyle != "none" }

// BundleID is Hadron's bundle identifier, which the Notifications pane of System Settings uses.
const BundleID = "tech.bhlabs.hadron"

// OpenSystemSettings opens Hadron's page in System Settings, Notifications.
func OpenSystemSettings() error {
	return exec.Command("/usr/bin/open", "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id="+BundleID).Run()
}
