package main

import (
	"testing"
	"time"
)

// Run 5 traces: Knights never cast Two-Hand Quicken (attack skills spent the SP first), a Blacksmith's Hammer Fall
// starved Adrenaline Rush, and a combo Monk never resummoned spheres for Combo Finish.
func TestKeyBuffReserve(t *testing.T) {
	items[1158] = &Item{ID: 1158, Name: "Katana", Type: "Weapon", Sub: "2hSword", Loc: 0x22, Atk: 60}
	w := &InvItem{Index: 2, ItemID: 1158, Count: 1, Loc: 0x22, Equipped: true}
	now := time.Now()
	b := &Bot{P: &Persona{BaseLevel: 63}, equipped: map[uint32]*InvItem{0x2: w}, buffAt: map[string]time.Time{},
		skillFailUntil: map[int]time.Time{}, skills: map[int]*SkillInfo{60: {Lv: 10, SP: 50}}, SP: 80, MaxSP: 220}
	if r := b.keyReserve(now); r != 14 {
		t.Fatalf("THQ due: reserve %d, want 14 (lv1 cost)", r)
	}
	if !b.keyBuffDue(now) {
		t.Fatal("THQ due must hold Hammer Fall-type skills")
	}
	// 80 SP of 220, 30% floor = 66: lv1 (14 SP) fits exactly at 66 -> buffLevelLeft needs strictly more; 90 SP fits lv2
	if lv := buffLevelLeft(10, func(l int) int { return 10 + 4*l }, 90, 220, skillSPFloor); lv < 1 {
		t.Fatalf("a lower THQ level must fit at 90/220 SP, got %d", lv)
	}
	b.buffAt["self:60"] = now.Add(time.Minute)
	if r := b.keyReserve(now); r != 0 || b.keyBuffDue(now) {
		t.Fatalf("THQ up: reserve %d, due %v", r, b.keyBuffDue(now))
	}
	b.equipped = map[uint32]*InvItem{}
	delete(b.buffAt, "self:60")
	if r := b.keyReserve(now); r != 0 {
		t.Fatalf("no two-handed sword: THQ can't be cast, reserve %d", r)
	}
	m := &Bot{skills: map[int]*SkillInfo{moFinish: {Lv: 4}}}
	if !m.spheresKey() {
		t.Fatal("a Monk with Combo Finish keeps its spheres as a key skill")
	}
	if (&Bot{skills: map[int]*SkillInfo{moChain: {Lv: 5}}}).spheresKey() {
		t.Fatal("Chain Combo alone needs no spheres")
	}
}
