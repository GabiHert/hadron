package themes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store keeps custom themes as one JSON file each in Dir ($HADRON_HOME/themes).
type Store struct{ Dir string }

// ErrNotFound is returned for an unknown theme id.
var ErrNotFound = errors.New("no such theme")

func builtin(id string) (Theme, bool) {
	for _, t := range Builtins {
		if t.ID == id {
			return clone(t), true
		}
	}
	return Theme{}, false
}

func clone(t Theme) Theme {
	m := make(map[string]string, len(t.Tokens))
	for k, v := range t.Tokens {
		m[k] = v
	}
	t.Tokens = m
	return t
}

func (s Store) path(id string) string { return filepath.Join(s.Dir, id+".json") }

// List returns the built-in themes, then the custom ones by name.
func (s Store) List() ([]Theme, error) {
	out := make([]Theme, 0, len(Builtins))
	for _, t := range Builtins {
		out = append(out, clone(t))
	}
	if s.Dir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var custom []Theme
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !idRe.MatchString(id) {
			continue
		}
		t, err := s.read(id)
		if err != nil {
			continue // a broken file must not hide the other themes
		}
		custom = append(custom, t)
	}
	sort.Slice(custom, func(i, j int) bool { return strings.ToLower(custom[i].Name) < strings.ToLower(custom[j].Name) })
	return append(out, custom...), nil
}

func (s Store) read(id string) (Theme, error) {
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		return Theme{}, err
	}
	t.ID, t.Builtin = id, false
	if err := Validate(&t); err != nil {
		return Theme{}, err
	}
	return t, nil
}

// Get returns one theme, built-in or custom.
func (s Store) Get(id string) (Theme, error) {
	if t, ok := builtin(id); ok {
		return t, nil
	}
	if !idRe.MatchString(id) || s.Dir == "" {
		return Theme{}, fmt.Errorf("%w %q", ErrNotFound, id)
	}
	t, err := s.read(id)
	if errors.Is(err, os.ErrNotExist) {
		return Theme{}, fmt.Errorf("%w %q", ErrNotFound, id)
	}
	return t, err
}

// Save writes a custom theme. With an empty ID it gets a new one from its name. A built-in theme
// can't be changed: duplicate it instead.
func (s Store) Save(t Theme) (Theme, error) {
	if s.Dir == "" {
		return Theme{}, errors.New("themes need a data folder")
	}
	if _, ok := builtin(t.ID); ok {
		return Theme{}, fmt.Errorf("%s is a built-in theme and can't be changed; save a copy under a new name", t.ID)
	}
	if err := Validate(&t); err != nil {
		return Theme{}, err
	}
	if t.ID == "" {
		base := Slug(t.Name)
		t.ID = base
		for i := 2; ; i++ {
			_, isBuiltin := builtin(t.ID)
			_, err := os.Stat(s.path(t.ID))
			if !isBuiltin && errors.Is(err, os.ErrNotExist) {
				break
			}
			t.ID = fmt.Sprintf("%s-%d", base, i)
		}
	} else if !idRe.MatchString(t.ID) {
		return Theme{}, fmt.Errorf("theme id %q must be lowercase letters, digits and dashes", t.ID)
	}
	t.Builtin = false
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Theme{}, err
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return Theme{}, err
	}
	tmp := s.path(t.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return Theme{}, err
	}
	return t, os.Rename(tmp, s.path(t.ID))
}

// Delete removes a custom theme.
func (s Store) Delete(id string) error {
	if _, ok := builtin(id); ok {
		return fmt.Errorf("%s is a built-in theme and can't be deleted", id)
	}
	if !idRe.MatchString(id) || s.Dir == "" {
		return fmt.Errorf("%w %q", ErrNotFound, id)
	}
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w %q", ErrNotFound, id)
	}
	return err
}
