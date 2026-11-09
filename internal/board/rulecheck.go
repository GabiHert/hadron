package board

import (
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/robfig/cron/v3"
)

// ReportKinds are the kinds a report trigger may name.
var ReportKinds = []string{"progress", "question", "blocked", "done"}

// ParseCron checks a five-field cron spec.
func ParseCron(spec string) (cron.Schedule, error) {
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("cron spec %q is not valid: %v. Use five fields, such as 0 9 * * 1-5 (minute hour day month weekday)", spec, err)
	}
	return s, nil
}

// ResolveColumn finds a column by id or by name (ignoring case).
func ResolveColumn(cols []domain.Column, ref string) (domain.Column, bool) {
	for _, c := range cols {
		if c.ID == ref {
			return c, true
		}
	}
	for _, c := range cols {
		if strings.EqualFold(c.Name, ref) {
			return c, true
		}
	}
	return domain.Column{}, false
}

// CheckRule validates a rule against its workspace and returns the trigger and actions with column
// names turned into ids. Every problem is listed, so a caller can fix them in one go.
func CheckRule(tx *store.Store, reg *fields.Registry, ws domain.Workspace, t Trigger, condition string, acts []Action) (Trigger, []Action, error) {
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	if reg == nil {
		reg = fields.NewRegistry()
	}
	schema, err := reg.Schema(tx, ws)
	if err != nil {
		return t, acts, err
	}
	cols, err := tx.Columns.List(ws.ID)
	if err != nil {
		return t, acts, err
	}
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}

	if !contains(Triggers, t.On) {
		bad("trigger.on %q must be one of %s", t.On, strings.Join(Triggers, ", "))
	}
	switch t.On {
	case OnFieldChanged:
		if t.Field != "" {
			if _, ok := fields.Find(schema, t.Field); !ok {
				bad("trigger.field %q is not a field of this workspace. Valid paths: %s", t.Field, strings.Join(fields.Paths(schema), ", "))
			}
		}
	case OnColumnEntered:
		if t.Column != "" {
			if c, ok := ResolveColumn(cols, t.Column); ok {
				t.Column = c.ID
			} else {
				bad("trigger.column %q is not a column of this workspace. Columns: %s", t.Column, strings.Join(colNames, ", "))
			}
		}
	case OnReport:
		if t.Kind != "" && !contains(ReportKinds, t.Kind) {
			bad("trigger.kind %q must be one of %s, or empty for any report", t.Kind, strings.Join(ReportKinds, ", "))
		}
	case OnIdleFor:
		if t.Minutes < 1 {
			bad("trigger.minutes must be 1 or more for idle_for")
		}
	case OnCron:
		if _, err := ParseCron(t.Spec); err != nil {
			bad("%v", err)
		}
	}

	if strings.TrimSpace(condition) != "" {
		env, err := reg.NewEnv(ws)
		if err != nil {
			return t, acts, err
		}
		if _, err := env.Compile(condition); err != nil {
			bad("condition: %v", err)
		}
	}

	if len(acts) == 0 {
		bad("a rule needs at least one action")
	}
	out := make([]Action, len(acts))
	copy(out, acts)
	for i, a := range out {
		at := func(format string, args ...any) { bad("actions[%d] (%s): %s", i, a.Type, fmt.Sprintf(format, args...)) }
		switch a.Type {
		case ActMove:
			n := 0
			for _, set := range []bool{a.ColumnID != "", a.Role != "", a.To != ""} {
				if set {
					n++
				}
			}
			switch {
			case n != 1:
				at("pass exactly one of column_id, role or to")
			case a.ColumnID != "":
				if c, ok := ResolveColumn(cols, a.ColumnID); ok {
					out[i].ColumnID = c.ID
				} else {
					at("column %q is not a column of this workspace. Columns: %s", a.ColumnID, strings.Join(colNames, ", "))
				}
			case a.Role != "":
				if !contains(domain.Roles, a.Role) {
					at("role %q must be one of %s", a.Role, strings.Join(domain.Roles, ", "))
				}
			case a.To != ReturnColumn:
				at("to must be %q", ReturnColumn)
			}
		case ActSetField:
			e, ok := fields.Find(schema, a.Path)
			switch {
			case !ok:
				at("path %q is not a field of this workspace. Valid paths: %s", a.Path, strings.Join(fields.Paths(schema), ", "))
			case !e.Editable:
				at("%s cannot be set: %s", a.Path, ReadOnlyWhy(a.Path))
			case e.Namespace() != fields.NSCard:
				if _, err := fields.Coerce(e, a.Value); err != nil {
					at("%v", err)
				}
			}
		case ActNotify, ActTellPlanner:
			if strings.TrimSpace(a.Text) == "" {
				at("text is required")
			}
		case ActRunAction:
			if a.Script == "" || a.Action == "" {
				at("script and action are required")
			}
		default:
			bad("actions[%d].type %q must be one of %s", i, a.Type, strings.Join(ActionTypes, ", "))
		}
	}
	if len(problems) > 0 {
		return t, out, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return t, out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
