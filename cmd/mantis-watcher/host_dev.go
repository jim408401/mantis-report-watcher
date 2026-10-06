//go:build !windows

package main

// Development host for non-Windows systems: serves the UI over HTTP so it can
// be exercised in a normal browser. Not used in the Windows build.

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"mantis-report-watcher/internal/app"
	"mantis-report-watcher/internal/ui"
)

type devPlatform struct {
	mu        sync.Mutex
	autostart bool
	notes     []string
}

func (p *devPlatform) Protect(b []byte) ([]byte, error) {
	return []byte("DEV:" + base64.StdEncoding.EncodeToString(b)), nil
}
func (p *devPlatform) Unprotect(b []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(string(b), "DEV:"))
}
func (p *devPlatform) Notify(title, body string) {
	p.mu.Lock()
	p.notes = append(p.notes, title+"\n"+body)
	p.mu.Unlock()
	log.Printf("NOTIFY %s | %s", title, strings.ReplaceAll(body, "\n", " / "))
}
func (p *devPlatform) OpenURL(u string) error  { log.Printf("OPEN %s", u); return nil }
func (p *devPlatform) OpenPath(s string) error { log.Printf("OPEN PATH %s", s); return nil }
func (p *devPlatform) SetAutostart(on bool) error {
	p.mu.Lock()
	p.autostart = on
	p.mu.Unlock()
	return nil
}
func (p *devPlatform) Autostart() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.autostart
}
func (p *devPlatform) SetTray(tip string, unread int) {}

func run() {
	addr := flag.String("addr", "127.0.0.1:8765", "dev UI address")
	flag.Parse()
	plat := &devPlatform{}
	a := app.New(dataDir(), plat, version+"-dev")

	var subMu sync.Mutex
	subs := map[chan struct{}]bool{}
	a.OnChange(func() {
		subMu.Lock()
		for c := range subs {
			select {
			case c <- struct{}{}:
			default:
			}
		}
		subMu.Unlock()
	})
	a.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, ui.IndexHTML)
	})
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		res, err := a.Call(nil, strings.TrimPrefix(r.URL.Path, "/rpc/"), body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		c := make(chan struct{}, 1)
		subMu.Lock()
		subs[c] = true
		subMu.Unlock()
		defer func() { subMu.Lock(); delete(subs, c); subMu.Unlock() }()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-c:
				fmt.Fprint(w, "data: changed\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
		}
	})
	mux.HandleFunc("/dev/notifications", func(w http.ResponseWriter, r *http.Request) {
		plat.mu.Lock()
		defer plat.mu.Unlock()
		_ = json.NewEncoder(w).Encode(plat.notes)
	})
	log.Printf("dev UI on http://%s  (data: %s)", *addr, dataDir())
	log.Fatal(http.ListenAndServe(*addr, mux))
}
