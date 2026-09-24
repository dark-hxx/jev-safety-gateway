package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"jev-safety-gateway/internal/config"
)

// testFS 是一个最小的控制台构建产物：入口页 + 一个带 hash 的资源目录。
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte(`<!doctype html><div id="app"></div>`)},
		"assets/index-a1.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
}

func newTestHandler(t *testing.T) (*Handler, *config.Store) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetAdminPasswordPlain("secret123"); err != nil {
		t.Fatalf("set admin password: %v", err)
	}
	return New(store, testFS(), nil, nil), store
}

// 登录一次，返回可用的 bearer token。
func loginToken(t *testing.T, h *Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/login", jsonBody(t, map[string]string{"password": "secret123"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", w.Code, w.Body.String())
	}
	var out struct{ Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("login response has no token: %s (%v)", w.Body.String(), err)
	}
	return out.Token
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewReader(b)
}

// --- SPA 回退（history 模式深链接）---

// 未命中静态文件的 GET 请求回退到 index.html，深链接直接打开与刷新不 404。
func TestSPAFallbackServesIndexForDeepLinks(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/dashboard", "/settings", "/audit", "/unknown/deep/path"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200（应回退到 index.html）", p, w.Code)
			continue
		}
		if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("GET %s content-type = %q", p, got)
		}
		if body := w.Body.String(); body != string(testFS()["index.html"].Data) {
			t.Errorf("GET %s did not return index.html: %q", p, body)
		}
	}
}

// 真实存在的静态文件照常返回，不被回退覆盖。
func TestSPAFallbackKeepsRealAssets(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/assets/index-a1.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET asset = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "console.log(1)" {
		t.Errorf("asset body = %q，回退不应拦截真实文件", body)
	}
}

// 未注册的 /api/ 路径保持 404，不会被回退成页面。
// `/api/../api/nope` 这类需规范化的路径由 mux 先做 301/307 跳转，这里只要求它不落到 SPA 外壳上。
func TestSPAFallbackDoesNotSwallowUnknownAPIPaths(t *testing.T) {
	h, _ := newTestHandler(t)
	shell := string(testFS()["index.html"].Data)
	for _, p := range []string{"/api", "/api/does-not-exist", "/api/"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
		if body := w.Body.String(); body == shell {
			t.Errorf("unknown %s path was served the SPA shell", p)
		}
	}
	for _, p := range []string{"/api/../api/nope", "/api/./nope"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusOK && w.Body.String() == shell {
			t.Errorf("GET %s was served the SPA shell", p)
		}
	}
}

// 非 GET/HEAD 不回退（静态服务只处理读取）。
func TestSPAFallbackOnlyHandlesReads(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code == http.StatusOK && w.Header().Get("Content-Type") == "text/html; charset=utf-8" {
		t.Error("POST /dashboard was served the SPA shell")
	}
}

// 根路径返回控制台入口页。
func TestSPAFallbackServesRoot(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != string(testFS()["index.html"].Data) {
		t.Fatalf("GET / = %d %q, want the console entry page", w.Code, w.Body.String())
	}
}

// --- /api/stats 新增字段 ---

// 既有字段语义不变，新增字段与库表记录一致（含空桶补 0 与未获分值计数）。
func TestStatsEndpointExtendsExistingResponse(t *testing.T) {
	h, store := newTestHandler(t)
	token := loginToken(t, h)

	now := time.Now()
	rows := []struct {
		ago      time.Duration
		decision string
		score    *float64
	}{
		{30 * time.Minute, "allow", ptr(0.9)},
		{20 * time.Minute, "block", ptr(0.1)},
		{10 * time.Minute, "allow", ptr(1.0)},
		{5 * time.Minute, "error", nil}, // 已送检但 JEV 未返回分值
		{4 * time.Minute, "skip", nil},  // 未送检
	}
	for _, r := range rows {
		store.AddLog(config.LogEntry{TS: now.Add(-r.ago), Decision: r.decision, Score: r.score})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats?hours=6", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats = %d, body = %s", w.Code, w.Body.String())
	}

	var out struct {
		Total   int64 `json:"total"`
		Allowed int64 `json:"allowed"`
		Blocked int64 `json:"blocked"`
		Skipped int64 `json:"skipped"`
		Errors  int64 `json:"errors"`

		BucketSeconds int `json:"bucket_seconds"`
		Series        []struct {
			TS      int64 `json:"ts"`
			Total   int64 `json:"total"`
			Allowed int64 `json:"allowed"`
			Blocked int64 `json:"blocked"`
		} `json:"series"`
		ScoreBuckets []int64 `json:"score_buckets"`
		Unscored     int64   `json:"unscored"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode stats: %v (%s)", err, w.Body.String())
	}

	// 既有字段
	if out.Total != 5 || out.Allowed != 2 || out.Blocked != 1 || out.Skipped != 1 || out.Errors != 1 {
		t.Errorf("existing aggregates changed: %+v", out)
	}

	// 分桶粒度按 hours 推导并回传
	if want := config.BucketSeconds(6); out.BucketSeconds != want {
		t.Errorf("bucket_seconds = %d, want %d", out.BucketSeconds, want)
	}
	if len(out.Series) == 0 {
		t.Fatal("series is empty; 前端需要逐桶序列")
	}
	step := int64(out.BucketSeconds) * 1000
	var seriesTotal, seriesAllowed, seriesBlocked int64
	for i, b := range out.Series {
		if b.TS%step != 0 {
			t.Errorf("bucket %d ts=%d not aligned to %dms", i, b.TS, step)
		}
		seriesTotal += b.Total
		seriesAllowed += b.Allowed
		seriesBlocked += b.Blocked
	}
	if seriesTotal != out.Total || seriesAllowed != out.Allowed || seriesBlocked != out.Blocked {
		t.Errorf("series sums (%d/%d/%d) disagree with the aggregates (%d/%d/%d)",
			seriesTotal, seriesAllowed, seriesBlocked, out.Total, out.Allowed, out.Blocked)
	}

	// 分值直方图：0.9→末档、0.1→首档、1.0→末档（末档含 1.0），error 记 unscored
	if len(out.ScoreBuckets) != config.ScoreSlots {
		t.Fatalf("score_buckets has %d slots, want %d", len(out.ScoreBuckets), config.ScoreSlots)
	}
	wantSlots := []int64{1, 0, 0, 0, 2}
	for i := range wantSlots {
		if out.ScoreBuckets[i] != wantSlots[i] {
			t.Errorf("score_buckets[%d] = %d, want %d (all: %v)", i, out.ScoreBuckets[i], wantSlots[i], out.ScoreBuckets)
		}
	}
	if out.Unscored != 1 {
		t.Errorf("unscored = %d, want 1", out.Unscored)
	}
}

// hours 缺省为 24；非法值不改变既有语义。
func TestStatsEndpointDefaultHours(t *testing.T) {
	h, _ := newTestHandler(t)
	token := loginToken(t, h)
	for _, q := range []string{"", "?hours=0", "?hours=-5", "?hours=abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/stats"+q, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/stats%s = %d", q, w.Code)
		}
		var out struct {
			BucketSeconds int `json:"bucket_seconds"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if want := config.BucketSeconds(24); out.BucketSeconds != want {
			t.Errorf("GET /api/stats%s bucket_seconds = %d, want %d (24h 默认)", q, out.BucketSeconds, want)
		}
	}
}

// /api/stats 仍需 bearer token（鉴权语义未被新增字段改动）。
func TestStatsEndpointStillRequiresToken(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats?hours=1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/stats = %d, want 401", w.Code)
	}
}

func ptr(v float64) *float64 { return &v }
