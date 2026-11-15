package main

import "testing"

// QA 2026-11-14 04:03: 105 lv30+ non-healers carried 0 Red/Orange/Yellow potions because the starter kit (Novice,
// Light potions) counted as stock. A lv52 with 115 starter potions must buy real ones first, and drink the strongest
// in an emergency.
func TestRealPotionsFirst(t *testing.T) {
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	items[502] = &Item{ID: 502, Name: "Orange Potion", Type: "Healing", Buy: 200, Weight: 100}
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	b := &Bot{P: &Persona{BaseLevel: 52, Chars: []CharSpec{{Name: "potstest"}}}, invSeen: true, Zeny: 1613,
		MaxWeight: 20000, Weight: 2000, MapName: "prontera", MaxHP: 1613, HP: 1613,
		inv: map[int]*InvItem{
			3: {Index: 3, ItemID: 569, Count: 100},
			4: {Index: 4, ItemID: 11500, Count: 15},
		}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if b.realPotCount() != 0 || b.potMin() < 10 {
		t.Fatalf("realPotCount %d potMin %d", b.realPotCount(), b.potMin())
	}
	if !b.needsPots() {
		t.Fatal("a lv52 with only starter potions needs potions")
	}
	if b.needTrip() != "out of potions" {
		t.Fatalf("needTrip = %q", b.needTrip())
	}
	hpN, _, _ := b.potBuyPlan(501, 50, 0, 0, 0)
	if hpN < b.potMin() {
		t.Fatalf("bought %d Red, want >= %d", hpN, b.potMin())
	}
	// a broke bow user with no arrows: up to 200 arrows first (QA 05:48: 5 Red + 69 arrows left wolfclaw70 unable to
	// fight), the rest on the potion minimum
	b.Zeny = 700
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15}
	b.inv[2] = bow
	b.equipped[0x2] = bow
	hpN, _, arrN := b.potBuyPlan(501, 50, 0, 0, 1)
	if hpN < 9 || arrN < 200 || hpN*50+arrN > 700 {
		t.Fatalf("700z: %d Red, %d arrows", hpN, arrN)
	}
	// emergency: Red over Novice; topping up out of danger: Novice first
	b.inv[5] = &InvItem{Index: 5, ItemID: 501, Count: 10}
	b.HP = 400
	if got := b.potionIndex(); got != 5 {
		t.Fatalf("emergency picked index %d, want the Red Potion", got)
	}
	if got := b.pickPotion(false); got != 3 {
		t.Fatalf("top-up picked index %d, want the Novice Potion", got)
	}
	// nearly full: no Red wasted on 20 missing HP... the weakest is used
	b.HP = 1600
	if got := b.pickPotion(true); got != 3 {
		t.Fatalf("20 HP missing picked %d, want Novice", got)
	}
	// low levels keep the starter kit as stock
	b.P.BaseLevel = 10
	if b.realPotCount() != 125 || b.potMin() != 0 {
		t.Fatalf("lv10: realPotCount %d potMin %d", b.realPotCount(), b.potMin())
	}
}

// QA 2026-11-14 Star claw: the town-run gear chain keeps the potion minimum (and the Kafra fare home)
func TestGearKeep(t *testing.T) {
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	items[502] = &Item{ID: 502, Name: "Orange Potion", Type: "Healing", Buy: 200, Weight: 100}
	b := &Bot{P: &Persona{BaseLevel: 52, Chars: []CharSpec{{Name: "keeptest"}}}, invSeen: true, Zeny: 5000,
		MaxHP: 1600, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	// batch 16: an hour's stock (hpPotTarget), not only the survival minimum, is kept from gear; priced at the bot's
	// potion (a lv52 with 1,600 HP restocks Orange)
	if want := max(b.potMin(), b.hpPotTarget()) * 200; b.gearKeep("prontera") != want {
		t.Fatalf("gearKeep %d, want %d", b.gearKeep("prontera"), want)
	}
	b.inv[1] = &InvItem{Index: 1, ItemID: 501, Count: 30}
	if got := b.gearKeep("prontera"); got != 0 {
		t.Fatalf("stocked, no goal: gearKeep %d", got)
	}
}

// QA 2026-11-14: "sold 5 surplus potions (bag was 13%)": surplus only at the regen weight line, above max(target, potMin)
func TestSurplusOnlyOverweight(t *testing.T) {
	items[501] = &Item{ID: 501, Name: "Red Potion", Type: "Healing", Buy: 50, Weight: 70}
	b := &Bot{P: &Persona{BaseLevel: 52, Chars: []CharSpec{{Name: "surplustest"}}}, invSeen: true, Zeny: 5000,
		MaxWeight: 10000, Weight: 1300, inv: map[int]*InvItem{1: {Index: 1, ItemID: 501, Count: 200}},
		equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if n := len(b.surplusPots()); n != 0 {
		t.Fatalf("13%% weight: surplus %d", n)
	}
	b.Weight = 9000
	if got := b.surplusPots()[1]; got != 200-max(b.hpPotTarget(), b.potMin()) {
		t.Fatalf("90%% weight: surplus %d", got)
	}
}

// Monks/Champions are melee, not healers: they stock HP potions like other melee (isHealer, skillbuff.go)
func TestMonkStocksHPPotions(t *testing.T) {
	monk := &Bot{P: &Persona{BaseLevel: 63, Chars: []CharSpec{{Name: "monktest"}}}, Class: 15, invSeen: true,
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	priest := &Bot{P: &Persona{BaseLevel: 63, Chars: []CharSpec{{Name: "priesttest"}}}, Class: 8, invSeen: true,
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	if monk.isHealer() || !priest.isHealer() {
		t.Fatalf("isHealer monk %v priest %v", monk.isHealer(), priest.isHealer())
	}
	if monk.potMin() != 20 || priest.potMin() != 5 {
		t.Fatalf("potMin monk %d priest %d", monk.potMin(), priest.potMin())
	}
	if monk.hpPotTarget() < 30 || priest.hpPotTarget() != tune().PotHPAcolyte {
		t.Fatalf("hpPotTarget monk %d priest %d", monk.hpPotTarget(), priest.hpPotTarget())
	}
	monk.Zeny = 5000
	if monk.needTrip() != "out of potions" {
		t.Fatalf("monk needTrip %q", monk.needTrip())
	}
}

// economy run 8: the HP top-up above the survival minimum keeps the wing saving floor in the purse (restocks left a
// median 1,049z: nothing saved for gear / a Peco).
func TestPotTopUpKeepsFloor(t *testing.T) {
	items[502] = &Item{ID: 502, Name: "Orange Potion", Type: "Healing", Buy: 200, Weight: 10}
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "floortest"}}}, invSeen: true, Zeny: 6000,
		MaxWeight: 20000, Weight: 2000, MapName: "prontera", MaxHP: 2000, HP: 2000,
		inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	hpN, _, _ := b.potBuyPlan(502, 200, 0, 0, 0)
	if hpN != b.potMin() {
		t.Fatalf("6000z lv60: bought %d, want only the minimum %d", hpN, b.potMin())
	}
	b.Zeny = 20000
	hpN, _, _ = b.potBuyPlan(502, 200, 0, 0, 0)
	if hpN != b.hpPotTarget() || b.Zeny-hpN*200 < wingSaveFloor(60) {
		t.Fatalf("20000z lv60: bought %d (target %d)", hpN, b.hpPotTarget())
	}
}
