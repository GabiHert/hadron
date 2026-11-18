#import <AppKit/AppKit.h>
#include "_cgo_export.h"

static id hadronQuitMonitor;
static volatile int hadronPowerOff;

// isQuitKey says whether e is the Q key with ⌘ and no other modifier but Caps Lock.
static BOOL isQuitKey(NSEvent *e) {
	NSEventModifierFlags mods = e.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagControl |
		NSEventModifierFlagOption | NSEventModifierFlagShift);
	if (mods != NSEventModifierFlagCommand) return NO;
	return [[e.charactersIgnoringModifiers lowercaseString] isEqualToString:@"q"];
}

// hadronQuitGuardInstall watches the keys inside Hadron (a local monitor needs no permission) and
// takes ⌘Q before the menu sees it, while the guard wants it. It also notes macOS logging out or
// shutting down, when a quit must never wait.
void hadronQuitGuardInstall(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hadronQuitMonitor != nil) return;
		__block BOOL down = NO;
		NSEventMask mask = NSEventMaskKeyDown | NSEventMaskKeyUp | NSEventMaskFlagsChanged;
		hadronQuitMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *e) {
			switch (e.type) {
			case NSEventTypeKeyDown:
				if (isQuitKey(e) && hadronQuitKeyDown()) {
					down = YES;
					return nil;
				}
				break;
			case NSEventTypeKeyUp:
				if (down && [[e.charactersIgnoringModifiers lowercaseString] isEqualToString:@"q"]) {
					down = NO;
					hadronQuitKeyUp();
				}
				break;
			case NSEventTypeFlagsChanged:
				if (down && !(e.modifierFlags & NSEventModifierFlagCommand)) {
					down = NO;
					hadronQuitKeyUp();
				}
				break;
			default:
				break;
			}
			return e;
		}];
		[[[NSWorkspace sharedWorkspace] notificationCenter] addObserverForName:NSWorkspaceWillPowerOffNotification
			object:nil queue:nil usingBlock:^(NSNotification *n) { hadronPowerOff = 1; }];
		// Losing the focus mid-hold (⌘Tab) never delivers the key up: cancel then too.
		[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidResignActiveNotification
			object:nil queue:nil usingBlock:^(NSNotification *n) {
				if (down) {
					down = NO;
					hadronQuitKeyUp();
				}
			}];
	});
}

int hadronSystemQuitting(void) { return hadronPowerOff; }
