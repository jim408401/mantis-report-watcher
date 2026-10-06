// Command mockmantis is a small fake MantisBT server used for local testing.
// It implements the parts of the SOAP and REST APIs the app uses.
//
//	go run ./tools/mockmantis -addr 127.0.0.1:8099            (login: demo / demo, token: demotoken)
//	go run ./tools/mockmantis -nosoap                          (simulate SOAP disabled)
//
// Test helpers: GET /mock/add (new assigned issue), GET /mock/touch?id=N (update issue).
package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type status struct {
	ID          int
	Name, Label string
	Color       string
}

var statuses = map[int]status{
	10: {10, "new", "新建", "#fcbdbd"},
	20: {20, "feedback", "進行中", "#e3b7eb"},
	30: {30, "acknowledged", "測試中", "#ffcd85"},
	40: {40, "confirmed", "已完成", "#fff494"},
	50: {50, "assigned", "已分配", "#c2dfff"},
	60: {60, "review", "待CR", "#d2f5b0"},
	80: {80, "resolved", "已解決", "#c9ccc4"},
}

type note struct {
	ID       int
	Reporter string
	Text     string
	Created  time.Time
}

type issue struct {
	ID            int
	Summary       string
	Project       string
	Category      string
	Status        int
	Priority      int
	Severity      int
	Reporter      string
	Handler       string
	TargetVersion string
	Description   string
	Steps         string
	Created       time.Time
	Updated       time.Time
	Notes         []note
}

var (
	mu     sync.Mutex
	issues []*issue
	nextID = 12500
	users  = map[string]struct {
		ID       int
		RealName string
	}{
		"demo":  {7, "王小明"},
		"alice": {8, "陳雅婷"},
		"bob":   {9, "林志豪"},
	}
	noSOAP = flag.Bool("nosoap", false, "simulate SOAP API disabled (404)")
	noExt  = flag.Bool("nosoapext", false, `simulate "PHP SOAP extension is not enabled." (POST hangs)`)
)

func seed() {
	now := time.Now()
	type s struct {
		sum, proj, cat string
		st             int
		tv             string
		ago            time.Duration
		handler        string
	}
	data := []s{
		{"登入頁面在 Safari 偶爾出現空白畫面", "ERP 前台", "前端", 20, "v3.2", 3 * time.Hour, "demo"},
		{"報表匯出 Excel 時中文欄位亂碼", "ERP 後台", "報表", 20, "v3.2", 26 * time.Hour, "demo"},
		{"訂單查詢加入「出貨日期」篩選條件", "ERP 前台", "功能需求", 50, "v3.3", 5 * time.Hour, "demo"},
		{"會員資料匯入 CSV 時驗證身分證格式", "CRM", "資料匯入", 50, "v3.3", 50 * time.Hour, "demo"},
		{"首頁儀表板載入速度優化（目前約 6 秒）", "ERP 前台", "效能", 50, "v3.4", 72 * time.Hour, "demo"},
		{"權限設定頁：角色複製功能", "ERP 後台", "功能需求", 60, "v3.2", 8 * time.Hour, "demo"},
		{"API 金鑰輪替排程程式", "平台服務", "後端", 60, "v3.2", 30 * time.Hour, "demo"},
		{"修正庫存數量在併發扣庫時可能為負數", "ERP 後台", "後端", 40, "v3.1", 96 * time.Hour, "demo"},
		{"客服工單通知信模板更新", "CRM", "通知", 40, "v3.1", 120 * time.Hour, "demo"},
		{"採購單審核流程第二關簽核人錯誤", "ERP 後台", "流程", 30, "v3.2", 10 * time.Hour, "demo"},
		{"手機版選單在 iOS 17 無法收合", "ERP 前台", "前端", 30, "v3.2", 40 * time.Hour, "demo"},
		{"新增供應商時統編重複檢查", "ERP 後台", "功能需求", 10, "", 2 * time.Hour, "demo"},
		{"整合 LINE 通知推播", "CRM", "通知", 20, "v3.4", 200 * time.Hour, "alice"},
	}
	for i, d := range data {
		created := now.Add(-d.ago - time.Duration(48+i*7)*time.Hour)
		is := &issue{
			ID: 12458 + i*3, Summary: d.sum, Project: d.proj, Category: d.cat, Status: d.st,
			Priority: 30 + (i%3)*10, Severity: 50, Reporter: "alice", Handler: d.handler,
			TargetVersion: d.tv, Created: created, Updated: now.Add(-d.ago),
			Description: "### 問題描述\n" + d.sum + "，使用者回報影響日常作業。\n\n### 重現步驟\n1. 以一般使用者登入\n2. 進入相關功能頁面\n3. 依照情境操作，觀察結果\n\n### 預期結果\n功能應正常運作，並顯示正確資料。\n\n```\nError: unexpected value at line 42\n```",
			Steps:       "請參考描述中的重現步驟。",
		}
		if i%2 == 0 {
			is.Notes = append(is.Notes, note{ID: 9000 + i, Reporter: "alice", Text: "麻煩協助確認，客戶這週要上線，**謝謝**！", Created: now.Add(-d.ago)})
		}
		if i%4 == 0 {
			is.Notes = append(is.Notes, note{ID: 9100 + i, Reporter: "bob", Text: "我這邊可以重現，log 已附在共享資料夾。", Created: now.Add(-d.ago + 30*time.Minute)})
		}
		issues = append(issues, is)
	}
}

func checkCreds(u, p string) bool { _, ok := users[u]; return ok && p == u }

// ---------------- SOAP ----------------

var tagRe = regexp.MustCompile(`(?s)<(?:[a-zA-Z0-9]+:)?(mc_[a-z_]+)[ >]`)

func soapField(body, name string) string {
	re := regexp.MustCompile(`(?s)<` + name + `[^>]*>(.*?)</` + name + `>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

func x(name, typ, v string) string {
	return fmt.Sprintf(`<%s xsi:type="%s">%s</%s>`, name, typ, v, name)
}
func xs(name, v string) string     { return x(name, "xsd:string", xmlEsc(v)) }
func xi(name string, v int) string { return x(name, "xsd:integer", strconv.Itoa(v)) }
func xmlEsc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func xAccount(name, user string) string {
	u := users[user]
	return x(name, "ns1:AccountData", xi("id", u.ID)+xs("name", user)+xs("real_name", u.RealName)+xs("email", user+"@example.com"))
}
func xRef(name string, id int, label string) string {
	return x(name, "ns1:ObjectRef", xi("id", id)+xs("name", label))
}

func soapEnvelope(method, inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ns1="http://futureware.biz/mantisconnect" xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:SOAP-ENC="http://schemas.xmlsoap.org/soap/encoding/" SOAP-ENV:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<SOAP-ENV:Body><ns1:` + method + `Response>` + inner + `</ns1:` + method + `Response></SOAP-ENV:Body></SOAP-ENV:Envelope>`
}

func soapFault(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(500)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"><SOAP-ENV:Body><SOAP-ENV:Fault><faultcode>SOAP-ENV:Client</faultcode><faultstring>`+xmlEsc(msg)+`</faultstring></SOAP-ENV:Fault></SOAP-ENV:Body></SOAP-ENV:Envelope>`)
}

func soapIssue(is *issue) string {
	st := statuses[is.Status]
	var b strings.Builder
	b.WriteString(xi("id", is.ID))
	b.WriteString(xRef("view_state", 10, "公開"))
	b.WriteString(x("last_updated", "xsd:dateTime", is.Updated.Format(time.RFC3339)))
	b.WriteString(xRef("project", 1, is.Project))
	b.WriteString(xs("category", is.Category))
	b.WriteString(xRef("priority", is.Priority, map[int]string{30: "一般", 40: "高", 50: "緊急"}[is.Priority]))
	b.WriteString(xRef("severity", is.Severity, "一般"))
	b.WriteString(xRef("status", st.ID, st.Label))
	b.WriteString(xAccount("reporter", is.Reporter))
	b.WriteString(xs("summary", is.Summary))
	b.WriteString(x("date_submitted", "xsd:dateTime", is.Created.Format(time.RFC3339)))
	if is.Handler != "" {
		b.WriteString(xAccount("handler", is.Handler))
	}
	b.WriteString(xRef("resolution", 10, "未處理"))
	if is.TargetVersion != "" {
		b.WriteString(xs("target_version", is.TargetVersion))
	}
	b.WriteString(xs("description", is.Description))
	b.WriteString(xs("steps_to_reproduce", is.Steps))
	if len(is.Notes) > 0 {
		var nb strings.Builder
		for _, n := range is.Notes {
			nb.WriteString(x("item", "ns1:IssueNoteData", xi("id", n.ID)+xAccount("reporter", n.Reporter)+xs("text", n.Text)+xRef("view_state", 10, "公開")+x("date_submitted", "xsd:dateTime", n.Created.Format(time.RFC3339))+x("last_modified", "xsd:dateTime", n.Created.Format(time.RFC3339))))
		}
		b.WriteString(fmt.Sprintf(`<notes SOAP-ENC:arrayType="ns1:IssueNoteData[%d]" xsi:type="SOAP-ENC:Array">%s</notes>`, len(is.Notes), nb.String()))
	}
	return x("item", "ns1:IssueData", b.String())
}

func page(list []*issue, pageNum, perPage int) []*issue {
	if pageNum < 1 {
		pageNum = 1
	}
	if perPage <= 0 {
		return list
	}
	start := (pageNum - 1) * perPage
	if start >= len(list) {
		return nil
	}
	end := start + perPage
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}

func filtered(scope, user string) []*issue {
	var out []*issue
	for _, is := range issues {
		switch scope {
		case "assigned":
			if is.Handler == user && is.Status < 80 {
				out = append(out, is)
			}
		case "reported":
			if is.Reporter == user {
				out = append(out, is)
			}
		case "monitored":
		case "1001":
			if is.Project == "ERP 前台" {
				out = append(out, is)
			}
		default:
			out = append(out, is)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

func handleSOAP(w http.ResponseWriter, r *http.Request) {
	if *noSOAP {
		http.NotFound(w, r)
		return
	}
	if *noExt {
		if r.Method == http.MethodPost {
			time.Sleep(40 * time.Second)
		}
		fmt.Fprint(w, "PHP SOAP extension is not enabled.")
		return
	}
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	m := tagRe.FindStringSubmatch(body)
	if m == nil {
		soapFault(w, "Bad request")
		return
	}
	method := m[1]
	user, pass := soapField(body, "username"), soapField(body, "password")
	if !checkCreds(user, pass) {
		soapFault(w, "Access denied")
		return
	}
	mu.Lock()
	defer mu.Unlock()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	switch method {
	case "mc_login":
		u := users[user]
		inner := x("return", "ns1:UserData", xAccount("account_data", user)+xi("access_level", 55)+xs("timezone", "Asia/Taipei"))
		_ = u
		fmt.Fprint(w, soapEnvelope(method, inner))
	case "mc_project_get_issues_for_user", "mc_filter_get_issues":
		scope := soapField(body, "filter_type")
		if method == "mc_filter_get_issues" {
			scope = soapField(body, "filter_id")
		}
		target := user
		if tu := soapField(body, "target_user"); tu != "" {
			if n := soapField(tu, "name"); n != "" {
				target = n
			}
		}
		pn, _ := strconv.Atoi(soapField(body, "page_number"))
		pp, _ := strconv.Atoi(soapField(body, "per_page"))
		list := page(filtered(scope, target), pn, pp)
		var items strings.Builder
		for _, is := range list {
			items.WriteString(soapIssue(is))
		}
		fmt.Fprint(w, soapEnvelope(method, fmt.Sprintf(`<return SOAP-ENC:arrayType="ns1:IssueData[%d]" xsi:type="SOAP-ENC:Array">%s</return>`, len(list), items.String())))
	case "mc_filter_get":
		item := x("item", "ns1:FilterData", xi("id", 1001)+xi("project_id", 0)+x("is_public", "xsd:boolean", "true")+xs("name", "前台待處理"))
		fmt.Fprint(w, soapEnvelope(method, `<return SOAP-ENC:arrayType="ns1:FilterData[1]" xsi:type="SOAP-ENC:Array">`+item+`</return>`))
	default:
		soapFault(w, "Unknown method "+method)
	}
}

// ---------------- REST ----------------

func restUser(r *http.Request) string {
	if a := r.Header.Get("Authorization"); a != "" {
		if strings.TrimPrefix(a, "Bearer ") == "demotoken" {
			return "demo"
		}
		return ""
	}
	if c, err := r.Cookie("MANTIS_STRING_COOKIE"); err == nil && strings.HasPrefix(c.Value, "sess-") {
		return strings.TrimPrefix(c.Value, "sess-")
	}
	return ""
}

func jUser(name string) map[string]any {
	u := users[name]
	return map[string]any{"id": u.ID, "name": name, "real_name": u.RealName, "email": name + "@example.com"}
}

func jIssue(is *issue) map[string]any {
	st := statuses[is.Status]
	m := map[string]any{
		"id": is.ID, "summary": is.Summary, "description": is.Description, "steps_to_reproduce": is.Steps,
		"project":    map[string]any{"id": 1, "name": is.Project},
		"category":   map[string]any{"id": 1, "name": is.Category},
		"reporter":   jUser(is.Reporter),
		"status":     map[string]any{"id": st.ID, "name": st.Name, "label": st.Label, "color": st.Color},
		"priority":   map[string]any{"id": is.Priority, "name": "normal", "label": "一般"},
		"severity":   map[string]any{"id": is.Severity, "name": "minor", "label": "一般"},
		"created_at": is.Created.Format(time.RFC3339), "updated_at": is.Updated.Format(time.RFC3339),
	}
	if is.Handler != "" {
		m["handler"] = jUser(is.Handler)
	}
	if is.TargetVersion != "" {
		m["target_version"] = map[string]any{"id": 3, "name": is.TargetVersion}
	}
	var notes []any
	for _, n := range is.Notes {
		notes = append(notes, map[string]any{"id": n.ID, "reporter": jUser(n.Reporter), "text": n.Text,
			"view_state": map[string]any{"id": 10, "name": "public"}, "created_at": n.Created.Format(time.RFC3339), "updated_at": n.Created.Format(time.RFC3339)})
	}
	if notes != nil {
		m["notes"] = notes
	}
	return m
}

func handleREST(w http.ResponseWriter, r *http.Request) {
	user := restUser(r)
	if user == "" {
		http.Error(w, "Valid API token required", http.StatusUnauthorized)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/mantisbt/api/rest")
	switch path {
	case "/users/me":
		_ = json.NewEncoder(w).Encode(jUser(user))
	case "/issues":
		pn, _ := strconv.Atoi(r.URL.Query().Get("page"))
		ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		list := page(filtered(r.URL.Query().Get("filter_id"), user), pn, ps)
		out := []any{}
		for _, is := range list {
			out = append(out, jIssue(is))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": out})
	case "/filters":
		_ = json.NewEncoder(w).Encode(map[string]any{"filters": []any{map[string]any{"id": 1001, "name": "前台待處理", "public": true}}})
	default:
		http.NotFound(w, r)
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	flag.Parse()
	seed()
	mux := http.NewServeMux()
	mux.HandleFunc("/mantisbt/api/soap/mantisconnect.php", handleSOAP)
	mux.HandleFunc("/mantisbt/api/rest/", handleREST)
	mux.HandleFunc("/mantisbt/login_password_page.php", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><form action="login.php" method="post"><input type="hidden" name="login_token" value="tok123"/><input name="password" type="password"/></form></html>`)
	})
	mux.HandleFunc("/mantisbt/login.php", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("login_token") == "tok123" && checkCreds(r.PostForm.Get("username"), r.PostForm.Get("password")) {
			http.SetCookie(w, &http.Cookie{Name: "MANTIS_STRING_COOKIE", Value: "sess-" + r.PostForm.Get("username"), Path: "/mantisbt/"})
			http.Redirect(w, r, "my_view_page.php", http.StatusFound)
			return
		}
		http.Redirect(w, r, "login_page.php?error=1", http.StatusFound)
	})
	mux.HandleFunc("/mantisbt/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<html><body>Mock Mantis page %s</body></html>", html.EscapeString(r.URL.Path))
	})
	mux.HandleFunc("/mock/add", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		nextID++
		now := time.Now()
		is := &issue{ID: nextID, Summary: fmt.Sprintf("【新指派】客戶回報結帳流程錯誤 #%d", nextID), Project: "ERP 前台", Category: "前端",
			Status: 50, Priority: 50, Severity: 50, Reporter: "bob", Handler: "demo", TargetVersion: "v3.2",
			Created: now, Updated: now, Description: "### 問題描述\n結帳第三步驟按下「確認」沒有反應。\n\n- 瀏覽器：Chrome 128\n- 發生頻率：約 30%"}
		issues = append(issues, is)
		fmt.Fprintf(w, "added %d\n", is.ID)
	})
	mux.HandleFunc("/mock/touch", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		mu.Lock()
		defer mu.Unlock()
		for _, is := range issues {
			if is.ID == id {
				is.Updated = time.Now()
				is.Notes = append(is.Notes, note{ID: int(time.Now().Unix() % 100000), Reporter: "bob", Text: "已更新測試結果，請再確認。", Created: time.Now()})
				fmt.Fprintf(w, "touched %d\n", id)
				return
			}
		}
		http.NotFound(w, r)
	})
	log.Printf("mock Mantis on http://%s/mantisbt (demo/demo, token demotoken, nosoap=%v)", *addr, *noSOAP)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
