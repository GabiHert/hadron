package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// EntityDash is the changed-event entity of the dashboard.
const EntityDash = "dash"

// BlockView is a block with its data resolved.
type BlockView struct {
	domain.Block
	Data  *dash.Result `json:"data"`
	Error string       `json:"error,omitempty"`
}

// DashGetOut is the result of dash_get.
type DashGetOut struct {
	Blocks  []BlockView      `json:"blocks"`
	Queries []dash.QuerySpec `json:"queries" jsonschema:"the named queries a block's source can use in this workspace"`
}

// DashGetIn is the input of dash_get.
type DashGetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// DashSetIn is the input of dash_set.
type DashSetIn struct {
	WorkspaceID string      `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Blocks      []dash.Spec `json:"blocks" jsonschema:"the whole dashboard, in order. Replaces the current blocks"`
}

// DashBlockAddIn is the input of dash_block_add.
type DashBlockAddIn struct {
	WorkspaceID string       `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Type        string       `json:"type" jsonschema:"stats, bars, table, heatmap, prose, planner, activity or custom"`
	Title       string       `json:"title" jsonschema:"the block's heading"`
	Span        int          `json:"span,omitempty" jsonschema:"1 for half width, 2 for full width; default 1"`
	Source      *dash.Source `json:"source,omitempty" jsonschema:"the named query, its args and options. dash_get lists the queries"`
	HTML        string       `json:"html,omitempty" jsonschema:"custom: the widget's HTML, rendered in a sandboxed frame with no network"`
	Text        string       `json:"text,omitempty" jsonschema:"prose or planner: the text"`
	Schedule    string       `json:"schedule,omitempty" jsonschema:"planner: hourly or daily HH:MM; empty to write only on demand"`
	Position    *int         `json:"position,omitempty" jsonschema:"where to put it, 0 for first; default last"`
}

// DashBlockUpdateIn is the input of dash_block_update. Only the fields you pass change.
type DashBlockUpdateIn struct {
	BlockID  string       `json:"block_id" jsonschema:"the block id from dash_get"`
	Title    *string      `json:"title,omitempty" jsonschema:"new heading"`
	Span     *int         `json:"span,omitempty" jsonschema:"1 or 2"`
	Source   *dash.Source `json:"source,omitempty" jsonschema:"new source; replaces the old one"`
	HTML     *string      `json:"html,omitempty" jsonschema:"custom: new HTML"`
	Text     *string      `json:"text,omitempty" jsonschema:"prose or planner: new text. For a planner block this is how the planner writes its summary"`
	Schedule *string      `json:"schedule,omitempty" jsonschema:"planner: hourly, daily HH:MM, or empty"`
}

// DashBlockMoveIn is the input of dash_block_move.
type DashBlockMoveIn struct {
	BlockID  string `json:"block_id" jsonschema:"the block id from dash_get"`
	Position int    `json:"position" jsonschema:"the new place, 0 for first"`
}

// DashBlockIDIn names one block.
type DashBlockIDIn struct {
	BlockID string `json:"block_id" jsonschema:"the block id from dash_get"`
}

// DashQueryRunIn is the input of dash_query_run.
type DashQueryRunIn struct {
	WorkspaceID string         `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Query       string         `json:"query" jsonschema:"a named query: cards.count, cards.list, questions.open, mcp.calls, rules.fired or <integration>.links / <integration>.count, or a query an integration defines (<integration>.<name>, see dash_get)"`
	Args        map[string]any `json:"args,omitempty" jsonschema:"the query's arguments, such as {\"by\": \"column\"}"`
}

// WidgetPinIn is the input of widget_pin.
type WidgetPinIn struct {
	MessageID string `json:"message_id" jsonschema:"the id of a widget message from chat_read"`
	Title     string `json:"title,omitempty" jsonschema:"the block's heading; default is the widget's title"`
}

func dashEnv(c *Call, w domain.Workspace) dash.Env {
	env := dash.Env{Store: c.Store, Fields: fieldRegistry(c), Workspace: w, Now: time.Now()}
	if c.Deps.Integrations != nil {
		env.Custom = c.Deps.Integrations
	}
	return env
}

func resolveBlock(ctx context.Context, env dash.Env, b domain.Block) BlockView {
	v := BlockView{Block: b}
	src, err := dash.ParseSource(b.Source)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	if src.Query == "" {
		return v
	}
	res, err := dash.Run(ctx, env, src.Query, src.Args)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.Data = &res
	if src.Layout == dash.LayoutCalendar {
		if p := dash.CalendarProblem(res.Groups); p != "" {
			v.Error = p
		}
	}
	if res.Error != "" && v.Error == "" {
		v.Error = "the last refresh failed: " + res.Error
	}
	return v
}

func blockByID(c *Call, id string) (domain.Block, domain.Workspace, error) {
	b, err := c.Store.Blocks.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return b, domain.Workspace{}, fmt.Errorf("no block %q. dash_get lists the blocks", id)
	}
	if err != nil {
		return b, domain.Workspace{}, err
	}
	w, err := c.Workspace(b.WorkspaceID)
	return b, w, err
}

func checkSpec(env dash.Env, specs ...dash.Spec) error {
	var problems []string
	for i := range specs {
		for _, p := range dash.ValidateOne(&specs[i], env.Lookup) {
			problems = append(problems, p)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("the block is not valid:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func registerDash(r *Registry) {
	Register(r, Spec{Name: "dash_get", Description: "Read the workspace's dashboard: each block with its data already resolved by its named query, and the queries available. Block types: stats, bars, table, heatmap, prose, planner, activity, custom."},
		func(ctx context.Context, c *Call, in DashGetIn) (DashGetOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return DashGetOut{}, err
			}
			blocks, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return DashGetOut{}, err
			}
			env := dashEnv(c, w)
			out := DashGetOut{Blocks: []BlockView{}, Queries: env.AllQueries()}
			for _, b := range blocks {
				out.Blocks = append(out.Blocks, resolveBlock(ctx, env, b))
			}
			return out, nil
		})

	Register(r, Spec{Name: "dash_set", Description: "Replace the whole dashboard with the given blocks, in order. Every problem is listed at once; nothing changes unless all blocks are valid. The history keeps the old layout.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in DashSetIn) ([]domain.Block, error) {
			if err := userOrPlanner(c, "change the dashboard"); err != nil {
				return nil, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			env := dashEnv(c, w)
			if problems := dash.Validate(in.Blocks, env.Lookup); len(problems) > 0 {
				return nil, fmt.Errorf("the dashboard is not valid:\n- %s", strings.Join(problems, "\n- "))
			}
			before, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return nil, err
			}
			for _, b := range before {
				if err := c.Store.Blocks.Delete(b.ID); err != nil {
					return nil, err
				}
			}
			out := []domain.Block{}
			for _, s := range in.Blocks {
				b, err := s.Domain(w.ID)
				if err != nil {
					return nil, err
				}
				if err := c.Store.Blocks.Create(&b); err != nil {
					return nil, err
				}
				out = append(out, b)
			}
			c.RecordChange(w.ID, before, out)
			return out, nil
		})

	Register(r, Spec{Name: "dash_block_add", Description: "Add one block to the dashboard. A stats, bars, heatmap, table or activity block needs a source query; prose needs text; planner blocks are written by the planner with dash_block_update; custom blocks take HTML.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in DashBlockAddIn) (domain.Block, error) {
			if err := userOrPlanner(c, "change the dashboard"); err != nil {
				return domain.Block{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.Block{}, err
			}
			env := dashEnv(c, w)
			spec := dash.Spec{Type: in.Type, Title: in.Title, Span: in.Span, Source: in.Source, HTML: in.HTML, Text: in.Text, Schedule: in.Schedule}
			if err := checkSpec(env, spec); err != nil {
				return domain.Block{}, err
			}
			existing, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return domain.Block{}, err
			}
			if len(existing) >= dash.MaxBlocks {
				return domain.Block{}, fmt.Errorf("the dashboard already has %d blocks, the most it holds. Remove one first", dash.MaxBlocks)
			}
			// spec was normalized by checkSpec (it works on a copy of the slice element, so redo it here).
			_ = dash.ValidateOne(&spec, env.Lookup)
			b, err := spec.Domain(w.ID)
			if err != nil {
				return b, err
			}
			if err := c.Store.Blocks.Create(&b); err != nil {
				return b, err
			}
			if in.Position != nil {
				if b, err = placeBlock(c, w.ID, b.ID, *in.Position); err != nil {
					return b, err
				}
			}
			c.RecordChange(w.ID, nil, b)
			return b, nil
		})

	Register(r, Spec{Name: "dash_block_update", Description: "Change a dashboard block. Pass only what changes. A planner block's text is written this way, which stamps the time. The result is checked as a whole, so a change that makes the block invalid is refused with every problem.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in DashBlockUpdateIn) (domain.Block, error) {
			if err := userOrPlanner(c, "change the dashboard"); err != nil {
				return domain.Block{}, err
			}
			b, w, err := blockByID(c, in.BlockID)
			if err != nil {
				return b, err
			}
			before := b
			spec := dash.SpecOf(b)
			if in.Title != nil {
				spec.Title = *in.Title
			}
			if in.Span != nil {
				spec.Span = *in.Span
			}
			if in.Source != nil {
				spec.Source = in.Source
			}
			if in.HTML != nil {
				spec.HTML = *in.HTML
			}
			if in.Text != nil {
				spec.Text = *in.Text
			}
			if in.Schedule != nil {
				spec.Schedule = *in.Schedule
			}
			if err := checkSpec(dashEnv(c, w), spec); err != nil {
				return b, err
			}
			_ = dash.ValidateOne(&spec, dashEnv(c, w).Lookup)
			nb, err := spec.Domain(w.ID)
			if err != nil {
				return b, err
			}
			b.Title, b.Span, b.Source, b.HTML, b.Text, b.Schedule = nb.Title, nb.Span, nb.Source, nb.HTML, nb.Text, nb.Schedule
			if in.Text != nil && b.Type == dash.TypePlanner {
				now := time.Now().UTC()
				b.TextAt = &now
			}
			if err := c.Store.Blocks.Update(&b); err != nil {
				return b, err
			}
			c.RecordChange(w.ID, before, b)
			return b, nil
		})

	Register(r, Spec{Name: "dash_block_move", Description: "Move a block to a place in the dashboard's order (0 is first).",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in DashBlockMoveIn) ([]domain.Block, error) {
			if err := userOrPlanner(c, "change the dashboard"); err != nil {
				return nil, err
			}
			b, w, err := blockByID(c, in.BlockID)
			if err != nil {
				return nil, err
			}
			before, _ := c.Store.Blocks.List(w.ID)
			if _, err := placeBlock(c, w.ID, b.ID, in.Position); err != nil {
				return nil, err
			}
			after, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return nil, err
			}
			c.RecordChange(w.ID, before, after)
			return after, nil
		})

	Register(r, Spec{Name: "dash_block_remove", Description: "Remove a block from the dashboard.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in DashBlockIDIn) (domain.Block, error) {
			if err := userOrPlanner(c, "change the dashboard"); err != nil {
				return domain.Block{}, err
			}
			b, w, err := blockByID(c, in.BlockID)
			if err != nil {
				return b, err
			}
			if err := c.Store.Blocks.Delete(b.ID); err != nil {
				return b, err
			}
			c.RecordChange(w.ID, b, nil)
			return b, nil
		})

	Register(r, Spec{Name: "dash_block_refresh", Description: "Refresh a block. A data block reruns its query and returns the result. A planner block types an instruction into the planner's session, asking it to write a short summary with dash_block_update; that costs the planner a turn, so it only happens when you ask (or on the block's own schedule).",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in DashBlockIDIn) (BlockView, error) {
			b, w, err := blockByID(c, in.BlockID)
			if err != nil {
				return BlockView{}, err
			}
			if b.Type != dash.TypePlanner {
				return resolveBlock(ctx, dashEnv(c, w), b), nil
			}
			if c.Actor.Kind == KindSession {
				return BlockView{}, errors.New("a card session cannot ask the planner to write a summary")
			}
			if _, err := sendTo(c, w.ID, nil, plannerInstruction(b), nil); err != nil {
				return BlockView{}, fmt.Errorf("could not ask the planner: %w. Start the planner (planner_start) and try again", err)
			}
			return BlockView{Block: b}, nil
		})

	Register(r, Spec{Name: "dash_query_run", Description: "Run a named dashboard query now and return its result. cards.count{by,expr} gives groups; cards.list{expr}, questions.open, mcp.calls{since,by} and rules.fired{since} give rows (or groups); an integration adds <integration>.links and <integration>.count{by}, plus any query its script defines in ops.queries (<integration>.<name>), served from a cache: the result carries age_s, stale, pending and error."},
		func(ctx context.Context, c *Call, in DashQueryRunIn) (dash.Result, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return dash.Result{}, err
			}
			return dash.Run(ctx, dashEnv(c, w), in.Query, in.Args)
		})

	Register(r, Spec{Name: "widget_pin", Description: "Pin a widget from a chat to the dashboard as a custom block. Only when the user asks: chat widgets are for explaining. Pinning the same widget twice returns the block that already exists.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in WidgetPinIn) (domain.Block, error) {
			if err := userOrPlanner(c, "pin widgets"); err != nil {
				return domain.Block{}, err
			}
			msg, err := c.Store.Messages.Get(in.MessageID)
			if err != nil {
				return domain.Block{}, fmt.Errorf("no message %q. chat_read lists messages and their ids", in.MessageID)
			}
			if msg.Kind != "widget" {
				return domain.Block{}, errors.New("that message is not a widget")
			}
			w, err := c.Workspace(msg.WorkspaceID)
			if err != nil {
				return domain.Block{}, err
			}
			blocks, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return domain.Block{}, err
			}
			for _, b := range blocks {
				if src, _ := dash.ParseSource(b.Source); src.From == msg.ID {
					return b, nil
				}
			}
			title := strings.TrimSpace(in.Title)
			if title == "" {
				title = msg.WidgetTitle
			}
			spec := dash.Spec{Type: dash.TypeCustom, Title: title, Span: 1, HTML: msg.WidgetHTML, Source: &dash.Source{From: msg.ID}}
			if err := checkSpec(dashEnv(c, w), spec); err != nil {
				return domain.Block{}, err
			}
			if len(blocks) >= dash.MaxBlocks {
				return domain.Block{}, fmt.Errorf("the dashboard already has %d blocks, the most it holds. Remove one first", dash.MaxBlocks)
			}
			b, err := spec.Domain(w.ID)
			if err != nil {
				return b, err
			}
			if err := c.Store.Blocks.Create(&b); err != nil {
				return b, err
			}
			c.RecordChange(w.ID, nil, b)
			return b, nil
		})
}

// placeBlock moves a block to index pos (clamped) and renumbers the rest.
func placeBlock(c *Call, workspaceID, id string, pos int) (domain.Block, error) {
	list, err := c.Store.Blocks.List(workspaceID)
	if err != nil {
		return domain.Block{}, err
	}
	var moved domain.Block
	rest := make([]domain.Block, 0, len(list))
	for _, b := range list {
		if b.ID == id {
			moved = b
		} else {
			rest = append(rest, b)
		}
	}
	if pos < 0 {
		pos = 0
	}
	if pos > len(rest) {
		pos = len(rest)
	}
	order := append(rest[:pos:pos], append([]domain.Block{moved}, rest[pos:]...)...)
	for i := range order {
		if order[i].Position != i {
			order[i].Position = i
			if err := c.Store.Blocks.Update(&order[i]); err != nil {
				return moved, err
			}
		}
		if order[i].ID == id {
			moved = order[i]
		}
	}
	return moved, nil
}

func plannerInstruction(b domain.Block) string {
	return fmt.Sprintf(`[dashboard] Write the text for the dashboard block "%s" (id %s): two to four plain sentences on what changed on the board and what needs the user. Then call dash_block_update with {"block_id":"%s","text":"<your text>"}. Do not answer in chat.`, b.Title, b.ID, b.ID)
}

// ---- planner blocks on a schedule ----

type dashClock struct {
	mu   sync.Mutex
	last time.Time
}

// TickDashboard asks the planner to write each planner block whose schedule came due since the last
// tick. A block with no schedule (the default) is never asked, so a default dashboard spends no
// tokens. The first tick only starts the clock, so a restart never replays missed times.
func (r *Registry) TickDashboard(ctx context.Context, now time.Time) error {
	r.dclock.mu.Lock()
	last := r.dclock.last
	r.dclock.last = now
	r.dclock.mu.Unlock()
	if last.IsZero() {
		return nil
	}
	workspaces, err := r.deps.Store.Workspaces.List(false)
	if err != nil {
		return err
	}
	for _, w := range workspaces {
		blocks, err := r.deps.Store.Blocks.List(w.ID)
		if err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != dash.TypePlanner || !scheduleDue(b.Schedule, last, now) {
				continue
			}
			raw, _ := json.Marshal(map[string]string{"block_id": b.ID})
			if _, err := r.Invoke(ctx, You, "dash_block_refresh", string(raw)); err != nil {
				log.Printf("dashboard schedule for %q: %v", b.Title, err)
			}
		}
	}
	return nil
}

// scheduleDue says whether a schedule fired between last (exclusive) and now (inclusive), in local time.
func scheduleDue(schedule string, last, now time.Time) bool {
	last, now = last.Local(), now.Local()
	switch {
	case schedule == "hourly":
		return !now.Truncate(time.Hour).Equal(last.Truncate(time.Hour))
	case strings.HasPrefix(schedule, "daily "):
		var h, m int
		if _, err := fmt.Sscanf(strings.TrimPrefix(schedule, "daily "), "%d:%d", &h, &m); err != nil {
			return false
		}
		for d := last; !d.After(now); d = d.AddDate(0, 0, 1) {
			at := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, d.Location())
			if at.After(last) && !at.After(now) {
				return true
			}
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
		return at.After(last) && !at.After(now)
	}
	return false
}
