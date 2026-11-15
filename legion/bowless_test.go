package main

import (
	"testing"
	"time"
)

// QA 2026-11-14: Archers in parties kept the starter Knife with 1,850-2,055z while a Bow sat one map away, because a
// class weapon waits for the next real-need party run. An Archer without any bow can't do its job: like "no weapon"
// it may call a run (6-min gate) and skips the gear-step delay. A Mage on a Knife still waits.
func TestBowlessArcherIsNecessary(t *testing.T) {
	saved := shopsByMap
	defer func() { shopsByMap = saved }()
	shopsByMap = map[string][]*Shop{"prontera": {{Map: "prontera", X: 142, Y: 57, Name: "Weapons Merchant#t", ID: "wd", Items: []int{1701, 1601}}}}
	clearCache := func() {
		gearMu.Lock()
		delete(gearShopsCache, 1701)
		delete(gearShopsCache, 1601)
		gearMu.Unlock()
	}
	clearCache()
	defer clearCache()
	items[1201] = &Item{ID: 1201, Name: "Knife", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 17, Buy: 50}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	items[1601] = &Item{ID: 1601, Name: "Rod", Type: "Weapon", Sub: "Staff", Loc: 0x2, Atk: 15, Buy: 50}
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	mk := func(class int) *Bot {
		knife := &InvItem{Index: 2, ItemID: 1201, Count: 1, Loc: 0x2, Equipped: true}
		return &Bot{P: &Persona{BaseLevel: 36, Chars: []CharSpec{{Name: "bowtest"}}}, Class: class, invSeen: true, Zeny: 5000,
			MaxWeight: 20000, Weight: 1000, MapName: "prontera", MaxHP: 1500, HP: 1500,
			inv:      map[int]*InvItem{2: knife, 5: {Index: 5, ItemID: 501, Count: 50}},
			equipped: map[uint32]*InvItem{0x2: knife}, gearFailed: map[int]bool{},
			buffAt: map[string]time.Time{"gearstepok": time.Now().Add(time.Hour)}} // the gear-step delay is not over
	}
	a := mk(3) // Archer
	if !a.bowless() {
		t.Fatal("an Archer holding a Knife is bowless")
	}
	if got := a.needTrip(); got != "class weapon" {
		t.Fatalf("bowless Archer needTrip = %q, want \"class weapon\" despite the gear-step delay", got)
	}
	if a.partyRunGap(a.bowless()) != 6*time.Minute {
		t.Fatal("a bowless Archer may call a party run after the 6-min gate")
	}
	m := mk(2) // Mage on a Knife: no spell power at all - the 50z Rod is an emergency too (QA: 35+ min waits)
	if !m.bowless() {
		t.Fatal("a Mage holding a Knife has no class weapon")
	}
	if got := m.needTrip(); got != "class weapon" {
		t.Fatalf("Mage on a Knife needTrip = %q, want \"class weapon\"", got)
	}
	if m.partyRunGap(m.bowless()) != 6*time.Minute {
		t.Fatal("a Mage without a rod may call a party run after the 6-min gate")
	}
	s := mk(1) // Swordman on a Knife: a Knife is not an emergency
	if s.bowless() {
		t.Fatal("a Swordman is never bowless")
	}
	if s.partyRunGap(s.bowless()) < 45*time.Minute {
		t.Fatal("non-emergency party runs keep the 45-min gate")
	}
}

// QA 07:42: Archer-line bots with 1,100-5,277z stayed on Knives while the gear turn saved for a 2500z Composite Bow.
// With no class weapon the turn step is the cheapest class weapon the pool can pay for now.
func TestPoolStepCheapestClassWeapon(t *testing.T) {
	saved := shopsByMap
	defer func() { shopsByMap = saved }()
	shopsByMap = map[string][]*Shop{"prontera": {{Map: "prontera", X: 142, Y: 57, Name: "Weapons Merchant#t2", ID: "wd2", Items: []int{1705, 1701}}}}
	clearCache := func() {
		gearMu.Lock()
		delete(gearShopsCache, 1701)
		delete(gearShopsCache, 1705)
		gearMu.Unlock()
	}
	clearCache()
	defer clearCache()
	items[1201] = &Item{ID: 1201, Name: "Knife", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 17, Buy: 50}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	items[1705] = &Item{ID: 1705, Name: "Composite Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 29, Buy: 2500}
	knife := &InvItem{Index: 2, ItemID: 1201, Count: 1, Loc: 0x2, Equipped: true}
	b := &Bot{P: &Persona{BaseLevel: 37, Chars: []CharSpec{{Name: "pooltest"}}}, Class: 3, invSeen: true, Zeny: 2040,
		MaxWeight: 20000, Weight: 1000, MapName: "prontera", inv: map[int]*InvItem{2: knife},
		equipped: map[uint32]*InvItem{0x2: knife}, gearFailed: map[int]bool{}, buffAt: map[string]time.Time{}}
	if !b.classWeaponMissing() {
		t.Fatal("an Archer on a Knife misses its class weapon")
	}
	it, sh := b.poolStep(3000, true)
	if it == nil || it.ID != 1701 || sh == nil {
		t.Fatalf("pool step %v, want the 1000z Bow (not the Composite Bow)", it)
	}
}

// QA 07:42: 23 bots carried a bow in the bag with no arrows and fought with a Knife. A bow in the bag makes a bow user
// ("no arrows" fires, party runs buy arrows), and once arrows are in the bag the bow outranks the Knife (equipPass).
func TestBagBowNoArrows(t *testing.T) {
	items[1201] = &Item{ID: 1201, Name: "Knife", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 17, Buy: 50}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	knife := &InvItem{Index: 2, ItemID: 1201, Count: 1, Loc: 0x2, Equipped: true}
	bow := &InvItem{Index: 3, ItemID: 1701, Count: 1, Loc: 0x22}
	b := &Bot{P: &Persona{BaseLevel: 30, Chars: []CharSpec{{Name: "bagbow"}}}, Class: 3, invSeen: true, Zeny: 900,
		MaxWeight: 20000, Weight: 1000, MapName: "prontera", inv: map[int]*InvItem{2: knife, 3: bow},
		equipped: map[uint32]*InvItem{0x2: knife}, gearFailed: map[int]bool{}, buffAt: map[string]time.Time{}}
	if !b.noArrows() || b.arrowsWanted(1, false) < minArrowTrip {
		t.Fatalf("bow in the bag, no arrows: noArrows %v, wanted %d", b.noArrows(), b.arrowsWanted(1, false))
	}
	if b.bowless() {
		t.Fatal("a wearable bow in the bag is not bowless")
	}
	if b.itemScore(items[1701]) <= b.itemScore(items[1201]) {
		t.Fatalf("an Archer must rank the Bow (%d) over the Knife (%d) once it has arrows", b.itemScore(items[1701]), b.itemScore(items[1201]))
	}
}

// QA 2026-11-14 LilRinkun (Archer lv51): a Bow in the bag, 0 arrows, a Knife in hand, 46% weight (13 unidentified
// Clubs). The 45% potion cap made every arrow buy 0, and "out of potions" (also unbuyable) won the needTrip order.
func TestBowInBagNoArrowsOverPotCap(t *testing.T) {
	items[1201] = &Item{ID: 1201, Name: "Knife", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 17, Buy: 50}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15, Buy: 1000}
	knife := &InvItem{Index: 2, ItemID: 1201, Count: 1, Loc: 0x2, Equipped: true}
	bow := &InvItem{Index: 19, ItemID: 1701, Count: 1, Loc: 0x22}
	b := &Bot{P: &Persona{BaseLevel: 51, Chars: []CharSpec{{Name: "rinktest"}}}, Class: 3, invSeen: true, Zeny: 5652,
		MaxWeight: 26300, Weight: 12240, MapName: "moc_fild01", MaxHP: 1061, HP: 1061,
		inv: map[int]*InvItem{2: knife, 19: bow}, equipped: map[uint32]*InvItem{0x2: knife}, gearFailed: map[int]bool{},
		buffAt: map[string]time.Time{}}
	if !b.noArrows() {
		t.Fatal("a Bow in the bag and no arrows: noArrows")
	}
	if n := b.arrowsWanted(1, false); n < 200 {
		t.Fatalf("arrowsWanted over the potion cap = %d, want >= 200", n)
	}
	if got := b.needTrip(); got != "no arrows" {
		t.Fatalf("needTrip = %q, want \"no arrows\" before \"out of potions\"", got)
	}
}
