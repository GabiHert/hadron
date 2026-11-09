package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/vt"
	cpty "github.com/creack/pty"
)

// Liveness values. They match domain.Liveness*; pty does not import domain.
const (
	Working = "working"
	Idle    = "idle"
	Waiting = "waiting"
	Ended   = "ended"
)

// Defaults.
const (
	DefaultCols     = 120
	DefaultRows     = 36
	ScrollbackBytes = 2 << 20
	WorkingWindow   = 3 * time.Second
	emuScrollback   = 5000
)

// Input delivery. The kernel hands a PTY write to the child in pieces of about a kilobyte, and the
// Claude CLI drops everything before the last piece of a paste it is still applying, so text is
// framed as one bracketed paste when the child asked for the mode, and cut into short chunks with a
// pause between them. Ideas from quant's input.go (MIT, Luis Brienze); see NOTICE.md.
const (
	pasteBegin = "\x1b[200~"
	pasteEnd   = "\x1b[201~"

	inputChunkBytes = 512
)

// Timings that tests shorten.
var (
	inputChunkGap = 5 * time.Millisecond
	// submitDelay separates the end of a paste from the Enter that submits it. Sent at once, the
	// CLI treats the Enter as part of the paste.
	submitDelay = 120 * time.Millisecond
	// stopGrace is how long Stop waits after SIGHUP before it kills the process group.
	stopGrace = 2 * time.Second
)

// ErrEnded is returned by writes to a session whose process has exited.
var ErrEnded = errors.New("the session has ended. Restart it first")

// Options says how to start a session.
type Options struct {
	Command string
	Args    []string
	Cwd     string
	// Env is the full environment of the child. Nil means the app's own.
	Env        []string
	Cols, Rows int
	// WorkingWindow is how long after output the session still counts as working. Zero means 3 s.
	WorkingWindow time.Duration

	// OnData gets every output chunk with the running byte offset after it. It runs on the read
	// loop, so it must not block.
	OnData func(data []byte, end int64)
	// OnLiveness gets each change of liveness, and OnExit the exit code once. Both run on one
	// goroutine that is never the read loop, so they may take locks the writers of input hold.
	OnLiveness func(liveness string)
	OnExit     func(code int)
}

// Session is one running process on a PTY.
type Session struct {
	opts Options
	cmd  *exec.Cmd
	ptmx *os.File

	wmu sync.Mutex // serializes input, so lines never interleave

	mu       sync.Mutex // guards everything below
	ring     *ring
	modes    *decModes
	emu      *vt.Emulator
	cols     int
	rows     int
	lastOut  time.Time
	waiting  bool
	ended    bool
	exitCode int
	window   time.Duration
	timer    *time.Timer

	notify chan struct{}
	done   chan struct{}
}

// Start spawns the command on a new PTY.
func Start(o Options) (*Session, error) {
	if o.Command == "" {
		return nil, errors.New("no command to run")
	}
	if o.Cols <= 0 {
		o.Cols = DefaultCols
	}
	if o.Rows <= 0 {
		o.Rows = DefaultRows
	}
	window := o.WorkingWindow
	if window <= 0 {
		window = WorkingWindow
	}
	cmd := exec.Command(o.Command, o.Args...)
	cmd.Dir = o.Cwd
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = withEnv(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: uint16(o.Cols), Rows: uint16(o.Rows)})
	if err != nil {
		return nil, fmt.Errorf("could not start %s: %w", o.Command, err)
	}
	emu := vt.NewEmulator(o.Cols, o.Rows)
	emu.SetScrollbackSize(emuScrollback)
	s := &Session{
		opts: o, cmd: cmd, ptmx: ptmx, ring: newRing(ScrollbackBytes), modes: newDecModes(), emu: emu,
		cols: o.Cols, rows: o.Rows, window: window,
		notify: make(chan struct{}, 1), done: make(chan struct{}),
	}
	// The emulator answers terminal queries (device attributes, cursor position) on a pipe that
	// blocks its Write until somebody reads. The attached terminal answers the child itself, so
	// the emulator's replies are dropped.
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := emu.Read(buf); err != nil {
				return
			}
		}
	}()
	go s.notifier()
	go s.readLoop()
	return s, nil
}

// withEnv returns env with the given KEY=VALUE pairs set, replacing earlier values of the same key.
func withEnv(env []string, set ...string) []string {
	skip := map[string]bool{}
	for _, kv := range set {
		k, _, _ := strings.Cut(kv, "=")
		skip[k] = true
	}
	out := make([]string, 0, len(env)+len(set))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !skip[k] {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}

func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.ingest(buf[:n], true)
		}
		if err != nil {
			break
		}
	}
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		code = -1
		if ps := s.cmd.ProcessState; ps != nil {
			code = ps.ExitCode()
			if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				code = 128 + int(ws.Signal())
			}
		}
	}
	// The notice is part of the output, so a terminal that mounts later still sees it.
	s.ingest([]byte(fmt.Sprintf("\r\n[process exited with code %d]\r\n", code)), false)
	s.mu.Lock()
	s.ended, s.exitCode = true, code
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	_ = s.ptmx.Close()
	// Closing the emulator would race with the drain goroutine's read of its closed flag, so close
	// the pipe the drain reads instead.
	if pw, ok := s.emu.InputPipe().(*io.PipeWriter); ok {
		_ = pw.CloseWithError(io.EOF)
	}
	s.signal()
	close(s.done)
}

// ingest records one chunk of output. Real output counts as activity; the exit notice does not.
func (s *Session) ingest(chunk []byte, activity bool) {
	data := append([]byte(nil), chunk...)
	s.mu.Lock()
	s.ring.write(data)
	s.modes.feed(data)
	_, _ = s.emu.Write(data)
	end := s.ring.total
	if activity {
		s.lastOut = time.Now()
		if s.timer == nil {
			s.timer = time.AfterFunc(s.window, s.signal)
		} else {
			s.timer.Reset(s.window)
		}
	}
	s.mu.Unlock()
	if s.opts.OnData != nil {
		s.opts.OnData(data, end)
	}
	if activity {
		s.signal()
	}
}

func (s *Session) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// notifier reports liveness changes and the exit, away from the read loop.
func (s *Session) notifier() {
	last := Idle
	for range s.notify {
		cur := s.Liveness()
		if cur != last {
			last = cur
			if s.opts.OnLiveness != nil {
				s.opts.OnLiveness(cur)
			}
		}
		if cur == Ended {
			if s.opts.OnExit != nil {
				s.opts.OnExit(s.ExitCode())
			}
			return
		}
	}
}

// Liveness is ended after the process exits, waiting while a question is open, working while
// output arrived within the working window, and idle otherwise.
func (s *Session) Liveness() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.ended:
		return Ended
	case s.waiting:
		return Waiting
	case !s.lastOut.IsZero() && time.Since(s.lastOut) < s.window:
		return Working
	}
	return Idle
}

// SetWaiting marks the session as waiting on the user (an open question), or clears it. The chat
// layer calls it.
func (s *Session) SetWaiting(waiting bool) {
	s.mu.Lock()
	changed := s.waiting != waiting
	s.waiting = waiting
	s.mu.Unlock()
	if changed {
		s.signal()
	}
}

// PID is the child's process id.
func (s *Session) PID() int { return s.cmd.Process.Pid }

// Done is closed when the process has exited and its output is fully read.
func (s *Session) Done() <-chan struct{} { return s.done }

// ExitCode is the exit code, meaningful once Done is closed. A signal gives 128 plus its number.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Size is the terminal size.
func (s *Session) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// Write sends raw input, such as the user's keystrokes from the attached terminal.
func (s *Session) Write(data []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.writeFrames(chunk(string(data)))
}

// Send types text into the session like a paste, then presses Enter when enter is true. The text is
// framed as a bracketed paste when the child asked for the mode (a plain shell would print the
// markers). The whole call holds the input lock, so concurrent sends never interleave.
func (s *Session) Send(text string, enter bool) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.mu.Lock()
	paste := s.modes.enabled(modeBracketedPaste)
	s.mu.Unlock()
	framed := paste && text != "" && utf8.RuneCountInString(text) > 1 && !strings.HasPrefix(text, "\x1b")
	if framed {
		text = pasteBegin + text + pasteEnd
	}
	if err := s.writeFrames(chunk(text)); err != nil {
		return err
	}
	if enter {
		if framed {
			time.Sleep(submitDelay)
		}
		return s.writeFrames([]string{"\r"})
	}
	return nil
}

// Keys presses named keys: Enter, Escape, Up, Ctrl-C, or a single character.
func (s *Session) Keys(keys []string) error {
	b, err := keyBytes(keys)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return s.writeFrames([]string{b})
}

// Interrupt sends Ctrl-C.
func (s *Session) Interrupt() error { return s.Keys([]string{"Ctrl-C"}) }

func (s *Session) writeFrames(frames []string) error {
	if s.isEnded() {
		return ErrEnded
	}
	for i, f := range frames {
		if i > 0 {
			time.Sleep(inputChunkGap)
		}
		if _, err := s.ptmx.Write([]byte(f)); err != nil {
			if s.isEnded() {
				return ErrEnded
			}
			return err
		}
	}
	return nil
}

func (s *Session) isEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// chunk cuts data into writes of at most inputChunkBytes, never inside a UTF-8 sequence.
func chunk(data string) []string {
	var out []string
	for len(data) > 0 {
		n := inputChunkBytes
		if n >= len(data) {
			out = append(out, data)
			break
		}
		for n > 0 && !utf8.RuneStart(data[n]) {
			n--
		}
		if n == 0 {
			n = inputChunkBytes
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// Resize changes the terminal size.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return fmt.Errorf("size %dx%d is out of range: use 1 to 1000 for columns and rows", cols, rows)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return ErrEnded
	}
	if err := cpty.Setsize(s.ptmx, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	s.cols, s.rows = cols, rows
	s.emu.Resize(cols, rows)
	return nil
}

// Scrollback returns the output to replay into a terminal that just mounted, and the byte offset it
// ends at. When older output has been dropped it starts with the DEC modes the child set, so the
// terminal is in the state the child believes it is in.
func (s *Session) Scrollback() (data []byte, end int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data = s.ring.bytes()
	if s.ring.wrapped() {
		data = append(s.modes.preamble(), data...)
	}
	return data, s.ring.total
}

// Screen returns the last lines lines of the visible screen as plain text, with trailing blank
// lines removed. With scrollback it includes the lines that scrolled off first.
func (s *Session) Screen(lines int, scrollback bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []string
	if scrollback && !s.emu.IsAltScreen() {
		for _, l := range s.emu.Scrollback().Lines() {
			all = append(all, strings.TrimRight(l.String(), " "))
		}
	}
	for _, l := range strings.Split(s.emu.String(), "\n") {
		all = append(all, strings.TrimRight(l, " \r"))
	}
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// Stop asks the process group to hang up, and kills it if it is still there after a grace period.
func (s *Session) Stop() {
	select {
	case <-s.done:
		return
	default:
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	_ = syscall.Kill(pid, syscall.SIGHUP)
	go func() {
		select {
		case <-s.done:
		case <-time.After(stopGrace):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()
}
