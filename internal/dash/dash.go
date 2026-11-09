// Package dash holds the dashboard: which blocks a workspace shows, how they are validated, and the
// named queries that fill them. A block never runs arbitrary code or SQL; it names a query.
package dash

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

// Block types.
const (
	TypeStats    = "stats"
	TypeBars     = "bars"
	TypeTable    = "table"
	TypeHeatmap  = "heatmap"
	TypeProse    = "prose"
	TypePlanner  = "planner"
	TypeActivity = "activity"
	TypeCustom   = "custom"
)

// Types lists every block type.
var Types = []string{TypeStats, TypeBars, TypeTable, TypeHeatmap, TypeProse, TypePlanner, TypeActivity, TypeCustom}

// Limits.
const (
	MaxBlocks     = 24
	MaxHTML       = 256 << 10
	MaxText       = 20000
	MinHeight     = 80
	MaxHeight     = 1200
	MaxTitle      = 80
	MaxTableCols  = 8
	DefaultActive = "live:diagram"
)

// Shapes a query returns.
const (
	ShapeGroups = "groups"
	ShapeRows   = "rows"
)

// accepts says which query shapes each data block type can show. A type that is not here takes no
// source (prose, planner) or an optional one (custom).
var accepts = map[string][]string{
	TypeStats:    {ShapeGroups},
	TypeBars:     {ShapeGroups},
	TypeHeatmap:  {ShapeGroups},
	TypeTable:    {ShapeGroups, ShapeRows},
	TypeActivity: {ShapeRows},
	TypeCustom:   {ShapeGroups, ShapeRows},
}

// needsSource lists the types that cannot work without a query.
var needsSource = map[string]bool{TypeStats: true, TypeBars: true, TypeTable: true, TypeHeatmap: true, TypeActivity: true}

// Column is a table column: which key of a row, and its heading.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Source is a block's data: the named query, its arguments and presentation options.
type Source struct {
	Query   string         `json:"query,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Height  int            `json:"height,omitempty"`  // 80 to 1200; 0 means automatic
	Columns []Column       `json:"columns,omitempty"` // table blocks: which keys to show
	From    string         `json:"from,omitempty"`    // custom blocks pinned from a chat widget: the message id
}

// ParseSource decodes a block's stored source. An empty source is the zero Source.
func ParseSource(raw json.RawMessage) (Source, error) {
	var s Source
	if len(raw) == 0 || string(raw) == "null" {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("the source is not valid JSON: %w", err)
	}
	return s, nil
}

// Spec is a block as a caller submits it (dash_set, dash_block_add).
type Spec struct {
	Type     string  `json:"type" jsonschema:"stats, bars, table, heatmap, prose, planner, activity or custom"`
	Title    string  `json:"title" jsonschema:"the block's heading"`
	Span     int     `json:"span,omitempty" jsonschema:"1 for half width, 2 for full width; default 1"`
	Source   *Source `json:"source,omitempty" jsonschema:"stats, bars, heatmap, table and activity blocks need a query here; custom may have one"`
	HTML     string  `json:"html,omitempty" jsonschema:"custom: the widget's HTML. It renders in a sandboxed frame with no network"`
	Text     string  `json:"text,omitempty" jsonschema:"prose: the text. planner: the last summary the planner wrote"`
	Schedule string  `json:"schedule,omitempty" jsonschema:"planner blocks only: hourly or daily HH:MM, or empty to write only on demand"`
}

// Validate checks specs and normalizes them in place (span defaults to 1). It returns every
// problem at once, each naming its block, so a caller can fix them all in one go.
func Validate(specs []Spec, knownQuery func(name string) (QuerySpec, bool)) []string {
	var errs []string
	if len(specs) == 0 {
		return []string{"the dashboard needs at least one block"}
	}
	if len(specs) > MaxBlocks {
		errs = append(errs, fmt.Sprintf("at most %d blocks, got %d", MaxBlocks, len(specs)))
	}
	for i := range specs {
		for _, p := range ValidateOne(&specs[i], knownQuery) {
			errs = append(errs, fmt.Sprintf("block %d (%s): %s", i+1, label(specs[i]), p))
		}
	}
	return errs
}

func label(s Spec) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	return "untitled"
}

var scheduleRe = regexp.MustCompile(`^daily ([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidateOne checks one spec and normalizes it. It returns every problem with it.
func ValidateOne(b *Spec, knownQuery func(name string) (QuerySpec, bool)) []string {
	var errs []string
	bad := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	b.Title = strings.TrimSpace(b.Title)
	if b.Title == "" {
		bad("needs a title")
	}
	if len([]rune(b.Title)) > MaxTitle {
		bad("the title is longer than %d characters", MaxTitle)
	}
	switch b.Span {
	case 0:
		b.Span = 1
	case 1, 2:
	default:
		bad("span must be 1 (half width) or 2 (full width), got %d", b.Span)
	}
	if !contains(Types, b.Type) {
		bad("type %q is not one of %s", b.Type, strings.Join(Types, ", "))
		return errs
	}
	src := b.Source
	if src != nil && src.Height != 0 && (src.Height < MinHeight || src.Height > MaxHeight) {
		bad("height must be between %d and %d, got %d", MinHeight, MaxHeight, src.Height)
	}
	shapes, takesSource := accepts[b.Type]
	switch {
	case takesSource && (src == nil || src.Query == ""):
		if needsSource[b.Type] {
			bad("%s blocks need a source query, such as {\"query\": \"cards.count\", \"args\": {\"by\": \"column\"}}", b.Type)
		}
	case takesSource:
		q, ok := knownQuery(src.Query)
		if !ok {
			bad("unknown query %q. Known: %s", src.Query, strings.Join(QueryNames(knownQuery), ", "))
			break
		}
		for _, p := range checkArgs(q, src.Args) {
			bad("%s: %s", src.Query, p)
		}
		if shape := ShapeOf(q, src.Args); !contains(shapes, shape) {
			bad("%s returns %s, which a %s block cannot show (use %s)", src.Query, shape, b.Type, strings.Join(shapes, " or "))
		}
	case src != nil && (src.Query != "" || len(src.Args) > 0):
		bad("%s blocks do not take a source query", b.Type)
	}
	if b.Type == TypeTable && src != nil {
		if len(src.Columns) > MaxTableCols {
			bad("at most %d columns", MaxTableCols)
		}
		for _, c := range src.Columns {
			if c.Key == "" {
				bad("every column needs a key")
			}
		}
	}
	if b.Type != TypeCustom && b.HTML != "" {
		bad("only custom blocks take html")
	}
	if b.Type != TypeProse && b.Type != TypePlanner && b.Text != "" {
		bad("only prose and planner blocks take text")
	}
	if b.Type != TypePlanner && b.Schedule != "" {
		bad("only planner blocks take a schedule")
	}
	switch b.Type {
	case TypeCustom:
		if strings.TrimSpace(b.HTML) == "" {
			bad("custom blocks need html")
		}
		if len(b.HTML) > MaxHTML {
			bad("the html is %d KB; keep it under %d KB", len(b.HTML)>>10, MaxHTML>>10)
		}
	case TypeProse:
		if strings.TrimSpace(b.Text) == "" {
			bad("prose blocks need text")
		}
	case TypePlanner:
		if b.Schedule != "" && b.Schedule != "hourly" && !scheduleRe.MatchString(b.Schedule) {
			bad("schedule %q must be hourly, daily HH:MM (24-hour), or empty", b.Schedule)
		}
	}
	if len(b.Text) > MaxText {
		bad("the text is %d characters; keep it under %d", len(b.Text), MaxText)
	}
	return errs
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Domain turns a validated spec into a block to store.
func (s Spec) Domain(workspaceID string) (domain.Block, error) {
	src := "{}"
	if s.Source != nil {
		b, err := json.Marshal(s.Source)
		if err != nil {
			return domain.Block{}, err
		}
		src = string(b)
	}
	return domain.Block{WorkspaceID: workspaceID, Type: s.Type, Title: s.Title, Span: s.Span, Source: json.RawMessage(src), HTML: s.HTML, Text: s.Text, Schedule: s.Schedule}, nil
}

// SpecOf is the inverse of Spec.Domain.
func SpecOf(b domain.Block) Spec {
	s := Spec{Type: b.Type, Title: b.Title, Span: b.Span, HTML: b.HTML, Text: b.Text, Schedule: b.Schedule}
	if src, err := ParseSource(b.Source); err == nil && (src.Query != "" || src.Height != 0 || len(src.Columns) > 0 || src.From != "" || len(src.Args) > 0) {
		s.Source = &src
	}
	return s
}

// SinceCutoff turns "today", "24h", "7d" or "90m" into the time it starts at.
func SinceCutoff(since string, now time.Time) (time.Time, error) {
	since = strings.TrimSpace(since)
	if since == "" {
		return time.Time{}, nil
	}
	if since == "today" {
		y, m, d := now.Local().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Local().Location()), nil
	}
	m := sinceRe.FindStringSubmatch(since)
	if m == nil {
		return time.Time{}, fmt.Errorf("since %q must be today, or a number with m, h or d, such as 90m, 24h or 7d", since)
	}
	var n int
	_, _ = fmt.Sscanf(m[1], "%d", &n)
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
	return now.Add(-time.Duration(n) * unit), nil
}

var sinceRe = regexp.MustCompile(`^([0-9]{1,4})([mhd])$`)
