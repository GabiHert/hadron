package app

import (
	"sort"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/actions"
)

// changeWindow is how long change events gather before the webview hears of them. Every working or
// idle flip of a session and every MCP call is a change, and each one makes the frontend refetch;
// with dozens of sessions they came several a second.
const changeWindow = 100 * time.Millisecond

// changeBatch merges the change events of one window into one event per workspace, with the union
// of their entities. An event with no entities means "anything may have changed", and stays so.
type changeBatch struct {
	emit   func(actions.ChangedEvent)
	window time.Duration

	mu      sync.Mutex
	pending map[string]map[string]bool // workspace id -> entities; an empty set means everything
	order   []string
	timer   *time.Timer
}

func newChangeBatch(window time.Duration, emit func(actions.ChangedEvent)) *changeBatch {
	return &changeBatch{emit: emit, window: window, pending: map[string]map[string]bool{}}
}

func (b *changeBatch) add(ev actions.ChangedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	set, seen := b.pending[ev.WorkspaceID]
	switch {
	case !seen:
		set = map[string]bool{}
		for _, e := range ev.Entities {
			set[e] = true
		}
		if len(ev.Entities) == 0 {
			set = nil
		}
		b.pending[ev.WorkspaceID] = set
		b.order = append(b.order, ev.WorkspaceID)
	case set == nil: // already "everything"
	case len(ev.Entities) == 0:
		b.pending[ev.WorkspaceID] = nil
	default:
		for _, e := range ev.Entities {
			set[e] = true
		}
	}
	if b.timer == nil {
		b.timer = time.AfterFunc(b.window, b.flush)
	}
}

func (b *changeBatch) flush() {
	b.mu.Lock()
	pending, order := b.pending, b.order
	b.pending, b.order, b.timer = map[string]map[string]bool{}, nil, nil
	b.mu.Unlock()
	for _, ws := range order {
		var entities []string
		for e := range pending[ws] {
			entities = append(entities, e)
		}
		sort.Strings(entities)
		if entities == nil {
			entities = []string{}
		}
		b.emit(actions.ChangedEvent{WorkspaceID: ws, Entities: entities})
	}
}
