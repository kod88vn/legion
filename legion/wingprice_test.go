package main

import (
	"os"
	"path/filepath"
	"testing"
)

// economy run 8: the Butterfly Wing price comes from the rathena db-import override (1,000 -> 300z), and the shops'
// real list price wins at runtime - correct before and after the rathena recreate.
func TestWingPriceOverride(t *testing.T) {
	defer wingPriceSeen.Store(0)
	wingPriceSeen.Store(0)
	items[itemButterflyWing] = &Item{ID: itemButterflyWing, Name: "Butterfly Wing", Buy: 1000, Weight: 50}
	dir := t.TempDir()
	f := filepath.Join(dir, "item_db.yml")
	os.WriteFile(f, []byte("Header:\n  Type: ITEM_DB\nBody:\n  - Id: 602\n    AegisName: Wing_Of_Butterfly\n    Buy: 300\n    Sell: 150\n  - Id: 999999\n    Buy: 5\n"), 0o644)
	loadItemOverrides(f)
	if items[itemButterflyWing].Buy != 300 || items[999999] != nil {
		t.Fatalf("override: Buy %d", items[itemButterflyWing].Buy)
	}
	if wingPrice() != 300 {
		t.Fatalf("wingPrice %d", wingPrice())
	}
	// the server still lists 1,000z (rathena not recreated yet): the shop's price wins
	b := &Bot{P: &Persona{}, inv: map[int]*InvItem{}, MaxWeight: 20000, Weight: 2000, BaseLv: 50, Zeny: 40000}
	b.butterflyWingsWanted(buyList(map[int]int{itemButterflyWing: 1000}), 10000, 0)
	if wingPrice() != 1000 {
		t.Fatalf("after a 1000z list: %d", wingPrice())
	}
	// 300z wings: a lv50 (floor 3,000z) with 4,000z carries 2 (900z spare buys a trip + the reserve); at 1,000z only 0
	b.Zeny = 4000
	if b.bwWant() != 0 {
		t.Fatalf("1000z wings, 4000z: want 0, got %d", b.bwWant())
	}
	wingPriceSeen.Store(300)
	if b.bwWant() != 2 {
		t.Fatalf("300z wings, 4000z: want 2, got %d", b.bwWant())
	}
	b.Zeny = 12000
	if b.bwWant() != bwTarget {
		t.Fatalf("300z wings, 12000z: want %d, got %d", bwTarget, b.bwWant())
	}
}
