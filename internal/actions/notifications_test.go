package actions

import (
	"strings"
	"sync"
	"testing"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/notifications"
)

type noteLog struct {
	mu    sync.Mutex
	notes []notifications.Note
}

func (l *noteLog) post(n notifications.Note) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notes = append(l.notes, n)
	return nil
}

func (l *noteLog) take() []notifications.Note {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.notes
	l.notes = nil
	return out
}

// noteHarness is a chat harness whose notifications are recorded.
func noteHarness(t *testing.T) (*chatHarness, *noteLog, *notifications.Service) {
	t.Helper()
	deps, rec := testDeps(t)
	fp := &fakePlanner{running: true}
	deps.Chat = chat.New(fp, deps.Store)
	log := &noteLog{}
	svc := &notifications.Service{Post: log.post, Settings: func() notifications.Settings { return NotificationSettings(deps.Store) },
		Status: func() notifications.Status { return notifications.Status{Permission: notifications.PermDenied} }}
	deps.Notifications = svc
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &chatHarness{harness: &harness{t: t, reg: r, rec: rec}, planner: fp, chat: deps.Chat}
	h.ws = h.workspace("Acme")
	h.do("card_create", m{"workspace_id": h.ws.ID, "nick": "billing-fix", "key": "ACME-508", "start": false}, &h.card)
	return h, log, svc
}

func TestNotifyToolPostsAndOpensTheCard(t *testing.T) {
	h, log, _ := noteHarness(t)
	var out NotifyOut
	h.do("notify", m{"title": "Build finished", "body": "All green", "card": "billing-fix", "sound": "macos"}, &out)
	notes := log.take()
	if !out.Sent || len(notes) != 1 {
		t.Fatalf("out = %+v notes %+v", out, notes)
	}
	n := notes[0]
	if n.Title != "Build finished" || n.Body != "All green" || n.UserInfo["card"] != h.card.ID || n.UserInfo["workspace"] != h.ws.ID || !n.Sound {
		t.Errorf("note = %+v", n)
	}
	toast := false
	h.rec.mu.Lock()
	for _, e := range h.rec.got {
		toast = toast || e.event == EventNotify
	}
	h.rec.mu.Unlock()
	if !toast {
		t.Error("the in-app toast is kept")
	}
	h.fails("notify", m{"title": "x", "sound": "trumpet"}, "must be one of")
	h.fails("notify", m{"title": " "}, "title is required")
	h.fails("notify", m{"title": "x", "card": "nope"}, "no card")
}

func TestNotificationSettingsGateTheEvents(t *testing.T) {
	h, log, _ := noteHarness(t)
	var got NotificationsOut
	h.do("notifications_settings_get", nil, &got)
	if !got.Settings.Enabled || got.Settings.Sound != "off" || got.Status.Permission != "denied" || len(got.Events) != 5 {
		t.Fatalf("defaults = %+v", got)
	}
	h.do("notifications_settings_set", m{"events": m{"message": false}, "sound": "hadron"}, &got)
	if got.Settings.Events["message"] || !got.Settings.Events["done"] || got.Settings.Sound != "hadron" {
		t.Fatalf("set = %+v", got)
	}
	var out NotifyOut
	h.do("notify", m{"title": "x"}, &out)
	if out.Sent || !strings.Contains(out.Reason, "message notifications are off") || len(log.take()) != 0 {
		t.Errorf("a switched-off event posts: %+v", out)
	}
	h.do("notifications_settings_set", m{"events": m{"message": true}, "muted_workspaces": []string{h.ws.ID}}, &got)
	h.do("notify", m{"title": "x"}, &out)
	if out.Sent || !strings.Contains(out.Reason, "muted") {
		t.Errorf("a muted workspace posts: %+v", out)
	}
	h.do("notifications_test", nil, &out)
	if !out.Sent || len(log.take()) != 1 || !strings.Contains(out.Reason, "macOS will not show it") {
		t.Errorf("the test notification ignores mutes but says macOS blocks it: %+v", out)
	}
	h.fails("notifications_settings_set", m{"events": m{"loud": true}}, "must be one of")
	h.fails("notifications_settings_set", m{"sound": "trumpet"}, "must be one of")
	h.fails("notifications_settings_set", m{"muted_workspaces": []string{"nope"}}, "no workspace")
	if err := h.as(h.session(h.card), "notifications_settings_set", m{"enabled": false}, nil); err == nil || !strings.Contains(err.Error(), "card session cannot") {
		t.Errorf("card session: %v", err)
	}
	h.do("notifications_settings_set", m{"enabled": false}, &got)
	h.do("notifications_test", nil, &out)
	if out.Sent {
		t.Error("the master switch stops the test")
	}
}

func TestReportsAndPlannerRepliesNotify(t *testing.T) {
	h, log, svc := noteHarness(t)
	if err := h.as(h.session(h.card), "card_report", m{"kind": "done", "text": "PR is up"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.as(h.session(h.card), "card_report", m{"kind": "progress", "text": "halfway"}, nil); err != nil {
		t.Fatal(err)
	}
	notes := log.take()
	if len(notes) != 1 || notes[0].Title != "billing-fix is done" || notes[0].Body != "PR is up" || notes[0].Subtitle != "Acme" {
		t.Fatalf("report notes = %+v", notes)
	}

	// Looking at the card, a blocked report stays quiet.
	h.do("ui_presence", m{"focused": true, "workspace_id": h.ws.ID, "card_id": h.card.ID}, nil)
	if err := h.as(h.session(h.card), "card_report", m{"kind": "blocked", "text": "need a key"}, nil); err != nil {
		t.Fatal(err)
	}
	if n := log.take(); len(n) != 0 {
		t.Errorf("the card on screen notifies: %+v", n)
	}
	// The planner's reply waits for the window to lose the focus.
	if err := h.as(h.plannerActor(), "chat_post", m{"channel": "planner", "text": "All set."}, nil); err != nil {
		t.Fatal(err)
	}
	if n := log.take(); len(n) != 0 {
		t.Errorf("planner reply while focused: %+v", n)
	}
	svc.SetPresence(notifications.Presence{})
	if err := h.as(h.plannerActor(), "chat_post", m{"channel": "planner", "text": "All set."}, nil); err != nil {
		t.Fatal(err)
	}
	if n := log.take(); len(n) != 1 || n[0].Title != "Planner" || n[0].UserInfo["planner"] != "1" {
		t.Errorf("planner reply = %+v", n)
	}
}

func TestNeedsYouNoticeSaysWhatItWaitsOn(t *testing.T) {
	h, log, _ := noteHarness(t)
	deps := h.reg.Deps()
	if err := h.as(h.session(h.card), "gate_set", m{"kind": "merge", "summary": "PR 12 is ready"}, nil); err != nil {
		t.Fatal(err)
	}
	NotifyNeedsYou(deps, h.card.ID)
	n := log.take()
	if len(n) != 1 || n[0].Title != "billing-fix needs you" || !strings.Contains(n[0].Body, "PR 12 is ready") || n[0].UserInfo["card"] != h.card.ID {
		t.Fatalf("gate note = %+v", n)
	}
	snap, err := NeedsYouSnapshot(deps.Store)
	if err != nil || !snap[h.card.ID] {
		t.Errorf("snapshot = %v %v", snap, err)
	}
	var cols []domain.Column
	h.do("column_list", m{"workspace_id": h.ws.ID}, &cols)
	if len(cols) == 0 {
		t.Fatal("no columns")
	}
}
