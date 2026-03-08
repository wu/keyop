#import <Cocoa/Cocoa.h>
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>

int objc_main_disabled(int argc, char *argv[]) {
    @autoreleasepool {
        NSString *title = @"";
        NSString *subtitle = @"";
        NSString *body = @"";
        NSString *iconPath = @"";
        int delaySeconds = 1;

        for (int i = 1; i < argc; i++) {
            if (strcmp(argv[i], "--title") == 0 && i+1 < argc) { title = [NSString stringWithUTF8String:argv[++i]]; continue; }
            if (strcmp(argv[i], "--subtitle") == 0 && i+1 < argc) { subtitle = [NSString stringWithUTF8String:argv[++i]]; continue; }
            if (strcmp(argv[i], "--body") == 0 && i+1 < argc) { body = [NSString stringWithUTF8String:argv[++i]]; continue; }
            if (strcmp(argv[i], "--icon") == 0 && i+1 < argc) { iconPath = [NSString stringWithUTF8String:argv[++i]]; continue; }
            if (strcmp(argv[i], "--delay") == 0 && i+1 < argc) { delaySeconds = atoi(argv[++i]); continue; }
        }

        UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];

        __block UNAuthorizationStatus authStatus = UNAuthorizationStatusNotDetermined;
        dispatch_semaphore_t settingsSem = dispatch_semaphore_create(0);
        [center getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
            authStatus = settings.authorizationStatus;
            dispatch_semaphore_signal(settingsSem);
        }];
        dispatch_semaphore_wait(settingsSem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));

        if (authStatus == UNAuthorizationStatusNotDetermined) {
            // Temporarily activate app to show permission UI
            [NSApplication sharedApplication];
            [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
            [NSApp activateIgnoringOtherApps:YES];

            __block BOOL granted = NO;
            dispatch_semaphore_t authSem = dispatch_semaphore_create(0);
            [center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert|UNAuthorizationOptionSound) completionHandler:^(BOOL g, NSError * _Nullable error) {
                granted = g;
                if (error) {
                    NSLog(@"requestAuthorization error: %@", error);
                } else {
                    NSLog(@"requestAuthorization granted: %d", g);
                }
                dispatch_semaphore_signal(authSem);
            }];

            // Wait up to 60 seconds for user to respond to permissions dialog
            long authWait = dispatch_semaphore_wait(authSem, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC));
            if (authWait != 0) {
                NSLog(@"authorization wait timed out");
            }
            authStatus = granted ? UNAuthorizationStatusAuthorized : UNAuthorizationStatusDenied;
        }

        // If authorized, hide the app from Dock to allow banner display
        if (authStatus == UNAuthorizationStatusAuthorized) {
            [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
            [NSApp hide:nil];
        } else {
            NSLog(@"Notifications not authorized (status=%ld)", (long)authStatus);
        }

        // If not authorized, fall back to NSUserNotification (legacy API) to try to show a basic notification
        if (authStatus != UNAuthorizationStatusAuthorized) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
            NSUserNotification *note = [[NSUserNotification alloc] init];
            if (title.length > 0) note.title = title;
            if (subtitle.length > 0) note.subtitle = subtitle;
            if (body.length > 0) note.informativeText = body;
            if (iconPath.length > 0) {
                NSImage *img = [[NSImage alloc] initWithContentsOfFile:iconPath];
                if (img) note.contentImage = img;
            }
            [[NSUserNotificationCenter defaultUserNotificationCenter] deliverNotification:note];
            NSLog(@"Delivered NSUserNotification fallback");
            // Give system a moment to present the notification UI
            sleep(1);
#pragma clang diagnostic pop
            return 0;
        }

        UNMutableNotificationContent *content = [UNMutableNotificationContent new];
        if (title.length > 0) content.title = title;
        if (subtitle.length > 0) content.subtitle = subtitle;
        if (body.length > 0) content.body = body;

        if (iconPath.length > 0) {
            NSURL *url = [NSURL fileURLWithPath:iconPath];
            NSError *err = nil;
            UNNotificationAttachment *att = [UNNotificationAttachment attachmentWithIdentifier:@"img" URL:url options:nil error:&err];
            if (att) {
                content.attachments = @[att];
            } else {
                NSLog(@"attachment error: %@", err);
            }
        }

        NSTimeInterval delay = (delaySeconds > 0) ? delaySeconds : 1;
        UNTimeIntervalNotificationTrigger *trigger = [UNTimeIntervalNotificationTrigger triggerWithTimeInterval:delay repeats:NO];
        NSString *identifier = [[NSUUID UUID] UUIDString];
        UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:identifier content:content trigger:trigger];

        dispatch_semaphore_t addSem = dispatch_semaphore_create(0);
        [center addNotificationRequest:request withCompletionHandler:^(NSError * _Nullable error) {
            if (error) {
                NSLog(@"addNotificationRequest error: %@", error);
            } else {
                NSLog(@"notification scheduled");
            }
            dispatch_semaphore_signal(addSem);
        }];

        dispatch_semaphore_wait(addSem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC));
        // Give system a moment to present the notification UI
        sleep(1);
        return 0;
    }
}
