//go:build darwin
// +build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UserNotifications -framework AppKit
#import <Cocoa/Cocoa.h>
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

        __block UNAuthorizationStatus status = UNAuthorizationStatusNotDetermined;
        dispatch_semaphore_t settingsSem = dispatch_semaphore_create(0);
        [center getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
            status = settings.authorizationStatus;
            dispatch_semaphore_signal(settingsSem);
        }];
        dispatch_semaphore_wait(settingsSem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        if (status == UNAuthorizationStatusNotDetermined) {
            [NSApplication sharedApplication];
            [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
            [NSApp activateIgnoringOtherApps:YES];

            __block BOOL granted = NO;
            dispatch_semaphore_t authSem = dispatch_semaphore_create(0);
            [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert|UNAuthorizationOptionSound) completionHandler:^(BOOL g, NSError * _Nullable error) {
                granted = g;
                dispatch_semaphore_signal(authSem);
            }];
            // wait up to 60 seconds for authorization callback
            dispatch_semaphore_wait(authSem, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC));
            if (granted) status = UNAuthorizationStatusAuthorized;
            else status = UNAuthorizationStatusDenied;
        }

        if (status == UNAuthorizationStatusAuthorized) {
            [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
            [NSApp hide:nil];
        }

        // If not authorized, fall back to NSUserNotification (legacy API) to try to show a basic notification
        if (status != UNAuthorizationStatusAuthorized) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
            NSUserNotification *note = [[NSUserNotification alloc] init];
            if (titleStr.length) note.title = titleStr;
            if (subtitleStr.length) note.subtitle = subtitleStr;
            if (bodyStr.length) note.informativeText = bodyStr;
            if (iconStr) {
                NSImage *img = [[NSImage alloc] initWithContentsOfFile:iconStr];
                if (img) note.contentImage = img;
            }
            [[NSUserNotificationCenter defaultUserNotificationCenter] deliverNotification:note];
            // give system a moment
            sleep(1);
#pragma clang diagnostic pop
            return;
        }

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
