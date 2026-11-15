package main

import (
	"encoding/binary"
	"testing"
)

// buyList: a fake NPC buy list (0x00c6) with these item -> price entries
func buyList(prices map[int]int) []byte {
	pk := make([]byte, 4, 4+13*len(prices))
	for id, pr := range prices {
		e := make([]byte, 13)
		binary.LittleEndian.PutUint32(e[4:], uint32(pr))
		binary.LittleEndian.PutUint32(e[9:], uint32(id))
		pk = append(pk, e...)
	}
	return pk
}

func TestButterflyWingRestock(t *testing.T) {
	pk := buyList(map[int]int{itemRedPotion: 50, itemFlyWing: 60, itemButterflyWing: 300})
	b := &Bot{P: &Persona{}, inv: map[int]*InvItem{}, MaxWeight: 20000, Weight: 2000, BaseLv: 50, Zeny: 40000}
	floor := wingSaveFloor(50) // 3,000z kept for gear
	if n := b.butterflyWingsWanted(pk, floor+5000, 0); n != bwTarget {
		t.Errorf("rich, none carried, money and room: want %d, got %d", bwTarget, n)
	}
	b.inv[1] = &InvItem{Index: 1, ItemID: itemButterflyWing, Count: 2}
	if n := b.butterflyWingsWanted(pk, floor+5000, 0); n != bwTarget-2 {
		t.Errorf("2 carried: top up %d, got %d", bwTarget-2, n)
	}
	b.inv[1].Count = 0
	if n := b.butterflyWingsWanted(pk, floor+650, 0); n != 2 {
		t.Errorf("650z above the floor buys 2, got %d", n)
	}
	if n := b.butterflyWingsWanted(pk, floor+200, 0); n != 0 {
		t.Errorf("can't afford one above the floor: got %d", n)
	}
	// run 7: a poor bot keeps its zeny for gear (lv50 with 2,000z: below the floor)
	b.Zeny = 2000
	if n := b.butterflyWingsWanted(pk, 2000, 0); n != 0 {
		t.Errorf("poor: no wings, got %d", n)
	}
	b.Zeny = floor + 3500 // middle class: 2 wings (a trip + the escape reserve)
	if n := b.butterflyWingsWanted(pk, b.Zeny, 0); n != 2 {
		t.Errorf("middle: 2 wings, got %d", n)
	}
	b.Zeny = 40000
	b.Weight = 20000*tune().PotWeightCapPct/100 - 5
	if n := b.butterflyWingsWanted(pk, floor+5000, 0); n != 0 {
		t.Errorf("at the weight cap: got %d", n)
	}
	b.Weight = 2000
	if n := b.butterflyWingsWanted(buyList(map[int]int{itemRedPotion: 50}), floor+5000, 0); n != 0 {
		t.Errorf("shop doesn't sell them: got %d", n)
	}
}

// owner 2026-11-14: a town run uses a Butterfly Wing whenever one is spare (the last one stays for escapes), however
// near the town; and the missing wing kit is kept back from gear (potions first, then wings, then gear).
func TestTownRunWingAndKit(t *testing.T) {
	items[itemButterflyWing] = &Item{ID: itemButterflyWing, Name: "Butterfly Wing", Buy: 300, Weight: 50}
	items[itemFlyWing] = &Item{ID: itemFlyWing, Name: "Fly Wing", Buy: 60, Weight: 50}
	items[1101] = &Item{ID: 1101, Name: "Sword", Type: "Weapon", Sub: "1hSword", Loc: 0x2, Atk: 25, Buy: 100}
	knife := &InvItem{Index: 2, ItemID: 1101, Count: 1, Loc: 0x2, Equipped: true} // a Swordman with its class weapon
	b := &Bot{P: &Persona{BaseLevel: 30}, Class: 1, invSeen: true, MapName: "prt_fild08", MaxHP: 1000, HP: 1000,
		inv:      map[int]*InvItem{2: knife, 3: {Index: 3, ItemID: itemRedPotion, Count: 50}, 4: {Index: 4, ItemID: itemButterflyWing, Count: 2}},
		equipped: map[uint32]*InvItem{0x2: knife}, gearFailed: map[int]bool{}}
	b.savedTown, b.saveKnown = "prontera", true // a Butterfly Wing lands on the save point: only for a trip to that town
	if !b.tripWingOK("prontera") {
		t.Fatal("2 Butterfly Wings: one is spare, the town run wings (even one map from town)")
	}
	b.inv[4].Count = 1
	if b.tripWingOK("prontera") {
		t.Fatal("the last Butterfly Wing stays for escapes: walk")
	}
	if got, want := b.wingKitCost(), max(0, b.bwWant()-1)*300+fwTarget*60; got != want {
		t.Fatalf("wing kit cost %d, want %d", got, want)
	}
	b.MapName = "prt_in"
	b.inv[4].Count = 3
	if b.tripWingOK("prontera") {
		t.Fatal("already inside the town: no wing")
	}
}

// QA 12:37: Ayla's friends winged to their own save towns (Morocc / Prontera) on her Payon town run.
func TestTownRunWingOnlyToSaveTown(t *testing.T) {
	items[itemButterflyWing] = &Item{ID: itemButterflyWing, Name: "Butterfly Wing", Buy: 300, Weight: 50}
	b := &Bot{P: &Persona{BaseLevel: 70}, invSeen: true, MapName: "pay_dun00", MaxHP: 3000, HP: 3000,
		inv: map[int]*InvItem{3: {Index: 3, ItemID: itemRedPotion, Count: 50}, 4: {Index: 4, ItemID: itemButterflyWing, Count: 3}}}
	b.savedTown, b.saveKnown = "morocc", true
	if b.tripWingOK("payon") {
		t.Error("saved in Morocc, town run to Payon: no Butterfly Wing (it would land in Morocc)")
	}
	b.savedTown = "payon"
	if !b.tripWingOK("payon") {
		t.Error("saved in Payon: wing to the Payon run")
	}
	b.saveKnown = false
	if b.tripWingOK("payon") {
		t.Error("unknown save point: no wing")
	}
}
