//go:build windows

package winapi

import (
	"net"
	"net/url"
	"path"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// SystemProxy reads the per-user WinINET proxy settings (the ones browsers use)
// and returns the proxy for u, or bypass=true when u matches ProxyOverride.
// PAC scripts (AutoConfigURL) are not evaluated; such setups connect directly.
func SystemProxy(u *url.URL) (*url.URL, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, true
	}
	defer k.Close()
	if en, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || en == 0 {
		return nil, true
	}
	server, _, _ := k.GetStringValue("ProxyServer")
	override, _, _ := k.GetStringValue("ProxyOverride")
	host := strings.ToLower(u.Hostname())
	for _, pat := range strings.Split(override, ";") {
		pat = strings.ToLower(strings.TrimSpace(pat))
		if pat == "" {
			continue
		}
		if pat == "<local>" {
			if !strings.Contains(host, ".") {
				return nil, true
			}
			continue
		}
		if ok, _ := path.Match(pat, host); ok {
			return nil, true
		}
		if _, n, err := net.ParseCIDR(pat); err == nil {
			if ip := net.ParseIP(host); ip != nil && n.Contains(ip) {
				return nil, true
			}
		}
	}
	// "host:port" or "http=host:port;https=host:port"
	proxy := ""
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			if strings.EqualFold(kv[0], u.Scheme) {
				proxy = kv[1]
			}
		} else if proxy == "" {
			proxy = part
		}
	}
	if proxy == "" {
		return nil, true
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	p, err := url.Parse(proxy)
	if err != nil {
		return nil, true
	}
	return p, false
}
