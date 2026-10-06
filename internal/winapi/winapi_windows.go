//go:build windows

// Package winapi wraps the bits of Win32 the app needs: DPAPI, the shell,
// the Run registry key, a single-instance mutex, icons and message boxes.
package winapi

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW          = user32.NewProc("CallWindowProcW")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	dwmapi                       = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
)

// SystemDPI returns the system DPI (96 = 100%).
func SystemDPI() int {
	if procGetDpiForSystem.Find() == nil {
		if r, _, _ := procGetDpiForSystem.Call(); r > 0 {
			return int(r)
		}
	}
	return 96
}

// WindowDPI returns the DPI of the monitor hosting hwnd.
func WindowDPI(hwnd uintptr) int {
	if procGetDpiForWindow.Find() == nil {
		if r, _, _ := procGetDpiForWindow.Call(hwnd); r > 0 {
			return int(r)
		}
	}
	return SystemDPI()
}

// SetDarkTitleBar toggles the immersive dark title bar (Windows 10 1809+).
func SetDarkTitleBar(hwnd uintptr, dark bool) {
	if procDwmSetWindowAttribute.Find() != nil {
		return
	}
	var v int32
	if dark {
		v = 1
	}
	for _, attr := range []uintptr{20, 19} { // DWMWA_USE_IMMERSIVE_DARK_MODE (new, old)
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&v)), 4); r == 0 {
			return
		}
	}
}

// SystemUsesDarkTheme reads the "apps use light theme" preference.
func SystemUsesDarkTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// Window message / constant values used across the package.
const (
	WM_CLOSE     = 0x0010
	WM_SETICON   = 0x0080
	WM_APP       = 0x8000
	SW_HIDE      = 0
	SW_SHOW      = 5
	SW_RESTORE   = 9
	SM_CXICON    = 11
	SM_CXSMICON  = 49
	ICON_SMALL   = 0
	ICON_BIG     = 1
	GWLP_WNDPROC = ^uintptr(3) // -4

	MB_OK              = 0x0
	MB_YESNO           = 0x4
	MB_ICONERROR       = 0x10
	MB_ICONWARNING     = 0x30
	MB_ICONINFORMATION = 0x40
	IDYES              = 6
)

// ---------------- DPAPI ----------------

// Protect encrypts data for the current Windows user (DPAPI).
func Protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	desc, _ := windows.UTF16PtrFromString("MantisWatcher")
	if err := windows.CryptProtectData(&in, desc, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// Unprotect decrypts data produced by Protect.
func Unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// ---------------- shell ----------------

// Open opens a URL or path with its default handler.
func Open(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, t, nil, nil, windows.SW_SHOWNORMAL)
}

// MessageBox shows a native message box and returns the button pressed.
func MessageBox(title, text string, flags uint32) int {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(flags|0x00010000 /*MB_SETFOREGROUND*/))
	return int(r)
}

// ---------------- autostart (HKCU\...\Run) ----------------

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// SetAutostart adds or removes the Run entry for command.
func SetAutostart(name, command string, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue(name, command)
	}
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return err
	}
	return nil
}

// Autostart returns the registered command ("" when absent).
func Autostart(name string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}

// ---------------- single instance ----------------

// AcquireSingleInstance returns false when another instance already holds the mutex.
func AcquireSingleInstance(name string) (bool, windows.Handle) {
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.CreateMutex(nil, false, n)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return false, 0
	}
	return true, h
}

// PostToWindowClass posts msg to the first top-level window of className.
func PostToWindowClass(className string, msg uint32) bool {
	c, _ := windows.UTF16PtrFromString(className)
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(c)), 0)
	if hwnd == 0 {
		return false
	}
	procAllowSetForegroundWindow.Call(^uintptr(0)) // ASFW_ANY: let the running instance come to front
	r, _, _ := procPostMessageW.Call(hwnd, uintptr(msg), 0, 0)
	return r != 0
}

// ---------------- windows ----------------

// SystemMetric wraps GetSystemMetrics.
func SystemMetric(i int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(i))
	return int(r)
}

// ShowAndFocus restores a (possibly hidden/minimized) window and brings it to the front.
func ShowAndFocus(hwnd uintptr) {
	if r, _, _ := procIsIconic.Call(hwnd); r != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
	} else {
		procShowWindow.Call(hwnd, SW_SHOW)
	}
	fg, _, _ := procGetForegroundWindow.Call()
	cur, _, _ := procGetCurrentThreadId.Call()
	fgThread, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
	if fgThread != 0 && fgThread != cur {
		procAttachThreadInput.Call(cur, fgThread, 1)
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procAttachThreadInput.Call(cur, fgThread, 0)
	} else {
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
	}
}

// Hide hides a window.
func Hide(hwnd uintptr) { procShowWindow.Call(hwnd, SW_HIDE) }

// IsVisible reports window visibility.
func IsVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

// PostMessage posts a message to hwnd.
func PostMessage(hwnd uintptr, msg uint32, wp, lp uintptr) {
	procPostMessageW.Call(hwnd, uintptr(msg), wp, lp)
}

// SetWindowIcons sets the title bar / taskbar icons.
func SetWindowIcons(hwnd uintptr, small, big uintptr) {
	if small != 0 {
		procSendMessageW.Call(hwnd, WM_SETICON, ICON_SMALL, small)
	}
	if big != 0 {
		procSendMessageW.Call(hwnd, WM_SETICON, ICON_BIG, big)
	}
}

// Subclass replaces a window's WndProc. The callback receives the original
// procedure so it can chain with CallWindowProc.
func Subclass(hwnd uintptr, proc func(orig, hwnd uintptr, msg uint32, wp, lp uintptr) uintptr) {
	var orig uintptr
	cb := windows.NewCallback(func(h, m, wp, lp uintptr) uintptr {
		return proc(orig, h, uint32(m), wp, lp)
	})
	orig, _, _ = procSetWindowLongPtrW.Call(hwnd, GWLP_WNDPROC, cb)
}

// CallWindowProc chains to the original WndProc.
func CallWindowProc(orig, hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procCallWindowProcW.Call(orig, hwnd, uintptr(msg), wp, lp)
	return r
}

// ---------------- icons ----------------

// IconFromICO creates an HICON of the requested size from .ico file bytes,
// picking the closest image (PNG-compressed entries are supported).
func IconFromICO(ico []byte, size int) uintptr {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return 0
	}
	count := int(binary.LittleEndian.Uint16(ico[4:]))
	best, bestDiff := -1, 1<<30
	for i := 0; i < count; i++ {
		e := 6 + i*16
		if e+16 > len(ico) {
			break
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		diff := w - size
		if diff < 0 {
			diff = -diff*4 + 1 // prefer downscaling a larger image
		}
		if diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 {
		return 0
	}
	e := 6 + best*16
	n := binary.LittleEndian.Uint32(ico[e+8:])
	off := binary.LittleEndian.Uint32(ico[e+12:])
	if int(off+n) > len(ico) {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&ico[off])), uintptr(n), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return h
}

// DestroyIcon frees an HICON.
func DestroyIcon(h uintptr) {
	if h != 0 {
		procDestroyIcon.Call(h)
	}
}
