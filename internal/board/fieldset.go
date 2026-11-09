package board

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

var nickRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// CheckNick says whether nick is a valid card nickname: kebab-case, at most 60 characters.
func CheckNick(nick string) error {
	if len(nick) > 60 {
		return fmt.Errorf("nick %q is longer than 60 characters", nick)
	}
	if !nickRe.MatchString(nick) {
		return fmt.Errorf("nick %q must be kebab-case: lowercase letters and digits joined by single dashes, such as billing-fix", nick)
	}
	return nil
}

// FieldResult is what SetField changed.
type FieldResult struct {
	Old, New any
	// Moved is set when the field was card.column and the card changed column.
	Moved            bool
	FromID, ToID     string
	FromName, ToName string
}

// ReadOnlyWhy explains why a field cannot be set.
func ReadOnlyWhy(path string) string {
	if path == "card.liveness" {
		return "it follows the card's session"
	}
	return "the field is read-only"
}

// NoField is the error for a path that is not in the schema.
func NoField(reg *fields.Registry, ws domain.Workspace, schema []fields.Entry, path string) error {
	ns, _ := fields.SplitPath(path)
	if _, registered := reg.Provider(ns); registered || (ns != fields.NSCard && ns != fields.NSCustom && strings.Contains(path, ".")) {
		return fields.NotEnabledError(path)
	}
	return fmt.Errorf("no field %q in workspace %q. Valid paths: %s", path, ws.Name, strings.Join(fields.Paths(schema), ", "))
}

// SetField writes one field of a card inside tx: a native field, a custom field, or a field of an
// integration through its provider. It returns the old and new value; New equals Old when nothing
// changed. When dry is true a linked field is not written (no call leaves the app).
func SetField(ctx context.Context, tx *store.Store, reg *fields.Registry, ws domain.Workspace, card domain.Card, path string, value any, dry bool) (FieldResult, error) {
	schema, err := reg.Schema(tx, ws)
	if err != nil {
		return FieldResult{}, err
	}
	entry, known := fields.Find(schema, path)
	if !known {
		return FieldResult{}, NoField(reg, ws, schema, path)
	}
	if !entry.Editable {
		return FieldResult{}, fmt.Errorf("%s cannot be set: %s", path, ReadOnlyWhy(path))
	}
	cols, err := fields.ColumnMap(tx, ws.ID)
	if err != nil {
		return FieldResult{}, err
	}
	vars, err := reg.Values(tx, ws, schema, cols, card)
	if err != nil {
		return FieldResult{}, err
	}
	res := FieldResult{Old: fields.Flat(vars)[path]}
	ns, name := fields.SplitPath(path)
	switch ns {
	case fields.NSCard:
		err = setNative(tx, &card, name, value, &res)
	case fields.NSCustom:
		var stored any
		if stored, err = fields.Coerce(entry, value); err == nil {
			if card.Fields == nil {
				card.Fields = map[string]any{}
			}
			if s, isStr := value.(string); isStr && strings.TrimSpace(s) == "" {
				delete(card.Fields, name)
			} else {
				card.Fields[name] = stored
			}
			res.New = stored
			if res.Old != stored {
				err = tx.Cards.Update(&card)
			}
		}
	default:
		p, _ := reg.Provider(ns)
		var stored any
		if stored, err = fields.Coerce(entry, value); err == nil {
			res.New = stored
			if res.Old != stored && !dry {
				err = p.Set(ctx, tx, card, name, stored)
			}
		}
	}
	return res, err
}

func setNative(tx *store.Store, card *domain.Card, name string, value any, res *FieldResult) error {
	s, ok := value.(string)
	if !ok {
		return fmt.Errorf("card.%s is text, so the value must be a string, got %v", name, value)
	}
	s = strings.TrimSpace(s)
	res.New = s
	switch name {
	case "column":
		cols, err := tx.Columns.List(card.WorkspaceID)
		if err != nil {
			return err
		}
		for _, col := range cols {
			if col.ID != s && !strings.EqualFold(col.Name, s) {
				continue
			}
			res.New = col.Name
			if col.ID != card.ColumnID {
				from, err := tx.Columns.Get(card.ColumnID)
				if err != nil {
					return err
				}
				res.Moved, res.FromID, res.ToID, res.FromName, res.ToName = true, from.ID, col.ID, from.Name, col.Name
				_, err = MoveCard(tx, card.ID, col.ID, -1)
				return err
			}
			return nil
		}
		names := make([]string, len(cols))
		for i, col := range cols {
			names[i] = col.Name
		}
		return fmt.Errorf("no column %q in this workspace. Columns: %s", s, strings.Join(names, ", "))
	case "nick":
		if err := CheckNick(s); err != nil {
			return err
		}
		card.Nick = s
	case "assignee":
		card.Assignee = s
	case "stage":
		card.Stage = s
	case "key":
		card.Key = s
	default:
		return errors.New("unknown native field card." + name)
	}
	if res.Old == s {
		return nil
	}
	return tx.Cards.Update(card)
}
