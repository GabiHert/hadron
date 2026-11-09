package fields

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/store"
)

// CustomKey is the settings key that holds a workspace's custom field definitions.
func CustomKey(workspaceID string) string { return "custom_fields:" + workspaceID }

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// LoadCustom returns the custom field definitions of a workspace, in creation order.
func LoadCustom(st *store.Store, workspaceID string) ([]Entry, error) {
	raw, err := st.Settings.Get(CustomKey(workspaceID))
	if errors.Is(err, store.ErrNotFound) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the stored custom fields are not valid JSON: %w", err)
	}
	return out, nil
}

// SaveCustom stores the definitions.
func SaveCustom(st *store.Store, workspaceID string, list []Entry) error {
	if list == nil {
		list = []Entry{}
	}
	b, _ := json.Marshal(list)
	return st.Settings.Set(CustomKey(workspaceID), b)
}

// CheckCustom validates a definition and normalises it: the path gets the custom. prefix, and the
// flags a custom field always has are set.
func CheckCustom(e Entry) (Entry, error) {
	name := strings.TrimPrefix(e.Path, NSCustom+".")
	if !nameRe.MatchString(name) {
		return e, fmt.Errorf("custom field name %q must be lowercase letters, digits and underscores, starting with a letter (at most 40 characters)", name)
	}
	if e.Type == "" {
		e.Type = TypeText
	}
	ok := false
	for _, t := range Types {
		ok = ok || t == e.Type
	}
	if !ok {
		return e, fmt.Errorf("type %q must be one of %s", e.Type, strings.Join(Types, ", "))
	}
	if e.Type == TypeEnum && len(e.Options) == 0 {
		return e, errors.New("an enum field needs options")
	}
	if e.Type != TypeEnum {
		e.Options = nil
	}
	if strings.TrimSpace(e.Label) == "" {
		e.Label = name
	}
	e.Path, e.Source = NSCustom+"."+name, "custom"
	e.Filterable, e.Editable = true, true
	return e, nil
}

// Coerce checks a value against a field's type and returns its stored form: numbers as float64,
// bools as bool, everything else as a trimmed string. An empty string clears any type.
func Coerce(e Entry, v any) (any, error) {
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return zero(e.Type), nil
	}
	switch e.Type {
	case TypeNumber:
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		}
		return nil, fmt.Errorf("%s is a number, got %v", e.Path, v)
	case TypeBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("%s is true or false, got %v", e.Path, v)
	case TypeEnum:
		s, ok := v.(string)
		if ok {
			for _, o := range e.Options {
				if o == s {
					return s, nil
				}
			}
		}
		return nil, fmt.Errorf("%s must be one of %s, got %v", e.Path, strings.Join(e.Options, ", "), v)
	default:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s is %s, so the value must be a string, got %v", e.Path, e.Type, v)
		}
		return strings.TrimSpace(s), nil
	}
}

func zero(t string) any {
	switch t {
	case TypeNumber:
		return float64(0)
	case TypeBool:
		return false
	}
	return ""
}
