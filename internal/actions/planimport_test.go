package actions

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plannerFixture writes a small planner run folder with neutral names.
func plannerFixture(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "planner")
	writeFile(t, filepath.Join(dir, "sessions", "billing-fix.md"), `# billing-fix — planner ↔ session

- task: ACME-12
- workspace: 4FAF665E-0000-0000-0000-000000000000
- claude session: 0A1B2C3D-1111-2222-3333-444455556666
- created: 2026-10-26 15:47

Working notes, not contract.

## How to use this file

- Read the newest section.

## 2026-10-26 15:47 · planner → session · new — Round invoices

Stage: **planning**. Fix the rounding.

## 2026-10-26 15:50 · session → planner · progress — Found it

Rounding happens in two places.

## 2026-10-26 16:20 · session → planner · question — Which rule?

Half up or banker's?
`)
	writeFile(t, filepath.Join(dir, "sessions", "invoice-sync.md"), `# invoice-sync

- task: none yet
- claude session: not started
- created: 2026-10-27 09:00

## 2026-10-27 09:05 · session → planner · done — All synced

Nothing left.
`)
	writeFile(t, filepath.Join(dir, "sessions", "Not A Nick.md"), "- task: none yet\n")
	writeFile(t, filepath.Join(dir, "sessions", "existing-card.md"), "- task: ACME-1\n")
	writeFile(t, filepath.Join(dir, "log.md"), `# Planner transition log (append-only)

- 2026-10-26 15:47 · billing-fix · (new) → planning · session 0a1b2c3d · Linear: none yet
- 2026-10-26 16:30 · ACME-12 billing-fix · planning → in-progress · brief approved
- 2026-10-26 16:31 · billing-fix · stale nudge: quiet for a day
- 2026-10-27 09:00 · invoice-sync · (new) → backlog (placeholder) · same intake
- 2026-10-27 09:10 · invoice-sync · in-progress → done · merged
- 2026-10-27 09:11 · ghost-session · (new) → planning · no session file
`)
	writeFile(t, filepath.Join(dir, "questions", "billing-fix", "Q-01.json"), `{
  "id": "Q-01", "nickname": "billing-fix", "kind": "decision", "asked_at": "2026-10-26 16:20",
  "context": "Two places round.", "status": "answered",
  "questions": [{"question": "Which rounding rule?", "multiSelect": false, "options": [
    {"label": "Half up", "description": "Common"}, {"label": "Banker's", "description": "Statistical"}]}],
  "answer": {"selections": [["Half up"]], "free_text": "keep cents exact", "answered_at": "2026-10-26T18:55:41.580Z", "via": "board"}
}`)
	writeFile(t, filepath.Join(dir, "questions", "billing-fix", "Q-02.json"), `{
  "id": "Q-02", "kind": "approval", "asked_at": "2026-10-26 17:00", "context": "Run the backfill?", "status": "open",
  "questions": [{"question": "Run it?", "options": [{"label": "Run"}, {"label": "Hold"}]}]
}`)
	writeFile(t, filepath.Join(dir, "gates", "billing-fix.json"), `{"nickname":"billing-fix","task":"ACME-12","gates":[
  {"kind":"merge","summary":"2 PRs green","since":"2026-10-28 10:00"},
  {"kind":"teleport","summary":"unknown kind","since":"2026-10-28 10:00"}]}`)
	writeFile(t, filepath.Join(dir, "gates", "invoice-sync.json"), `{"gates":[]}`)
	return dir
}

func importCounts(h *harness, wsID string) (cards, questions, gates int) {
	st := h.reg.Deps().Store
	cs, _ := st.Cards.List(wsID, storeAll)
	for _, card := range cs {
		qs, _ := st.Questions.List(wsID, card.ID, "")
		gs, _ := st.Gates.List(wsID, card.ID)
		questions += len(qs)
		gates += len(gs)
	}
	return len(cs), questions, gates
}

func TestImportPlannerDirDryRunThenImportThenAgain(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	h.do("card_create", m{"workspace_id": w.ID, "nick": "existing-card", "start": false}, nil)
	dir := plannerFixture(t)
	cols := h.columns(w.ID)
	active, backlog, done, needs := colByRole(cols, domain.RoleActive), colByRole(cols, domain.RoleBacklog), colByRole(cols, domain.RoleDone), colByRole(cols, domain.RoleNeedsYou)

	var plan ImportOut
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir}, &plan) // dry run is the default
	if !plan.DryRun || plan.Created != 2 || plan.Skipped != 2 || plan.UnmatchedLog != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	by := map[string]ImportCard{}
	for _, c := range plan.Cards {
		by[c.Nick] = c
	}
	bf := by["billing-fix"]
	if bf.Action != "create" || bf.Key != "ACME-12" || bf.Column != needs.Name || bf.Stage != "in-progress" || bf.Liveness != "waiting" ||
		bf.Messages != 3 || bf.Questions != 2 || bf.OpenQuestions != 1 || bf.Gates != 1 || bf.ClaudeSessionID != "0a1b2c3d-1111-2222-3333-444455556666" {
		t.Errorf("billing-fix = %+v", bf)
	}
	is := by["invoice-sync"]
	if is.Column != done.Name || is.Key != "" || is.ClaudeSessionID != "" || is.Stage != "" {
		t.Errorf("invoice-sync = %+v (the last arrow, in-progress to done, wins; 'not started' is no session id)", is)
	}
	if by["existing-card"].Action != "skip" || !strings.Contains(by["existing-card"].Reason, "already exists") ||
		by["Not A Nick"].Action != "skip" || !strings.Contains(by["Not A Nick"].Reason, "kebab-case") {
		t.Errorf("skips = %+v", plan.Cards)
	}
	joined := strings.Join(plan.Warnings, "|")
	if !strings.Contains(joined, `gate kind "teleport"`) {
		t.Errorf("warnings = %v", plan.Warnings)
	}
	if n, q, g := importCounts(h, w.ID); n != 1 || q != 0 || g != 0 {
		t.Fatalf("a dry run writes nothing: %d cards %d questions %d gates", n, q, g)
	}

	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir, "dry_run": false}, &plan)
	if plan.DryRun || plan.Created != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	n, q, g := importCounts(h, w.ID)
	if n != 3 || q != 2 || g != 1 {
		t.Fatalf("imported: %d cards %d questions %d gates", n, q, g)
	}

	card := h.cards(m{"workspace_id": w.ID, "role": "needs-you"})
	if len(card) != 1 || card[0].Nick != "billing-fix" || card[0].ReturnColumnID != active.ID || card[0].Stage != "in-progress" || card[0].Key != "ACME-12" {
		t.Fatalf("billing-fix = %+v (an open question puts it in Needs you and remembers where it came from)", card)
	}
	if got := h.cards(m{"workspace_id": w.ID, "column_id": done.ID}); len(got) != 1 || got[0].Nick != "invoice-sync" {
		t.Errorf("done = %+v", got)
	}
	_ = backlog

	var msgs []domain.Message
	h.do("chat_read", m{"workspace_id": w.ID, "channel": "card:" + card[0].ID}, &msgs)
	if len(msgs) != 3 || msgs[0].Author != "planner" || msgs[0].Kind != "text" || !strings.Contains(msgs[0].Text, "Round invoices") ||
		msgs[2].Kind != "report" || msgs[2].ReportKind != "question" || msgs[1].ReportKind != "progress" {
		t.Errorf("messages = %+v", msgs)
	}
	if msgs[0].CreatedAt.Year() != 2026 || msgs[0].CreatedAt.Month() != 10 {
		t.Errorf("message times come from the file: %v", msgs[0].CreatedAt)
	}

	var qs []domain.Question
	h.do("question_list", m{"workspace_id": w.ID, "card": card[0].ID}, &qs)
	sort.Slice(qs, func(i, j int) bool { return qs[i].Seq < qs[j].Seq })
	if len(qs) != 2 || qs[0].Status != "answered" || qs[0].Text != "Which rounding rule?" || len(qs[0].Options) != 2 ||
		!strings.Contains(string(qs[0].Answer), `"Half up"`) || !strings.Contains(string(qs[0].Answer), `"notes":"keep cents exact"`) || !strings.Contains(string(qs[0].Answer), `"via":"board"`) ||
		qs[1].Status != "open" || qs[1].Kind != "approval" {
		t.Errorf("questions = %+v", qs)
	}
	var gates []domain.Gate
	h.do("gate_list", m{"workspace_id": w.ID}, &gates)
	if len(gates) != 1 || gates[0].Kind != "merge" || gates[0].Summary != "2 PRs green" {
		t.Errorf("gates = %+v", gates)
	}

	// The session row carries the Claude id, so session_start resumes it.
	rows, _ := h.reg.Deps().Store.Sessions.ByClaudeID("0a1b2c3d-1111-2222-3333-444455556666")
	if len(rows) != 1 || rows[0].CardID != card[0].ID || rows[0].Status != "exited" {
		t.Errorf("session rows = %+v", rows)
	}

	// Importing again changes nothing.
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir, "dry_run": false}, &plan)
	if plan.Created != 0 || plan.Skipped != 4 {
		t.Errorf("second import = %+v", plan)
	}
	if n2, q2, g2 := importCounts(h, w.ID); n2 != n || q2 != q || g2 != g {
		t.Errorf("a second import changed the data: %d %d %d", n2, q2, g2)
	}
}

func TestImportPlannerDirErrors(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	h.fails("workspace_import_planner_dir", m{"workspace_id": w.ID}, "is not a folder", ".ai/runs/planner")
	empty := t.TempDir()
	h.fails("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": empty}, "no sessions/*.md")
	// The default folder is <root>/.ai/runs/planner.
	writeFile(t, filepath.Join(w.Root, ".ai", "runs", "planner", "sessions", "solo.md"), "- task: none yet\n")
	var plan ImportOut
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID}, &plan)
	if len(plan.Cards) != 1 || plan.Cards[0].Nick != "solo" || plan.Cards[0].Column != "Working" {
		t.Errorf("plan = %+v", plan)
	}
}

func TestAnImportedCardResumesItsClaudeConversation(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	dir := plannerFixture(t)
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir, "dry_run": false}, nil)
	time.Sleep(100 * time.Millisecond)
	if h.file(".started") != "" {
		t.Fatal("importing starts nothing")
	}
	h.do("session_start", m{"card": "billing-fix"}, nil)
	waitUntil(t, "claude", func() bool { return h.file(".started") != "" })
	if !strings.Contains(h.file(".argv"), "--resume\x000a1b2c3d-1111-2222-3333-444455556666") {
		t.Errorf("argv = %q", h.file(".argv"))
	}
}
