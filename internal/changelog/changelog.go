// Package changelog reads CHANGELOG.md, which is embedded in the app so "What's new" works offline.
//
// The file follows Keep a Changelog: a "## [Unreleased]" section, then one "## [X.Y.Z] - date"
// section per release, newest first, each with "### Added / Changed / Fixed" lists of "- " bullets.
// scripts/release.sh refuses to release when [Unreleased] is empty, and moves it under the new
// version.
package changelog

import (
	"regexp"
	"strings"
	"sync"
)

// Unreleased is the version name of the section that collects what has not shipped yet.
const Unreleased = "Unreleased"

// Section is one "### Added" style heading with its bullets.
type Section struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

// Release is one "## [version] - date" section.
type Release struct {
	Version  string    `json:"version"`
	Date     string    `json:"date"`
	Sections []Section `json:"sections"`
	// Body is the section's markdown, without its "## [...]" line. It is the release notes.
	Body string `json:"body"`
}

// Highlights returns up to n bullets, in order, for a tooltip.
func (r Release) Highlights(n int) []string {
	var out []string
	for _, s := range r.Sections {
		for _, it := range s.Items {
			if len(out) == n {
				return out
			}
			out = append(out, it)
		}
	}
	return out
}

// Empty says whether the section has no bullets.
func (r Release) Empty() bool { return len(r.Highlights(1)) == 0 }

var headRe = regexp.MustCompile(`^##\s+\[?([^\]\s]+)\]?(?:\s*[-–]\s*(\d{4}-\d{2}-\d{2}))?\s*$`)

// Parse reads a changelog, newest section first as written.
func Parse(md string) []Release {
	var out []Release
	var cur *Release
	var sec *Section
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
		out = append(out, *cur)
		cur, sec, body = nil, nil, nil
	}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	for _, line := range lines {
		if m := headRe.FindStringSubmatch(line); m != nil && strings.HasPrefix(line, "## ") {
			flush()
			cur = &Release{Version: m[1], Date: m[2]}
			continue
		}
		if cur == nil {
			continue
		}
		body = append(body, line)
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "### "):
			cur.Sections = append(cur.Sections, Section{Title: strings.TrimSpace(strings.TrimPrefix(line, "### "))})
			sec = &cur.Sections[len(cur.Sections)-1]
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			if sec == nil {
				cur.Sections = append(cur.Sections, Section{Title: "Changes"})
				sec = &cur.Sections[len(cur.Sections)-1]
			}
			sec.Items = append(sec.Items, strings.TrimSpace(trimmed[2:]))
		case trimmed != "" && sec != nil && len(sec.Items) > 0 && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")):
			sec.Items[len(sec.Items)-1] += " " + trimmed
		}
	}
	flush()
	return out
}

// Normalize turns v0.2.0 into 0.2.0.
func Normalize(version string) string { return strings.TrimPrefix(strings.TrimSpace(version), "v") }

var releaseVersionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// IsRelease says whether version is a plain vX.Y.Z, as opposed to a dev build such as
// v0.2.0-3-gabc123-dirty or dev.
func IsRelease(version string) bool { return releaseVersionRe.MatchString(strings.TrimSpace(version)) }

// Select picks the notes to show for the running version: its own section; for a dev build, the
// Unreleased section when it has bullets, else the newest release. found is false when a released
// version has no section.
func Select(releases []Release, running string) (rel Release, found bool) {
	if IsRelease(running) {
		want := Normalize(running)
		for _, r := range releases {
			if Normalize(r.Version) == want {
				return r, true
			}
		}
		return Release{}, false
	}
	for _, r := range releases {
		if r.Version == Unreleased && !r.Empty() {
			return r, true
		}
	}
	for _, r := range releases {
		if r.Version != Unreleased {
			return r, true
		}
	}
	return Release{}, false
}

var (
	mu       sync.RWMutex
	embedded string
)

// SetEmbedded records the CHANGELOG.md that main.go embedded. Call it once before starting.
func SetEmbedded(md string) {
	mu.Lock()
	embedded = md
	mu.Unlock()
}

// Embedded returns the embedded CHANGELOG.md, or "" before SetEmbedded.
func Embedded() string {
	mu.RLock()
	defer mu.RUnlock()
	return embedded
}
