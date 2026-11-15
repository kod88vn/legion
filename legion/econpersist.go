package main

// econpersist.go (economy run 10, market mind: "economy counters reset at each deploy - persist them like
// markdowns"): the /api/economy counters (deal / shop / mail counts, match misses, trades, vend sales, gifts, cards)
// are saved every minute to <data>/econ_counters[_core].json and added back at start-up, so the numbers are
// cumulative across deploys. "counters_since" says when counting began.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type econSaved struct {
	Since     time.Time        `json:"since"`
	Saved     time.Time        `json:"saved"`
	Counts    map[string]int64 `json:"counts"`
	Misses    map[string]int64 `json:"misses"`
	MissItems map[string]int64 `json:"miss_items"`
	Trades    int64            `json:"trades"`
	Failed    int64            `json:"failed"`
	VendSales int64            `json:"vend_sales"`
	VendZeny  int64            `json:"vend_zeny"`
	GearGifts int64            `json:"gear_gifts"`
	Cards     int64            `json:"cards"`
}

var econSince atomic.Value // time.Time

func econCountersFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "econ_counters_core.json")
	}
	return filepath.Join(*dataDir, "econ_counters.json")
}

// loadEconCounters (start-up, before the first round): add the last run's counters to this run's.
func loadEconCounters() {
	econSince.Store(time.Now())
	raw, err := os.ReadFile(econCountersFile())
	if err != nil {
		return
	}
	var s econSaved
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	if !s.Since.IsZero() {
		econSince.Store(s.Since)
	}
	econMissMu.Lock()
	for k, v := range s.Counts {
		econCountN[k] += v
	}
	for k, v := range s.Misses {
		econMissN[k] += v
	}
	for k, v := range s.MissItems {
		econMissItemN[k] += v
	}
	econMissMu.Unlock()
	atomic.AddInt64(&econTrades, s.Trades)
	atomic.AddInt64(&econ.failed, s.Failed)
	atomic.AddInt64(&vendSales, s.VendSales)
	atomic.AddInt64(&vendSalesZeny, s.VendZeny)
	atomic.AddInt64(&gearGifts, s.GearGifts)
	atomic.AddInt64(&cardsInserted, s.Cards)
}

// saveEconCounters writes the cumulative counters (no lock held).
func saveEconCounters(now time.Time) {
	s := econSaved{Saved: now, Counts: econCounts(), Misses: econMisses(), MissItems: econMissesItems(),
		Trades: atomic.LoadInt64(&econTrades), Failed: atomic.LoadInt64(&econ.failed), VendSales: atomic.LoadInt64(&vendSales),
		VendZeny: atomic.LoadInt64(&vendSalesZeny), GearGifts: atomic.LoadInt64(&gearGifts), Cards: atomic.LoadInt64(&cardsInserted)}
	if t, ok := econSince.Load().(time.Time); ok {
		s.Since = t
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := econCountersFile() + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		os.Rename(tmp, econCountersFile())
	}
}

// countersSince: when the cumulative counters began
func countersSince() time.Time {
	t, _ := econSince.Load().(time.Time)
	return t
}

// dealFailReasons: why agreed deals fell through (cumulative), from the "deal ended: <reason>" misses
func dealFailReasons() map[string]int64 {
	out := map[string]int64{}
	for k, v := range econMisses() {
		if len(k) > 12 && k[:12] == "deal ended: " {
			out[k[12:]] = v
		}
	}
	return out
}
