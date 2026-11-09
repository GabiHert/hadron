package changelog

import (
	"os"
	"strings"
	"testing"
)

const sample = `# Changelog

Intro text.

## [Unreleased]

### Added
- A new thing that
  wraps onto a second line.

### Fixed
- A bug.

## [0.2.0] - 2026-11-09

### Added
- Feature A.
- Feature B.

### Changed
- Behaviour C.

## [0.1.0] - 2026-11-08

- First release.
`

func TestParse(t *testing.T) {
	rs := Parse(sample)
	if len(rs) != 3 || rs[0].Version != "Unreleased" || rs[1].Version != "0.2.0" || rs[1].Date != "2026-11-09" || rs[2].Version != "0.1.0" {
		t.Fatalf("releases = %+v", rs)
	}
	u := rs[0]
	if len(u.Sections) != 2 || u.Sections[0].Title != "Added" || u.Sections[0].Items[0] != "A new thing that wraps onto a second line." || u.Sections[1].Items[0] != "A bug." {
		t.Fatalf("unreleased = %+v", u.Sections)
	}
	if !strings.HasPrefix(rs[1].Body, "### Added\n- Feature A.") || strings.Contains(rs[1].Body, "0.1.0") {
		t.Errorf("body = %q", rs[1].Body)
	}
	if got := rs[1].Highlights(2); len(got) != 2 || got[1] != "Feature B." {
		t.Errorf("highlights = %v", got)
	}
	if len(rs[2].Sections) != 1 || rs[2].Sections[0].Title != "Changes" || rs[2].Sections[0].Items[0] != "First release." {
		t.Errorf("bullets without a heading go under Changes: %+v", rs[2].Sections)
	}
	if rs[0].Empty() || !(Release{}).Empty() {
		t.Error("Empty")
	}
}

func TestSelect(t *testing.T) {
	rs := Parse(sample)
	for _, tc := range []struct {
		running, want string
		found         bool
	}{
		{"v0.2.0", "0.2.0", true},
		{"0.1.0", "0.1.0", true},
		{"v0.3.0", "", false},
		{"dev", "Unreleased", true},
		{"v0.2.0-3-gabc123-dirty", "Unreleased", true},
	} {
		got, ok := Select(rs, tc.running)
		if ok != tc.found || (ok && got.Version != tc.want) {
			t.Errorf("Select(%q) = %q, %v; want %q, %v", tc.running, got.Version, ok, tc.want, tc.found)
		}
	}
	// A dev build with an empty Unreleased shows the newest release.
	empty := Parse("## [Unreleased]\n\n## [0.2.0] - 2026-11-09\n\n- A.\n")
	if got, ok := Select(empty, "dev"); !ok || got.Version != "0.2.0" {
		t.Errorf("dev with nothing unreleased = %q %v", got.Version, ok)
	}
	if !IsRelease("v1.2.3") || IsRelease("v1.2.3-rc1") || IsRelease("dev") || Normalize("v1.2.3") != "1.2.3" {
		t.Error("IsRelease/Normalize")
	}
}

// The repository's own CHANGELOG.md must keep the shape the app and scripts/release.sh rely on.
func TestTheRepositoryChangelogHasTheShapeTheToolsNeed(t *testing.T) {
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	rs := Parse(string(b))
	if len(rs) < 2 || rs[0].Version != Unreleased {
		t.Fatalf("CHANGELOG.md must start its sections with [Unreleased]: %+v", rs)
	}
	var has010 bool
	for _, r := range rs[1:] {
		if r.Version == Unreleased {
			t.Error("[Unreleased] twice")
		}
		if r.Date == "" || r.Empty() {
			t.Errorf("release %s needs a date and entries", r.Version)
		}
		has010 = has010 || r.Version == "0.1.0"
	}
	if !has010 {
		t.Error("the 0.1.0 section is missing")
	}
}
