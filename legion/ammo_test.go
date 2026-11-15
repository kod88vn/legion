package main

import (
	"encoding/binary"
	"testing"
)

// QA 2026-11-14: 86 Archers with a bow and no arrows could not attack. A bow user wants arrows, equips them, and
// "no arrows" is a necessary need; a Knife is taken meanwhile.
func TestArrows(t *testing.T) {
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15}
	items[1201] = &Item{ID: 1201, Name: "Knife", Type: "Weapon", Sub: "Dagger", Loc: 0x2, Atk: 17}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	knife := &InvItem{Index: 3, ItemID: 1201, Count: 1, Loc: 0x2}
	b := &Bot{P: &Persona{BaseLevel: 25}, invSeen: true, Zeny: 5000, MaxWeight: 10000, Weight: 1000,
		inv: map[int]*InvItem{2: bow, 3: knife, 5: {Index: 5, ItemID: 501, Count: 20}}, equipped: map[uint32]*InvItem{0x2: bow}}
	if !b.noArrows() {
		t.Fatal("a bow user with no arrows must be a need")
	}
	if b.needTrip() != "no arrows" {
		t.Fatalf("needTrip = %q", b.needTrip())
	}
	n := b.arrowsWanted(1, false)
	if n < 500 || n > 3000 { // arrowTarget: 1000 + lv x 30 (batch 22)
		t.Fatalf("arrows wanted %d", n)
	}
	// a non-bow user wants none, unless it is buying a bow now
	m := &Bot{P: &Persona{BaseLevel: 25}, invSeen: true, Zeny: 5000, MaxWeight: 10000, inv: map[int]*InvItem{3: knife},
		equipped: map[uint32]*InvItem{0x2: knife}}
	if m.arrowsWanted(1, false) != 0 || m.arrowsWanted(1, true) == 0 {
		t.Fatal("arrows only for bow users / a bow purchase")
	}
	// buy list parsing
	pk := make([]byte, 4+13*2)
	binary.LittleEndian.PutUint32(pk[4+4:], 10)
	binary.LittleEndian.PutUint32(pk[4+9:], 1201)
	binary.LittleEndian.PutUint32(pk[17+4:], 1)
	binary.LittleEndian.PutUint32(pk[17+9:], 1750)
	if listedPrice(pk, 1750) != 1 || listedPrice(pk, 501) != 0 {
		t.Fatal("listedPrice")
	}
	// bought arrows: stocked, and 0x013c puts them in the ammo slot
	b.inv[4] = &InvItem{Index: 4, ItemID: 1750, Count: 1500} // over 3/4 of the lv25 target (1750)
	if b.noArrows() || b.arrowsWanted(1, false) != 0 {
		t.Fatal("1500 arrows: no need, no top-up")
	}
	b.onArrowEquip([]byte{0x3c, 0x01, 4, 0})
	if a := b.equipped[eqpAmmo]; a == nil || !a.Equipped || a.ItemID != 1750 {
		t.Fatal("arrow equip ack")
	}
}

// QA 2026-11-14 06:05: archers "loaded" with nothing in the ammo slot. 0x0b09's WearState is the item db location
// (0x8000 on every arrow stack), not "worn"; only 0x013c fills the slot, and 0x013b type 0 empties it.
func TestArrowWearStateIsNotWorn(t *testing.T) {
	items[1750] = &Item{ID: 1750, Name: "Arrow", Type: "Ammo", Sub: "Arrow", Buy: 1, Weight: 1}
	items[1701] = &Item{ID: 1701, Name: "Bow", Type: "Weapon", Sub: "Bow", Loc: 0x22, Atk: 15}
	bow := &InvItem{Index: 2, ItemID: 1701, Count: 1, Loc: 0x22, Equipped: true}
	b := &Bot{P: &Persona{BaseLevel: 25, Chars: []CharSpec{{Name: "ArrowTest"}}}, inv: map[int]*InvItem{2: bow}, equipped: map[uint32]*InvItem{0x2: bow},
		cart: map[int]*InvItem{}}
	b.equipped[eqpAmmo] = &InvItem{Index: 9, ItemID: 1750, Count: 5, Equipped: true} // stale, from before
	pk := make([]byte, 5+34)
	binary.LittleEndian.PutUint16(pk, 0x0b09)
	binary.LittleEndian.PutUint16(pk[2:], uint16(len(pk)))
	binary.LittleEndian.PutUint16(pk[5:], 18)
	binary.LittleEndian.PutUint32(pk[7:], 1750)
	binary.LittleEndian.PutUint16(pk[12:], 750)
	binary.LittleEndian.PutUint32(pk[14:], eqpAmmo) // WearState = id->equip
	b.handle(pk)
	if a := b.inv[18]; a == nil || a.Count != 750 || a.Equipped || b.equipped[eqpAmmo] != nil {
		t.Fatalf("item list WearState must not mark arrows worn: %+v %+v", b.inv[18], b.equipped[eqpAmmo])
	}
	b.handle([]byte{0x3c, 0x01, 18, 0}) // the server's word: the slot holds index 18
	if a := b.equipped[eqpAmmo]; a == nil || !a.Equipped || a.Index != 18 {
		t.Fatal("0x013c fills the ammo slot")
	}
	b.handle([]byte{0x3b, 0x01, 3, 0}) // "ammo equipped" message: nothing changes
	if b.equipped[eqpAmmo] == nil {
		t.Fatal("0x013b type 3 is success")
	}
	b.handle([]byte{0x3b, 0x01, 0, 0}) // "Please equip the proper ammunition first"
	if b.equipped[eqpAmmo] != nil || b.inv[18].Equipped || !b.ammoAt.IsZero() {
		t.Fatal("0x013b type 0 empties the ammo slot so ammoThink equips again")
	}
}
