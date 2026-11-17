package accessibility

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static int hadronReduceTransparency(void) {
	@autoreleasepool {
		return [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceTransparency] ? 1 : 0;
	}
}
*/
import "C"

// Read reads the display settings now.
func Read() Display {
	return Display{ReduceTransparency: C.hadronReduceTransparency() != 0}
}
