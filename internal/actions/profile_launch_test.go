package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

var skipPerms = []string{"--model", "opus", "--dangerously-skip-permissions"}

func hasPair(argv []string, flag, val string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == val {
			return true
		}
	}
	return false
}

func count(argv []string, flag string) int {
	n := 0
	for _, a := range argv {
		if a == flag {
			n++
		}
	}
	return n
}

func TestAdoptWithALaunchProfileResumesWithItsArgsAndNoFirstPrompt(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "55555555-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Plan the migration", 3000, time.Hour)
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "trusted", "args": skipPerms, "first_prompt": "/brief {{nick}}", "prompt": "Profile layer text.", "env": m{"ACME_MODE": "trusted"}}, nil)
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "migration", "launch_profile": "nope"}, "does not exist")
	if len(h.cards(m{"workspace_id": w.ID})) != 0 {
		t.Fatal("a refused adopt leaves no card")
	}

	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "migration", "launch_profile": "trusted", "handoff": false}, &out)
	if out.Card == nil || out.Card.LaunchProfile != "trusted" {
		t.Fatalf("the card keeps the profile for restarts: %+v", out.Card)
	}
	waitUntil(t, "claude", func() bool {
		return strings.Contains(h.file(".argv"), "--dangerously-skip-permissions") && h.file(".started") != ""
	})
	argv := argvOf(h)
	if !hasPair(argv, "--resume", id) || !hasPair(argv, "--model", "opus") || count(argv, "--model") != 1 || count(argv, "--dangerously-skip-permissions") != 1 || count(argv, "--mcp-config") != 1 || count(argv, "--append-system-prompt") != 1 {
		t.Fatalf("argv = %q", argv)
	}
	if !strings.Contains(strings.Join(argv, "\n"), "Profile layer text.") {
		t.Error("the profile's prompt layer applies")
	}
	if !strings.Contains(h.file(".env"), "ACME_MODE=trusted\n") {
		t.Error("the profile's env applies")
	}
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(h.file(".in"), "/brief") {
		t.Errorf("an adopt never sends the profile's first prompt: %q", h.file(".in"))
	}
}

func TestPlannerLaunchProfileAppliesToStartRestartAndAdopt(t *testing.T) {
	h, w, _ := adoptHarness(t)
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "trusted", "args": skipPerms, "first_prompt": "/brief nobody"}, nil)

	var st SessionStatus
	h.do("planner_get", m{"workspace_id": w.ID}, &st)
	if st.LaunchProfile != "" {
		t.Fatalf("no profile yet: %+v", st)
	}
	h.fails("planner_set_profile", m{"workspace_id": w.ID, "launch_profile": "nope"}, "does not exist")
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: w.ID}, "planner_set_profile", m{"launch_profile": "trusted"}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Errorf("the planner may not widen its own permissions: %v", err)
	}
	h.do("planner_set_profile", m{"workspace_id": w.ID, "launch_profile": "trusted"}, &st)
	if st.LaunchProfile != "trusted" {
		t.Fatalf("set: %+v", st)
	}
	h.do("planner_get", m{"workspace_id": w.ID}, &st)
	if st.LaunchProfile != "trusted" {
		t.Fatalf("planner_get shows it: %+v", st)
	}

	// Start.
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "planner argv", func() bool { return strings.Contains(h.file(".argv"), "--dangerously-skip-permissions") })
	argv := argvOf(h)
	if !hasPair(argv, "--model", "opus") || count(argv, "--model") != 1 || !strings.Contains(strings.Join(argv, " "), "You are the planner") {
		t.Fatalf("start argv = %q", argv)
	}
	// Restart resumes the conversation with the same args, and sends no first prompt.
	planner := h.latest(w.ID, "")
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "restart argv", func() bool {
		a := argvOf(h)
		return hasPair(a, "--resume", planner.ClaudeSessionID) && count(a, "--dangerously-skip-permissions") == 1
	})
	argv = argvOf(h)
	if !hasPair(argv, "--model", "opus") || count(argv, "--model") != 1 {
		t.Fatalf("restart argv = %q", argv)
	}
	time.Sleep(400 * time.Millisecond)
	if strings.Contains(h.file(".in"), "/brief") {
		t.Errorf("a planner never gets the profile's first prompt: %q", h.file(".in"))
	}

	// Adopt with a profile sets it too, then uses it.
	st = SessionStatus{}
	h.do("planner_set_profile", m{"workspace_id": w.ID, "launch_profile": ""}, &st)
	if st.LaunchProfile != "" {
		t.Fatalf("cleared: %+v", st)
	}
}

func TestPlannerAdoptWithALaunchProfileKeepsItForTheWorkspace(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "66666666-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "You are the planner", 3000, time.Hour)
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "trusted", "args": skipPerms}, nil)
	h.fails("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "launch_profile": "nope"}, "does not exist")

	h.do("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "launch_profile": "trusted"}, nil)
	waitUntil(t, "adopted planner", func() bool { return strings.Contains(h.file(".argv"), "--dangerously-skip-permissions") })
	argv := argvOf(h)
	if !hasPair(argv, "--resume", id) || !hasPair(argv, "--model", "opus") || count(argv, "--model") != 1 {
		t.Fatalf("adopt argv = %q", argv)
	}
	var st SessionStatus
	h.do("planner_get", m{"workspace_id": w.ID}, &st)
	if st.LaunchProfile != "trusted" {
		t.Errorf("adopt keeps the profile for later starts: %+v", st)
	}
	// A restart afterwards still carries the args.
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "restart", func() bool {
		return hasPair(argvOf(h), "--resume", id) && strings.Contains(h.file(".argv"), "--dangerously-skip-permissions")
	})
}

func TestTheModelSettingWinsOverAModelInProfileArgs(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "mixed", "args": []string{"--model=haiku", "--verbose"}, "model": "sonnet"}, nil)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "launch_profile": "mixed"}, &card)
	waitUntil(t, "argv", func() bool { return strings.Contains(h.file(".argv"), "--verbose") })
	argv := argvOf(h)
	if !hasPair(argv, "--model", "sonnet") || strings.Contains(strings.Join(argv, " "), "haiku") || count(argv, "--model") != 1 {
		t.Fatalf("the model setting wins and is not repeated: %q", argv)
	}
	// With no model setting, the model in the args stays.
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "args-only", "args": []string{"--model", "opus"}}, nil)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync", "launch_profile": "args-only"}, nil)
	waitUntil(t, "argv 2", func() bool { return hasPair(argvOf(h), "--model", "opus") })
}

func TestPlannerStop(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	h.fails("planner_stop", m{"workspace_id": w.ID}, "no running session")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "planner", func() bool { return h.file(".started") != "" })
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: w.ID}, "planner_stop", m{"workspace_id": w.ID}, nil); err == nil || !strings.Contains(err.Error(), "on itself") {
		t.Fatalf("the planner may not stop itself: %v", err)
	}
	var st SessionStatus
	h.do("planner_stop", m{"workspace_id": w.ID}, &st)
	waitUntil(t, "stopped", func() bool {
		var g SessionStatus
		h.do("planner_get", m{"workspace_id": w.ID}, &g)
		return !g.Running
	})
}
