// Package update replaces a macOS app with its newest GitHub Release: it downloads the release
// zip that `make release` built and published, checks it against the release's SHA256SUMS and its
// code signature, hands the new app to a detached install script and quits. The script swaps the
// apps and rolls back if the new one does not start. Nothing is built on the machine that updates.
// From the release-updater skill (~/.claude/skills/release-updater).
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Events the service emits through Deps.Emit.
const (
	EventOutput    = "update:output"    // {seq, line}: one line of the update's output
	EventState     = "update:state"     // {state, message}
	EventAvailable = "update:available" // {tag}: a newer release is out
)

// Status is what the Updates card shows.
type Status struct {
	Version      string
	Repo         string
	Latest       *Release // nil until a check succeeds
	UpToDate     bool
	HasToken     bool   // a GitHub token is saved in the keychain
	State        string // "idle", "checking", "downloading", "verifying", "installing" or "failed"
	Message      string
	Lines        []string // the current run's output, at most maxLines; never nil
	Seq          int      // sequence number of the last line ever emitted
	InstalledApp string   // "" when the app does not run from an app bundle
}

// Deps are what the service needs.
type Deps struct {
	Version string
	AppName string          // the bundle is AppName.app and its executable Contents/MacOS/AppName
	Repo    string          // owner/name on GitHub
	Env     func() []string // the login shell's environment, for gh
	Emit    func(event string, data any)
	Quit    func()
	// Notify tells the person a new version is out; nil means no notification.
	Notify  func(title, body string)
	Dir     string // the update workspace, paths.UpdateDir()
	LogPath string // the install script's log
}

// Service runs one update at a time.
type Service struct {
	d          Deps
	gh         *GitHub
	keys       Keychain
	signer     Signer
	executable func() (string, error)
	install    func(pid int, newApp, installedApp, logPath string) error
	unzip      func(ctx context.Context, zip, dir string) error

	mu       sync.Mutex
	busy     bool
	state    string
	msg      string
	lines    []string
	seq      int
	latest   *Release
	notified string // the tag the person was last told about
}

var (
	checkTimeout    = 30 * time.Second
	downloadTimeout = 15 * time.Minute
	verifyTimeout   = 5 * time.Minute
	quitDelay       = 1500 * time.Millisecond
	// staleAfter is how old a staged app must be before startup deletes it: the install script
	// copies it away within seconds of the old app quitting.
	staleAfter = 10 * time.Minute
)

const (
	maxLines    = 5000
	maxDownload = 1 << 30
	wOK         = 0x2 // access(2) write permission
)

var errBusy = errors.New("An update is already running.")

// New returns an idle service.
func New(d Deps) *Service {
	s := &Service{d: d, keys: Keychain{Service: d.AppName + " updates"}, signer: codesign{},
		executable: os.Executable, state: "idle", lines: []string{}, unzip: ditto}
	s.gh = newGitHub(d.Repo, s.tokens)
	s.install = func(pid int, newApp, installedApp, logPath string) error {
		return startInstaller(pid, newApp, installedApp, d.AppName, logPath, nil)
	}
	return s
}

func (s *Service) unchanged() string { return " " + s.d.AppName + " did not change." }

// tokens returns the saved token, then gh's tokens on a machine that has gh.
func (s *Service) tokens(ctx context.Context, owner string) []string {
	var out []string
	if t := s.keys.Get(ctx, s.gh.Repo); t != "" {
		out = append(out, t)
	}
	if s.d.Env != nil {
		out = append(out, ghTokens(ctx, s.d.Env(), owner)...)
	}
	return out
}

// SetToken saves a GitHub token for the update repo; "" removes the saved one.
func (s *Service) SetToken(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return s.keys.Delete(ctx, s.gh.Repo)
	}
	return s.keys.Set(ctx, s.gh.Repo, token)
}

// CleanLeftovers removes what an interrupted update left behind. The staged app may still be
// copied by a running install script, so only an old one goes.
func (s *Service) CleanLeftovers() {
	s.trash(s.workDir())
	if fi, err := os.Lstat(s.stagedApp()); err == nil && time.Since(fi.ModTime()) > staleAfter {
		_ = os.RemoveAll(s.stagedApp())
	}
	old, _ := filepath.Glob(filepath.Join(s.d.Dir, ".trash-*"))
	go func() {
		for _, p := range old {
			_ = os.RemoveAll(p)
		}
	}()
}

func (s *Service) workDir() string   { return filepath.Join(s.d.Dir, "download") }
func (s *Service) stagedApp() string { return filepath.Join(s.d.Dir, s.d.AppName+".app") }

// trash moves dir out of the way at once and deletes it in the background, so a new download
// never lands in a folder that is still being deleted.
func (s *Service) trash(dir string) {
	t := filepath.Join(s.d.Dir, fmt.Sprintf(".trash-%d", time.Now().UnixNano()))
	if os.Rename(dir, t) == nil {
		go func() { _ = os.RemoveAll(t) }()
	}
}

// Status returns the current version, the last check and the current run.
func (s *Service) Status() Status {
	inst, _ := s.installedApp()
	hasToken := s.keys.Get(context.Background(), s.gh.Repo) != ""
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Version: s.d.Version, Repo: s.gh.Repo, State: s.state, Message: s.msg,
		Lines: append([]string{}, s.lines...), Seq: s.seq, InstalledApp: inst, HasToken: hasToken}
	if s.latest != nil {
		r := *s.latest
		st.Latest, st.UpToDate = &r, !newer(r.Tag, s.d.Version)
	}
	return st
}

// Check asks GitHub for the newest release and returns the status with it.
func (s *Service) Check(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	r, err := s.gh.Latest(ctx)
	if err != nil {
		return s.Status(), err
	}
	s.mu.Lock()
	s.latest = &r
	s.mu.Unlock()
	return s.Status(), nil
}

// Watch checks for a new release after first, then every interval, until ctx ends. It tells the
// person once per new version; a failed check is quiet, and the installed version keeps running.
func (s *Service) Watch(ctx context.Context, first, interval time.Duration) {
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		busy := s.busy
		s.mu.Unlock()
		if !busy {
			if st, err := s.Check(ctx); err == nil && st.Latest != nil && !st.UpToDate {
				s.announce(st.Latest.Tag)
			}
		}
		t.Reset(interval)
	}
}

func (s *Service) announce(tag string) {
	s.mu.Lock()
	if s.notified == tag {
		s.mu.Unlock()
		return
	}
	s.notified = tag
	s.mu.Unlock()
	s.emit(EventAvailable, map[string]any{"tag": tag})
	if s.d.Notify != nil {
		s.d.Notify(s.d.AppName+" "+tag+" is out", "Open Settings → Updates to install it.")
	}
}

// installedApp returns the .app bundle the app runs from.
func (s *Service) installedApp() (string, bool) {
	exe, err := s.executable()
	if err != nil {
		return "", false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	i := strings.LastIndex(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return "", false
	}
	return exe[:i+len(".app")], true
}

// Start checks that an update is safe, then checks, downloads, verifies and installs in the background.
func (s *Service) Start() error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errBusy
	}
	s.busy = true
	s.mu.Unlock()

	inst, err := s.preflight()
	if err != nil {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.lines = []string{}
	s.setStateLocked("checking", "")
	s.mu.Unlock()
	go s.run(inst)
	return nil
}

func (s *Service) preflight() (string, error) {
	n := s.d.AppName
	inst, ok := s.installedApp()
	if !ok {
		return "", fmt.Errorf("%s is not running from an app bundle, so it cannot replace itself. Open %s.app from your Applications folder and update from there.", n, n)
	}
	if translocated(inst) {
		return "", fmt.Errorf("macOS is running %s from a temporary read-only copy, because it was opened from the folder it was downloaded to. Quit %s, drag %s.app into Applications in Finder, then open it from there and update again.", n, n, n)
	}
	if _, err := os.Lstat(inst + ".old"); err == nil {
		return "", fmt.Errorf("A previous update left %s.old. Check that %s opens, delete %s.old, then update again.", inst, n, inst)
	}
	if dir := filepath.Dir(inst); syscall.Access(dir, wOK) != nil {
		return "", fmt.Errorf("%s cannot write to %s, so it cannot replace %s. Move %s to a folder you can write to, such as Applications.", n, dir, inst, n)
	}
	if err := os.MkdirAll(s.d.Dir, 0o700); err != nil {
		return "", fmt.Errorf("%s cannot create %s: %v", n, s.d.Dir, err)
	}
	return inst, nil
}

func (s *Service) run(installed string) {
	s.out("Checking the releases of " + s.gh.Repo + " on GitHub")
	st, err := s.Check(context.Background())
	if err != nil {
		s.fail(err.Error() + s.unchanged())
		return
	}
	r := *st.Latest
	if st.UpToDate {
		s.out(s.d.AppName + " " + s.d.Version + " is the newest release (" + r.Tag + ")")
		s.finish("idle", s.d.AppName+" is up to date.")
		return
	}
	staged, err := s.fetch(r, installed)
	s.trash(s.workDir())
	if err != nil {
		_ = os.RemoveAll(s.stagedApp())
		s.fail(strings.TrimSuffix(err.Error(), ".") + "." + s.unchanged())
		return
	}

	s.setState("installing", s.d.AppName+" quits now and reopens when the new version is in place.")
	if err := s.install(os.Getpid(), staged, installed, s.d.LogPath); err != nil {
		_ = os.RemoveAll(staged)
		s.fail(fmt.Sprintf("%s could not start the installer: %v.%s", s.d.AppName, err, s.unchanged()))
		return
	}
	time.Sleep(quitDelay)
	s.d.Quit()
}

// fetch downloads release r, checks it and returns the staged app.
func (s *Service) fetch(r Release, installed string) (string, error) {
	work := s.workDir()
	s.trash(work)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", err
	}

	s.setState("downloading", "")
	s.out(fmt.Sprintf("Downloading %s · %s (%d MB)", r.Tag, r.Asset.Name, r.Asset.Size>>20))
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	sums := map[string]string{}
	err := s.gh.Download(ctx, r.Sums, func(rd io.Reader) (err error) {
		sums, err = parseSums(rd)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("Downloading %s failed: %w", sumsName, err)
	}
	want, ok := sums[r.Asset.Name]
	if !ok {
		return "", fmt.Errorf("%s has no line for %s", sumsName, r.Asset.Name)
	}
	zip := filepath.Join(work, filepath.Base(r.Asset.Name))
	err = s.gh.Download(ctx, r.Asset, func(rd io.Reader) error { return writeFile(zip, rd, maxDownload) })
	if err != nil {
		return "", fmt.Errorf("Downloading %s failed: %w", r.Asset.Name, err)
	}

	s.setState("verifying", "")
	got, err := hashFile(zip)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("The download does not match %s (got %s, want %s), so %s did not use it", sumsName, got[:12], want[:12], s.d.AppName)
	}
	s.out("SHA-256 matches " + sumsName)

	vctx, vcancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer vcancel()
	unpacked := filepath.Join(work, "unpacked")
	if err := s.unzip(vctx, zip, unpacked); err != nil {
		return "", fmt.Errorf("Unpacking %s failed: %w", r.Asset.Name, err)
	}
	next := filepath.Join(unpacked, s.d.AppName+".app")
	if fi, err := os.Stat(filepath.Join(next, "Contents", "MacOS", s.d.AppName)); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s has no %s.app at its top level", r.Asset.Name, s.d.AppName)
	}
	if err := checkSignature(vctx, s.signer, installed, next); err != nil {
		return "", fmt.Errorf("The new version failed its signature check: %w", err)
	}
	s.out("Code signature checked")

	staged := s.stagedApp()
	_ = os.RemoveAll(staged)
	if err := os.Rename(next, staged); err != nil {
		return "", fmt.Errorf("%s could not move the new app into place: %w", s.d.AppName, err)
	}
	now := time.Now()
	_ = os.Chtimes(staged, now, now) // CleanLeftovers judges staleness by this time
	s.out("Staged " + staged)
	return staged, nil
}

// translocated reports whether macOS App Translocation runs app from a read-only random path.
// Gatekeeper does this for a quarantined app opened where it was unpacked; moving the app in
// Finder ends it.
func translocated(app string) bool { return strings.Contains(app, "/AppTranslocation/") }

// writeFile copies at most limit bytes of r to path.
func writeFile(path string, r io.Reader, limit int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > limit {
		err = fmt.Errorf("the file is larger than %d MB", limit>>20)
	}
	return err
}

// ditto unpacks a zip made by `ditto -c -k --keepParent`, keeping the bundle's signature,
// symlinks and permissions intact.
func ditto(ctx context.Context, zip, dir string) error {
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", zip, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Service) out(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.lines = append(s.lines, line)
	if n := len(s.lines) - maxLines; n > 0 {
		s.lines = s.lines[n:]
	}
	s.emit(EventOutput, map[string]any{"seq": s.seq, "line": line})
}

func (s *Service) emit(event string, data any) {
	if s.d.Emit != nil {
		s.d.Emit(event, data)
	}
}

func (s *Service) setStateLocked(st, m string) {
	s.state, s.msg = st, m
	s.emit(EventState, map[string]any{"state": st, "message": m})
}

func (s *Service) setState(st, m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setStateLocked(st, m)
}

func (s *Service) finish(st, m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setStateLocked(st, m)
	s.busy = false
}

func (s *Service) fail(m string) { s.finish("failed", m) }
