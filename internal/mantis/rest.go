package mantis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// restClient uses the MantisBT REST API (api/rest). It authenticates either
// with an API token, or with the session cookie obtained by logging in with
// username/password (the same mechanism Mantis' own web UI uses for the API).
type restClient struct {
	cfg      Config
	base     string
	http     *http.Client
	useToken bool
	loggedIn bool
}

func newRESTClient(cfg Config, base string, useToken bool) *restClient {
	return &restClient{cfg: cfg, base: base, http: newHTTPClient(cfg), useToken: useToken}
}

func (c *restClient) Mode() string {
	if c.useToken {
		return "rest-token"
	}
	return "rest-session"
}

var loginTokenRe = regexp.MustCompile(`name=["']login_token["'][^>]*value=["']([^"']+)["']|value=["']([^"']+)["'][^>]*name=["']login_token["']`)

// sessionLogin performs Mantis' standard form login to obtain a session cookie.
func (c *restClient) sessionLogin(ctx context.Context) error {
	// The login form carries a CSRF token on newer versions.
	pageURL := c.base + "/login_password_page.php?username=" + url.QueryEscape(c.cfg.Username)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	page, _ := readBody(resp, 4<<20)
	if resp.StatusCode == http.StatusNotFound {
		return newErr(KindUnavailable, "找不到 Mantis 登入頁", httpStatusErr(resp, page))
	}
	form := url.Values{}
	form.Set("username", c.cfg.Username)
	form.Set("password", c.cfg.Password)
	form.Set("return", "index.php")
	if m := loginTokenRe.FindSubmatch(page); m != nil {
		tok := string(m[1])
		if tok == "" {
			tok = string(m[2])
		}
		form.Set("login_token", tok)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/login.php", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = c.http.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	body, _ := readBody(resp, 4<<20)
	if resp.StatusCode == http.StatusNotFound && resp.Request.URL.Path == strings.TrimRight(req.URL.Path, "/") {
		return newErr(KindUnavailable, "找不到 Mantis 登入頁", httpStatusErr(resp, body))
	}
	u, _ := url.Parse(c.base + "/")
	for _, ck := range c.http.Jar.Cookies(u) {
		if strings.Contains(strings.ToUpper(ck.Name), "STRING_COOKIE") && ck.Value != "" {
			c.loggedIn = true
			return nil
		}
	}
	return newErr(KindAuth, "帳號或密碼錯誤", nil)
}

func (c *restClient) get(ctx context.Context, path string, out any) error {
	hadSession := c.loggedIn
	err := c.getOnce(ctx, path, out)
	// Session cookies expire; log in again once and retry.
	if err != nil && !c.useToken && hadSession && KindOf(err) == KindAuth {
		c.loggedIn = false
		err = c.getOnce(ctx, path, out)
	}
	return err
}

func (c *restClient) getOnce(ctx context.Context, path string, out any) error {
	if !c.useToken && !c.loggedIn {
		if err := c.sessionLogin(ctx); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/rest"+path, nil)
	if err != nil {
		return newErr(KindNotMantis, "Mantis 網址格式不正確", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.useToken {
		req.Header.Set("Authorization", c.cfg.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	b, err := readBody(resp, 64<<20)
	if err != nil {
		return classifyTransportError(err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		c.loggedIn = false
		if c.useToken {
			return newErr(KindAuth, "API Token 無效或權限不足", httpStatusErr(resp, b))
		}
		return newErr(KindAuth, "登入已失效或權限不足", httpStatusErr(resp, b))
	case resp.StatusCode == http.StatusNotFound:
		return newErr(KindUnavailable, "此伺服器未開放 REST API", httpStatusErr(resp, b))
	case resp.StatusCode >= 400:
		return newErr(KindServer, "Mantis 伺服器錯誤", httpStatusErr(resp, b))
	}
	if err := json.Unmarshal(b, out); err != nil {
		return newErr(KindUnavailable, "REST 回應格式無法解析（網址可能不是 Mantis）", fmt.Errorf("%v: %s", err, snippet(b)))
	}
	return nil
}

func (c *restClient) Login(ctx context.Context) (User, error) {
	var m map[string]any
	if err := c.get(ctx, "/users/me", &m); err != nil {
		return User{}, err
	}
	u := jUser(m)
	if u.ID == 0 && u.Name == "" {
		return User{}, newErr(KindNotMantis, "回應中沒有使用者資料，網址可能不是 Mantis", nil)
	}
	return u, nil
}

func (c *restClient) Issues(ctx context.Context, scope string) ([]Issue, error) {
	if scope == "" {
		scope = ScopeAssigned
	}
	const pageSize = 100
	var out []Issue
	seen := map[int]bool{}
	for page := 1; page <= 50; page++ {
		var resp struct {
			Issues []map[string]any `json:"issues"`
		}
		q := url.Values{}
		q.Set("filter_id", scope)
		q.Set("page_size", strconv.Itoa(pageSize))
		q.Set("page", strconv.Itoa(page))
		if err := c.get(ctx, "/issues?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		added := 0
		for _, m := range resp.Issues {
			is := jIssue(m)
			if is.ID == 0 || seen[is.ID] {
				continue
			}
			seen[is.ID] = true
			out = append(out, is)
			added++
		}
		if len(resp.Issues) < pageSize || added == 0 {
			break
		}
	}
	return out, nil
}

func (c *restClient) Filters(ctx context.Context) ([]Filter, error) {
	var resp struct {
		Filters []map[string]any `json:"filters"`
	}
	if err := c.get(ctx, "/filters", &resp); err != nil {
		return nil, err
	}
	var out []Filter
	for _, m := range resp.Filters {
		f := Filter{ID: jInt(m["id"]), Name: jStr(m["name"]), Public: jBool(m["public"])}
		if pm, ok := m["project"].(map[string]any); ok {
			f.ProjectID = jInt(pm["id"])
		}
		out = append(out, f)
	}
	return out, nil
}

// ---- tolerant JSON helpers (field shapes vary slightly across versions) ----

func jStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		if s, ok := t["name"].(string); ok {
			return s
		}
	}
	return ""
}

func jInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

func jBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return false
}

func jRef(v any) Ref {
	m, ok := v.(map[string]any)
	if !ok {
		if s := jStr(v); s != "" {
			return Ref{Name: s, Label: s}
		}
		return Ref{}
	}
	r := Ref{ID: jInt(m["id"]), Name: jStr(m["name"]), Label: jStr(m["label"]), Color: jStr(m["color"])}
	if r.Label == "" {
		r.Label = r.Name
	}
	if r.Color == "currentcolor" {
		r.Color = ""
	}
	return r
}

func jUser(v any) User {
	m, ok := v.(map[string]any)
	if !ok {
		return User{}
	}
	return User{ID: jInt(m["id"]), Name: jStr(m["name"]), RealName: jStr(m["real_name"]), Email: jStr(m["email"])}
}

func jIssue(m map[string]any) Issue {
	is := Issue{
		ID:             jInt(m["id"]),
		Summary:        jStr(m["summary"]),
		Project:        jRef(m["project"]),
		Category:       jStr(m["category"]),
		Status:         jRef(m["status"]),
		Priority:       jRef(m["priority"]),
		Severity:       jRef(m["severity"]),
		Resolution:     jRef(m["resolution"]),
		Reporter:       jUser(m["reporter"]),
		Handler:        jUser(m["handler"]),
		Version:        jStr(m["version"]),
		TargetVersion:  jStr(m["target_version"]),
		FixedInVersion: jStr(m["fixed_in_version"]),
		Description:    jStr(m["description"]),
		Steps:          jStr(m["steps_to_reproduce"]),
		AdditionalInfo: jStr(m["additional_information"]),
		Created:        parseTime(jStr(m["created_at"])),
		Updated:        parseTime(jStr(m["updated_at"])),
		DueDate:        parseTime(jStr(m["due_date"])),
	}
	if notes, ok := m["notes"].([]any); ok {
		for _, nv := range notes {
			nm, ok := nv.(map[string]any)
			if !ok {
				continue
			}
			vs := jRef(nm["view_state"])
			is.Notes = append(is.Notes, Note{
				ID:       jInt(nm["id"]),
				Reporter: jUser(nm["reporter"]),
				Text:     jStr(nm["text"]),
				Private:  vs.ID == 50 || strings.EqualFold(vs.Name, "private"),
				Created:  parseTime(jStr(nm["created_at"])),
				Modified: parseTime(jStr(nm["updated_at"])),
			})
		}
	}
	if tags, ok := m["tags"].([]any); ok {
		for _, t := range tags {
			if s := jStr(t); s != "" {
				is.Tags = append(is.Tags, s)
			}
		}
	}
	return is
}
