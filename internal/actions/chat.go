package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// Entities of the chat actions' changed events.
const EntityMessages = "messages"

// maxWidgetHTML is the largest widget a message can carry.
const maxWidgetHTML = 200 * 1024

// ChatListIn is the input of chat_list.
type ChatListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ChatReadIn is the input of chat_read.
type ChatReadIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Channel     string `json:"channel" jsonschema:"planner, or card:<card id, nickname or key>"`
	Since       string `json:"since,omitempty" jsonschema:"only messages after this RFC 3339 time"`
	Limit       int    `json:"limit,omitempty" jsonschema:"keep only the newest N messages (default: all)"`
}

// ChatSendIn is the input of chat_send and chat_post.
type ChatSendIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Channel     string `json:"channel" jsonschema:"planner, or card:<card id, nickname or key>"`
	Text        string `json:"text" jsonschema:"the message; control characters are stripped and it is cut at 8,000 characters"`
}

// WidgetPostIn is the input of widget_post.
type WidgetPostIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Channel     string `json:"channel" jsonschema:"planner, or card:<card id, nickname or key>"`
	Title       string `json:"title" jsonschema:"a short title for the widget"`
	HTML        string `json:"html" jsonschema:"the widget as HTML; it renders in a sandboxed frame with no network"`
}

// CardReportIn is the input of card_report.
type CardReportIn struct {
	Card string `json:"card,omitempty" jsonschema:"the card (id, nickname or key); a session reports on its own card and may leave this out"`
	Kind string `json:"kind" jsonschema:"progress, question, blocked or done"`
	Text string `json:"text" jsonschema:"what to tell the planner and the user"`
}

var reportKinds = []string{"progress", "question", "blocked", "done"}

func registerChat(r *Registry) {
	Register(r, Spec{Name: "chat_list", Description: "List the channels that have messages: the planner's and each card's, with the message count and the newest message."},
		func(ctx context.Context, c *Call, in ChatListIn) ([]store.ChannelSummary, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.Messages.Channels(w.ID)
		})

	Register(r, Spec{Name: "chat_read", Description: "Read a channel's messages, oldest first. Optionally only those after a time, or only the newest few."},
		func(ctx context.Context, c *Call, in ChatReadIn) ([]domain.Message, error) {
			ch, err := resolveChannel(c, in.WorkspaceID, in.Channel)
			if err != nil {
				return nil, err
			}
			since := ""
			if in.Since != "" {
				t, err := time.Parse(time.RFC3339Nano, in.Since)
				if err != nil {
					return nil, fmt.Errorf("since %q is not an RFC 3339 time, such as 2026-11-08T12:00:00Z", in.Since)
				}
				since = t.UTC().Format(time.RFC3339Nano)
			}
			if in.Limit < 0 {
				return nil, errors.New("limit must be 0 (all) or more")
			}
			msgs, err := c.Store.Messages.List(ch.workspaceID, ch.name, since)
			if err != nil {
				return nil, err
			}
			if in.Limit > 0 && len(msgs) > in.Limit {
				msgs = msgs[len(msgs)-in.Limit:]
			}
			return msgs, nil
		})

	Register(r, Spec{Name: "chat_send", Description: "Send a message as the user. On a card's channel it is typed into the planner as \"[KEY nick] text\"; on the planner channel as \"[planner] text\". User-only: see mcp.user_actions. The planner answers with chat_post.",
		Mutates: true, Entities: []string{EntityMessages}},
		func(ctx context.Context, c *Call, in ChatSendIn) (domain.Message, error) {
			if err := c.UserOnly("chat_send"); err != nil {
				return domain.Message{}, err
			}
			ch, err := resolveChannel(c, in.WorkspaceID, in.Channel)
			if err != nil {
				return domain.Message{}, err
			}
			text, err := chat.Sanitize(in.Text)
			if err != nil {
				return domain.Message{}, err
			}
			line := chat.PlannerLine(text)
			if ch.card != nil {
				line = chat.CardLine(*ch.card, text)
			}
			return postAndRoute(c, ch, domain.Message{Author: "you", Kind: "text", Text: text}, line)
		})

	Register(r, Spec{Name: "chat_command", Description: "Send a Claude Code slash command (such as /compact or /model sonnet) from a chat straight to the terminal of that channel's session: a card's channel types it into the card's session, the planner channel into the planner, then presses Enter, as session_send and planner_send do. It is never relayed to the planner as a chat line. The command is stored on the channel as a kind: command message. The text must start with one /; it fails, and stores nothing, when the session is not running. User-only: see mcp.user_actions.",
		Mutates: true, Entities: []string{EntityMessages, "sessions"}},
		func(ctx context.Context, c *Call, in ChatSendIn) (domain.Message, error) {
			if err := c.UserOnly("chat_command"); err != nil {
				return domain.Message{}, err
			}
			ch, err := resolveChannel(c, in.WorkspaceID, in.Channel)
			if err != nil {
				return domain.Message{}, err
			}
			text, err := chat.Sanitize(in.Text)
			if err != nil {
				return domain.Message{}, err
			}
			if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") || len(strings.TrimSpace(text)) < 2 {
				return domain.Message{}, fmt.Errorf("a command starts with one / and a name, such as /compact; send other text with chat_send")
			}
			if err := mayControl(c, ch.workspaceID, ch.card, true, "chat_command"); err != nil {
				return domain.Message{}, err
			}
			m := domain.Message{WorkspaceID: ch.workspaceID, Channel: ch.name, Author: "you", Kind: domain.MessageCommand, Text: text, Delivered: true}
			if err := c.Store.Messages.Create(&m); err != nil {
				return m, err
			}
			if _, err := sendTo(c, ch.workspaceID, ch.card, text, nil); err != nil {
				return domain.Message{}, err
			}
			return m, nil
		})

	Register(r, Spec{Name: "chat_post", Description: "Post the planner's reply on a channel. Only the planner (or the user, in tests) may call it. The post is stored; it is not typed anywhere.",
		Mutates: true, Entities: []string{EntityMessages}},
		func(ctx context.Context, c *Call, in ChatSendIn) (domain.Message, error) {
			if c.Actor.Kind != KindPlanner && c.Actor.Kind != KindYou {
				return domain.Message{}, fmt.Errorf("chat_post is the planner's reply, and %s is not the planner. A session reports with card_report", c.Actor)
			}
			ch, err := resolveChannel(c, in.WorkspaceID, in.Channel)
			if err != nil {
				return domain.Message{}, err
			}
			if err := sameWorkspace(c, ch.workspaceID); err != nil {
				return domain.Message{}, err
			}
			text, err := chat.Sanitize(in.Text)
			if err != nil {
				return domain.Message{}, err
			}
			m := domain.Message{WorkspaceID: ch.workspaceID, Channel: ch.name, Author: "planner", Kind: "text", Text: text, Delivered: true}
			return m, c.Store.Messages.Create(&m)
		})

	Register(r, Spec{Name: "widget_post", Description: "Post a small HTML widget on a channel to explain something visually: a comparison table, a before/after, an SVG diagram, a timeline, a chart, a plan's steps, a status board. The planner, the user, or a session on its own card may post. It is stored now and rendered in a sandboxed frame later.",
		Mutates: true, Entities: []string{EntityMessages}},
		func(ctx context.Context, c *Call, in WidgetPostIn) (domain.Message, error) {
			ch, err := resolveChannel(c, in.WorkspaceID, in.Channel)
			if err != nil {
				return domain.Message{}, err
			}
			author := ""
			switch c.Actor.Kind {
			case KindYou:
				author = "you"
			case KindPlanner:
				author = "planner"
				if err := sameWorkspace(c, ch.workspaceID); err != nil {
					return domain.Message{}, err
				}
			case KindSession:
				author = "session"
				if ch.card == nil || ch.card.ID != c.Actor.CardID {
					return domain.Message{}, errors.New("a session may post a widget only on its own card's channel")
				}
			default:
				return domain.Message{}, fmt.Errorf("widget_post is for the planner, the user and card sessions, and %s is none of them", c.Actor)
			}
			title := strings.TrimSpace(in.Title)
			if title == "" || strings.TrimSpace(in.HTML) == "" {
				return domain.Message{}, errors.New("a widget needs a title and html")
			}
			if len(in.HTML) > maxWidgetHTML {
				return domain.Message{}, fmt.Errorf("the widget is %d KB; keep it under %d KB", len(in.HTML)/1024, maxWidgetHTML/1024)
			}
			m := domain.Message{WorkspaceID: ch.workspaceID, Channel: ch.name, Author: author, Kind: "widget", WidgetTitle: title, WidgetHTML: in.HTML, Delivered: true}
			return m, c.Store.Messages.Create(&m)
		})

	Register(r, Spec{Name: "card_report", Description: "Report progress, a question, a blocker or completion on a card. It is stored on the card's channel and typed into the planner as \"[KEY nick] report <kind>: <first 200 characters>\". A session reports on its own card; so may the user.",
		Mutates: true, Entities: []string{EntityMessages}},
		func(ctx context.Context, c *Call, in CardReportIn) (domain.Message, error) {
			card, err := reportCard(c, in.Card)
			if err != nil {
				return domain.Message{}, err
			}
			if !contains(reportKinds, in.Kind) {
				return domain.Message{}, fmt.Errorf("kind %q must be one of %s", in.Kind, strings.Join(reportKinds, ", "))
			}
			text, err := chat.Sanitize(in.Text)
			if err != nil {
				return domain.Message{}, err
			}
			ch := channelRef{name: chat.CardChannel(card.ID), workspaceID: card.WorkspaceID, card: &card}
			author := "session"
			if c.Actor.Kind == KindYou {
				author = "you"
			}
			msg, err := postAndRoute(c, ch, domain.Message{Author: author, Kind: "report", ReportKind: in.Kind, Text: text}, chat.ReportNotice(card, in.Kind, text))
			if err != nil {
				return msg, err
			}
			return msg, runEvent(c, board.Event{Trigger: board.OnReport, CardID: card.ID, ReportKind: in.Kind})
		})
}

// channelRef is a resolved channel.
type channelRef struct {
	name        string // planner, or card:<id>
	workspaceID string
	card        *domain.Card // nil for the planner channel
}

// resolveChannel turns "planner" or "card:<ref>" into a workspace and a normalized channel name.
func resolveChannel(c *Call, workspaceID, channel string) (channelRef, error) {
	kind, ref, err := chat.ParseChannel(channel)
	if err != nil {
		return channelRef{}, err
	}
	if kind == "planner" {
		w, err := c.Workspace(workspaceID)
		if err != nil {
			return channelRef{}, err
		}
		return channelRef{name: chat.ChannelPlanner, workspaceID: w.ID}, nil
	}
	card, err := cardByRef(c, ref)
	if err != nil {
		return channelRef{}, err
	}
	return channelRef{name: chat.CardChannel(card.ID), workspaceID: card.WorkspaceID, card: &card}, nil
}

// sameWorkspace stops a planner from writing into another workspace.
func sameWorkspace(c *Call, workspaceID string) error {
	if c.Actor.WorkspaceID != "" && c.Actor.WorkspaceID != workspaceID {
		return errors.New("that channel belongs to another workspace than the caller's")
	}
	return nil
}

// reportCard finds the card a report is about. A session may only report on its own card.
func reportCard(c *Call, ref string) (domain.Card, error) {
	switch c.Actor.Kind {
	case KindSession:
		own, err := getCard(c, c.Actor.CardID)
		if err != nil {
			return own, err
		}
		if ref != "" && ref != own.ID && ref != own.Nick && ref != own.Key {
			return own, fmt.Errorf("a session may report only on its own card (%s), not on %q", own.Nick, ref)
		}
		return own, nil
	case KindYou:
		if ref == "" {
			return domain.Card{}, errors.New("pass card: the id, nickname or key of the card the report is about")
		}
		return cardByRef(c, ref)
	}
	return domain.Card{}, fmt.Errorf("card_report is for a card's own session, and %s is not one", c.Actor)
}

// postAndRoute stores m on the channel and types line into the workspace's planner once the call
// commits. Whether the planner could be reached, and why not, is recorded on the message.
func postAndRoute(c *Call, ch channelRef, m domain.Message, line string) (domain.Message, error) {
	m.WorkspaceID, m.Channel = ch.workspaceID, ch.name
	ok, reason := c.Deps.Chat.Available(c.Store, ch.workspaceID)
	m.Delivered, m.Reason = ok, reason
	if err := c.Store.Messages.Create(&m); err != nil {
		return m, err
	}
	if ok {
		id, ws, deps := m.ID, ch.workspaceID, c.Deps
		c.AfterCommit(func() {
			deps.Chat.Enqueue(ws, line, func(why string) { markUndelivered(deps, ws, id, why) })
		})
	}
	return m, nil
}

// markUndelivered records, after the fact, that a line never reached the planner.
func markUndelivered(deps Deps, workspaceID, messageID, reason string) {
	m, err := deps.Store.Messages.Get(messageID)
	if err != nil {
		return
	}
	m.Delivered, m.Reason = false, reason
	if deps.Store.Messages.Update(&m) == nil && deps.Emit != nil {
		deps.Emit(EventChanged, ChangedEvent{WorkspaceID: workspaceID, Entities: []string{EntityMessages}})
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
