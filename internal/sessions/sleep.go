package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/store"
)

// A card's session can sleep: Hadron stops its claude process to give its memory back, keeps the
// conversation id, and marks the card asleep. The next message to it (session_send, an answer to
// a question, keys typed in its terminal) wakes it first: the conversation is resumed with
// --resume, and the message goes in once Claude's prompt shows. The planner never sleeps.

// Settings keys.
const (
	// SettingSleepAfter is how many idle minutes put a card's session to sleep; 0 is never.
	SettingSleepAfter = "sessions.sleep_after_minutes"
	// SettingMemoryGuard says whether the memory guard sleeps idle sessions when macOS reports
	// memory pressure. On unless false.
	SettingMemoryGuard = "sessions.memory_guard"
)

// DefaultSleepAfterMinutes is the idle time before a session sleeps when the setting is not set.
const DefaultSleepAfterMinutes = 30

// Why a session went to sleep.
const (
	SleepIdle   = "idle"
	SleepMemory = "memory"
	SleepAsked  = "asked"
)

// GuardMinIdle is how long a session must have been quiet before the memory guard may put it to
// sleep: a session between two steps of its work is left alone.
const GuardMinIdle = 2 * time.Minute

// DefaultGuardSettle is how long the guard waits after a sleep for the pressure to ease before it
// sleeps the next session.
const DefaultGuardSettle = 5 * time.Second

// childSample and childBusyCPU tune the check for work under a session: the processes below it
// are sampled for childSample, and childBusyCPU of CPU in that time means they are working.
const (
	childSample  = time.Second
	childBusyCPU = 50 * time.Millisecond
)

// SleepAfter reads the setting: how long a session must be idle before it sleeps; 0 is never.
func SleepAfter(st *store.Store) time.Duration {
	n := float64(DefaultSleepAfterMinutes)
	if raw, err := st.Settings.Get(SettingSleepAfter); err == nil {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			switch x := v.(type) {
			case float64:
				n = x
			case string:
				if strings.EqualFold(strings.TrimSpace(x), "never") {
					n = 0
				}
			case nil:
			}
		}
	}
	if n <= 0 {
		return 0
	}
	return time.Duration(n * float64(time.Minute))
}

// MemoryGuardOn reads the setting.
func MemoryGuardOn(st *store.Store) bool {
	if raw, err := st.Settings.Get(SettingMemoryGuard); err == nil {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b
		}
	}
	return true
}

// ErrNotAsleep says a session that was to be woken is not asleep.
var ErrNotAsleep = errors.New("the session is not asleep")

// Sleeper is a card session that is running, with what keeps it awake.
type Sleeper struct {
	Session domain.Session
	Card    domain.Card
	// Idle is how long it has been quiet (no output, no input).
	Idle time.Duration
	// Blocker says why it cannot sleep now; empty when it can.
	Blocker string
}

// childBusy reports whether processes under pid are working.
func (s *Service) childBusy(pid int) bool {
	if s.cfg.ChildBusy != nil {
		return s.cfg.ChildBusy(pid)
	}
	return pty.ChildrenBusy(pid, childSample, childBusyCPU)
}

// pressure is the system's memory pressure level.
func (s *Service) pressure() int {
	if s.cfg.Pressure != nil {
		return s.cfg.Pressure()
	}
	return pty.MemoryPressure()
}

// SetPressureForTest swaps the memory pressure reading. Tests use it.
func (s *Service) SetPressureForTest(f func() int) { s.cfg.Pressure = f }

// SetGuardMinIdleForTest swaps how long a session must be quiet before the guard may sleep it.
func (s *Service) SetGuardMinIdleForTest(d time.Duration) { s.cfg.GuardMinIdle = d }

// Pressure is the system's memory pressure level (pty.PressureNormal, Warning or Critical).
func (s *Service) Pressure() int { return s.pressure() }

// Sleepers lists the running card sessions with how long each has been idle and, when it cannot
// sleep, why. With children it also samples the processes under each one (a second each), which
// is the slow check; without it a session whose children are working may show no blocker.
func (s *Service) Sleepers(children bool) []Sleeper {
	var out []Sleeper
	for _, row := range s.LiveSessions() {
		if row.CardID == "" || row.Kind != "card" || !s.isLatest(row, row.CardID) {
			continue
		}
		ps, ok := s.pty.Get(row.ID)
		if !ok {
			continue
		}
		card, err := s.cfg.Store.Cards.Get(row.CardID)
		if err != nil {
			continue
		}
		out = append(out, Sleeper{Session: row, Card: card, Idle: ps.IdleFor(), Blocker: s.blocker(row, ps, false)})
	}
	if children {
		for i := range out {
			if out[i].Blocker == "" {
				out[i].Blocker = s.childBlocker(out[i].Session)
			}
		}
	}
	return out
}

// blocker says why a card's running session must stay awake, or "" when it may sleep. It does not
// look at the processes under it (childBlocker does).
func (s *Service) blocker(row domain.Session, ps *pty.Session, _ bool) string {
	select {
	case <-ps.Done():
		return "it is not running"
	default:
	}
	if row.CardID == "" {
		return "the planner never sleeps"
	}
	if !isClaude(row.Command) || row.ClaudeSessionID == "" {
		return "it does not run claude, so it could not be resumed"
	}
	switch ps.Liveness() {
	case pty.Working:
		return "it is working"
	case pty.Waiting:
		return "it has a question waiting for an answer"
	}
	if open, err := s.cfg.Store.Questions.List(row.WorkspaceID, row.CardID, "open"); err == nil && len(open) > 0 {
		return "it has a question waiting for an answer"
	}
	if !ScreenShowsInput(ps.Screen(0, false)) {
		return "it is not at Claude's prompt (a dialog or menu may be open)"
	}
	return ""
}

// childBlocker samples the processes under a session.
func (s *Service) childBlocker(row domain.Session) string {
	ps, ok := s.pty.Get(row.ID)
	if !ok {
		return "it is not running"
	}
	if s.childBusy(ps.PID()) {
		return "a process it started (a test, a build) is still working"
	}
	return ""
}

// SleepCard puts a card's running session to sleep now, whatever its idle time, unless something
// keeps it awake (it is working, has a question open, shows a dialog, or a process under it is
// working). The planner never sleeps.
func (s *Service) SleepCard(st *store.Store, workspaceID, cardID string) error {
	if cardID == "" {
		return errors.New("the planner never sleeps")
	}
	t, err := s.Find(st, workspaceID, cardID)
	if err != nil {
		return err
	}
	if t.Asleep {
		return nil
	}
	if t.Live == nil {
		return errors.New("its session is not running")
	}
	if why := s.blocker(t.Row, t.Live, false); why != "" {
		return fmt.Errorf("it cannot sleep now: %s", why)
	}
	if why := s.childBlocker(t.Row); why != "" {
		return fmt.Errorf("it cannot sleep now: %s", why)
	}
	s.sleep(t.Row, t.Live, SleepAsked, 0)
	return nil
}

// sleep stops a session as asleep and waits until its row says so.
func (s *Service) sleep(row domain.Session, ps *pty.Session, why string, idle time.Duration) {
	s.mu.Lock()
	s.asleep[row.ID] = sleepNote{why: why, idle: idle}
	s.mu.Unlock()
	s.cfg.Log.Info("session sleeps", "session", row.ID, "card", row.CardID, "why", why, "idle", idle.Round(time.Second).String())
	ps.Stop()
	select {
	case <-ps.Done():
	case <-time.After(15 * time.Second):
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cur, err := s.cfg.Store.Sessions.Get(row.ID); err == nil && cur.Asleep {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Its scrollback and terminal model are dropped too: the card shows that it sleeps, and a wake
	// starts a new session that redraws the conversation.
	s.pty.Forget(row.ID)
}

type sleepNote struct {
	why  string
	idle time.Duration
}

// sleepEvent is the line kept on a card that went to sleep.
func sleepEvent(n sleepNote) string {
	wake := " A message wakes it: it resumes its conversation first."
	switch n.why {
	case SleepIdle:
		return fmt.Sprintf("Asleep after %s idle, to save memory.%s", humanMinutes(n.idle), wake)
	case SleepMemory:
		return "Asleep: the Mac was short of memory and this session was idle the longest." + wake
	}
	return "Asleep, as asked." + wake
}

func humanMinutes(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	if m >= 120 && m%60 == 0 {
		return fmt.Sprintf("%d hours", m/60)
	}
	return fmt.Sprintf("%d minutes", m)
}

// markAsleep finishes a sleep once the process is gone: the row and the card say asleep, and the
// card's chat says why and how it wakes. It reports false when the session was not put to sleep.
func (s *Service) markAsleep(row domain.Session, cardID string) bool {
	s.mu.Lock()
	n, ok := s.asleep[row.ID]
	delete(s.asleep, row.ID)
	s.mu.Unlock()
	if !ok || s.closing.Load() {
		return false
	}
	st := s.cfg.Store
	_ = st.Sessions.SetRestore(row.ID, false)
	if cur, err := st.Sessions.Get(row.ID); err == nil {
		now := time.Now().UTC()
		cur.Status, cur.EndedAt = "exited", &now
		_ = st.Sessions.Update(&cur)
	}
	_ = st.Sessions.SetAsleep(row.ID, true)
	if cardID != "" && s.isLatest(row, cardID) {
		_ = st.Cards.SetLiveness(cardID, domain.LivenessAsleep)
		ev := domain.Message{WorkspaceID: row.WorkspaceID, Channel: "card:" + cardID, Author: "system", Kind: "event", Text: sleepEvent(n), Delivered: true}
		_ = st.Messages.Create(&ev)
	}
	s.changed(row.WorkspaceID, "cards", "sessions", "messages")
	return true
}

// SleepIdle puts to sleep every card session that has been idle for the sleep_after setting and
// that nothing keeps awake. It returns the sessions it put to sleep. One run at a time.
func (s *Service) SleepIdle() []Sleeper {
	after := SleepAfter(s.cfg.Store)
	if after <= 0 || !s.sweeping.CompareAndSwap(false, true) {
		return nil
	}
	defer s.sweeping.Store(false)
	var slept []Sleeper
	for _, c := range s.Sleepers(false) {
		if c.Blocker != "" || c.Idle < after {
			continue
		}
		if why := s.childBlocker(c.Session); why != "" {
			continue
		}
		ps, ok := s.pty.Get(c.Session.ID)
		if !ok || s.blocker(c.Session, ps, false) != "" || ps.IdleFor() < after {
			continue // it woke up while the children were sampled
		}
		s.sleep(c.Session, ps, SleepIdle, ps.IdleFor())
		slept = append(slept, c)
	}
	return slept
}

// GuardResult is what one run of the memory guard did.
type GuardResult struct {
	// Level is the pressure it found (pty.PressureNormal, Warning or Critical; 0 unknown).
	Level int
	// Slept are the sessions it put to sleep, longest idle first.
	Slept []Sleeper
	// Awake are the sessions held awake by their work (each with its Blocker), set when it slept
	// none and none was merely active a moment ago: the caller warns.
	Awake []Sleeper
	// Warn is set the first time in a stretch of pressure that it could sleep nothing.
	Warn bool
}

// GuardTick runs the memory guard once: under warning pressure it puts the longest-idle sessions to
// sleep, one at a time, until the pressure eases or none is left. It never stops a session that is
// working. It does nothing while the setting is off or the pressure is normal.
func (s *Service) GuardTick() GuardResult {
	if !MemoryGuardOn(s.cfg.Store) {
		return GuardResult{}
	}
	level := s.pressure()
	res := GuardResult{Level: level}
	if level < pty.PressureWarning {
		s.guardWarned.Store(false)
		return res
	}
	if !s.sweeping.CompareAndSwap(false, true) {
		return res
	}
	defer s.sweeping.Store(false)
	all := s.Sleepers(false)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Idle > all[j].Idle })
	settle := s.cfg.GuardSettle
	if settle <= 0 {
		settle = DefaultGuardSettle
	}
	// What keeps the others awake: a real blocker (working, a question, a busy child), or only
	// that they were active a moment ago. The latter may sleep at the next run, so no warning yet.
	var blocked []Sleeper
	recent := 0
	for _, c := range all {
		if c.Blocker != "" {
			blocked = append(blocked, c)
			continue
		}
		if c.Idle < s.guardMinIdle() {
			recent++
			continue
		}
		if why := s.childBlocker(c.Session); why != "" {
			c.Blocker = why
			blocked = append(blocked, c)
			continue
		}
		ps, ok := s.pty.Get(c.Session.ID)
		if !ok {
			continue
		}
		if why := s.blocker(c.Session, ps, false); why != "" {
			c.Blocker = why
			blocked = append(blocked, c)
			continue
		}
		s.sleep(c.Session, ps, SleepMemory, ps.IdleFor())
		res.Slept = append(res.Slept, c)
		time.Sleep(settle)
		if res.Level = s.pressure(); res.Level < pty.PressureWarning {
			break
		}
	}
	if len(res.Slept) > 0 {
		s.guardWarned.Store(false)
		return res
	}
	// Warn only when every session is held awake by its work, once per stretch of pressure.
	if recent > 0 || len(blocked) == 0 {
		return res
	}
	res.Awake = blocked
	if !s.guardWarned.Swap(true) {
		res.Warn = true
	}
	return res
}

func (s *Service) guardMinIdle() time.Duration {
	if s.cfg.GuardMinIdle > 0 {
		return s.cfg.GuardMinIdle
	}
	return GuardMinIdle
}

// GuardNotices are the chat lines a guard run leaves, one per workspace: the planner's channel gets
// it, and the planner is told.
func GuardNotices(res GuardResult) map[string]string {
	out := map[string]string{}
	level := pty.PressureName(res.Level)
	if len(res.Slept) > 0 {
		by := map[string][]string{}
		for _, c := range res.Slept {
			by[c.Session.WorkspaceID] = append(by[c.Session.WorkspaceID], c.Card.Nick)
		}
		for ws, names := range by {
			out[ws] = fmt.Sprintf("The Mac is short of memory, so Hadron put these idle sessions to sleep, longest idle first: %s. A message to one wakes it.", strings.Join(names, ", "))
		}
		return out
	}
	if !res.Warn {
		return out
	}
	by := map[string][]string{}
	for _, c := range res.Awake {
		by[c.Session.WorkspaceID] = append(by[c.Session.WorkspaceID], c.Card.Nick)
	}
	for ws, names := range by {
		out[ws] = fmt.Sprintf("The Mac is short of memory (pressure: %s), and every session is busy, so Hadron put none to sleep. Busy: %s. Stopping one you do not need frees its memory.", level, strings.Join(names, ", "))
	}
	return out
}

// Wake brings a card's sleeping session back: it resumes the same Claude conversation and returns
// once Claude's prompt shows, so text sent next is not lost. Two wakes of one card at once share
// the same start. A session that is already running is returned as it is.
func (s *Service) Wake(ctx context.Context, workspaceID, cardID string) (Target, error) {
	s.mu.Lock()
	if op, ok := s.waking[cardID]; ok {
		s.mu.Unlock()
		select {
		case <-op.done:
			return op.t, op.err
		case <-ctx.Done():
			return Target{}, ctx.Err()
		}
	}
	op := &wakeOp{done: make(chan struct{})}
	s.waking[cardID] = op
	s.mu.Unlock()
	op.t, op.err = s.wake(ctx, workspaceID, cardID)
	s.mu.Lock()
	delete(s.waking, cardID)
	s.mu.Unlock()
	close(op.done)
	return op.t, op.err
}

type wakeOp struct {
	done chan struct{}
	t    Target
	err  error
}

func (s *Service) wake(ctx context.Context, workspaceID, cardID string) (Target, error) {
	st := s.cfg.Store
	t, err := s.Find(st, workspaceID, cardID)
	if err != nil {
		return t, err
	}
	if t.Live != nil {
		return t, nil
	}
	if !t.Asleep {
		return t, ErrNotAsleep
	}
	ws, err := st.Workspaces.Get(workspaceID)
	if err != nil {
		return t, err
	}
	card, err := st.Cards.Get(cardID)
	if err != nil {
		return t, err
	}
	var launch func()
	if err := st.Tx(func(tx *store.Store) error {
		var perr error
		_, launch, perr = s.Prepare(tx, Request{Workspace: ws, Card: &card, Restore: true})
		return perr
	}); err != nil {
		return t, fmt.Errorf("could not wake %q: %w", card.Nick, err)
	}
	launch()
	t, err = s.Find(st, workspaceID, cardID)
	if err != nil {
		return t, err
	}
	if t.Live == nil {
		why := t.Err
		if why == "" {
			why = "the session ended as it started"
		}
		return t, fmt.Errorf("could not wake %q: %s", card.Nick, why)
	}
	s.cfg.Log.Info("session woke", "session", t.Row.ID, "card", cardID, "claude_session", t.Row.ClaudeSessionID)
	ev := domain.Message{WorkspaceID: workspaceID, Channel: "card:" + cardID, Author: "system", Kind: "event", Text: "Woke up for a message: resumed its conversation.", Delivered: true}
	_ = st.Messages.Create(&ev)
	s.changed(workspaceID, "cards", "sessions", "messages")
	if err := s.waitPrompt(ctx, t.Live); err != nil {
		return t, fmt.Errorf("woke %q, but %w", card.Nick, err)
	}
	return t, nil
}

// waitPrompt waits for Claude's input box on a session that just started, up to ReadyTimeout.
func (s *Service) waitPrompt(ctx context.Context, ps *pty.Session) error {
	deadline := time.Now().Add(s.cfg.ReadyTimeout)
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if ScreenShowsInput(ps.Screen(0, false)) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%w: Claude's prompt did not show within %s of waking", ErrNotReady, s.cfg.ReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ps.Done():
			return fmt.Errorf("%w: the session ended before Claude was ready", ErrNotReady)
		case <-tick.C:
		}
	}
}

// WakeAndSend wakes a card's session when it sleeps and types text into it once its prompt
// shows. It is for deliveries that cannot wait on the caller (an answer to a question): it logs a
// failure and leaves a line on the card.
func (s *Service) WakeAndSend(workspaceID, cardID, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ReadyTimeout+time.Minute)
	defer cancel()
	t, err := s.Wake(ctx, workspaceID, cardID)
	if err == nil {
		err = t.Live.Send(text, true)
	}
	if err != nil {
		s.cfg.Log.Warn("wake and send", "card", cardID, "error", err.Error())
		ev := domain.Message{WorkspaceID: workspaceID, Channel: "card:" + cardID, Author: "system", Kind: "event", Text: "Could not wake the session to deliver a message: " + err.Error(), Delivered: true}
		_ = s.cfg.Store.Messages.Create(&ev)
		s.changed(workspaceID, "messages")
	}
}
