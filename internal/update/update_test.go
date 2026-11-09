package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, current string
		want         bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.2.0", "v1.2.0-dirty", false},
		{"v1.2.0", "v1.10.0", false},
		{"v2.0.0", "v1.99.99", true},
		{"v1.2.0", "v1.2.0-rc.1", true},
		{"v1.2.0-rc.2", "v1.2.0-rc.1", true},
		{"v1.2.0-rc.1", "v1.2.0", false},
		{"v1.2.0", "v1.2.0-3-g670535b", false}, // built after the tag
		{"v1.2.1", "v1.2.0-3-g670535b", true},
		{"v1.0.0", "670535b", true}, // a build off a tag is older than any release
		{"v1.0.0", "dev", true},
		{"v1.0.0", "", true},
		{"not-a-version", "v0.0.1", false},
		{"v1.2", "v1.1.0", false},
		{"1.3.0", "v1.2.0", true},
	}
	for _, c := range cases {
		if got := newer(c.tag, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.tag, c.current, got, c.want)
		}
	}
}

func TestParseSums(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	got, err := parseSums(strings.NewReader(a + "  Demo-v1.0.0-darwin-arm64.zip\n\n" + b + " *other.zip\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["Demo-v1.0.0-darwin-arm64.zip"] != a || got["other.zip"] != strings.ToLower(b) {
		t.Errorf("got %v", got)
	}
	if _, err := parseSums(strings.NewReader("abc  x.zip\n")); err == nil {
		t.Error("a short sum was accepted")
	}
}

func TestParseSigInfo(t *testing.T) {
	s := parseSigInfo("Executable=/x\nIdentifier=com.example.demo\nSignature=adhoc\nTeamIdentifier=not set\n")
	if s != (SigInfo{Identifier: "com.example.demo", Adhoc: true}) {
		t.Errorf("adhoc: %+v", s)
	}
	s = parseSigInfo("Identifier=com.example.demo\nAuthority=Developer ID Application: X (ABCDE12345)\nTeamIdentifier=ABCDE12345\n")
	if s != (SigInfo{Identifier: "com.example.demo", TeamID: "ABCDE12345"}) {
		t.Errorf("developer id: %+v", s)
	}
}

// fakeSigner reports each app's signature from a map keyed by the app's base folder name.
type fakeSigner struct {
	info     map[string]SigInfo
	broken   bool
	assessed []string
	rejected bool
}

func (f *fakeSigner) key(app string) string { return filepath.Base(filepath.Dir(app)) }

func (f *fakeSigner) Info(_ context.Context, app string) (SigInfo, error) {
	s, ok := f.info[f.key(app)]
	if !ok {
		return SigInfo{}, fmt.Errorf("no signature for %s", app)
	}
	return s, nil
}

func (f *fakeSigner) Verify(context.Context, string) error {
	if f.broken {
		return errors.New("broken")
	}
	return nil
}

func (f *fakeSigner) Assess(_ context.Context, app string) error {
	f.assessed = append(f.assessed, app)
	if f.rejected {
		return errors.New("rejected")
	}
	return nil
}

func TestCheckSignature(t *testing.T) {
	adhoc := SigInfo{Identifier: "com.example.demo", Adhoc: true}
	team := SigInfo{Identifier: "com.example.demo", TeamID: "TEAM1"}
	other := SigInfo{Identifier: "com.example.demo", TeamID: "TEAM2"}
	cases := []struct {
		name          string
		cur, next     SigInfo
		broken, block bool
		wantErr       string
		wantAssess    bool
	}{
		{name: "adhoc to adhoc", cur: adhoc, next: adhoc},
		{name: "adhoc to team", cur: adhoc, next: team, wantAssess: true},
		{name: "team to team", cur: team, next: team, wantAssess: true},
		{name: "team to adhoc", cur: team, next: adhoc, wantErr: "signed ad hoc"},
		{name: "team to other team", cur: team, next: other, wantErr: "by team TEAM2"},
		{name: "other identifier", cur: adhoc, next: SigInfo{Identifier: "com.evil"}, wantErr: "com.evil"},
		{name: "broken", cur: adhoc, next: adhoc, broken: true, wantErr: "broken"},
		{name: "gatekeeper says no", cur: team, next: team, block: true, wantErr: "rejected", wantAssess: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeSigner{info: map[string]SigInfo{"installed": c.cur, "next": c.next}, broken: c.broken, rejected: c.block}
			err := checkSignature(context.Background(), f, "/x/installed/Demo.app", "/x/next/Demo.app")
			if c.wantErr == "" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			if (len(f.assessed) > 0) != c.wantAssess {
				t.Errorf("assessed = %v, want %v", f.assessed, c.wantAssess)
			}
		})
	}
}

const zipName = "Demo-v1.1.0-darwin-arm64.zip"

// releaseServer serves a latest release whose zip is zip and whose SHA256SUMS lists sum. When
// token is set, every request needs it.
func releaseServer(t *testing.T, zip []byte, sum, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/repos/owner/demo/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.1.0","name":"Demo 1.1","body":"notes","html_url":"https://x","published_at":"2026-11-05T10:00:00Z",
				"assets":[{"id":1,"name":"Demo-v1.1.0-darwin-amd64.zip","size":1},{"id":2,"name":%q,"size":%d},{"id":3,"name":"SHA256SUMS","size":100}]}`, zipName, len(zip))
		case "/repos/owner/demo/releases/assets/2":
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "accept", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(zip)
		case "/repos/owner/demo/releases/assets/3":
			fmt.Fprintf(w, "%s  %s\n", sum, zipName)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLatestPicksThisMacAndFallsBackToToken(t *testing.T) {
	srv := releaseServer(t, []byte("zip"), strings.Repeat("0", 64), "good")
	defer srv.Close()
	var owners []string
	g := &GitHub{Repo: "owner/demo", BaseURL: srv.URL, Client: srv.Client(),
		AssetSuffixes: []string{"-darwin-arm64.zip", "-darwin-universal.zip"},
		Tokens: func(_ context.Context, owner string) []string {
			owners = append(owners, owner)
			return []string{"bad", "good"}
		}}
	r, err := g.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v1.1.0" || r.Asset.ID != 2 || r.Sums.ID != 3 || r.Date.Year() != 2026 || len(owners) != 1 || owners[0] != "owner" {
		t.Errorf("got %+v owners %v", r, owners)
	}

	g.Tokens = func(context.Context, string) []string { return nil }
	if _, err := g.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "Add a GitHub token") {
		t.Errorf("err = %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/demo" && r.Header.Get("Authorization") == "Bearer good" {
			fmt.Fprint(w, `{}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer empty.Close()
	g2 := *g
	g2.BaseURL, g2.Client = empty.URL, empty.Client()
	g2.Tokens = func(context.Context, string) []string { return []string{"good"} }
	if _, err := g2.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no published release yet") {
		t.Errorf("no release: err = %v", err)
	}

	g.Tokens = func(context.Context, string) []string { return []string{"good"} }
	g.AssetSuffixes = []string{"-darwin-ppc.zip"}
	if _, err := g.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no file for this Mac") {
		t.Errorf("err = %v", err)
	}
}

// releaseZip builds a Demo.app folder and zips it the way the release script does.
func releaseZip(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "Demo.app", "Contents", "MacOS", "Demo")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), zipName)
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", filepath.Join(dir, "Demo.app"), zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v %s", err, out)
	}
	b, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sumOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func newTestService(t *testing.T, srv *httptest.Server, f *fakeSigner) (*Service, string) {
	t.Helper()
	installed := filepath.Join(t.TempDir(), "installed", "Demo.app")
	s := New(Deps{Version: "v1.0.0", AppName: "Demo", Dir: t.TempDir()})
	s.gh.BaseURL, s.gh.Client, s.gh.Repo = srv.URL, srv.Client(), "owner/demo"
	s.gh.AssetSuffixes = []string{"-darwin-arm64.zip"}
	s.gh.Tokens = nil
	s.keys.Bin = "/usr/bin/false"
	s.signer = f
	return s, installed
}

func TestFetchStagesAVerifiedApp(t *testing.T) {
	zip := releaseZip(t)
	srv := releaseServer(t, zip, sumOf(zip), "")
	defer srv.Close()
	adhoc := SigInfo{Identifier: "com.example.demo", Adhoc: true}
	// The unpacked app's parent folder is "unpacked".
	f := &fakeSigner{info: map[string]SigInfo{"installed": adhoc, "unpacked": adhoc}}
	s, installed := newTestService(t, srv, f)

	st, err := s.Check(context.Background())
	if err != nil || st.UpToDate || st.Latest.Tag != "v1.1.0" {
		t.Fatalf("check: %+v %v", st, err)
	}
	staged, err := s.fetch(*st.Latest, installed)
	if err != nil {
		t.Fatal(err)
	}
	if staged != s.stagedApp() {
		t.Errorf("staged = %s", staged)
	}
	if _, err := os.Stat(filepath.Join(staged, "Contents", "MacOS", "Demo")); err != nil {
		t.Errorf("staged app is missing its executable: %v", err)
	}
}

func TestFetchRefusesABadDownload(t *testing.T) {
	zip := releaseZip(t)
	adhoc := SigInfo{Identifier: "com.example.demo", Adhoc: true}
	team := SigInfo{Identifier: "com.example.demo", TeamID: "TEAM1"}
	cases := []struct {
		name, sum string
		signer    *fakeSigner
		want      string
	}{
		{"checksum", strings.Repeat("0", 64), &fakeSigner{info: map[string]SigInfo{"installed": adhoc, "unpacked": adhoc}}, "does not match SHA256SUMS"},
		{"signature", sumOf(zip), &fakeSigner{info: map[string]SigInfo{"installed": team, "unpacked": adhoc}}, "signature check"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := releaseServer(t, zip, c.sum, "")
			defer srv.Close()
			s, installed := newTestService(t, srv, c.signer)
			st, err := s.Check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.fetch(*st.Latest, installed); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if _, err := os.Stat(s.stagedApp()); err == nil {
				t.Error("a refused app was staged")
			}
		})
	}
}

func TestUpToDateRelease(t *testing.T) {
	srv := releaseServer(t, []byte("zip"), strings.Repeat("0", 64), "")
	defer srv.Close()
	s, _ := newTestService(t, srv, &fakeSigner{})
	s.d.Version = "v1.1.0"
	st, err := s.Check(context.Background())
	if err != nil || !st.UpToDate {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestPreflightExplainsAppTranslocation(t *testing.T) {
	app := "/private/var/folders/6r/x/T/AppTranslocation/0EBE69DF/d/Demo.app"
	s := New(Deps{Version: "v0.2.0", AppName: "Demo", Dir: t.TempDir()})
	s.executable = func() (string, error) { return app + "/Contents/MacOS/Demo", nil }
	if _, err := s.preflight(); err == nil || !strings.Contains(err.Error(), "drag Demo.app into Applications in Finder") {
		t.Fatalf("err = %v", err)
	}
}
