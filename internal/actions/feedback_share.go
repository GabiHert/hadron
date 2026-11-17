package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/feedback"
	"github.com/GabiHert/hadron/internal/store"
)

// SettingPrivateWords is the "words to keep private" list. It lives only in this Mac's database
// and is left out of diagnostics.
const SettingPrivateWords = "feedback.private_words"

var errNoSharing = errors.New("sharing feedback is not available in this build")

// FeedbackSendIn is the input of feedback_send.
type FeedbackSendIn struct {
	ID          string  `json:"id" jsonschema:"the note id from feedback_list"`
	Preview     bool    `json:"preview,omitempty" jsonschema:"only build the draft (title, body, labels, what was removed, private words found) and send nothing"`
	Title       *string `json:"title,omitempty" jsonschema:"the reviewed title; the draft's when left out"`
	Body        *string `json:"body,omitempty" jsonschema:"the reviewed body; the draft's when left out"`
	KeepDetails bool    `json:"keep_details,omitempty" jsonschema:"keep folder paths, the user name and the host name in the draft instead of replacing them"`
}

// FeedbackSendOut is what feedback_send returns.
type FeedbackSendOut struct {
	Draft feedback.Draft `json:"draft"`
	// PrivateHits are the private words in the title or body; sending is refused while there are any.
	PrivateHits []string `json:"private_hits"`
	// Via is how it is or would be sent: token (the issue is created) or browser (a filled-in
	// new-issue page opens and the user presses Submit there).
	Via string `json:"via"`
	// URL is the new issue (token) or the new-issue page that was opened (browser). Empty in a preview.
	URL  string       `json:"url,omitempty"`
	Note FeedbackView `json:"note"`
}

// FeedbackListSharedIn is the input of feedback_list_shared.
type FeedbackListSharedIn struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"fetch from GitHub now instead of using the list fetched in the last 3 hours"`
}

// SharedIssue is a shared note with its local copy, if any.
type SharedIssue struct {
	feedback.Issue
	LocalID string `json:"local_id,omitempty" jsonschema:"the local note for this issue: yours, or one you imported"`
}

// FeedbackListSharedOut is what feedback_list_shared returns.
type FeedbackListSharedOut struct {
	Issues    []SharedIssue `json:"issues"`
	FetchedAt string        `json:"fetched_at"`
	Error     string        `json:"error,omitempty" jsonschema:"why the last fetch failed; the list is then the cached one"`
}

// FeedbackImportIn is the input of feedback_import_shared.
type FeedbackImportIn struct {
	Number int `json:"number" jsonschema:"the issue number from feedback_list_shared"`
}

// FeedbackSharingOut is the sharing setup.
type FeedbackSharingOut struct {
	Repo         string   `json:"repo"`
	HasToken     bool     `json:"has_token" jsonschema:"a sending token is saved, so notes are sent through the API; otherwise they open in the browser"`
	PrivateWords []string `json:"private_words" jsonschema:"words a note may not contain to be sent"`
}

// FeedbackSharingSetIn is the input of feedback_sharing_set.
type FeedbackSharingSetIn struct {
	PrivateWords []string `json:"private_words" jsonschema:"the whole list of words to keep private (names of clients, projects, people); it replaces the saved list and stays on this Mac"`
}

// SetFeedbackTokenIn is the input of set_feedback_token.
type SetFeedbackTokenIn struct {
	Token string `json:"token,omitempty" jsonschema:"a fine-grained GitHub token with Issues: write on GabiHert/hadron; empty removes the saved token"`
}

func privateWords(c *Call) []string {
	raw, err := c.Store.Settings.Get(SettingPrivateWords)
	if err != nil || len(raw) == 0 {
		return []string{}
	}
	var w []string
	if json.Unmarshal(raw, &w) != nil {
		return []string{}
	}
	return feedback.CleanWords(w)
}

func sharingOut(ctx context.Context, c *Call) FeedbackSharingOut {
	return FeedbackSharingOut{Repo: feedback.Repo, HasToken: c.Deps.Feedback.HasToken(ctx), PrivateWords: privateWords(c)}
}

func getNote(c *Call, id string) (domain.Feedback, error) {
	f, err := c.Store.Feedback.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return f, fmt.Errorf("no feedback %q. feedback_list shows the ids", id)
	}
	return f, err
}

func noteMacOS(f domain.Feedback) string {
	if isNull(f.Diagnostics) {
		return ""
	}
	var d DiagnosticsOut
	if json.Unmarshal(f.Diagnostics, &d) != nil {
		return ""
	}
	return d.MacOS
}

func registerFeedbackSharing(r *Registry) {
	Register(r, Spec{Name: "feedback_send", Description: "Share a feedback note as a public GitHub issue on GabiHert/hadron, labelled feedback and its kind. Call it with preview first and show the user the title and body: folder paths, the user name and the host name are replaced unless keep_details. Send only when the user says so, with the title and body they approved. It is refused while the text has a word from the private list, and for a note already sent. With a saved token the issue is created and its link recorded on the note; without one a filled-in new-issue page opens in the browser, and the link is recorded once the shared list sees the issue.",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackSendIn) (FeedbackSendOut, error) {
			if err := userOrPlanner(c, "send feedback to GitHub"); err != nil {
				return FeedbackSendOut{}, err
			}
			svc := c.Deps.Feedback
			if svc == nil {
				return FeedbackSendOut{}, errNoSharing
			}
			f, err := getNote(c, in.ID)
			if err != nil {
				return FeedbackSendOut{}, err
			}
			draft := feedback.MakeDraft(feedback.Note{ID: f.ID, Kind: f.Kind, Text: f.Text, Area: f.Area, Version: f.Version, MacOS: noteMacOS(f)}, svc.Details(), in.KeepDetails)
			if in.Title != nil {
				draft.Title = strings.TrimSpace(*in.Title)
			}
			if in.Body != nil {
				draft.Body = strings.TrimSpace(*in.Body)
			}
			out := FeedbackSendOut{Draft: draft, PrivateHits: feedback.PrivateHits(privateWords(c), draft.Title, draft.Body), Via: "browser", Note: feedbackView(f)}
			if svc.HasToken(ctx) {
				out.Via = "token"
			}
			if in.Preview {
				return out, nil
			}
			if f.GithubURL != "" {
				return out, fmt.Errorf("this note is already on GitHub: %s", f.GithubURL)
			}
			if draft.Title == "" || draft.Body == "" {
				return out, errors.New("the title and the body cannot be empty")
			}
			if len(out.PrivateHits) > 0 {
				return out, fmt.Errorf("not sent: the text contains words you keep private (%s). Take them out and try again", strings.Join(out.PrivateHits, ", "))
			}
			sent, err := svc.Send(ctx, draft.Title, feedback.WithMarker(draft.Body, f.ID), draft.Labels)
			if err != nil {
				return out, err
			}
			out.Via, out.URL = sent.Via, sent.URL
			if sent.Via == "token" {
				f.GithubURL = sent.URL
				if err := c.Store.Feedback.Update(&f); err != nil {
					return out, err
				}
				out.Note = feedbackView(f)
			}
			c.Deps.Log.Info("feedback sent", "note", f.ID, "via", sent.Via)
			return out, nil
		})

	Register(r, Spec{Name: "feedback_list_shared", Description: "List the feedback every Hadron user shared: the open GitHub issues on GabiHert/hadron labelled feedback, read through the public API with no token. The list is cached for 3 hours; refresh fetches now. Each issue has its title, kind, version, link and, when you have it as a local note, local_id. feedback_import_shared copies one into the inbox.",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackListSharedIn) (FeedbackListSharedOut, error) {
			svc := c.Deps.Feedback
			if svc == nil {
				return FeedbackListSharedOut{}, errNoSharing
			}
			sh := svc.List(ctx, in.Refresh)
			notes, err := c.Store.Feedback.List("")
			if err != nil {
				return FeedbackListSharedOut{}, err
			}
			byURL, byID := map[string]string{}, map[string]*domain.Feedback{}
			for i := range notes {
				if notes[i].GithubURL != "" {
					byURL[notes[i].GithubURL] = notes[i].ID
				}
				byID[notes[i].ID] = &notes[i]
			}
			out := FeedbackListSharedOut{Issues: []SharedIssue{}, Error: sh.Error}
			if !sh.FetchedAt.IsZero() {
				out.FetchedAt = sh.FetchedAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			for _, is := range sh.Issues {
				si := SharedIssue{Issue: is, LocalID: byURL[is.URL]}
				// A note sent through the browser gets its issue link once the issue shows up here.
				if n := byID[is.NoteID]; si.LocalID == "" && n != nil && n.GithubURL == "" {
					n.GithubURL = is.URL
					if err := c.Store.Feedback.Update(n); err == nil {
						si.LocalID = n.ID
					}
				}
				out.Issues = append(out.Issues, si)
			}
			return out, nil
		})

	Register(r, Spec{Name: "feedback_import_shared", Description: "Copy a shared feedback issue (by number, from feedback_list_shared) into the local inbox as a note with its link, so it can be triaged like your own. An issue already in the inbox is not copied again.",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackImportIn) (FeedbackView, error) {
			svc := c.Deps.Feedback
			if svc == nil {
				return FeedbackView{}, errNoSharing
			}
			var is *feedback.Issue
			for _, x := range svc.List(ctx, false).Issues {
				if x.Number == in.Number {
					x := x
					is = &x
				}
			}
			if is == nil {
				return FeedbackView{}, fmt.Errorf("no shared feedback #%d. feedback_list_shared {refresh: true} shows the open ones", in.Number)
			}
			notes, err := c.Store.Feedback.List("")
			if err != nil {
				return FeedbackView{}, err
			}
			for _, n := range notes {
				if n.GithubURL == is.URL {
					return FeedbackView{}, fmt.Errorf("#%d is already in your inbox", in.Number)
				}
			}
			kind := is.Kind
			if !contains(FeedbackKinds, kind) {
				kind = "idea"
			}
			text := strings.TrimSpace(is.Text)
			if text == "" {
				text = is.Title
			}
			if r := []rune(text); len(r) > maxFeedbackText {
				text = string(r[:maxFeedbackText-1]) + "…"
			}
			f := domain.Feedback{Kind: kind, Text: text, Area: is.Area, Version: is.Version, Diagnostics: json.RawMessage("null"), GithubURL: is.URL}
			if err := c.Store.Feedback.Create(&f); err != nil {
				return FeedbackView{}, err
			}
			return feedbackView(f), nil
		})

	Register(r, Spec{Name: "feedback_sharing_get", Description: "Read how feedback is shared: the repo, whether a sending token is saved, and the words to keep private."},
		func(ctx context.Context, c *Call, in struct{}) (FeedbackSharingOut, error) {
			if c.Deps.Feedback == nil {
				return FeedbackSharingOut{}, errNoSharing
			}
			return sharingOut(ctx, c), nil
		})

	Register(r, Spec{Name: "feedback_sharing_set", Description: "Replace the list of words to keep private. A note that contains one of them is not sent to GitHub. The list stays on this Mac.",
		Mutates: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in FeedbackSharingSetIn) (FeedbackSharingOut, error) {
			if err := userOrPlanner(c, "change the private words"); err != nil {
				return FeedbackSharingOut{}, err
			}
			if c.Deps.Feedback == nil {
				return FeedbackSharingOut{}, errNoSharing
			}
			b, _ := json.Marshal(feedback.CleanWords(in.PrivateWords))
			if err := c.Store.Settings.Set(SettingPrivateWords, b); err != nil {
				return FeedbackSharingOut{}, err
			}
			return sharingOut(ctx, c), nil
		})

	Register(r, Spec{Name: "set_feedback_token", Description: "Save the GitHub token feedback is sent with, in the login keychain: a fine-grained token with Issues: write on GabiHert/hadron. An empty token removes it, and notes then open in the browser instead.",
		Mutates: true, Sensitive: true, Entities: []string{"feedback"}},
		func(ctx context.Context, c *Call, in SetFeedbackTokenIn) (FeedbackSharingOut, error) {
			if err := userOrPlanner(c, "change the feedback token"); err != nil {
				return FeedbackSharingOut{}, err
			}
			if c.Deps.Feedback == nil {
				return FeedbackSharingOut{}, errNoSharing
			}
			if err := c.Deps.Feedback.SetToken(ctx, in.Token); err != nil {
				return sharingOut(ctx, c), err
			}
			return sharingOut(ctx, c), nil
		})
}
