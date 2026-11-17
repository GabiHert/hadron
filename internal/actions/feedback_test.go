package actions

import (
	"strings"
	"testing"
)

func TestFeedbackCreateListUpdate(t *testing.T) {
	h, _ := logHarness(t)
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "  The board flickers when I drag a card.  ", "area": "board"}, &f)
	if f.ID == "" || f.Status != "new" || f.Kind != "bug" || f.Text != "The board flickers when I drag a card." || f.Version != "test" || f.Area != "board" || string(f.Diagnostics) != "null" {
		t.Fatalf("created = %+v", f)
	}
	h.do("feedback_create", m{"kind": "idea", "text": "Let me pin a card"}, nil)
	h.do("feedback_create", m{"kind": "praise", "text": "The chat is lovely"}, nil)

	var all, onlyNew []FeedbackView
	h.do("feedback_list", nil, &all)
	if len(all) != 3 || all[0].Kind != "praise" || all[2].ID != f.ID {
		t.Fatalf("newest first: %+v", all)
	}
	var upd FeedbackView
	h.do("feedback_update", m{"id": f.ID, "status": "in_progress", "note": "Reproduced on drag over a folded column", "github_url": "https://github.com/GabiHert/hadron/issues/9"}, &upd)
	if upd.Status != "in_progress" || upd.Note != "Reproduced on drag over a folded column" || !strings.HasSuffix(upd.GithubURL, "/issues/9") {
		t.Errorf("updated = %+v", upd)
	}
	h.do("feedback_update", m{"id": f.ID, "status": "done"}, &upd)
	if upd.Note == "" {
		t.Error("an update that omits the note keeps it")
	}
	h.do("feedback_list", m{"status": "new"}, &onlyNew)
	if len(onlyNew) != 2 {
		t.Errorf("filter: %+v", onlyNew)
	}
	h.do("feedback_list", m{"status": "done"}, &onlyNew)
	if len(onlyNew) != 1 || onlyNew[0].ID != f.ID {
		t.Errorf("filter: %+v", onlyNew)
	}
}

func TestFeedbackValidation(t *testing.T) {
	h, _ := logHarness(t)
	h.fails("feedback_create", m{"kind": "rant", "text": "x"}, "must be one of bug, idea, annoyance, praise")
	h.fails("feedback_create", m{"kind": "bug", "text": "   "}, "text is empty")
	h.fails("feedback_create", m{"kind": "bug", "text": strings.Repeat("x", 9000)}, "keep it under")
	h.fails("feedback_list", m{"status": "someday"}, "must be one of new, planned")
	h.fails("feedback_update", m{"id": "nope", "status": "done"}, "no feedback")
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "x"}, &f)
	h.fails("feedback_update", m{"id": f.ID, "status": "someday"}, "must be one of")
}

func TestFeedbackWithDiagnostics(t *testing.T) {
	h, l := logHarness(t)
	l.SetDebug(true)
	l.Info("a line for the log tail")
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "Crash on start", "area": "settings:rules", "attach_diagnostics": true}, &f)
	if string(f.Diagnostics) == "null" || !strings.Contains(string(f.Diagnostics), "recent_log_lines") || f.Shared {
		t.Fatalf("created = %+v", f)
	}
}
