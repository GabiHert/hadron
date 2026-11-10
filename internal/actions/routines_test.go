package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
)

func TestRoutineValidation(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	other := h.workspace("Beta")
	var card, far domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "start": false}, &card)
	h.do("card_create", m{"workspace_id": other.ID, "nick": "elsewhere", "start": false}, &far)

	base := func(over m) m {
		a := m{"workspace_id": w.ID, "name": "Standup", "every_minutes": 60, "prompt": "Post a standup."}
		for k, v := range over {
			a[k] = v
		}
		return a
	}
	h.fails("routine_create", base(m{"every_minutes": 0}), "needs a schedule")
	h.fails("routine_create", base(m{"cron": "0 9 * * *"}), "not both")
	h.fails("routine_create", base(m{"every_minutes": 3}), "between 5 and 10080")
	h.fails("routine_create", base(m{"every_minutes": 0, "cron": "*/2 * * * *"}), "more often than every 5 minutes")
	h.fails("routine_create", base(m{"every_minutes": 0, "cron": "0,2 9 * * *"}), "more often than every 5 minutes")
	h.fails("routine_create", base(m{"every_minutes": 0, "cron": "nine am"}), "not valid")
	h.fails("routine_create", base(m{"prompt": "  "}), "prompt is required")
	h.fails("routine_create", base(m{"target": "card"}), "card is required")
	h.fails("routine_create", base(m{"card": far.ID}), "another workspace")
	h.fails("routine_create", base(m{"target": "boss"}), `target "boss" is not valid`)

	// A session may not create one; the planner of the workspace may.
	if err := h.as(Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID, CardID: card.ID}, "routine_create", base(nil), nil); err == nil || !strings.Contains(err.Error(), "may not use routine_create") {
		t.Fatalf("a session created a routine: %v", err)
	}
	var byPlanner domain.Routine
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: w.ID}, "routine_create", base(m{"card": "billing-fix"}), &byPlanner); err != nil {
		t.Fatal(err)
	}
	if byPlanner.Target != domain.RoutineCard || byPlanner.CardID != card.ID || !byPlanner.CatchUp || !byPlanner.Enabled {
		t.Fatalf("routine = %+v", byPlanner)
	}
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: other.ID}, "routine_delete", m{"routine_id": byPlanner.ID}, nil); err == nil {
		t.Fatal("another workspace's planner deleted the routine")
	}
}

func TestRoutineScheduleIsLocalCronOrEveryN(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	before := time.Now()
	var every, daily domain.Routine
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Check in", "every_minutes": 10, "prompt": "Anything new?"}, &every)
	if every.Target != domain.RoutinePlanner || every.NextRunAt == nil || every.NextRunAt.Sub(before) < 10*time.Minute || every.NextRunAt.Sub(before) > 10*time.Minute+5*time.Second {
		t.Fatalf("every 10: %+v", every)
	}
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Standup", "cron": "0 9 * * *", "prompt": "Post a standup."}, &daily)
	local := daily.NextRunAt.In(time.Local)
	if local.Hour() != 9 || local.Minute() != 0 || !local.After(before) || local.Sub(before) > 24*time.Hour {
		t.Fatalf("0 9 * * * next = %v (local %v)", daily.NextRunAt, local)
	}

	// Pausing clears the next run; resuming sets one from now. Switching to every_minutes drops cron.
	var paused, resumed, switched domain.Routine
	h.do("routine_update", m{"routine_id": daily.ID, "enabled": false}, &paused)
	if paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("paused = %+v", paused)
	}
	h.do("routine_update", m{"routine_id": daily.ID, "enabled": true}, &resumed)
	if !resumed.Enabled || resumed.NextRunAt == nil {
		t.Fatalf("resumed = %+v", resumed)
	}
	h.do("routine_update", m{"routine_id": daily.ID, "every_minutes": 30}, &switched)
	if switched.Cron != "" || switched.EveryMinutes != 30 {
		t.Fatalf("switched = %+v", switched)
	}
	h.fails("routine_update", m{"routine_id": daily.ID, "cron": "0 9 * * *", "every_minutes": 30}, "not both")

	var list []domain.Routine
	h.do("routine_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	var del RoutineDeleteOut
	h.do("routine_delete", m{"routine_id": every.ID}, &del)
	h.do("routine_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 1 || del.Deleted != every.ID {
		t.Fatalf("after delete: %+v", list)
	}
}

// due moves a routine's next run to at, as if that time had come.
func due(t *testing.T, h *sessionHarness, id string, at time.Time) {
	t.Helper()
	st := h.reg.Deps().Store
	rt, err := st.Routines.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	rt.NextRunAt = &at
	if err := st.Routines.Update(&rt); err != nil {
		t.Fatal(err)
	}
}

func (h *sessionHarness) routine(id string) domain.Routine {
	rt, err := h.reg.Deps().Store.Routines.Get(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return rt
}

func TestRoutineStartsAStoppedSessionThenTypesIntoTheRunningOne(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "start": false}, &card)
	var rt domain.Routine
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Nightly check", "every_minutes": 60, "card": "billing-fix", "prompt": "Run the nightly check."}, &rt)

	now := time.Now().UTC()
	due(t, h, rt.ID, now.Add(-20*time.Second))
	if err := h.reg.TickRoutines(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the routine's prompt in the started session", func() bool { return strings.Contains(h.file(".in"), "Run the nightly check.") })
	got := h.routine(rt.ID)
	if !strings.HasPrefix(got.LastResult, "started the stopped session") || got.LastRunAt == nil || got.NextRunAt == nil || got.NextRunAt.Sub(now) < 59*time.Minute {
		t.Fatalf("after the first run: %+v", got)
	}

	// Not due yet: nothing happens.
	if err := h.reg.TickRoutines(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.routine(rt.ID).LastRunAt.After(now.Add(time.Second)) {
		t.Fatal("a routine ran before its time")
	}

	// Due again with the session running: the prompt is typed into it.
	later := time.Now().UTC()
	due(t, h, rt.ID, later.Add(-10*time.Second))
	if err := h.reg.TickRoutines(context.Background(), later); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the second prompt", func() bool { return strings.Count(h.file(".in"), "Run the nightly check.") == 2 })
	if r := h.routine(rt.ID).LastResult; r != "sent the prompt to the running session" {
		t.Fatalf("second result = %q", r)
	}

	// Each run leaves a note in the card's chat.
	msgs, err := h.reg.Deps().Store.Messages.List(w.ID, chat.CardChannel(card.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	for _, msg := range msgs {
		if msg.Author == "system" && strings.Contains(msg.Text, `Routine "Nightly check"`) {
			notes++
		}
	}
	if notes != 2 {
		t.Fatalf("routine notes in the card chat = %d, want 2", notes)
	}
}

func TestRoutineMissedWhileClosedRunsOnceOrIsSkipped(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var catchUp, skip domain.Routine
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Catch up", "every_minutes": 15, "prompt": "Morning summary, please."}, &catchUp)
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Skip", "every_minutes": 15, "prompt": "Only on time.", "catch_up": false}, &skip)

	// Hadron was closed for hours: three-hour-old run times.
	now := time.Now().UTC()
	due(t, h, catchUp.ID, now.Add(-3*time.Hour))
	due(t, h, skip.ID, now.Add(-3*time.Hour))
	if err := h.reg.TickRoutines(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the caught-up prompt in the planner", func() bool { return strings.Contains(h.file(".in"), "Morning summary, please.") })
	if strings.Count(h.file(".in"), "Morning summary, please.") != 1 {
		t.Fatalf("caught up more than once: %q", h.file(".in"))
	}
	s := h.routine(skip.ID)
	if !strings.HasPrefix(s.LastResult, "skipped the run of") || s.LastRunAt != nil || !s.NextRunAt.After(now) {
		t.Fatalf("skipped routine = %+v", s)
	}
	if strings.Contains(h.file(".in"), "Only on time.") {
		t.Fatal("a routine with catch_up off ran a missed time")
	}
	if c := h.routine(catchUp.ID); !c.NextRunAt.After(now) {
		t.Fatalf("the caught-up routine's next run is not in the future: %v", c.NextRunAt)
	}
}

func TestRoutineRunNowAndPausedRoutinesDoNotFire(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var rt, paused domain.Routine
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Weekly", "cron": "0 9 1 1 *", "prompt": "Weekly review."}, &rt)
	h.do("routine_create", m{"workspace_id": w.ID, "name": "Off", "every_minutes": 5, "prompt": "Never.", "enabled": false}, &paused)
	if paused.NextRunAt != nil {
		t.Fatalf("a paused routine has a next run: %+v", paused)
	}

	var ran domain.Routine
	h.do("routine_run", m{"routine_id": rt.ID}, &ran)
	waitUntil(t, "the run-now prompt", func() bool { return strings.Contains(h.file(".in"), "Weekly review.") })
	if ran.LastRunAt == nil || ran.NextRunAt == nil || !ran.NextRunAt.Equal(*rt.NextRunAt) {
		t.Fatalf("run now changed the schedule or kept no run: %+v (before %v)", ran, rt.NextRunAt)
	}

	if err := h.reg.TickRoutines(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if h.routine(paused.ID).LastRunAt != nil {
		t.Fatal("a paused routine ran")
	}
}
