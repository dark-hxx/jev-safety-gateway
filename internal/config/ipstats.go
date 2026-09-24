package config

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

// Danger-rating thresholds for an IP's computed score (see ipDanger). A score at
// or above dangerCrit is "crit", and so on down; below dangerMed is "low". They
// are tuned so that only an IP that is both judged unsafe and blocked repeatedly
// at volume reaches the top band — a single unlucky block stays low.
const (
	dangerCrit = 0.55
	dangerHigh = 0.30
	dangerMed  = 0.12
)

// ipDangerFullVolume is the request count at which the frequency weight reaches
// 1. Below it the weight scales linearly, so a handful of blocks from a one-off
// probe cannot rate as high as a sustained abuser.
const ipDangerFullVolume = 40.0

// IP list sizing. The default keeps the dashboard payload small; the max bounds
// a hostile ?limit=. The window-wide summary is computed regardless of the list
// limit.
const (
	ipStatsDefaultLimit = 50
	ipStatsMaxLimit     = 500
)

// IPStats aggregates the audit log by client IP over the window and returns the
// most dangerous IPs first, plus a window-wide summary. The danger rating is
// deterministic and explainable — see ipDanger — so the console can justify each
// rating from the raw counts it also carries.
//
// The summary is computed over every distinct IP in the window; only the
// returned slice is limited. Rows with no IP (a skip logged before the client
// was known) are excluded, mirroring LogModels' handling of empty model values.
func (s *Store) IPStats(since time.Time, limit int) ([]IPStat, IPStatsSummary, error) {
	if limit <= 0 {
		limit = ipStatsDefaultLimit
	}
	if limit > ipStatsMaxLimit {
		limit = ipStatsMaxLimit
	}
	sinceMS := since.UnixMilli()

	rows, err := s.db.Query(
		`SELECT ip,
			COUNT(*),
			SUM(CASE WHEN decision='allow' THEN 1 ELSE 0 END),
			SUM(CASE WHEN decision='block' THEN 1 ELSE 0 END),
			SUM(CASE WHEN decision='skip'  THEN 1 ELSE 0 END),
			SUM(CASE WHEN decision='error' THEN 1 ELSE 0 END),
			COUNT(score), AVG(score), MIN(score),
			MIN(ts), MAX(ts)
		 FROM logs WHERE ts>=? AND ip<>'' GROUP BY ip`, sinceMS)
	if err != nil {
		return nil, IPStatsSummary{}, err
	}
	defer rows.Close()

	var (
		stats   []IPStat
		summary IPStatsSummary
	)
	for rows.Next() {
		var (
			st                 IPStat
			avgScore, minScore sql.NullFloat64
		)
		if err := rows.Scan(&st.IP, &st.Total, &st.Allowed, &st.Blocked,
			&st.Skipped, &st.Errors, &st.Scored, &avgScore, &minScore,
			&st.FirstSeen, &st.LastSeen); err != nil {
			return nil, IPStatsSummary{}, err
		}
		if avgScore.Valid {
			v := avgScore.Float64
			st.AvgScore = &v
		}
		if minScore.Valid {
			v := minScore.Float64
			st.MinScore = &v
		}
		st.Danger = ipDanger(st)
		st.Level = dangerLevel(st.Danger)

		summary.DistinctIPs++
		summary.TotalEvents += st.Total
		summary.TotalBlocked += st.Blocked
		if st.Blocked > 0 {
			summary.BlockedIPs++
		}
		switch st.Level {
		case "crit":
			summary.Crit++
		case "high":
			summary.High++
		}
		stats = append(stats, st)
	}
	if err := rows.Err(); err != nil {
		return nil, IPStatsSummary{}, err
	}

	// Most dangerous first; ties broken by raw block count, then recency, then IP
	// so the order is stable across identical inputs.
	sort.Slice(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.Danger != b.Danger {
			return a.Danger > b.Danger
		}
		if a.Blocked != b.Blocked {
			return a.Blocked > b.Blocked
		}
		if a.LastSeen != b.LastSeen {
			return a.LastSeen > b.LastSeen
		}
		return a.IP < b.IP
	})
	if len(stats) > limit {
		stats = stats[:limit]
	}

	if err := s.attachLastReason(stats, sinceMS); err != nil {
		return nil, IPStatsSummary{}, err
	}
	return stats, summary, nil
}

// ipDanger computes a deterministic 0–1 danger rating for one IP:
//
//	unsafety   = max(0, 1 - avgScore)        how unsafe its judged content looked
//	blockRate  = blocked / scored            share of judged rows that were blocked
//	freqWeight = min(1, total / fullVolume)  suppress low-volume noise
//	danger     = unsafety * blockRate * freqWeight
//
// blockRate is taken over SCORED rows, not total, on purpose: benign skips
// (non-POST, disabled gateway, un-inspectable bodies) must not dilute the rate
// of an IP that gets blocked every time it is actually judged. An IP with no
// scored rows has no evidence of intent and rates 0. Note avgScore is JEV's
// "higher = safer" probability, so 1-avgScore is the unsafety.
func ipDanger(st IPStat) float64 {
	if st.Scored == 0 || st.AvgScore == nil {
		return 0
	}
	unsafety := 1 - *st.AvgScore
	if unsafety < 0 {
		unsafety = 0
	}
	blockRate := float64(st.Blocked) / float64(st.Scored)
	freqWeight := float64(st.Total) / ipDangerFullVolume
	if freqWeight > 1 {
		freqWeight = 1
	}
	d := unsafety * blockRate * freqWeight
	if d > 1 {
		d = 1
	}
	return d
}

// dangerLevel buckets a danger rating into the console's four bands.
func dangerLevel(d float64) string {
	switch {
	case d >= dangerCrit:
		return "crit"
	case d >= dangerHigh:
		return "high"
	case d >= dangerMed:
		return "med"
	default:
		return "low"
	}
}

// attachLastReason fills LastReason for the given IPs with the reason of their
// most recent row in the window. It is one bounded query over just the returned
// IPs (never the whole table): the newest row per IP is found by MAX(id), then
// joined back for its reason. id is monotonic with insertion, so MAX(id) is the
// latest row even when two rows share a millisecond ts.
func (s *Store) attachLastReason(stats []IPStat, sinceMS int64) error {
	if len(stats) == 0 {
		return nil
	}
	placeholders := make([]string, len(stats))
	args := make([]interface{}, 0, len(stats)+1)
	args = append(args, sinceMS)
	for i, st := range stats {
		placeholders[i] = "?"
		args = append(args, st.IP)
	}
	in := strings.Join(placeholders, ",")

	rows, err := s.db.Query(
		`SELECT l.ip, l.reason FROM logs l
		 JOIN (SELECT ip, MAX(id) AS mid FROM logs
		       WHERE ts>=? AND ip IN (`+in+`) GROUP BY ip) m
		 ON l.id = m.mid`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	reason := make(map[string]string, len(stats))
	for rows.Next() {
		var ip, r string
		if err := rows.Scan(&ip, &r); err != nil {
			return err
		}
		reason[ip] = r
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range stats {
		stats[i].LastReason = reason[stats[i].IP]
	}
	return nil
}
