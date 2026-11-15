package main

import (
	"testing"
	"time"
)

func TestMapReadsEmpty(t *testing.T) {
	now := time.Now()
	for i := 0; i < 40; i++ {
		seenAdd("test_empty", i%20/19, now) // 2 sightings in 40 samples (QA pay_fild01)
		seenAdd("test_full", 6, now)
	}
	for i := 0; i < 5; i++ {
		seenAdd("test_few", 0, now) // too few samples to judge
	}
	if !mapReadsEmpty("test_empty") {
		t.Error("test_empty should read empty")
	}
	if mapReadsEmpty("test_full") || mapReadsEmpty("test_few") || mapReadsEmpty("test_unknown") {
		t.Error("full / few-sample / unknown maps must not read empty")
	}
	// decay: 30 min later the old samples weigh ~5%, below the minimum - the map is tried again
	mapSeen.Lock()
	mapSeen.m["test_empty"].at = now.Add(-30 * time.Minute)
	mapSeen.Unlock()
	if mapReadsEmpty("test_empty") {
		t.Error("an empty reading must decay away")
	}
}

func TestMapReadsThin(t *testing.T) {
	now := time.Now()
	for i := 0; i < 40; i++ {
		seenAdd("test_thin", i%2*2, now) // ~1 monster per bot (QA pay_fild01 0.7, mjolnir_02 1.5)
		seenAdd("test_ok", 5, now)
	}
	if !mapReadsThin("test_thin") || mapReadsEmpty("test_thin") {
		t.Error("test_thin should read thin, not empty")
	}
	if mapReadsThin("test_ok") || mapReadsThin("test_unknown") {
		t.Error("a populated / unknown map must not read thin")
	}
	if mapHasRoom("test_thin", 1) || !mapCrowded("test_thin") {
		t.Error("a thin map counts as full")
	}
}

func TestSightNear(t *testing.T) {
	now := time.Now()
	sightAdd("test_sight", mobSight{x: 50, y: 50, lv: 10, at: now})
	sightAdd("test_sight", mobSight{x: 30, y: 30, lv: 40, at: now})                  // too high
	sightAdd("test_sight", mobSight{x: 20, y: 20, lv: 5, at: now.Add(-time.Minute)}) // stale
	sightAdd("test_sight", mobSight{x: 12, y: 10, lv: 5, at: now})                   // inside our own view
	all := func(int, int) bool { return true }
	if x, y, ok := sightNear("test_sight", 10, 10, 15, 80, 20, now, all); !ok || x != 50 || y != 50 {
		t.Errorf("want the lv10 sighting at 50,50, got %d,%d %v", x, y, ok)
	}
	if _, _, ok := sightNear("test_sight", 10, 10, 15, 30, 20, now, all); ok {
		t.Error("nothing within 30 cells")
	}
	for i := 0; i < 100; i++ { // the ring keeps the newest sightKeep
		sightAdd("test_sight", mobSight{x: i, y: 0, lv: 1, at: now})
	}
	mapSights.Lock()
	n := len(mapSights.m["test_sight"])
	mapSights.Unlock()
	if n != sightKeep {
		t.Errorf("ring holds %d, want %d", n, sightKeep)
	}
}
