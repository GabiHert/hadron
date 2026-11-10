package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// EntityRoutines is the changed-event entity of routines.
const EntityRoutines = "routines"

// Routine limits.
const (
	// MinRoutineEvery is the shortest gap between two runs of a routine, in minutes.
	MinRoutineEvery = 5
	// MaxRoutineEvery is the longest every_minutes: one week.
	MaxRoutineEvery = 7 * 24 * 60
	// MaxRoutines is how many routines one workspace may have.
	MaxRoutines = 50
	// routineGrace is how late a run may be and still count as on time. A later one was missed
	// (Hadron was closed or the Mac asleep): catch_up decides whether it runs.
	routineGrace = 2 * time.Minute
)

// RoutineListIn is the input of routine_list.
type RoutineListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// RoutineCreateIn is the input of routine_create.
type RoutineCreateIn struct {
	WorkspaceID  string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name         string `json:"name" jsonschema:"a short name, such as Morning standup"`
	Cron         string `json:"cron,omitempty" jsonschema:"when it runs: a five-field cron spec in the Mac's local time, such as 0 9 * * 1-5 (09:00 on weekdays). Pass cron or every_minutes"`
	EveryMinutes int    `json:"every_minutes,omitempty" jsonschema:"when it runs: every this many minutes (5 to 10080). Pass cron or every_minutes"`
	Target       string `json:"target,omitempty" jsonschema:"planner or card; defaults to card when card is passed, else planner"`
	Card         string `json:"card,omitempty" jsonschema:"the card whose session gets the prompt: its id, nickname or key"`
	Prompt       string `json:"prompt" jsonschema:"what to type into the session; a stopped session is started and gets it once Claude is ready"`
	Enabled      *bool  `json:"enabled,omitempty" jsonschema:"whether it runs (default true)"`
	CatchUp      *bool  `json:"catch_up,omitempty" jsonschema:"when Hadron was closed at a run time, run once when it opens (default true); false skips the missed run"`
}

// RoutineUpdateIn is the input of routine_update. Only the fields you pass change.
type RoutineUpdateIn struct {
	RoutineID    string  `json:"routine_id" jsonschema:"the routine id"`
	Name         *string `json:"name,omitempty" jsonschema:"new name"`
	Cron         *string `json:"cron,omitempty" jsonschema:"a new five-field cron spec in local time; replaces every_minutes"`
	EveryMinutes *int    `json:"every_minutes,omitempty" jsonschema:"run every this many minutes instead (5 to 10080); replaces cron"`
	Target       *string `json:"target,omitempty" jsonschema:"planner or card"`
	Card         *string `json:"card,omitempty" jsonschema:"the card: its id, nickname or key"`
	Prompt       *string `json:"prompt,omitempty" jsonschema:"new prompt"`
	Enabled      *bool   `json:"enabled,omitempty" jsonschema:"false pauses the routine, true resumes it from the next run time"`
	CatchUp      *bool   `json:"catch_up,omitempty" jsonschema:"run a missed time once when Hadron opens (true) or skip it (false)"`
}

// RoutineIDIn names one routine.
type RoutineIDIn struct {
	RoutineID string `json:"routine_id" jsonschema:"the routine id"`
}

// RoutineDeleteOut is the result of routine_delete.
type RoutineDeleteOut struct {
	Deleted string `json:"deleted"`
}

func registerRoutines(r *Registry) {
	Register(r, Spec{Name: "routine_list", Description: "List a workspace's routines: prompts sent to the planner or a card's session on a schedule, with each one's next run, last run and its result."},
		func(ctx context.Context, c *Call, in RoutineListIn) ([]domain.Routine, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.Routines.List(w.ID)
		})

	Register(r, Spec{Name: "routine_create", Description: "Create a routine: on a schedule (cron in local time, or every N minutes, at most every 5 minutes) type a prompt into the planner or a card's session. A stopped session is started and gets the prompt once Claude is ready. A run missed while Hadron was closed runs once when it opens, unless catch_up is false. Sessions may not create routines.",
		Mutates: true, Entities: []string{EntityRoutines}},
		func(ctx context.Context, c *Call, in RoutineCreateIn) (domain.Routine, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.Routine{}, err
			}
			if err := mayManageRoutines(c, w.ID, "routine_create"); err != nil {
				return domain.Routine{}, err
			}
			all, err := c.Store.Routines.List(w.ID)
			if err != nil {
				return domain.Routine{}, err
			}
			if len(all) >= MaxRoutines {
				return domain.Routine{}, fmt.Errorf("a workspace may have at most %d routines. Delete one with routine_delete first", MaxRoutines)
			}
			rt := domain.Routine{WorkspaceID: w.ID, Enabled: in.Enabled == nil || *in.Enabled, CatchUp: in.CatchUp == nil || *in.CatchUp,
				Cron: strings.TrimSpace(in.Cron), EveryMinutes: in.EveryMinutes}
			if rt.Name, err = checkName("routine", in.Name); err != nil {
				return rt, err
			}
			if err := checkSchedule(rt.Cron, rt.EveryMinutes); err != nil {
				return rt, err
			}
			tgt := in.Target
			if tgt == "" {
				tgt = domain.RoutinePlanner
				if in.Card != "" {
					tgt = domain.RoutineCard
				}
			}
			if err := setRoutineTarget(c, &rt, tgt, in.Card); err != nil {
				return rt, err
			}
			if rt.Prompt, err = checkRoutinePrompt(in.Prompt); err != nil {
				return rt, err
			}
			if err := schedule(&rt, time.Now()); err != nil {
				return rt, err
			}
			if err := c.Store.Routines.Create(&rt); err != nil {
				return rt, err
			}
			return rt, nil
		})

	Register(r, Spec{Name: "routine_update", Description: "Change a routine: its name, schedule (cron or every_minutes), target, prompt or catch_up. enabled: false pauses it and true resumes it from the next run time (paused runs are not caught up).",
		Mutates: true, Entities: []string{EntityRoutines}},
		func(ctx context.Context, c *Call, in RoutineUpdateIn) (domain.Routine, error) {
			rt, err := getRoutine(c, in.RoutineID)
			if err != nil {
				return rt, err
			}
			if err := mayManageRoutines(c, rt.WorkspaceID, "routine_update"); err != nil {
				return rt, err
			}
			if in.Cron != nil && in.EveryMinutes != nil && strings.TrimSpace(*in.Cron) != "" && *in.EveryMinutes != 0 {
				return rt, errors.New("pass cron or every_minutes, not both")
			}
			resched := false
			if in.Name != nil {
				if rt.Name, err = checkName("routine", *in.Name); err != nil {
					return rt, err
				}
			}
			if in.Cron != nil && strings.TrimSpace(*in.Cron) != "" {
				rt.Cron, rt.EveryMinutes, resched = strings.TrimSpace(*in.Cron), 0, true
			}
			if in.EveryMinutes != nil && *in.EveryMinutes != 0 {
				rt.Cron, rt.EveryMinutes, resched = "", *in.EveryMinutes, true
			}
			if err := checkSchedule(rt.Cron, rt.EveryMinutes); err != nil {
				return rt, err
			}
			if in.Target != nil || in.Card != nil {
				tgt, ref := rt.Target, rt.CardID
				if in.Target != nil {
					tgt = *in.Target
				}
				if in.Card != nil {
					ref = *in.Card
					if in.Target == nil && ref != "" {
						tgt = domain.RoutineCard
					}
				}
				if err := setRoutineTarget(c, &rt, tgt, ref); err != nil {
					return rt, err
				}
			}
			if in.Prompt != nil {
				if rt.Prompt, err = checkRoutinePrompt(*in.Prompt); err != nil {
					return rt, err
				}
			}
			if in.CatchUp != nil {
				rt.CatchUp = *in.CatchUp
			}
			if in.Enabled != nil && *in.Enabled != rt.Enabled {
				rt.Enabled, resched = *in.Enabled, true
			}
			if resched {
				if err := schedule(&rt, time.Now()); err != nil {
					return rt, err
				}
			}
			return rt, c.Store.Routines.Update(&rt)
		})

	Register(r, Spec{Name: "routine_delete", Description: "Delete a routine. It does not run again; sessions it started keep running.",
		Mutates: true, Entities: []string{EntityRoutines}},
		func(ctx context.Context, c *Call, in RoutineIDIn) (RoutineDeleteOut, error) {
			rt, err := getRoutine(c, in.RoutineID)
			if err != nil {
				return RoutineDeleteOut{}, err
			}
			if err := mayManageRoutines(c, rt.WorkspaceID, "routine_delete"); err != nil {
				return RoutineDeleteOut{}, err
			}
			return RoutineDeleteOut{Deleted: rt.ID}, c.Store.Routines.Delete(rt.ID)
		})

	Register(r, Spec{Name: "routine_run", Description: "Run a routine now, paused or not: type its prompt into its target, starting the session if it is stopped. Its schedule does not change.",
		Mutates: true, Entities: []string{EntityRoutines, "sessions", "cards", EntityMessages}},
		func(ctx context.Context, c *Call, in RoutineIDIn) (domain.Routine, error) {
			rt, err := getRoutine(c, in.RoutineID)
			if err != nil {
				return rt, err
			}
			if err := mayManageRoutines(c, rt.WorkspaceID, "routine_run"); err != nil {
				return rt, err
			}
			now := time.Now()
			rt.LastRunAt, rt.LastResult = &now, runRoutine(c, rt)
			return rt, c.Store.Routines.Update(&rt)
		})
}

// mayManageRoutines keeps sessions out: a routine types into other sessions on its own, later.
func mayManageRoutines(c *Call, workspaceID, what string) error {
	switch c.Actor.Kind {
	case KindSession:
		return fmt.Errorf("%s may not use %s: routines type into the planner and other sessions. Ask the planner with card_report", c.Actor, what)
	case KindPlanner:
		if c.Actor.WorkspaceID != "" && c.Actor.WorkspaceID != workspaceID {
			return fmt.Errorf("the routine belongs to another workspace, so %s may not use %s on it", c.Actor, what)
		}
	}
	return nil
}

func getRoutine(c *Call, id string) (domain.Routine, error) {
	rt, err := c.Store.Routines.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return rt, fmt.Errorf("routine %q does not exist. Call routine_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(rt.WorkspaceID)
	}
	return rt, err
}

func checkRoutinePrompt(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("prompt is required: it is what the routine types into the session")
	}
	if len(p) > chat.MaxText {
		return "", fmt.Errorf("prompt is %d characters; the most is %d", len(p), chat.MaxText)
	}
	return p, nil
}

func setRoutineTarget(c *Call, rt *domain.Routine, tgt, ref string) error {
	switch tgt {
	case domain.RoutinePlanner:
		rt.Target, rt.CardID = tgt, ""
		return nil
	case domain.RoutineCard:
		if strings.TrimSpace(ref) == "" {
			return errors.New("card is required when target is card: pass its id, nickname or key")
		}
		card, err := cardByRef(c, ref)
		if err != nil {
			return err
		}
		if card.WorkspaceID != rt.WorkspaceID {
			return fmt.Errorf("card %q is in another workspace", card.Nick)
		}
		rt.Target, rt.CardID = tgt, card.ID
		return nil
	}
	return fmt.Errorf("target %q is not valid. Use planner or card", tgt)
}

// checkSchedule wants exactly one of a cron spec and every_minutes, no more often than every 5 minutes.
func checkSchedule(spec string, every int) error {
	switch {
	case spec == "" && every == 0:
		return errors.New("a routine needs a schedule: pass cron (such as 0 9 * * 1-5) or every_minutes")
	case spec != "" && every != 0:
		return errors.New("pass cron or every_minutes, not both")
	case every != 0:
		if every < MinRoutineEvery || every > MaxRoutineEvery {
			return fmt.Errorf("every_minutes must be between %d and %d (one week)", MinRoutineEvery, MaxRoutineEvery)
		}
		return nil
	}
	sched, err := board.ParseCron(spec)
	if err != nil {
		return err
	}
	// Look at the next runs from a fixed day, so a spec that fires twice within 5 minutes anywhere
	// in its pattern (such as 0,2 9 * * *) is caught.
	t := time.Date(2026, 1, 5, 0, 0, 0, 0, time.Local)
	prev := sched.Next(t)
	for i := 0; i < 200; i++ {
		next := sched.Next(prev)
		if next.Sub(prev) < MinRoutineEvery*time.Minute {
			return fmt.Errorf("cron %q runs more often than every %d minutes (%s, then %s). Routines run at most every %d minutes", spec, MinRoutineEvery, prev.Format("15:04"), next.Format("15:04"), MinRoutineEvery)
		}
		prev = next
	}
	return nil
}

// NextRoutineRun is the first run time of a schedule after t, in the Mac's local time.
func NextRoutineRun(spec string, every int, t time.Time) (time.Time, error) {
	if every > 0 {
		return t.Add(time.Duration(every) * time.Minute), nil
	}
	sched, err := board.ParseCron(spec)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(t.In(time.Local)), nil
}

// schedule sets the next run of an enabled routine after now; a paused one has none.
func schedule(rt *domain.Routine, now time.Time) error {
	if !rt.Enabled {
		rt.NextRunAt = nil
		return nil
	}
	next, err := NextRoutineRun(rt.Cron, rt.EveryMinutes, now)
	if err != nil {
		return err
	}
	next = next.UTC()
	rt.NextRunAt = &next
	return nil
}

// runRoutine types the prompt into the routine's target, starting a stopped session with it as
// the first message. It returns the result line kept on the routine, and posts it in the target's chat.
func runRoutine(c *Call, rt domain.Routine) string {
	res, err := deliverRoutine(c, rt)
	if err != nil {
		res = "failed: " + err.Error()
	}
	channel := chat.ChannelPlanner
	if rt.Target == domain.RoutineCard {
		channel = chat.CardChannel(rt.CardID)
	}
	ev := domain.Message{WorkspaceID: rt.WorkspaceID, Channel: channel, Author: "system", Kind: "event", Delivered: true,
		Text: fmt.Sprintf("Routine %q: %s. Prompt: %s", rt.Name, res, rt.Prompt)}
	if err := c.Store.Messages.Create(&ev); err == nil {
		c.Touch(EntityMessages)
	}
	return res
}

func deliverRoutine(c *Call, rt domain.Routine) (string, error) {
	w, err := c.Workspace(rt.WorkspaceID)
	if err != nil {
		return "", err
	}
	var card *domain.Card
	if rt.Target == domain.RoutineCard {
		cd, err := c.Store.Cards.Get(rt.CardID)
		if err != nil {
			return "", errors.New("its card no longer exists")
		}
		if cd.Archived {
			return "", fmt.Errorf("card %q is archived", cd.Nick)
		}
		card = &cd
	}
	t, err := target(c, w.ID, card)
	if err != nil {
		return "", err
	}
	if t.Live != nil {
		live, prompt := t.Live, rt.Prompt
		c.AfterCommit(func() { _ = live.Send(prompt, true) })
		return "sent the prompt to the running session", nil
	}
	if card != nil {
		if _, err := startSessionWith(c, *card, "", false, false, rt.Prompt); err != nil {
			return "", err
		}
	} else if _, err := startPlannerWith(c, w, false, false, rt.Prompt); err != nil {
		return "", err
	}
	return "started the stopped session; the prompt goes in once Claude is ready", nil
}

// TickRoutines runs the routines that are due at now. The app calls it every 30 seconds. A routine
// runs at most once per tick, however many run times it missed; a missed one (more than two minutes
// late, as after Hadron was closed) runs once if catch_up is on and is skipped otherwise.
func (r *Registry) TickRoutines(ctx context.Context, now time.Time) error {
	all, err := r.deps.Store.Routines.List("")
	if err != nil {
		return err
	}
	var firstErr error
	for _, rt := range all {
		if !rt.Enabled || rt.NextRunAt == nil || rt.NextRunAt.After(now) {
			continue
		}
		missed := now.Sub(*rt.NextRunAt) > routineGrace
		if err := r.fireRoutine(rt, now, missed && !rt.CatchUp); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Registry) fireRoutine(rt domain.Routine, now time.Time, skip bool) error {
	c := &Call{Actor: Actor{Kind: KindRule, Name: "routine " + rt.Name, WorkspaceID: rt.WorkspaceID}, Deps: r.deps, Store: r.deps.Store}
	due := *rt.NextRunAt
	err := r.deps.Store.Tx(func(tx *store.Store) error {
		c.Store = tx
		if skip {
			rt.LastResult = "skipped the run of " + due.In(time.Local).Format("Mon 2 Jan 15:04") + ": Hadron was not running then (catch_up is off)"
		} else {
			t := now
			rt.LastRunAt, rt.LastResult = &t, runRoutine(c, rt)
		}
		if err := schedule(&rt, now); err != nil {
			return err
		}
		return tx.Routines.Update(&rt)
	})
	if err != nil {
		return err
	}
	for _, fn := range c.afterCommit {
		fn()
	}
	r.emit(rt.WorkspaceID, EntityRoutines, "sessions", "cards", EntityMessages)
	return nil
}
