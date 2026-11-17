// Package notifications decides when Hadron shows a macOS notification and posts it.
//
// The events are a card arriving in Needs you, a card reporting done or blocked, a planner reply,
// and messages from the notify tool, rules and integrations. Each can be switched off, a whole
// workspace can be muted, and nothing shows for the card the user is already looking at (or, for
// a planner reply, while the window has the focus). Posting goes through UNUserNotificationCenter
// (native_darwin.go); clicks come back through Wails' notification delegate.
package notifications

import (
	"fmt"
	"strings"
	"sync"
)

// Event types.
const (
	NeedsYou     = "needs_you"
	Done         = "done"
	Blocked      = "blocked"
	PlannerReply = "planner_reply"
	Message      = "message"
)

// Events are every event type, in the order Settings shows them.
var Events = []string{NeedsYou, Done, Blocked, PlannerReply, Message}

// Sound choices: none, the macOS notification sound, or Hadron's own chimes.
const (
	SoundOff    = "off"
	SoundMacOS  = "macos"
	SoundHadron = "hadron"
)

// Sounds are the sound settings.
var Sounds = []string{SoundOff, SoundMacOS, SoundHadron}

// Settings are the notification settings.
type Settings struct {
	Enabled         bool            `json:"enabled" jsonschema:"notifications at all"`
	Events          map[string]bool `json:"events" jsonschema:"each event type on or off: needs_you, done, blocked, planner_reply, message"`
	MutedWorkspaces []string        `json:"muted_workspaces" jsonschema:"workspace ids that never notify"`
	Sound           string          `json:"sound" jsonschema:"off (silent), macos (the macOS notification sound) or hadron (Hadron's chimes)"`
}

// Defaults are the settings before the user changes any: everything on, silent, because Hadron
// already chimes when a card needs you.
func Defaults() Settings {
	ev := map[string]bool{}
	for _, e := range Events {
		ev[e] = true
	}
	return Settings{Enabled: true, Events: ev, MutedWorkspaces: []string{}, Sound: SoundOff}
}

// Normalize fills what is missing with the defaults and drops unknown keys.
func (s Settings) Normalize() Settings {
	d := Defaults()
	for _, e := range Events {
		if v, ok := s.Events[e]; ok {
			d.Events[e] = v
		}
	}
	d.Enabled = s.Enabled
	seen := map[string]bool{}
	for _, w := range s.MutedWorkspaces {
		if w = strings.TrimSpace(w); w != "" && !seen[w] {
			seen[w] = true
			d.MutedWorkspaces = append(d.MutedWorkspaces, w)
		}
	}
	if contains(Sounds, s.Sound) {
		d.Sound = s.Sound
	}
	return d
}

func (s Settings) muted(ws string) bool { return ws != "" && contains(s.MutedWorkspaces, ws) }

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Presence is what the user is looking at, as the window reports it.
type Presence struct {
	Focused     bool   `json:"focused"`
	WorkspaceID string `json:"workspace_id"`
	CardID      string `json:"card_id"`
}

// Note is one notification to post. A newer one with the same ID replaces it.
type Note struct {
	ID       string
	Title    string
	Subtitle string
	Body     string
	Thread   string
	Sound    bool
	UserInfo map[string]string
}

// Request asks for a notification.
type Request struct {
	Event       string
	WorkspaceID string
	CardID      string // the card a click opens, if any
	Title, Body string
	Subtitle    string
	// Sound overrides the setting: none, macos, or a Hadron sound (needs_you, back_to_work).
	Sound string
	// Force skips the event switches, mutes and presence (the test notification); the master
	// switch and macOS permission still apply.
	Force bool
}

// Result says whether a notification went out and, when not, why.
type Result struct {
	Sent   bool   `json:"sent"`
	Reason string `json:"reason,omitempty"`
}

// Service posts notifications by the settings.
type Service struct {
	// Post shows a notification. Nil, as in tests, records nothing and sends nothing.
	Post func(Note) error
	// PlaySound plays one of Hadron's sounds. It may be nil.
	PlaySound func(kind string)
	// Settings reads the settings.
	Settings func() Settings
	// Status reads what macOS allows; Open opens Hadron's page in System Settings. Either may be nil.
	Status func() Status
	Open   func() error

	mu       sync.Mutex
	presence Presence
}

// SetPresence records what the user is looking at.
func (s *Service) SetPresence(p Presence) {
	s.mu.Lock()
	s.presence = p
	s.mu.Unlock()
}

// Presence returns what the user is looking at.
func (s *Service) Presence() Presence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presence
}

func (s *Service) settings() Settings {
	if s.Settings == nil {
		return Defaults()
	}
	return s.Settings().Normalize()
}

// Decide says whether a request may notify now, and why not.
func (s *Service) Decide(r Request) Result {
	st := s.settings()
	if !st.Enabled {
		return Result{Reason: "notifications are off in Settings, Notifications"}
	}
	if r.Force {
		return Result{Sent: true}
	}
	if on, known := st.Events[r.Event]; known && !on {
		return Result{Reason: fmt.Sprintf("%s notifications are off in Settings, Notifications", r.Event)}
	}
	if st.muted(r.WorkspaceID) {
		return Result{Reason: "this workspace is muted in Settings, Notifications"}
	}
	p := s.Presence()
	switch r.Event {
	case PlannerReply:
		if p.Focused {
			return Result{Reason: "Hadron has the focus"}
		}
	case NeedsYou, Done, Blocked:
		if p.Focused && r.CardID != "" && p.CardID == r.CardID {
			return Result{Reason: "you are looking at that card"}
		}
	}
	return Result{Sent: true}
}

// hadronSound is the chime an event plays when the sound setting is hadron.
func hadronSound(event string) string {
	if event == Done || event == PlannerReply {
		return "back_to_work"
	}
	return "needs_you"
}

// Send posts the notification when the settings and presence allow it.
func (s *Service) Send(r Request) (Result, error) {
	res := s.Decide(r)
	if !res.Sent {
		return res, nil
	}
	sound := r.Sound
	if sound == "" {
		switch s.settings().Sound {
		case SoundMacOS:
			sound = SoundMacOS
		case SoundHadron:
			sound = hadronSound(r.Event)
		default:
			sound = "none"
		}
	}
	n := Note{Title: r.Title, Subtitle: r.Subtitle, Body: r.Body, Sound: sound == SoundMacOS, UserInfo: map[string]string{"workspace": r.WorkspaceID}}
	switch {
	case r.CardID != "":
		n.ID, n.Thread = "card:"+r.CardID, "card:"+r.CardID
		n.UserInfo["card"] = r.CardID
	case r.Event == PlannerReply:
		n.ID, n.Thread = "planner:"+r.WorkspaceID, "planner:"+r.WorkspaceID
		n.UserInfo["planner"] = "1"
	default:
		n.ID, n.Thread = fmt.Sprintf("message:%s:%s", r.WorkspaceID, r.Title), "messages"
	}
	if s.Post == nil {
		return Result{Reason: "notifications are not available here"}, nil
	}
	if err := s.Post(n); err != nil {
		return Result{Reason: err.Error()}, err
	}
	if sound != "none" && sound != SoundMacOS && s.PlaySound != nil {
		s.PlaySound(sound)
	}
	return Result{Sent: true}, nil
}

// Entered lists the cards that arrived in a needs-you column between two snapshots (card id to
// whether it is in one): a known card that moved there. A card that is new and already there
// counts too, but not at the first look.
func Entered(prev, cur map[string]bool) []string {
	var out []string
	for id, now := range cur {
		was, known := prev[id]
		if now && (!known || !was) {
			out = append(out, id)
		}
	}
	return out
}
