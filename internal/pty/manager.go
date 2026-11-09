package pty

import (
	"fmt"
	"strings"
	"sync"
)

// Manager keeps the live sessions by id.
type Manager struct {
	// Env returns the environment sessions start with: the login shell's. Nil means the app's own.
	Env func() []string

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns an empty manager.
func NewManager(env func() []string) *Manager {
	return &Manager{Env: env, sessions: map[string]*Session{}}
}

// Start spawns a session under id. A bare command name is looked up on the session's own PATH,
// because a GUI app's PATH is minimal.
func (m *Manager) Start(id string, o Options) (*Session, error) {
	if o.Env == nil && m.Env != nil {
		o.Env = m.Env()
	}
	if o.Command != "" && !strings.Contains(o.Command, "/") {
		env := o.Env
		if env == nil {
			env = envOrOS()
		}
		p, err := LookPath(env, o.Command)
		if err != nil {
			return nil, fmt.Errorf("%s was not found on PATH. Install it, or set the setting session.command to its full path", o.Command)
		}
		o.Command = p
	}
	m.mu.Lock()
	if old, ok := m.sessions[id]; ok {
		select {
		case <-old.Done():
		default:
			m.mu.Unlock()
			return nil, fmt.Errorf("session %s is already running", id)
		}
	}
	m.mu.Unlock()
	s, err := Start(o)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

// Get returns the session, running or ended, that was started under id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Forget drops a session from the manager, stopping it first if it is still running.
func (m *Manager) Forget(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.Stop()
	}
}

// StopAll stops every running session.
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.Stop()
	}
}
