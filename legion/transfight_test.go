package main

import (
	"testing"
	"time"
)

// trans skill rules: a Lord Knight keeps Aura Blade up (a Knight never sees it), a High Wizard amplifies only before a
// grouped crowd, Frenzy never solo
func TestTransBuffs(t *testing.T) {
	now := time.Now()
	saved := items[98811]
	items[98811] = &Item{ID: 98811, Type: "Weapon", Loc: 0x22, Sub: "2hSword"}
	defer func() {
		if saved == nil {
			delete(items, 98811)
		} else {
			items[98811] = saved
		}
	}()
	mobs[99911] = &Mob{ID: 99911, Level: 80, HP: 50000, Boss: true}
	defer delete(mobs, 99911)
	mk := func(class int, sk map[int]*SkillInfo) *Bot {
		b := &Bot{Class: class, skills: sk, buffAt: map[string]time.Time{}, skillFailUntil: map[int]time.Time{},
			SP: 400, MaxSP: 400, HP: 1000, MaxHP: 1000, X: 10, Y: 10, ents: map[uint32]*Entity{},
			equipped: map[uint32]*InvItem{0x2: {ItemID: 98811}}}
		b.P = &Persona{Chars: []CharSpec{{Name: "transtest"}}}
		return b
	}
	boss := &Entity{ID: 1, Type: objMob, Class: 99911, X: 11, Y: 10, DX: 11, DY: 10}
	lk := mk(4008, map[int]*SkillInfo{lkAuraBlade: {Lv: 5}, lkFrenzy: {Lv: 1}})
	if !lk.transBuffs(boss, now) || lk.State != "Aura Blade" {
		t.Fatalf("lord knight: %q", lk.State)
	}
	lk.skillReady = time.Time{}
	if lk.transBuffs(boss, now) { // Aura Blade up; Frenzy needs a party healer
		t.Fatalf("Frenzy solo: %q", lk.State)
	}
	if mk(7, map[int]*SkillInfo{lkAuraBlade: {Lv: 5}}).transBuffs(boss, now) {
		t.Fatal("a Knight has no trans rules")
	}
	hw := mk(4010, map[int]*SkillInfo{hwMagicAmp: {Lv: 5}})
	hw.equipped = map[uint32]*InvItem{}
	if hw.transBuffs(boss, now) {
		t.Fatal("amplify with one monster")
	}
	for i := uint32(2); i <= 4; i++ {
		hw.ents[i] = &Entity{ID: i, Type: objMob, Class: 99911, X: 12, Y: 11, DX: 12, DY: 11}
	}
	if !hw.transBuffs(boss, now) || hw.State != "Mystical Amplification" {
		t.Fatalf("high wizard: %q", hw.State)
	}
}

// Aid Potion: the best potion we carry up to the learned level, at the most hurt member in reach; none without potions
func TestAidPotion(t *testing.T) {
	now := time.Now()
	b := &Bot{Class: 18, aid: 1, skills: map[int]*SkillInfo{amAidPotion: {Lv: 5}}, buffAt: map[string]time.Time{},
		skillFailUntil: map[int]time.Time{}, SP: 100, MaxSP: 100, X: 10, Y: 10,
		partyHP: map[uint32][2]int{1: {10, 100}, 7: {40, 100}, 8: {90, 100}},
		ents:    map[uint32]*Entity{7: {ID: 7, X: 12, Y: 10, DX: 12, DY: 10}, 8: {ID: 8, X: 11, Y: 10, DX: 11, DY: 10}}}
	b.P = &Persona{Chars: []CharSpec{{Name: "aidtest"}}}
	if b.aidPotion(now) {
		t.Fatal("no potion in the bag")
	}
	b.inv = map[int]*InvItem{2: {ItemID: 503, Count: 3}}
	if !b.aidPotion(now) || b.buffAt["aid:7"].Before(now) {
		t.Fatal("Aid Potion on member 7")
	}
}
