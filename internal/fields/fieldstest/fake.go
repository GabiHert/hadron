// Package fieldstest has a fake integration for tests of anything that reads or writes
// integration-contributed fields.
package fieldstest

import (
	"context"
	"fmt"
	"sync"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// Fake is an in-memory integration. By default it is "jira" with an editable enum status and a
// read-only text field summary.
type Fake struct {
	NS   string
	Defs []fields.Entry

	mu   sync.Mutex
	vals map[string]map[string]any // card id -> field -> value
	// Sets records every Set call, as "<card nick>:<field>=<value>".
	Sets []string
}

// NewJira returns a fake with the namespace "jira".
func NewJira() *Fake {
	return &Fake{NS: "jira", Defs: []fields.Entry{
		{Path: "jira.status", Label: "Status", Type: fields.TypeEnum, Options: []string{"To do", "In progress", "Finished"}, Filterable: true, Editable: true, CardChip: true, Source: "jira"},
		{Path: "jira.summary", Label: "Summary", Type: fields.TypeText, Filterable: true, Source: "jira"},
		{Path: "jira.points", Label: "Points", Type: fields.TypeNumber, Filterable: true, Editable: true, Source: "jira"},
	}}
}

// Namespace implements fields.Provider.
func (f *Fake) Namespace() string { return f.NS }

// Schema implements fields.Provider.
func (f *Fake) Schema(domain.Workspace) []fields.Entry { return f.Defs }

// Values implements fields.Provider.
func (f *Fake) Values(_ *store.Store, card domain.Card) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{}
	for k, v := range f.vals[card.ID] {
		out[k] = v
	}
	return out, nil
}

// Set implements fields.Provider.
func (f *Fake) Set(_ context.Context, _ *store.Store, card domain.Card, field string, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sets = append(f.Sets, fmt.Sprintf("%s:%s=%v", card.Nick, field, value))
	f.put(card.ID, field, value)
	return nil
}

// Put sets a value as if the remote item had changed, without recording a Set.
func (f *Fake) Put(cardID, field string, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.put(cardID, field, value)
}

func (f *Fake) put(cardID, field string, value any) {
	if f.vals == nil {
		f.vals = map[string]map[string]any{}
	}
	if f.vals[cardID] == nil {
		f.vals[cardID] = map[string]any{}
	}
	f.vals[cardID][field] = value
}
