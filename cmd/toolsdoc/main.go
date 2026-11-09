// Command toolsdoc writes docs/mcp-tools.md from the action registry.
//
//	go run ./cmd/toolsdoc            # rewrite docs/mcp-tools.md
//	go run ./cmd/toolsdoc -o -       # print to stdout
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/GabiHert/hadron/internal/actions"
)

func main() {
	out := flag.String("o", "docs/mcp-tools.md", "output file, or - for stdout")
	flag.Parse()
	r := actions.NewRegistry(actions.Deps{})
	actions.RegisterAll(r)
	doc := actions.ToolsDoc(r)
	if *out == "-" {
		fmt.Print(doc)
		return
	}
	if err := os.WriteFile(*out, []byte(doc), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d tools)\n", *out, r.Len())
}
