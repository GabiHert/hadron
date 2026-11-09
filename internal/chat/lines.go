// Package chat routes messages between the user, the planner and card sessions (spec section 5).
// Everything the planner hears is one tagged line typed into its terminal, so the planner always
// knows which card a line is about and who wrote it.
package chat

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/GabiHert/hadron/internal/domain"
)

// MaxText is the longest message, in characters. Longer text is cut.
const MaxText = 8000

// MaxNotice is how much of a report the one-line notice to the planner carries.
const MaxNotice = 200

// Channels.
const (
	ChannelPlanner = "planner"
	cardPrefix     = "card:"
)

// CardChannel is the channel of a card.
func CardChannel(cardID string) string { return cardPrefix + cardID }

// ParseChannel splits a channel into its kind: ("planner", "") or ("card", "<ref>"). ref is a card
// id, nickname or key.
func ParseChannel(channel string) (kind, ref string, err error) {
	channel = strings.TrimSpace(channel)
	switch {
	case channel == ChannelPlanner:
		return "planner", "", nil
	case strings.HasPrefix(channel, cardPrefix) && len(channel) > len(cardPrefix):
		return "card", channel[len(cardPrefix):], nil
	}
	return "", "", fmt.Errorf("channel %q is not valid. Use %q, or %q followed by a card id, nickname or key", channel, ChannelPlanner, cardPrefix+"<card>")
}

// ErrEmpty means a message had no text left after sanitizing.
var ErrEmpty = errors.New("the message is empty. Pass the text to send")

// Sanitize strips control characters other than newline and caps the text at MaxText characters.
// It trims the ends, so a message never starts or ends blank. A line the planner reads starts with
// its tag, so a leading /, ! or # cannot become a command.
func Sanitize(text string) (string, error) {
	text = Clean(strings.ReplaceAll(text, "\r\n", "\n"))
	var b strings.Builder
	n := 0
	for _, r := range text {
		if r != '\n' && (unicode.IsControl(r) || r == ' ' || r == ' ') {
			continue
		}
		if n == MaxText {
			break
		}
		b.WriteRune(r)
		n++
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", ErrEmpty
	}
	return out, nil
}

// sep separates a line's subject from what kind of line it is: "[ACME-1 nick · report]". Only
// Hadron writes it, so Sanitize and Clean take it, and its look-alikes, out of everything a user,
// a session or a rule typed. A line the planner reads with the separator in its tag is Hadron's.
const sep = " · "

var lookalikes = strings.NewReplacer("·", "-", "•", "-", "∙", "-", "⋅", "-", "‧", "-", "・", "-", "●", "-")

// Clean removes the notice separator and its look-alikes from a name or a text that goes into a tag.
func Clean(s string) string { return lookalikes.Replace(s) }

// subject is how the planner names a card inside a notice: "KEY nick", or "nick" without a key.
func subject(card domain.Card) string {
	if card.Key != "" {
		return Clean(card.Key) + " " + Clean(card.Nick)
	}
	return Clean(card.Nick)
}

// Tag is how the planner names a card in a line the user wrote: [KEY nick], or [nick] when the card
// has no key.
func Tag(card domain.Card) string { return "[" + subject(card) + "]" }

// CardLine is the user's message on a card's channel, as the planner hears it.
func CardLine(card domain.Card, text string) string { return Tag(card) + " " + text }

// PlannerLine is the user's message to the planner itself.
func PlannerLine(text string) string { return "[planner] " + text }

// ReportNotice tells the planner a card reported: "[KEY nick · report] kind: first 200 characters".
func ReportNotice(card domain.Card, kind, text string) string {
	return fmt.Sprintf("[%s%sreport] %s: %s", subject(card), sep, Clean(kind), oneLine(Clean(text), MaxNotice))
}

// AnswerNotice tells the planner the user's answer to a question: "[KEY nick · answer] Q-NN:
// labels; notes: ...".
func AnswerNotice(card domain.Card, seq int, selections []string, notes string) string {
	return answerLine(card, seq, selections, notes, "")
}

// RelayedAnswerNotice is AnswerNotice for an answer the planner relayed for the user.
func RelayedAnswerNotice(card domain.Card, seq int, selections []string, notes string) string {
	return answerLine(card, seq, selections, notes, " (relayed)")
}

func answerLine(card domain.Card, seq int, selections []string, notes, suffix string) string {
	clean := make([]string, len(selections))
	for i, l := range selections {
		clean[i] = Clean(l)
	}
	line := fmt.Sprintf("[%s%sanswer%s] Q-%02d: %s", subject(card), sep, suffix, seq, strings.Join(clean, ", "))
	if notes = strings.TrimSpace(notes); notes != "" {
		line += "; notes: " + oneLine(Clean(notes), MaxText)
	}
	return line
}

// RuleNotice is what a rule tells the planner: "[rule · name] [KEY nick] text".
func RuleNotice(rule string, card domain.Card, text string) string {
	return fmt.Sprintf("[rule%s%s] %s %s", sep, Clean(rule), Tag(card), Clean(text))
}

// oneLine keeps the first n characters of text, with newlines folded to spaces so the notice stays
// one line.
func oneLine(text string, n int) string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	r := []rune(text)
	if len(r) > n {
		return string(r[:n])
	}
	return text
}
