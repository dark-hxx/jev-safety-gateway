package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"jev-safety-gateway/internal/config"
)

// Telemetry endpoints: /api/version (public, build identity + listen addresses)
// and /api/stats/latency (authenticated, percentiles over the audit log). The
// split between them is the point of this file — one is deliberately open to the
// login screen, the other must never be.

// newVersionHandler 构造一个带 Info 的处理器。版本信息全部来自构建与监听回填，
// 与审计数据无关，所以这里直接注入。
func newVersionHandler(t *testing.T, info *Info) (*Handler, *config.Store) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetAdminPasswordPlain("secret123"); err != nil {
		t.Fatalf("set admin password: %v", err)
	}
	return New(store, testFS(), info), store
}

// 版本端点是唯一免鉴权的 /api/ 路由：登录页要在拿到 token 之前显示构建信息与
// 守护进程地址。这里刻意不带 Authorization 请求，把「公开」这一行为钉住——若后来
// 有人顺手给它套上 h.auth，登录页会静默退回「未接入」降级态，而不会有任何报错。
func TestVersionIsPublicAndCarriesBuildInfoAndAddresses(t *testing.T) {
	h, _ := newVersionHandler(t, &Info{
		Version:   "v0.4.1",
		Commit:    "7a4ffcb",
		BuiltAt:   "2026-09-01T00:00:00Z",
		Go:        "go1.23.4",
		ProxyAddr: ":8080",
		AdminAddr: "127.0.0.1:8081",
	})

	w := getWithToken(t, h, "/api/version", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/version = %d, want 200（该端点设计上免鉴权）", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /api/version: %v", err)
	}
	want := map[string]string{
		"version":    "v0.4.1",
		"commit":     "7a4ffcb",
		"built_at":   "2026-09-01T00:00:00Z",
		"go":         "go1.23.4",
		"proxy_addr": ":8080",
		"admin_addr": "127.0.0.1:8081",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q（全量：%v）", k, got[k], v, got)
		}
	}
}

// 没有 Info 时也要给出可用响应：报告运行时的 Go 版本，而不是 500。
func TestVersionWithoutInfoStillAnswers(t *testing.T) {
	h, _ := newVersionHandler(t, nil)

	w := getWithToken(t, h, "/api/version", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/version = %d, want 200", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /api/version: %v", err)
	}
	if got["version"] != "dev" {
		t.Errorf("version = %q, want \"dev\"", got["version"])
	}
	if got["go"] == "" {
		t.Errorf("go = %q, want the runtime version", got["go"])
	}
}

func TestVersionRejectsNonGET(t *testing.T) {
	h, _ := newVersionHandler(t, &Info{Version: "v0.4.1"})

	req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/version = %d, want 405", w.Code)
	}
}

// 延迟分位数派生自审计日志，属于受保护数据：没有 token 必须 401。
// 与 /api/version 的公开性正好相反，两者不要被后来的人「统一」掉。
func TestLatencyRequiresToken(t *testing.T) {
	h, _ := newVersionHandler(t, nil)

	if w := getWithToken(t, h, "/api/stats/latency", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/stats/latency without a token = %d, want 401", w.Code)
	}
}

// 带 token 时返回窗口内的延迟分位，窗口外的记录不参与。
func TestLatencyReturnsWindowStats(t *testing.T) {
	h, store := newVersionHandler(t, nil)
	token := loginToken(t, h)

	now := time.Now()
	for i := 1; i <= 10; i++ {
		store.AddLog(config.LogEntry{TS: now.Add(-time.Duration(i) * time.Second), LatencyMS: int64(i * 10)})
	}
	store.AddLog(config.LogEntry{TS: now.Add(-48 * time.Hour), LatencyMS: 9999})

	w := getWithToken(t, h, "/api/stats/latency?hours=24", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/latency = %d, body = %s", w.Code, w.Body.String())
	}
	var st config.LatencyStats
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode latency stats: %v", err)
	}
	if st.Count != 10 {
		t.Errorf("count = %d, want 10（窗口外的记录必须排除）", st.Count)
	}
	if st.Min != 10 || st.Max != 100 {
		t.Errorf("min/max = %d/%d, want 10/100", st.Min, st.Max)
	}
	if st.P99 != 100 {
		t.Errorf("p99 = %v, want 100（实得 %+v）", st.P99, st)
	}
	if st.Sampled {
		t.Errorf("10 rows must not report Sampled")
	}
}

// hours 参数在统计端点之间必须一致：缺省 24、非法值回退、超上限被夹取。
func TestHoursParamDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 24},
		{"?hours=", 24},
		{"?hours=0", 24},
		{"?hours=-5", 24},
		{"?hours=abc", 24},
		{"?hours=1", 1},
		{"?hours=168", 168},
		{"?hours=99999", maxWindowHours},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/stats/latency"+c.query, nil)
		if got := hoursParam(req); got != c.want {
			t.Errorf("hoursParam(%q) = %d, want %d", c.query, got, c.want)
		}
	}
}
