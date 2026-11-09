package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

// open returns a store in a temp HADRON_HOME. Tests never touch ~/.hadron.
func open(t *testing.T) *Store {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	s, err := Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustWS(t *testing.T, s *Store, name string) domain.Workspace {
	t.Helper()
	w := domain.Workspace{Name: name, Color: "#24408e", Root: "/tmp/" + name}
	if err := s.Workspaces.Create(&w); err != nil {
		t.Fatal(err)
	}
	return w
}

func mustCol(t *testing.T, s *Store, ws, name, role string) domain.Column {
	t.Helper()
	c := domain.Column{WorkspaceID: ws, Name: name, Role: role}
	if err := s.Columns.Create(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func mustCard(t *testing.T, s *Store, ws, col, nick string) domain.Card {
	t.Helper()
	c := domain.Card{WorkspaceID: ws, ColumnID: col, Nick: nick}
	if err := s.Cards.Create(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMigrationsApplyOnceAndSurviveReopen(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "hadron.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SchemaVersion()
	if err != nil || v != latestMigration(t) {
		t.Fatalf("SchemaVersion = %d, %v; want %d", v, err, latestMigration(t))
	}
	w := mustWS(t, s, "keep")
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var n int
	_ = s2.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n != latestMigration(t) {
		t.Fatalf("schema_migrations rows = %d after reopen, want %d", n, latestMigration(t))
	}
	got, err := s2.Workspaces.Get(w.ID)
	if err != nil || got.Name != "keep" {
		t.Fatalf("workspace after reopen = %+v, %v", got, err)
	}
}

func TestEveryTableFromTheSpecExists(t *testing.T) {
	s := open(t)
	for _, tbl := range []string{"workspaces", "columns", "cards", "sessions", "messages", "questions", "gates", "links", "rules", "views", "dash_blocks", "prompts", "launch_profiles", "scripts", "changes", "mcp_calls", "settings"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Errorf("table %s: %v", tbl, err)
		}
	}
}

func TestWorkspaceCRUD(t *testing.T) {
	s := open(t)
	a, b := mustWS(t, s, "a"), mustWS(t, s, "b")
	if a.Position != 0 || b.Position != 1 {
		t.Fatalf("positions = %d, %d; want 0, 1", a.Position, b.Position)
	}
	a.Integrations, a.FilterBar, a.UI = []string{"linear"}, []string{"card.assignee"}, map[string]any{"collapsed": true}
	a.Archived = true
	if err := s.Workspaces.Update(&a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Workspaces.Get(a.ID)
	if !got.Archived || got.Integrations[0] != "linear" || got.FilterBar[0] != "card.assignee" || got.UI["collapsed"] != true {
		t.Fatalf("round trip lost data: %+v", got)
	}
	live, _ := s.Workspaces.List(false)
	all, _ := s.Workspaces.List(true)
	if len(live) != 1 || len(all) != 2 {
		t.Fatalf("List(false)=%d, List(true)=%d; want 1, 2", len(live), len(all))
	}
	if err := s.Workspaces.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Workspaces.Get(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := s.Workspaces.Delete(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestColumnsRenameKeepsIdAndReorder(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	c1 := mustCol(t, s, w.ID, "Needs you", domain.RoleNeedsYou)
	c2 := mustCol(t, s, w.ID, "Working", domain.RoleActive)
	c3 := mustCol(t, s, w.ID, "Done", domain.RoleDone)
	c1.Name, c1.WIP, c1.Fold = "Inbox", 3, true
	if err := s.Columns.Update(&c1); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Columns.Get(c1.ID)
	if got.ID != c1.ID || got.Name != "Inbox" || got.WIP != 3 || !got.Fold || got.Role != domain.RoleNeedsYou {
		t.Fatalf("after rename: %+v", got)
	}
	if err := s.Columns.Reorder(w.ID, []string{c3.ID, c1.ID, c2.ID}); err != nil {
		t.Fatal(err)
	}
	cols, _ := s.Columns.List(w.ID)
	if cols[0].ID != c3.ID || cols[1].ID != c1.ID || cols[2].ID != c2.ID {
		t.Fatalf("order = %v", []string{cols[0].Name, cols[1].Name, cols[2].Name})
	}
	if err := s.Columns.Reorder(w.ID, []string{c1.ID}); err == nil {
		t.Fatal("Reorder with a missing column should fail")
	}
	if err := s.Columns.Reorder(w.ID, []string{c1.ID, c1.ID, c2.ID}); err == nil {
		t.Fatal("Reorder with a repeated column should fail")
	}
	bad := domain.Column{WorkspaceID: w.ID, Name: "x", Role: "nonsense"}
	if err := s.Columns.Create(&bad); err == nil {
		t.Fatal("an unknown role should violate the CHECK constraint")
	}
}

func TestDeleteMovingMovesCardsAndClearsReturnColumn(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	from := mustCol(t, s, w.ID, "From", domain.RoleActive)
	to := mustCol(t, s, w.ID, "To", domain.RoleActive)
	other := mustCol(t, s, w.ID, "Other", domain.RoleNeedsYou)
	keep := mustCard(t, s, w.ID, to.ID, "keep")
	m1 := mustCard(t, s, w.ID, from.ID, "m1")
	m2 := mustCard(t, s, w.ID, from.ID, "m2")
	elsewhere := mustCard(t, s, w.ID, other.ID, "elsewhere")
	elsewhere.ReturnColumnID = from.ID
	if err := s.Cards.Update(&elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := s.Tx(func(tx *Store) error { return tx.Columns.DeleteMoving(from.ID, to.ID) }); err != nil {
		t.Fatal(err)
	}
	cards, _ := s.Cards.List(w.ID, CardFilter{ColumnID: to.ID})
	if len(cards) != 3 || cards[0].ID != keep.ID || cards[1].ID != m1.ID || cards[2].ID != m2.ID {
		t.Fatalf("cards in target = %+v", cards)
	}
	if _, err := s.Columns.Get(from.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted column still there: %v", err)
	}
	e, _ := s.Cards.Get(elsewhere.ID)
	if e.ReturnColumnID != "" {
		t.Fatalf("return_column_id = %q, want cleared", e.ReturnColumnID)
	}
	if err := s.Tx(func(tx *Store) error { return tx.Columns.DeleteMoving(to.ID, to.ID) }); err == nil {
		t.Fatal("moving a column's cards into itself should fail")
	}
}

func TestCardCRUDFiltersAndNickUniqueness(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	w2 := mustWS(t, s, "w2")
	needs := mustCol(t, s, w.ID, "Needs", domain.RoleNeedsYou)
	work := mustCol(t, s, w.ID, "Work", domain.RoleActive)
	c1 := mustCard(t, s, w.ID, needs.ID, "alpha")
	c2 := mustCard(t, s, w.ID, work.ID, "beta")
	c2.Key, c2.Liveness, c2.Fields = "ACME-1", domain.LivenessWorking, map[string]any{"est": 3.0}
	if err := s.Cards.Update(&c2); err != nil {
		t.Fatal(err)
	}
	dup := domain.Card{WorkspaceID: w.ID, ColumnID: work.ID, Nick: "alpha"}
	if err := s.Cards.Create(&dup); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate nick = %v, want ErrDuplicate", err)
	}
	other := mustCol(t, s, w2.ID, "Work", domain.RoleActive)
	mustCard(t, s, w2.ID, other.ID, "alpha") // same nick in another workspace is fine

	byRole, _ := s.Cards.List(w.ID, CardFilter{Role: domain.RoleNeedsYou})
	byLive, _ := s.Cards.List(w.ID, CardFilter{Liveness: domain.LivenessWorking})
	all, _ := s.Cards.List(w.ID, CardFilter{})
	if len(byRole) != 1 || byRole[0].ID != c1.ID || len(byLive) != 1 || byLive[0].ID != c2.ID || len(all) != 2 {
		t.Fatalf("filters: role=%d live=%d all=%d", len(byRole), len(byLive), len(all))
	}
	if got, err := s.Cards.ByNick(w.ID, "ACME-1"); err != nil || got.ID != c2.ID {
		t.Fatalf("ByNick(key) = %+v, %v", got, err)
	}
	got, _ := s.Cards.Get(c2.ID)
	if got.Fields["est"] != 3.0 || got.Key != "ACME-1" {
		t.Fatalf("round trip: %+v", got)
	}
	c1.Archived = true
	_ = s.Cards.Update(&c1)
	vis, _ := s.Cards.List(w.ID, CardFilter{})
	inc, _ := s.Cards.List(w.ID, CardFilter{IncludeArchived: true})
	if len(vis) != 1 || len(inc) != 2 {
		t.Fatalf("archived: visible=%d included=%d", len(vis), len(inc))
	}
	if err := s.Cards.Delete(c2.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeletingAWorkspaceCascades(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	col := mustCol(t, s, w.ID, "c", domain.RoleActive)
	card := mustCard(t, s, w.ID, col.ID, "x")
	_ = s.Messages.Create(&domain.Message{WorkspaceID: w.ID, Channel: "planner", Author: "you", Text: "hi"})
	_ = s.Questions.Create(&domain.Question{WorkspaceID: w.ID, CardID: card.ID, Kind: "decision", Text: "?"})
	if err := s.Workspaces.Delete(w.ID); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"columns", "cards", "messages", "questions"} {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows after the workspace was deleted", tbl, n)
		}
	}
}

func TestSessionsAndPlannerLookup(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	col := mustCol(t, s, w.ID, "c", domain.RoleActive)
	card := mustCard(t, s, w.ID, col.ID, "x")
	pl := domain.Session{WorkspaceID: w.ID, Kind: "planner", Command: "claude", Cwd: "/tmp", PID: 11}
	cs := domain.Session{WorkspaceID: w.ID, CardID: card.ID, Kind: "card", Command: "claude", PID: 12}
	for _, x := range []*domain.Session{&pl, &cs} {
		if err := s.Sessions.Create(x); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Sessions.Latest(w.ID, ""); err != nil || got.ID != pl.ID || got.CardID != "" {
		t.Fatalf("planner = %+v, %v", got, err)
	}
	if got, err := s.Sessions.Latest(w.ID, card.ID); err != nil || got.ID != cs.ID {
		t.Fatalf("card session = %+v, %v", got, err)
	}
	cs.Status, cs.ExitCode, cs.ClaudeSessionID = "exited", 2, "abc"
	end := now()
	cs.EndedAt = &end
	if err := s.Sessions.Update(&cs); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Sessions.Get(cs.ID)
	if got.Status != "exited" || got.ExitCode != 2 || got.EndedAt == nil || got.ClaudeSessionID != "abc" {
		t.Fatalf("after update: %+v", got)
	}
	running, _ := s.Sessions.ListRunning()
	if len(running) != 1 || running[0].ID != pl.ID {
		t.Fatalf("running = %+v", running)
	}
	n, err := s.Sessions.MarkRunningExited()
	if err != nil || n != 1 {
		t.Fatalf("MarkRunningExited = %d, %v", n, err)
	}
}

func TestMessagesChannelsAndDelivery(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	m1 := domain.Message{WorkspaceID: w.ID, Channel: "planner", Author: "you", Text: "one"}
	m2 := domain.Message{WorkspaceID: w.ID, Channel: "card:1", Author: "you", Text: "two"}
	m3 := domain.Message{WorkspaceID: w.ID, Channel: "planner", Author: "planner", Text: "three", Kind: "widget", WidgetTitle: "t", WidgetHTML: "<b>x</b>"}
	for _, m := range []*domain.Message{&m1, &m2, &m3} {
		if err := s.Messages.Create(m); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Messages.List(w.ID, "planner", "")
	if len(got) != 2 || got[0].Text != "one" || got[1].WidgetHTML != "<b>x</b>" {
		t.Fatalf("planner channel = %+v", got)
	}
	if after, _ := s.Messages.List(w.ID, "planner", ts(m1.CreatedAt)); len(after) != 1 || after[0].ID != m3.ID {
		t.Fatalf("since filter = %+v", after)
	}
	m2.Delivered, m2.Reason = false, "planner_not_running"
	_ = s.Messages.Update(&m2)
	back, _ := s.Messages.Get(m2.ID)
	if back.Reason != "planner_not_running" || back.Kind != "text" {
		t.Fatalf("delivery fields: %+v", back)
	}
	if err := s.Messages.Create(&domain.Message{WorkspaceID: w.ID, Channel: "planner", Author: "stranger"}); err == nil {
		t.Fatal("an unknown author should violate the CHECK constraint")
	}
}

func TestQuestionsNumberPerCardAndAnswer(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	col := mustCol(t, s, w.ID, "c", domain.RoleActive)
	a, b := mustCard(t, s, w.ID, col.ID, "a"), mustCard(t, s, w.ID, col.ID, "b")
	q1 := domain.Question{WorkspaceID: w.ID, CardID: a.ID, Kind: "decision", Text: "go?", Options: []domain.QuestionOption{{Label: "Yes", Description: "d"}}}
	q2 := domain.Question{WorkspaceID: w.ID, CardID: a.ID, Kind: "approval", Text: "run?", Approval: json.RawMessage(`{"target":"PRD"}`)}
	q3 := domain.Question{WorkspaceID: w.ID, CardID: b.ID, Kind: "unblock", Text: "help"}
	for _, q := range []*domain.Question{&q1, &q2, &q3} {
		if err := s.Questions.Create(q); err != nil {
			t.Fatal(err)
		}
	}
	if q1.Seq != 1 || q2.Seq != 2 || q3.Seq != 1 {
		t.Fatalf("seq = %d, %d, %d; want 1, 2, 1", q1.Seq, q2.Seq, q3.Seq)
	}
	at := now()
	q1.Status, q1.Answer, q1.AnsweredAt = "answered", json.RawMessage(`{"selections":["Yes"],"notes":"n","via":"you"}`), &at
	if err := s.Questions.Update(&q1); err != nil {
		t.Fatal(err)
	}
	open, _ := s.Questions.List(w.ID, "", "open")
	forA, _ := s.Questions.List(w.ID, a.ID, "")
	if len(open) != 2 || len(forA) != 2 {
		t.Fatalf("open=%d forA=%d; want 2, 2", len(open), len(forA))
	}
	got, _ := s.Questions.Get(q1.ID)
	if got.Status != "answered" || got.AnsweredAt == nil || !strings.Contains(string(got.Answer), `"notes":"n"`) || got.Options[0].Label != "Yes" {
		t.Fatalf("answered question: %+v", got)
	}
	g2, _ := s.Questions.Get(q2.ID)
	if !strings.Contains(string(g2.Approval), "PRD") {
		t.Fatalf("approval lost: %s", g2.Approval)
	}
}

func TestGatesAndLinks(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	col := mustCol(t, s, w.ID, "c", domain.RoleActive)
	card := mustCard(t, s, w.ID, col.ID, "x")
	g := domain.Gate{WorkspaceID: w.ID, CardID: card.ID, Kind: "merge", Summary: "PR #1"}
	if err := s.Gates.Create(&g); err != nil {
		t.Fatal(err)
	}
	if gs, _ := s.Gates.List(w.ID, card.ID); len(gs) != 1 || gs[0].Since.IsZero() {
		t.Fatalf("gates = %+v", gs)
	}
	if err := s.Gates.Delete(g.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Gates.Create(&domain.Gate{WorkspaceID: w.ID, CardID: card.ID, Kind: "bogus"}); err == nil {
		t.Fatal("unknown gate kind should fail")
	}

	l := domain.Link{WorkspaceID: w.ID, CardID: card.ID, Integration: "jira", ItemID: "D-1", Title: "t", Fields: map[string]any{"status": "To Do"}}
	if err := s.Links.Create(&l); err != nil {
		t.Fatal(err)
	}
	dup := domain.Link{WorkspaceID: w.ID, CardID: card.ID, Integration: "jira", ItemID: "D-1"}
	if err := s.Links.Create(&dup); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate link = %v", err)
	}
	l.Fields["status"] = "Finished"
	synced := now()
	l.SyncedAt = &synced
	_ = s.Links.Update(&l)
	ls, _ := s.Links.ListByCard(card.ID)
	if len(ls) != 1 || ls[0].Fields["status"] != "Finished" || ls[0].SyncedAt == nil {
		t.Fatalf("links = %+v", ls)
	}
	if err := s.Links.Delete(l.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRulesViewsBlocks(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	r1 := domain.Rule{WorkspaceID: w.ID, Name: "one", Enabled: true, Builtin: true, Trigger: json.RawMessage(`{"on":"question_opened"}`), Actions: json.RawMessage(`[{"move":{"column_id":"x"}}]`)}
	r2 := domain.Rule{WorkspaceID: w.ID, Name: "two", Condition: `jira.status == "Finished"`}
	for _, r := range []*domain.Rule{&r1, &r2} {
		if err := s.Rules.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	r2.Enabled = true
	_ = s.Rules.Update(&r2)
	rs, _ := s.Rules.List(w.ID)
	if len(rs) != 2 || rs[0].Name != "one" || !rs[0].Builtin || !rs[1].Enabled || rs[1].Condition == "" || !strings.Contains(string(rs[0].Trigger), "question_opened") {
		t.Fatalf("rules = %+v", rs)
	}
	if err := s.Rules.Delete(r2.ID); err != nil {
		t.Fatal(err)
	}

	v := domain.View{WorkspaceID: w.ID, Name: "Red", Filters: json.RawMessage(`{"vals":{"a":"b"}}`)}
	if err := s.Views.Create(&v); err != nil {
		t.Fatal(err)
	}
	v.Name = "Red checks"
	_ = s.Views.Update(&v)
	if vs, _ := s.Views.List(w.ID); len(vs) != 1 || vs[0].Name != "Red checks" {
		t.Fatalf("views = %+v", vs)
	}
	_ = s.Views.Delete(v.ID)

	at := now()
	b := domain.Block{WorkspaceID: w.ID, Type: "stats", Title: "Cards", Span: 2, Source: json.RawMessage(`{"query":"cards.count"}`), Text: "t", TextAt: &at}
	if err := s.Blocks.Create(&b); err != nil {
		t.Fatal(err)
	}
	bad := domain.Block{WorkspaceID: w.ID, Type: "x", Span: 3}
	if err := s.Blocks.Create(&bad); err == nil {
		t.Fatal("span 3 should violate the CHECK constraint")
	}
	got, _ := s.Blocks.Get(b.ID)
	if got.Span != 2 || got.TextAt == nil || !strings.Contains(string(got.Source), "cards.count") {
		t.Fatalf("block = %+v", got)
	}
	_ = s.Blocks.Delete(b.ID)
}

func TestPromptsLaunchProfilesScripts(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	tpl := "You are {{workspace.name}}"
	if err := s.Prompts.Set(w.ID, "planner", &tpl); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompts.Set(w.ID, "planner", nil); err != nil { // upsert back to the default
		t.Fatal(err)
	}
	p, err := s.Prompts.Get(w.ID, "planner")
	if err != nil || p.Template != nil {
		t.Fatalf("prompt = %+v, %v; want nil template", p, err)
	}
	if ps, _ := s.Prompts.List(w.ID); len(ps) != 1 {
		t.Fatalf("prompts = %d rows, want 1", len(ps))
	}

	lp := domain.LaunchProfile{WorkspaceID: w.ID, Name: "brief", Args: []string{"--x"}, FirstPrompt: "/brief {{card.nick}}", Env: map[string]string{"A": "1"}, Worktree: true}
	if err := s.LaunchProfiles.Create(&lp); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LaunchProfiles.Get(lp.ID)
	if got.Command != "claude" || got.Args[0] != "--x" || got.Env["A"] != "1" || !got.Worktree {
		t.Fatalf("profile = %+v", got)
	}
	got.Name = "renamed"
	_ = s.LaunchProfiles.Update(&got)
	_ = s.LaunchProfiles.Delete(got.ID)

	global := domain.Script{Name: "g", Kind: "script"}
	local := domain.Script{WorkspaceID: w.ID, Name: "l", Kind: "integration", Manifest: json.RawMessage(`{"name":"l"}`)}
	_ = s.Scripts.Create(&global)
	_ = s.Scripts.Create(&local)
	if global.Status != "pending" {
		t.Fatalf("new script status = %q, want pending", global.Status)
	}
	at := now()
	local.Status, local.ApprovedBy, local.ApprovedAt = "enabled", "you", &at
	_ = s.Scripts.Update(&local)
	ss, _ := s.Scripts.List(w.ID)
	if len(ss) != 2 || ss[0].WorkspaceID != "" || ss[1].Status != "enabled" || ss[1].ApprovedAt == nil {
		t.Fatalf("scripts = %+v", ss)
	}
	_ = s.Scripts.Delete(global.ID)
}

func TestChangesCallsAndSettings(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	c := domain.Change{WorkspaceID: w.ID, Actor: "you", Action: "column_update", Before: json.RawMessage(`{"name":"a"}`), After: json.RawMessage(`{"name":"b"}`)}
	if err := s.Changes.Create(&c); err != nil {
		t.Fatal(err)
	}
	if err := s.Changes.MarkReverted(c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Changes.Get(c.ID)
	if !got.Reverted || string(got.Before) != `{"name":"a"}` {
		t.Fatalf("change = %+v", got)
	}

	long := domain.MCPCall{WorkspaceID: w.ID, Actor: "you", Tool: "card_create", OK: true, Args: json.RawMessage(`"` + strings.Repeat("x", 5000) + `"`)}
	if err := s.MCPCalls.Create(&long); err != nil {
		t.Fatal(err)
	}
	calls, _ := s.MCPCalls.List(w.ID, 10)
	if len(calls) != 1 || len(calls[0].Args) > maxCallArgs+8 {
		t.Fatalf("args were not truncated: %d bytes", len(calls[0].Args))
	}
	if !json.Valid(calls[0].Args) {
		t.Fatal("truncated args are not valid JSON")
	}

	if _, err := s.Settings.Get("theme"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing setting = %v", err)
	}
	_ = s.Settings.Set("theme", json.RawMessage(`"dark"`))
	_ = s.Settings.Set("theme", json.RawMessage(`"light"`))
	v, _ := s.Settings.Get("theme")
	all, _ := s.Settings.All()
	if string(v) != `"light"` || len(all) != 1 {
		t.Fatalf("theme = %s, all = %v", v, all)
	}
}

func TestTxRollsBackOnErrorAndRefusesNesting(t *testing.T) {
	s := open(t)
	err := s.Tx(func(tx *Store) error {
		if err := tx.Workspaces.Create(&domain.Workspace{Name: "ghost"}); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Tx error = %v", err)
	}
	if ws, _ := s.Workspaces.List(true); len(ws) != 0 {
		t.Fatalf("rolled-back workspace is still there: %+v", ws)
	}
	err = s.Tx(func(tx *Store) error { return tx.Tx(func(*Store) error { return nil }) })
	if err == nil {
		t.Fatal("nested Tx should fail")
	}
	if err := s.Tx(func(tx *Store) error { return tx.Workspaces.Create(&domain.Workspace{Name: "real"}) }); err != nil {
		t.Fatal(err)
	}
	if ws, _ := s.Workspaces.List(true); len(ws) != 1 {
		t.Fatalf("committed workspace missing: %+v", ws)
	}
}

// latestMigration is the number of embedded migrations, so adding one does not break this test.
func latestMigration(t *testing.T) int {
	t.Helper()
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestListRestorableReturnsThePlannerFirstAndOneSessionPerOwner(t *testing.T) {
	s := open(t)
	w := domain.Workspace{Name: "Acme", Root: t.TempDir()}
	if err := s.Workspaces.Create(&w); err != nil {
		t.Fatal(err)
	}
	col := domain.Column{WorkspaceID: w.ID, Name: "Working", Role: "active"}
	if err := s.Columns.Create(&col); err != nil {
		t.Fatal(err)
	}
	mk := func(nick string) domain.Card {
		c := domain.Card{WorkspaceID: w.ID, ColumnID: col.ID, Nick: nick}
		if err := s.Cards.Create(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := mk("billing-fix"), mk("invoice-sync")
	add := func(card string, restore bool, at time.Time) domain.Session {
		kind := "card"
		if card == "" {
			kind = "planner"
		}
		x := domain.Session{WorkspaceID: w.ID, CardID: card, Kind: kind, Command: "claude", Cwd: "/", Status: "exited", StartedAt: at, Restore: restore}
		if err := s.Sessions.Create(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	t0 := time.Now().Add(-time.Hour)
	add(a.ID, true, t0.Add(1*time.Minute))
	add(b.ID, true, t0.Add(2*time.Minute))
	newerA := add(a.ID, true, t0.Add(3*time.Minute)) // the newest row of an owner wins
	add(b.ID, false, t0.Add(4*time.Minute))          // not flagged: ignored
	pl := add("", true, t0.Add(5*time.Minute))
	got, err := s.Sessions.ListRestorable()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != pl.ID || got[1].CardID != b.ID || got[2].ID != newerA.ID {
		t.Fatalf("restorable = %+v", got)
	}
	if err := s.Sessions.ClearRestore(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Sessions.ListRestorable(); len(got) != 0 {
		t.Errorf("after ClearRestore: %+v", got)
	}
}
