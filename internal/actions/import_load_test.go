package actions

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// bigPlannerFolder writes a planner run folder shaped like a real, long-lived one: dozens of
// sessions with long histories, hundreds of questions, a log of thousands of lines, and thousands
// of chat and inbox files that the import must never touch.
func bigPlannerFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "planner")
	var log strings.Builder
	log.WriteString("# Planner transition log (append-only)\n\n")
	for i := 0; i < 53; i++ {
		nick := fmt.Sprintf("task-%02d", i)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n- task: ACME-%d\n- claude session: 0A1B2C3D-0000-0000-0000-%012d\n- created: 2026-10-01 09:00\n\n", nick, 100+i, i)
		for j := 0; j < 120; j++ {
			fmt.Fprintf(&b, "## 2026-10-%02d 10:%02d · session → planner · progress — Step %d\n\n%s\n\n", 1+j%27, j%60, j, strings.Repeat("Detail of the work done in this step. ", 40))
		}
		writeFile(t, filepath.Join(dir, "sessions", nick+".md"), b.String())
		for q := 1; q <= 7; q++ {
			writeFile(t, filepath.Join(dir, "questions", nick, fmt.Sprintf("Q-%02d.json", q)),
				fmt.Sprintf(`{"id":"Q-%02d","kind":"decision","text":"Which option?","options":[{"label":"A"},{"label":"B"}],"status":"answered","selections":["A"],"asked_at":"2026-10-02T10:00:00Z","answered_at":"2026-10-02T11:00:00Z"}`, q))
		}
		if i%3 == 0 {
			writeFile(t, filepath.Join(dir, "gates", nick+".json"), `{"kind":"merge","summary":"PR is green","since":"2026-10-03T10:00:00Z"}`)
		}
	}
	for i := 0; i < 3800; i++ {
		fmt.Fprintf(&log, "- 2026-10-%02d 10:%02d · task-%02d · in-progress → in-progress · note %d\n", 1+i%27, i%60, i%53, i)
	}
	writeFile(t, filepath.Join(dir, "log.md"), log.String())
	for i := 0; i < 2600; i++ {
		writeFile(t, filepath.Join(dir, "chat", fmt.Sprintf("c%04d.json", i)), `{"text":"hello"}`)
	}
	for i := 0; i < 460; i++ {
		writeFile(t, filepath.Join(dir, "inbox", fmt.Sprintf("m%04d.md", i)), "an inbox message")
	}
	return dir
}

func TestImportOfALargePlannerFolderIsFastAndNeverBlocksOtherActions(t *testing.T) {
	dir := bigPlannerFolder(t)
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")

	// While the import runs, app_status must keep answering within a moment.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var slowest time.Duration
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			t0 := time.Now()
			if _, err := h.reg.Invoke(context.Background(), You, "app_status", ""); err != nil {
				t.Errorf("app_status during an import: %v", err)
				return
			}
			mu.Lock()
			if d := time.Since(t0); d > slowest {
				slowest = d
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	t0 := time.Now()
	var out ImportOut
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir}, &out)
	if out.Created != 53 || !out.DryRun || time.Since(t0) > 10*time.Second {
		t.Fatalf("dry run: created=%d took %v", out.Created, time.Since(t0))
	}
	if cards := h.cards(m{"workspace_id": w.ID}); len(cards) != 0 {
		t.Fatalf("a dry run writes nothing, but %d cards exist", len(cards))
	}
	t0 = time.Now()
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir, "dry_run": false}, &out)
	if out.Created != 53 || time.Since(t0) > 20*time.Second {
		t.Fatalf("import: created=%d took %v", out.Created, time.Since(t0))
	}
	close(stop)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if slowest > time.Second {
		t.Errorf("app_status took %v while the import ran", slowest)
	}
	cs := h.cards(m{"workspace_id": w.ID})
	if len(cs) != 53 {
		t.Fatalf("cards = %d", len(cs))
	}
	// Importing again changes nothing.
	h.do("workspace_import_planner_dir", m{"workspace_id": w.ID, "dir": dir, "dry_run": false}, &out)
	if out.Created != 0 || out.Skipped != 53 {
		t.Errorf("second import = %+v", out)
	}
}

func TestImportStopsAtItsTimeBudgetAndWritesNothing(t *testing.T) {
	dir := bigPlannerFolder(t)
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	// A budget that has already run out when reading starts.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	_, err := h.reg.Invoke(ctx, You, "workspace_import_planner_dir", fmt.Sprintf(`{"workspace_id":%q,"dir":%q,"dry_run":false}`, w.ID, dir))
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("an exhausted budget stops the import: %v", err)
	}
	if cards := h.cards(m{"workspace_id": w.ID}); len(cards) != 0 {
		t.Errorf("nothing is written when it stops: %d cards", len(cards))
	}
}

// A mutating action that takes a long time inside its transaction must not stop app_status or
// the reads the UI makes.
func TestASlowMutatingActionDoesNotBlockAppStatus(t *testing.T) {
	h := newSessionHarness(t, false)
	h.workspace("Acme")
	entered, release := make(chan struct{}), make(chan struct{})
	Register(h.reg, Spec{Name: "zz_slow_test", Description: "test only", Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in struct{}) (struct{}, error) {
			// Hold a write transaction open, as a long handler would.
			if _, err := c.Store.Cards.List("none", storeFilter(false)); err != nil {
				return struct{}{}, err
			}
			if err := c.Store.Settings.Set("zz.slow", []byte(`1`)); err != nil {
				return struct{}{}, err
			}
			close(entered)
			<-release
			return struct{}{}, nil
		})
	done := make(chan error, 1)
	go func() {
		_, err := h.reg.Invoke(context.Background(), You, "zz_slow_test", "")
		done <- err
	}()
	<-entered
	for i := 0; i < 5; i++ {
		for _, call := range []struct {
			who  Actor
			name string
		}{{Actor{Kind: KindExternal}, "app_status"}, {You, "workspace_list"}} {
			t0 := time.Now()
			if _, err := h.reg.Invoke(context.Background(), call.who, call.name, ""); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(t0); d > 500*time.Millisecond {
				t.Errorf("%s took %v while a write transaction was open", call.name, d)
			}
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
