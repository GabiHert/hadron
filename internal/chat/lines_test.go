package chat

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func TestParseChannel(t *testing.T) {
	for in, want := range map[string][2]string{"planner": {"planner", ""}, " card:billing-fix ": {"card", "billing-fix"}, "card:ACME-1": {"card", "ACME-1"}} {
		kind, ref, err := ParseChannel(in)
		if err != nil || kind != want[0] || ref != want[1] {
			t.Errorf("%q = %q %q %v", in, kind, ref, err)
		}
	}
	for _, bad := range []string{"", "card:", "cards:x", "Planner", "other"} {
		if _, _, err := ParseChannel(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestSanitize(t *testing.T) {
	got, err := Sanitize("\x1b[2Jhi\x00 there\r\nnext line\ttab")
	if err != nil || got != "[2Jhi there\nnextlinetab" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := Sanitize(" \x07\n\t "); err != ErrEmpty {
		t.Errorf("blank = %v", err)
	}
	long, _ := Sanitize(strings.Repeat("a", MaxText+500))
	if len(long) != MaxText {
		t.Errorf("length = %d", len(long))
	}
}

func TestNotices(t *testing.T) {
	c := domain.Card{Key: "ACME-9", Nick: "x"}
	if got := ReportNotice(c, "done", strings.Repeat("é", 250)); got != "[ACME-9 x] report done: "+strings.Repeat("é", MaxNotice) {
		t.Errorf("report = %q", got)
	}
	if CardLine(domain.Card{Nick: "y"}, "hi") != "[y] hi" || PlannerLine("hi") != "[planner] hi" {
		t.Error("tags")
	}
}
