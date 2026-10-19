//go:build windows

package app

import (
	"encoding/binary"
	"net/url"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type webUIRuntimeOptions struct {
	HideConsole bool
}

const (
	swHide = 0

	wmDestroy       = 0x0002
	wmCommand       = 0x0111
	wmTrayIcon      = 0x0400 + 1
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	trayID         = 1
	trayOpenID     = 1001
	trayExitID     = 1002
	trayPPTID      = 1003
	idiApplication = 32512
	iconVersion    = 0x00030000
)

var (
	runtimeKernel32         = syscall.NewLazyDLL("kernel32.dll")
	runtimeUser32           = syscall.NewLazyDLL("user32.dll")
	runtimeShell32          = syscall.NewLazyDLL("shell32.dll")
	procGetConsoleWindow    = runtimeKernel32.NewProc("GetConsoleWindow")
	procFreeConsole         = runtimeKernel32.NewProc("FreeConsole")
	procGetModuleHandleW    = runtimeKernel32.NewProc("GetModuleHandleW")
	procShowWindow          = runtimeUser32.NewProc("ShowWindow")
	procRegisterClassExW    = runtimeUser32.NewProc("RegisterClassExW")
	procCreateWindowExW     = runtimeUser32.NewProc("CreateWindowExW")
	procDefWindowProcW      = runtimeUser32.NewProc("DefWindowProcW")
	procDestroyWindow       = runtimeUser32.NewProc("DestroyWindow")
	procPostQuitMessage     = runtimeUser32.NewProc("PostQuitMessage")
	procGetMessageW         = runtimeUser32.NewProc("GetMessageW")
	procTranslateMessage    = runtimeUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = runtimeUser32.NewProc("DispatchMessageW")
	procLoadIconW           = runtimeUser32.NewProc("LoadIconW")
	procCreateIconResource  = runtimeUser32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon         = runtimeUser32.NewProc("DestroyIcon")
	procCreatePopupMenu     = runtimeUser32.NewProc("CreatePopupMenu")
	procAppendMenuW         = runtimeUser32.NewProc("AppendMenuW")
	procDestroyMenu         = runtimeUser32.NewProc("DestroyMenu")
	procGetCursorPos        = runtimeUser32.NewProc("GetCursorPos")
	procSetForegroundWindow = runtimeUser32.NewProc("SetForegroundWindow")
	procTrackPopupMenu      = runtimeUser32.NewProc("TrackPopupMenu")
	procShellNotifyIconW    = runtimeShell32.NewProc("Shell_NotifyIconW")
	trayStateMu             sync.Mutex
	trayState               *webUITrayState
)

type webUITrayState struct {
	hwnd uintptr
	url  string
	stop func()
}

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconDataW struct {
	Size            uint32
	HWnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        [16]byte
	BalloonIcon     uintptr
}

func hideWebUIConsole() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
		procFreeConsole.Call()
	}
}

func startWebUITray(rawURL string, stop func()) func() {
	done := make(chan struct{})
	ready := make(chan struct{})
	go runWebUITray(rawURL, stop, ready, done)
	<-ready
	return func() {
		trayStateMu.Lock()
		hwnd := uintptr(0)
		if trayState != nil {
			hwnd = trayState.hwnd
		}
		trayStateMu.Unlock()
		if hwnd != 0 {
			procDestroyWindow.Call(hwnd)
		}
		<-done
	}
}

func runWebUITray(rawURL string, stop func(), ready, done chan<- struct{}) {
	defer close(done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	instance, _, _ := procGetModuleHandleW.Call(0)
	className := syscall.StringToUTF16Ptr("KuaimaCLIWebUITray")
	wndProc := syscall.NewCallback(webUITrayWndProc)
	trayIcon := loadWebUIIcon()
	wc := wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		WndProc:   wndProc,
		Instance:  instance,
		Icon:      trayIcon,
		ClassName: className,
		IconSm:    trayIcon,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		close(ready)
		return
	}

	trayStateMu.Lock()
	trayState = &webUITrayState{hwnd: hwnd, url: rawURL, stop: stop}
	trayStateMu.Unlock()
	addTrayIcon(hwnd, trayIcon)
	close(ready)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	removeTrayIcon(hwnd)
	if trayIcon != 0 {
		procDestroyIcon.Call(trayIcon)
	}
}

func webUITrayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			openTrayWebUI()
			return 0
		case wmRButtonUp:
			showTrayMenu(hwnd)
			return 0
		}
	case wmCommand:
		switch uint32(wParam & 0xffff) {
		case trayOpenID:
			openTrayWebUI()
		case trayPPTID:
			openTrayWebUIPath("/ppt.html")
		case trayExitID:
			stopTrayWebUI()
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func addTrayIcon(hwnd, icon uintptr) {
	var data notifyIconDataW
	data.Size = uint32(unsafe.Sizeof(data))
	data.HWnd = hwnd
	data.ID = trayID
	data.Flags = nifMessage | nifIcon | nifTip
	data.CallbackMessage = wmTrayIcon
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, idiApplication)
	}
	data.Icon = icon
	copy(data.Tip[:], syscall.StringToUTF16("快马 AI WebUI"))
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))
}

func removeTrayIcon(hwnd uintptr) {
	var data notifyIconDataW
	data.Size = uint32(unsafe.Sizeof(data))
	data.HWnd = hwnd
	data.ID = trayID
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
}

func loadWebUIIcon() uintptr {
	ico, err := webUIAssets.ReadFile("webui/favicon.ico")
	if err != nil {
		return 0
	}
	image, width, height := bestICOImage(ico)
	if len(image) == 0 {
		return 0
	}
	icon, _, _ := procCreateIconResource.Call(
		uintptr(unsafe.Pointer(&image[0])),
		uintptr(len(image)),
		1,
		iconVersion,
		uintptr(width),
		uintptr(height),
		0,
	)
	return icon
}

func bestICOImage(ico []byte) ([]byte, uint32, uint32) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		return nil, 0, 0
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	bestOffset := uint32(0)
	bestSize := uint32(0)
	bestWidth := uint32(0)
	bestHeight := uint32(0)
	for i := 0; i < count; i++ {
		entry := 6 + i*16
		if entry+16 > len(ico) {
			break
		}
		width := uint32(ico[entry])
		height := uint32(ico[entry+1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		size := binary.LittleEndian.Uint32(ico[entry+8 : entry+12])
		offset := binary.LittleEndian.Uint32(ico[entry+12 : entry+16])
		if offset > uint32(len(ico)) || size > uint32(len(ico))-offset {
			continue
		}
		if bestSize == 0 || width*height > bestWidth*bestHeight {
			bestOffset = offset
			bestSize = size
			bestWidth = width
			bestHeight = height
		}
	}
	if bestSize == 0 {
		return nil, 0, 0
	}
	return ico[bestOffset : bestOffset+bestSize], bestWidth, bestHeight
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	openText := syscall.StringToUTF16Ptr("打开 WebUI")
	pptText := syscall.StringToUTF16Ptr("PPT 工作台")
	exitText := syscall.StringToUTF16Ptr("退出")
	procAppendMenuW.Call(menu, mfString, trayOpenID, uintptr(unsafe.Pointer(openText)))
	procAppendMenuW.Call(menu, mfString, trayPPTID, uintptr(unsafe.Pointer(pptText)))
	procAppendMenuW.Call(menu, mfString, trayExitID, uintptr(unsafe.Pointer(exitText)))
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightAlign|tpmBottomAlign|tpmRightButton|tpmReturnCmd, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	if cmd != 0 {
		webUITrayWndProc(hwnd, wmCommand, cmd, 0)
	}
}

func openTrayWebUI() {
	openTrayWebUIPath("")
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
