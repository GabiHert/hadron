// Package app wires Hadron together and opens the window.
package app

import (
	"context"
	"embed"
	"errors"
	"github.com/GabiHert/hadron/internal/dlog"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/GabiHert/hadron/internal/attachments"
	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/google/uuid"
	wailsv2 "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Run starts the app and blocks until the window closes.
//
// Only the primary instance touches the data folder. Run takes a lock on it first; a process that
// cannot (a second launch, or the binary `wails build` runs to generate bindings) opens no
// database, migrates nothing and reconciles no sessions, so it can never change the rows of the
// Hadron that is running. In that case Wails' own single-instance lock hands the launch to the
// running app and this process exits.
func Run(assets embed.FS) error {
	st, lock, err := prepare(bindingsMode, run.Home())
	if err != nil {
		return err
	}
	if st == nil {
		return runShell(assets)
	}
	defer lock.Close()
	a := New(st)
	// Anything the app prints with the standard logger is an error worth keeping.
	log.SetOutput(io.MultiWriter(os.Stderr, logWriter{a.log}))
	defer func() {
		if r := recover(); r != nil {
			a.log.Panic("main", r)
			panic(r)
		}
	}()
	return wailsv2.Run(appOptions(assets, a, buildMenu(a), true))
}

// prepare decides whether this process is the primary instance and, only then, opens (and
// migrates) the store. A nil store means a shell: the bindings generator, or a second launch.
// Neither touches the data folder: not the database, not the lock file, not a migration.
func prepare(bindings bool, home string) (*store.Store, *os.File, error) {
	if bindings {
		return nil, nil, nil
	}
	lock, err := lockPrimary(home)
	if errors.Is(err, errNotPrimary) {
		log.Printf("Hadron is already running on %s; handing over to it", home)
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	return st, lock, nil
}

// reconcile is the startup work on the database that only the primary instance does, before any
// session is restored: sessions left running by a previous run whose process is gone are marked
// exited (orphans that are still alive are stopped first), and launch profiles are backfilled.
func reconcile(st *store.Store, ops sessions.ProcOps, logf func(string, ...any)) {
	// An orphan of a crashed Hadron is stopped before anything is restored, so a conversation
	// never has two processes.
	sessions.ReapOrphans(st, ops, os.Getpid(), logf)
	if _, err := st.Sessions.MarkRunningExited(); err != nil {
		log.Printf("sessions: %v", err)
	}
	if err := board.BackfillLaunchProfiles(st); err != nil {
		log.Printf("launch profiles: %v", err)
	}
}

// runShell runs Wails with a bare App that has no store: for the bindings generator, and for a
// second launch, which Wails' single-instance lock hands over to the running app before any of
// this starts. If it ever did start, it quits at once.
func runShell(assets embed.FS) error {
	a := &App{}
	opts := appOptions(assets, a, nil, false)
	opts.OnStartup = func(ctx context.Context) { wruntime.Quit(ctx) }
	return wailsv2.Run(opts)
}

func appOptions(assets embed.FS, a *App, m *menu.Menu, primary bool) *options.App {
	o := &options.App{
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
		Menu: m,
		// ⌘W and the close button hide the app instead of quitting, so sessions keep running. ⌘Q quits.
		HideWindowOnClose: true,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID(),
			// A second launch only brings the running window forward; its process exits at once.
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { a.showWindow() },
		},
		// /attachments/ serves the thumbnails of attached images, from ~/.hadron/attachments only.
		AssetServer: &assetserver.Options{Assets: assets, Middleware: attachments.Middleware(run.Home())},
		// Files dropped on the window reach the page as absolute paths (runtime.OnFileDrop), which
		// the terminal types and the chat composer attaches. The page's own drag and drop still works.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Bind:        []interface{}{a},
	}
	if primary {
		o.OnStartup = func(ctx context.Context) { a.startup(ctx) }
		o.OnShutdown = func(ctx context.Context) { a.shutdown() }
	}
	return o
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
