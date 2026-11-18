package actions

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/GabiHert/hadron/internal/quitguard"
	"github.com/GabiHert/hadron/internal/store"
)

// AppStatusOut is the result of app_status.
type AppStatusOut struct {
	Version       string `json:"version"`
	Home          string `json:"home"`
	Actions       int    `json:"actions"`
	SchemaVersion int    `json:"schema_version"`
}

func registerApp(r *Registry) {
	Register(r, Spec{Name: "app_status", Description: "Report the running Hadron version, its data folder, how many actions exist and the database schema version."},
		func(ctx context.Context, c *Call, in struct{}) (AppStatusOut, error) {
			v, err := c.Store.SchemaVersion()
			if err != nil {
				return AppStatusOut{}, err
			}
			return AppStatusOut{Version: c.Deps.Version, Home: c.Deps.Home, Actions: r.Len(), SchemaVersion: v}, nil
		})

	Register(r, Spec{Name: "app_quit", Description: "Quit Hadron now, past the quit guard. Only the user can, from the window's quit confirmation; sessions and MCP clients cannot. The guard (hold ⌘Q to quit; Quit from the menu or the Dock asks first) is the setting " + quitguard.SettingEnabled + ", on unless false."},
		func(ctx context.Context, c *Call, in struct{}) (AppQuitOut, error) {
			if c.Actor.Kind != KindYou {
				return AppQuitOut{}, errors.New("only the user can quit Hadron, from the window")
			}
			if c.Deps.Quit == nil {
				return AppQuitOut{}, nil
			}
			go c.Deps.Quit()
			return AppQuitOut{Quitting: true}, nil
		})
}

// AppQuitOut is the result of app_quit.
type AppQuitOut struct {
	Quitting bool `json:"quitting"`
}

// QuitGuardOn reads the app.quit_guard setting: on unless set to false.
func QuitGuardOn(st *store.Store) bool {
	raw, err := st.Settings.Get(quitguard.SettingEnabled)
	if err != nil || len(raw) == 0 {
		return true
	}
	var on bool
	if json.Unmarshal(raw, &on) != nil {
		return true
	}
	return on
}
