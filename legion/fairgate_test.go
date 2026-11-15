package main

import (
	"math/rand"
	"testing"
	"time"
)

// swapFairTables swaps the spawn, monster, hunt-spot, guide-spot, map and tuning tables for one test and restores them.
func swapFairTables(t *testing.T) {
	t.Helper()
	savedSpawns, savedMobs, savedSpots, savedGuides, savedMaps := spawnsByMap, mobs, huntSpots, guideSpots, maps
	savedTune := tune()
	t.Cleanup(func() {
		spawnsByMap, mobs, huntSpots, guideSpots, maps = savedSpawns, savedMobs, savedSpots, savedGuides, savedMaps
		curTuning.Store(savedTune)
	})
}

// fairWorld: pay_fild07 (Bigfoot lv29 x20, Caramel lv25 x20, one lv45 monster) and moc_fild02 (lv60 x15, lv55 x15).
func fairWorld() {
	mobs = map[int]*Mob{1001: {ID: 1001, Level: 29}, 1002: {ID: 1002, Level: 25}, 1003: {ID: 1003, Level: 45},
		2001: {ID: 2001, Level: 60}, 2002: {ID: 2002, Level: 55}}
	spawnsByMap = map[string][]Spawn{
		"pay_fild07": {{"pay_fild07", 1001, 20}, {"pay_fild07", 1002, 20}, {"pay_fild07", 1003, 1}},
		"moc_fild02": {{"moc_fild02", 2001, 15}, {"moc_fild02", 2002, 15}},
	}
	huntSpots = []HuntSpot{{Map: "pay_fild07", MinLv: 25, MaxLv: 45, Mobs: 41}, {Map: "moc_fild02", MinLv: 55, MaxLv: 60, Mobs: 30}}
	guideSpots = nil
	maps = map[string]*GameMap{"pay_fild07": {}, "moc_fild02": {}}
}

func containsSpot(spots []HuntSpot, m string) bool {
	for _, s := range spots {
		if s.Map == m {
			return true
		}
	}
	return false
}

// bugq b1794751012196-928: a lv53 Monk was sent to pay_fild07 (all kills under the fair line 53-15=38: 0 fair kills).
func TestFairSpotGateRefusesLowMapForHighLevel(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	if s, known := fairBandShare("pay_fild07", 53); !known || s != 1.0/41 {
		t.Fatalf("pay_fild07 fair share for base 53 = %v known=%v", s, known)
	}
	b := &Bot{P: &Persona{BaseLevel: 53, Risk: 0.5}, BaseLv: 53}
	spots := b.spotsFor(53)
	if containsSpot(spots, "pay_fild07") || !containsSpot(spots, "moc_fild02") {
		t.Fatalf("spots for a solo lv53 = %+v, want moc_fild02 and not pay_fild07", spots)
	}
	// the flag off restores the old rule (highest monster only)
	off := *tune()
	off.FairSpotGateOn = false
	curTuning.Store(&off)
	if !containsSpot(b.spotsFor(53), "pay_fild07") {
		t.Fatal("with fair_spot_gate_on off the old spot rule must apply")
	}
}

// the gate measures at base level, not the effective level the spot score uses
func TestFairSpotGateUsesBaseLevel(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	b := &Bot{P: &Persona{BaseLevel: 53, Risk: 0.5}, BaseLv: 53}
	if fairSpotOK("pay_fild07", b.BaseLvOr()) {
		t.Fatal("base 53 must refuse pay_fild07")
	}
	if containsSpot(b.spotsFor(30), "pay_fild07") {
		t.Fatal("the gate must judge the base level 53, not the effective level 30")
	}
}

// a party member keeps the plain list (party pickers are not gated here)
func TestFairSpotGateSparesParties(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	b := &Bot{P: &Persona{BaseLevel: 53, Risk: 0.5}, BaseLv: 53, PartyID: 7}
	if !containsSpot(b.spotsFor(53), "pay_fild07") {
		t.Fatal("a party member must keep pay_fild07 in the plain spot list")
	}
}

// missing data is neutral: a map with only bosses or no monsters is kept, not dropped
func TestFairSpotGateKeepsUnknownMaps(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	mobs[3001] = &Mob{ID: 3001, Level: 99, Boss: true}
	spawnsByMap["boss_fild"] = []Spawn{{"boss_fild", 3001, 1}}
	if !fairSpotOK("boss_fild", 53) || !fairSpotOK("no_such_map", 53) {
		t.Fatal("a map with no known non-boss monsters must be kept")
	}
}

// the fair share is its own tuning value (0 or less = 0.5)
func TestFairSpotShareTunable(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	b := &Bot{P: &Persona{BaseLevel: 53, Risk: 0.5}, BaseLv: 53}
	low := *tune()
	low.FairSpotShare = 0.01 // pay_fild07 has 1/41 fair
	curTuning.Store(&low)
	if !containsSpot(b.spotsFor(53), "pay_fild07") {
		t.Fatal("fair_spot_share 0.01 must let pay_fild07 (1/41 fair) through")
	}
	zero := *tune()
	zero.FairSpotShare = 0
	curTuning.Store(&zero)
	if fairSpotShare() != 0.5 {
		t.Fatalf("unset share = %v, want 0.5", fairSpotShare())
	}
}

// the gate removes every spot, but the only spot is still taken: the bot hunts the best-share spot, it does not idle
func TestGatedEmptyResultFallsBack(t *testing.T) {
	swapFairTables(t)
	// the ungated list has gef_fild05 (one lv34 monster is in its reach); the gate drops it (1 of 50 fair for lv45)
	mobs = map[int]*Mob{1: {ID: 1, Level: 23}, 2: {ID: 2, Level: 21}, 3: {ID: 3, Level: 29}, 4: {ID: 4, Level: 34}}
	spawnsByMap = map[string][]Spawn{"gef_fild05": {{"gef_fild05", 1, 20}, {"gef_fild05", 2, 20}, {"gef_fild05", 3, 9}, {"gef_fild05", 4, 1}}}
	huntSpots = []HuntSpot{{Map: "gef_fild05", MinLv: 21, MaxLv: 34, Mobs: 50}}
	guideSpots = nil
	maps = map[string]*GameMap{"gef_fild05": {}}
	b := &Bot{P: &Persona{BaseLevel: 45, Risk: 0.5, Chars: []CharSpec{{Name: "FallbackTest"}}}, BaseLv: 45, MapName: "gef_fild02", invSeen: true,
		buffAt: map[string]time.Time{}, cart: map[int]*InvItem{}, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{},
		rng: rand.New(rand.NewSource(1))}
	if got := b.spotsFor(45); len(got) != 1 || got[0].Map != "gef_fild05" {
		t.Fatalf("lv45 solo with only gef_fild05: spots = %+v, want the fallback gef_fild05", got)
	}
	b.chooseHuntingGround()
	if b.Goal.Mode != "hunt" || b.Goal.Map != "gef_fild05" {
		t.Fatalf("goal = %+v, want a hunt on gef_fild05 (never idle while a spot exists)", b.Goal)
	}
}

// the guide-spot list is gated too: a synthetic guide spot whose monsters are all below the fair line
func TestGuideSpotsAreGated(t *testing.T) {
	swapFairTables(t)
	mobs = map[int]*Mob{1: {ID: 1, Level: 10}}
	spawnsByMap = map[string][]Spawn{"low_fild": {{"low_fild", 1, 30}}}
	guideSpots = []GuideSpot{{Min: 20, Max: 35, Map: "low_fild"}}
	maps = map[string]*GameMap{"low_fild": {}}
	solo := &Bot{P: &Persona{BaseLevel: 34, Risk: 0.5}, BaseLv: 34}
	if len(solo.guideSpotsFor(34)) != 0 {
		t.Fatal("a solo lv34 must not get a guide spot whose monsters are all below its fair line (19)")
	}
	party := &Bot{P: &Persona{BaseLevel: 34, Risk: 0.5}, BaseLv: 34, PartyID: 3}
	if len(party.guideSpotsFor(34)) != 1 {
		t.Fatal("a party member keeps the guide spot list")
	}
}

// the spot re-plan condition (ai.go think): only a "nowhere reachable" idle or hunt goal whose minute is up
func TestHuntRetryDue(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	cases := []struct {
		name string
		g    Goal
		at   time.Time
		want bool
	}{
		{"idle nowhere, due", Goal{Mode: "idle", Set: "auto (nowhere reachable)"}, past, true},
		{"hunt nowhere, due", Goal{Mode: "hunt", Map: "gef_fild02", Set: "auto (nowhere reachable)"}, past, true},
		{"idle nowhere, not yet", Goal{Mode: "idle", Set: "auto (nowhere reachable)"}, now.Add(time.Minute), false},
		{"idle calling-it-a-day", Goal{Mode: "idle", Set: "auto"}, past, false},
		{"hunt auto", Goal{Mode: "hunt", Map: "gef_fild02", Set: "auto"}, past, false},
		{"party idle", Goal{Mode: "idle", Set: "party-leader-hold"}, past, false},
	}
	for _, c := range cases {
		if got := huntRetryDue(c.g, c.at, now); got != c.want {
			t.Errorf("%s: huntRetryDue = %v, want %v", c.name, got, c.want)
		}
	}
}
