// Package app contains the platform-independent application logic:
// settings, encrypted credentials, polling, new/updated detection and the
// snapshot the UI renders.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mantis-report-watcher/internal/mantis"
)

// Platform abstracts OS integration (implemented for Windows; a stub is
// used for development on other systems).
type Platform interface {
	Protect(b []byte) ([]byte, error)
	Unprotect(b []byte) ([]byte, error)
	Notify(title, body string)
	OpenURL(u string) error
	OpenPath(p string) error
	SetAutostart(on bool) error
	Autostart() bool
	SetTray(tooltip string, unread int)
}

// ErrInfo is an error prepared for the UI.
type ErrInfo struct {
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at"`
}

func errInfo(err error) *ErrInfo {
	if err == nil {
		return nil
	}
	ei := &ErrInfo{Kind: string(mantis.KindOf(err)), At: time.Now()}
	var me *mantis.Error
	if errors.As(err, &me) {
		ei.Message = me.Msg
		if me.Err != nil {
			ei.Detail = me.Err.Error()
		}
	} else {
		ei.Message = err.Error()
	}
	return ei
}

// App is the application core. All exported methods are safe for concurrent use.
type App struct {
	Version string

	dir    string
	plat   Platform
	logger *log.Logger

	mu         sync.Mutex
	settings   Settings
	sec        secrets
	client     mantis.Client
	user       mantis.User
	state      persistedState
	issues     []mantis.Issue
	syncedAt   time.Time
	nextSync   time.Time
	syncing    bool
	connecting bool
	failures   int
	lastErr    *ErrInfo
	loginErr   *ErrInfo
	view       string // "login" | "main"
	session    int

	onChange func()
	changed  chan struct{}
	wake     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

// New loads persisted data from dir.
func New(dir string, plat Platform, version string) *App {
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o700)
	a := &App{
		Version:  version,
		dir:      dir,
		plat:     plat,
		settings: defaultSettings(),
		changed:  make(chan struct{}, 1),
		wake:     make(chan struct{}, 1),
		view:     "login",
	}
	a.logger = log.New(openLog(filepath.Join(dir, "logs", "app.log")), "", log.LstdFlags)
	_ = readJSON(a.path("settings.json"), &a.settings)
	if a.settings.IntervalMin < 1 {
		a.settings.IntervalMin = 10
	}
	if b, err := os.ReadFile(a.path("credentials.bin")); err == nil {
		if plain, err := plat.Unprotect(b); err == nil {
			_ = jsonUnmarshal(plain, &a.sec)
		} else {
			a.logf("cannot decrypt saved credentials: %v", err)
		}
	}
	_ = readJSON(a.path("state.json"), &a.state)
	if a.state.Items == nil {
		a.state.Items = map[string]*itemState{}
	}
	var c cache
	if err := readJSON(a.path("cache.json"), &c); err == nil && c.Key == a.key() {
		a.issues, a.syncedAt, a.user = c.Issues, c.SyncedAt, c.User
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	return a
}

func (a *App) path(name string) string { return filepath.Join(a.dir, name) }

// DataDir returns the folder holding settings, state and logs.
func (a *App) DataDir() string { return a.dir }

func (a *App) logf(format string, args ...any) { a.logger.Printf(format, args...) }

func openLog(path string) io.Writer {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return io.Discard
	}
	return f
}

// key identifies the server/account/scope the remembered state belongs to.
func (a *App) key() string {
	return strings.ToLower(strings.TrimRight(a.settings.BaseURL, "/")) + "|" + strings.ToLower(a.settings.Username) + "|" + a.settings.Scope
}

// OnChange registers a callback invoked (from a goroutine) whenever the snapshot changes.
func (a *App) OnChange(f func()) {
	a.onChange = f
	go func() {
		for range a.changed {
			if a.onChange != nil {
				a.onChange()
			}
		}
	}()
}

func (a *App) emit() {
	select {
	case a.changed <- struct{}{}:
	default:
	}
	a.updateTray()
}

func (a *App) kick() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Start begins background work. If credentials are remembered it signs in automatically.
func (a *App) Start() {
	a.mu.Lock()
	if a.settings.BaseURL != "" && a.settings.Username != "" && a.settings.Remember && (a.sec.Password != "" || (a.settings.UseToken && a.sec.Token != "")) {
		a.view = "main"
	}
	a.mu.Unlock()
	a.logf("start version=%s view=%s", a.Version, a.view)
	go a.loop()
	if a.view == "main" {
		a.kick()
	}
	a.emit()
}

// Stop ends background work.
func (a *App) Stop() { a.cancel() }

func (a *App) loop() {
	for {
		a.mu.Lock()
		wait := time.Duration(-1)
		if a.view == "main" && !a.nextSync.IsZero() {
			wait = time.Until(a.nextSync)
			if wait < 0 {
				wait = 0
			}
		}
		a.mu.Unlock()
		var timer <-chan time.Time
		var t *time.Timer
		if wait >= 0 {
			t = time.NewTimer(wait)
			timer = t.C
		}
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
		case <-timer:
		}
		if t != nil {
			t.Stop()
		}
		a.syncOnce()
	}
}

func (a *App) mantisConfig() mantis.Config {
	cfg := mantis.Config{
		BaseURL:  a.settings.BaseURL,
		Username: a.settings.Username,
		Password: a.sec.Password,
		Insecure: a.settings.Insecure,
		Mode:     a.settings.AuthMode,
		Timeout:  45 * time.Second,
	}
	if a.settings.UseToken {
		cfg.Token = a.sec.Token
	}
	return cfg
}

func (a *App) syncOnce() {
	a.mu.Lock()
	if a.view != "main" || a.syncing {
		a.mu.Unlock()
		return
	}
	a.syncing = true
	client := a.client
	session := a.session
	cfg := a.mantisConfig()
	scope := a.settings.Scope
	a.mu.Unlock()
	a.emit()

	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()

	var user mantis.User
	var err error
	if client == nil {
		a.setConnecting(true)
		client, user, err = mantis.Connect(ctx, cfg)
		a.setConnecting(false)
	}
	var issues []mantis.Issue
	if err == nil {
		issues, err = client.Issues(ctx, scope)
	}

	a.mu.Lock()
	defer func() {
		a.syncing = false
		a.mu.Unlock()
		a.emit()
	}()
	if session != a.session {
		return // logged out / re-logged in meanwhile
	}
	interval := time.Duration(a.settings.IntervalMin) * time.Minute
	if err != nil {
		a.failures++
		a.lastErr = errInfo(err)
		a.logf("sync failed: %v", err)
		if mantis.KindOf(err) == mantis.KindAuth {
			// Password changed / token revoked: ask the user to sign in again.
			a.client = nil
			a.view = "login"
			a.loginErr = &ErrInfo{Kind: "auth", Message: "登入已失效，請重新輸入密碼", At: time.Now()}
			a.sec.Password = ""
			a.nextSync = time.Time{}
			go a.plat.Notify("Mantis 小幫手", "登入已失效，請開啟程式重新登入。")
			return
		}
		backoff := time.Duration(a.failures) * time.Minute
		if backoff > interval {
			backoff = interval
		}
		a.nextSync = time.Now().Add(backoff)
		return
	}
	if client != a.client {
		a.client = client
		if user.Name != "" {
			a.user = user
		}
		a.logf("connected via %s as %s", client.Mode(), a.user.Name)
	}
	a.failures = 0
	a.lastErr = nil
	a.applyIssues(issues)
	a.nextSync = time.Now().Add(interval)
}

func (a *App) setConnecting(v bool) {
	a.mu.Lock()
	a.connecting = v
	a.mu.Unlock()
	a.emit()
}

func snapshotState(st *itemState, is mantis.Issue) {
	st.SeenUpdated = is.Updated
	if st.SeenUpdated.IsZero() {
		st.SeenUpdated = time.Unix(1, 0)
	}
	st.SeenStatus = is.Status.Label
	st.SeenNotes = len(is.Notes)
	st.SeenTarget = is.TargetVersion
	if is.Updated.After(st.NotifiedUpd) {
		st.NotifiedUpd = is.Updated
	}
}

// applyIssues diffs a fresh fetch against remembered state (a.mu held).
func (a *App) applyIssues(issues []mantis.Issue) {
	now := time.Now()
	key := a.key()
	baseline := false
	if a.state.Key != key {
		a.state = persistedState{Key: key, Items: map[string]*itemState{}}
		baseline = true
	}
	me := a.user.Name
	if me == "" {
		me = a.settings.Username
	}
	var fresh, updated []mantis.Issue
	present := map[string]bool{}
	for _, is := range issues {
		id := strconv.Itoa(is.ID)
		present[id] = true
		st, ok := a.state.Items[id]
		if !ok {
			st = &itemState{FirstSeen: now}
			a.state.Items[id] = st
			if baseline {
				snapshotState(st, is)
			} else {
				st.NotifiedUpd = is.Updated
				fresh = append(fresh, is)
			}
			continue
		}
		if st.SeenUpdated.IsZero() || !is.Updated.After(st.SeenUpdated) {
			continue
		}
		if onlyMyOwnChange(is, st, me) {
			snapshotState(st, is)
			continue
		}
		if is.Updated.After(st.NotifiedUpd) {
			st.NotifiedUpd = is.Updated
			updated = append(updated, is)
		}
	}
	for id := range a.state.Items {
		if !present[id] {
			delete(a.state.Items, id)
		}
	}
	a.issues = issues
	a.syncedAt = now
	if err := writeJSON(a.path("state.json"), a.state); err != nil {
		a.logf("save state: %v", err)
	}
	if err := writeJSON(a.path("cache.json"), cache{Key: key, SyncedAt: now, User: a.user, Issues: issues}); err != nil {
		a.logf("save cache: %v", err)
	}
	a.logf("sync ok: %d issues, %d new, %d updated", len(issues), len(fresh), len(updated))
	a.notifyChanges(fresh, updated)
}

func (a *App) notifyChanges(fresh, updated []mantis.Issue) {
	if !a.settings.NotifyNew {
		fresh = nil
	}
	if !a.settings.NotifyUpdates {
		updated = nil
	}
	if len(fresh) == 0 && len(updated) == 0 {
		return
	}
	var title string
	switch {
	case len(fresh) > 0 && len(updated) > 0:
		title = fmt.Sprintf("Mantis：%d 筆新項目、%d 筆有更新", len(fresh), len(updated))
	case len(fresh) > 0:
		title = fmt.Sprintf("Mantis：%d 筆新項目", len(fresh))
	default:
		title = fmt.Sprintf("Mantis：%d 筆項目有更新", len(updated))
	}
	var lines []string
	me := a.settings.Username
	for _, is := range fresh {
		lines = append(lines, fmt.Sprintf("🆕 #%d %s", is.ID, is.Summary))
	}
	for _, is := range updated {
		st := a.state.Items[strconv.Itoa(is.ID)]
		reason := ""
		if st != nil {
			reason = "（" + strings.Join(changesSince(is, st, me), "、") + "）"
		}
		lines = append(lines, fmt.Sprintf("✏️ #%d %s%s", is.ID, is.Summary, reason))
	}
	if len(lines) > 4 {
		rest := len(lines) - 3
		lines = append(lines[:3], fmt.Sprintf("…還有 %d 筆", rest))
	}
	body := strings.Join(lines, "\n")
	go a.plat.Notify(title, body)
}

func (a *App) unreadLocked() (newCount, updCount int) {
	for _, is := range a.issues {
		st := a.state.Items[strconv.Itoa(is.ID)]
		if st == nil {
			continue
		}
		if st.SeenUpdated.IsZero() {
			newCount++
		} else if is.Updated.After(st.SeenUpdated) {
			updCount++
		}
	}
	return
}

func (a *App) updateTray() {
	a.mu.Lock()
	n, u := a.unreadLocked()
	total := len(a.issues)
	view := a.view
	errMsg := ""
	if a.lastErr != nil {
		errMsg = a.lastErr.Message
	}
	a.mu.Unlock()
	tip := "Mantis 小幫手"
	switch {
	case view == "login":
		tip += "\n尚未登入"
	case errMsg != "":
		tip += "\n⚠ " + errMsg
	default:
		tip += fmt.Sprintf("\n%d 筆項目", total)
		if n+u > 0 {
			tip += fmt.Sprintf("，%d 筆未讀", n+u)
		}
	}
	a.plat.SetTray(tip, n+u)
}
