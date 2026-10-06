package mantis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Client is an authenticated connection to Mantis.
type Client interface {
	// Mode reports the API flavour in use: "soap", "rest-token" or "rest-session".
	Mode() string
	Login(ctx context.Context) (User, error)
	// Issues returns issues for a scope: "assigned", "reported", "monitored"
	// or a numeric saved-filter ID.
	Issues(ctx context.Context, scope string) ([]Issue, error)
	Filters(ctx context.Context) ([]Filter, error)
}

// Connect validates the credentials and returns a working client.
//
// Strategy:
//   - API token given  -> REST API with the token.
//   - otherwise        -> SOAP API with username/password; if SOAP is
//     unavailable, REST API with the session obtained by logging in.
func Connect(ctx context.Context, cfg Config) (Client, User, error) {
	base, err := NormalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, User{}, err
	}
	cfg.BaseURL = base
	cfg.Token = strings.TrimSpace(cfg.Token)

	var candidates []Client
	switch {
	case cfg.Token != "":
		candidates = []Client{newRESTClient(cfg, base, true)}
	case cfg.Mode == "soap":
		candidates = []Client{newSOAPClient(cfg, base)}
	case cfg.Mode == "rest-session" || cfg.Mode == "rest":
		candidates = []Client{newRESTClient(cfg, base, false)}
	default:
		candidates = []Client{newSOAPClient(cfg, base), newRESTClient(cfg, base, false)}
	}
	if cfg.Token == "" && (strings.TrimSpace(cfg.Username) == "" || cfg.Password == "") {
		return nil, User{}, newErr(KindAuth, "請輸入帳號與密碼", nil)
	}

	var firstErr, authErr error
	for i, c := range candidates {
		u, err := c.Login(ctx)
		if err == nil {
			return c, u, nil
		}
		last := i == len(candidates)-1
		switch KindOf(err) {
		case KindNetwork, KindTLS:
			// A SOAP call can hang on misconfigured servers; still try REST.
			if last || !isTimeout(err) {
				return nil, User{}, withRoute(err, base)
			}
		case KindAuth:
			if authErr == nil {
				authErr = err
			}
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if authErr != nil {
		return nil, User{}, authErr
	}
	if k := KindOf(firstErr); k == KindUnavailable || k == KindNotMantis {
		return nil, User{}, newErr(KindNotMantis, "這個網址找不到 Mantis API，請確認網址（例如 http://伺服器/mantisbt）", firstErr)
	}
	return nil, User{}, firstErr
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded)
}

// withRoute adds "direct / via proxy" to network errors to ease diagnosis.
func withRoute(err error, base string) error {
	var me *Error
	if errors.As(err, &me) && (me.Kind == KindNetwork || me.Kind == KindTLS) {
		return &Error{Kind: me.Kind, Msg: me.Msg, Err: fmt.Errorf("%s；%v", RouteDescription(base), me.Err)}
	}
	return err
}
