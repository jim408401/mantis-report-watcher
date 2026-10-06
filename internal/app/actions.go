package app

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"mantis-report-watcher/internal/mantis"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Snapshot is everything the UI needs to render.
type Snapshot struct {
	View             string      `json:"view"`
	Version          string      `json:"version"`
	Settings         Settings    `json:"settings"`
	Autostart        bool        `json:"autostart"`
	HasSavedPassword bool        `json:"hasSavedPassword"`
	HasSavedToken    bool        `json:"hasSavedToken"`
	User             mantis.User `json:"user"`
	Mode             string      `json:"mode"`
	Groups           []Group     `json:"groups"`
	Issues           []IssueView `json:"issues"`
	SyncedAt         *time.Time  `json:"syncedAt,omitempty"`
	NextSync         *time.Time  `json:"nextSync,omitempty"`
	Syncing          bool        `json:"syncing"`
	Connecting       bool        `json:"connecting"`
	Error            *ErrInfo    `json:"error,omitempty"`
	LoginError       *ErrInfo    `json:"loginError,omitempty"`
	NewCount         int         `json:"newCount"`
	UpdatedCount     int         `json:"updatedCount"`
	DataDir          string      `json:"dataDir"`
}

// Snapshot returns the current state for the UI.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Snapshot{
		View:             a.view,
		Version:          a.Version,
		Settings:         a.settings,
		Autostart:        a.plat.Autostart(),
		HasSavedPassword: a.sec.Password != "",
		HasSavedToken:    a.sec.Token != "",
		User:             a.user,
		Groups:           groups,
		Syncing:          a.syncing,
		Connecting:       a.connecting,
		Error:            a.lastErr,
		LoginError:       a.loginErr,
		DataDir:          a.dir,
		Issues:           []IssueView{},
	}
	if a.client != nil {
		s.Mode = a.client.Mode()
	}
	if !a.syncedAt.IsZero() {
		t := a.syncedAt
		s.SyncedAt = &t
	}
	if !a.nextSync.IsZero() && a.view == "main" {
		t := a.nextSync
		s.NextSync = &t
	}
	base, _ := mantis.NormalizeBaseURL(a.settings.BaseURL)
	me := a.user.Name
	if me == "" {
		me = a.settings.Username
	}
	for _, is := range a.issues {
		v := buildView(base, is, a.state.Items[strconv.Itoa(is.ID)], me)
		if v.IsNew {
			s.NewCount++
		}
		if v.IsUpdated {
			s.UpdatedCount++
		}
		s.Issues = append(s.Issues, v)
	}
	sortViews(s.Issues)
	return s
}

// LoginRequest comes from the login form.
type LoginRequest struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
	UseToken bool   `json:"useToken"`
	Remember bool   `json:"remember"`
	Insecure bool   `json:"insecure"`
	AuthMode string `json:"authMode"`
}

// Login validates credentials against Mantis (blocking) and switches to the main view.
func (a *App) Login(req LoginRequest) *ErrInfo {
	a.mu.Lock()
	if req.Password == "" && strings.EqualFold(req.Username, a.settings.Username) && a.sec.Password != "" {
		req.Password = a.sec.Password // "keep saved password"
	}
	if req.UseToken && req.Token == "" && a.sec.Token != "" {
		req.Token = a.sec.Token
	}
	a.connecting = true
	a.loginErr = nil
	a.mu.Unlock()
	a.emit()

	base, err := mantis.NormalizeBaseURL(req.BaseURL)
	var client mantis.Client
	var user mantis.User
	if err == nil {
		cfg := mantis.Config{BaseURL: base, Username: strings.TrimSpace(req.Username), Password: req.Password,
			Insecure: req.Insecure, Mode: req.AuthMode, Timeout: 30 * time.Second}
		if req.UseToken {
			cfg.Token = req.Token
		}
		ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
		client, user, err = mantis.Connect(ctx, cfg)
		cancel()
	}

	a.mu.Lock()
	a.connecting = false
	if err != nil {
		a.loginErr = errInfo(err)
		a.logf("login failed for %s@%s: %v", req.Username, req.BaseURL, err)
		a.mu.Unlock()
		a.emit()
		return a.loginErr
	}
	a.session++
	a.settings.BaseURL = base
	if user.Name != "" {
		a.settings.Username = user.Name
	} else {
		a.settings.Username = strings.TrimSpace(req.Username)
	}
	a.settings.Remember = req.Remember
	a.settings.Insecure = req.Insecure
	a.settings.UseToken = req.UseToken
	if req.AuthMode != "" {
		a.settings.AuthMode = req.AuthMode
	}
	a.sec = secrets{Password: req.Password}
	if req.UseToken {
		a.sec.Token = req.Token
	}
	a.client = client
	a.user = user
	a.view = "main"
	a.lastErr = nil
	a.failures = 0
	a.nextSync = time.Time{}
	if a.state.Key != a.key() {
		a.issues = nil
		a.syncedAt = time.Time{}
	}
	a.saveSettingsLocked()
	a.saveSecretsLocked()
	a.logf("login ok: %s via %s", a.settings.Username, client.Mode())
	a.mu.Unlock()
	a.kick()
	a.emit()
	return nil
}

// Logout forgets the saved password and returns to the login screen.
func (a *App) Logout() {
	a.mu.Lock()
	a.session++
	a.client = nil
	a.sec = secrets{}
	a.view = "login"
	a.loginErr = nil
	a.lastErr = nil
	a.nextSync = time.Time{}
	a.saveSecretsLocked()
	a.saveSettingsLocked()
	a.mu.Unlock()
	a.emit()
}

// Refresh triggers an immediate sync.
func (a *App) Refresh() { a.kick() }

// MarkRead marks issues as read (clears NEW / updated badges).
func (a *App) MarkRead(ids []int) {
	a.mu.Lock()
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	all := len(ids) == 0
	for _, is := range a.issues {
		if all || want[is.ID] {
			id := strconv.Itoa(is.ID)
			st := a.state.Items[id]
			if st == nil {
				st = &itemState{FirstSeen: time.Now()}
				a.state.Items[id] = st
			}
			snapshotState(st, is)
		}
	}
	if err := writeJSON(a.path("state.json"), a.state); err != nil {
		a.logf("save state: %v", err)
	}
	a.mu.Unlock()
	a.emit()
}

// SettingsPatch contains user-editable settings (nil = unchanged).
type SettingsPatch struct {
	Scope         *string          `json:"scope"`
	ScopeName     *string          `json:"scopeName"`
	IntervalMin   *int             `json:"intervalMin"`
	NotifyNew     *bool            `json:"notifyNew"`
	NotifyUpdates *bool            `json:"notifyUpdates"`
	Theme         *string          `json:"theme"`
	CloseToTray   *bool            `json:"closeToTray"`
	SortMode      *string          `json:"sortMode"`
	Collapsed     *map[string]bool `json:"collapsed"`
	Autostart     *bool            `json:"autostart"`
}

// SaveSettings applies a patch.
func (a *App) SaveSettings(p SettingsPatch) *ErrInfo {
	var autostartErr error
	if p.Autostart != nil {
		autostartErr = a.plat.SetAutostart(*p.Autostart)
	}
	a.mu.Lock()
	resync := false
	if p.Scope != nil && *p.Scope != a.settings.Scope {
		a.settings.Scope = *p.Scope
		resync = true
	}
	if p.ScopeName != nil {
		a.settings.ScopeName = *p.ScopeName
	}
	if p.IntervalMin != nil && *p.IntervalMin >= 1 && *p.IntervalMin <= 1440 {
		if *p.IntervalMin != a.settings.IntervalMin && !a.syncedAt.IsZero() {
			a.nextSync = a.syncedAt.Add(time.Duration(*p.IntervalMin) * time.Minute)
		}
		a.settings.IntervalMin = *p.IntervalMin
	}
	if p.NotifyNew != nil {
		a.settings.NotifyNew = *p.NotifyNew
	}
	if p.NotifyUpdates != nil {
		a.settings.NotifyUpdates = *p.NotifyUpdates
	}
	if p.Theme != nil {
		a.settings.Theme = *p.Theme
	}
	if p.CloseToTray != nil {
		a.settings.CloseToTray = *p.CloseToTray
	}
	if p.SortMode != nil {
		a.settings.SortMode = *p.SortMode
	}
	if p.Collapsed != nil {
		a.settings.Collapsed = *p.Collapsed
	}
	a.saveSettingsLocked()
	a.mu.Unlock()
	if resync {
		a.kick()
	} else {
		a.kick() // re-evaluate timer
	}
	a.emit()
	if autostartErr != nil {
		return &ErrInfo{Kind: "server", Message: "無法設定開機自動啟動：" + autostartErr.Error(), At: time.Now()}
	}
	return nil
}

// Filters lists saved filters on the server.
func (a *App) Filters() ([]mantis.Filter, *ErrInfo) {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return nil, &ErrInfo{Kind: "auth", Message: "尚未連線", At: time.Now()}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	fs, err := c.Filters(ctx)
	if err != nil {
		return nil, errInfo(err)
	}
	return fs, nil
}

// CloseToTray reports whether closing the window should keep the app in the tray.
func (a *App) CloseToTray() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.CloseToTray
}

// Theme returns the configured theme (system | dark | light).
func (a *App) Theme() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.Theme
}

// OpenURL opens a link in the default browser (only http/https).
func (a *App) OpenURL(u string) {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		_ = a.plat.OpenURL(u)
	}
}

// OpenDataDir opens the data folder in Explorer.
func (a *App) OpenDataDir() { _ = a.plat.OpenPath(a.dir) }

// TestNotification shows a sample notification.
func (a *App) TestNotification() {
	a.plat.Notify("Mantis：1 筆新項目", "🆕 #9999 這是一則測試通知")
}

func (a *App) saveSettingsLocked() {
	if err := writeJSON(a.path("settings.json"), a.settings); err != nil {
		a.logf("save settings: %v", err)
	}
}

func (a *App) saveSecretsLocked() {
	p := a.path("credentials.bin")
	if !a.settings.Remember || (a.sec.Password == "" && a.sec.Token == "") {
		_ = os.Remove(p)
		return
	}
	b, _ := json.Marshal(a.sec)
	enc, err := a.plat.Protect(b)
	if err != nil {
		a.logf("protect credentials: %v", err)
		return
	}
	if err := writeFileAtomic(p, enc); err != nil {
		a.logf("save credentials: %v", err)
	}
}
