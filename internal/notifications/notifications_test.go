package notifications

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type capture struct {
	mu     sync.Mutex
	notes  []Note
	sounds []string
}

func (c *capture) post(n Note) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes = append(c.notes, n)
	return nil
}

func service(s Settings) (*Service, *capture) {
	c := &capture{}
	return &Service{Post: c.post, PlaySound: func(k string) { c.sounds = append(c.sounds, k) }, Settings: func() Settings { return s }}, c
}

func TestDefaultsAndNormalize(t *testing.T) {
	d := Defaults()
	if !d.Enabled || d.Sound != SoundOff || len(d.Events) != len(Events) {
		t.Fatalf("defaults = %+v", d)
	}
	n := Settings{Enabled: true, Events: map[string]bool{Done: false, "bogus": true}, MutedWorkspaces: []string{" w1 ", "w1", ""}, Sound: "loud"}.Normalize()
	if n.Events[Done] || !n.Events[NeedsYou] || n.Events["bogus"] || strings.Join(n.MutedWorkspaces, ",") != "w1" || n.Sound != SoundOff {
		t.Errorf("normalized = %+v", n)
	}
}

func TestDecide(t *testing.T) {
	s := Defaults()
	s.Events[Done] = false
	s.MutedWorkspaces = []string{"quiet"}
	svc, _ := service(s)
	cases := []struct {
		r    Request
		want bool
		why  string
	}{
		{Request{Event: NeedsYou, WorkspaceID: "w", CardID: "c1"}, true, ""},
		{Request{Event: Done, WorkspaceID: "w", CardID: "c1"}, false, "done notifications are off"},
		{Request{Event: NeedsYou, WorkspaceID: "quiet", CardID: "c1"}, false, "muted"},
		{Request{Event: Done, WorkspaceID: "quiet", Force: true}, true, ""},
	}
	for _, c := range cases {
		res := svc.Decide(c.r)
		if res.Sent != c.want || !strings.Contains(res.Reason, c.why) {
			t.Errorf("%+v = %+v", c.r, res)
		}
	}

	svc.SetPresence(Presence{Focused: true, WorkspaceID: "w", CardID: "c1"})
	if res := svc.Decide(Request{Event: NeedsYou, WorkspaceID: "w", CardID: "c1"}); res.Sent || !strings.Contains(res.Reason, "looking at that card") {
		t.Errorf("the card on screen notifies: %+v", res)
	}
	if res := svc.Decide(Request{Event: Blocked, WorkspaceID: "w", CardID: "c2"}); !res.Sent {
		t.Errorf("another card is silenced: %+v", res)
	}
	if res := svc.Decide(Request{Event: PlannerReply, WorkspaceID: "w"}); res.Sent {
		t.Errorf("a planner reply notifies while focused: %+v", res)
	}
	svc.SetPresence(Presence{Focused: false, WorkspaceID: "w", CardID: "c1"})
	if res := svc.Decide(Request{Event: NeedsYou, WorkspaceID: "w", CardID: "c1"}); !res.Sent {
		t.Errorf("an unfocused window still notifies: %+v", res)
	}
	if res := svc.Decide(Request{Event: PlannerReply, WorkspaceID: "w"}); !res.Sent {
		t.Errorf("planner reply while away: %+v", res)
	}

	off := Defaults()
	off.Enabled = false
	svc2, _ := service(off)
	if res := svc2.Decide(Request{Event: Message, Force: true}); res.Sent {
		t.Error("the master switch stops even the test notification")
	}
}

func TestSendBuildsTheNoteAndSound(t *testing.T) {
	s := Defaults()
	svc, c := service(s)
	if _, err := svc.Send(Request{Event: NeedsYou, WorkspaceID: "w", CardID: "c1", Title: "billing-fix needs you", Body: "Asks: merge?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(Request{Event: PlannerReply, WorkspaceID: "w", Title: "Planner", Body: "Done."}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(Request{Event: Message, WorkspaceID: "w", Title: "Build", Sound: SoundMacOS}); err != nil {
		t.Fatal(err)
	}
	if len(c.notes) != 3 {
		t.Fatalf("notes = %+v", c.notes)
	}
	a, b, m := c.notes[0], c.notes[1], c.notes[2]
	if a.ID != "card:c1" || a.UserInfo["card"] != "c1" || a.UserInfo["workspace"] != "w" || a.Sound {
		t.Errorf("card note = %+v", a)
	}
	if b.ID != "planner:w" || b.UserInfo["planner"] != "1" || b.UserInfo["card"] != "" {
		t.Errorf("planner note = %+v", b)
	}
	if !m.Sound || len(c.sounds) != 0 {
		t.Errorf("macos sound: %+v %v", m, c.sounds)
	}

	s.Sound = SoundHadron
	svc, c = service(s)
	_, _ = svc.Send(Request{Event: NeedsYou, CardID: "c1", Title: "x"})
	_, _ = svc.Send(Request{Event: Done, CardID: "c2", Title: "y"})
	_, _ = svc.Send(Request{Event: Message, Title: "z", Sound: "none"})
	if strings.Join(c.sounds, ",") != "needs_you,back_to_work" || c.notes[0].Sound {
		t.Errorf("hadron sounds = %v", c.sounds)
	}

	fail := &Service{Post: func(Note) error { return errors.New("notifications need Hadron.app") }}
	if res, err := fail.Send(Request{Event: Message, Title: "x"}); err == nil || res.Sent || !strings.Contains(res.Reason, "Hadron.app") {
		t.Errorf("failed post = %+v %v", res, err)
	}
	none := &Service{}
	if res, _ := none.Send(Request{Event: Message, Title: "x"}); res.Sent {
		t.Error("no poster sends nothing")
	}
}

func TestWatcherReportsCardsThatArrive(t *testing.T) {
	board := map[string]bool{"a": false, "b": true}
	var mu sync.Mutex
	var got []string
	w := &Watcher{Snapshot: func() (map[string]bool, error) {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]bool{}
		for k, v := range board {
			out[k] = v
		}
		return out, nil
	}, Entered: func(id string) { mu.Lock(); got = append(got, id); mu.Unlock() }, Debounce: 10 * time.Millisecond}
	if err := w.Seed(); err != nil {
		t.Fatal(err)
	}
	w.Check()
	if len(got) != 0 {
		t.Fatalf("a card already there at startup notifies: %v", got)
	}
	mu.Lock()
	board["a"], board["c"], board["b"] = true, true, false
	mu.Unlock()
	w.Changed()
	w.Changed()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	sort.Strings(got)
	mu.Unlock()
	if strings.Join(got, ",") != "a,c" {
		t.Errorf("entered = %v", got)
	}
}
