package main

// Tests for the metrics that must be on disk while the engine is down (Constitution 3.2, 3.9): the per-bot meter, the
// overview, the level book (core instance included), and friends.json (decayed score, effective score, decay clock).

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := *dataDir
	*dataDir = dir
	t.Cleanup(func() { *dataDir = old })
	return dir
}

// The per-bot meter is written to meter_bots.json: every bot online in the last hour, with its kills/min and zeny/h.
func TestMeterBotsOnDisk(t *testing.T) {
	dir := withDataDir(t)
	now := time.Now()
	rows := []*meterRow{row("mate", 1, 30, 10, 0, 0, 0), row("solo", 2, 30, 10, 0, 0, 0)}
	rows[1].Win[mw60].Min = 0 // not online in the last hour: left out
	meterDerive(rows, [mwN]float64{5, 5, 5})
	v := meterBuild(rows, now)
	meterWriteBots(v, now)
	raw, err := os.ReadFile(filepath.Join(dir, "meter_bots.json"))
	if err != nil {
		t.Fatalf("meter_bots.json not written: %v", err)
	}
	var f meterBotsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Total != 1 || len(f.Bots) != 1 || f.Bots[0].Name != "mate" || f.Window != "1h" {
		t.Fatalf("bots file %+v", f)
	}
	if f.Bots[0].KPM <= 0 {
		t.Fatalf("kpm not written: %+v", f.Bots[0])
	}
}

// The overview is saved as /api/overview builds it.
func TestOverviewOnDisk(t *testing.T) {
	dir := withDataDir(t)
	d := &dashCollector{buckets: map[int64]*minuteBucket{}, start: time.Now()}
	d.saveOverview(nil)
	raw, err := os.ReadFile(filepath.Join(dir, "overview.json"))
	if err != nil {
		t.Fatalf("overview.json not written: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		t.Fatalf("overview.json: %v %s", err, raw)
	}
}

// Core instance characters go to levels.core.json; the main instance merges them (newer sighting wins).
func TestLevelBookCoreMerge(t *testing.T) {
	dir := withDataDir(t)
	oldOnly := *onlyNames
	*onlyNames = "Zendancer84"
	defer func() { *onlyNames = oldOnly }()
	if levelBookFile() != "levels.core.json" {
		t.Fatalf("core level book file %q", levelBookFile())
	}
	*onlyNames = ""
	if levelBookFile() != "levels.json" {
		t.Fatalf("main level book file %q", levelBookFile())
	}
	core := map[string]*lvEntry{"Zendancer84": {Name: "Zendancer84", Level: 81, Seen: 200}, "Old": {Name: "Old", Level: 5, Seen: 100}}
	raw, _ := json.Marshal(core)
	os.WriteFile(filepath.Join(dir, "levels.core.json"), raw, 0644)
	levelBook.Lock()
	levelBook.m = map[string]*lvEntry{"Old": {Name: "Old", Level: 9, Seen: 300}}
	levelBook.Unlock()
	mergeLevelBook(filepath.Join(dir, "levels.core.json"))
	levelBook.Lock()
	z, o := levelBook.m["Zendancer84"], levelBook.m["Old"]
	levelBook.Unlock()
	if z == nil || z.Level != 81 {
		t.Fatalf("core character not merged: %+v", z)
	}
	if o.Level != 9 { // main's sighting is newer: kept
		t.Fatalf("newer entry replaced: %+v", o)
	}
}

// Ayla is in the level book from status.json (she is OpenKore, not a Legion bot).
func TestLevelBookAyla(t *testing.T) {
	dir := withDataDir(t)
	f := filepath.Join(dir, "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", f)
	b, _ := json.Marshal(humanStatus{Name: "Ayla", Online: true, BaseLevel: 83, JobID: 12, Guild: "Starveil", TS: 1700000000})
	os.WriteFile(f, b, 0644)
	e := aylaLevelEntry(time.Now().Unix())
	if e == nil || e.Name != "Ayla" || e.Level != 83 || e.Seen != 1700000000 {
		t.Fatalf("ayla entry %+v", e)
	}
	updateLevelBook(nil, true)
	raw, err := os.ReadFile(filepath.Join(dir, "levels.json"))
	if err != nil {
		t.Fatalf("levels.json not written: %v", err)
	}
	var m map[string]*lvEntry
	json.Unmarshal(raw, &m)
	if m["Ayla"] == nil || m["Ayla"].Level != 83 {
		t.Fatalf("levels.json has no Ayla: %s", raw)
	}
}

// friends.json keeps the decayed score, the effective score the engine shows, and the decay clock: a restart resumes
// the decay where it stopped instead of decaying the same time twice.
func TestFriendsSaveKeepsDecay(t *testing.T) {
	withDataDir(t)
	resetFriends()
	now := time.Now()
	last := now.Add(-20 * 24 * time.Hour)
	decayedTo := now.Add(-friendHalfLife)
	friends.Lock()
	friends.pairs["Ann|Bob"] = &friendPair{A: "Ann", B: "Bob", Score: 40, Last: last, decayAt: decayedTo}
	friends.bonus["Ann|Bob"] = 5
	friends.dirty = true
	friends.Unlock()
	friendsSave(true)
	resetFriends()
	friendsLoad()
	friends.Lock()
	p := friends.pairs["Ann|Bob"]
	friends.Unlock()
	if p == nil {
		t.Fatal("pair not loaded")
	}
	if d := p.decayAt.Sub(decayedTo); d > time.Second || d < -time.Second {
		t.Fatalf("decay clock %v, want the saved %v", p.decayAt, decayedTo)
	}
	if math.Abs(p.Score-40) > 1e-9 {
		t.Fatalf("raw score changed on load: %v", p.Score)
	}
	raw, err := os.ReadFile(friendFile())
	if err != nil {
		t.Fatal(err)
	}
	var d friendFileData
	json.Unmarshal(raw, &d)
	if len(d.Pairs) != 1 || d.Pairs[0].Eff != 42.5 { // raw 40 + the memory bonus (5 one way, 0 back) / 2
		t.Fatalf("saved eff %+v", d.Pairs)
	}
}

// The counters snapshot runs every meter_persist_sec (default 300); a value under a minute is ignored.
func TestMeterPersistTuning(t *testing.T) {
	dir := withDataDir(t)
	os.WriteFile(filepath.Join(dir, "tuning.json"), []byte(`{"meter_persist_sec": 30}`), 0644)
	meterLoadTuning()
	if got := meterTune().PersistSec; got != 300 {
		t.Fatalf("persist sec %d, want 300 (values under 60 fall back)", got)
	}
	os.WriteFile(filepath.Join(dir, "tuning.json"), []byte(`{"meter_persist_sec": 600}`), 0644)
	meterLoadTuning()
	if got := meterTune().PersistSec; got != 600 {
		t.Fatalf("persist sec %d, want 600", got)
	}
	meterTuneCur.Store(nil)
}

// A crash leaves ".meter_state*.tmp" behind; the start removes it, but only this instance's own temp files.
func TestMeterOrphanTempRemoved(t *testing.T) {
	dir := withDataDir(t)
	mine := filepath.Join(dir, ".meter_state.json.gz.123.tmp")
	other := filepath.Join(dir, ".meter_state.core.json.gz.456.tmp")
	os.WriteFile(mine, []byte("x"), 0644)
	os.WriteFile(other, []byte("x"), 0644)
	meterRestoreLoad()
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatal("orphan temp of this instance not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("the other instance's temp file was removed")
	}
}
