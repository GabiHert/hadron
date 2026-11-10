package sounds

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
)

// Player plays the sounds with afplay from WAV files it writes once into Dir.
type Player struct {
	Dir string
}

// File returns the path of a sound, writing it first when it is missing.
func (p *Player) File(kind string) (string, error) {
	if !slices.Contains(Kinds, kind) {
		return "", fmt.Errorf("no sound %q: use needs_you or back_to_work", kind)
	}
	path := filepath.Join(p.Dir, kind+".wav")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, WAV(kind), 0o600); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// Play starts afplay and returns at once; the sound plays on its own.
func (p *Player) Play(kind string, volume float64) error {
	path, err := p.File(kind)
	if err != nil {
		return err
	}
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	cmd := exec.Command("/usr/bin/afplay", "-v", strconv.FormatFloat(volume, 'f', 2, 64), path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not play the sound: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
