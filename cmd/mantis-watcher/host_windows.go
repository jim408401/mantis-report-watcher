//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"

	"mantis-report-watcher/assets"
	"mantis-report-watcher/internal/app"
	"mantis-report-watcher/internal/mantis"
	"mantis-report-watcher/internal/ui"
	"mantis-report-watcher/internal/winapi"
)

const (
	appName      = "Mantis 小幫手"
	autostartKey = "MantisReportWatcher"
	mutexName    = `Local\MantisReportWatcher.SingleInstance`
)

// winPlatform implements app.Platform on Windows.
type winPlatform struct{ tray *winapi.Tray }

func (p *winPlatform) Protect(b []byte) ([]byte, error)   { return winapi.Protect(b) }
func (p *winPlatform) Unprotect(b []byte) ([]byte, error) { return winapi.Unprotect(b) }
func (p *winPlatform) Notify(title, body string) {
	if p.tray != nil {
		p.tray.Notify(title, body)
	}
}
func (p *winPlatform) OpenURL(u string) error  { return winapi.Open(u) }
func (p *winPlatform) OpenPath(s string) error { return winapi.Open(s) }
func (p *winPlatform) SetAutostart(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return winapi.SetAutostart(autostartKey, `"`+exe+`" --background`, on)
}
func (p *winPlatform) Autostart() bool { return winapi.Autostart(autostartKey) != "" }
func (p *winPlatform) SetTray(tip string, unread int) {
	if p.tray != nil {
		p.tray.SetStatus(tip, unread > 0)
	}
}

// host owns the WebView2 window.
type host struct {
	a         *app.App
	mu        sync.Mutex
	w         webview2.WebView
	hwnd      uintptr
	quitting  bool
	showReq   chan struct{}
	quitReq   chan struct{}
	hintShown bool
	darkSet   *bool
	plat      *winPlatform
}

func (h *host) requestShow() {
	h.mu.Lock()
	w, hwnd := h.w, h.hwnd
	h.mu.Unlock()
	if w == nil {
		select {
		case h.showReq <- struct{}{}:
		default:
		}
		return
	}
	w.Dispatch(func() {
		winapi.ShowAndFocus(hwnd)
		h.push()
	})
}

// HideWindow implements app.Shell.
func (h *host) HideWindow() {
	h.mu.Lock()
	w, hwnd := h.w, h.hwnd
	h.mu.Unlock()
	if w != nil {
		w.Dispatch(func() { winapi.Hide(hwnd) })
	}
}

// Quit implements app.Shell.
func (h *host) Quit() {
	h.mu.Lock()
	h.quitting = true
	w, hwnd := h.w, h.hwnd
	h.mu.Unlock()
	if w == nil {
		select {
		case h.quitReq <- struct{}{}:
		default:
		}
		return
	}
	winapi.PostMessage(hwnd, winapi.WM_CLOSE, 0, 0)
}

// push sends the current snapshot to the page.
func (h *host) push() {
	h.mu.Lock()
	w := h.w
	h.mu.Unlock()
	if w == nil {
		return
	}
	b, err := json.Marshal(h.a.Snapshot())
	if err != nil {
		return
	}
	js := "window.__mwState && window.__mwState(" + string(b) + ")"
	w.Dispatch(func() {
		w.Eval(js)
		h.mu.Lock()
		hwnd := h.hwnd
		h.mu.Unlock()
		if hwnd != 0 {
			h.applyTitleBar(hwnd)
		}
	})
}

// applyTitleBar matches the native title bar to the app theme (UI thread).
func (h *host) applyTitleBar(hwnd uintptr) {
	theme := h.a.Theme()
	dark := theme == "dark" || (theme != "light" && winapi.SystemUsesDarkTheme())
	if h.darkSet != nil && *h.darkSet == dark {
		return
	}
	h.darkSet = &dark
	winapi.SetDarkTitleBar(hwnd, dark)
}

func (h *host) runWindow() {
	if v, err := webviewloader.GetInstalledVersion(); err != nil || v == "" {
		if winapi.MessageBox(appName, "這台電腦缺少 Microsoft Edge WebView2 Runtime，無法顯示畫面。\n\n要開啟微軟官方下載頁面嗎？（安裝後重新開啟本程式即可）", winapi.MB_YESNO|winapi.MB_ICONWARNING) == winapi.IDYES {
			_ = winapi.Open("https://go.microsoft.com/fwlink/p/?LinkId=2124703")
		}
		return
	}
	scale := func(v int) int { return v * winapi.SystemDPI() / 96 }
	width, height := scale(1120), scale(800)
	if sw := winapi.SystemMetric(0); sw > 0 && width > sw-40 { // SM_CXSCREEN
		width = sw - 40
	}
	if sh := winapi.SystemMetric(1); sh > 0 && height > sh-80 {
		height = sh - 80
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     os.Getenv("MANTIS_WATCHER_DEBUG") != "",
		AutoFocus: true,
		DataPath:  filepath.Join(h.a.DataDir(), "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title:  appName,
			Width:  uint(width),
			Height: uint(height),
			Center: true,
		},
	})
	if w == nil {
		winapi.MessageBox(appName, "無法建立 WebView2 視窗。請確認已安裝 Microsoft Edge WebView2 Runtime。", winapi.MB_OK|winapi.MB_ICONERROR)
		return
	}
	hwnd := uintptr(w.Window())
	w.SetSize(scale(720), scale(520), webview2.HintMin)
	h.applyTitleBar(hwnd)

	big := winapi.IconFromICO(assets.AppIcon, winapi.SystemMetric(winapi.SM_CXICON))
	small := winapi.IconFromICO(assets.AppIcon, winapi.SystemMetric(winapi.SM_CXSMICON))
	winapi.SetWindowIcons(hwnd, small, big)

	// Closing the window keeps the app running in the tray (unless disabled).
	winapi.Subclass(hwnd, func(orig, hw uintptr, msg uint32, wp, lp uintptr) uintptr {
		if msg == winapi.WM_CLOSE {
			h.mu.Lock()
			quitting := h.quitting
			h.mu.Unlock()
			if !quitting && h.a.CloseToTray() {
				winapi.Hide(hw)
				if !h.hintShown {
					h.hintShown = true
					h.plat.Notify(appName+" 仍在背景執行", "會持續幫你檢查 Mantis。要完全結束，請在系統匣圖示按右鍵 →「結束」。")
				}
				return 0
			}
			h.mu.Lock()
			h.quitting = true
			h.mu.Unlock()
		}
		return winapi.CallWindowProc(orig, hw, msg, wp, lp)
	})

	_ = w.Bind("mwRpc", func(id int, method string, payload string) {
		go func() {
			res, err := h.a.Call(h, method, json.RawMessage(payload))
			var js string
			if err != nil {
				eb, _ := json.Marshal(err.Error())
				js = fmt.Sprintf("window.__mwResolve(%d,false,%s)", id, eb)
			} else {
				rb, _ := json.Marshal(res)
				js = fmt.Sprintf("window.__mwResolve(%d,true,%s)", id, rb)
			}
			w.Dispatch(func() { w.Eval(js) })
		}()
	})

	h.mu.Lock()
	h.w, h.hwnd = w, hwnd
	h.mu.Unlock()

	w.SetHtml(ui.IndexHTML)
	winapi.ShowAndFocus(hwnd)
	w.Run() // returns when the window is really destroyed

	h.mu.Lock()
	h.w, h.hwnd = nil, 0
	h.darkSet = nil
	h.mu.Unlock()
	winapi.DestroyIcon(big)
	winapi.DestroyIcon(small)
}

func run() {
	runtime.LockOSThread()
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("%v\n\n%s", r, debug.Stack())
			_ = os.MkdirAll(filepath.Join(dataDir(), "logs"), 0o700)
			_ = os.WriteFile(filepath.Join(dataDir(), "logs", "crash.log"), []byte(msg), 0o600)
			winapi.MessageBox(appName, "程式發生錯誤而結束，詳細資訊已寫入：\n"+filepath.Join(dataDir(), "logs", "crash.log"), winapi.MB_OK|winapi.MB_ICONERROR)
		}
	}()
	background := flag.Bool("background", false, "start minimized to the tray")
	flag.Parse()

	ok, mutex := winapi.AcquireSingleInstance(mutexName)
	if !ok {
		// Already running: ask that instance to show its window.
		winapi.PostToWindowClass(winapi.TrayClass, winapi.ShowRequestMessage)
		return
	}
	_ = mutex

	mantis.SystemProxy = winapi.SystemProxy
	plat := &winPlatform{}
	a := app.New(dataDir(), plat, version)
	h := &host{a: a, plat: plat, showReq: make(chan struct{}, 1), quitReq: make(chan struct{}, 1)}

	plat.tray = winapi.StartTray(winapi.TrayOptions{
		Icon:       assets.AppIcon,
		IconUnread: assets.AppIconUnread,
		Tooltip:    appName,
		OnOpen:     h.requestShow,
		OnRefresh:  a.Refresh,
		OnQuit:     h.Quit,
	})
	defer plat.tray.Close()

	// Keep the Run entry pointing at the current exe location if enabled.
	if cmd := winapi.Autostart(autostartKey); cmd != "" {
		if exe, err := os.Executable(); err == nil && !strings.Contains(strings.ToLower(cmd), strings.ToLower(exe)) {
			_ = plat.SetAutostart(true)
		}
	}

	a.OnChange(h.push)
	a.Start()
	defer a.Stop()

	if !*background {
		h.showReq <- struct{}{}
	}
	for {
		select {
		case <-h.showReq:
			h.runWindow()
			h.mu.Lock()
			q := h.quitting
			h.mu.Unlock()
			if q {
				return
			}
		case <-h.quitReq:
			return
		}
	}
}
