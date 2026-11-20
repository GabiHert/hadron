package store

import (
	"strconv"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func TestListNewestKeepsTheTailInOrder(t *testing.T) {
	s := open(t)
	w := mustWS(t, s, "w")
	for i := range 50 {
		m := domain.Message{WorkspaceID: w.ID, Channel: "planner", Author: "you", Text: strconv.Itoa(i)}
		if err := s.Messages.Create(&m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Messages.ListNewest(w.ID, "planner", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 || got[0].Text != "40" || got[9].Text != "49" {
		t.Fatalf("newest 10 = %d messages, %q to %q", len(got), got[0].Text, got[len(got)-1].Text)
	}
	all, _ := s.Messages.ListNewest(w.ID, "planner", "", 0)
	if len(all) != 50 || all[0].Text != "0" {
		t.Fatalf("limit 0 should be the whole channel, got %d", len(all))
	}
	if more, _ := s.Messages.ListNewest(w.ID, "planner", "", 500); len(more) != 50 {
		t.Fatalf("a limit past the channel gives it all, got %d", len(more))
	}
}
