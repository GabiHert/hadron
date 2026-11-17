package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/notifications"
	"github.com/GabiHert/hadron/internal/store"
)

// SettingNotifications holds the notification settings as one JSON object.
const SettingNotifications = "notifications"

// EventNotificationOpen is the Wails event a click on a notification sends the window: which
// workspace, and the card or planner chat to open.
const EventNotificationOpen = "notification:open"

// NotificationOpen is the payload of EventNotificationOpen.
type NotificationOpen struct {
	WorkspaceID string `json:"workspace_id"`
	CardID      string `json:"card_id,omitempty"`
	Planner     bool   `json:"planner,omitempty"`
}

// NotificationSettings reads the notification settings, with the defaults for what is unset.
func NotificationSettings(st *store.Store) notifications.Settings {
	raw, err := st.Settings.Get(SettingNotifications)
	if err != nil || len(raw) == 0 {
		return notifications.Defaults()
	}
	var s notifications.Settings
	d := notifications.Defaults()
	s.Enabled, s.Events, s.Sound = d.Enabled, map[string]bool{}, d.Sound
	if json.Unmarshal(raw, &s) != nil {
		return d
	}
	return s.Normalize()
}

// NeedsYouSnapshot reads every card of every open workspace: whether its column has the needs-you role.
func NeedsYouSnapshot(st *store.Store) (map[string]bool, error) {
	snap, err := SoundSnapshot(st)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(snap))
	for id, c := range snap {
		out[id] = c.NeedsYou
	}
	return out, nil
}

// sendNotice asks for a notification; without the service (tests, no bundle) it does nothing.
func sendNotice(deps Deps, r notifications.Request) notifications.Result {
	if deps.Notifications == nil {
		return notifications.Result{Reason: "notifications are not available here"}
	}
	res, err := deps.Notifications.Send(r)
	if err != nil {
		deps.Log.Warn("notification", "event", r.Event, "error", err.Error())
	}
	return res
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func cardLabel(card domain.Card) string {
	switch {
	case card.Nick != "":
		return card.Nick
	case card.Key != "":
		return card.Key
	}
	return clip(card.Title, 40)
}

func workspaceName(st *store.Store, id string) string {
	if w, err := st.Workspaces.Get(id); err == nil {
		return w.Name
	}
	return ""
}

// NotifyNeedsYou notifies that a card arrived in Needs you, saying what it waits on.
func NotifyNeedsYou(deps Deps, cardID string) {
	st := deps.Store
	card, err := st.Cards.Get(cardID)
	if err != nil || card.Archived {
		return
	}
	body := ""
	if qs, err := st.Questions.List(card.WorkspaceID, card.ID, "open"); err == nil && len(qs) > 0 {
		body = "Asks: " + qs[0].Text
	} else if gs, err := st.Gates.List(card.WorkspaceID, card.ID); err == nil && len(gs) > 0 {
		body = "Waiting on you: " + firstNonBlank(gs[0].Summary, gs[0].Kind)
	} else {
		body = clip(card.Title, 120)
	}
	sendNotice(deps, notifications.Request{Event: notifications.NeedsYou, WorkspaceID: card.WorkspaceID, CardID: card.ID,
		Title: cardLabel(card) + " needs you", Subtitle: workspaceName(st, card.WorkspaceID), Body: clip(body, 200)})
}

// notifyReport notifies a card's done or blocked report.
func notifyReport(deps Deps, card domain.Card, kind, text string) {
	ev, verb := notifications.Done, " is done"
	if kind == "blocked" {
		ev, verb = notifications.Blocked, " is blocked"
	}
	sendNotice(deps, notifications.Request{Event: ev, WorkspaceID: card.WorkspaceID, CardID: card.ID,
		Title: cardLabel(card) + verb, Subtitle: workspaceName(deps.Store, card.WorkspaceID), Body: clip(text, 200)})
}

// notifyPlannerReply notifies a planner reply; on a card's channel a click opens that card.
func notifyPlannerReply(deps Deps, workspaceID string, card *domain.Card, text string) {
	r := notifications.Request{Event: notifications.PlannerReply, WorkspaceID: workspaceID, Title: "Planner", Subtitle: workspaceName(deps.Store, workspaceID), Body: clip(text, 200)}
	if card != nil {
		r.Title = "Planner on " + cardLabel(*card)
		r.CardID = card.ID
		// A reply on a card is still a reply: presence on that card should not hide it unless focused.
	}
	sendNotice(deps, r)
}

func firstNonBlank(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// NotifyIn is the input of notify.
type NotifyIn struct {
	Title       string `json:"title" jsonschema:"the notification title"`
	Body        string `json:"body,omitempty" jsonschema:"the notification text"`
	Card        string `json:"card,omitempty" jsonschema:"a card (id, nickname or key) that a click on the notification opens"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the caller's, or the card's"`
	Sound       string `json:"sound,omitempty" jsonschema:"none, macos (the macOS notification sound), needs_you or back_to_work (Hadron's chimes); the Notifications setting when left out"`
}

// NotifyOut is what notify returns.
type NotifyOut struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Sent   bool   `json:"sent" jsonschema:"a macOS notification was posted"`
	Reason string `json:"reason,omitempty" jsonschema:"why none was posted: a setting, a muted workspace, or macOS"`
}

// NotificationsOut is what notifications_settings_get and _set return.
type NotificationsOut struct {
	Settings notifications.Settings `json:"settings"`
	Status   notifications.Status   `json:"status" jsonschema:"what macOS allows Hadron"`
	Events   []string               `json:"events"`
}

// NotificationsSetIn is the input of notifications_settings_set.
type NotificationsSetIn struct {
	Enabled         *bool           `json:"enabled,omitempty" jsonschema:"notifications at all"`
	Events          map[string]bool `json:"events,omitempty" jsonschema:"event types to switch on or off: needs_you, done, blocked, planner_reply, message; others are kept"`
	MutedWorkspaces *[]string       `json:"muted_workspaces,omitempty" jsonschema:"the whole list of muted workspace ids"`
	Sound           *string         `json:"sound,omitempty" jsonschema:"off, macos or hadron"`
}

// NotificationsTestIn is the input of notifications_test.
type NotificationsTestIn struct {
	Card  string `json:"card,omitempty" jsonschema:"a card a click opens"`
	Sound string `json:"sound,omitempty" jsonschema:"none, macos, needs_you or back_to_work"`
}

// UIPresenceIn is the input of ui_presence.
type UIPresenceIn struct {
	Focused     bool   `json:"focused" jsonschema:"the Hadron window has the focus"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace shown"`
	CardID      string `json:"card_id,omitempty" jsonschema:"the card open in the card panel, if any"`
}

var noticeSounds = []string{"none", notifications.SoundMacOS, "needs_you", "back_to_work"}

func notificationsOut(c *Call) NotificationsOut {
	st := notifications.Status{Permission: notifications.PermUnavailable}
	if n := c.Deps.Notifications; n != nil && n.Status != nil {
		st = n.Status()
	}
	return NotificationsOut{Settings: NotificationSettings(c.Store), Status: st, Events: notifications.Events}
}

func checkNoticeSound(s string) error {
	if s != "" && !contains(noticeSounds, s) {
		return fmt.Errorf("sound %q must be one of %s", s, strings.Join(noticeSounds, ", "))
	}
	return nil
}

func registerNotifications(r *Registry) {
	Register(r, Spec{Name: "notify", Description: "Show the user a macOS notification (and a toast in the app). A click brings Hadron to the front and opens card, when given. It follows Settings, Notifications: the message switch, workspace mutes and the sound; the result says whether it was posted and, if not, why."},
		func(ctx context.Context, c *Call, in NotifyIn) (NotifyOut, error) {
			out := NotifyOut{Title: strings.TrimSpace(in.Title), Body: strings.TrimSpace(in.Body)}
			if out.Title == "" {
				return out, errors.New("title is required")
			}
			if err := checkNoticeSound(in.Sound); err != nil {
				return out, err
			}
			ws := firstNonBlank(in.WorkspaceID, c.Actor.WorkspaceID)
			cardID := ""
			if in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return out, err
				}
				cardID, ws = card.ID, card.WorkspaceID
			}
			if ws == "" {
				if w, err := c.Workspace(""); err == nil {
					ws = w.ID
				}
			}
			if c.Deps.Emit != nil {
				c.Deps.Emit(EventNotify, NotifyEvent{WorkspaceID: ws, CardID: cardID, Title: out.Title, Body: out.Body})
			}
			res := sendNotice(c.Deps, notifications.Request{Event: notifications.Message, WorkspaceID: ws, CardID: cardID, Title: out.Title, Body: out.Body, Sound: in.Sound})
			out.Sent, out.Reason = res.Sent, res.Reason
			return out, nil
		})

	Register(r, Spec{Name: "notifications_settings_get", Description: "Read the notification settings (on or off, each event type, muted workspaces, sound) and whether macOS allows Hadron to show notifications."},
		func(ctx context.Context, c *Call, in struct{}) (NotificationsOut, error) {
			return notificationsOut(c), nil
		})

	Register(r, Spec{Name: "notifications_settings_set", Description: "Change the notification settings: enabled, events (needs_you, done, blocked, planner_reply, message: each true or false), muted_workspaces (the whole list of workspace ids), sound (off, macos or hadron). Fields left out are kept.",
		Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in NotificationsSetIn) (NotificationsOut, error) {
			if err := userOrPlanner(c, "change the notification settings"); err != nil {
				return NotificationsOut{}, err
			}
			s := NotificationSettings(c.Store)
			if in.Enabled != nil {
				s.Enabled = *in.Enabled
			}
			for k, v := range in.Events {
				if !contains(notifications.Events, k) {
					return NotificationsOut{}, fmt.Errorf("event %q must be one of %s", k, strings.Join(notifications.Events, ", "))
				}
				s.Events[k] = v
			}
			if in.MutedWorkspaces != nil {
				for _, id := range *in.MutedWorkspaces {
					if _, err := c.Store.Workspaces.Get(id); err != nil {
						return NotificationsOut{}, fmt.Errorf("no workspace %q. workspace_list shows the ids", id)
					}
				}
				s.MutedWorkspaces = *in.MutedWorkspaces
			}
			if in.Sound != nil {
				if !contains(notifications.Sounds, *in.Sound) {
					return NotificationsOut{}, fmt.Errorf("sound %q must be one of %s", *in.Sound, strings.Join(notifications.Sounds, ", "))
				}
				s.Sound = *in.Sound
			}
			b, _ := json.Marshal(s.Normalize())
			if err := c.Store.Settings.Set(SettingNotifications, b); err != nil {
				return NotificationsOut{}, err
			}
			return notificationsOut(c), nil
		})

	Register(r, Spec{Name: "notifications_test", Description: "Post a test notification now, whatever the event switches and mutes say, to check that macOS shows Hadron's notifications. A click opens card, when given."},
		func(ctx context.Context, c *Call, in NotificationsTestIn) (NotifyOut, error) {
			if err := checkNoticeSound(in.Sound); err != nil {
				return NotifyOut{}, err
			}
			r := notifications.Request{Event: notifications.Message, Title: "Hadron", Body: "Notifications work. Click to come back to Hadron.", Sound: in.Sound, Force: true}
			if w, err := c.Workspace(""); err == nil {
				r.WorkspaceID = w.ID
			}
			if in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return NotifyOut{}, err
				}
				r.CardID, r.WorkspaceID = card.ID, card.WorkspaceID
				r.Body = "Notifications work. Click to open " + cardLabel(card) + "."
			}
			res := sendNotice(c.Deps, r)
			out := NotifyOut{Title: r.Title, Body: r.Body, Sent: res.Sent, Reason: res.Reason}
			if res.Sent && c.Deps.Notifications != nil && c.Deps.Notifications.Status != nil {
				if st := c.Deps.Notifications.Status(); !st.Shows() {
					out.Reason = fmt.Sprintf("posted, but macOS will not show it: permission %s, alert style %s. Open Notification Settings to allow it", st.Permission, firstNonBlank(st.AlertStyle, "unknown"))
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "notifications_open_system_settings", Description: "Open Hadron's page in macOS System Settings, Notifications, where the user allows or blocks its notifications."},
		func(ctx context.Context, c *Call, in struct{}) (struct{}, error) {
			if n := c.Deps.Notifications; n != nil && n.Open != nil {
				return struct{}{}, n.Open()
			}
			return struct{}{}, errors.New("notifications are not available here")
		})

	Register(r, Spec{Name: "ui_presence", Description: "The app window reports whether it has the focus and which card it shows, so Hadron does not notify about what the user is already looking at."},
		func(ctx context.Context, c *Call, in UIPresenceIn) (struct{}, error) {
			if n := c.Deps.Notifications; n != nil {
				n.SetPresence(notifications.Presence{Focused: in.Focused, WorkspaceID: in.WorkspaceID, CardID: in.CardID})
			}
			return struct{}{}, nil
		})
}
