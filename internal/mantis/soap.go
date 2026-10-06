package mantis

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const soapNS = "http://futureware.biz/mantisconnect"

// soapClient uses MantisConnect (api/soap/mantisconnect.php), which accepts
// the account's username and password directly.
type soapClient struct {
	cfg      Config
	base     string
	endpoint string
	http     *http.Client
	user     User
}

func newSOAPClient(cfg Config, base string) *soapClient {
	return &soapClient{
		cfg:      cfg,
		base:     base,
		endpoint: base + "/api/soap/mantisconnect.php",
		http:     newHTTPClient(cfg),
	}
}

func (c *soapClient) Mode() string { return "soap" }

type soapParam struct {
	name  string
	value string // already XML-escaped / raw XML when raw is true
	raw   bool
}

func p(name, value string) soapParam { return soapParam{name: name, value: value} }

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (c *soapClient) call(ctx context.Context, method string, params ...soapParam) (*node, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	body.WriteString(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:man="` + soapNS + `">`)
	body.WriteString(`<soapenv:Body><man:` + method + `>`)
	all := append([]soapParam{p("username", c.cfg.Username), p("password", c.cfg.Password)}, params...)
	for _, prm := range all {
		v := prm.value
		if !prm.raw {
			v = xmlEscape(v)
		}
		body.WriteString("<" + prm.name + ">" + v + "</" + prm.name + ">")
	}
	body.WriteString(`</man:` + method + `></soapenv:Body></soapenv:Envelope>`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(body.String()))
	if err != nil {
		return nil, newErr(KindNotMantis, "Mantis 網址格式不正確", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", soapNS+"/"+method)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, classifyTransportError(err)
	}
	b, err := readBody(resp, 64<<20)
	if err != nil {
		return nil, classifyTransportError(err)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, newErr(KindUnavailable, "此伺服器未開放 SOAP API", httpStatusErr(resp, b))
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	trimmed := bytes.TrimSpace(b)
	if !bytes.HasPrefix(trimmed, []byte("<?xml")) && !bytes.Contains(trimmed[:min(len(trimmed), 400)], []byte("Envelope")) {
		if strings.Contains(ct, "html") || resp.StatusCode >= 400 {
			return nil, newErr(KindUnavailable, "此伺服器未開放 SOAP API", httpStatusErr(resp, b))
		}
	}
	root, err := parseXML(b)
	if err != nil {
		return nil, newErr(KindUnavailable, "SOAP 回應格式無法解析", err)
	}
	if fault := root.find("Fault"); fault != nil {
		msg := strings.TrimSpace(fault.str("faultstring"))
		code := strings.TrimSpace(fault.str("faultcode"))
		low := strings.ToLower(msg)
		if strings.Contains(low, "access denied") || strings.Contains(low, "login failed") {
			return nil, newErr(KindAuth, "帳號或密碼錯誤", nil)
		}
		if strings.Contains(low, "disabled") && strings.Contains(low, "api") {
			return nil, newErr(KindUnavailable, "此伺服器停用了 SOAP API", nil)
		}
		_ = code
		return nil, newErr(KindServer, "Mantis 回傳錯誤："+msg, nil)
	}
	if resp.StatusCode >= 400 {
		return nil, newErr(KindServer, "Mantis 伺服器錯誤", httpStatusErr(resp, b))
	}
	ret := root.find(method + "Response")
	if ret == nil {
		return nil, newErr(KindUnavailable, "SOAP 回應缺少內容", nil)
	}
	if r := ret.child("return"); r != nil {
		return r, nil
	}
	return ret, nil
}

// preflight does a quick GET of the endpoint so servers without the PHP SOAP
// extension ("PHP SOAP extension is not enabled.") are detected immediately
// instead of waiting for a POST that may hang.
func (c *soapClient) preflight(ctx context.Context) error {
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return newErr(KindNotMantis, "Mantis 網址格式不正確", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return classifyTransportError(err)
	}
	b, _ := readBody(resp, 64<<10)
	low := strings.ToLower(string(b))
	if resp.StatusCode == http.StatusNotFound || strings.Contains(low, "soap extension is not enabled") || strings.Contains(low, "soap api disabled") {
		return newErr(KindUnavailable, "此伺服器未啟用 SOAP API", httpStatusErr(resp, b))
	}
	return nil
}

func (c *soapClient) Login(ctx context.Context) (User, error) {
	if err := c.preflight(ctx); err != nil {
		return User{}, err
	}
	r, err := c.call(ctx, "mc_login")
	if err != nil {
		return User{}, err
	}
	acc := r.child("account_data")
	if acc == nil {
		return User{}, newErr(KindUnavailable, "SOAP 登入回應缺少帳號資料", nil)
	}
	c.user = soapAccount(acc)
	if c.user.Name == "" {
		c.user.Name = c.cfg.Username
	}
	return c.user, nil
}

func (c *soapClient) Issues(ctx context.Context, scope string) ([]Issue, error) {
	const perPage = 100
	var out []Issue
	seen := map[int]bool{}
	for page := 1; page <= 50; page++ {
		var r *node
		var err error
		if id, convErr := strconv.Atoi(scope); convErr == nil {
			r, err = c.call(ctx, "mc_filter_get_issues",
				p("project_id", "0"), p("filter_id", strconv.Itoa(id)),
				p("page_number", strconv.Itoa(page)), p("per_page", strconv.Itoa(perPage)))
		} else {
			if scope == "" {
				scope = ScopeAssigned
			}
			target := "<name>" + xmlEscape(c.cfg.Username) + "</name>"
			if c.user.ID > 0 {
				target = "<id>" + strconv.Itoa(c.user.ID) + "</id>" + target
			}
			r, err = c.call(ctx, "mc_project_get_issues_for_user",
				p("project_id", "0"), p("filter_type", scope),
				soapParam{name: "target_user", value: target, raw: true},
				p("page_number", strconv.Itoa(page)), p("per_page", strconv.Itoa(perPage)))
		}
		if err != nil {
			return nil, err
		}
		items := r.items()
		added := 0
		for _, it := range items {
			is := soapIssue(it)
			if is.ID == 0 || seen[is.ID] {
				continue
			}
			seen[is.ID] = true
			out = append(out, is)
			added++
		}
		if len(items) < perPage || added == 0 {
			break
		}
	}
	return out, nil
}

func (c *soapClient) Filters(ctx context.Context) ([]Filter, error) {
	r, err := c.call(ctx, "mc_filter_get", p("project_id", "0"))
	if err != nil {
		return nil, err
	}
	var out []Filter
	for _, it := range r.items() {
		out = append(out, Filter{
			ID:        it.int("id"),
			Name:      it.str("name"),
			ProjectID: it.int("project_id"),
			Public:    it.boolean("is_public"),
		})
	}
	return out, nil
}

func soapAccount(n *node) User {
	if n == nil || n.Nil {
		return User{}
	}
	return User{ID: n.int("id"), Name: n.str("name"), RealName: n.str("real_name"), Email: n.str("email")}
}

func soapRef(n *node) Ref {
	if n == nil || n.Nil {
		return Ref{}
	}
	name := n.str("name")
	return Ref{ID: n.int("id"), Name: name, Label: name}
}

func soapIssue(n *node) Issue {
	is := Issue{
		ID:             n.int("id"),
		Summary:        n.str("summary"),
		Project:        soapRef(n.child("project")),
		Category:       n.str("category"),
		Status:         soapRef(n.child("status")),
		Priority:       soapRef(n.child("priority")),
		Severity:       soapRef(n.child("severity")),
		Resolution:     soapRef(n.child("resolution")),
		Reporter:       soapAccount(n.child("reporter")),
		Handler:        soapAccount(n.child("handler")),
		Version:        n.str("version"),
		TargetVersion:  n.str("target_version"),
		FixedInVersion: n.str("fixed_in_version"),
		Description:    n.str("description"),
		Steps:          n.str("steps_to_reproduce"),
		AdditionalInfo: n.str("additional_information"),
		Created:        n.time("date_submitted"),
		Updated:        n.time("last_updated"),
		DueDate:        n.time("due_date"),
	}
	for _, nn := range n.child("notes").items() {
		vs := soapRef(nn.child("view_state"))
		is.Notes = append(is.Notes, Note{
			ID:       nn.int("id"),
			Reporter: soapAccount(nn.child("reporter")),
			Text:     nn.str("text"),
			Private:  vs.ID == 50,
			Created:  nn.time("date_submitted"),
			Modified: nn.time("last_modified"),
		})
	}
	for _, t := range n.child("tags").items() {
		if name := t.str("name"); name != "" {
			is.Tags = append(is.Tags, name)
		}
	}
	return is
}
