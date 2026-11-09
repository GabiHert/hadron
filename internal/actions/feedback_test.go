package actions

import (
	"net/url"
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

func TestFeedbackWithDiagnosticsAndTheIssueLink(t *testing.T) {
	h, l := logHarness(t)
	l.SetDebug(true)
	h.workspace("W")
	for i := 0; i < 300; i++ {
		l.Info("filler line for the log tail", "n", i, "pad", strings.Repeat("p", 80))
	}
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "Crash on start & more <b>", "area": "settings:rules", "attach_diagnostics": true}, &f)
	if string(f.Diagnostics) == "null" || !strings.Contains(string(f.Diagnostics), "recent_log_lines") {
		t.Fatalf("diagnostics = %s", f.Diagnostics)
	}
	u, err := url.Parse(f.IssueURL)
	if err != nil || u.Host != "github.com" || u.Path != "/GabiHert/hadron/issues/new" {
		t.Fatalf("issue url = %q (%v)", f.IssueURL, err)
	}
	if got := u.Query().Get("title"); got != "[bug] Crash on start & more <b>" {
		t.Errorf("title = %q", got)
	}
	body := u.Query().Get("body")
	for _, want := range []string{"Crash on start & more <b>", "Area: settings:rules", "Hadron version: test"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if len(f.IssueURL) > issueURLLimit+200 {
		t.Errorf("the link is %d bytes, over the limit", len(f.IssueURL))
	}
	if !strings.Contains(body, "Diagnostics") {
		t.Error("what fits of the diagnostics is attached")
	}

	// A huge text still gives a link that fits.
	var big FeedbackView
	h.do("feedback_create", m{"kind": "idea", "text": strings.Repeat("a long wish ", 600), "attach_diagnostics": true}, &big)
	if len(big.IssueURL) > issueURLLimit+200 {
		t.Errorf("the link is %d bytes", len(big.IssueURL))
	}
	bu, _ := url.Parse(big.IssueURL)
	if !strings.Contains(bu.Query().Get("body"), "Hadron version: test") && !strings.HasSuffix(bu.Query().Get("body"), "…") {
		t.Error("a trimmed body ends visibly trimmed")
	}
}
