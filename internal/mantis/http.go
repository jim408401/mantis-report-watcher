package mantis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// Config holds connection parameters.
type Config struct {
	BaseURL  string // e.g. http://server/mantisbt (any Mantis page URL is accepted)
	Username string
	Password string
	Token    string // optional REST API token
	Insecure bool   // skip TLS certificate verification
	Mode     string // "auto" (default), "soap", "rest"
	Timeout  time.Duration
}

// NormalizeBaseURL turns any Mantis URL (search.php?..., login_page.php,
// my_view_page.php, .../api/rest/...) into the installation root.
func NormalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", newErr(KindNotMantis, "請輸入 Mantis 網址", nil)
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", newErr(KindNotMantis, "Mantis 網址格式不正確", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", newErr(KindNotMantis, "Mantis 網址必須以 http:// 或 https:// 開頭", nil)
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	p := u.Path
	for _, marker := range []string{"/api/rest", "/api/soap"} {
		if i := strings.Index(p, marker); i >= 0 {
			p = p[:i]
		}
	}
	if strings.HasSuffix(strings.ToLower(p), ".php") {
		if i := strings.LastIndex(p, "/"); i >= 0 {
			p = p[:i]
		}
	}
	u.Path = strings.TrimRight(p, "/")
	u.RawPath = ""
	return u.String(), nil
}

func newHTTPClient(cfg Config) *http.Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = proxyFor
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit user opt-in for self-signed intranet servers
	}
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: timeout, Transport: tr, Jar: jar}
}

// classifyTransportError maps low level errors to friendly kinds.
func classifyTransportError(err error) *Error {
	if err == nil {
		return nil
	}
	var me *Error
	if errors.As(err, &me) {
		return me
	}
	var uaErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &uaErr) || errors.As(err, &hostErr) || errors.As(err, &certErr) {
		return newErr(KindTLS, "伺服器憑證無法驗證（可在進階選項勾選「略過憑證檢查」）", err)
	}
	if errors.Is(err, context.Canceled) {
		return newErr(KindNetwork, "已取消", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newErr(KindNetwork, "連線逾時，請確認網路或 VPN 是否已連上", err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return newErr(KindNetwork, "找不到伺服器，請確認網址或公司網路/VPN", err)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return newErr(KindNetwork, "無法連線到 Mantis 伺服器", err)
	}
	return newErr(KindNetwork, "連線失敗", err)
}

func readBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func httpStatusErr(resp *http.Response, body []byte) error {
	return fmt.Errorf("HTTP %d %s", resp.StatusCode, snippet(body))
}
