package main

import (
	"strings"
	"testing"
	"time"
)

func restBot(class, hp, maxHP, sp, maxSP int) *Bot {
	now := time.Now()
	return &Bot{P: &Persona{BaseLevel: 80, Chars: []CharSpec{{Name: "me"}}}, Class: class, BaseLv: 80, HP: hp, MaxHP: maxHP,
		SP: sp, MaxSP: maxSP, MapName: "aldebaran", X: 140, Y: 120, Goal: Goal{Map: "payon", Mode: "party"},
		diedAt: now.Add(-3 * time.Minute), ents: map[uint32]*Entity{}, inv: map[int]*InvItem{},
		equipped: map[uint32]*InvItem{}, buffAt: map[string]time.Time{}}
}

// QA 2026-11-15: "low HP (100%): resting before moving on" (Monk, SP short after a death) and "low HP (89%)".
func TestLowHPGuardNoRestAtFullHP(t *testing.T) {
	now := time.Now()
	monk := restBot(15, 5000, 5000, 100, 500) // Monk, full HP, 20% SP, 3 min after a death
	if monk.lowHPGuard(nil, false, now) {
		t.Fatalf("a Monk at full HP must not rest for SP after a death: %q", monk.State)
	}
	if strings.Contains(monk.State, "low HP") {
		t.Fatalf("state %q", monk.State)
	}
	priest := restBot(8, 5000, 5000, 100, 500) // a real caster at full HP, SP short: SP rest, honestly labelled
	if !priest.lowHPGuard(nil, false, now) || !strings.HasPrefix(priest.State, "resting for SP") {
		t.Fatalf("priest full HP, 20%% SP after a death: want an SP rest, got %q", priest.State)
	}
	priest.SP = 480 // SP back: stands and goes on at once
	if priest.lowHPGuard(nil, false, now) || priest.wantSit {
		t.Fatalf("SP back over 90%%: no more rest (state %q, wantSit %v)", priest.State, priest.wantSit)
	}
	bunny := restBot(19, 890, 1000, 300, 300) // Bard at 89% after a death, not resting yet: goes on
	if bunny.lowHPGuard(nil, false, now) {
		t.Fatalf("89%% HP after a death must not start a rest: %q", bunny.State)
	}
	bunny.HP = 700 // under the post-death entry line: rests ...
	if !bunny.lowHPGuard(nil, false, now) || !strings.Contains(bunny.State, "low HP (70%)") {
		t.Fatalf("70%% after a death: want a rest, got %q", bunny.State)
	}
	bunny.HP = 890 // ... until 90%
	if !bunny.lowHPGuard(nil, false, now) {
		t.Fatal("a started post-death rest runs to 90%")
	}
	bunny.HP = 900
	if bunny.lowHPGuard(nil, false, now) || bunny.wantSit {
		t.Fatal("at 90% the rest ends at once")
	}
}
