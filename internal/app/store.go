package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"mantis-report-watcher/internal/mantis"
)

// Settings are persisted in settings.json (no secrets).
type Settings struct {
	BaseURL       string          `json:"baseUrl"`
	Username      string          `json:"username"`
	Remember      bool            `json:"remember"`
	AuthMode      string          `json:"authMode"` // auto | soap | rest
	UseToken      bool            `json:"useToken"`
	Insecure      bool            `json:"insecure"`
	Scope         string          `json:"scope"`       // assigned | reported | monitored | <filter id>
	IntervalMin   int             `json:"intervalMin"` // polling interval
	NotifyNew     bool            `json:"notifyNew"`
	NotifyUpdates bool            `json:"notifyUpdates"`
	Theme         string          `json:"theme"` // system | dark | light
	CloseToTray   bool            `json:"closeToTray"`
	SortMode      string          `json:"sortMode"`
	ScopeName     string          `json:"scopeName,omitempty"`
	Collapsed     map[string]bool `json:"collapsed,omitempty"`
}

func defaultSettings() Settings {
	return Settings{
		AuthMode:      "auto",
		Scope:         mantis.ScopeAssigned,
		IntervalMin:   10,
		NotifyNew:     true,
		NotifyUpdates: true,
		Theme:         "system",
		CloseToTray:   true,
		SortMode:      "updated",
		Remember:      true,
	}
}

// secrets are stored encrypted (DPAPI on Windows) in credentials.bin.
type secrets struct {
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

// itemState is what we remember about an issue between runs.
type itemState struct {
	FirstSeen   time.Time `json:"firstSeen"`
	SeenUpdated time.Time `json:"seenUpdated"` // issue.Updated when the user last read it; zero = never read (new)
	SeenStatus  string    `json:"seenStatus,omitempty"`
	SeenNotes   int       `json:"seenNotes"`
	SeenTarget  string    `json:"seenTarget,omitempty"`
	NotifiedUpd time.Time `json:"notifiedUpdated"` // last issue.Updated we notified about
}

// persistedState is stored in state.json.
type persistedState struct {
	Key   string                `json:"key"` // server|user|scope the baseline belongs to
	Items map[string]*itemState `json:"items"`
}

// cache keeps the last successful fetch so the window opens instantly.
type cache struct {
	Key      string         `json:"key"`
	SyncedAt time.Time      `json:"syncedAt"`
	User     mantis.User    `json:"user"`
	Issues   []mantis.Issue `json:"issues"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
