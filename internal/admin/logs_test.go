package admin

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"jev-safety-gateway/internal/config"
)

// 审计页的筛选条件与模型下拉都走这里：新增的模型选项端点、以及 /api/logs 新增的
// path / ip / model 参数。鉴权语义与既有 /api/* 一致，不因为新增路由而放宽。

// seedLogs 写入几条覆盖三个筛选维度的审计记录。
func seedLogs(t *testing.T, store *config.Store) {
	t.Helper()
	now := time.Now()
	rows := []config.LogEntry{
		{Method: "POST", Path: "/v1/chat/completions", Model: "gpt-4o", IP: "192.168.1.20", Decision: "allow", Kind: "chat"},
		{Method: "POST", Path: "/v1/chat/completions", Model: "gpt-4o-mini", IP: "192.168.1.20", Decision: "block", Kind: "chat"},
		{Method: "POST", Path: "/v1/messages", Model: "claude-3-5-sonnet", IP: "194.26.7.9", Decision: "allow", Kind: "messages"},
	}
	for i, e := range rows {
		e.TS = now.Add(time.Duration(i) * time.Second)
		store.AddLog(e)
	}
}

func getWithToken(t *testing.T, h *Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// 模型下拉端点是 /api/* 的一员，未带 token 必须 401。
func TestLogModelsRequiresToken(t *testing.T) {
	h, _ := newTestHandler(t)
	if w := getWithToken(t, h, "/api/logs/models", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("未带 token 的 /api/logs/models = %d, want 401", w.Code)
	}
}

// 端点为下拉返回「库中真实出现过的模型 + 出现次数」，按次数倒序。
func TestLogModelsEndpointReturnsChoiceList(t *testing.T) {
	h, store := newTestHandler(t)
	seedLogs(t, store)
	store.AddLog(config.LogEntry{TS: time.Now(), Method: "POST", Path: "/v1/chat/completions", Model: "gpt-4o", IP: "1.1.1.1"})

	w := getWithToken(t, h, "/api/logs/models", loginToken(t, h))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []config.ModelCount `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (body = %s)", err, w.Body.String())
	}
	if len(out.Items) != 3 {
		t.Fatalf("模型数 = %d, want 3 (items = %+v)", len(out.Items), out.Items)
	}
	// gpt-4o 出现 2 次，应排在最前；其余各 1 次，按值升序。
	if out.Items[0].Value != "gpt-4o" || out.Items[0].Count != 2 {
		t.Errorf("items[0] = %+v, want {gpt-4o 2}", out.Items[0])
	}
	if out.Items[1].Value != "claude-3-5-sonnet" || out.Items[2].Value != "gpt-4o-mini" {
		t.Errorf("items = %+v, want 剩余两项按值升序", out.Items)
	}
}

// since 透传到下拉：窗口内没有记录时返回空列表而不是报错。
func TestLogModelsEndpointHonoursSince(t *testing.T) {
	h, store := newTestHandler(t)
	seedLogs(t, store)

	future := time.Now().Add(time.Hour).UnixMilli()
	w := getWithToken(t, h, "/api/logs/models?since="+strconv.FormatInt(future, 10), loginToken(t, h))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []config.ModelCount `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Items) != 0 {
		t.Errorf("未来窗口的模型数 = %d, want 0", len(out.Items))
	}
}

// /api/logs 新增的 path / ip / model 参数生效，且仍是「与」的关系。
func TestLogsEndpointFiltersByPathIPAndModel(t *testing.T) {
	h, store := newTestHandler(t)
	seedLogs(t, store)
	token := loginToken(t, h)

	cases := []struct {
		name  string
		query string
		want  int64
	}{
		{"路径子串", "&path=%2Fv1%2Fchat", 2},
		{"完整路径", "&path=%2Fv1%2Fchat%2Fcompletions", 2},
		{"IP 前缀", "&ip=192.168.1", 2},
		{"IP 完整地址", "&ip=194.26.7.9", 1},
		{"IP 带星号（原型写法）", "&ip=194.26.*", 1},
		{"模型精确匹配", "&model=gpt-4o", 1},
		{"模型不匹配同类前缀", "&model=gpt-4o-mini", 1},
		{"三个条件为与", "&model=gpt-4o&ip=192.168.1&path=%2Fv1%2Fchat", 1},
		{"互斥条件无命中", "&model=gpt-4o&ip=194.26.", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := getWithToken(t, h, "/api/logs?limit=50&offset=0"+tc.query, token)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var out struct {
				Items []config.LogEntry `json:"items"`
				Total int64              `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if out.Total != tc.want {
				t.Errorf("total = %d, want %d (items = %d)", out.Total, tc.want, len(out.Items))
			}
			if int64(len(out.Items)) != tc.want {
				t.Errorf("items = %d, want %d", len(out.Items), tc.want)
			}
		})
	}
}

// 注册了嵌套路由之后，同前缀下未注册的路径仍须 404，不能被当成页面或吞掉。
func TestUnknownLogsSubpathStill404(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/api/logs/nope", "/api/logs/models/extra"} {
		w := getWithToken(t, h, p, loginToken(t, h))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
	}
}

// CSV 导出：带 BOM、表头列数与每行列数一致，并且沿用与 /api/logs 相同的筛选条件。
func TestLogsExportCSV(t *testing.T) {
	h, store := newTestHandler(t)
	seedLogs(t, store)
	token := loginToken(t, h)

	w := getWithToken(t, h, "/api/logs/export?model=gpt-4o", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("content-disposition = %q, want an attachment", cd)
	}
	body := w.Body.String()
	// Excel 靠 BOM 认出 UTF-8，缺了它中文原因与摘要会乱码。
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Fatalf("export is missing the UTF-8 BOM")
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatalf("export is not valid CSV: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want header + 1 filtered row:\n%s", len(recs), body)
	}
	header, row := recs[0], recs[1]
	if len(row) != len(header) {
		t.Fatalf("row has %d fields, header has %d", len(row), len(header))
	}
	col := map[string]string{}
	for i, name := range header {
		col[name] = row[i]
	}
	// model=gpt-4o 是精确匹配，gpt-4o-mini 那一行不得出现。
	if col["模型"] != "gpt-4o" {
		t.Errorf("模型 = %q, want gpt-4o", col["模型"])
	}
	if strings.Contains(body, "gpt-4o-mini") {
		t.Errorf("model filter leaked gpt-4o-mini into the export:\n%s", body)
	}
	// 分数列在未评分的行为空，与真实的 0 分相区分。
	if _, ok := col["分数"]; !ok {
		t.Errorf("header is missing the 分数 column: %v", header)
	}
}

// 导出是 /api/* 的一员，未带 token 必须 401。
func TestLogsExportRequiresToken(t *testing.T) {
	h, _ := newTestHandler(t)
	if w := getWithToken(t, h, "/api/logs/export", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// 导出走的是浏览器下载，<a download> 带不了请求头，所以只有这条路由额外接受
// 查询串里的 token；其它 /api/* 路由仍只认 Authorization 头。
func TestLogsExportAcceptsQueryToken(t *testing.T) {
	h, store := newTestHandler(t)
	seedLogs(t, store)

	if w := getWithToken(t, h, "/api/logs/export?token=bogus", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("bogus query token = %d, want 401", w.Code)
	}
	w := getWithToken(t, h, "/api/logs/export?token="+loginToken(t, h), "")
	if w.Code != http.StatusOK {
		t.Errorf("valid query token = %d, want 200", w.Code)
	}
	// 同样的查询串 token 在别的路由上不生效。
	if w := getWithToken(t, h, "/api/logs?token="+loginToken(t, h), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("query token on /api/logs = %d, want 401", w.Code)
	}
}
