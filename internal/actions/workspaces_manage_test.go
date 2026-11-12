package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func (h *harness) tabOrder() string {
	var live []domain.Workspace
	h.do("workspace_list", nil, &live)
	names := make([]string, len(live))
	for i, w := range live {
		names[i] = w.Name
	}
	return strings.Join(names, ",")
}

func (h *harness) active() string {
	h.t.Helper()
	var c Call
	c.Store = h.reg.Deps().Store
	return activeID(&c)
}

func TestWorkspaceUpdateRenamesAndMovesInTheTabBar(t *testing.T) {
	h := newHarness(t)
	a, b, c := h.workspace("Acme"), h.workspace("Beta"), h.workspace("Gamma")

	var got domain.Workspace
	h.do("workspace_update", m{"workspace_id": b.ID, "name": "Beta web"}, &got)
	if got.Name != "Beta web" || got.ID != b.ID {
		t.Fatalf("rename = %+v", got)
	}
	h.fails("workspace_update", m{"workspace_id": b.ID, "name": "  "}, "name")

	h.do("workspace_update", m{"workspace_id": c.ID, "position": 0}, nil)
	if o := h.tabOrder(); o != "Gamma,Acme,Beta web" {
		t.Fatalf("after moving Gamma first: %s", o)
	}
	h.do("workspace_update", m{"workspace_id": c.ID, "position": 99}, nil)
	if o := h.tabOrder(); o != "Acme,Beta web,Gamma" {
		t.Fatalf("a position past the end means last: %s", o)
	}
	h.do("workspace_update", m{"workspace_id": a.ID, "position": 1, "name": "Acme ops"}, nil)
	if o := h.tabOrder(); o != "Beta web,Acme ops,Gamma" {
		t.Fatalf("rename and move in one call: %s", o)
	}
	h.fails("workspace_update", m{"workspace_id": a.ID, "position": -1}, "0 or more")

	// Archived workspaces are not counted, and keep out of the way.
	h.do("workspace_archive", m{"workspace_id": b.ID}, nil)
	h.fails("workspace_update", m{"workspace_id": b.ID, "position": 0}, "is archived")
	h.do("workspace_update", m{"workspace_id": c.ID, "position": 0}, nil)
	if o := h.tabOrder(); o != "Gamma,Acme ops" {
		t.Fatalf("with Beta archived: %s", o)
	}
	h.do("workspace_archive", m{"workspace_id": b.ID, "archived": false}, nil)
	if o := h.tabOrder(); o != "Gamma,Acme ops,Beta web" {
		t.Fatalf("a restored workspace comes back last: %s", o)
	}

	// The order is stored: a new registry on the same store reads it back.
	var all []domain.Workspace
	h.do("workspace_list", nil, &all)
	for i, w := range all {
		if w.Position != i {
			t.Errorf("%s has position %d, want %d", w.Name, w.Position, i)
		}
	}
	again, err := h.reg.Deps().Store.Workspaces.List(false)
	if err != nil || len(again) != 3 || again[0].ID != c.ID || again[2].ID != b.ID {
		t.Fatalf("stored order = %+v (%v)", again, err)
	}
}

func TestWorkspaceDeleteCountsStopsAndKeepsTheRootFolder(t *testing.T) {
	h := newHarness(t)
	a := h.workspace("Acme")
	b := h.workspace("Beta")
	if err := os.WriteFile(filepath.Join(b.Root, "notes.md"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.do("card_create", m{"workspace_id": b.ID, "nick": "billing-fix"}, nil)
	h.do("card_create", m{"workspace_id": b.ID, "nick": "export"}, nil)
	h.do("chat_post", m{"workspace_id": b.ID, "channel": "planner", "text": "hello"}, nil)
	h.do("workspace_switch", m{"workspace_id": b.ID}, nil)

	var dry WorkspaceDeleteOut
	h.do("workspace_delete", m{"workspace_id": b.ID, "dry_run": true}, &dry)
	if dry.Deleted || dry.Cards != 2 || dry.Messages < 1 || dry.ActiveID != b.ID {
		t.Fatalf("dry run = %+v", dry)
	}
	h.do("workspace_get", m{"workspace_id": b.ID}, nil) // still there

	var out WorkspaceDeleteOut
	h.do("workspace_delete", m{"workspace_id": b.ID}, &out)
	if !out.Deleted || out.Cards != 2 || out.ActiveID != a.ID {
		t.Fatalf("delete = %+v", out)
	}
	if h.active() != a.ID {
		t.Fatalf("deleting the active workspace switches to %s, got %s", a.ID, h.active())
	}
	if n := len(h.cards(m{"workspace_id": a.ID})); n != 0 {
		t.Fatalf("Acme's cards changed: %d", n)
	}
	if b, err := os.ReadFile(filepath.Join(b.Root, "notes.md")); err != nil || string(b) != "keep me" {
		t.Fatalf("the root folder's files must stay: %q %v", b, err)
	}

	// The last workspace in the tab bar can be neither deleted nor archived.
	h.fails("workspace_delete", m{"workspace_id": a.ID}, "only one", "Create another workspace first")
	h.fails("workspace_delete", m{"workspace_id": a.ID, "dry_run": true}, "only one")
	h.fails("workspace_archive", m{"workspace_id": a.ID}, "only one")
	h.do("workspace_get", m{"workspace_id": a.ID}, nil)
}

func TestArchivingTheActiveWorkspaceSwitchesToTheNextOne(t *testing.T) {
	h := newHarness(t)
	a, b, c := h.workspace("Acme"), h.workspace("Beta"), h.workspace("Gamma")
	h.do("workspace_switch", m{"workspace_id": b.ID}, nil)
	h.do("workspace_archive", m{"workspace_id": b.ID}, nil)
	if h.active() != c.ID {
		t.Fatalf("active = %s, want the next one (Gamma)", h.active())
	}
	h.do("workspace_archive", m{"workspace_id": c.ID}, nil)
	if h.active() != a.ID {
		t.Fatalf("active = %s, want the one before (Acme)", h.active())
	}
	// An archived workspace can still be deleted, even with one left in the tab bar.
	h.do("workspace_delete", m{"workspace_id": b.ID}, nil)
	h.fails("workspace_get", m{"workspace_id": b.ID}, "does not exist")
}

func TestDeletingAWorkspaceStopsItsRunningSessions(t *testing.T) {
	h := newSessionHarness(t, false)
	h.workspace("Acme")
	b := h.workspace("Beta")
	var card domain.Card
	h.do("card_create", m{"workspace_id": b.ID, "nick": "billing-fix"}, &card)
	waitUntil(t, "the session to start", func() bool { return h.runningIn(b.ID) })
	var dry WorkspaceDeleteOut
	h.do("workspace_delete", m{"workspace_id": b.ID, "dry_run": true}, &dry)
	if dry.Running != 1 {
		t.Fatalf("dry run counts %d running sessions, want 1", dry.Running)
	}
	h.do("workspace_delete", m{"workspace_id": b.ID}, nil)
	waitFor(t, "the session to stop", slowWait, func() bool { return !h.runningIn(b.ID) })
}
