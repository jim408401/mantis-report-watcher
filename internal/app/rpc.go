package app

import (
	"encoding/json"
	"fmt"
)

// Shell lets RPC calls reach window-level features (implemented by the GUI host).
type Shell interface {
	HideWindow()
	Quit()
}

// Call dispatches a UI request. It may block (network); hosts call it off the UI thread.
func (a *App) Call(shell Shell, method string, payload json.RawMessage) (any, error) {
	decode := func(v any) error {
		if len(payload) == 0 || string(payload) == "null" {
			return nil
		}
		return json.Unmarshal(payload, v)
	}
	switch method {
	case "getState":
		return a.Snapshot(), nil
	case "login":
		var req LoginRequest
		if err := decode(&req); err != nil {
			return nil, err
		}
		if ei := a.Login(req); ei != nil {
			return map[string]any{"ok": false, "error": ei}, nil
		}
		return map[string]any{"ok": true}, nil
	case "logout":
		a.Logout()
		return true, nil
	case "refresh":
		a.Refresh()
		return true, nil
	case "markRead":
		var ids []int
		if err := decode(&ids); err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return true, nil
		}
		a.MarkRead(ids)
		return true, nil
	case "markAllRead":
		a.MarkRead(nil)
		return true, nil
	case "saveSettings":
		var p SettingsPatch
		if err := decode(&p); err != nil {
			return nil, err
		}
		if ei := a.SaveSettings(p); ei != nil {
			return map[string]any{"ok": false, "error": ei}, nil
		}
		return map[string]any{"ok": true}, nil
	case "filters":
		fs, ei := a.Filters()
		if ei != nil {
			return map[string]any{"ok": false, "error": ei}, nil
		}
		return map[string]any{"ok": true, "filters": fs}, nil
	case "openUrl":
		var u string
		if err := decode(&u); err != nil {
			return nil, err
		}
		a.OpenURL(u)
		return true, nil
	case "openDataDir":
		a.OpenDataDir()
		return true, nil
	case "testNotification":
		a.TestNotification()
		return true, nil
	case "hideWindow":
		if shell != nil {
			shell.HideWindow()
		}
		return true, nil
	case "quit":
		if shell != nil {
			shell.Quit()
		}
		return true, nil
	}
	return nil, fmt.Errorf("unknown method %q", method)
}
