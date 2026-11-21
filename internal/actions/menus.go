package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// Claude Code's own menus on a card's terminal (an MCP server approval, a permission prompt, an
// AskUserQuestion) become questions on the card, so they reach the planner and the user. Answering
// the question presses the keys; answering in the Terminal closes the question.

// ViaTerminal is the "via" of a menu question that was answered in the Terminal.
const ViaTerminal = "terminal"

// SessionMenuIn is the input of session_menu.
type SessionMenuIn struct {
	Card string `json:"card" jsonschema:"the card: its id, nickname or key"`
}

// SessionMenuOut is what session_menu returns.
type SessionMenuOut struct {
	// Open is false when the card's terminal shows no menu.
	Open bool           `json:"open"`
	Menu *sessions.Menu `json:"menu,omitempty"`
	// QuestionID is the card's open question for this menu, when it has one.
	QuestionID string `json:"question_id,omitempty"`
}

// SessionMenuAnswerIn is the input of session_menu_answer.
type SessionMenuAnswerIn struct {
	Card    string   `json:"card" jsonschema:"the card: its id, nickname or key"`
	Choices []string `json:"choices,omitempty" jsonschema:"the rows to pick, by label or number; several only when the menu takes several (check boxes)"`
	Text    string   `json:"text,omitempty" jsonschema:"text for the menu's free-text row (Type something); with another row chosen, or when the menu has none, it is sent to Claude as a message once the menu closes"`
	Cancel  bool     `json:"cancel,omitempty" jsonschema:"press Esc to close the menu without choosing"`
}

// SessionMenuAnswerOut is what session_menu_answer returns.
type SessionMenuAnswerOut struct {
	sessions.MenuResult
	// QuestionID is the menu's question, now answered, when it had one.
	QuestionID string `json:"question_id,omitempty"`
}

func registerMenus(r *Registry) {
	Register(r, Spec{Name: "session_menu", Description: "Read the Claude Code menu open on a card's terminal, if any: an MCP server approval, trust this folder, a tool permission prompt, an AskUserQuestion (one choice or several), the plan approval, or a select list such as /model. It returns the kind, the title and text, each row with its number, description and whether it is highlighted or ticked, and the card's open question for it. Hadron turns each such menu into a question on the card by itself; answer it with question_answer or session_menu_answer."},
		func(ctx context.Context, c *Call, in SessionMenuIn) (SessionMenuOut, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionMenuOut{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, false, "session_menu"); err != nil {
				return SessionMenuOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return SessionMenuOut{}, err
			}
			m, err := sv.CardMenu(card.WorkspaceID, card.ID)
			if errors.Is(err, sessions.ErrNoMenu) {
				return SessionMenuOut{}, nil
			}
			if err != nil {
				return SessionMenuOut{}, fmt.Errorf("card %q: %v", card.Nick, err)
			}
			out := SessionMenuOut{Open: true, Menu: m}
			if q, ok := openMenuQuestion(c.Store, card, m.Sig); ok {
				out.QuestionID = q.ID
			}
			return out, nil
		})

	Register(r, Spec{Name: "session_menu_answer", Description: "Answer the Claude Code menu open on a card's terminal: Hadron moves the highlight with the arrow keys, ticks rows with Space, types text into the free-text row and presses Enter (or Esc with cancel), then checks that the menu closed, trying once more if not. A multi-select AskUserQuestion's review step is confirmed too. When the menu has an open question on the card, the planner policy applies as for question_answer, and the question is marked answered.",
		Mutates: true, NoTx: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in SessionMenuAnswerIn) (SessionMenuAnswerOut, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionMenuAnswerOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return SessionMenuAnswerOut{}, err
			}
			m, err := sv.CardMenu(card.WorkspaceID, card.ID)
			if errors.Is(err, sessions.ErrNoMenu) {
				return SessionMenuAnswerOut{}, fmt.Errorf("card %q shows no menu. Look at it with session_read_screen", card.Nick)
			}
			if err != nil {
				return SessionMenuAnswerOut{}, fmt.Errorf("card %q: %v", card.Nick, err)
			}
			q, hasQ := openMenuQuestion(c.Store, card, m.Sig)
			if hasQ {
				if err := mayAnswer(c, q); err != nil {
					return SessionMenuAnswerOut{}, err
				}
			} else if err := mayControl(c, card.WorkspaceID, &card, true, "session_menu_answer"); err != nil {
				return SessionMenuAnswerOut{}, err
			}
			res, err := sv.AnswerMenu(ctx, card.WorkspaceID, card.ID, sessions.MenuChoice{Sig: m.Sig, Labels: in.Choices, Text: in.Text, Cancel: in.Cancel})
			if err != nil {
				return SessionMenuAnswerOut{MenuResult: res}, fmt.Errorf("card %q: %v", card.Nick, err)
			}
			out := SessionMenuAnswerOut{MenuResult: res}
			if hasQ {
				out.QuestionID = q.ID
				labels := in.Choices
				if in.Cancel {
					labels = []string{sessions.CancelLabel}
				}
				err := c.Deps.Store.Tx(func(tx *store.Store) error {
					cur, err := tx.Questions.Get(q.ID)
					if err != nil || cur.Status != "open" {
						return err
					}
					return closeMenuQuestion(tx, card, cur, labels, in.Text, authorOf(c.Actor))
				})
				if err != nil {
					return out, err
				}
				c.SetWorkspace(card.WorkspaceID)
				sv.SetWaiting(card.WorkspaceID, card.ID, hasOpen(c.Deps.Store, card))
			}
			return out, nil
		})
}

func hasOpen(st *store.Store, card domain.Card) bool {
	open, err := st.Questions.List(card.WorkspaceID, card.ID, "open")
	return err == nil && len(open) > 0
}

// openMenuQuestion finds the card's open question for the menu with this sig.
func openMenuQuestion(st *store.Store, card domain.Card, sig string) (domain.Question, bool) {
	open, err := st.Questions.List(card.WorkspaceID, card.ID, "open")
	if err != nil {
		return domain.Question{}, false
	}
	for _, q := range open {
		if q.Menu != nil && q.Menu.Sig == sig {
			return q, true
		}
	}
	return domain.Question{}, false
}

// closeMenuQuestion records a menu question's answer, given by Hadron's keys (via the one who
// answered) or in the Terminal (ViaTerminal, no labels), and notes it in the card's chat.
func closeMenuQuestion(st *store.Store, card domain.Card, q domain.Question, labels []string, notes, via string) error {
	if labels == nil {
		labels = []string{}
	}
	ans, _ := json.Marshal(map[string]any{"selections": labels, "notes": notes, "via": via})
	now := time.Now().UTC()
	q.Status, q.Answer, q.AnsweredAt = "answered", ans, &now
	if err := st.Questions.Update(&q); err != nil {
		return err
	}
	text := fmt.Sprintf("Q-%02d: %s", q.Seq, strings.Join(labels, ", "))
	author := "system"
	switch via {
	case ViaTerminal:
		text = fmt.Sprintf("Q-%02d was answered in the Terminal.", q.Seq)
	case KindYou:
		author = "you"
	case KindPlanner:
		author = "planner"
	}
	if len(labels) == 0 && via != ViaTerminal {
		text = fmt.Sprintf("Q-%02d: answered with text", q.Seq)
	}
	if notes != "" && via != ViaTerminal {
		text += "\n" + notes
	}
	kind := "answer"
	if via == ViaTerminal {
		kind = "event"
	}
	msg := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: author, Kind: kind, Text: text, QuestionID: q.ID, Delivered: true}
	return st.Messages.Create(&msg)
}

// menuQuestion builds the question that stands for a menu.
func menuQuestion(sg sessions.MenuSighting) domain.Question {
	m := sg.Menu
	q := domain.Question{WorkspaceID: sg.WorkspaceID, CardID: sg.CardID, Kind: "decision", Multi: m.Multi, Options: []domain.QuestionOption{}}
	free := false
	for i, o := range m.Options {
		if o.Submit {
			continue
		}
		if o.FreeText {
			free = true
			continue
		}
		def := i == m.Cursor
		if m.Multi {
			def = o.Box && o.Checked
		}
		q.Options = append(q.Options, domain.QuestionOption{Label: o.Label, Description: o.Description, Default: def})
	}
	if m.Cancel && find(q.Options, sessions.CancelLabel) < 0 {
		q.Options = append(q.Options, domain.QuestionOption{Label: sessions.CancelLabel, Description: "Close the menu without choosing"})
	}
	if len(q.Options) > maxOptions {
		q.Options = q.Options[:maxOptions]
	}
	q.Text = m.Title
	if q.Text == "" {
		q.Text = m.Header
	}
	if q.Text == "" {
		q.Text = "Claude Code shows a menu"
	}
	var ctx []string
	if m.Header != "" && m.Header != q.Text {
		ctx = append(ctx, "**"+m.Header+"**")
	}
	if m.Body != "" {
		ctx = append(ctx, m.Body)
	}
	if free {
		ctx = append(ctx, "To answer in your own words, write it in Notes: it is typed into the menu.")
	}
	q.Context = strings.Join(ctx, "\n\n")
	q.Menu = &domain.QuestionMenu{Sig: m.Sig, Kind: m.Kind, FreeText: free}
	switch m.Kind {
	case "mcp", "trust", "permission", "plan":
		q.Kind = "approval"
		command := m.Body
		if command == "" {
			command = m.Title
		}
		raw, _ := json.Marshal(ApprovalIn{Target: sg.Cwd, RunsAs: "Claude Code in " + sg.Nick, Reversible: ReversiblePartial,
			ReversibleNote: "The choice is made once in the menu; what Claude does after it may not be undone.", Command: command, CommandLang: "text"})
		q.Approval = raw
	}
	return q
}

func find(opts []domain.QuestionOption, label string) int {
	for i, o := range opts {
		if strings.EqualFold(o.Label, label) {
			return i
		}
	}
	return -1
}

// SyncMenus scans every running card session for Claude Code menus once: a menu that has stayed up
// gets a question on its card (once: the same menu never makes a second one), and an open menu
// question whose menu is gone (answered in the Terminal, or replaced by another) closes. The app
// calls it about every second.
func (r *Registry) SyncMenus(ctx context.Context) {
	sv := r.deps.Sessions
	if sv == nil {
		return
	}
	sights := sv.ScanMenus()
	open, err := r.deps.Store.Questions.ListOpenMenus()
	if err != nil {
		return
	}
	byCard := map[string][]domain.Question{}
	for _, q := range open {
		byCard[q.CardID] = append(byCard[q.CardID], q)
	}
	seen := map[string]bool{}
	for _, sg := range sights {
		seen[sg.CardID] = true
		if !sg.Settled {
			continue
		}
		have := false
		var stale []domain.Question
		for _, q := range byCard[sg.CardID] {
			if sg.Menu != nil && q.Menu.Sig == sg.Menu.Sig {
				have = true
			} else {
				stale = append(stale, q)
			}
		}
		if len(stale) == 0 && (have || sg.Menu == nil) {
			continue
		}
		actor := Actor{Kind: KindSession, Name: sg.Nick, WorkspaceID: sg.WorkspaceID, CardID: sg.CardID}
		r.internal(ctx, actor, "menu_sync", func(c *Call) error {
			card, err := getCard(c, sg.CardID)
			if err != nil {
				return err
			}
			for _, q := range stale {
				cur, err := c.Store.Questions.Get(q.ID)
				if err != nil || cur.Status != "open" {
					continue
				}
				if err := closeMenuQuestion(c.Store, card, cur, nil, "", ViaTerminal); err != nil {
					return err
				}
			}
			if sg.Menu != nil && !have {
				q := menuQuestion(sg)
				if err := c.Store.Questions.Create(&q); err != nil {
					return err
				}
				msg := domain.Message{Author: "session", Kind: "report", ReportKind: "question", Text: q.Text, QuestionID: q.ID}
				ch := channelRef{name: chat.CardChannel(card.ID), workspaceID: card.WorkspaceID, card: &card}
				line := chat.ReportNotice(card, "question", "Claude Code's menu in the Terminal asks: "+q.Text+fmt.Sprintf(" (Q-%02d, question_get %s)", q.Seq, q.ID))
				if _, err := postAndRoute(c, ch, msg, line); err != nil {
					return err
				}
			}
			return reconcile(c, card)
		})
	}
	// A card whose session stopped cannot show its menu any more.
	for cardID, qs := range byCard {
		if seen[cardID] {
			continue
		}
		q0 := qs[0]
		actor := Actor{Kind: KindSession, WorkspaceID: q0.WorkspaceID, CardID: cardID}
		r.internal(ctx, actor, "menu_sync", func(c *Call) error {
			card, err := getCard(c, cardID)
			if err != nil {
				return err
			}
			for _, q := range qs {
				q.Status = "withdrawn"
				if err := c.Store.Questions.Update(&q); err != nil {
					return err
				}
				ev := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: "system", Kind: "event", QuestionID: q.ID, Delivered: true,
					Text: fmt.Sprintf("Q-%02d withdrawn: the session stopped, so its menu is gone.", q.Seq)}
				if err := c.Store.Messages.Create(&ev); err != nil {
					return err
				}
			}
			return reconcile(c, card)
		})
	}
}

// internal runs fn as one transaction for an actor, like an action that is not a tool: after it
// commits, its side effects run and the frontend hears about the change.
func (r *Registry) internal(ctx context.Context, actor Actor, what string, fn func(c *Call) error) {
	c := &Call{Actor: actor, Deps: r.deps, Store: r.deps.Store}
	err := r.deps.Store.Tx(func(tx *store.Store) error {
		c.Store = tx
		return fn(c)
	})
	if err != nil {
		if r.deps.Log != nil {
			r.deps.Log.Warn(what, "error", err.Error())
		}
		return
	}
	for _, f := range c.afterCommit {
		f()
	}
	c.SetWorkspace(actor.WorkspaceID)
	r.emit(c.workspaceID, attentionEntities...)
}

// answerMenuLater presses the keys for an answered menu question once the answer is committed. A
// failure goes to the card's chat and to the planner; the watcher then asks again, since the menu
// is still up.
func answerMenuLater(c *Call, card domain.Card, q domain.Question, labels []string, notes string) {
	sv, deps := c.Deps.Sessions, c.Deps
	if sv == nil {
		return
	}
	choice := sessions.MenuChoice{Sig: q.Menu.Sig, Labels: labels, Text: notes}
	for _, l := range labels {
		if l == sessions.CancelLabel {
			choice = sessions.MenuChoice{Sig: q.Menu.Sig, Cancel: true, Text: notes}
		}
	}
	c.AfterCommit(func() {
		go func() {
			_, err := sv.AnswerMenu(context.Background(), card.WorkspaceID, card.ID, choice)
			if err == nil {
				return
			}
			text := fmt.Sprintf("Q-%02d's answer could not be given to Claude Code's menu: %v", q.Seq, err)
			ev := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: "system", Kind: "event", Text: text, QuestionID: q.ID, Delivered: true}
			_ = deps.Store.Messages.Create(&ev)
			if deps.Emit != nil {
				deps.Emit(EventChanged, ChangedEvent{WorkspaceID: card.WorkspaceID, Entities: []string{EntityMessages}})
			}
			if deps.Chat != nil {
				deps.Chat.Enqueue(card.WorkspaceID, chat.ReportNotice(card, "blocked", text), nil)
			}
			if deps.Log != nil {
				deps.Log.Warn("menu answer", "card", card.Nick, "error", err.Error())
			}
		}()
	})
}
