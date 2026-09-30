package config

import (
	"path/filepath"
	"testing"
	"time"
)

// These tests cover the audit-log filters the console exposes as search
// conditions. The three text conditions deliberately differ (exact model, prefix
// IP, substring path), so each is pinned here: a change of matching semantics
// would otherwise silently return the wrong rows.

// newLogStore opens a store with a few audit rows covering the fields under test.
func newLogStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	base := time.Now().Add(-time.Minute)
	rows := []LogEntry{
		// Two models sharing a prefix, to catch substring matching on model.
		{Method: "POST", Path: "/v1/chat/completions", Kind: "chat", Decision: "allow", Model: "gpt-4o", IP: "192.168.1.20"},
		{Method: "POST", Path: "/v1/chat/completions", Kind: "chat", Decision: "block", Model: "gpt-4o-mini", IP: "192.168.1.20"},
		{Method: "POST", Path: "/v1/messages", Kind: "messages", Decision: "allow", Model: "claude-3-5-sonnet", IP: "10.0.0.5"},
		{Method: "POST", Path: "/v1/embeddings", Kind: "embeddings", Decision: "allow", Model: "text-embedding-3", IP: "194.26.7.9"},
		{Method: "POST", Path: "/v1/responses", Kind: "oversize", Decision: "skip", IP: "194.26.7.9"},
		// A LIKE-metacharacter model, to prove filters match literally.
		{Method: "POST", Path: "/v1/files", Kind: "files", Decision: "skip", Model: "100%_free", IP: "8.8.8.8"},
	}
	for i, e := range rows {
		e.TS = base.Add(time.Duration(i) * time.Second)
		store.AddLog(e)
	}
	return store
}

func mustQuery(t *testing.T, s *Store, f LogFilter) []LogEntry {
	t.Helper()
	f.Limit = 50
	rows, _, err := s.QueryLogs(f)
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	return rows
}

// 模型为精确匹配：选 gpt-4o 不能把 gpt-4o-mini 一起带出来。
func TestLogFilterMatchesModelExactly(t *testing.T) {
	store := newLogStore(t)

	rows := mustQuery(t, store, LogFilter{Model: "gpt-4o"})
	if len(rows) != 1 {
		t.Fatalf("model=gpt-4o 命中 %d 条，want 1（精确匹配）", len(rows))
	}
	if rows[0].Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", rows[0].Model)
	}
}

// 路径为子串匹配：填前缀片段应命中其下所有端点。
func TestLogFilterMatchesPathAsSubstring(t *testing.T) {
	store := newLogStore(t)

	rows := mustQuery(t, store, LogFilter{Path: "/v1/chat"})
	if len(rows) != 2 {
		t.Fatalf(`path="/v1/chat" 命中 %d 条，want 2`, len(rows))
	}
	for _, r := range rows {
		if r.Path != "/v1/chat/completions" {
			t.Errorf("命中路径 %q，want /v1/chat/completions", r.Path)
		}
	}
}

// IP 为前缀匹配：完整地址、片段、以及原型宣称的 "194.26.*" 都应生效。
func TestLogFilterMatchesIPByPrefix(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want int
	}{
		{"完整地址命中自己", "194.26.7.9", 2},
		{"片段命中同网段", "194.26.", 2},
		{"原型写法，尾部星号被去掉", "194.26.*", 2},
		{"更短的片段不误伤", "194.26.7.1", 0},
		{"另一网段", "192.168.1", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newLogStore(t)
			if got := mustQuery(t, store, LogFilter{IP: tc.ip}); len(got) != tc.want {
				t.Errorf("ip=%q 命中 %d 条，want %d", tc.ip, len(got), tc.want)
			}
		})
	}
}

// 只填星号（去星号后为空）不应变成"匹配一切"的约束，而是等同于没有该条件。
func TestLogFilterIgnoresEmptyIPPrefix(t *testing.T) {
	store := newLogStore(t)

	all := mustQuery(t, store, LogFilter{})
	for _, ip := range []string{"*", "  *  ", ""} {
		if got := mustQuery(t, store, LogFilter{IP: ip}); len(got) != len(all) {
			t.Errorf("ip=%q 命中 %d 条，want %d（应视为无约束）", ip, len(got), len(all))
		}
	}
}

// LIKE 元字符按字面匹配，不能把用户输入当成通配符。
func TestLogFilterEscapesLikeMetacharacters(t *testing.T) {
	store := newLogStore(t)

	if got := mustQuery(t, store, LogFilter{Model: "100%_free"}); len(got) != 1 {
		t.Errorf(`model="100%%_free" 命中 %d 条，want 1（字面匹配）`, len(got))
	}
	// "%" 若未转义会匹配任意串；转义后应无命中。
	if got := mustQuery(t, store, LogFilter{Model: "%"}); len(got) != 0 {
		t.Errorf(`model="%%" 命中 %d 条，want 0（元字符应被转义）`, len(got))
	}
	if got := mustQuery(t, store, LogFilter{Path: "%"}); len(got) != 0 {
		t.Errorf(`path="%%" 命中 %d 条，want 0（元字符应被转义）`, len(got))
	}
}

// 多个条件之间为「与」。
func TestLogFiltersCombineWithAnd(t *testing.T) {
	store := newLogStore(t)

	rows := mustQuery(t, store, LogFilter{IP: "194.26.", Decision: "skip"})
	if len(rows) != 1 {
		t.Fatalf("ip+decision 命中 %d 条，want 1", len(rows))
	}
	if rows[0].Kind != "oversize" {
		t.Errorf("kind = %q, want oversize", rows[0].Kind)
	}

	if got := mustQuery(t, store, LogFilter{IP: "194.26.", Decision: "allow"}); len(got) != 1 {
		t.Errorf("ip=194.26. + decision=allow 命中 %d 条，want 1", len(got))
	}
	// 互斥条件应无命中，而不是退化成其中一个。
	if got := mustQuery(t, store, LogFilter{Model: "gpt-4o", Path: "/v1/messages"}); len(got) != 0 {
		t.Errorf("互斥条件命中 %d 条，want 0", len(got))
	}
}

// total 必须是匹配总数（忽略 Limit/Offset），分页据此计算页数。
func TestQueryLogsTotalIgnoresPaging(t *testing.T) {
	store := newLogStore(t)

	_, total, err := store.QueryLogs(LogFilter{Limit: 1, Offset: 0, Path: "/v1/chat"})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2（匹配总数，与分页无关）", total)
	}
}

// 模型下拉的选项：只含真实出现过的非空模型，按出现次数倒序。
func TestLogModelsListsExistingValuesByCount(t *testing.T) {
	store := newLogStore(t)

	models, err := store.LogModels(0)
	if err != nil {
		t.Fatalf("LogModels: %v", err)
	}
	if len(models) != 5 {
		t.Fatalf("模型数 = %d, want 5（空 model 的那条 skip 记录不参与）", len(models))
	}
	// 六个固定记录里四条 skip/chat 之外的模型各出现 1 次，gpt-4o 与
	// gpt-4o-mini 都在列表里（精确匹配的测试依赖这一点）。次数相同时按值
	// 升序，顺序稳定才可断言。
	want := []string{"100%_free", "claude-3-5-sonnet", "gpt-4o", "gpt-4o-mini", "text-embedding-3"}
	for i, m := range models {
		if m.Value != want[i] {
			t.Errorf("models[%d].Value = %q, want %q", i, m.Value, want[i])
		}
		if m.Count != 1 {
			t.Errorf("models[%d].Count = %d, want 1", i, m.Count)
		}
	}
}

// 出现次数多的模型排在最前，便于直接选到常用值。
func TestLogModelsOrdersByCountFirst(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	for i := 0; i < 3; i++ {
		store.AddLog(LogEntry{TS: now, Method: "POST", Path: "/v1/chat/completions", Model: "frequent", IP: "1.1.1.1"})
	}
	store.AddLog(LogEntry{TS: now, Method: "POST", Path: "/v1/messages", Model: "rare", IP: "1.1.1.1"})

	models, err := store.LogModels(0)
	if err != nil {
		t.Fatalf("LogModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("模型数 = %d, want 2", len(models))
	}
	if models[0].Value != "frequent" || models[0].Count != 3 {
		t.Errorf("models[0] = %+v, want {frequent 3}", models[0])
	}
	if models[1].Value != "rare" || models[1].Count != 1 {
		t.Errorf("models[1] = %+v, want {rare 1}", models[1])
	}
}

// 与日志列表一致，模型选项也只认时间窗内的记录。
func TestLogModelsHonoursSinceWindow(t *testing.T) {
	store := newLogStore(t)
	all, err := store.LogModels(0)
	if err != nil {
		t.Fatalf("LogModels: %v", err)
	}

	// 未来时间窗内必然没有任何记录。
	future := time.Now().Add(time.Hour).UnixMilli()
	models, err := store.LogModels(future)
	if err != nil {
		t.Fatalf("LogModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("未来窗口的模型数 = %d, want 0", len(models))
	}

	// 窗口下界早于全部记录时结果与无窗口一致。
	past := time.Now().Add(-time.Hour).UnixMilli()
	models, err = store.LogModels(past)
	if err != nil {
		t.Fatalf("LogModels: %v", err)
	}
	if len(models) != len(all) {
		t.Errorf("过去窗口的模型数 = %d, want %d", len(models), len(all))
	}
}

// --- 仪表盘与审计页新增读数所依赖的查询 ---

// 峰值流量是「最繁忙的单个自然秒」的请求数，不是区间总量、也不是每桶恒为 1。
// `ts/1000` 的分桶一旦被改成浮点除法（每个成一行、每行计数 1），或 MAX 被写成
// SUM，这个数字就会静默地变成别的量——所以这里用 3/1/2 的分布把三者区分开。
func TestPeakPPSIsBusiestSingleSecond(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// 秒内偏移跨越整秒边界，确保分桶按自然秒而不是按「距首条记录多久」。
	base := time.Now().Add(-time.Minute).Truncate(time.Second)
	in := func(offset time.Duration) {
		store.AddLog(LogEntry{TS: base.Add(offset), Method: "POST", Path: "/v1/chat/completions", Decision: "allow", IP: "1.1.1.1"})
	}
	in(0)
	in(300 * time.Millisecond)
	in(700 * time.Millisecond) // 与上两条同属第 0 秒
	in(time.Second)            // 第 1 秒
	in(2 * time.Second)
	in(2*time.Second + 500*time.Millisecond) // 第 2 秒

	// 窗口外的记录不参与：更早一秒，若被算进去峰值仍应是 3，故单看它无法区分——
	// 但它同时验证了下界，且总量断言会随它变化。
	store.AddLog(LogEntry{TS: base.Add(-time.Second), Method: "POST", Path: "/v1/chat/completions", Decision: "allow", IP: "1.1.1.1"})

	peak, err := store.PeakPPS(base, base.Add(3*time.Second))
	if err != nil {
		t.Fatalf("PeakPPS: %v", err)
	}
	if peak != 3 {
		t.Errorf("peak = %d, want 3（第 0 秒最繁忙；总量是 6，浮点分桶会得到 1）", peak)
	}

	// 窗口只盖住第 1 秒时，峰值必须是那一秒的 1，而不是全库的最大值 3。
	peak, err = store.PeakPPS(base.Add(time.Second), base.Add(2*time.Second))
	if err != nil {
		t.Fatalf("PeakPPS: %v", err)
	}
	if peak != 1 {
		t.Errorf("窄窗口 peak = %d, want 1", peak)
	}

	// 窗口内没有任何记录时是 0，不是错误。
	peak, err = store.PeakPPS(base.Add(time.Hour), base.Add(time.Hour+time.Second))
	if err != nil {
		t.Fatalf("PeakPPS: %v", err)
	}
	if peak != 0 {
		t.Errorf("空窗口 peak = %d, want 0", peak)
	}
}

// 环比的两段窗口必须「不重叠、不留缝」：上期是 [from, since)，本期是 [since, ∞)。
// 恰好落在 since 上的那条记录属于本期——若两边都用闭区间，它会同时计入两期，
// 环比就会出现一个凭空的增量。
func TestStatsBetweenPreviousWindowIsHalfOpen(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	since := time.Now().Add(-time.Hour).Truncate(time.Second)
	prevFrom := since.Add(-time.Hour)

	add := func(ts time.Time) {
		store.AddLog(LogEntry{TS: ts, Method: "POST", Path: "/v1/chat/completions", Decision: "allow", IP: "1.1.1.1"})
	}
	add(prevFrom.Add(-time.Second))   // 上期之前：两期都不算
	add(prevFrom)                     // 上期下界：闭，算上期
	add(since.Add(-time.Millisecond)) // 上期末：算上期
	add(since)                        // 边界：只能算本期
	add(since.Add(time.Minute))       // 本期

	prev, err := store.StatsBetween(prevFrom, since)
	if err != nil {
		t.Fatalf("StatsBetween: %v", err)
	}
	if prev.Total != 2 {
		t.Errorf("上期 total = %d, want 2（下界含、上界不含）", prev.Total)
	}

	cur, err := store.StatsSince(since)
	if err != nil {
		t.Fatalf("StatsSince: %v", err)
	}
	if cur.Total != 2 {
		t.Errorf("本期 total = %d, want 2（since 上那条属于本期）", cur.Total)
	}
}

// 转发耗时是在响应首字节（或缓冲响应读完后）回填的：审计行的 id 先落库、
// 耗时后补。回填必须只命中那一行，且 id<=0 的无效调用不得误伤其它行。
func TestUpdateLogForwardBackfillsLatency(t *testing.T) {
	store := newLogStore(t)
	before := mustQuery(t, store, LogFilter{IP: "192.168.1.20"})
	if len(before) == 0 {
		t.Fatal("前置条件不成立：样例库里没有 192.168.1.20 的记录")
	}
	target := before[0]

	store.UpdateLogForward(target.ID, 120, 340)

	after := mustQuery(t, store, LogFilter{IP: "192.168.1.20"})
	var got *LogEntry
	for i := range after {
		if after[i].ID == target.ID {
			got = &after[i]
		}
	}
	if got == nil {
		t.Fatalf("回填后找不到 id=%d 的行", target.ID)
	}
	if got.UpstreamMS == nil || *got.UpstreamMS != 120 {
		t.Errorf("upstream_ms = %v, want 120", got.UpstreamMS)
	}
	if got.LatencyMS != 340 {
		t.Errorf("latency_ms = %d, want 340（回填成检定+转发之和）", got.LatencyMS)
	}
	// 同一 IP 的另一条记录不受影响。
	for _, e := range after {
		if e.ID != target.ID && e.LatencyMS == 340 {
			t.Errorf("id=%d 被误改：latency_ms 也成了 340", e.ID)
		}
	}

	// 无效 id：静默忽略，不得把全表或第 0 行改掉。
	store.UpdateLogForward(0, 9, 9)
	again := mustQuery(t, store, LogFilter{IP: "192.168.1.20"})
	for _, e := range again {
		if e.LatencyMS == 9 {
			t.Fatalf("id=0 的调用改了 id=%d 的行", e.ID)
		}
	}
}
