package actions

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/testwait"
)

// sleepHarness is a session harness whose child check and memory pressure the test controls. A
// nil busy uses the real check (CPU sampled under the session).
func sleepHarness(t *testing.T, busy func(int) bool, pressure func() int, extra ...string) *sessionHarness {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	return newSessionHarnessWith(t, false, testwait.Scaled(5*time.Second), func(c *sessions.Config) {
		c.ChildBusy = busy
		c.Pressure = pressure
		c.GuardSettle = 10 * time.Millisecond
		c.GuardMinIdle = 200 * time.Millisecond
	}, extra...)
}

func idleChildren(int) bool { return false }

// startedCard creates a card and waits until its fake claude shows the prompt and goes quiet.
func (h *sessionHarness) startedCard(w domain.Workspace, nick string) domain.Card {
	h.t.Helper()
	var c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": nick}, &c)
	waitUntil(h.t, nick+" at its prompt", func() bool {
		t, err := h.svc.Find(h.reg.Deps().Store, w.ID, c.ID)
		return err == nil && t.Live != nil && sessions.ScreenShowsInput(t.Live.Screen(0, false)) && t.Live.Liveness() == pty.Idle
	})
	return c
}

func (h *sessionHarness) sleepAfter(minutes float64) {
	h.do("settings_update", m{"values": m{sessions.SettingSleepAfter: minutes}}, nil)
}

func (h *sessionHarness) cardEvents(w domain.Workspace, c domain.Card) string {
	msgs, err := h.reg.Deps().Store.Messages.List(w.ID, "card:"+c.ID, "")
	if err != nil {
		h.t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Text + "\n")
	}
	return b.String()
}

func TestAnIdleCardSessionSleepsAndTheCardSaysAsleep(t *testing.T) {
	h := sleepHarness(t, idleChildren, nil)
	w := h.workspace("Acme")
	card := h.startedCard(w, "billing-fix")

	// Not yet idle for long enough: nothing sleeps.
	h.sleepAfter(30)
	if slept := h.svc.SleepIdle(); len(slept) != 0 {
		t.Fatalf("slept %d sessions before 30 idle minutes", len(slept))
	}
	// "never" turns it off.
	h.sleepAfter(0)
	time.Sleep(50 * time.Millisecond)
	if slept := h.svc.SleepIdle(); len(slept) != 0 {
		t.Fatal("a session slept with the setting at never")
	}

	h.sleepAfter(0.002) // 120 ms
	time.Sleep(200 * time.Millisecond)
	slept := h.svc.SleepIdle()
	if len(slept) != 1 || slept[0].Card.ID != card.ID {
		t.Fatalf("slept = %+v, want billing-fix", slept)
	}
	st := h.status("billing-fix")
	if !st.Asleep || st.Running || st.Liveness != domain.LivenessAsleep {
		t.Fatalf("status = %+v, want asleep", st)
	}
	if got := h.card(card.ID).Liveness; got != domain.LivenessAsleep {
		t.Fatalf("card liveness = %q, want asleep", got)
	}
	row := h.latest(w.ID, card.ID)
	if !row.Asleep || row.Status != "exited" || row.Restore {
		t.Fatalf("session row = status %q restore %v, want asleep and not restored at launch", row.Status, row.Restore)
	}
	if ev := h.cardEvents(w, card); !strings.Contains(ev, "Asleep after") || !strings.Contains(ev, "A message wakes it") {
		t.Fatalf("the card's chat does not say it sleeps:\n%s", ev)
	}
	var out SleepStatusOut
	h.do("session_sleep_status", m{}, &out)
	if len(out.Asleep) != 1 || out.Asleep[0].Nick != "billing-fix" || len(out.Awake) != 0 {
		t.Fatalf("session_sleep_status = %+v", out)
	}
}

func TestAMessageWakesASleepingSessionAndArrivesExactlyOnce(t *testing.T) {
	h := sleepHarness(t, idleChildren, nil)
	w := h.workspace("Acme")
	card := h.startedCard(w, "billing-fix")
	before := h.latest(w.ID, card.ID)
	writeConversation(t, os.Getenv("CLAUDE_CONFIG_DIR"), w.Root, before.ClaudeSessionID, "hello", 3000, time.Minute)

	h.do("session_sleep", m{"card": "billing-fix"}, nil)
	if !h.status("billing-fix").Asleep {
		t.Fatal("session_sleep did not put it to sleep")
	}
	starts := len(h.history())

	// Two messages at once: one wake, and each message typed once.
	var wg sync.WaitGroup
	for _, text := range []string{"first message", "second message"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			if err := h.as(You, "session_send", m{"card": "billing-fix", "text": text}, nil); err != nil {
				t.Error(err)
			}
		}(text)
	}
	wg.Wait()
	waitUntil(t, "both messages in the session", func() bool {
		in := h.file(".in")
		return strings.Contains(in, "first message") && strings.Contains(in, "second message")
	})
	time.Sleep(300 * time.Millisecond) // a late duplicate would show by now
	in := h.file(".in")
	if strings.Count(in, "first message") != 1 || strings.Count(in, "second message") != 1 {
		t.Fatalf("each message must arrive exactly once:\n%s", in)
	}
	hist := h.history()
	if len(hist) != starts+1 {
		t.Fatalf("the wake started %d processes, want 1: %v", len(hist)-starts, hist)
	}
	if want := "card --resume " + before.ClaudeSessionID; hist[len(hist)-1] != want {
		t.Fatalf("woke with %q, want %q (the same conversation)", hist[len(hist)-1], want)
	}
	st := h.status("billing-fix")
	if !st.Running || st.Asleep || h.card(card.ID).Liveness == domain.LivenessAsleep {
		t.Fatalf("after the wake status = %+v", st)
	}
	if !strings.Contains(h.cardEvents(w, card), "Woke up for a message") {
		t.Fatal("the card's chat does not say it woke")
	}
}

func TestTypingInASleepingCardsTerminalWakesItAndKeepsTheKeysInOrder(t *testing.T) {
	h := sleepHarness(t, idleChildren, nil)
	w := h.workspace("Acme")
	card := h.startedCard(w, "billing-fix")
	h.do("session_sleep", m{"card": "billing-fix"}, nil)
	old := h.latest(w.ID, card.ID)
	for _, k := range []string{"h", "e", "l", "l", "o", "\r"} {
		if err := h.svc.Write(old.ID, []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, "the typed line in the woken session", func() bool { return strings.Contains(h.file(".in"), "hello") })
	if n := strings.Count(h.file(".in"), "hello"); n != 1 {
		t.Fatalf("hello arrived %d times", n)
	}
}

func TestAnAnswerWakesTheSleepingAskingSession(t *testing.T) {
	h := sleepHarness(t, idleChildren, nil)
	w := h.workspace("Acme")
	card := h.startedCard(w, "billing-fix")
	session := Actor{Kind: KindSession, Name: card.Nick, WorkspaceID: w.ID, CardID: card.ID}
	var q domain.Question
	if err := h.as(session, "question_ask", deliveryQuestion, &q); err != nil {
		t.Fatal(err)
	}
	// An open question keeps it awake.
	if err := h.as(You, "session_sleep", m{"card": "billing-fix"}, nil); err == nil || !strings.Contains(err.Error(), "question") {
		t.Fatalf("session_sleep with a question open = %v, want refused", err)
	}
	// Once answered it may sleep; a question it asks later is answered while it sleeps, and the
	// answer wakes it.
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Nearest"}}, nil)
	waitUntil(t, "the first answer in the session", func() bool { return strings.Contains(h.file(".in"), "Q-01: Nearest") })
	waitUntil(t, "quiet again", func() bool {
		t0, err := h.svc.Find(h.reg.Deps().Store, w.ID, card.ID)
		return err == nil && t0.Live != nil && t0.Live.Liveness() == pty.Idle
	})
	h.do("session_sleep", m{"card": "billing-fix"}, nil)
	var q2 domain.Question
	if err := h.as(session, "question_ask", deliveryQuestion, &q2); err != nil {
		t.Fatal(err)
	}
	h.do("question_answer", m{"question_id": q2.ID, "selections": []string{"Nearest"}}, nil)
	waitUntil(t, "the answer in the woken session", func() bool { return strings.Contains(h.file(".in"), "Q-02: Nearest") })
	if n := strings.Count(h.file(".in"), "Q-02: Nearest"); n != 1 {
		t.Fatalf("the answer arrived %d times", n)
	}
}

func TestASessionWithAWorkingChildProcessDoesNotSleep(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "busy")
	if err := os.WriteFile(flag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := sleepHarness(t, nil, nil, "HADRON_TEST_BUSY="+flag) // the real check: CPU under the session
	w := h.workspace("Acme")
	h.startedCard(w, "billing-fix")
	h.sleepAfter(0.002)
	time.Sleep(200 * time.Millisecond)

	if slept := h.svc.SleepIdle(); len(slept) != 0 {
		t.Fatal("a session slept while a test was still running under it")
	}
	if err := h.as(You, "session_sleep", m{"card": "billing-fix"}, nil); err == nil || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("session_sleep with a busy child = %v, want refused", err)
	}
	var out SleepStatusOut
	h.do("session_sleep_status", m{"children": true}, &out)
	if len(out.Awake) != 1 || !strings.Contains(out.Awake[0].Blocker, "still working") {
		t.Fatalf("session_sleep_status = %+v, want the child named as the blocker", out)
	}

	// The test finishes: now it sleeps.
	_ = os.Remove(flag)
	time.Sleep(300 * time.Millisecond)
	if slept := h.svc.SleepIdle(); len(slept) != 1 {
		t.Fatalf("slept %d after the child finished, want 1", len(slept))
	}
}

func TestTheMemoryGuardSleepsTheLongestIdleFirstUntilThePressureEases(t *testing.T) {
	var level atomic.Int32
	level.Store(pty.PressureNormal)
	h := sleepHarness(t, idleChildren, func() int { return int(level.Load()) })
	w := h.workspace("Acme")
	h.sleepAfter(0) // only the guard sleeps here
	oldest := h.startedCard(w, "oldest")
	time.Sleep(150 * time.Millisecond)
	middle := h.startedCard(w, "middle")
	time.Sleep(150 * time.Millisecond)
	newest := h.startedCard(w, "newest")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	time.Sleep(300 * time.Millisecond)
	// middle is touched last, so it is now the least idle of the three.
	h.do("session_send", m{"card": "middle", "text": "still here"}, nil)
	time.Sleep(250 * time.Millisecond)

	if res := h.svc.GuardTick(); len(res.Slept) != 0 {
		t.Fatal("the guard slept a session under normal pressure")
	}
	// Warning pressure that eases after the second sleep.
	level.Store(pty.PressureWarning)
	calls := 0
	h.svc.SetPressureForTest(func() int {
		calls++
		if calls >= 3 {
			return pty.PressureNormal
		}
		return pty.PressureWarning
	})
	res := h.svc.GuardTick()
	var names []string
	for _, s := range res.Slept {
		names = append(names, s.Card.Nick)
	}
	if strings.Join(names, ",") != "oldest,newest" {
		t.Fatalf("the guard slept %v, want oldest then newest (longest idle first), stopping when the pressure eased", names)
	}
	if !h.status("oldest").Asleep || !h.status("newest").Asleep || h.status("middle").Asleep {
		t.Fatal("the wrong sessions sleep")
	}
	if pl := h.latest(w.ID, ""); pl.Status != "running" {
		t.Fatalf("the planner's session is %q, want running: it never sleeps", pl.Status)
	}
	notes := sessions.GuardNotices(res)
	if !strings.Contains(notes[w.ID], "oldest, newest") {
		t.Fatalf("the notice does not name the cards: %v", notes)
	}
	_, _, _ = oldest, middle, newest
}

func TestTheMemoryGuardOnlyWarnsWhenEverySessionIsBusy(t *testing.T) {
	h := sleepHarness(t, func(int) bool { return true }, func() int { return pty.PressureWarning })
	w := h.workspace("Acme")
	h.sleepAfter(0)
	h.startedCard(w, "billing-fix")
	time.Sleep(300 * time.Millisecond)

	res := h.svc.GuardTick()
	if len(res.Slept) != 0 || !res.Warn {
		t.Fatalf("guard = %+v, want a warning and nothing asleep", res)
	}
	if note := sessions.GuardNotices(res)[w.ID]; !strings.Contains(note, "billing-fix") || !strings.Contains(note, "none to sleep") {
		t.Fatalf("warning = %q", note)
	}
	if h.status("billing-fix").Asleep {
		t.Fatal("a working session was stopped")
	}
	// It warns once per stretch of pressure, not every tick.
	if again := h.svc.GuardTick(); again.Warn {
		t.Fatal("the guard warned twice in one stretch of pressure")
	}
	// Off means off.
	h.do("settings_update", m{"values": m{sessions.SettingMemoryGuard: false}}, nil)
	if off := h.svc.GuardTick(); off.Level != 0 || len(off.Slept) != 0 {
		t.Fatalf("guard with the setting off = %+v", off)
	}
}

func TestTheMemoryGuardDoesNotCallARecentlyActiveSessionBusy(t *testing.T) {
	h := sleepHarness(t, idleChildren, func() int { return pty.PressureWarning })
	w := h.workspace("Acme")
	h.sleepAfter(0)
	h.startedCard(w, "billing-fix")
	// Quiet, but for less than the guard's minimum: it is not busy, it just has not waited yet.
	h.svc.SetGuardMinIdleForTest(time.Hour)
	if res := h.svc.GuardTick(); len(res.Slept) != 0 || res.Warn || len(sessions.GuardNotices(res)) != 0 {
		t.Fatalf("guard = %+v, want neither a sleep nor a warning", res)
	}
	// Once it has been quiet long enough, it sleeps.
	h.svc.SetGuardMinIdleForTest(100 * time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	if res := h.svc.GuardTick(); len(res.Slept) != 1 {
		t.Fatalf("guard = %+v, want billing-fix asleep", res)
	}
}

func TestThePlannerNeverSleeps(t *testing.T) {
	h := sleepHarness(t, idleChildren, func() int { return pty.PressureCritical })
	w := h.workspace("Acme")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the planner running", func() bool { return len(h.svc.LiveSessions()) == 1 })
	h.sleepAfter(0.001)
	time.Sleep(400 * time.Millisecond)

	if slept := h.svc.SleepIdle(); len(slept) != 0 {
		t.Fatal("the idle sleeper slept the planner")
	}
	if res := h.svc.GuardTick(); len(res.Slept) != 0 {
		t.Fatal("the memory guard slept the planner")
	}
	if err := h.svc.SleepCard(h.reg.Deps().Store, w.ID, ""); err == nil || !strings.Contains(err.Error(), "planner never sleeps") {
		t.Fatalf("SleepCard on the planner = %v", err)
	}
	if pl := h.latest(w.ID, ""); pl.Status != "running" {
		t.Fatalf("the planner's session is %q, want running", pl.Status)
	}
}

func TestSleepSettingParses(t *testing.T) {
	h := sleepHarness(t, idleChildren, nil)
	st := h.reg.Deps().Store
	if got := sessions.SleepAfter(st); got != 30*time.Minute {
		t.Fatalf("default = %s, want 30m", got)
	}
	h.do("settings_update", m{"values": m{sessions.SettingSleepAfter: "never"}}, nil)
	if got := sessions.SleepAfter(st); got != 0 {
		t.Fatalf("never = %s", got)
	}
	h.sleepAfter(60)
	if got := sessions.SleepAfter(st); got != time.Hour {
		t.Fatalf("60 = %s", got)
	}
	if !sessions.MemoryGuardOn(st) {
		t.Fatal("the memory guard is off by default")
	}
}
