package notifications

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>

// hadronNotify posts a notification. It returns 1 without a bundle (go run, tests), where
// UNUserNotificationCenter is not available.
static int hadronNotify(const char *ident, const char *title, const char *subtitle, const char *body,
		const char *thread, int sound, const char *infoJSON) {
	@autoreleasepool {
		if ([NSBundle mainBundle].bundleIdentifier == nil) return 1;
		UNMutableNotificationContent *c = [[UNMutableNotificationContent alloc] init];
		c.title = [NSString stringWithUTF8String:title];
		c.subtitle = [NSString stringWithUTF8String:subtitle];
		c.body = [NSString stringWithUTF8String:body];
		c.threadIdentifier = [NSString stringWithUTF8String:thread];
		if (sound) c.sound = [UNNotificationSound defaultSound];
		NSData *json = [[NSString stringWithUTF8String:infoJSON] dataUsingEncoding:NSUTF8StringEncoding];
		NSDictionary *info = [NSJSONSerialization JSONObjectWithData:json options:0 error:nil];
		if ([info isKindOfClass:[NSDictionary class]]) c.userInfo = info;
		UNNotificationRequest *r = [UNNotificationRequest requestWithIdentifier:[NSString stringWithUTF8String:ident] content:c trigger:nil];
		[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:r withCompletionHandler:^(NSError *err) {
			if (err != nil) NSLog(@"hadron: notification: %@", err);
		}];
		return 0;
	}
}

// hadronNotifyStatus reads what macOS allows: -1 without a bundle, else the authorization status
// (0 not determined, 1 denied, 2 authorized, 3 provisional, 4 ephemeral) times 10 plus the alert
// style (0 none, 1 banner, 2 alert). It waits up to 3 seconds for the answer.
static int hadronNotifyStatus(void) {
	@autoreleasepool {
		if ([NSBundle mainBundle].bundleIdentifier == nil) return -1;
		__block int out = -1;
		dispatch_semaphore_t done = dispatch_semaphore_create(0);
		[[UNUserNotificationCenter currentNotificationCenter] getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *s) {
			out = (int)s.authorizationStatus * 10 + (int)s.alertStyle;
			dispatch_semaphore_signal(done);
		}];
		dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC));
		return out;
	}
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

// PostNative shows n through UNUserNotificationCenter. Without an app bundle it does nothing and
// says so.
func PostNative(n Note) error {
	info, _ := json.Marshal(n.UserInfo)
	if n.UserInfo == nil {
		info = []byte("{}")
	}
	args := []*C.char{C.CString(n.ID), C.CString(n.Title), C.CString(n.Subtitle), C.CString(n.Body), C.CString(n.Thread), C.CString(string(info))}
	defer func() {
		for _, a := range args {
			C.free(unsafe.Pointer(a))
		}
	}()
	sound := C.int(0)
	if n.Sound {
		sound = 1
	}
	if C.hadronNotify(args[0], args[1], args[2], args[3], args[4], sound, args[5]) != 0 {
		return errors.New("notifications need Hadron.app")
	}
	return nil
}

// NativeStatus says what macOS allows.
func NativeStatus() Status {
	v := int(C.hadronNotifyStatus())
	if v < 0 {
		return Status{Permission: PermUnavailable}
	}
	st := Status{AlertStyle: []string{"none", "banner", "alert"}[clamp(v%10, 0, 2)]}
	switch v / 10 {
	case 0:
		st.Permission = PermNotDetermined
	case 1:
		st.Permission = PermDenied
	default:
		st.Permission = PermAllowed
	}
	return st
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
