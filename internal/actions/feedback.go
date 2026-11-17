package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// Feedback kinds and statuses.
var (
	FeedbackKinds    = []string{"bug", "idea", "annoyance", "praise"}
	FeedbackStatuses = []string{"new", "planned", "in_progress", "done", "wontfix"}
)

const maxFeedbackText = 8000

// FeedbackView is a note as the inbox shows it.
type FeedbackView struct {
	domain.Feedback
	// Shared says the note is on GitHub (github_url is set), so it is not sent twice.
	Shared bool `json:"shared" jsonschema:"the note has a GitHub issue (github_url), so it is not sent again"`
}

// FeedbackCreateIn is the input of feedback_create.
type FeedbackCreateIn struct {
	Kind              string `json:"kind" jsonschema:"bug, idea, annoyance or praise"`
	Text              string `json:"text" jsonschema:"what to tell the maker of Hadron, in the user's words"`
	Area              string `json:"area,omitempty" jsonschema:"where it was sent from: a view, card or setting, such as board, card:billing-fix or settings:rules"`
	AttachDiagnostics bool   `json:"attach_diagnostics,omitempty" jsonschema:"attach the version, macOS version, settings without secrets and the last 300 log lines (kept on this Mac; never sent to GitHub)"`
}

// FeedbackListIn is the input of feedback_list.
type FeedbackListIn struct {
	Status string `json:"status,omitempty" jsonschema:"only notes with this status: new, planned, in_progress, done or wontfix"`
}

// FeedbackUpdateIn is the input of feedback_update.
type FeedbackUpdateIn struct {
	ID        string  `json:"id" jsonschema:"the note id from feedback_list"`
	Status    *string `json:"status,omitempty" jsonschema:"new, planned, in_progress, done or wontfix"`
	Note      *string `json:"note,omitempty" jsonschema:"your own note on it, such as what was done or why not"`
	GithubURL *string `json:"github_url,omitempty" jsonschema:"the GitHub issue or pull request that tracks it"`
}

func feedbackView(f domain.Feedback) FeedbackView {
	return FeedbackView{Feedback: f, Shared: f.GithubURL != ""}
}

func registerFeedback(r *Registry) {
	Register(r, Spec{Name: "feedback_create", Description: "Put a note about Hadron itself in the feedback inbox (Settings, Feedback): a bug, an idea, something that annoys, or praise. Use it when the user says \"feedback: ...\". The version is added; attach_diagnostics adds the log tail and settings without secrets. The note stays on this Mac until the user sends it to GitHub (feedback_send).",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackCreateIn) (FeedbackView, error) {
			if !contains(FeedbackKinds, in.Kind) {
				return FeedbackView{}, fmt.Errorf("kind %q must be one of %s", in.Kind, strings.Join(FeedbackKinds, ", "))
			}
			text := strings.TrimSpace(in.Text)
			if text == "" {
				return FeedbackView{}, errors.New("text is empty. Say what the feedback is")
			}
			if len([]rune(text)) > maxFeedbackText {
				return FeedbackView{}, fmt.Errorf("the text is %d characters; keep it under %d", len([]rune(text)), maxFeedbackText)
			}
			f := domain.Feedback{Kind: in.Kind, Text: text, Area: strings.TrimSpace(in.Area), Version: c.Deps.Version, Diagnostics: json.RawMessage("null")}
			if in.AttachDiagnostics {
				d := collectDiagnostics(c, r)
				b, _ := json.Marshal(d)
				f.Diagnostics = b
			}
			if err := c.Store.Feedback.Create(&f); err != nil {
				return FeedbackView{}, err
			}
			return feedbackView(f), nil
		})

	Register(r, Spec{Name: "feedback_list", Description: "List the feedback inbox, newest first, optionally only one status. A Claude session can read it and work through the items; mark each with feedback_update."},
		func(ctx context.Context, c *Call, in FeedbackListIn) ([]FeedbackView, error) {
			if in.Status != "" && !contains(FeedbackStatuses, in.Status) {
				return nil, fmt.Errorf("status %q must be one of %s", in.Status, strings.Join(FeedbackStatuses, ", "))
			}
			rows, err := c.Store.Feedback.List(in.Status)
			if err != nil {
				return nil, err
			}
			out := make([]FeedbackView, len(rows))
			for i, f := range rows {
				out[i] = feedbackView(f)
			}
			return out, nil
		})

	Register(r, Spec{Name: "feedback_update", Description: "Change a feedback note's status (new, planned, in_progress, done, wontfix), add a note of your own, or record the GitHub issue that tracks it.",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackUpdateIn) (FeedbackView, error) {
			f, err := c.Store.Feedback.Get(in.ID)
			if errors.Is(err, store.ErrNotFound) {
				return FeedbackView{}, fmt.Errorf("no feedback %q. feedback_list shows the ids", in.ID)
			}
			if err != nil {
				return FeedbackView{}, err
			}
			if in.Status != nil {
				if !contains(FeedbackStatuses, *in.Status) {
					return FeedbackView{}, fmt.Errorf("status %q must be one of %s", *in.Status, strings.Join(FeedbackStatuses, ", "))
				}
				f.Status = *in.Status
			}
			if in.Note != nil {
				f.Note = strings.TrimSpace(*in.Note)
			}
			if in.GithubURL != nil {
				f.GithubURL = strings.TrimSpace(*in.GithubURL)
			}
			if err := c.Store.Feedback.Update(&f); err != nil {
				return FeedbackView{}, err
			}
			return feedbackView(f), nil
		})

	registerFeedbackSharing(r)
}
