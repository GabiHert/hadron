// Package fields is the field schema registry and the CEL environment that filters and rules
// evaluate against. Three namespaces exist: card.* (native), custom.* (per workspace) and one per
// enabled integration, contributed by a Provider.
package fields

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// Field types.
const (
	TypeText   = "text"
	TypeNumber = "number"
	TypeEnum   = "enum"
	TypeUser   = "user"
	TypeDate   = "date"
	TypeBool   = "bool"
)

// Types lists every valid field type.
var Types = []string{TypeText, TypeNumber, TypeEnum, TypeUser, TypeDate, TypeBool}

// Namespaces of the built-in fields.
const (
	NSCard   = "card"
	NSCustom = "custom"
)

// Entry describes one field.
type Entry struct {
	Path       string   `json:"path"`
	Label      string   `json:"label"`
	Type       string   `json:"type"`
	Options    []string `json:"options,omitempty"`
	Filterable bool     `json:"filterable"`
	Editable   bool     `json:"editable"`
	CardChip   bool     `json:"card_chip"`
	// Source is "native", "custom" or the integration id.
	Source string `json:"source"`
}

// Namespace is the part of the path before the first dot.
func (e Entry) Namespace() string { ns, _ := SplitPath(e.Path); return ns }

// SplitPath splits "jira.status" into "jira" and "status".
func SplitPath(path string) (ns, name string) {
	ns, name, _ = strings.Cut(path, ".")
	return ns, name
}

// Provider contributes the fields of one integration. M6 registers one per integration; tests
// register a fake.
type Provider interface {
	// Namespace is the integration id, such as "jira".
	Namespace() string
	// Schema lists the integration's fields, with paths already prefixed by the namespace.
	Schema(ws domain.Workspace) []Entry
	// Values returns the card's current values, keyed by the bare field name (no namespace).
	Values(st *store.Store, card domain.Card) (map[string]any, error)
	// Set writes a field of the linked item (the integration's update op). A provider that cannot
	// write returns an error that says why.
	Set(ctx context.Context, st *store.Store, card domain.Card, field string, value any) error
}

// Registry holds the providers. The zero value is not usable; call NewRegistry.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty registry: native and custom fields only.
func NewRegistry() *Registry { return &Registry{providers: map[string]Provider{}} }

var nsRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Register adds or replaces the provider of a namespace. It rejects the reserved namespaces.
func (r *Registry) Register(p Provider) error {
	ns := p.Namespace()
	if !nsRe.MatchString(ns) {
		return fmt.Errorf("integration id %q must be lowercase letters, digits and underscores, starting with a letter", ns)
	}
	if ns == NSCard || ns == NSCustom {
		return fmt.Errorf("%q is reserved for the built-in fields", ns)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[ns] = p
	return nil
}

// Unregister removes a provider.
func (r *Registry) Unregister(ns string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, ns)
}

// Provider returns the provider of a namespace.
func (r *Registry) Provider(ns string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[ns]
	return p, ok
}

// enabled lists the providers a workspace has switched on, by namespace.
func (r *Registry) enabled(ws domain.Workspace) []Provider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Provider
	for _, id := range ws.Integrations {
		if p, ok := r.providers[id]; ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace() < out[j].Namespace() })
	return out
}

// Native is the schema of the card.* namespace.
func Native() []Entry {
	return []Entry{
		{Path: "card.column", Label: "Column", Type: TypeText, Filterable: true, Editable: true, Source: "native"},
		{Path: "card.assignee", Label: "Assignee", Type: TypeUser, Filterable: true, Editable: true, CardChip: true, Source: "native"},
		{Path: "card.liveness", Label: "Liveness", Type: TypeEnum, Options: []string{domain.LivenessWorking, domain.LivenessIdle, domain.LivenessWaiting, domain.LivenessEnded}, Filterable: true, Source: "native"},
		{Path: "card.key", Label: "Key", Type: TypeText, Filterable: true, Editable: true, CardChip: true, Source: "native"},
		{Path: "card.nick", Label: "Nickname", Type: TypeText, Filterable: true, Editable: true, Source: "native"},
		{Path: "card.stage", Label: "Stage", Type: TypeText, Filterable: true, Editable: true, CardChip: true, Source: "native"},
	}
}

// Schema lists every field of a workspace: native, custom and those of its enabled integrations.
func (r *Registry) Schema(st *store.Store, ws domain.Workspace) ([]Entry, error) {
	out := Native()
	custom, err := LoadCustom(st, ws.ID)
	if err != nil {
		return nil, err
	}
	out = append(out, custom...)
	for _, p := range r.enabled(ws) {
		out = append(out, p.Schema(ws)...)
	}
	return out, nil
}

// Find returns the entry of a path.
func Find(schema []Entry, path string) (Entry, bool) {
	for _, e := range schema {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

// ErrNotEnabled is returned for a field of an integration that is not enabled.
var ErrNotEnabled = errors.New("not available until the integration is enabled")

// NotEnabledError says which integration's field cannot be used yet.
func NotEnabledError(path string) error {
	ns, _ := SplitPath(path)
	return fmt.Errorf("field %q is not available until the %q integration is enabled for this workspace: %w", path, ns, ErrNotEnabled)
}
