package actions

import (
	"strings"
	"testing"
)

const changelogFixture = `# Changelog

## [Unreleased]

### Added
- Unreleased thing.

## [0.2.0] - 2026-11-09

### Added
- Feature A.
- Feature B.
- Feature C.
- Feature D.

## [0.1.0] - 2026-11-08

### Added
- First release.
`

func changelogHarness(t *testing.T, version string) *harness {
	t.Helper()
	deps, rec := testDeps(t)
	deps.Version = version
	deps.Changelog = changelogFixture
	r := NewRegistry(deps)
	RegisterAll(r)
	return &harness{t: t, reg: r, rec: rec}
}

func TestChangelogGetForTheRunningVersion(t *testing.T) {
	h := changelogHarness(t, "v0.2.0")
	var out ChangelogOut
	h.do("changelog_get", m{}, &out)
	if !out.Found || out.Release.Version != "0.2.0" || out.Release.Date != "2026-11-09" || len(out.Highlights) != 3 || out.Highlights[0] != "Feature A." {
		t.Fatalf("out = %+v", out)
	}
	if len(out.Releases) != 3 || out.Releases[0].Version != "Unreleased" || out.Releases[2].Summary != "First release." {
		t.Errorf("releases = %+v", out.Releases)
	}
	if !strings.Contains(out.Release.Body, "- Feature D.") {
		t.Errorf("body = %q", out.Release.Body)
	}
	// Another version by name, and Unreleased.
	h.do("changelog_get", m{"version": "v0.1.0"}, &out)
	if out.Release.Version != "0.1.0" || out.Show {
		t.Errorf("by name = %+v", out)
	}
	h.do("changelog_get", m{"version": "Unreleased"}, &out)
	if !out.Found || out.Release.Version != "Unreleased" {
		t.Errorf("unreleased = %+v", out)
	}
	h.do("changelog_get", m{"version": "9.9.9"}, &out)
	if out.Found {
		t.Errorf("an unknown version is not found: %+v", out)
	}
}

func TestChangelogShowOnlyForANewReleaseNobodySawAndNeverForDevBuilds(t *testing.T) {
	// A brand-new install (no workspace, nothing seen) has nothing to catch up on.
	h := changelogHarness(t, "v0.2.0")
	var out ChangelogOut
	h.do("changelog_get", m{}, &out)
	if out.Show {
		t.Error("a fresh install does not open What's new")
	}
	// Once there is a workspace (an upgrade from a release that had none of this), it shows.
	h.workspace("Acme")
	h.do("changelog_get", m{}, &out)
	if !out.Show || out.Seen != "" {
		t.Fatalf("first run of a new version = %+v", out)
	}
	// Marking it seen through settings stops it.
	h.do("settings_update", m{"values": m{"changelog.seen": "v0.2.0"}}, nil)
	h.do("changelog_get", m{}, &out)
	if out.Show || out.Seen != "v0.2.0" {
		t.Errorf("after seen = %+v", out)
	}
	// The older seen version shows the next release again.
	h.do("settings_update", m{"values": m{"changelog.seen": "0.1.0"}}, nil)
	h.do("changelog_get", m{}, &out)
	if !out.Show {
		t.Errorf("seen 0.1.0, running 0.2.0 = %+v", out)
	}

	// A dev build shows Unreleased in the tooltip but never pops the window.
	d := changelogHarness(t, "v0.2.0-4-gabc123-dirty")
	d.workspace("Acme")
	d.do("changelog_get", m{}, &out)
	if out.Show || !out.Found || out.Release.Version != "Unreleased" || out.Highlights[0] != "Unreleased thing." {
		t.Errorf("dev build = %+v", out)
	}
	// A released version missing from the file is reported as not found, without failing.
	x := changelogHarness(t, "v0.3.0")
	x.workspace("Acme")
	x.do("changelog_get", m{}, &out)
	if out.Found || out.Show {
		t.Errorf("a version with no entry = %+v", out)
	}
}

func TestChangelogDefaultsToTheEmbeddedFile(t *testing.T) {
	deps, rec := testDeps(t)
	deps.Version = "v0.1.0"
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r, rec: rec}
	var out ChangelogOut
	h.do("changelog_get", m{}, &out)
	if out.Found {
		t.Errorf("nothing is embedded in a test binary, so there is nothing to find: %+v", out)
	}
}
