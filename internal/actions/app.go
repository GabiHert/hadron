package actions

import "context"

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
}
