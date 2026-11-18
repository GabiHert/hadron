package quitguard

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
void hadronQuitGuardInstall(void);
int hadronSystemQuitting(void);
*/
import "C"

import "sync/atomic"

var active atomic.Pointer[Guard]

// Install hands ⌘Q to g: from now on the key goes to g.KeyDown and g.KeyUp before the menu.
func Install(g *Guard) {
	active.Store(g)
	C.hadronQuitGuardInstall()
}

// SystemQuitting reports that macOS is logging out, restarting or shutting down.
func SystemQuitting() bool { return C.hadronSystemQuitting() != 0 }

//export hadronQuitKeyDown
func hadronQuitKeyDown() C.int {
	if g := active.Load(); g != nil && g.KeyDown() {
		return 1
	}
	return 0
}

//export hadronQuitKeyUp
func hadronQuitKeyUp() {
	if g := active.Load(); g != nil {
		g.KeyUp()
	}
}
