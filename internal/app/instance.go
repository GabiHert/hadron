package app

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// errNotPrimary says another Hadron already holds the data folder.
var errNotPrimary = errors.New("another Hadron is running on this data folder")

// lockPrimary takes the exclusive lock that makes this process the one Hadron for a data folder.
// It is held until the process exits (the returned file stays open). Only the process that holds
// it may open the database, migrate it or reconcile sessions: a second launch of the binary,
// such as the one `wails build` makes, would otherwise rewrite the running app's rows.
func lockPrimary(home string) (*os.File, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, "primary.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errNotPrimary
		}
		return nil, err
	}
	return f, nil
}
