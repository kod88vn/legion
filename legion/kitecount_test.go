package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withKiteData points the counters at a throwaway data dir (G23) and clears the in-memory counts.
func withKiteData(t *testing.T) string {
	t.Helper()
	oldData := *dataDir
	oldOnly := *onlyNames
	dir := t.TempDir()
	*dataDir, *onlyNames = dir, ""
	kiteMu.Lock()
	kiteSteps = map[string]int64{}
	kiteDeaths = map[string]int64{}
	kiteSince = time.Time{}
	kiteMu.Unlock()
	t.Cleanup(func() {
		*dataDir, *onlyNames = oldData, oldOnly
		kiteMu.Lock()
		kiteSteps = map[string]int64{}
		kiteDeaths = map[string]int64{}
		kiteSince = time.Time{}
		kiteMu.Unlock()
	})
	return dir
}

func TestKiteDeathWindow(t *testing.T) {
	now := time.Now()
	if kiteDeathCounts(time.Time{}, now) {
		t.Fatal("a bot that never kited must not count a death as a kite death")
	}
	if !kiteDeathCounts(now.Add(-30*time.Second), now) {
		t.Fatal("a death 30 s after a kiting step must count")
	}
	if kiteDeathCounts(now.Add(-kiteDeathWindow-time.Second), now) {
		t.Fatal("a death more than the window after the last step must not count")
	}
}

func TestKiteCountersPersistAcrossRuns(t *testing.T) {
	dir := withKiteData(t)
	noteKiteStep("Alpha")
	noteKiteStep("Alpha")
	noteKiteStep("Beta")
	kiteMu.Lock()
	kiteDeaths["Alpha"]++
	kiteMu.Unlock()
	saveKiteCounters(time.Now())

	if _, err := os.Stat(filepath.Join(dir, "kite_counters.json")); err != nil {
		t.Fatalf("counters file not written: %v", err)
	}
	// a restart: memory is empty, the file is read back and added
	kiteMu.Lock()
	kiteSteps = map[string]int64{}
	kiteDeaths = map[string]int64{}
	kiteMu.Unlock()
	loadKiteCounters()
	c := kiteCounters()
	if c["kite_steps"].(int64) != 3 || c["kite_deaths"].(int64) != 1 {
		t.Fatalf("totals after reload: steps %v deaths %v, want 3 and 1", c["kite_steps"], c["kite_deaths"])
	}
}

func TestKiteCountersByBot(t *testing.T) {
	withKiteData(t)
	noteKiteStep("Alpha")
	noteKiteStep("Alpha")
	kiteMu.Lock()
	kiteDeaths["Alpha"] = 1
	kiteMu.Unlock()
	by := kiteCounters()["by_bot"].(map[string][2]int64)
	if by["Alpha"] != [2]int64{2, 1} {
		t.Fatalf("by_bot[Alpha] = %v, want [2 1]", by["Alpha"])
	}
}

// The Creamy Card is item 4040 in rathena's item_db_etc.yml (Creamy_Card, Type Card, Both_Accessory, script
// AL_TELEPORT,1). The engine's two constants must name that card.
func TestCreamyCardIDMatchesItemDB(t *testing.T) {
	if itemCreamy != 4040 || itemCreamyCard != 4040 {
		t.Fatalf("Creamy Card ids: itemCreamy=%d itemCreamyCard=%d, item_db says 4040", itemCreamy, itemCreamyCard)
	}
}
