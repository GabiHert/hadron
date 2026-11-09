package actions

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/actions.json")

// manifest is what the typed TS client (frontend/src/api.ts) is checked against. A Vitest test
// reads testdata/actions.json and fails if api.ts lacks a wrapper for any action in it.
type manifestEntry struct {
	Name    string  `json:"name"`
	Mutates bool    `json:"mutates"`
	Fields  []Field `json:"fields"`
}

func TestActionManifestMatchesGolden(t *testing.T) {
	h := newHarness(t)
	var entries []manifestEntry
	for _, info := range h.reg.Infos() {
		f := info.Fields
		for i := range f {
			f[i].Description = "" // descriptions change often and the client does not need them
		}
		entries = append(entries, manifestEntry{Name: info.Name, Mutates: info.Mutates, Fields: f})
	}
	got, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/actions.json"
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v. Run: go test ./internal/actions -run Manifest -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the registered actions differ from testdata/actions.json. Run: go test ./internal/actions -run Manifest -update, then add the missing wrappers to frontend/src/api.ts")
	}
}

func TestMCPToolsDocIsCurrent(t *testing.T) {
	r := NewRegistry(Deps{})
	RegisterAll(r)
	got := ToolsDoc(r)
	const path = "../../docs/mcp-tools.md"
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v. Run: go run ./cmd/toolsdoc", err)
	}
	if string(want) != got {
		t.Fatal("docs/mcp-tools.md is out of date. Run: go run ./cmd/toolsdoc")
	}
}
