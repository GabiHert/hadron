package main

import (
	"embed"
	"log"

	"github.com/GabiHert/hadron/internal/app"
	"github.com/GabiHert/hadron/internal/changelog"
)

//go:embed all:frontend/dist
var assets embed.FS

// changelogMD is CHANGELOG.md, shown by "What's new" and read by changelog_get.
//
//go:embed CHANGELOG.md
var changelogMD string

func main() {
	changelog.SetEmbedded(changelogMD)
	if err := app.Run(assets); err != nil {
		log.Fatal(err)
	}
}
