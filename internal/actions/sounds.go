package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sounds"
	"github.com/GabiHert/hadron/internal/store"
)

// Sound settings.
const (
	// SettingSoundsEnabled turns the needs-you sounds on or off (default on).
	SettingSoundsEnabled = "sounds.enabled"
	// SettingSoundsVolume is their volume, 0 to 1 (default 0.6).
	SettingSoundsVolume = "sounds.volume"
	// SettingSoundsMuted lists the workspaces whose cards play no sound.
	SettingSoundsMuted = "sounds.muted_workspaces"
)

// DefaultSoundVolume is the volume when the setting is not set.
const DefaultSoundVolume = 0.6

// SoundPrefs reads the sound settings.
func SoundPrefs(st *store.Store) sounds.Prefs {
	p := sounds.Prefs{Enabled: true, Volume: DefaultSoundVolume, Muted: map[string]bool{}}
	if raw, err := st.Settings.Get(SettingSoundsEnabled); err == nil {
		_ = json.Unmarshal(raw, &p.Enabled)
	}
	if raw, err := st.Settings.Get(SettingSoundsVolume); err == nil {
		_ = json.Unmarshal(raw, &p.Volume)
	}
	var muted []string
	if raw, err := st.Settings.Get(SettingSoundsMuted); err == nil {
		_ = json.Unmarshal(raw, &muted)
	}
	for _, id := range muted {
		p.Muted[id] = true
	}
	return p
}

// SoundSnapshot reads where every card of every open workspace is: whether its column has the
// needs-you role. Archived cards and archived workspaces are left out.
func SoundSnapshot(st *store.Store) (map[string]sounds.CardState, error) {
	wss, err := st.Workspaces.List(false)
	if err != nil {
		return nil, err
	}
	out := map[string]sounds.CardState{}
	for _, w := range wss {
		cols, err := st.Columns.List(w.ID)
		if err != nil {
			return nil, err
		}
		needs := map[string]bool{}
		for _, c := range cols {
			needs[c.ID] = c.Role == domain.RoleNeedsYou
		}
		cards, err := st.Cards.List(w.ID, store.CardFilter{})
		if err != nil {
			return nil, err
		}
		for _, c := range cards {
			if !c.Archived {
				out[c.ID] = sounds.CardState{WorkspaceID: w.ID, NeedsYou: needs[c.ColumnID]}
			}
		}
	}
	return out, nil
}

// checkSoundSetting validates the sounds.* settings, so a planner over MCP gets a clear error.
func checkSoundSetting(k string, v any) error {
	switch k {
	case SettingSoundsEnabled:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false, got %v", k, v)
		}
	case SettingSoundsVolume:
		f, ok := v.(float64)
		if !ok || f < 0 || f > 1 {
			return fmt.Errorf("%s must be a number from 0 (silent) to 1 (full), got %v", k, v)
		}
	case SettingSoundsMuted:
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be a list of workspace ids, got %v", k, v)
		}
		for _, x := range list {
			if s, ok := x.(string); !ok || s == "" {
				return fmt.Errorf("%s must be a list of workspace ids, got %v", k, x)
			}
		}
	}
	return nil
}

// SoundPreviewIn is the input of sound_preview.
type SoundPreviewIn struct {
	Sound  string   `json:"sound" jsonschema:"needs_you (a card moved into a needs-you column) or back_to_work (it moved out)"`
	Volume *float64 `json:"volume,omitempty" jsonschema:"0 to 1; defaults to the sounds.volume setting"`
}

// SoundPreviewOut is the result of sound_preview.
type SoundPreviewOut struct {
	Played string  `json:"played"`
	Volume float64 `json:"volume"`
}

func registerSounds(r *Registry) {
	Register(r, Spec{Name: "sound_preview", Description: "Play one of Hadron's sounds now, even when sounds are off: needs_you (the chime when a card moves into a needs-you column) or back_to_work (the note when it moves out). Settings: sounds.enabled, sounds.volume (0 to 1) and sounds.muted_workspaces (workspace ids), through settings_update."},
		func(ctx context.Context, c *Call, in SoundPreviewIn) (SoundPreviewOut, error) {
			if !slices.Contains(sounds.Kinds, in.Sound) {
				return SoundPreviewOut{}, fmt.Errorf("sound %q does not exist. Use needs_you or back_to_work", in.Sound)
			}
			vol := SoundPrefs(c.Store).Volume
			if in.Volume != nil {
				if *in.Volume < 0 || *in.Volume > 1 {
					return SoundPreviewOut{}, errors.New("volume must be from 0 to 1")
				}
				vol = *in.Volume
			}
			if c.Deps.PlaySound == nil {
				return SoundPreviewOut{}, errors.New("this build of the app cannot play sounds")
			}
			if err := c.Deps.PlaySound(in.Sound, vol); err != nil {
				return SoundPreviewOut{}, err
			}
			return SoundPreviewOut{Played: in.Sound, Volume: vol}, nil
		})
}
