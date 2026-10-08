//go:build darwin && cgo

package app

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Cocoa -framework CoreFoundation

#import <Cocoa/Cocoa.h>
#import <CoreFoundation/CoreFoundation.h>
#import <stdlib.h>

void webui_tray_stop(void);
void webui_tray_run(const void *iconBytes, size_t iconLen);
*/
import "C"

import (
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type webUIRuntimeOptions struct {
	HideConsole bool
}

type webUITrayState struct {
	id   uint64
	url  string
	stop func()
}

var (
	trayStateMu  sync.Mutex
	trayState    *webUITrayState
	trayStateSeq uint64
	trayReadyMu  sync.Mutex
	trayReady    chan struct{}
)

func init() {
	runtime.LockOSThread()
}

func hideWebUIConsole() {}

func startWebUITray(rawURL string, stop func()) func() {
	done := make(chan struct{})
	ready := make(chan struct{})
	trayReadyMu.Lock()
	trayReady = ready
	trayReadyMu.Unlock()

	runWebUITray(rawURL, stop, ready, done)
	<-done
	return nil
}

func runWebUITray(rawURL string, stop func(), ready, done chan struct{}) {
	defer close(done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	once := sync.Once{}
	trayStop := func() {
		once.Do(func() {
			C.webui_tray_stop()
			if stop != nil {
				stop()
			}
		})
	}

	stateID := atomic.AddUint64(&trayStateSeq, 1)
	trayStateMu.Lock()
	trayState = &webUITrayState{id: stateID, url: rawURL, stop: trayStop}
	trayStateMu.Unlock()
	defer func() {
		trayStateMu.Lock()
		if trayState != nil && trayState.id == stateID {
			trayState = nil
		}
		trayStateMu.Unlock()
	}()

	var iconPtr unsafe.Pointer
	var iconLen C.size_t
	if logo, err := webUIAssets.ReadFile("webui/logo.png"); err == nil && len(logo) > 0 {
		iconPtr = C.CBytes(logo)
		iconLen = C.size_t(len(logo))
		defer C.free(iconPtr)
	}

	C.webui_tray_run(iconPtr, iconLen)
}

//export goWebUITrayReady
func goWebUITrayReady() {
	trayReadyMu.Lock()
	ready := trayReady
	trayReady = nil
	trayReadyMu.Unlock()
	if ready != nil {
		close(ready)
	}
}

//export goWebUITrayOpen
func goWebUITrayOpen() {
	openTrayWebUI()
}

//export goWebUITrayExit
func goWebUITrayExit() {
	stopTrayWebUI()
}

func openTrayWebUI() {
	openTrayWebUIPath("")
}

//export goWebUITrayPPT
func goWebUITrayPPT() {
	openTrayWebUIPath("/ppt.html")
}

func openTrayWebUIPath(path string) {
	trayStateMu.Lock()
	rawURL := ""
	if trayState != nil {
		rawURL = trayState.url
	}
	trayStateMu.Unlock()
	if rawURL != "" {
		if path != "" {
			parsed, err := url.Parse(rawURL)
			if err != nil {
				return
			}
			rawURL = parsed.ResolveReference(&url.URL{Path: path}).String()
		}
		_ = openBrowser(rawURL)
	}
}

func stopTrayWebUI() {
	trayStateMu.Lock()
	stop := func() {}
	if trayState != nil && trayState.stop != nil {
		stop = trayState.stop
	}
	trayStateMu.Unlock()
	stop()
}
