package main

import "testing"

// QA 2026-11-14 07:24: Archers/Hunters/Bards/Assassins sat to rest for SP at full HP after burning it on skills.
// Only SP casters (Mage/Wizard/Sage line) and healers (Acolyte/Priest line) rest for SP; everyone else auto-attacks.
func TestOnlyCastersRestForSP(t *testing.T) {
	mk := func(class, sp, maxSP int) *Bot {
		return &Bot{P: &Persona{BaseLevel: 40, Chars: []CharSpec{{Name: "sptest"}}}, Class: class,
			HP: 700, MaxHP: 700, SP: sp, MaxSP: maxSP, inv: map[int]*InvItem{}, equipped: map[uint32]*InvItem{}}
	}
	for _, c := range []struct {
		name  string
		class int
		rest  bool
	}{
		{"archer", 3, false}, {"hunter", 11, false}, {"bard", 19, false}, {"assassin", 12, false},
		{"knight", 7, false}, {"monk", 15, false}, {"blacksmith", 10, false},
		{"mage", 2, true}, {"wizard", 9, true}, {"sage", 16, true}, {"acolyte", 4, true}, {"priest", 8, true},
	} {
		b := mk(c.class, 5, 100)
		if got := b.lowSP(); got != c.rest {
			t.Errorf("%s at 5%% SP: lowSP %v, want %v", c.name, got, c.rest)
		}
		if b.spCaster() != c.rest {
			t.Errorf("%s: spCaster %v", c.name, b.spCaster())
		}
		if !c.rest && !b.fightSPLow() {
			t.Errorf("%s at 5%% SP: fightSPLow false (it may drink an SP potion in a fight)", c.name)
		}
		if b2 := mk(c.class, 60, 100); b2.lowSP() || b2.fightSPLow() {
			t.Errorf("%s at 60%% SP: low", c.name)
		}
	}
}
