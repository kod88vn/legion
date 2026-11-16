package main

import (
	"testing"
	"time"
)

// AY11 (2026-11-15): Ayla's party is five, Ayla included: 2 frontline, 2 ranged, 1 support - Ayla (Assassin), Crusader,
// Hunter, Wizard, Priest.
const (
	clsAssassin = 12
	clsCrusader = 14
	clsHunter   = 11
	clsWizard   = 9
	clsPriest   = 8
	clsKnight   = 7
	clsMonk     = 15
	clsAcolyte  = 4
	clsSage     = 16
	clsBard     = 19
	clsNovice   = 0
)

func TestCompSlotByClass(t *testing.T) {
	cases := map[string]struct{ class, slot int }{
		"Assassin": {clsAssassin, slotFront},
		"Crusader": {clsCrusader, slotFront},
		"Knight":   {clsKnight, slotFront},
		"Monk":     {clsMonk, slotFront},
		"Hunter":   {clsHunter, slotRanged},
		"Wizard":   {clsWizard, slotRanged},
		"Sage":     {clsSage, slotRanged},
		"Bard":     {clsBard, slotRanged},
		"Priest":   {clsPriest, slotSupport},
		"Acolyte":  {clsAcolyte, slotSupport},
		"Novice":   {clsNovice, slotNone},
	}
	for name, c := range cases {
		if got := compSlot(c.class); got != c.slot {
			t.Errorf("%s: slot %d, want %d", name, got, c.slot)
		}
	}
}

// compSlot must agree with partyRole for every class at its default stats (AGI 1: an Assassin is a melee, a frontline).
func TestCompSlotMatchesPartyRole(t *testing.T) {
	slotOfRole := map[int]int{roleTank: slotFront, roleMelee: slotFront, roleRanged: slotRanged, roleSupport: slotSupport, roleNone: slotNone}
	for _, class := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 15, 16, 17, 18, 19, 20, 23, 4002, 4008, 4016, 4021} {
		b := &Bot{Class: class}
		if want, got := slotOfRole[partyRole(b)], compSlot(class); got != want {
			t.Errorf("class %d: compSlot %d, partyRole says %d", class, got, want)
		}
	}
}

func TestCompositionAylaPartyIsOK(t *testing.T) {
	five := []int{clsAssassin, clsCrusader, clsHunter, clsWizard, clsPriest}
	if !compositionOK(five) {
		t.Fatalf("Ayla, Crusader, Hunter, Wizard, Priest must be the AY11 shape")
	}
	if f, r, s, n := rosterComposition(five); f != 2 || r != 2 || s != 1 || n != 0 {
		t.Fatalf("composition %d/%d/%d/%d, want 2/2/1/0", f, r, s, n)
	}
}

func TestCompositionRejectsOtherShapes(t *testing.T) {
	cases := map[string][]int{
		"four members":     {clsAssassin, clsCrusader, clsHunter, clsPriest},
		"three frontlines": {clsAssassin, clsCrusader, clsKnight, clsHunter, clsPriest},
		"three ranged":     {clsAssassin, clsCrusader, clsHunter, clsWizard, clsWizard},
		"no support":       {clsAssassin, clsCrusader, clsHunter, clsWizard, clsKnight},
		"a novice":         {clsAssassin, clsCrusader, clsHunter, clsWizard, clsNovice},
		"six members":      {clsAssassin, clsCrusader, clsHunter, clsWizard, clsPriest, clsKnight},
	}
	for name, cls := range cases {
		if compositionOK(cls) {
			t.Errorf("%s: accepted as the AY11 shape", name)
		}
	}
}

// A replacement keeps the composition: the swap must stay in the same slot (an Assassin for a Knight is a frontline
// for a frontline; a Hunter for a Wizard is ranged for ranged; a Hunter for a Priest is refused).
func TestSwapKeepsComposition(t *testing.T) {
	five := []int{clsAssassin, clsCrusader, clsHunter, clsWizard, clsPriest}
	if !swapKeepsComposition(clsCrusader, clsKnight) || !swapKeepsComposition(clsHunter, clsSage) {
		t.Fatal("a same-slot swap must be allowed")
	}
	if swapKeepsComposition(clsPriest, clsHunter) || swapKeepsComposition(clsWizard, clsPriest) {
		t.Fatal("a cross-slot swap must be refused")
	}
	if swapKeepsComposition(clsHunter, clsNovice) {
		t.Fatal("a member that fits no slot must be refused")
	}
	// the slot counts of the five are unchanged by an allowed swap
	swapped := []int{clsAssassin, clsKnight, clsHunter, clsWizard, clsPriest}
	if !compositionOK(swapped) || !swapKeepsComposition(clsCrusader, clsKnight) {
		t.Fatalf("a Knight in for the Crusader keeps 2/2/1 (%v)", five)
	}
}

// pickRotation refuses a swap that changes the composition, even when the rotation roles match: a Monk (rotRole support,
// frontline slot) is never replaced by a Bard (rotRole support, ranged slot); a Hunter is replaced by an Archer.
func TestRotationKeepsComposition(t *testing.T) {
	now := time.Now()
	lg := &rotLog{}
	monk := []rotMember{{Char: "Monky", Class: clsMonk, Build: "monk", Lv: 80}}
	bard := []rotCand{{Char: "Bardy", Account: "a7", Class: 19, Build: "bard", Lv: 80, Guild: "Starveil", Online: true}}
	if _, _, ok, _ := pickRotation(monk, bard, lg, 80, "Starveil", now); ok {
		t.Fatal("a Bard must not replace a Monk: the frontline slot would become ranged")
	}
	hunter := []rotMember{{Char: "Huntsy", Class: clsHunter, Build: "hunter", Lv: 80}}
	archer := []rotCand{{Char: "Archy", Account: "a8", Class: 3, Build: "archer", Lv: 80, Guild: "Starveil", Online: true}}
	if out, in, ok, why := pickRotation(hunter, archer, lg, 80, "Starveil", now); !ok || out.Char != "Huntsy" || in.Char != "Archy" {
		t.Fatalf("an Archer for a Hunter keeps the ranged slot: ok=%v why=%q", ok, why)
	}
}
