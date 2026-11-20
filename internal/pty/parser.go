package pty

import (
	"reflect"
	"unsafe"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// parserData is the buffer each terminal emulator's parser keeps for OSC, DCS and APC payloads
// (titles, hyperlinks, clipboard writes). vt allocates 4 MB for it up front, for every emulator:
// about 40 MB for ten sessions, and 4 MB more for every screen rebuilt with scrollback. Claude's
// payloads are a few hundred bytes; a longer one is cut short in this model of the screen only,
// never in the terminal the user sees (xterm.js parses the bytes itself).
const parserData = 64 << 10

// trimParser shrinks an emulator's parser buffer to parserData. vt does not export its parser, so
// this reaches the field by name; it reports false (and changes nothing) if a vt update renamed
// or retyped it, which TestEmulatorParserIsTrimmed catches.
func trimParser(emu *vt.Emulator) bool {
	f := reflect.ValueOf(emu).Elem().FieldByName("parser")
	if !f.IsValid() || f.Type() != reflect.TypeFor[*ansi.Parser]() {
		return false
	}
	p := *(**ansi.Parser)(unsafe.Pointer(f.UnsafeAddr()))
	if p == nil {
		return false
	}
	p.SetDataSize(parserData)
	return true
}

// newEmulator is vt.NewEmulator with the parser trimmed and the given scrollback.
func newEmulator(cols, rows, scrollback int) *vt.Emulator {
	emu := vt.NewEmulator(cols, rows)
	trimParser(emu)
	emu.SetScrollbackSize(scrollback)
	return emu
}
