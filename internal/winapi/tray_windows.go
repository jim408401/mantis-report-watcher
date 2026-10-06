//go:build windows

package winapi

import (
	"runtime"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	nifShowTip = 0x80
	niifUser   = 0x04
	niifLarge  = 0x20
	niifQuiet  = 0x80

	wmUser          = 0x0400
	wmTrayCallback  = WM_APP + 1
	wmShowRequest   = WM_APP + 2 // posted by a second instance
	wmLButtonDblClk = 0x0203
	wmContextMenu   = 0x007B
	ninSelect       = wmUser + 0
	ninKeySelect    = wmUser + 1
	ninBalloonClick = wmUser + 5
	wmCommand       = 0x0111
	wmDestroy       = 0x0002

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	cmdOpen    = 1
	cmdRefresh = 2
	cmdQuit    = 3
)

// TrayClass is the window class of the tray window (used to find a running instance).
const TrayClass = "MantisWatcherTrayWnd"

// ShowRequestMessage is posted to TrayClass by a second instance.
const ShowRequestMessage = wmShowRequest

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type msgT struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// TrayOptions configures the tray icon.
type TrayOptions struct {
	Icon, IconUnread []byte // .ico bytes
	Tooltip          string
	OnOpen           func()
	OnRefresh        func()
	OnQuit           func()
}

// Tray is a notification-area icon with a context menu and balloon notifications.
type Tray struct {
	opts       TrayOptions
	mu         sync.Mutex
	hwnd       uintptr
	iconNormal uintptr
	iconUnread uintptr
	iconLarge  uintptr
	unread     bool
	tip        string
	taskbarMsg uint32
	ready      chan struct{}
}

var activeTray *Tray

// StartTray creates the tray icon on a dedicated OS thread and returns once it exists.
func StartTray(opts TrayOptions) *Tray {
	t := &Tray{opts: opts, tip: opts.Tooltip, ready: make(chan struct{})}
	activeTray = t
	go t.loop()
	<-t.ready
	return t
}

func (t *Tray) loop() {
	runtime.LockOSThread()
	var hinst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinst)
	cls, _ := windows.UTF16PtrFromString(TrayClass)
	wc := wndClassEx{LpszClassName: cls, HInstance: hinst, LpfnWndProc: windows.NewCallback(trayWndProc)}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := windows.UTF16PtrFromString("Mantis Watcher")
	t.hwnd, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, uintptr(hinst), 0)
	tb, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tb)))
	t.taskbarMsg = uint32(r)

	small := SystemMetric(SM_CXSMICON)
	if small <= 0 {
		small = 16
	}
	t.iconNormal = IconFromICO(t.opts.Icon, small)
	t.iconUnread = IconFromICO(t.opts.IconUnread, small)
	t.iconLarge = IconFromICO(t.opts.Icon, SystemMetric(SM_CXICON)*2)
	t.add()
	close(t.ready)

	var m msgT
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *Tray) baseData() notifyIconData {
	var d notifyIconData
	d.CbSize = uint32(unsafe.Sizeof(d))
	d.HWnd = t.hwnd
	d.UID = 1
	return d
}

func copyUTF16(dst []uint16, s string) {
	u := utf16.Encode([]rune(s))
	if len(u) > len(dst)-1 {
		u = u[:len(dst)-1]
	}
	n := copy(dst, u)
	dst[n] = 0
}

func (t *Tray) currentIcon() uintptr {
	if t.unread && t.iconUnread != 0 {
		return t.iconUnread
	}
	return t.iconNormal
}

func (t *Tray) add() {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.baseData()
	d.UFlags = nifMessage | nifIcon | nifTip | nifShowTip
	d.UCallbackMessage = wmTrayCallback
	d.HIcon = t.currentIcon()
	copyUTF16(d.SzTip[:], t.tip)
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
	d.UVersion = 4 // NOTIFYICON_VERSION_4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&d)))
}

// SetStatus updates the tooltip and the unread badge.
func (t *Tray) SetStatus(tip string, unread bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tip == t.tip && unread == t.unread {
		return
	}
	t.tip, t.unread = tip, unread
	d := t.baseData()
	d.UFlags = nifIcon | nifTip | nifShowTip
	d.HIcon = t.currentIcon()
	copyUTF16(d.SzTip[:], tip)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

// Notify shows a Windows notification (balloon / toast). Clicking it opens the app.
func (t *Tray) Notify(title, body string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := t.baseData()
	d.UFlags = nifInfo
	copyUTF16(d.SzInfoTitle[:], title)
	copyUTF16(d.SzInfo[:], body)
	d.DwInfoFlags = niifUser | niifLarge | niifQuiet
	d.HBalloonIcon = t.iconLarge
	if d.HBalloonIcon == 0 {
		d.DwInfoFlags = 0x01 | niifQuiet // NIIF_INFO
	}
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

// Close removes the icon and stops the tray thread.
func (t *Tray) Close() {
	t.mu.Lock()
	d := t.baseData()
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	t.mu.Unlock()
	PostMessage(t.hwnd, WM_CLOSE, 0, 0)
}

func (t *Tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	add := func(id uintptr, text string) {
		p, _ := windows.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(p)))
	}
	add(cmdOpen, "開啟 Mantis 小幫手")
	add(cmdRefresh, "立即更新")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	add(cmdQuit, "結束")
	procSetMenuDefaultItem.Call(menu, cmdOpen, 0)
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmBottomAlign, uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, 0, 0, 0) // WM_NULL, per MSDN
	procDestroyMenu.Call(menu)
	switch cmd {
	case cmdOpen:
		t.fire(t.opts.OnOpen)
	case cmdRefresh:
		t.fire(t.opts.OnRefresh)
	case cmdQuit:
		t.fire(t.opts.OnQuit)
	}
}

func (t *Tray) fire(f func()) {
	if f != nil {
		go f()
	}
}

func trayWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	t := activeTray
	if t != nil && hwnd == t.hwnd {
		switch uint32(msg) {
		case wmTrayCallback:
			switch uint32(lp & 0xFFFF) {
			case ninSelect, ninKeySelect, wmLButtonDblClk, ninBalloonClick:
				t.fire(t.opts.OnOpen)
			case wmContextMenu:
				t.showMenu()
			}
			return 0
		case wmShowRequest:
			t.fire(t.opts.OnOpen)
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
		if t.taskbarMsg != 0 && uint32(msg) == t.taskbarMsg {
			t.add() // Explorer restarted
			return 0
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}
