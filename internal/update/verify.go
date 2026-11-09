package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// assetSuffixes are the release zips this Mac can run, best first.
func assetSuffixes() []string {
	return []string{"-darwin-" + runtime.GOARCH + ".zip", "-darwin-universal.zip"}
}

// parseSums reads a SHA256SUMS file ("<hex>  <name>" or "<hex> *<name>" per line) into name → hex.
func parseSums(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(r, 1<<20))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sum, name, found := strings.Cut(line, " ")
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		sum = strings.ToLower(sum)
		if !found || name == "" || len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("%s has a line the updater cannot read: %q", sumsName, line)
		}
		out[name] = sum
	}
	return out, sc.Err()
}

// hashFile returns the hex SHA-256 of path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SigInfo is what codesign says about an app.
type SigInfo struct {
	Identifier string // the bundle's signing identifier, such as com.example.app
	TeamID     string // "" for ad hoc and self-signed apps
	Adhoc      bool
}

// Signer checks code signatures. Tests replace it.
type Signer interface {
	// Info describes the signature of app.
	Info(ctx context.Context, app string) (SigInfo, error)
	// Verify checks that every file in app matches its signature.
	Verify(ctx context.Context, app string) error
	// Assess asks Gatekeeper whether app may run (Developer ID and notarization).
	Assess(ctx context.Context, app string) error
}

type codesign struct{}

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

func (codesign) Info(ctx context.Context, app string) (SigInfo, error) {
	out, err := run(ctx, "/usr/bin/codesign", "-dvv", app)
	if err != nil {
		return SigInfo{}, fmt.Errorf("codesign could not read the signature of %s: %s", app, out)
	}
	return parseSigInfo(out), nil
}

func parseSigInfo(out string) SigInfo {
	var s SigInfo
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "Identifier":
			s.Identifier = v
		case "TeamIdentifier":
			if v != "not set" {
				s.TeamID = v
			}
		case "Signature":
			s.Adhoc = v == "adhoc"
		}
	}
	return s
}

func (codesign) Verify(ctx context.Context, app string) error {
	if out, err := run(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", app); err != nil {
		return fmt.Errorf("the signature of the new app is broken: %s", out)
	}
	return nil
}

func (codesign) Assess(ctx context.Context, app string) error {
	if out, err := run(ctx, "/usr/sbin/spctl", "--assess", "--type", "execute", app); err != nil {
		return fmt.Errorf("Gatekeeper would not open the new app: %s", out)
	}
	return nil
}

// checkSignature decides whether the new app may replace the installed one. The new app must be
// intact and carry the same identifier. When the installed app is signed by a Developer ID team,
// the new one must be signed by the same team and pass Gatekeeper, so an update never lowers
// what the installed app is trusted with.
func checkSignature(ctx context.Context, sg Signer, installed, next string) error {
	if err := sg.Verify(ctx, next); err != nil {
		return err
	}
	cur, err := sg.Info(ctx, installed)
	if err != nil {
		return err
	}
	nw, err := sg.Info(ctx, next)
	if err != nil {
		return err
	}
	if nw.Identifier != cur.Identifier {
		return fmt.Errorf("the new app is signed as %q, not %q", nw.Identifier, cur.Identifier)
	}
	if cur.TeamID != "" && nw.TeamID != cur.TeamID {
		who := "ad hoc"
		if nw.TeamID != "" {
			who = "by team " + nw.TeamID
		}
		return fmt.Errorf("the installed app is signed by team %s but the new one is signed %s", cur.TeamID, who)
	}
	if nw.TeamID != "" {
		return sg.Assess(ctx, next)
	}
	return nil
}

// newer reports whether tag names a later version than current. Versions are semver tags such as
// "v1.2.3" or "v1.2.3-rc.1"; a build that is not on a tag ("v1.2.3-4-gabc1234", a bare commit or
// "dev") counts as older than any release, except that "v1.2.3-4-gabc" is newer than v1.2.3.
func newer(tag, current string) bool {
	t, ok := parseSemver(tag)
	if !ok {
		return false
	}
	c, ok := parseSemver(current)
	if !ok {
		return true
	}
	return compareSemver(t, c) > 0
}

type semver struct {
	n   [3]int
	pre string // "" for a release
	// ahead is set for a git describe version ("v1.2.3-4-gabc1234"): commits after the tag.
	ahead bool
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimSuffix(strings.TrimSpace(v), "-dirty")
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return semver{}, false
	}
	var s semver
	if i := strings.LastIndex(v, "-g"); i >= 0 {
		if j := strings.LastIndex(v[:i], "-"); j >= 0 {
			if _, err := strconv.Atoi(v[j+1 : i]); err == nil {
				s.ahead, v = true, v[:j]
			}
		}
	}
	core, pre, _ := strings.Cut(v, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		s.n[i] = n
	}
	s.pre = pre
	return s, true
}

// compareSemver orders versions; a pre-release sorts before its release, and a build ahead of a
// tag sorts after it.
func compareSemver(a, b semver) int {
	for i := range a.n {
		if a.n[i] != b.n[i] {
			if a.n[i] > b.n[i] {
				return 1
			}
			return -1
		}
	}
	if a.pre != b.pre {
		switch {
		case a.pre == "":
			return 1
		case b.pre == "":
			return -1
		case a.pre > b.pre:
			return 1
		default:
			return -1
		}
	}
	switch {
	case a.ahead && !b.ahead:
		return 1
	case b.ahead && !a.ahead:
		return -1
	}
	return 0
}
