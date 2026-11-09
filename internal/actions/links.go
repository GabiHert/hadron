package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/script"
)

// LinkListIn is the input of link_list.
type LinkListIn struct {
	Card string `json:"card" jsonschema:"the card: id, nickname or key"`
}

// LinkSearchIn is the input of link_search.
type LinkSearchIn struct {
	Integration string `json:"integration" jsonschema:"the integration name, such as jira"`
	Q           string `json:"q" jsonschema:"what to look for: a key, or words from the title"`
}

// LinkSearchOut is the result of link_search.
type LinkSearchOut struct {
	Items []integrations.Item `json:"items"`
}

// LinkAddIn is the input of link_add.
type LinkAddIn struct {
	Card        string `json:"card" jsonschema:"the card: id, nickname or key"`
	Integration string `json:"integration" jsonschema:"the integration name, such as jira"`
	ItemID      string `json:"item_id" jsonschema:"the item's id in the integration; link_search finds it"`
	Title       string `json:"title,omitempty" jsonschema:"title to show when the integration cannot fetch the item"`
	URL         string `json:"url,omitempty" jsonschema:"link to the item"`
}

// LinkRemoveIn is the input of link_remove.
type LinkRemoveIn struct {
	LinkID string `json:"link_id" jsonschema:"the link id; link_list shows it"`
}

// LinkRefreshIn is the input of link_refresh.
type LinkRefreshIn struct {
	LinkID string `json:"link_id,omitempty" jsonschema:"refresh this link"`
	Card   string `json:"card,omitempty" jsonschema:"or refresh every link of this card"`
}

// LinkRefreshOut is the result of link_refresh.
type LinkRefreshOut struct {
	Links   []domain.Link `json:"links"`
	Changed []string      `json:"changed" jsonschema:"the field paths whose value changed"`
}

// LinkActionRunIn is the input of link_action_run.
type LinkActionRunIn struct {
	LinkID string `json:"link_id" jsonschema:"the link the action is about"`
	Action string `json:"action" jsonschema:"one of the integration's actions, such as comment"`
	Input  any    `json:"input,omitempty" jsonschema:"extra input for the action; item_id is filled in"`
}

// RemoteSessionListIn is the input of remote_session_list.
type RemoteSessionListIn struct {
	Integration string `json:"integration,omitempty" jsonschema:"only this integration; empty for every one that runs sessions"`
	Card        string `json:"card,omitempty" jsonschema:"only sessions about this card's linked items"`
}

// RemoteSession is a cloud agent session an integration reports.
type RemoteSession struct {
	Integration string `json:"integration"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	ItemID      string `json:"item_id"`
}

// RemoteSessionRefIn names one remote session.
type RemoteSessionRefIn struct {
	Integration string `json:"integration" jsonschema:"the integration that runs the session"`
	ID          string `json:"id" jsonschema:"the remote session id from remote_session_list"`
}

// RemoteSessionSendIn is the input of remote_session_send.
type RemoteSessionSendIn struct {
	Integration string `json:"integration" jsonschema:"the integration that runs the session"`
	ID          string `json:"id" jsonschema:"the remote session id"`
	Text        string `json:"text" jsonschema:"what to tell the session"`
}

// RemoteSessionStartIn is the input of remote_session_start.
type RemoteSessionStartIn struct {
	Integration string `json:"integration" jsonschema:"the integration that runs sessions"`
	Card        string `json:"card,omitempty" jsonschema:"start it for this card's linked item"`
	ItemID      string `json:"item_id,omitempty" jsonschema:"or for this item id"`
	Prompt      string `json:"prompt,omitempty" jsonschema:"what the session should do"`
}

// RemoteSessionOutputOut is the result of remote_session_output.
type RemoteSessionOutputOut struct {
	Text string `json:"text"`
}

func registerLinks(r *Registry) {
	Register(r, Spec{Name: "link_list", Description: "List the items linked to a card, each with the snapshot of its fields (jira.status and so on) and when it was last refreshed."},
		func(ctx context.Context, c *Call, in LinkListIn) ([]domain.Link, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return nil, err
			}
			return c.Store.Links.ListByCard(card.ID)
		})

	Register(r, Spec{Name: "link_search", Description: "Search an enabled integration for items to link (issues, pull requests). Runs the integration's search op."},
		func(ctx context.Context, c *Call, in LinkSearchIn) (LinkSearchOut, error) {
			m, err := integ(c)
			if err != nil {
				return LinkSearchOut{}, err
			}
			w, sc, err := enabledFor(c, "", in.Integration)
			if err != nil {
				return LinkSearchOut{}, err
			}
			run, err := m.Op(ctx, c.Store, w, sc, "search", map[string]any{"q": in.Q})
			if err != nil {
				return LinkSearchOut{}, err
			}
			var out LinkSearchOut
			_ = json.Unmarshal(run.Result.Output, &out)
			if out.Items == nil {
				out.Items = []integrations.Item{}
			}
			return out, nil
		})

	Register(r, Spec{Name: "link_add", Description: "Link an item of an integration to a card. The integration's get op fetches its fields, which then appear as <integration>.<field> in filters, rules and on the card. Rules that watch those fields run.",
		Mutates: true, Entities: []string{EntityLinks, "cards", "fields"}},
		func(ctx context.Context, c *Call, in LinkAddIn) (domain.Link, error) {
			if err := userOrPlanner(c, "link items"); err != nil {
				return domain.Link{}, err
			}
			m, err := integ(c)
			if err != nil {
				return domain.Link{}, err
			}
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return domain.Link{}, err
			}
			w, sc, err := enabledFor(c, card.WorkspaceID, in.Integration)
			if err != nil {
				return domain.Link{}, err
			}
			links, _ := c.Store.Links.ListByCard(card.ID)
			for _, l := range links {
				if l.Integration == sc.Name && l.ItemID == in.ItemID {
					return l, fmt.Errorf("card %q already links %s %s", card.Nick, sc.Name, in.ItemID)
				}
			}
			man, _ := script.ParseManifest(sc.Manifest)
			title, url, fl := in.Title, in.URL, map[string]any{}
			if man.HasOp("get") {
				it, err := m.Get(ctx, c.Store, w, sc, in.ItemID)
				if err != nil {
					return domain.Link{}, fmt.Errorf("%s could not fetch %s: %w", sc.Name, in.ItemID, err)
				}
				title, url, fl = it.Title, it.URL, it.Fields
			}
			link := domain.Link{WorkspaceID: card.WorkspaceID, CardID: card.ID, Integration: sc.Name, ItemID: in.ItemID, Title: title, URL: url, Fields: fl}
			if link.Title == "" {
				link.Title = in.ItemID
			}
			if err := c.Store.Links.Create(&link); err != nil {
				return link, err
			}
			c.SetWorkspace(card.WorkspaceID)
			var events []board.Event
			for _, k := range sortedKeys(fl) {
				events = append(events, board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: sc.Name + "." + k, Old: "", New: fl[k]})
			}
			return link, fireEvents(c, events)
		})

	Register(r, Spec{Name: "link_remove", Description: "Remove a link from its card. The item itself is untouched.",
		Mutates: true, Entities: []string{EntityLinks, "cards", "fields"}},
		func(ctx context.Context, c *Call, in LinkRemoveIn) (domain.Link, error) {
			if err := userOrPlanner(c, "remove links"); err != nil {
				return domain.Link{}, err
			}
			l, err := c.Store.Links.Get(in.LinkID)
			if err != nil {
				return l, fmt.Errorf("no link %q. link_list shows the ids", in.LinkID)
			}
			c.SetWorkspace(l.WorkspaceID)
			return l, c.Store.Links.Delete(l.ID)
		})

	Register(r, Spec{Name: "link_refresh", Description: "Re-read linked items from their integration now, instead of waiting for the next poll. Pass a link_id, or a card to refresh all of its links. Rules that watch a field that changed run.",
		Mutates: true, Entities: []string{EntityLinks, "cards", "fields"}},
		func(ctx context.Context, c *Call, in LinkRefreshIn) (LinkRefreshOut, error) {
			m, err := integ(c)
			if err != nil {
				return LinkRefreshOut{}, err
			}
			var links []domain.Link
			switch {
			case in.LinkID != "":
				l, err := c.Store.Links.Get(in.LinkID)
				if err != nil {
					return LinkRefreshOut{}, fmt.Errorf("no link %q. link_list shows the ids", in.LinkID)
				}
				links = []domain.Link{l}
			case in.Card != "":
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return LinkRefreshOut{}, err
				}
				if links, err = c.Store.Links.ListByCard(card.ID); err != nil {
					return LinkRefreshOut{}, err
				}
			default:
				return LinkRefreshOut{}, errors.New("pass link_id or card")
			}
			out := LinkRefreshOut{Links: []domain.Link{}, Changed: []string{}}
			var events []board.Event
			for _, l := range links {
				c.SetWorkspace(l.WorkspaceID)
				w, sc, err := enabledFor(c, l.WorkspaceID, l.Integration)
				if err != nil {
					return out, err
				}
				it, err := m.Get(ctx, c.Store, w, sc, l.ItemID)
				if err != nil {
					return out, fmt.Errorf("%s could not refresh %s: %w", sc.Name, l.ItemID, err)
				}
				changes, err := integrations.Apply(c.Store, &l, it.Title, it.URL, it.Fields)
				if err != nil {
					return out, err
				}
				for _, ch := range changes {
					out.Changed = append(out.Changed, ch.Path)
					events = append(events, board.Event{Trigger: board.OnFieldChanged, CardID: ch.CardID, Field: ch.Path, Old: ch.Old, New: ch.New})
				}
				out.Links = append(out.Links, l)
			}
			return out, fireEvents(c, events)
		})

	Register(r, Spec{Name: "link_action_run", Description: "Run one of an integration's actions on a linked item, such as commenting on a Jira issue. The list of actions is in integration_list.",
		Mutates: true, Entities: []string{EntityLinks, "cards", "fields"}},
		func(ctx context.Context, c *Call, in LinkActionRunIn) (RunOpOut, error) {
			if err := userOrPlanner(c, "run integration actions"); err != nil {
				return RunOpOut{}, err
			}
			m, err := integ(c)
			if err != nil {
				return RunOpOut{}, err
			}
			l, err := c.Store.Links.Get(in.LinkID)
			if err != nil {
				return RunOpOut{}, fmt.Errorf("no link %q. link_list shows the ids", in.LinkID)
			}
			c.SetWorkspace(l.WorkspaceID)
			w, sc, err := enabledFor(c, l.WorkspaceID, l.Integration)
			if err != nil {
				return RunOpOut{}, err
			}
			input := map[string]any{}
			if given, ok := in.Input.(map[string]any); ok {
				for k, v := range given {
					input[k] = v
				}
			} else if in.Input != nil {
				return RunOpOut{}, errors.New("input must be an object")
			}
			input["item_id"] = l.ItemID
			run, err := m.Op(ctx, c.Store, w, sc, "actions."+in.Action, input)
			if err != nil {
				return RunOpOut{}, err
			}
			if err := fireEvents(c, run.Events); err != nil {
				return RunOpOut{}, err
			}
			var out any
			_ = json.Unmarshal(run.Result.Output, &out)
			logs := run.Result.Logs
			if logs == nil {
				logs = []string{}
			}
			return RunOpOut{Output: out, Logs: logs, DurationMS: int(run.Result.Duration.Milliseconds())}, nil
		})

	registerRemoteSessions(r)
}

func sortedKeys(m map[string]any) []string { return script.SortedKeys(m) }

// enabledFor finds an enabled integration of the workspace.
func enabledFor(c *Call, workspaceID, name string) (domain.Workspace, domain.Script, error) {
	w, sc, _, err := loadScript(c, workspaceID, name, integrations.KindIntegration)
	if err != nil {
		return w, sc, err
	}
	if sc.Status != integrations.StatusEnabled {
		return w, sc, fmt.Errorf("%s is %s. Approve it in Settings, Integrations first", sc.Name, sc.Status)
	}
	return w, sc, nil
}

func registerRemoteSessions(r *Registry) {
	run := func(ctx context.Context, c *Call, integration, op string, input any) (script.Result, error) {
		m, err := integ(c)
		if err != nil {
			return script.Result{}, err
		}
		w, sc, err := enabledFor(c, "", integration)
		if err != nil {
			return script.Result{}, err
		}
		man, _ := script.ParseManifest(sc.Manifest)
		if !man.HasOp(op) {
			return script.Result{}, fmt.Errorf("%s does not run remote sessions: its manifest has no ops.%s", sc.Name, op)
		}
		out, err := m.Op(ctx, c.Store, w, sc, op, input)
		return out.Result, err
	}

	Register(r, Spec{Name: "remote_session_list", Description: "List cloud agent sessions that integrations run (those whose manifest declares ops.sessions), with their status. Pass a card to see only the sessions about its linked items."},
		func(ctx context.Context, c *Call, in RemoteSessionListIn) ([]RemoteSession, error) {
			w, err := c.Workspace("")
			if err != nil {
				return nil, err
			}
			names := []string{in.Integration}
			if in.Integration == "" {
				names = nil
				list, _ := c.Store.Scripts.List(w.ID)
				for _, sc := range list {
					if sc.Kind == integrations.KindIntegration && sc.Status == integrations.StatusEnabled {
						if man, _ := script.ParseManifest(sc.Manifest); man.HasOp("sessions.list") {
							names = append(names, sc.Name)
						}
					}
				}
				sort.Strings(names)
			}
			var items map[string]bool
			if in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return nil, err
				}
				links, _ := c.Store.Links.ListByCard(card.ID)
				items = map[string]bool{}
				for _, l := range links {
					items[l.Integration+"/"+l.ItemID] = true
				}
			}
			out := []RemoteSession{}
			for _, name := range names {
				res, err := run(ctx, c, name, "sessions.list", map[string]any{})
				if err != nil {
					if in.Integration == "" {
						continue // one integration being down must not hide the others
					}
					return nil, err
				}
				var got struct {
					Sessions []RemoteSession `json:"sessions"`
				}
				_ = json.Unmarshal(res.Output, &got)
				for _, s := range got.Sessions {
					s.Integration = name
					if items != nil && !items[name+"/"+s.ItemID] {
						continue
					}
					out = append(out, s)
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "remote_session_output", Description: "Read the latest output of a remote session."},
		func(ctx context.Context, c *Call, in RemoteSessionRefIn) (RemoteSessionOutputOut, error) {
			res, err := run(ctx, c, in.Integration, "sessions.output", map[string]any{"id": in.ID})
			if err != nil {
				return RemoteSessionOutputOut{}, err
			}
			var out RemoteSessionOutputOut
			_ = json.Unmarshal(res.Output, &out)
			return out, nil
		})

	Register(r, Spec{Name: "remote_session_send", Description: "Send a message to a running remote session.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in RemoteSessionSendIn) (struct{}, error) {
			if err := userOrPlanner(c, "message remote sessions"); err != nil {
				return struct{}{}, err
			}
			_, err := run(ctx, c, in.Integration, "sessions.send", map[string]any{"id": in.ID, "text": in.Text})
			return struct{}{}, err
		})

	Register(r, Spec{Name: "remote_session_start", Description: "Start a remote session, for a card's linked item or a given item id.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in RemoteSessionStartIn) (RemoteSession, error) {
			if err := userOrPlanner(c, "start remote sessions"); err != nil {
				return RemoteSession{}, err
			}
			item := in.ItemID
			if item == "" && in.Card != "" {
				card, err := cardByRef(c, in.Card)
				if err != nil {
					return RemoteSession{}, err
				}
				links, _ := c.Store.Links.ListByCard(card.ID)
				for _, l := range links {
					if l.Integration == in.Integration {
						item = l.ItemID
						break
					}
				}
				if item == "" {
					return RemoteSession{}, fmt.Errorf("card %q has no %s link to start a session for", card.Nick, in.Integration)
				}
			}
			input := map[string]any{}
			if item != "" {
				input["item_id"] = item
			}
			if strings.TrimSpace(in.Prompt) != "" {
				input["prompt"] = in.Prompt
			}
			res, err := run(ctx, c, in.Integration, "sessions.start", input)
			if err != nil {
				return RemoteSession{}, err
			}
			var out RemoteSession
			_ = json.Unmarshal(res.Output, &out)
			out.Integration, out.ItemID = in.Integration, item
			return out, nil
		})

	Register(r, Spec{Name: "remote_session_stop", Description: "Stop a remote session.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in RemoteSessionRefIn) (struct{}, error) {
			if err := userOrPlanner(c, "stop remote sessions"); err != nil {
				return struct{}{}, err
			}
			_, err := run(ctx, c, in.Integration, "sessions.stop", map[string]any{"id": in.ID})
			return struct{}{}, err
		})
}
