package config

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// openTestStore 打开一个位于临时目录的 Store：聚合查询只依赖库表本身，
// 因此这里不做任何网络或进程级设置。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func f(v float64) *float64 { return &v }

// 趋势序列：空桶必须补 0、桶边界必须对齐、桶内计数必须与写入的记录一致。
func TestStatsSeriesFillsEmptyBucketsAndAlignsBoundaries(t *testing.T) {
	s := openTestStore(t)

	// 固定「现在」，让桶边界可预测：桶宽 60s，起点对齐到分钟边界。
	until := time.UnixMilli(1_700_000_000_000)
	since := until.Add(-3 * time.Minute)

	// start = floor(until-3min) 对齐到 60s -> 覆盖 since..until 共 4 个桶。
	add := func(offset time.Duration, decision string) {
		s.AddLog(LogEntry{TS: since.Add(offset), Decision: decision})
	}
	add(10*time.Second, "allow")              // 桶 0
	add(20*time.Second, "block")              // 桶 0
	add(2*time.Minute+5*time.Second, "allow") // 桶 2（桶 1 故意留空）
	add(3*time.Minute, "skip")                // 桶 3

	series, err := s.StatsSeries(since, until, 60)
	if err != nil {
		t.Fatalf("stats series: %v", err)
	}
	if len(series) != 4 {
		t.Fatalf("expected 4 buckets, got %d (%+v)", len(series), series)
	}
	for i, b := range series {
		if b.TS%60_000 != 0 {
			t.Errorf("bucket %d ts=%d is not aligned to the 60s boundary", i, b.TS)
		}
		if i > 0 && b.TS-series[i-1].TS != 60_000 {
			t.Errorf("bucket %d ts=%d does not follow the previous bucket by one step", i, b.TS)
		}
	}
	if series[0].TS > since.UnixMilli() {
		t.Errorf("first bucket ts=%d starts after since=%d", series[0].TS, since.UnixMilli())
	}
	if got := series[0]; got.Total != 2 || got.Allowed != 1 || got.Blocked != 1 {
		t.Errorf("bucket 0 = %+v, want total=2 allowed=1 blocked=1", got)
	}
	if got := series[1]; got.Total != 0 || got.Allowed != 0 || got.Blocked != 0 {
		t.Errorf("empty bucket 1 = %+v, want all zeros", got)
	}
	if got := series[2]; got.Total != 1 || got.Allowed != 1 {
		t.Errorf("bucket 2 = %+v, want total=1 allowed=1", got)
	}

	// 序列总和必须等于同一区间内的判定总量（skip 计入 total，不计入 allowed/blocked）。
	var total, allowed, blocked int64
	for _, b := range series {
		total += b.Total
		allowed += b.Allowed
		blocked += b.Blocked
	}
	if total != 4 || allowed != 2 || blocked != 1 {
		t.Errorf("series sums = total %d allowed %d blocked %d, want 4/2/1", total, allowed, blocked)
	}

	// 与既有区间总量口径一致。
	st, err := s.StatsSince(since)
	if err != nil {
		t.Fatalf("stats since: %v", err)
	}
	if st.Total != total || st.Allowed != allowed || st.Blocked != blocked {
		t.Errorf("StatsSince totals (%+v) disagree with the series sums (%d/%d/%d)", st, total, allowed, blocked)
	}
}

// 窗口外的记录不得进入序列。
func TestStatsSeriesExcludesOutOfWindowRows(t *testing.T) {
	s := openTestStore(t)
	until := time.UnixMilli(1_700_000_000_000)
	since := until.Add(-2 * time.Minute)

	s.AddLog(LogEntry{TS: since.Add(-time.Second), Decision: "allow"}) // 早于窗口
	s.AddLog(LogEntry{TS: until.Add(time.Second), Decision: "allow"})  // 晚于窗口
	s.AddLog(LogEntry{TS: since.Add(30 * time.Second), Decision: "block"})

	series, err := s.StatsSeries(since, until, 60)
	if err != nil {
		t.Fatalf("stats series: %v", err)
	}
	var total int64
	for _, b := range series {
		total += b.Total
	}
	if total != 1 {
		t.Errorf("series total = %d, want 1 (仅窗口内的那一条)", total)
	}
}

// 分桶粒度必须是 hours 的确定性函数，且把点数控制在可绘制的规模。
func TestBucketSecondsIsDeterministicAndBounded(t *testing.T) {
	cases := []struct {
		hours int
		want  int
	}{
		{1, 300}, {6, 300}, {12, 900}, {24, 900}, {48, 3600}, {72, 3600},
		{168, 10800}, {720, 86400}, {0, 300}, {-3, 300},
	}
	for _, c := range cases {
		if got := BucketSeconds(c.hours); got != c.want {
			t.Errorf("BucketSeconds(%d) = %d, want %d", c.hours, got, c.want)
		}
		// 同输入必须同输出（前端据此渲染，不能抖动）。
		if got := BucketSeconds(c.hours); got != c.want {
			t.Errorf("BucketSeconds(%d) is not deterministic: %d", c.hours, got)
		}
		if c.hours > 0 {
			if points := c.hours * 3600 / BucketSeconds(c.hours); points > 100 {
				t.Errorf("BucketSeconds(%d) yields %d points, want at most 100", c.hours, points)
			}
		}
	}
}

// 分值直方图：五档边界（含 0.8 与 1.0）、未获分值的单列，以及 skip 记录被排除。
func TestScoreHistogramSlotsAndUnscored(t *testing.T) {
	s := openTestStore(t)
	since := time.UnixMilli(1_700_000_000_000)
	ts := since.Add(time.Second)

	rows := []struct {
		score    *float64
		decision string
	}{
		{f(0), "block"},
		{f(0.19), "block"},
		{f(0.2), "block"}, // 恰好落在第二档下界
		{f(0.8), "allow"}, // 恰好落在末档下界
		{f(1.0), "allow"}, // 末档上界（含 1.0）
		{nil, "error"},    // 已送检但 JEV 未返回分值
		{nil, "skip"},     // 未送检，必须排除
		{f(0.55), "skip"}, // 未送检，即使有分值也排除
	}
	for _, r := range rows {
		s.AddLog(LogEntry{TS: ts, Decision: r.decision, Score: r.score})
	}

	h, err := s.ScoreHistogram(since)
	if err != nil {
		t.Fatalf("score histogram: %v", err)
	}
	want := []int64{2, 1, 0, 0, 2}
	if len(h.Counts) != len(want) {
		t.Fatalf("got %d slots, want %d", len(h.Counts), len(want))
	}
	for i := range want {
		if h.Counts[i] != want[i] {
			t.Errorf("slot %d = %d, want %d (all slots: %v)", i, h.Counts[i], want[i], h.Counts)
		}
	}
	if h.Unscored != 1 {
		t.Errorf("unscored = %d, want 1", h.Unscored)
	}

	// 五档之和 + unscored 必须等于区间内已送检（非 skip）的记录数。
	var sum int64
	for _, c := range h.Counts {
		sum += c
	}
	if got := sum + h.Unscored; got != 6 {
		t.Errorf("slots+unscored = %d, want 6 (区间内已送检记录数)", got)
	}
}

// 窗口外的记录不进入直方图；区间内没有记录时返回全 0 而不是 nil。
func TestScoreHistogramWindowAndEmpty(t *testing.T) {
	s := openTestStore(t)
	since := time.UnixMilli(1_700_000_000_000)

	h, err := s.ScoreHistogram(since)
	if err != nil {
		t.Fatalf("score histogram: %v", err)
	}
	if len(h.Counts) != ScoreSlots {
		t.Fatalf("empty histogram must still expose %d slots, got %d", ScoreSlots, len(h.Counts))
	}
	for i, c := range h.Counts {
		if c != 0 {
			t.Errorf("slot %d = %d, want 0", i, c)
		}
	}
	if h.Unscored != 0 {
		t.Errorf("unscored = %d, want 0", h.Unscored)
	}

	s.AddLog(LogEntry{TS: since.Add(-time.Millisecond), Decision: "allow", Score: f(0.5)})
	h, err = s.ScoreHistogram(since)
	if err != nil {
		t.Fatalf("score histogram: %v", err)
	}
	var sum int64
	for _, c := range h.Counts {
		sum += c
	}
	if sum != 0 || h.Unscored != 0 {
		t.Errorf("out-of-window row leaked into the histogram: %v unscored=%d", h.Counts, h.Unscored)
	}
}

// 桶内计数用整数累加，不引入浮点误差（防止前端显示 0.30000000000000004 一类数字）。
func TestStatsSeriesCountsAreIntegral(t *testing.T) {
	s := openTestStore(t)
	until := time.UnixMilli(1_700_000_000_000)
	since := until.Add(-time.Minute)
	for i := 0; i < 7; i++ {
		s.AddLog(LogEntry{TS: since.Add(time.Duration(i) * time.Second), Decision: "allow"})
	}
	series, err := s.StatsSeries(since, until, 60)
	if err != nil {
		t.Fatalf("stats series: %v", err)
	}
	var total int64
	for _, b := range series {
		total += b.Total
		if math.Trunc(float64(b.Total)) != float64(b.Total) {
			t.Errorf("bucket total %v is not integral", b.Total)
		}
	}
	if total != 7 {
		t.Errorf("series total = %d, want 7", total)
	}
}
