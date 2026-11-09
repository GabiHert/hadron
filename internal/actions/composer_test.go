package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/complete"
	"github.com/GabiHert/hadron/internal/domain"
)

func writeComposerFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commandNamed(cmds []complete.Command, name string) (complete.Command, bool) {
	for _, c := range cmds {
		if c.Name == name {
			return c, true
		}
	}
	return complete.Command{}, false
}

func TestSessionCommandsListReadsTheCardFolderThePlannerFolderAndTheUser(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	writeComposerFile(t, filepath.Join(cfg, "commands/standup.md"), "---\ndescription: Write the standup\n---\n")
	h := newHarness(t)
	w := h.workspace("Acme")
	cardDir, plannerDir := t.TempDir(), t.TempDir()
	writeComposerFile(t, filepath.Join(cardDir, ".claude/commands/billing/close-month.md"), "---\ndescription: Close the billing month\n---\n")
	writeComposerFile(t, filepath.Join(plannerDir, ".claude/skills/triage/SKILL.md"), "---\nname: triage\ndescription: Triage the board\n---\n")
	writeComposerFile(t, filepath.Join(w.Root, ".claude/commands/root-only.md"), "root")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "start": false, "cwd": cardDir}, &card)
	h.do("workspace_update", m{"workspace_id": w.ID, "planner_cwd": plannerDir}, nil)

	var cmds []complete.Command
	h.do("session_commands_list", m{"workspace_id": w.ID, "card": "billing-fix"}, &cmds)
	if c, ok := commandNamed(cmds, "billing:close-month"); !ok || c.Source != "project" || c.Description != "Close the billing month" {
		t.Errorf("card project command = %+v", c)
	}
	if c, ok := commandNamed(cmds, "standup"); !ok || c.Source != "user" {
		t.Errorf("user command = %+v", c)
	}
	if c, ok := commandNamed(cmds, "compact"); !ok || c.Source != "built-in" {
		t.Errorf("built-in = %+v", c)
	}
	if _, ok := commandNamed(cmds, "root-only"); ok {
		t.Error("a card with its own folder does not read the workspace root's commands")
	}
	if _, ok := commandNamed(cmds, "triage"); ok {
		t.Error("the planner's skills are not the card's")
	}

	h.do("session_commands_list", m{"workspace_id": w.ID, "planner": true}, &cmds)
	if c, ok := commandNamed(cmds, "triage"); !ok || c.Kind != "skill" || c.Source != "project" {
		t.Errorf("planner skill = %+v", c)
	}
	if _, ok := commandNamed(cmds, "billing:close-month"); ok {
		t.Error("the card's commands are not the planner's")
	}

	h.fails("session_commands_list", m{"workspace_id": w.ID}, "pass card")
	h.fails("session_commands_list", m{"workspace_id": w.ID, "card": "billing-fix", "planner": true}, "not both")
	h.fails("session_commands_list", m{"workspace_id": w.ID, "card": "nobody"}, `no card "nobody"`)
}

func TestFilesSearchListsTheCardFolderAndRefusesTheOutside(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	writeComposerFile(t, filepath.Join(w.Root, "docs/plan.md"), "plan")
	writeComposerFile(t, filepath.Join(w.Root, "node_modules/x/index.js"), "x")
	outside := t.TempDir()
	writeComposerFile(t, filepath.Join(outside, "secret.txt"), "x")
	if err := os.Symlink(outside, filepath.Join(w.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "docs-pass", "start": false}, &card)

	var got []complete.File
	h.do("files_search", m{"workspace_id": w.ID, "card": "docs-pass", "q": "plan"}, &got)
	if len(got) == 0 || got[0].Path != "docs/plan.md" || got[0].IsDir {
		t.Fatalf("files_search plan = %+v", got)
	}
	h.do("files_search", m{"workspace_id": w.ID, "card": "docs-pass", "q": "", "limit": 200}, &got)
	for _, f := range got {
		if strings.HasPrefix(f.Path, "node_modules") || strings.HasPrefix(f.Path, "escape") {
			t.Errorf("%s must not be listed", f.Path)
		}
	}
	h.do("files_search", m{"workspace_id": w.ID, "planner": true, "q": "docs"}, &got)
	if len(got) == 0 || got[0].Path != "docs" || !got[0].IsDir {
		t.Fatalf("the planner searches its folder (the root): %+v", got)
	}
	h.fails("files_search", m{"workspace_id": w.ID, "card": "docs-pass", "q": "../secret"}, "outside the folder")
	h.fails("files_search", m{"workspace_id": w.ID, "card": "docs-pass", "q": "/etc/hosts"}, "outside the folder")

	// A session may search only its own card's folder.
	var other domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "other", "start": false}, &other)
	self := Actor{Kind: KindSession, Name: "docs-pass", WorkspaceID: w.ID, CardID: card.ID}
	if err := h.as(self, "files_search", m{"card": "docs-pass", "q": "plan"}, &got); err != nil {
		t.Fatalf("a session searches its own folder: %v", err)
	}
	if err := h.as(self, "files_search", m{"card": "other", "q": "plan"}, nil); err == nil {
		t.Error("a session may not search another card's folder")
	}
	if err := h.as(self, "session_commands_list", m{"planner": true}, nil); err == nil {
		t.Error("a session may not list the planner's commands")
	}
}

func TestChatCommandTypesIntoTheSessionTerminalNotThePlanner(t *testing.T) {
	h := newSessionHarness(t, false)
	fp := &fakePlanner{running: true}
	h.reg.deps.Chat = chat.New(fp, h.reg.deps.Store)
	w := h.workspace("Acme")
	h.do("workspace_switch", m{"workspace_id": w.ID}, nil)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508", "launch_profile": "plain"}, &card)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })

	var msg domain.Message
	h.do("chat_command", m{"workspace_id": w.ID, "channel": "card:billing-fix", "text": "/compact keep the invoice notes"}, &msg)
	if msg.Kind != domain.MessageCommand || msg.Author != "you" || msg.Channel != "card:"+card.ID || !msg.Delivered || msg.Text != "/compact keep the invoice notes" {
		t.Fatalf("stored = %+v", msg)
	}
	waitUntil(t, "the command in the terminal", func() bool { return strings.Contains(h.file(".in"), "/compact keep the invoice notes\n") })
	h.reg.deps.Chat.Wait()
	if lines := fp.typed(); len(lines) != 0 {
		t.Fatalf("a slash command must not reach the planner, got %q", lines)
	}
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "card:billing-fix"}, &msgs)
	if len(msgs) != 1 || msgs[0].Kind != "command" {
		t.Fatalf("channel = %+v", msgs)
	}

	h.fails("chat_command", m{"channel": "card:billing-fix", "text": "//not a command"}, "starts with one /")
	h.fails("chat_command", m{"channel": "card:billing-fix", "text": "plain words"}, "starts with one /")
	h.fails("chat_command", m{"channel": "card:billing-fix", "text": "/"}, "starts with one /")

	// Not running: an error and nothing stored.
	var later domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "later", "start": false}, &later)
	h.fails("chat_command", m{"channel": "card:later", "text": "/cost"}, "no session yet", "session_start")
	h.do("chat_read", m{"channel": "card:later"}, &msgs)
	if len(msgs) != 0 {
		t.Fatalf("a command for a stopped session is not stored: %+v", msgs)
	}
	h.fails("chat_command", m{"channel": "planner", "text": "/cost"}, "no session yet", "planner_start")

	// The planner channel types into the planner's own terminal.
	h.do("planner_start", m{}, nil)
	waitUntil(t, "the planner", func() bool { return h.latest(w.ID, "").ID != "" })
	h.do("chat_command", m{"channel": "planner", "text": "/context"}, &msg)
	waitUntil(t, "the planner command", func() bool { return strings.Contains(h.file(".in"), "/context\n") })
	if strings.Contains(h.file(".in"), "[planner] /context") {
		t.Fatal("the command is typed bare, not as a tagged chat line")
	}
	h.reg.deps.Chat.Wait()
	if lines := fp.typed(); len(lines) != 0 {
		t.Fatalf("the planner channel's command is not a chat line either: %q", lines)
	}

	// A session may not send commands, even to itself; chat_command is the user's.
	self := Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID, CardID: card.ID}
	if err := h.as(self, "chat_command", m{"channel": "card:billing-fix", "text": "/clear"}, nil); err == nil {
		t.Fatal("a session may not use chat_command")
	}
}
