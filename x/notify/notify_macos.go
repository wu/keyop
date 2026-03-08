//go:build darwin
// +build darwin

package notify

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>
#include <stdlib.h>

void sendNotification(const char* title, const char* subtitle, const char* body, const char* iconPath, int delaySeconds) {
    @autoreleasepool {
        NSString *titleStr = title ? [NSString stringWithUTF8String:title] : @"";
        NSString *subtitleStr = subtitle ? [NSString stringWithUTF8String:subtitle] : @"";
        NSString *bodyStr = body ? [NSString stringWithUTF8String:body] : @"";
        NSString *iconStr = iconPath ? [NSString stringWithUTF8String:iconPath] : nil;

        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
        dispatch_semaphore_t sema = dispatch_semaphore_create(0);
        [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert|UNAuthorizationOptionSound) completionHandler:^(BOOL granted, NSError * _Nullable error) {
            dispatch_semaphore_signal(sema);
        }];
        // wait up to 5 seconds for authorization callback
        dispatch_semaphore_wait(sema, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        UNMutableNotificationContent *content = [UNMutableNotificationContent new];
        if (titleStr.length) content.title = titleStr;
        if (subtitleStr.length) content.subtitle = subtitleStr;
        if (bodyStr.length) content.body = bodyStr;

        if (iconStr) {
            NSURL *url = [NSURL fileURLWithPath:iconStr];
            NSError *err = nil;
            UNNotificationAttachment *attach = [UNNotificationAttachment attachmentWithIdentifier:@"icon" URL:url options:nil error:&err];
            if (attach) {
                content.attachments = @[attach];
            }
        }

        NSTimeInterval delay = (delaySeconds > 0) ? delaySeconds : 1;
        UNTimeIntervalNotificationTrigger *trigger = [UNTimeIntervalNotificationTrigger triggerWithTimeInterval:delay repeats:NO];
        NSString *identifier = [[NSUUID UUID] UUIDString];
        UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:identifier content:content trigger:trigger];

        [center addNotificationRequest:request withCompletionHandler:^(NSError * _Nullable error) {
            // no-op: completion handler intentionally ignored
        }];
    }
}
*/
import "C"
import "unsafe"

// SendNativeNotification sends a macOS native user notification using the UserNotifications
// framework. It accepts a title, subtitle, body, and an optional path to an icon file to attach
// to the notification. delaySeconds sets when the notification will be delivered (1 second if
// <=0). This function is a best-effort wrapper and does not return detailed platform errors.
func SendNativeNotification(title, subtitle, body, iconPath string, delaySeconds int) error {
	var ct *C.char
	var cs *C.char
	var cb *C.char
	var ci *C.char

	if title != "" {
		ct = C.CString(title)
		defer C.free(unsafe.Pointer(ct))
	}
	if subtitle != "" {
		cs = C.CString(subtitle)
		defer C.free(unsafe.Pointer(cs))
	}
	if body != "" {
		cb = C.CString(body)
		defer C.free(unsafe.Pointer(cb))
	}
	if iconPath != "" {
		ci = C.CString(iconPath)
		defer C.free(unsafe.Pointer(ci))
	}

	C.sendNotification(ct, cs, cb, ci, C.int(delaySeconds))
	return nil
}
