package actions

import (
	"context"
	"errors"

	"github.com/GabiHert/hadron/internal/update"
)

var errNoUpdater = errors.New("updates are not available in this build")

// UpdateRelease is the newest release, as the Updates card shows it.
type UpdateRelease struct {
	Tag   string `json:"tag"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
	Date  int64  `json:"date" jsonschema:"milliseconds since the epoch"`
	URL   string `json:"url"`
}

// UpdateStatus is the result of the update actions. The field names are the ones the Updates card
// from the release-updater skill reads.
type UpdateStatus struct {
	Version      string         `json:"version"`
	Repo         string         `json:"repo"`
	Latest       *UpdateRelease `json:"latest"`
	UpToDate     bool           `json:"upToDate"`
	HasToken     bool           `json:"hasToken"`
	State        string         `json:"state"`
	Message      string         `json:"message"`
	Lines        []string       `json:"lines"`
	Seq          int            `json:"seq"`
	InstalledApp string         `json:"installedApp"`
}

// SetUpdateTokenIn is the input of set_update_token.
type SetUpdateTokenIn struct {
	Token string `json:"token,omitempty" jsonschema:"a fine-grained GitHub token with Contents: read-only on the Hadron repo; empty removes the saved token"`
}

func toUpdateStatus(s update.Status) UpdateStatus {
	d := UpdateStatus{Version: s.Version, Repo: s.Repo, UpToDate: s.UpToDate, HasToken: s.HasToken, State: s.State, Message: s.Message,
		Lines: s.Lines, Seq: s.Seq, InstalledApp: s.InstalledApp}
	if d.Lines == nil {
		d.Lines = []string{}
	}
	if r := s.Latest; r != nil {
		d.Latest = &UpdateRelease{Tag: r.Tag, Name: r.Name, Notes: r.Notes, Date: r.Date.UnixMilli(), URL: r.URL}
	}
	return d
}

func registerUpdate(r *Registry) {
	Register(r, Spec{Name: "get_update_status", Description: "Report the running version, the newest release found, and the state of any update in progress."},
		func(ctx context.Context, c *Call, in struct{}) (UpdateStatus, error) {
			if c.Deps.Update == nil {
				return UpdateStatus{Version: c.Deps.Version, State: "idle", Lines: []string{}}, nil
			}
			return toUpdateStatus(c.Deps.Update.Status()), nil
		})

	Register(r, Spec{Name: "check_for_update", Description: "Ask GitHub whether a newer Hadron release exists. It downloads and installs nothing."},
		func(ctx context.Context, c *Call, in struct{}) (UpdateStatus, error) {
			if c.Deps.Update == nil {
				return UpdateStatus{Version: c.Deps.Version, State: "idle", Lines: []string{}}, errNoUpdater
			}
			s, err := c.Deps.Update.Check(ctx)
			c.Deps.Log.Info("updater check", "state", s.State, "up_to_date", s.UpToDate, "error", errText(err))
			return toUpdateStatus(s), err
		})

	Register(r, Spec{Name: "start_update", Description: "Download the newest release, verify its checksum and code signature, then quit and swap the app. If the new version does not start within 20 seconds, the old one comes back.",
		Mutates: true, Entities: []string{"update"}},
		func(ctx context.Context, c *Call, in struct{}) (struct{}, error) {
			if c.Deps.Update == nil {
				return struct{}{}, errNoUpdater
			}
			err := c.Deps.Update.Start()
			c.Deps.Log.Info("updater start", "error", errText(err))
			return struct{}{}, err
		})

	Register(r, Spec{Name: "set_update_token", Description: "Save a GitHub token in the login keychain, for checking a private repo. An empty token removes it.",
		Mutates: true, Sensitive: true, Entities: []string{"update"}},
		func(ctx context.Context, c *Call, in SetUpdateTokenIn) (UpdateStatus, error) {
			if c.Deps.Update == nil {
				return UpdateStatus{}, errNoUpdater
			}
			if err := c.Deps.Update.SetToken(ctx, in.Token); err != nil {
				return toUpdateStatus(c.Deps.Update.Status()), err
			}
			return toUpdateStatus(c.Deps.Update.Status()), nil
		})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
