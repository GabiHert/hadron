package app

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/testwait"

	"github.com/GabiHert/hadron/internal/actions"
)

func TestChangeEventsMergePerWorkspace(t *testing.T) {
	var mu sync.Mutex
	var got []actions.ChangedEvent
	b := newChangeBatch(time.Hour, func(ev actions.ChangedEvent) { mu.Lock(); got = append(got, ev); mu.Unlock() })
	// A burst like 36 sessions flipping working and idle while they call MCP tools.
	for i := 0; i < 50; i++ {
		b.add(actions.ChangedEvent{WorkspaceID: "w1", Entities: []string{"cards", "sessions"}})
		b.add(actions.ChangedEvent{WorkspaceID: "w1", Entities: []string{"mcp_calls"}})
		b.add(actions.ChangedEvent{WorkspaceID: "w2", Entities: []string{"messages"}})
	}
	b.add(actions.ChangedEvent{WorkspaceID: "w3", Entities: []string{"cards"}})
	b.add(actions.ChangedEvent{WorkspaceID: "w3"}) // no entities: anything may have changed
	b.add(actions.ChangedEvent{WorkspaceID: "w3", Entities: []string{"columns"}})
	b.flush()
	want := []actions.ChangedEvent{
		{WorkspaceID: "w1", Entities: []string{"cards", "mcp_calls", "sessions"}},
		{WorkspaceID: "w2", Entities: []string{"messages"}},
		{WorkspaceID: "w3", Entities: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("151 events became %+v, want %+v", got, want)
	}
}

func TestChangeEventsArriveAfterTheWindow(t *testing.T) {
	done := make(chan actions.ChangedEvent, 4)
	b := newChangeBatch(20*time.Millisecond, func(ev actions.ChangedEvent) { done <- ev })
	b.add(actions.ChangedEvent{WorkspaceID: "w1", Entities: []string{"cards"}})
	b.add(actions.ChangedEvent{WorkspaceID: "w1", Entities: []string{"questions"}})
	select {
	case ev := <-done:
		if !reflect.DeepEqual(ev.Entities, []string{"cards", "questions"}) {
			t.Fatalf("merged event = %+v", ev)
		}
	case <-testwait.After():
		t.Fatal("the merged event never came")
	}
	select {
	case ev := <-done:
		t.Fatalf("one window sent a second event: %+v", ev)
	case <-time.After(60 * time.Millisecond):
	}
	// A later change starts a new window.
	b.add(actions.ChangedEvent{WorkspaceID: "w1", Entities: []string{"cards"}})
	select {
	case <-done:
	case <-testwait.After():
		t.Fatal("a change after the window was lost")
	}
}
