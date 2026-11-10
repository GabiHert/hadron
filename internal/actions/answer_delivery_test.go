package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

var deliveryQuestion = m{"kind": "decision", "text": "Round invoices up or to nearest?",
	"options": []m{{"label": "Nearest", "description": "recommended"}, {"label": "Up"}}}

// TestAnAnsweredQuestionIsTypedIntoTheIdleAskingSession is the regression test for feedback f308498e:
// an idle card's session sat waiting forever, because no answer, from the user or relayed by the
// planner, was ever typed into its terminal.
func TestAnAnsweredQuestionIsTypedIntoTheIdleAskingSession(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508"}, &card)
	waitUntil(t, "the fake claude to start", func() bool { return h.file(".started") != "" })
	session := Actor{Kind: KindSession, Name: card.Nick, WorkspaceID: w.ID, CardID: card.ID}

	// The user answers in the UI.
	var q1 domain.Question
	if err := h.as(session, "question_ask", deliveryQuestion, &q1); err != nil {
		t.Fatal(err)
	}
	h.do("question_answer", m{"question_id": q1.ID, "selections": []string{"Nearest"}, "notes": "half up on ties"}, nil)
	waitUntil(t, "the user's answer in the session", func() bool {
		return strings.Contains(h.file(".in"), "[· answer] Q-01: Nearest; notes: half up on ties")
	})

	// The planner relays an answer the user gave in chat.
	h.do("planner_policy_set", m{"policy": "user_only"}, nil)
	var q2 domain.Question
	if err := h.as(session, "question_ask", deliveryQuestion, &q2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // the user's message must be newer than the question
	var userMsg domain.Message
	h.do("chat_send", m{"channel": "card:" + card.ID, "text": "round it up"}, &userMsg)
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "question_answer", m{"question_id": q2.ID, "selections": []string{"Up"}, "on_behalf_of": "user", "source_message_id": userMsg.ID}, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the relayed answer in the session", func() bool {
		return strings.Contains(h.file(".in"), "[· answer, relayed by the planner] Q-02: Up")
	})
	if n := strings.Count(h.file(".in"), "[· answer"); n != 2 {
		t.Fatalf("the session got %d answer lines, want 2:\n%s", n, h.file(".in"))
	}
}

func TestAnAnswerForAStoppedSessionSaysSoInTheCardChat(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "idle-one", "start": false}, &card)
	var q domain.Question
	if err := h.as(Actor{Kind: KindSession, Name: card.Nick, WorkspaceID: w.ID, CardID: card.ID}, "question_ask", deliveryQuestion, &q); err != nil {
		t.Fatal(err)
	}
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "card:" + card.ID}, &msgs)
	found := false
	for _, msg := range msgs {
		found = found || (msg.Kind == "event" && strings.Contains(msg.Text, "not typed into the session because it is not running"))
	}
	if !found {
		t.Fatalf("no note about the undelivered answer: %+v", msgs)
	}
}
