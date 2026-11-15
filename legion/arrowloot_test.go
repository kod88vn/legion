package main

import "testing"

// QA 2026-11-14 05:40: poor bow users (46-50z, junk loot in the bag) never got arrows; overweight bots kept looting junk.
func TestArrowLootAndHeavyJunk(t *testing.T) {
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15}
	items[90909] = &Item{ID: 90909, Name: "TestJunk", Type: "Etc", Buy: 20, Weight: 10}
	items[90910] = &Item{ID: 90910, Name: "TestDearEtc", Type: "Etc", Buy: 400, Weight: 10}
	items[90911] = &Item{ID: 90911, Name: "TestCard", Type: "Card", Buy: 20, Weight: 1}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	b := &Bot{P: &Persona{BaseLevel: 49, Chars: []CharSpec{{Name: "arrowloot"}}}, invSeen: true, Zeny: 50, MaxWeight: 10000, Weight: 1000,
		inv: map[int]*InvItem{2: bow}, equipped: map[uint32]*InvItem{0x2: bow}, gearFailed: map[int]bool{}}
	if b.arrowLootOK() {
		t.Fatal("no loot: no sell-loot arrow trip")
	}
	b.inv[7] = &InvItem{Index: 7, ItemID: 90909, Count: 20} // sells for 200z -> 180z at 90%: 180 arrows
	if !b.arrowLootOK() {
		t.Fatal("50z + 200z of junk: the loot pays for 100+ arrows")
	}
	if b.Zeny != 50 || b.Weight != 1000 {
		t.Fatal("arrowLootOK must restore zeny/weight")
	}
	if got := b.needTrip(); got != "no arrows (sell loot)" {
		t.Fatalf("needTrip = %q", got)
	}
	b.inv[7].Count = 5 // 50z of junk -> 45 arrows: not worth a trip
	if b.arrowLootOK() {
		t.Fatal("junk worth < 100 arrows: no trip")
	}
	if !heavyJunk(90909) || heavyJunk(90910) || heavyJunk(90911) || heavyJunk(1701) || heavyJunk(1750) {
		t.Fatal("heavyJunk: only cheap NPC junk is left on the ground")
	}

	// food.go: herbs/wings/fruit past their caps are loot; fruit is eaten out of combat, Novice Potions past 30 at lv30+
	items[511] = &Item{ID: 511, Name: "Green Herb", Type: "Healing", Buy: 10, Weight: 30}
	items[601] = &Item{ID: 601, Name: "Fly Wing", Type: "DelayConsume", Buy: 250, Weight: 50}
	items[512] = &Item{ID: 512, Name: "Apple", Type: "Healing", Buy: 15, Weight: 20}
	c := &Bot{P: &Persona{BaseLevel: 38, Chars: []CharSpec{{Name: "foodtest"}}}, invSeen: true, MaxHP: 1000, HP: 900, MaxWeight: 10000, Weight: 7700,
		inv: map[int]*InvItem{1: {Index: 1, ItemID: 511, Count: 43}, 2: {Index: 2, ItemID: 601, Count: 43}, 3: {Index: 3, ItemID: 569, Count: 162}}}
	sur := c.surplusConsumables()
	if sur[1] != 33 || sur[2] != 28 || sur[3] != 0 {
		t.Fatalf("surplus %v", sur)
	}
	if !c.hasLoot() {
		t.Fatal("surplus herbs/wings are loot")
	}
	if c.foodIndex() != 3 {
		t.Fatal("lv30+: surplus Novice Potion as a top-up")
	}
	c.inv[4] = &InvItem{Index: 4, ItemID: 512, Count: 5}
	if c.foodIndex() != 4 {
		t.Fatal("fruit before the starter potions")
	}
}
