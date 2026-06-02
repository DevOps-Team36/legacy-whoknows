package searchlog

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"time"
)

type Stats struct {
	Total        int
	WithResults  int
	ZeroResults  int
	HitRate      float64
	TopMissed    []string
}

func ReadStats() (Stats, error) {
	path := os.Getenv("WHOKNOWS_SEARCH_LOG_PATH")
	if path == "" {
		path = defaultLogPath
	}

	f, err := os.Open(path) // #nosec G304,G703 -- path comes from deployment config
	if err != nil {
		if os.IsNotExist(err) {
			return Stats{}, nil
		}
		return Stats{}, err
	}
	defer func() { _ = f.Close() }()

	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	missedCounts := map[string]int{}
	var stats Stats

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil || t.Before(cutoff) {
			continue
		}
		stats.Total++
		if e.ResultCount > 0 {
			stats.WithResults++
		} else {
			missedCounts[e.Query]++
		}
	}

	stats.ZeroResults = stats.Total - stats.WithResults
	if stats.Total > 0 {
		stats.HitRate = float64(stats.WithResults) / float64(stats.Total) * 100
	}

	type kv struct {
		query string
		count int
	}
	ranked := make([]kv, 0, len(missedCounts))
	for q, c := range missedCounts {
		ranked = append(ranked, kv{q, c})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].count > ranked[j].count })

	const topN = 5
	for i := 0; i < len(ranked) && i < topN; i++ {
		stats.TopMissed = append(stats.TopMissed, ranked[i].query)
	}

	return stats, scanner.Err()
}
