package mantis

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// SystemProxy, when set (Windows), returns the system (WinINET) proxy for a URL
// and whether the host is in the bypass list. Nil means "use environment only".
var SystemProxy func(u *url.URL) (*url.URL, bool)

// isIntranetHost reports hosts that should always be reached directly:
// private / loopback / link-local IPs and single-label names.
func isIntranetHost(host string) bool {
	h := strings.Trim(host, "[]")
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
	}
	return !strings.Contains(h, ".") || strings.HasSuffix(strings.ToLower(h), ".local")
}

// proxyFor decides how to reach a URL, mirroring what browsers do:
// intranet hosts go direct; otherwise HTTP(S)_PROXY env, then system proxy.
func proxyFor(req *http.Request) (*url.URL, error) {
	if isIntranetHost(req.URL.Hostname()) {
		return nil, nil
	}
	if p, err := http.ProxyFromEnvironment(req); p != nil || err != nil {
		return p, err
	}
	if SystemProxy != nil {
		if p, bypass := SystemProxy(req.URL); !bypass && p != nil {
			return p, nil
		}
	}
	return nil, nil
}

// RouteDescription explains how a URL will be reached (for diagnostics).
func RouteDescription(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p, _ := proxyFor(&http.Request{URL: u})
	if p == nil {
		return "直接連線"
	}
	return "經由 Proxy " + p.Host
}
