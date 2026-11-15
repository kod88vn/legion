package main

import "testing"

// giftShortfall uses the buy's own reserve rule (min(base + lv*per, zeny/div)): a member who can already afford a
// 50z Rod gets nothing (it was handed 400z), and a short one gets just the gap.
func TestGiftShortfall(t *testing.T) {
	div := tune().GearReserveZenyDiv
	if n := giftShortfall(50, 600, 900); n > 0 {
		t.Fatalf("600z buys a 50z Rod, shortfall %d", n)
	}
	n := giftShortfall(1000, 500, 900)
	if n <= 0 || n > 1000 {
		t.Fatalf("1000z bow with 500z: shortfall %d", n)
	}
	z := 500 + n
	if z-min(900, z/div) < 1000 {
		t.Fatalf("after the gift (%dz) the bow is still unaffordable", z)
	}
}

func TestIsClassWeapon(t *testing.T) {
	knife, bow, rod := &Item{Sub: "Dagger"}, &Item{Sub: "Bow"}, &Item{Sub: "Staff"}
	if isClassWeapon(famArcher, knife) || !isClassWeapon(famArcher, bow) || isClassWeapon(famMage, knife) ||
		!isClassWeapon(famMage, rod) || !isClassWeapon(famAcolyte, rod) || isClassWeapon(famSword, knife) || !isClassWeapon(famThief, knife) {
		t.Fatal("class weapon table")
	}
}
