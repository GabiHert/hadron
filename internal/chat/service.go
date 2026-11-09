package chat

import (
	"errors"
	"sync"

	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// Delivery reasons recorded on a message the planner did not receive.
const (
	ReasonPlannerNotRunning = "planner_not_running"
	ReasonTypeFailedPrefix  = "type_failed: "
)

// Planner is what chat needs from the session layer: whether a workspace's planner is running, and
// a way to type a line into it.
type Planner interface {
	Running(st *store.Store, workspaceID string) bool
	// Type types one line like a paste followed by Enter. It blocks until the line is written.
	Type(st *store.Store, workspaceID, line string) error
}

// SessionsPlanner adapts the session service to Planner.
type SessionsPlanner struct{ Sessions *sessions.Service }

// Running reports whether the workspace's planner process is alive.
func (p SessionsPlanner) Running(st *store.Store, workspaceID string) bool {
	t, err := p.Sessions.Find(st, workspaceID, "")
	return err == nil && t.Live != nil
}

// Type types a line into the planner's terminal.
func (p SessionsPlanner) Type(st *store.Store, workspaceID, line string) error {
	t, err := p.Sessions.Find(st, workspaceID, "")
	if err != nil || t.Live == nil {
		return errors.New("the planner is not running")
	}
	return t.Live.Send(line, true)
}

// Service types tagged lines into planners. Lines go through one queue, so they reach the planner
// in the order the actions ran, and an action never waits for the terminal.
type Service struct {
	// Log is Hadron's own log. It may be nil.
	Log *dlog.Logger

	planner Planner
	store   *store.Store

	mu      sync.Mutex
	queue   []job
	working bool
	idle    *sync.Cond
}

type job struct {
	workspaceID string
	line        string
	onFail      func(reason string)
}

// New returns a Service. st is the root store, used by the queue after the calling action's
// transaction has ended.
func New(planner Planner, st *store.Store) *Service {
	s := &Service{planner: planner, store: st}
	s.idle = sync.NewCond(&s.mu)
	return s
}

// Available says whether a line for the workspace's planner can be typed now. When it cannot, the
// reason is what a message records.
func (s *Service) Available(st *store.Store, workspaceID string) (ok bool, reason string) {
	if s == nil || s.planner == nil || !s.planner.Running(st, workspaceID) {
		return false, ReasonPlannerNotRunning
	}
	return true, ""
}

// Enqueue types line into the workspace's planner in the background, after earlier lines. onFail is
// called with a reason if the line could not be typed.
func (s *Service) Enqueue(workspaceID, line string, onFail func(reason string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.queue = append(s.queue, job{workspaceID, line, onFail})
	if !s.working {
		s.working = true
		go s.drain()
	}
	s.mu.Unlock()
}

func (s *Service) drain() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.working = false
			s.idle.Broadcast()
			s.mu.Unlock()
			return
		}
		j := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()

		if !s.planner.Running(s.store, j.workspaceID) {
			s.Log.Debug("chat undelivered", "workspace", j.workspaceID, "reason", ReasonPlannerNotRunning, "line_chars", len(j.line))
			if j.onFail != nil {
				j.onFail(ReasonPlannerNotRunning)
			}
			continue
		}
		if err := s.planner.Type(s.store, j.workspaceID, j.line); err != nil {
			s.Log.Debug("chat undelivered", "workspace", j.workspaceID, "reason", ReasonTypeFailedPrefix+err.Error(), "line_chars", len(j.line))
			if j.onFail != nil {
				j.onFail(ReasonTypeFailedPrefix + err.Error())
			}
			continue
		}
		s.Log.Debug("chat delivered", "workspace", j.workspaceID, "line_chars", len(j.line))
	}
}

// Wait blocks until every queued line has been typed. Tests use it.
func (s *Service) Wait() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.working || len(s.queue) > 0 {
		s.idle.Wait()
	}
}
