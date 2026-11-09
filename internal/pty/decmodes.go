package pty

import (
	"sort"
	"strconv"
	"strings"
)

// Bracketed paste is DEC private mode 2004. The CLI switches it on once, at start-up.
const modeBracketedPaste = 2004

// maxTrackedModes caps the modes remembered, so junk that looks like mode sequences cannot grow the map.
const maxTrackedModes = 64

// bufferModes swap the normal and alternate screen. They are not replayed: a replay into the
// alternate buffer would leave the user nothing to scroll back through.
var bufferModes = map[int]bool{47: true, 1047: true, 1048: true, 1049: true}

// decModes follows the final on or off state of every DEC private mode the child sets, across
// chunks. Replaying a truncated scrollback loses the sequences that put the terminal in the state
// the child believes it is in (bracketed paste above all), so the ring's replay starts with
// preamble(). The scanner is adapted from quant's decmodes.go (MIT, Luis Brienze); see NOTICE.md.
type decModes struct {
	modes  map[int]bool
	state  int
	params []byte
}

const (
	decNormal = iota
	decEsc
	decCSI
	decParams
)

func newDecModes() *decModes { return &decModes{modes: map[int]bool{}} }

func (d *decModes) enabled(mode int) bool { return d.modes[mode] }

func (d *decModes) feed(chunk []byte) {
	for _, c := range chunk {
		switch d.state {
		case decNormal:
			if c == 0x1b {
				d.state = decEsc
			}
		case decEsc:
			switch c {
			case '[':
				d.state = decCSI
			case 0x1b:
			default:
				d.state = decNormal
			}
		case decCSI:
			switch c {
			case '?':
				d.state, d.params = decParams, d.params[:0]
			case 0x1b:
				d.state = decEsc
			default:
				d.state = decNormal
			}
		case decParams:
			switch {
			case (c >= '0' && c <= '9') || c == ';':
				if len(d.params) < 64 {
					d.params = append(d.params, c)
				}
			case c == 'h' || c == 'l':
				d.apply(c == 'h')
				d.state = decNormal
			case c == 0x1b:
				d.state = decEsc
			default:
				d.state = decNormal
			}
		}
	}
}

func (d *decModes) apply(on bool) {
	for _, f := range strings.Split(string(d.params), ";") {
		mode, err := strconv.Atoi(f)
		if err != nil || mode <= 0 {
			continue
		}
		if _, known := d.modes[mode]; !known && len(d.modes) >= maxTrackedModes {
			continue
		}
		d.modes[mode] = on
	}
}

// preamble returns the sequences that restore the final state of every tracked mode.
func (d *decModes) preamble() []byte {
	nums := make([]int, 0, len(d.modes))
	for m := range d.modes {
		if !bufferModes[m] {
			nums = append(nums, m)
		}
	}
	sort.Ints(nums)
	var b strings.Builder
	for _, m := range nums {
		b.WriteString("\x1b[?" + strconv.Itoa(m))
		if d.modes[m] {
			b.WriteString("h")
		} else {
			b.WriteString("l")
		}
	}
	return []byte(b.String())
}
