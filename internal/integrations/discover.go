package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/script"
	"github.com/GabiHert/hadron/internal/store"
)

// Discovery limits.
const (
	// DefaultDiscoverMax is how many cards one discovery run makes when the workspace sets no cap.
	DefaultDiscoverMax = 10
	// MaxDiscoverMax is the highest cap a workspace may set.
	MaxDiscoverMax = 50
	// MaxCardCreatesPerRun is how many cards one op run may make with host.card.create.
	MaxCardCreatesPerRun = 25
)

// DiscoverSettings is one workspace's discovery switch for one integration. It lives in the
// setting DiscoverKey(workspace, integration), with the last run's outcome.
type DiscoverSettings struct {
	Enabled bool `json:"enabled"`
	// Column is where new cards go: a column id, name or role. Empty means the backlog column,
	// else the first active one.
	Column string `json:"column"`
	// MaxPerRun caps the cards one run makes; the rest wait for the next run.
	MaxPerRun int `json:"max_per_run"`

	LastRunAt   *time.Time `json:"last_run_at"`
	LastCreated int        `json:"last_created"`
	LastError   string     `json:"last_error"`
}

// DiscoverKey is the settings key of a workspace's discovery for an integration.
func DiscoverKey(workspaceID, name string) string { return "integration_discover:" + workspaceID + ":" + name }

// LoadDiscover reads a workspace's discovery settings; a missing one is off.
func LoadDiscover(st *store.Store, workspaceID, name string) DiscoverSettings {
	var d DiscoverSettings
	if raw, err := st.Settings.Get(DiscoverKey(workspaceID, name)); err == nil {
		_ = json.Unmarshal(raw, &d)
	}
	return d
}

// SaveDiscover writes them.
func SaveDiscover(st *store.Store, workspaceID, name string, d DiscoverSettings) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return st.Settings.Set(DiscoverKey(workspaceID, name), raw)
}

// CardSpec is a card an integration asks for.
type CardSpec struct {
	Nick, Key, Title, Column string
	ItemID, URL              string
	Fields                   map[string]any
}

// ResolveColumn finds a column by id, name (any case) or role. Empty picks the backlog column,
// else the first active one, else the first.
func ResolveColumn(cols []domain.Column, ref string) (domain.Column, error) {
	ref = strings.TrimSpace(ref)
	if len(cols) == 0 {
		return domain.Column{}, errors.New("the workspace has no columns")
	}
	if ref == "" {
		for _, role := range []string{domain.RoleBacklog, domain.RoleActive} {
			for _, c := range cols {
				if c.Role == role {
					return c, nil
				}
			}
		}
		return cols[0], nil
	}
	for _, c := range cols {
		if c.ID == ref || strings.EqualFold(c.Name, ref) {
			return c, nil
		}
	}
	for _, c := range cols {
		if c.Role == ref {
			return c, nil
		}
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return domain.Column{}, fmt.Errorf("no column %q: use a column id, name (%s) or role", ref, strings.Join(names, ", "))
}

var nonNick = regexp.MustCompile(`[^a-z0-9]+`)

// NickFor makes a kebab-case nickname from a key or a title: PROJ-123 → proj-123, "Fix the login
// page" → fix-the-login-page. It starts with a letter and stays under 40 characters.
func NickFor(key, title string) string {
	src := key
	if strings.TrimSpace(src) == "" {
		src = title
	}
	n := strings.Trim(nonNick.ReplaceAllString(strings.ToLower(src), "-"), "-")
	if len(n) > 40 {
		n = strings.TrimRight(n[:40], "-")
	}
	if n == "" || n[0] < 'a' || n[0] > 'z' {
		n = strings.TrimRight("item-"+n, "-")
	}
	return n
}

// freeNick returns base, or base-2, base-3 … when a card already uses it.
func freeNick(st *store.Store, workspaceID, base string) (string, error) {
	n := base
	for i := 2; i < 1000; i++ {
		_, err := st.Cards.ByNick(workspaceID, n)
		if errors.Is(err, store.ErrNotFound) {
			return n, nil
		}
		if err != nil {
			return "", err
		}
		n = fmt.Sprintf("%s-%d", base, i)
	}
	return "", fmt.Errorf("no free nickname like %q", base)
}

// linkedCard returns the card that already links the item, if one does (archived cards count).
func linkedCard(st *store.Store, workspaceID, integration, itemID string) (domain.Card, bool, error) {
	links, err := st.Links.ListByIntegration(integration)
	if err != nil {
		return domain.Card{}, false, err
	}
	for _, l := range links {
		if l.WorkspaceID == workspaceID && l.ItemID == itemID {
			c, err := st.Cards.Get(l.CardID)
			if err == nil {
				return c, true, nil
			}
		}
	}
	return domain.Card{}, false, nil
}

// CreateCard makes a session-less card for an integration and links it to the item. An item
// some card already links returns that card with created false. The card_created event goes to events.
func (m *Manager) CreateCard(st *store.Store, ws domain.Workspace, integration string, spec CardSpec, events *[]board.Event) (card domain.Card, created bool, err error) {
	spec.ItemID = strings.TrimSpace(spec.ItemID)
	if spec.ItemID != "" {
		if c, ok, err := linkedCard(st, ws.ID, integration, spec.ItemID); err != nil || ok {
			return c, false, err
		}
	}
	cols, err := st.Columns.List(ws.ID)
	if err != nil {
		return card, false, err
	}
	col, err := ResolveColumn(cols, spec.Column)
	if err != nil {
		return card, false, err
	}
	nick := strings.TrimSpace(spec.Nick)
	if nick != "" {
		if err := board.CheckNick(nick); err != nil {
			return card, false, err
		}
	} else {
		nick = NickFor(spec.Key, spec.Title)
	}
	if nick, err = freeNick(st, ws.ID, nick); err != nil {
		return card, false, err
	}
	card = domain.Card{WorkspaceID: ws.ID, ColumnID: col.ID, Nick: nick, Key: strings.TrimSpace(spec.Key), Title: strings.TrimSpace(spec.Title), Assignee: "You", Liveness: domain.LivenessIdle}
	if err := st.Cards.Create(&card); err != nil {
		return card, false, err
	}
	if spec.ItemID != "" {
		title := spec.Title
		if title == "" {
			title = spec.Key
		}
		if _, err := m.upsertLink(st, card, integration, spec.ItemID, title, spec.URL, spec.Fields); err != nil {
			return card, false, err
		}
		if err := st.Discovered.Add(ws.ID, integration, spec.ItemID, card.ID); err != nil {
			return card, false, err
		}
	}
	if events != nil {
		*events = append(*events, board.Event{Trigger: board.OnCardCreated, CardID: card.ID})
	}
	return card, true, nil
}

// DiscoverResult is one discovery run in one workspace.
type DiscoverResult struct {
	Created []domain.Card `json:"created"`
	// Known are the items a card already held, or that were discovered before.
	Known int `json:"known"`
	// Left are new items over the cap, for the next run.
	Left int `json:"left"`
}

// DiscoveredItem is one item ops.discover returns.
type DiscoveredItem struct {
	ItemID string         `json:"item_id"`
	Title  string         `json:"title"`
	Key    string         `json:"key"`
	URL    string         `json:"url"`
	Fields map[string]any `json:"fields"`
}

// Discover runs an integration's discover op for a workspace and makes a card for each new item,
// up to the workspace's cap. It never makes a card for an item it discovered before, even when
// that card was archived or deleted. The run's outcome is kept with the settings.
func (m *Manager) Discover(ctx context.Context, ws domain.Workspace, sc domain.Script) (DiscoverResult, error) {
	res := DiscoverResult{Created: []domain.Card{}}
	man, problems := script.ParseManifest(sc.Manifest)
	if len(problems) > 0 {
		return res, fmt.Errorf("%s has a manifest problem: %s", sc.Name, strings.Join(problems, "; "))
	}
	if !man.HasOp("discover") {
		return res, fmt.Errorf("%s has no ops.discover", sc.Name)
	}
	set := LoadDiscover(m.Base, ws.ID, sc.Name)
	limit := set.MaxPerRun
	if limit <= 0 {
		limit = DefaultDiscoverMax
	}
	res, runErr := m.discover(ctx, ws, sc, set, limit)
	now := time.Now().UTC()
	set = LoadDiscover(m.Base, ws.ID, sc.Name)
	set.LastRunAt, set.LastCreated, set.LastError = &now, len(res.Created), errString(runErr)
	if err := SaveDiscover(m.Base, ws.ID, sc.Name, set); err != nil && runErr == nil {
		runErr = err
	}
	if len(res.Created) > 0 {
		if m.Changed != nil {
			m.Changed(ws.ID, "cards", "links")
		}
		if m.Notify != nil {
			m.Notify(ws.ID, sc.Name, fmt.Sprintf("%s added %d card%s to the board.", sc.Name, len(res.Created), map[bool]string{true: "", false: "s"}[len(res.Created) == 1]))
		}
	}
	return res, runErr
}

func (m *Manager) discover(ctx context.Context, ws domain.Workspace, sc domain.Script, set DiscoverSettings, limit int) (DiscoverResult, error) {
	res := DiscoverResult{Created: []domain.Card{}}
	// The network work runs outside any transaction; the cards are made in one short one.
	run, err := m.Op(ctx, m.Base, ws, sc, "discover", map[string]any{"limit": limit})
	if err != nil {
		return res, err
	}
	var out struct {
		Items []DiscoveredItem `json:"items"`
	}
	if err := json.Unmarshal(run.Result.Output, &out); err != nil {
		return res, err
	}
	events := append([]board.Event{}, run.Events...)
	err = m.Base.Tx(func(tx *store.Store) error {
		seen := map[string]bool{}
		for _, it := range out.Items {
			if seen[it.ItemID] {
				continue
			}
			seen[it.ItemID] = true
			done, err := tx.Discovered.Has(ws.ID, sc.Name, it.ItemID)
			if err != nil {
				return err
			}
			if done {
				res.Known++
				continue
			}
			if c, ok, err := linkedCard(tx, ws.ID, sc.Name, it.ItemID); err != nil {
				return err
			} else if ok {
				// A card the user linked by hand: remember it, make nothing.
				res.Known++
				if err := tx.Discovered.Add(ws.ID, sc.Name, it.ItemID, c.ID); err != nil {
					return err
				}
				continue
			}
			if len(res.Created) >= limit {
				res.Left++
				continue
			}
			card, _, err := m.CreateCard(tx, ws, sc.Name, CardSpec{Key: it.Key, Title: it.Title, Column: set.Column, ItemID: it.ItemID, URL: it.URL, Fields: it.Fields}, &events)
			if err != nil {
				return fmt.Errorf("item %s: %w", it.ItemID, err)
			}
			res.Created = append(res.Created, card)
		}
		return nil
	})
	if err != nil {
		return DiscoverResult{Created: []domain.Card{}}, err
	}
	if m.Events != nil && len(events) > 0 {
		if err := m.Events(ctx, ws.ID, events); err != nil {
			m.Log.Debug("discover rules", "script", sc.Name, "error", err.Error())
		}
	}
	return res, nil
}

// discoverAll runs discovery for every workspace that uses the integration and switched it on.
func (m *Manager) discoverAll(ctx context.Context, sc domain.Script) error {
	wss, err := m.Base.Workspaces.List(false)
	if err != nil {
		return err
	}
	var firstErr error
	for _, ws := range wss {
		if (sc.WorkspaceID != "" && sc.WorkspaceID != ws.ID) || !contains(ws.Integrations, sc.Name) {
			continue
		}
		if !LoadDiscover(m.Base, ws.ID, sc.Name).Enabled {
			continue
		}
		if _, err := m.Discover(ctx, ws, sc); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("discover in %s: %w", ws.Name, err)
		}
	}
	return firstErr
}
