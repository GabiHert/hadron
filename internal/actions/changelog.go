package actions

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/GabiHert/hadron/internal/changelog"
)

// SettingChangelogSeen is the version whose "What's new" the user has already seen.
const SettingChangelogSeen = "changelog.seen"

// ChangelogGetIn is the input of changelog_get.
type ChangelogGetIn struct {
	Version string `json:"version,omitempty" jsonschema:"a release such as v0.2.0 or Unreleased; default is the running version (a dev build shows Unreleased)"`
}

// ChangelogEntry is one release in the list.
type ChangelogEntry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

// ChangelogOut is the result of changelog_get.
type ChangelogOut struct {
	// Running is the version of this app.
	Running string `json:"running"`
	// Found says whether Release holds a section; a released version without one is a missing entry.
	Found   bool              `json:"found"`
	Release changelog.Release `json:"release"`
	// Highlights are the first bullets of Release, for a tooltip.
	Highlights []string         `json:"highlights"`
	Releases   []ChangelogEntry `json:"releases"`
	// Seen is the version whose notes the user already saw. Show says the app should open What's
	// new now: this is a new release, nobody has seen its notes, and it is not a dev build.
	Seen string `json:"seen"`
	Show bool   `json:"show"`
}

func registerChangelog(r *Registry) {
	Register(r, Spec{Name: "changelog_get", Description: "Read what changed in Hadron: the notes of the running version (or of the version you name, or Unreleased), the list of every release, and whether the user has seen the running version's notes. The notes come from CHANGELOG.md, which ships inside the app. Mark them seen with settings_update {\"changelog.seen\": \"<version>\"}."},
		func(ctx context.Context, c *Call, in ChangelogGetIn) (ChangelogOut, error) {
			md := c.Deps.Changelog
			if md == "" {
				md = changelog.Embedded()
			}
			all := changelog.Parse(md)
			out := ChangelogOut{Running: c.Deps.Version, Releases: []ChangelogEntry{}, Highlights: []string{}}
			for _, rel := range all {
				if rel.Version == changelog.Unreleased && rel.Empty() {
					continue
				}
				summary := ""
				if h := rel.Highlights(1); len(h) > 0 {
					summary = h[0]
				}
				out.Releases = append(out.Releases, ChangelogEntry{Version: rel.Version, Date: rel.Date, Summary: summary})
			}
			if v := strings.TrimSpace(in.Version); v != "" {
				for _, rel := range all {
					if strings.EqualFold(rel.Version, v) || changelog.Normalize(rel.Version) == changelog.Normalize(v) {
						out.Release, out.Found = rel, true
					}
				}
			} else {
				out.Release, out.Found = changelog.Select(all, c.Deps.Version)
			}
			if out.Found {
				out.Highlights = out.Release.Highlights(3)
			}
			if raw, err := c.Store.Settings.Get(SettingChangelogSeen); err == nil {
				_ = json.Unmarshal(raw, &out.Seen)
			}
			fresh := out.Seen != "" // an upgrade; a brand-new install has nothing to catch up on
			if !fresh {
				if ws, err := c.Store.Workspaces.List(true); err == nil && len(ws) > 0 {
					fresh = true
				}
			}
			out.Show = in.Version == "" && out.Found && changelog.IsRelease(c.Deps.Version) && fresh &&
				changelog.Normalize(out.Seen) != changelog.Normalize(c.Deps.Version)
			return out, nil
		})
}
