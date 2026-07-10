//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <CoreFoundation/CoreFoundation.h>
#import <stdlib.h>

extern void goWebUITrayReady(void);
extern void goWebUITrayOpen(void);
extern void goWebUITrayExit(void);

@interface KuaimaTrayTarget : NSObject
@end

@implementation KuaimaTrayTarget
- (void)openClicked:(id)sender {
	(void)sender;
	goWebUITrayOpen();
}

- (void)exitClicked:(id)sender {
	(void)sender;
	goWebUITrayExit();
}
@end

static CFRunLoopRef trayRunLoop = NULL;
static NSStatusItem *trayStatusItem = nil;
static KuaimaTrayTarget *trayTarget = nil;

static NSImage *loadTrayImage(const void *bytes, size_t length) {
	if (bytes == NULL || length == 0) {
		return nil;
	}
	NSData *data = [NSData dataWithBytes:bytes length:length];
	NSImage *image = [[NSImage alloc] initWithData:data];
	return image;
}

static NSImage *makeTrayFallbackImage(void) {
	NSImage *image = [[NSImage alloc] initWithSize:NSMakeSize(18.0, 18.0)];
	[image lockFocus];
	NSDictionary *attrs = @{
		NSFontAttributeName: [NSFont boldSystemFontOfSize:14.0],
		NSForegroundColorAttributeName: [NSColor blackColor],
	};
	NSString *title = @"K";
	NSSize textSize = [title sizeWithAttributes:attrs];
	NSPoint point = NSMakePoint((18.0 - textSize.width) / 2.0, (18.0 - textSize.height) / 2.0 - 1.0);
	[title drawAtPoint:point withAttributes:attrs];
	[image unlockFocus];
	image.template = YES;
	return image;
}

void webui_tray_stop(void) {
	[NSApp performSelectorOnMainThread:@selector(terminate:) withObject:nil waitUntilDone:NO];
	if (trayRunLoop != NULL) {
		CFRunLoopStop(trayRunLoop);
	}
}

void webui_tray_run(const void *iconBytes, size_t iconLen) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

		trayRunLoop = CFRunLoopGetCurrent();
		if (trayRunLoop != NULL) {
			CFRetain(trayRunLoop);
		}

		trayTarget = [KuaimaTrayTarget new];
		trayStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
		if (trayStatusItem != nil) {
			NSStatusBarButton *button = trayStatusItem.button;
			NSImage *image = loadTrayImage(iconBytes, iconLen);
			BOOL isFallbackImage = NO;
			if (image == nil) {
				image = makeTrayFallbackImage();
				isFallbackImage = YES;
			}
			if (button != nil && image != nil) {
				image.template = isFallbackImage;
				image.size = NSMakeSize(18.0, 18.0);
				button.image = image;
				button.imageScaling = NSImageScaleProportionallyDown;
				button.imagePosition = NSImageOnly;
				button.toolTip = @"快马 AI WebUI";
			}

			NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Kuaima CLI"];
			NSMenuItem *openItem = [[NSMenuItem alloc] initWithTitle:@"打开 WebUI" action:@selector(openClicked:) keyEquivalent:@""];
			[openItem setTarget:trayTarget];
			[menu addItem:openItem];
			[menu addItem:[NSMenuItem separatorItem]];
			NSMenuItem *exitItem = [[NSMenuItem alloc] initWithTitle:@"退出" action:@selector(exitClicked:) keyEquivalent:@""];
			[exitItem setTarget:trayTarget];
			[menu addItem:exitItem];
			trayStatusItem.menu = menu;
		}

		goWebUITrayReady();
		[NSApp run];

		if (trayStatusItem != nil) {
			[[NSStatusBar systemStatusBar] removeStatusItem:trayStatusItem];
			trayStatusItem = nil;
		}
		trayTarget = nil;
		if (trayRunLoop != NULL) {
			CFRelease(trayRunLoop);
			trayRunLoop = NULL;
		}
	}
}
