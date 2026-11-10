package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
)

// EntityChipColors is the changed-event entity of a workspace's tag colours.
const EntityChipColors = "chip_colors"

// ChipPalette is every colour a tag may have: theme tokens (--tag-<name> in the frontend's
// tokens.css), readable in the light and the dark theme. Automatic colours use all but grey.
var ChipPalette = []string{"grey", "blue", "teal", "green", "lime", "amber", "orange", "red", "pink", "purple"}

// Paths that are tags without being in the field schema.
const (
	ChipPathKey   = "card.key" // the value is the key's prefix: ACME for ACME-508
	ChipPathStage = "card.stage"
	ChipPathModel = "card.model"
)

// chipColorsKey is the setting that holds a workspace's tag colours.
func chipColorsKey(workspaceID string) string { return "chip_colors:" + workspaceID }

// ChipColors is a workspace's tag colours: a colour per field path and value, and the fields
// whose values without a colour stay plain grey instead of getting an automatic one.
type ChipColors struct {
	Colors map[string]map[string]string `json:"colors" jsonschema:"colour by field path, then by value"`
	Plain  []string                     `json:"plain" jsonschema:"field paths whose uncoloured values stay grey (automatic colours off)"`
}

// ChipColorRule is one coloured value.
type ChipColorRule struct {
	Path  string `json:"path"`
	Value string `json:"value"`
	Color string `json:"color"`
}

// ChipColorListOut is the result of chip_color_list.
type ChipColorListOut struct {
	Palette []string        `json:"palette"`
	Rules   []ChipColorRule `json:"rules"`
	Plain   []string        `json:"plain"`
}

// ChipColorListIn is the input of chip_color_list.
type ChipColorListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ChipColorSetIn is the input of chip_color_set.
type ChipColorSetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Path        string `json:"path" jsonschema:"the tag's field path, such as jira.status, custom.team, card.stage, card.model, or card.key for the key's prefix"`
	Value       string `json:"value" jsonschema:"the value to colour, such as In Progress; for card.key the prefix, such as ACME"`
	Color       string `json:"color,omitempty" jsonschema:"grey, blue, teal, green, lime, amber, orange, red, pink or purple; empty removes the colour (back to automatic)"`
}

// ChipColorAutoSetIn is the input of chip_color_auto_set.
type ChipColorAutoSetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Path        string `json:"path" jsonschema:"the tag's field path"`
	Auto        bool   `json:"auto" jsonschema:"true gives values with no colour an automatic one; false keeps them plain grey"`
}

func loadChipColors(c *Call, workspaceID string) ChipColors {
	cc := ChipColors{Colors: map[string]map[string]string{}, Plain: []string{}}
	if raw, err := c.Store.Settings.Get(chipColorsKey(workspaceID)); err == nil {
		_ = json.Unmarshal(raw, &cc)
	}
	if cc.Colors == nil {
		cc.Colors = map[string]map[string]string{}
	}
	if cc.Plain == nil {
		cc.Plain = []string{}
	}
	return cc
}

func saveChipColors(c *Call, workspaceID string, cc ChipColors) error {
	raw, err := json.Marshal(cc)
	if err != nil {
		return err
	}
	return c.Store.Settings.Set(chipColorsKey(workspaceID), raw)
}

func (cc ChipColors) out() ChipColorListOut {
	o := ChipColorListOut{Palette: ChipPalette, Rules: []ChipColorRule{}, Plain: append([]string{}, cc.Plain...)}
	for p, vals := range cc.Colors {
		for v, col := range vals {
			o.Rules = append(o.Rules, ChipColorRule{Path: p, Value: v, Color: col})
		}
	}
	sort.Slice(o.Rules, func(i, j int) bool {
		if o.Rules[i].Path != o.Rules[j].Path {
			return o.Rules[i].Path < o.Rules[j].Path
		}
		return o.Rules[i].Value < o.Rules[j].Value
	})
	sort.Strings(o.Plain)
	return o
}

// checkChipPath accepts card.key, card.stage, card.model and every path of the workspace's field schema.
func checkChipPath(c *Call, w domain.Workspace, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == ChipPathKey || path == ChipPathStage || path == ChipPathModel {
		return path, nil
	}
	if path == "" {
		return "", errors.New("path is required, such as jira.status or card.stage")
	}
	schema, err := fieldRegistry(c).Schema(c.Store, w)
	if err != nil {
		return "", err
	}
	if _, ok := fields.Find(schema, path); ok {
		return path, nil
	}
	return "", fmt.Errorf("%q is not a field of workspace %q. Use card.key, card.stage, card.model or a path from field_schema_get", path, w.Name)
}

// ChipKeyPrefix is the part of a card key that card.key colours: ACME for ACME-508.
func ChipKeyPrefix(key string) string {
	key = strings.TrimSpace(key)
	if i := strings.LastIndex(key, "-"); i > 0 {
		key = key[:i]
	}
	return strings.ToUpper(key)
}

func registerChipColors(r *Registry) {
	Register(r, Spec{Name: "chip_color_list", Description: "List a workspace's tag colours: the palette, the colour set for each field value (such as jira.status = Blocked → red), and the fields whose other values stay plain grey. A value with no colour gets a stable automatic one from the palette, except key prefixes (card.key), which stay plain until coloured."},
		func(ctx context.Context, c *Call, in ChipColorListIn) (ChipColorListOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ChipColorListOut{}, err
			}
			return loadChipColors(c, w.ID).out(), nil
		})

	Register(r, Spec{Name: "chip_color_set", Description: "Colour a tag on the card faces: one value of one field, such as path jira.status, value In Progress, colour blue. Works for linked and custom fields, card.stage, card.model and card.key (the value is the key's prefix, such as ACME). An empty colour removes it. Colours are theme tokens, so both themes stay readable. Set colours only when the user asks.",
		Mutates: true, Entities: []string{EntityChipColors}},
		func(ctx context.Context, c *Call, in ChipColorSetIn) (ChipColorListOut, error) {
			if err := userOrPlanner(c, "change tag colours"); err != nil {
				return ChipColorListOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ChipColorListOut{}, err
			}
			path, err := checkChipPath(c, w, in.Path)
			if err != nil {
				return ChipColorListOut{}, err
			}
			value := strings.TrimSpace(in.Value)
			if path == ChipPathKey {
				value = ChipKeyPrefix(value)
			}
			if value == "" || len(value) > 200 {
				return ChipColorListOut{}, errors.New("value must be 1 to 200 characters: the tag's text, such as In Progress")
			}
			color := strings.ToLower(strings.TrimSpace(in.Color))
			if color != "" && !slices.Contains(ChipPalette, color) {
				return ChipColorListOut{}, fmt.Errorf("colour %q is not in the palette: %s, or empty to remove it", in.Color, strings.Join(ChipPalette, ", "))
			}
			cc := loadChipColors(c, w.ID)
			if color == "" {
				delete(cc.Colors[path], value)
				if len(cc.Colors[path]) == 0 {
					delete(cc.Colors, path)
				}
			} else {
				if cc.Colors[path] == nil {
					cc.Colors[path] = map[string]string{}
				}
				if len(cc.Colors[path]) >= 100 && cc.Colors[path][value] == "" {
					return ChipColorListOut{}, fmt.Errorf("%s already has 100 coloured values; remove some first", path)
				}
				cc.Colors[path][value] = color
			}
			if err := saveChipColors(c, w.ID, cc); err != nil {
				return ChipColorListOut{}, err
			}
			return cc.out(), nil
		})

	Register(r, Spec{Name: "chip_color_auto_set", Description: "Turn automatic tag colours on or off for one field: off keeps its values without a colour plain grey; values you coloured keep their colour.",
		Mutates: true, Entities: []string{EntityChipColors}},
		func(ctx context.Context, c *Call, in ChipColorAutoSetIn) (ChipColorListOut, error) {
			if err := userOrPlanner(c, "change tag colours"); err != nil {
				return ChipColorListOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ChipColorListOut{}, err
			}
			path, err := checkChipPath(c, w, in.Path)
			if err != nil {
				return ChipColorListOut{}, err
			}
			cc := loadChipColors(c, w.ID)
			cc.Plain = slices.DeleteFunc(cc.Plain, func(p string) bool { return p == path })
			if !in.Auto {
				cc.Plain = append(cc.Plain, path)
			}
			if err := saveChipColors(c, w.ID, cc); err != nil {
				return ChipColorListOut{}, err
			}
			return cc.out(), nil
		})
}
