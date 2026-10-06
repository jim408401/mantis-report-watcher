package mantis

import (
	"net/http"
	"net/url"
	"testing"
)

func TestProxyIntranetDirect(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.corp.example:8080")
	t.Setenv("HTTPS_PROXY", "http://proxy.corp.example:8080")
	for raw, wantDirect := range map[string]bool{
		"http://192.168.115.109/mantisbt/api/rest/users/me": true,
		"http://10.1.2.3/x":            true,
		"http://mantis/x":              true,
		"https://mantis.example.com/x": false,
	} {
		u, _ := url.Parse(raw)
		p, _ := proxyFor(&http.Request{URL: u})
		if (p == nil) != wantDirect {
			t.Errorf("%s: proxy=%v, want direct=%v", raw, p, wantDirect)
		}
	}
	if got := RouteDescription("http://192.168.115.109/mantisbt"); got != "直接連線" {
		t.Errorf("route %q", got)
	}
}
