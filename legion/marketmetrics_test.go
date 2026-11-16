package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures only, no live data (G23): the counters write to a t.TempDir().

// withMMData points the metrics at a throwaway data dir and clears the counters.
func withMMData(t *testing.T) string {
	t.Helper()
	oldData := *dataDir
	dir := t.TempDir()
	*dataDir = dir
	resetMM()
	t.Cleanup(func() {
		*dataDir = oldData
		resetMM()
	})
	return dir
}

func resetMM() {
	mm.Lock()
	mm.ensured = false
	mm.since = time.Time{}
	mm.rounds = 0
	mm.offers, mm.accepts, mm.deaths, mm.farmerH = 0, 0, 0, 0
	mmEnsureLocked()
	mm.Unlock()
}

func blockJSON(t *testing.T, now time.Time) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(marketMetricsBlock(now))
	if err != nil {
		t.Fatalf("block does not marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("block does not unmarshal: %v", err)
	}
	return out
}

func sub(m map[string]interface{}, k string) map[string]interface{} {
	v, _ := m[k].(map[string]interface{})
	return v
}

func TestMMStddevFormula(t *testing.T) {
	if _, ok := mmStddev(2, 2, 2); ok {
		t.Fatal("fewer than mmPriceMinN samples must not give a stddev")
	}
	// ratios 1,1,1: no spread
	if sd, ok := mmStddev(3, 3, 3); !ok || sd != 0 {
		t.Fatalf("constant ratio: sd=%v ok=%v", sd, ok)
	}
	// ratios 0.5, 1.5, 0.5, 1.5: mean 1, population variance 0.25, stddev 0.5
	if sd, ok := mmStddev(4, 4, 0.25+0.25+2.25+2.25); !ok || math.Abs(sd-0.5) > 1e-9 {
		t.Fatalf("fixture stddev want 0.5 got %v ok=%v", sd, ok)
	}
}

func TestMMCoverageFormula(t *testing.T) {
	if c, ok := mmCoverage(6, 12); !ok || c != 0.5 {
		t.Fatalf("6 of 12: %v %v", c, ok)
	}
	if _, ok := mmCoverage(3, 0); ok {
		t.Fatal("no need must give no coverage (null), not 0 or 1")
	}
}

func TestMMEmptyDataIsNull(t *testing.T) {
	withMMData(t)
	oldMeter := meterCur.Load()
	meterCur.Store(nil)
	defer meterCur.Store(oldMeter)
	b := blockJSON(t, time.Now())
	cov := sub(b, "coverage")
	if cov["status"] != "no_data" || cov["overall"] != nil {
		t.Fatalf("empty coverage: %v", cov)
	}
	ps := sub(b, "price_stability")
	if ps["status"] != "no_data" || ps["overall"] != nil || ps["within_target"] != nil {
		t.Fatalf("empty price stability: %v", ps)
	}
	f := sub(b, "farmers")
	if f["status"] != "no_data" || f["accept_rate"] != nil || f["deaths_per_hour"] != nil || f["deaths"].(float64) != 0 {
		t.Fatalf("empty farmers: %v", f)
	}
	k := sub(b, "kite")
	if k["steps"].(float64) == 0 && k["deaths_per_step"] != nil {
		t.Fatalf("no steps must give a null ratio: %v", k)
	}
	z := sub(b, "zeny_per_hour")
	if z["status"] != "no_data" {
		t.Fatalf("no meter view must say no_data: %v", z)
	}
}

func TestMMCoverageByBandAndItemFromRounds(t *testing.T) {
	withMMData(t)
	// a 10:10 round: 3 bots short of item 501 in novice; 10:20 peak 5; 11:05 closes the 10:00 hour (peak 5)
	base := time.Date(2026, 11, 15, 10, 10, 0, 0, time.UTC)
	k := mmKey{Band: "novice", Item: 501}
	mmApplyRound(base, map[mmKey]int{k: 3}, nil)
	mmApplyRound(base.Add(10*time.Minute), map[mmKey]int{k: 5}, nil)
	mmApplyRound(base.Add(20*time.Minute), map[mmKey]int{k: 2}, nil)
	noteMMSupplied("novice", 501, 2)
	b := blockJSON(t, base.Add(20*time.Minute)) // the hour is still open: its peak 5 counts already
	cov := sub(b, "coverage")
	if cov["status"] != "ok" || cov["overall"].(float64) != 0.4 {
		t.Fatalf("open hour: coverage %v (want 2 supplied / 5 need = 0.4)", cov)
	}
	rows := cov["by_band"].([]interface{})
	if len(rows) != 1 || rows[0].(map[string]interface{})["band"] != "novice" {
		t.Fatalf("by_band: %v", rows)
	}
	// closed on the next hour: the need is kept, not counted twice
	mmApplyRound(base.Add(60*time.Minute), map[mmKey]int{}, nil)
	mmApplyRound(base.Add(61*time.Minute), map[mmKey]int{}, nil)
	mm.Lock()
	closed := mm.need["novice"][501]
	mm.Unlock()
	if closed != 5 {
		t.Fatalf("closed hour need %v want 5", closed)
	}
}

func TestMMPriceStabilityFromRounds(t *testing.T) {
	withMMData(t)
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	// ratios 1.0, 2.0, 1.0 for item 7: mean 4/3, variance 2/9, stddev 0.471
	for i, r := range []float64{1, 2, 1} {
		mmApplyRound(t0.Add(time.Duration(i)*time.Minute), nil, map[int]float64{7: r})
	}
	b := blockJSON(t, t0)
	ps := sub(b, "price_stability")
	if ps["status"] != "ok" || ps["items_counted"].(float64) != 1 {
		t.Fatalf("price stability: %v", ps)
	}
	items := ps["items"].([]interface{})
	sd := items[0].(map[string]interface{})["stddev"].(float64)
	if math.Abs(sd-0.471) > 0.0005 {
		t.Fatalf("item 7 stddev %v want 0.471", sd)
	}
	if ps["within_target"] != true || ps["target"].(float64) != 1.5 {
		t.Fatalf("within target: %v", ps)
	}
}

func TestMMCountersPersistAcrossRuns(t *testing.T) {
	dir := withMMData(t)
	t0 := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	k := mmKey{Band: "novice", Item: 501}
	mmApplyRound(t0, map[mmKey]int{k: 4}, map[int]float64{501: 1})
	mmApplyRound(t0.Add(time.Hour), map[mmKey]int{}, map[int]float64{501: 2})
	noteMMSupplied("novice", 501, 3)
	noteMMFarmerOffer()
	noteMMFarmerDecline("unsafe map")
	noteMMFarmerDeath()
	noteMMFarmerHours(2.5)
	saveMarketMetrics(t0.Add(time.Hour))
	if _, err := os.Stat(filepath.Join(dir, "market_metrics.json")); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "market_metrics.json.tmp")); err == nil {
		t.Fatal("temp file left behind: the write is not atomic")
	}
	before := blockJSON(t, t0.Add(2*time.Hour))

	// restart: memory empty, the file is read back
	resetMM()
	loadMarketMetrics()
	after := blockJSON(t, t0.Add(2*time.Hour))
	for _, key := range []string{"coverage", "price_stability", "farmers"} {
		bb, _ := json.Marshal(sub(before, key))
		aa, _ := json.Marshal(sub(after, key))
		if string(bb) != string(aa) {
			t.Fatalf("%s changed across a reload:\nbefore %s\nafter  %s", key, bb, aa)
		}
	}
	f := sub(after, "farmers")
	if f["offers"].(float64) != 1 || f["deaths"].(float64) != 1 || f["farmer_hours"].(float64) != 2.5 {
		t.Fatalf("farmer counters after reload: %v", f)
	}
	if !strings.Contains(string(mustJSON(t, f["declined"])), "unsafe map") {
		t.Fatalf("decline reason lost: %v", f["declined"])
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMMFeedZenyLineGoesOnlyWhenFed(t *testing.T) {
	all := []string{"zeny_per_hour: meter feed not wired (WP8)", "bands: approximate", "mob speed and map danger: x"}
	if got := meFeedMissingNow(all, true); len(got) != 2 || strings.HasPrefix(got[0], "zeny_per_hour") {
		t.Fatalf("fed: %v", got)
	}
	if got := meFeedMissingNow(all, false); len(got) != 3 {
		t.Fatalf("not fed must keep every line: %v", got)
	}
	oldMeter := meterCur.Load()
	meterCur.Store(nil)
	defer meterCur.Store(oldMeter)
	if z := meZenyFeed(); len(z) != 0 {
		t.Fatalf("no meter view: zeny feed must be empty, got %v", z)
	}
}

func TestMMEngineAPIAdditive(t *testing.T) {
	withMMData(t)
	mux := http.NewServeMux()
	registerMarketEngine(mux)
	meterHandlers(mux)

	// flag off: the old keys stay, "metrics" is added
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/engine", nil))
	var off map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &off)
	if off["status"] != "off" || off["flag"] != "market_engine_on" || off["note"] == nil || off["metrics"] == nil {
		t.Fatalf("off response: %s", rec.Body.String())
	}

	// flag on with a stored run: every old key is still there
	prev := tune()
	on := defaultTuning
	on.MarketEngineOn = true
	curTuning.Store(&on)
	defer curTuning.Store(prev)
	res := meEvaluate(MESnapshot{Now: meNow, Items: []MEItem{{ID: 1, Name: "A", Price: 100, Demand: 4, Maps: meMaps("a", 1, 1, 10)}},
		Farmers: meFarmers(4, 30)})
	meState.Lock()
	meState.result, meState.runs = &res, 1
	meState.Unlock()
	defer func() {
		meState.Lock()
		meState.result, meState.runs = nil, 0
		meState.Unlock()
	}()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/engine", nil))
	var on2 map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &on2)
	for _, k := range []string{"status", "runs", "last_run", "farmers", "caps", "ordered", "unassigned", "distribution", "needs", "orders", "metrics"} {
		if _, ok := on2[k]; !ok {
			t.Errorf("engine response lost key %q: %s", k, rec.Body.String())
		}
	}
	if on2["status"] != "ok" {
		t.Fatalf("status %v", on2["status"])
	}

	// the meter route is registered and serves the block
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meter/market", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"price_stability"`) {
		t.Fatalf("/api/meter/market: %d %s", rec.Code, rec.Body.String())
	}
}
