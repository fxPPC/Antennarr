#import <AppKit/AppKit.h>
#include <stdlib.h>

extern int desktopState(void);
extern char *desktopDetail(void);
extern char *desktopViewers(void);
extern char *desktopCoverage(void);

static NSString *take(char *s) {
    NSString *text = [NSString stringWithUTF8String:s]; free(s); return text;
}

@interface AntennarrMenu : NSObject <NSApplicationDelegate, NSMenuDelegate>
@property NSStatusItem *item;
@property NSMenu *menu;
@property NSMenuItem *status, *viewers, *coverage, *settings, *admin, *login, *quit;
@property NSURL *url, *settingsURL, *folder, *agent;
@property BOOL opened, shownError, testing, settingsOpened, quitRequested;
@end

@implementation AntennarrMenu
- (void)openAdmin:(id)sender { [[NSWorkspace sharedWorkspace] openURL:self.url]; }
- (void)openSettings:(id)sender {
    if (self.testing) { self.settingsOpened = [self.settingsURL.fragment isEqual:@"admin"]; return; }
    [[NSWorkspace sharedWorkspace] openURL:self.settingsURL];
}
- (void)openFolder:(id)sender { [[NSWorkspace sharedWorkspace] openURL:self.folder]; }
- (BOOL)loginEnabled {
    NSDictionary *entry = [NSDictionary dictionaryWithContentsOfURL:self.agent];
    return [entry[@"Label"] isEqual:@"org.antennarr.desktop.beta"];
}
- (void)toggleLogin:(id)sender {
    NSFileManager *fm = [NSFileManager defaultManager];
    NSError *error = nil;
    if ([self loginEnabled]) { [fm removeItemAtURL:self.agent error:&error]; }
    else if ([fm fileExistsAtPath:self.agent.path]) {
        error = [NSError errorWithDomain:@"Antennarr" code:1 userInfo:@{NSLocalizedDescriptionKey:@"Another file already uses this login item name."}];
    } else if ([fm createDirectoryAtURL:self.agent.URLByDeletingLastPathComponent withIntermediateDirectories:YES attributes:nil error:&error]) {
        NSDictionary *entry = @{@"Label":@"org.antennarr.desktop.beta", @"ProgramArguments":@[@"/usr/bin/open", @"-g", NSBundle.mainBundle.bundlePath, @"--args", @"--login-item"], @"RunAtLoad":@YES};
        [entry writeToURL:self.agent error:&error];
    }
    if (error && !self.testing) { [[NSAlert alertWithError:error] runModal]; }
    self.login.state = [self loginEnabled] ? NSControlStateValueOn : NSControlStateValueOff;
}
- (void)quitApp:(id)sender {
    self.quitRequested = YES;
    if (!self.testing) {
        // Run after the native menu's tracking loop returns.
        dispatch_async(dispatch_get_main_queue(), ^{
            [NSApp stop:nil];
            [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined
                location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
                context:nil subtype:0 data1:0 data2:0] atStart:NO];
        });
    }
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    [self quitApp:nil]; return NSTerminateCancel;
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
    if (desktopState() == 1) [self openAdmin:nil]; return NO;
}
- (NSMenuItem *)add:(NSString *)title action:(SEL)action {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:@""];
    item.target = self; item.enabled = action != NULL; [self.menu addItem:item]; return item;
}
- (void)build {
    self.menu = [NSMenu new]; self.menu.autoenablesItems = NO; self.menu.delegate = self;
    self.status = [self add:@"Starting Antennarr…" action:NULL];
    self.viewers = [self add:@"" action:NULL]; self.coverage = [self add:@"" action:NULL];
    [self.menu addItem:NSMenuItem.separatorItem];
    self.admin = [self add:@"Open Antennarr" action:@selector(openAdmin:)];
    self.settings = [self add:@"Settings…" action:@selector(openSettings:)];
    [self add:@"Show Data Folder" action:@selector(openFolder:)];
    self.login = [self add:@"Start at Login" action:@selector(toggleLogin:)];
    [self.menu addItem:NSMenuItem.separatorItem];
    self.quit = [self add:@"Quit Antennarr" action:@selector(quitApp:)];
    [self menuNeedsUpdate:self.menu];
}
- (void)menuNeedsUpdate:(NSMenu *)menu {
    self.status.title = take(desktopDetail());
    self.viewers.title = take(desktopViewers()); self.coverage.title = take(desktopCoverage());
    self.admin.enabled = self.settings.enabled = desktopState() == 1;
    self.login.state = [self loginEnabled] ? NSControlStateValueOn : NSControlStateValueOff;
}
- (void)tick:(NSTimer *)timer {
    int state = desktopState();
    if (state == 1 && !self.opened) { self.opened = YES; [self openAdmin:nil]; }
    if (state == 2 && !self.shownError) {
        self.shownError = YES;
        NSAlert *alert = [NSAlert new]; alert.messageText = @"Antennarr stopped";
        alert.informativeText = take(desktopDetail()); [alert runModal];
    }
    if (state == 3) [self quitApp:nil];
}
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.opened = [NSProcessInfo.processInfo.arguments containsObject:@"--login-item"];
    [self build];
    self.item = [NSStatusBar.systemStatusBar statusItemWithLength:NSVariableStatusItemLength];
    NSImage *mark = [NSImage imageNamed:@"TrayIcon"];
    if (mark) { mark.size = NSMakeSize(18,18); self.item.button.image = mark; }
    else { self.item.button.title = @"Antennarr"; }
    self.item.button.toolTip = @"Antennarr";
    self.item.button.accessibilityLabel = @"Antennarr";
    self.item.menu = self.menu;
    [NSTimer scheduledTimerWithTimeInterval:0.4 target:self selector:@selector(tick:) userInfo:nil repeats:YES];
}
@end

// The smoke test drives these same NSMenuItem targets with a scratch plist;
// it neither launches a browser nor changes the user's login configuration.
int desktopMenu(const char *url, const char *settings, const char *folder, const char *scratch) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        AntennarrMenu *menu = [AntennarrMenu new];
        menu.url = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
        menu.settingsURL = [NSURL URLWithString:[NSString stringWithUTF8String:settings]];
        menu.folder = [NSURL fileURLWithPath:[NSString stringWithUTF8String:folder]];
        menu.testing = scratch != NULL;
        NSString *base = menu.testing ? [NSString stringWithUTF8String:scratch] : [NSHomeDirectory() stringByAppendingPathComponent:@"Library/LaunchAgents"];
        menu.agent = [NSURL fileURLWithPath:[base stringByAppendingPathComponent:@"org.antennarr.desktop.beta.plist"]];
        if (menu.testing) {
            [menu build];
            if (!menu.admin.enabled || !menu.settings.enabled || !menu.viewers.title.length || !menu.coverage.title.length) return 0;
            [menu.menu performActionForItemAtIndex:[menu.menu indexOfItem:menu.settings]];
            [menu.menu performActionForItemAtIndex:[menu.menu indexOfItem:menu.login]];
            BOOL enabled = menu.login.state == NSControlStateValueOn && [menu loginEnabled];
            [menu.menu performActionForItemAtIndex:[menu.menu indexOfItem:menu.login]];
            BOOL disabled = menu.login.state == NSControlStateValueOff && ![menu loginEnabled];
            [menu.menu performActionForItemAtIndex:[menu.menu indexOfItem:menu.quit]];
            return menu.settingsOpened && enabled && disabled && menu.quitRequested;
        }
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSApp.delegate = menu; [NSApp run]; NSApp.delegate = nil;
        return 1;
    }
}
void desktopAlert(const char *message) {
    @autoreleasepool {
        [NSApplication sharedApplication]; [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        [NSApp activateIgnoringOtherApps:YES];
        NSAlert *alert = [NSAlert new]; alert.messageText = @"Antennarr could not start";
        alert.informativeText = [NSString stringWithUTF8String:message]; [alert runModal];
    }
}
