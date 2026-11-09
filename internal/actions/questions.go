package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// Entities of the question and gate actions' changed events.
var attentionEntities = []string{"questions", "gates", EntityMessages, "cards"}

// Planner policies (spec section 5): what the planner may answer without the user.
const (
	PolicyAllowAll      = "allow_all"
	PolicyDecisionsOnly = "decisions_only"
	PolicyAskForPRD     = "ask_for_prd"
)

// PlannerPolicies lists every policy.
var PlannerPolicies = []string{PolicyAllowAll, PolicyDecisionsOnly, PolicyAskForPRD}

// maxOptions bounds how many choices a question offers.
const maxOptions = 12

// ApprovalIn describes what an approval question asks the user to allow.
type ApprovalIn struct {
	Target     string `json:"target" jsonschema:"what it touches, such as DEV, PRD or a repo"`
	RunsAs     string `json:"runs_as" jsonschema:"the identity it runs as, such as a role or user"`
	Reversible bool   `json:"reversible" jsonschema:"whether it can be undone"`
	Command    string `json:"command" jsonschema:"the exact command or operation"`
}

// QuestionAskIn is the input of question_ask.
type QuestionAskIn struct {
	Card     string                  `json:"card,omitempty" jsonschema:"the card; a session asks about its own card and may leave this out"`
	Kind     string                  `json:"kind" jsonschema:"decision, unblock or approval"`
	Text     string                  `json:"text" jsonschema:"the question"`
	Context  string                  `json:"context,omitempty" jsonschema:"what the reader needs to know to answer"`
	Options  []domain.QuestionOption `json:"options,omitempty" jsonschema:"the choices, each with a label and a description; put your recommendation first"`
	Multi    bool                    `json:"multi,omitempty" jsonschema:"allow picking more than one option"`
	Approval *ApprovalIn             `json:"approval,omitempty" jsonschema:"required for kind approval: what is being approved"`
}

// QuestionListIn is the input of question_list.
type QuestionListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Card        string `json:"card,omitempty" jsonschema:"only this card's questions (id, nickname or key)"`
	Status      string `json:"status,omitempty" jsonschema:"open, answered or withdrawn"`
}

// QuestionIDIn is the input of the actions that need only a question.
type QuestionIDIn struct {
	QuestionID string `json:"question_id" jsonschema:"the question id"`
}

// QuestionAnswerIn is the input of question_answer.
type QuestionAnswerIn struct {
	QuestionID string   `json:"question_id" jsonschema:"the question id"`
	Selections []string `json:"selections,omitempty" jsonschema:"the labels of the chosen options"`
	Notes      string   `json:"notes,omitempty" jsonschema:"free text that goes with the answer, or the whole answer when no option fits"`
}

// QuestionWithdrawIn is the input of question_withdraw.
type QuestionWithdrawIn struct {
	QuestionID string `json:"question_id" jsonschema:"the question id"`
	Reason     string `json:"reason,omitempty" jsonschema:"why it no longer needs an answer"`
}

// GateSetIn is the input of gate_set.
type GateSetIn struct {
	Card    string `json:"card,omitempty" jsonschema:"the card; a session sets a gate on its own card and may leave this out"`
	Kind    string `json:"kind" jsonschema:"merge, promote, deploy-confirm or close"`
	Summary string `json:"summary" jsonschema:"what waits on the user, in one line"`
}

// GateClearIn is the input of gate_clear.
type GateClearIn struct {
	GateID string `json:"gate_id,omitempty" jsonschema:"the gate id; or pass card and kind"`
	Card   string `json:"card,omitempty" jsonschema:"the card (id, nickname or key), with kind"`
	Kind   string `json:"kind,omitempty" jsonschema:"merge, promote, deploy-confirm or close, with card"`
}

// GateListIn is the input of gate_list.
type GateListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Card        string `json:"card,omitempty" jsonschema:"only this card's gates (id, nickname or key)"`
}

// PlannerPolicyOut is the result of the planner policy actions.
type PlannerPolicyOut struct {
	Policy  string   `json:"policy"`
	Options []string `json:"options"`
}

// PlannerPolicySetIn is the input of planner_policy_set.
type PlannerPolicySetIn struct {
	Policy string `json:"policy" jsonschema:"allow_all, decisions_only or ask_for_prd"`
}

var gateKinds = []string{"merge", "promote", "deploy-confirm", "close"}
var questionKinds = []string{"decision", "unblock", "approval"}
var prdTarget = regexp.MustCompile(`(?i)\b(prd|prod|production)\b`)

func registerQuestions(r *Registry) {
	Register(r, Spec{Name: "question_ask", Description: "Ask the user a question about your card: a decision (with options and your recommendation), an unblock, or an approval (with what it touches, who it runs as, whether it can be undone, and the command). The card moves to Needs you until it is answered.",
		Mutates: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in QuestionAskIn) (domain.Question, error) {
			card, err := ownCardOrYou(c, in.Card, "question_ask")
			if err != nil {
				return domain.Question{}, err
			}
			q, err := buildQuestion(card, in)
			if err != nil {
				return q, err
			}
			if err := c.Store.Questions.Create(&q); err != nil {
				return q, err
			}
			author := "session"
			if c.Actor.Kind == KindYou {
				author = "you"
			}
			msg := domain.Message{Author: author, Kind: "report", ReportKind: "question", Text: q.Text, QuestionID: q.ID}
			ch := channelRef{name: chat.CardChannel(card.ID), workspaceID: card.WorkspaceID, card: &card}
			if _, err := postAndRoute(c, ch, msg, chat.ReportNotice(card, "question", q.Text)); err != nil {
				return q, err
			}
			return q, reconcile(c, card)
		})

	Register(r, Spec{Name: "question_list", Description: "List questions, newest first, optionally for one card or one status (open, answered, withdrawn)."},
		func(ctx context.Context, c *Call, in QuestionListIn) ([]domain.Question, error) {
			if in.Status != "" && !contains([]string{"open", "answered", "withdrawn"}, in.Status) {
				return nil, fmt.Errorf("status %q must be open, answered or withdrawn", in.Status)
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			cardID := ""
			if in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return nil, err
				}
				cardID = card.ID
			}
			return c.Store.Questions.List(w.ID, cardID, in.Status)
		})

	Register(r, Spec{Name: "question_get", Description: "Get one question with its options, approval details and answer. A session polls this to see whether its question has been answered."},
		func(ctx context.Context, c *Call, in QuestionIDIn) (domain.Question, error) {
			return getQuestion(c, in.QuestionID)
		})

	Register(r, Spec{Name: "question_answer", Description: "Answer an open question with the labels of the chosen options and optional notes. The user may always answer. The planner may answer under the planner policy. A session can never answer a question, least of all its own.",
		Mutates: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in QuestionAnswerIn) (domain.Question, error) {
			q, err := getQuestion(c, in.QuestionID)
			if err != nil {
				return q, err
			}
			card, err := getCard(c, q.CardID)
			if err != nil {
				return q, err
			}
			if err := mayAnswer(c, q); err != nil {
				return q, err
			}
			if q.Status != "open" {
				return q, fmt.Errorf("Q-%02d is already %s", q.Seq, q.Status)
			}
			labels, notes, err := checkAnswer(q, in)
			if err != nil {
				return q, err
			}
			via := c.Actor.Kind
			answer, _ := json.Marshal(map[string]any{"selections": labels, "notes": notes, "via": via})
			now := time.Now().UTC()
			q.Status, q.Answer, q.AnsweredAt = "answered", answer, &now
			if err := c.Store.Questions.Update(&q); err != nil {
				return q, err
			}
			text := fmt.Sprintf("Q-%02d: %s", q.Seq, strings.Join(labels, ", "))
			if len(labels) == 0 {
				text = fmt.Sprintf("Q-%02d: answered in notes", q.Seq)
			}
			if notes != "" {
				text += "\n" + notes
			}
			msg := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: "you", Kind: "answer", Text: text, QuestionID: q.ID, Delivered: true}
			if via == KindYou {
				ch := channelRef{name: msg.Channel, workspaceID: card.WorkspaceID, card: &card}
				if _, err := postAndRoute(c, ch, msg, chat.AnswerNotice(card, q.Seq, labels, notes)); err != nil {
					return q, err
				}
			} else {
				// The planner (or another client) answered, and the planner already knows, so nothing is typed back.
				msg.Author = "system"
				if via == KindPlanner {
					msg.Author = "planner"
				}
				if err := c.Store.Messages.Create(&msg); err != nil {
					return q, err
				}
			}
			return q, reconcile(c, card)
		})

	Register(r, Spec{Name: "question_withdraw", Description: "Withdraw an open question that no longer needs an answer. The session that asked it, the planner and the user may withdraw it.",
		Mutates: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in QuestionWithdrawIn) (domain.Question, error) {
			q, err := getQuestion(c, in.QuestionID)
			if err != nil {
				return q, err
			}
			card, err := getCard(c, q.CardID)
			if err != nil {
				return q, err
			}
			switch c.Actor.Kind {
			case KindYou:
			case KindPlanner:
				if err := sameWorkspace(c, q.WorkspaceID); err != nil {
					return q, err
				}
			case KindSession:
				if c.Actor.CardID != q.CardID {
					return q, errors.New("a session may withdraw only its own card's questions")
				}
			default:
				return q, fmt.Errorf("%s may not withdraw questions", c.Actor)
			}
			if q.Status != "open" {
				return q, fmt.Errorf("Q-%02d is already %s", q.Seq, q.Status)
			}
			q.Status = "withdrawn"
			if err := c.Store.Questions.Update(&q); err != nil {
				return q, err
			}
			text := fmt.Sprintf("Q-%02d withdrawn", q.Seq)
			if r := strings.TrimSpace(in.Reason); r != "" {
				text += ": " + r
			}
			ev := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: "system", Kind: "event", Text: text, QuestionID: q.ID, Delivered: true}
			if err := c.Store.Messages.Create(&ev); err != nil {
				return q, err
			}
			return q, reconcile(c, card)
		})

	Register(r, Spec{Name: "gate_set", Description: "Mark that a merge, promote, deploy-confirm or close on your card waits for the user. The card moves to Needs you until the gate is cleared. Setting the same kind again updates its summary.",
		Mutates: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in GateSetIn) (domain.Gate, error) {
			card, err := gateCard(c, in.Card)
			if err != nil {
				return domain.Gate{}, err
			}
			if !contains(gateKinds, in.Kind) {
				return domain.Gate{}, fmt.Errorf("kind %q must be one of %s", in.Kind, strings.Join(gateKinds, ", "))
			}
			summary, err := chat.Sanitize(in.Summary)
			if err != nil {
				return domain.Gate{}, errors.New("summary is empty. Say in one line what waits on the user")
			}
			gates, err := c.Store.Gates.List(card.WorkspaceID, card.ID)
			if err != nil {
				return domain.Gate{}, err
			}
			g := domain.Gate{WorkspaceID: card.WorkspaceID, CardID: card.ID, Kind: in.Kind, Summary: summary}
			found := false
			for _, old := range gates {
				if old.Kind == in.Kind {
					g, found = old, true
					g.Summary = summary
					err = c.Store.Gates.Update(&g)
				}
			}
			if !found {
				err = c.Store.Gates.Create(&g)
			}
			if err != nil {
				return g, err
			}
			ev := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: authorOf(c.Actor), Kind: "event", Text: fmt.Sprintf("Waiting on you to %s: %s", in.Kind, summary), Delivered: true}
			if err := c.Store.Messages.Create(&ev); err != nil {
				return g, err
			}
			return g, reconcile(c, card)
		})

	Register(r, Spec{Name: "gate_list", Description: "List the gates waiting on the user, oldest first, optionally for one card."},
		func(ctx context.Context, c *Call, in GateListIn) ([]domain.Gate, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			cardID := ""
			if in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return nil, err
				}
				cardID = card.ID
			}
			return c.Store.Gates.List(w.ID, cardID)
		})

	Register(r, Spec{Name: "gate_clear", Description: "Clear a gate once the user has done or declined it. The user may always clear it; the planner only under the allow_all policy. A session can never clear its own gate.",
		Mutates: true, Entities: attentionEntities},
		func(ctx context.Context, c *Call, in GateClearIn) (domain.Gate, error) {
			g, err := findGate(c, in)
			if err != nil {
				return g, err
			}
			card, err := getCard(c, g.CardID)
			if err != nil {
				return g, err
			}
			switch c.Actor.Kind {
			case KindYou:
			case KindPlanner, KindExternal:
				if err := sameWorkspace(c, g.WorkspaceID); err != nil {
					return g, err
				}
				if p := plannerPolicy(c.Store); p != PolicyAllowAll {
					return g, fmt.Errorf("the planner policy is %s, so only the user clears a %s gate. Bring it to the user in the card's chat", p, g.Kind)
				}
			default:
				return g, errors.New("a session cannot clear a gate: it waits on the user")
			}
			if err := c.Store.Gates.Delete(g.ID); err != nil {
				return g, err
			}
			ev := domain.Message{WorkspaceID: card.WorkspaceID, Channel: chat.CardChannel(card.ID), Author: authorOf(c.Actor), Kind: "event", Text: fmt.Sprintf("Cleared the %s gate", g.Kind), Delivered: true}
			if err := c.Store.Messages.Create(&ev); err != nil {
				return g, err
			}
			return g, reconcile(c, card)
		})

	Register(r, Spec{Name: "planner_policy_get", Description: "Report the planner policy: allow_all (the planner may answer everything), decisions_only (it may answer decisions and unblocks, not approvals) or ask_for_prd (it may answer approvals that target DEV, not PRD)."},
		func(ctx context.Context, c *Call, in struct{}) (PlannerPolicyOut, error) {
			return PlannerPolicyOut{Policy: plannerPolicy(c.Store), Options: PlannerPolicies}, nil
		})

	Register(r, Spec{Name: "planner_policy_set", Description: "Set the planner policy. Only the user can: the planner cannot widen its own permissions.",
		Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in PlannerPolicySetIn) (PlannerPolicyOut, error) {
			if c.Actor.Kind != KindYou {
				return PlannerPolicyOut{}, errors.New("only the user sets the planner policy")
			}
			if !contains(PlannerPolicies, in.Policy) {
				return PlannerPolicyOut{}, fmt.Errorf("policy %q must be one of %s", in.Policy, strings.Join(PlannerPolicies, ", "))
			}
			if err := c.Store.Settings.Set(sessions.SettingPlannerPolicy, json.RawMessage(toJSON(in.Policy))); err != nil {
				return PlannerPolicyOut{}, err
			}
			return PlannerPolicyOut{Policy: in.Policy, Options: PlannerPolicies}, nil
		})
}

// plannerPolicy reads planner.policy; allow_all is the default.
func plannerPolicy(st *store.Store) string {
	raw, err := st.Settings.Get(sessions.SettingPlannerPolicy)
	if err != nil {
		return PolicyAllowAll
	}
	var p string
	if json.Unmarshal(raw, &p) != nil || !contains(PlannerPolicies, p) {
		return PolicyAllowAll
	}
	return p
}

// mayAnswer applies who-may-answer: the user always; a session never; the planner (and any other
// client that is not the user) as far as the planner policy allows.
func mayAnswer(c *Call, q domain.Question) error {
	switch c.Actor.Kind {
	case KindYou:
		return nil
	case KindSession:
		if c.Actor.CardID == q.CardID {
			return errors.New("a session cannot answer its own question: it waits for the user, or for the planner when the policy allows")
		}
		return errors.New("a session cannot answer questions. Only the user, or the planner under the planner policy, can")
	}
	if err := sameWorkspace(c, q.WorkspaceID); err != nil {
		return err
	}
	policy := plannerPolicy(c.Store)
	if q.Kind != "approval" {
		return nil
	}
	switch policy {
	case PolicyDecisionsOnly:
		return errors.New("the planner policy is decisions_only, so the planner may not answer approvals. Bring this approval to the user in the card's chat")
	case PolicyAskForPRD:
		var ap struct {
			Target string `json:"target"`
		}
		_ = json.Unmarshal(q.Approval, &ap)
		if ap.Target == "" || prdTarget.MatchString(ap.Target) {
			return fmt.Errorf("the planner policy is ask_for_prd, so the planner may not answer an approval that targets %q. Bring it to the user in the card's chat", ap.Target)
		}
	}
	return nil
}

// checkAnswer validates an answer against the question's options and returns the labels and notes.
func checkAnswer(q domain.Question, in QuestionAnswerIn) ([]string, string, error) {
	notes := ""
	if strings.TrimSpace(in.Notes) != "" {
		var err error
		if notes, err = chat.Sanitize(in.Notes); err != nil {
			return nil, "", err
		}
	}
	labels := []string{}
	seen := map[string]bool{}
	for _, s := range in.Selections {
		var match *domain.QuestionOption
		for i := range q.Options {
			if strings.EqualFold(strings.TrimSpace(s), q.Options[i].Label) {
				match = &q.Options[i]
			}
		}
		if match == nil {
			return nil, "", fmt.Errorf("%q is not an option of Q-%02d. The options are: %s", s, q.Seq, optionLabels(q))
		}
		if !seen[match.Label] {
			seen[match.Label] = true
			labels = append(labels, match.Label)
		}
	}
	if len(labels) > 1 && !q.Multi {
		return nil, "", fmt.Errorf("Q-%02d takes one option, and you picked %d", q.Seq, len(labels))
	}
	if len(labels) == 0 && notes == "" {
		return nil, "", errors.New("pick an option, or write notes: an answer cannot be empty")
	}
	return labels, notes, nil
}

func optionLabels(q domain.Question) string {
	var l []string
	for _, o := range q.Options {
		l = append(l, o.Label)
	}
	if len(l) == 0 {
		return "none (answer in notes)"
	}
	return strings.Join(l, ", ")
}

// buildQuestion validates a question_ask input.
func buildQuestion(card domain.Card, in QuestionAskIn) (domain.Question, error) {
	q := domain.Question{WorkspaceID: card.WorkspaceID, CardID: card.ID, Kind: in.Kind, Multi: in.Multi, Options: []domain.QuestionOption{}}
	if !contains(questionKinds, in.Kind) {
		return q, fmt.Errorf("kind %q must be one of %s", in.Kind, strings.Join(questionKinds, ", "))
	}
	text, err := chat.Sanitize(in.Text)
	if err != nil {
		return q, errors.New("text is empty. Write the question")
	}
	q.Text = text
	if strings.TrimSpace(in.Context) != "" {
		if q.Context, err = chat.Sanitize(in.Context); err != nil {
			return q, err
		}
	}
	if len(in.Options) > maxOptions {
		return q, fmt.Errorf("%d options is too many; offer at most %d", len(in.Options), maxOptions)
	}
	seen := map[string]bool{}
	for i, o := range in.Options {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return q, fmt.Errorf("option %d has no label", i+1)
		}
		if seen[strings.ToLower(label)] {
			return q, fmt.Errorf("option %q appears twice", label)
		}
		seen[strings.ToLower(label)] = true
		q.Options = append(q.Options, domain.QuestionOption{Label: label, Description: strings.TrimSpace(o.Description)})
	}
	if in.Kind == "approval" {
		ap := in.Approval
		if ap == nil || strings.TrimSpace(ap.Target) == "" || strings.TrimSpace(ap.Command) == "" {
			return q, errors.New("an approval needs approval.target and approval.command, and should say who it runs_as and whether it is reversible")
		}
		raw, _ := json.Marshal(ApprovalIn{Target: strings.TrimSpace(ap.Target), RunsAs: strings.TrimSpace(ap.RunsAs), Reversible: ap.Reversible, Command: strings.TrimSpace(ap.Command)})
		q.Approval = raw
		if len(q.Options) == 0 {
			q.Options = []domain.QuestionOption{{Label: "Approve"}, {Label: "Reject"}}
		}
	} else if in.Approval != nil {
		return q, errors.New("approval details belong to kind approval only")
	}
	if q.Multi && len(q.Options) < 2 {
		return q, errors.New("multi needs at least two options")
	}
	return q, nil
}

func getQuestion(c *Call, id string) (domain.Question, error) {
	q, err := c.Store.Questions.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return q, fmt.Errorf("question %q does not exist. Call question_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(q.WorkspaceID)
	}
	return q, err
}

// ownCardOrYou finds the card a session acts on: its own. The user names one.
func ownCardOrYou(c *Call, ref, action string) (domain.Card, error) {
	switch c.Actor.Kind {
	case KindSession:
		own, err := getCard(c, c.Actor.CardID)
		if err != nil {
			return own, err
		}
		if ref != "" && ref != own.ID && ref != own.Nick && ref != own.Key {
			return own, fmt.Errorf("a session may use %s only on its own card (%s), not on %q", action, own.Nick, ref)
		}
		return own, nil
	case KindYou:
		if ref == "" {
			return domain.Card{}, errors.New("pass card: the id, nickname or key of the card")
		}
		return cardByRef(c, ref)
	}
	return domain.Card{}, fmt.Errorf("%s is for a card's own session, and %s is not one", action, c.Actor)
}

// gateCard is ownCardOrYou, and the planner may also set a gate on any card of its workspace.
func gateCard(c *Call, ref string) (domain.Card, error) {
	if c.Actor.Kind != KindPlanner {
		return ownCardOrYou(c, ref, "gate_set")
	}
	if ref == "" {
		return domain.Card{}, errors.New("pass card: the id, nickname or key of the card")
	}
	card, err := cardByRef(c, ref)
	if err != nil {
		return card, err
	}
	return card, sameWorkspace(c, card.WorkspaceID)
}

func findGate(c *Call, in GateClearIn) (domain.Gate, error) {
	if in.GateID != "" {
		g, err := c.Store.Gates.Get(in.GateID)
		if errors.Is(err, store.ErrNotFound) {
			return g, fmt.Errorf("gate %q does not exist. Call gate_list for the ids", in.GateID)
		}
		if err == nil {
			c.SetWorkspace(g.WorkspaceID)
		}
		return g, err
	}
	if in.Card == "" || in.Kind == "" {
		return domain.Gate{}, errors.New("pass gate_id, or card and kind")
	}
	card, err := cardByRef(c, in.Card)
	if err != nil {
		return domain.Gate{}, err
	}
	gates, err := c.Store.Gates.List(card.WorkspaceID, card.ID)
	if err != nil {
		return domain.Gate{}, err
	}
	for _, g := range gates {
		if g.Kind == in.Kind {
			return g, nil
		}
	}
	return domain.Gate{}, fmt.Errorf("card %q has no %s gate. gate_list shows the open ones", card.Nick, in.Kind)
}

func authorOf(a Actor) string {
	switch a.Kind {
	case KindYou:
		return "you"
	case KindPlanner:
		return "planner"
	case KindSession:
		return "session"
	}
	return "system"
}

// reconcile runs the built-in rules for the card, then sets whether its session waits on the user.
func reconcile(c *Call, card domain.Card) error {
	attention, err := board.Attention(c.Store, card)
	if err != nil {
		return err
	}
	// The built-in rules look at the card's state, not at which call got us here, so repeating
	// the event is harmless.
	trigger := board.OnQuestionAnswered
	if attention > 0 {
		trigger = board.OnQuestionOpened
	}
	if err := runEvent(c, board.Event{Trigger: trigger, CardID: card.ID}); err != nil {
		return err
	}
	open, err := c.Store.Questions.List(card.WorkspaceID, card.ID, "open")
	if err != nil {
		return err
	}
	waiting := len(open) > 0
	if sv := c.Deps.Sessions; sv != nil {
		ws, id := card.WorkspaceID, card.ID
		c.AfterCommit(func() { sv.SetWaiting(ws, id, waiting) })
	}
	return nil
}
