// Command mantis-watcher is a desktop helper that collects the Mantis issues
// assigned to you (via the Mantis SOAP/REST API) and notifies you of changes.
package main

import (
	"os"
	"path/filepath"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "2.0.2"

func dataDir() string {
	if d := os.Getenv("MANTIS_WATCHER_HOME"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil { // %LOCALAPPDATA% on Windows
		return filepath.Join(d, "MantisWatcher")
	}
	return filepath.Join(os.TempDir(), "MantisWatcher")
}

func main() { run() }
