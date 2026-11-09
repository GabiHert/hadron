// Package app wires Hadron together and opens the window.
package app

import (
	"context"
	"embed"
	"github.com/GabiHert/hadron/internal/dlog"
	"io"
	"log"
	"os"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/google/uuid"
	wailsv2 "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// Run starts the app and blocks until the window closes.
func Run(assets embed.FS) error {
	if err := os.MkdirAll(run.Home(), 0o700); err != nil {
		return err
	}
	st, err := store.Open(run.DBPath())
	if err != nil {
		return err
	}
	if _, err := st.Sessions.MarkRunningExited(); err != nil {
		log.Printf("sessions: %v", err)
	}
	if err := board.BackfillLaunchProfiles(st); err != nil {
		log.Printf("launch profiles: %v", err)
	}
	a := New(st)
	// Anything the app prints with the standard logger is an error worth keeping.
	log.SetOutput(io.MultiWriter(os.Stderr, logWriter{a.log}))
	defer func() {
		if r := recover(); r != nil {
			a.log.Panic("main", r)
			panic(r)
		}
	}()
	return wailsv2.Run(&options.App{
		Title:            "Hadron",
		Width:            1360,
		Height:           860,
		MinWidth:         960,
		MinHeight:        620,
		BackgroundColour: &options.RGBA{R: 251, G: 251, B: 248, A: 255},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About:    &mac.AboutInfo{Title: "Hadron", Message: "Version " + run.Version},
		},
		Menu:               buildMenu(a),
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: instanceID()},
		AssetServer:        &assetserver.Options{Assets: assets},
		OnStartup:          func(ctx context.Context) { a.startup(ctx) },
		OnShutdown:         func(ctx context.Context) { a.shutdown() },
		Bind:               []interface{}{a},
	})
}

// EventWhatsNew asks the frontend to open the "What's new" window.
const EventWhatsNew = "menu:whatsnew"

// buildMenu is the standard Mac menu (the app menu, Edit and Window) plus Help, whose "What's New in
// Hadron" opens the changelog window.
func buildMenu(a *App) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	help := m.AddSubmenu("Help")
	help.AddText("What's New in Hadron", nil, func(*menu.CallbackData) { a.emit(EventWhatsNew, nil) })
	return m
}

// instanceID is the single-instance lock. A separate HADRON_HOME is a separate Hadron, so it gets
// its own lock and can run next to the installed app.
func instanceID() string {
	if h := os.Getenv("HADRON_HOME"); h != "" {
		return "tech.bhlabs.hadron." + uuid.NewSHA1(uuid.NameSpaceURL, []byte(h)).String()[:8]
	}
	return "tech.bhlabs.hadron"
}

// logWriter sends the standard logger's lines to Hadron's log as errors.
type logWriter struct{ l *dlog.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	w.l.Error(strings.TrimSpace(string(p)))
	return len(p), nil
}
