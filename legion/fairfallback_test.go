package main

import "testing"

// A bot whose effective level sits well under its base level (poor gear) asks for a band that is mostly monsters
// under base-15, so the gate empties every list. It must not idle: it takes the spot with the most fair kills.

// base 60, effective 43: the fair line is 45. orc_a has 10 of 30 monsters at lv46 (share 0.33), orc_b has none (0).
func TestFairFallbackBase60Power43(t *testing.T) {
	swapFairTables(t)
	mobs = map[int]*Mob{1001: {ID: 1001, Level: 30}, 1002: {ID: 1002, Level: 46}, 1003: {ID: 1003, Level: 30}, 1004: {ID: 1004, Level: 40}}
	spawnsByMap = map[string][]Spawn{
		"orc_a": {{"orc_a", 1001, 20}, {"orc_a", 1002, 10}},
		"orc_b": {{"orc_b", 1003, 20}, {"orc_b", 1004, 10}},
	}
	huntSpots = []HuntSpot{{Map: "orc_a", MinLv: 30, MaxLv: 46, Mobs: 30}, {Map: "orc_b", MinLv: 30, MaxLv: 40, Mobs: 30}}
	guideSpots = nil
	maps = map[string]*GameMap{"orc_a": {}, "orc_b": {}}
	b := &Bot{P: &Persona{BaseLevel: 60, Risk: 0.5, Chars: []CharSpec{{Name: "FallbackTest"}}}, BaseLv: 60}
	if len(SpotsFor(43, 0.5)) != 2 || len(b.spotsFor(43)) != 1 {
		t.Fatalf("setup: ungated %d, gated fallback %d", len(SpotsFor(43, 0.5)), len(b.spotsFor(43)))
	}
	got := b.spotsFor(43)
	if len(got) != 1 || got[0].Map != "orc_a" {
		t.Fatalf("base 60 / power 43: spots = %+v, want the best-share spot orc_a", got)
	}
}

// base 70, effective 50: the fair line is 55. x_a has 10 of 30 at lv56 (share 0.33), x_b has none.
func TestFairFallbackBase70Power50(t *testing.T) {
	swapFairTables(t)
	mobs = map[int]*Mob{2001: {ID: 2001, Level: 50}, 2002: {ID: 2002, Level: 56}, 2003: {ID: 2003, Level: 50}}
	spawnsByMap = map[string][]Spawn{
		"x_a": {{"x_a", 2001, 20}, {"x_a", 2002, 10}},
		"x_b": {{"x_b", 2003, 30}},
	}
	huntSpots = []HuntSpot{{Map: "x_a", MinLv: 50, MaxLv: 56, Mobs: 30}, {Map: "x_b", MinLv: 50, MaxLv: 50, Mobs: 30}}
	guideSpots = nil
	maps = map[string]*GameMap{"x_a": {}, "x_b": {}}
	b := &Bot{P: &Persona{BaseLevel: 70, Risk: 0.5, Chars: []CharSpec{{Name: "FallbackTest"}}}, BaseLv: 70}
	got := b.spotsFor(50)
	if len(got) != 1 || got[0].Map != "x_a" {
		t.Fatalf("base 70 / power 50: spots = %+v, want the best-share spot x_a", got)
	}
}

// the fallback is logged once per bot per hour, not on every pick
func TestFairFallbackLogsOncePerHour(t *testing.T) {
	swapFairTables(t)
	mobs = map[int]*Mob{2003: {ID: 2003, Level: 50}}
	spawnsByMap = map[string][]Spawn{"x_b": {{"x_b", 2003, 30}}}
	huntSpots = []HuntSpot{{Map: "x_b", MinLv: 50, MaxLv: 50, Mobs: 30}}
	maps = map[string]*GameMap{"x_b": {}}
	b := &Bot{P: &Persona{BaseLevel: 70, Risk: 0.5, Chars: []CharSpec{{Name: "FallbackTest"}}}, BaseLv: 70}
	b.spotsFor(50)
	first := b.buffAt["fairlog"]
	if first.IsZero() {
		t.Fatal("the first fallback must be logged")
	}
	b.spotsFor(50)
	if !b.buffAt["fairlog"].Equal(first) {
		t.Fatal("a second fallback within the hour must not log again")
	}
}

// the gate off keeps the plain list (no fallback needed)
func TestFairFallbackGateOff(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	off := *tune()
	off.FairSpotGateOn = false
	curTuning.Store(&off)
	b := &Bot{P: &Persona{BaseLevel: 53, Risk: 0.5, Chars: []CharSpec{{Name: "FallbackTest"}}}, BaseLv: 53}
	if len(b.spotsFor(53)) != len(SpotsFor(53, 0.5)) {
		t.Fatal("with the gate off the plain list must come back unchanged")
	}
}
