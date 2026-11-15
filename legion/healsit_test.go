package main

import (
	"testing"
	"time"
)

// owner 2026-11-14: "If it's not moving or blessing, it sits" - a party healer sits whenever it has nothing to do,
// and stands for a hurt member or a leader walking off.
func TestHealerIdleSit(t *testing.T) {
	b := &Bot{P: &Persona{Name: "zz_sitpriest", Chars: []CharSpec{{Name: "zz_sitpriest"}}}, Class: 8, HP: 900, MaxHP: 1000,
		SP: 500, MaxSP: 500, MapName: "pay_fild04", X: 50, Y: 50, skills: map[int]*SkillInfo{1: {Lv: 9}},
		buffAt: map[string]time.Time{}}
	now := time.Now()
	lead := pubState{Name: "zz_sitlead", Map: "pay_fild04", X: 53, Y: 52, LX: 53, LY: 52, Online: true, HPFrac: 1}
	members := []pubState{lead}
	if !b.healerIdleSit(lead, members, 50, 50, 53, 52, now) {
		t.Fatal("nothing to do, full SP: sit")
	}
	hurt := append(members, pubState{Name: "zz_sithurt", Map: "pay_fild04", X: 51, Y: 51, Online: true, HPFrac: 0.3})
	if b.healerIdleSit(lead, hurt, 50, 50, 53, 52, now) {
		t.Fatal("someone hurt: stand and heal")
	}
	walking := lead
	walking.DX, walking.DY = 70, 52
	if b.healerIdleSit(walking, members, 50, 50, 53, 52, now) {
		t.Fatal("the leader walks off: follow")
	}
	if b.healerIdleSit(lead, members, 50, 50, 60, 52, now) {
		t.Fatal("leader 10 cells away: follow first")
	}
}

// QA 2026-11-14 13:15: Kira storm sat/stood 15 times in 67 s behind a roaming leader (full SP).
func TestHealerIdleSitHysteresis(t *testing.T) {
	b := &Bot{P: &Persona{Name: "zz_flappriest", Chars: []CharSpec{{Name: "zz_flappriest"}}}, Class: 8, HP: 1000, MaxHP: 1000,
		SP: 500, MaxSP: 500, MapName: "m", X: 50, Y: 50, skills: map[int]*SkillInfo{1: {Lv: 9}}, buffAt: map[string]time.Time{}}
	now := time.Now()
	lead := pubState{Name: "zz_flaplead", Map: "m", X: 53, Y: 50, LX: 53, LY: 50, Online: true, HPFrac: 1}
	ms := []pubState{lead}
	b.healerIdleSit(lead, ms, 50, 50, 53, 50, now) // first sight
	lead.X, lead.LX = 55, 55                       // the leader moved and stopped
	if b.healerIdleSit(lead, ms, 50, 50, 55, 50, now.Add(time.Second)) {
		t.Error("the leader stopped 1 s ago: don't sit yet")
	}
	if !b.healerIdleSit(lead, ms, 50, 50, 55, 50, now.Add(6*time.Second)) {
		t.Fatal("the leader has stood 5 s: sit")
	}
	b.Sitting, b.idleSitAt = true, now.Add(6*time.Second)
	lead.X, lead.LX = 58, 58 // 8 cells: beyond the sit-down reach, inside the stay reach
	if !b.healerIdleSit(lead, ms, 50, 50, 58, 50, now.Add(8*time.Second)) {
		t.Error("seated, leader 8 cells away: stay seated (hysteresis)")
	}
	walk := lead
	walk.DX, walk.DY = 75, 50
	if !b.healerIdleSit(walk, ms, 50, 50, 58, 50, now.Add(9*time.Second)) {
		t.Error("seated 3 s ago: minimum sit time")
	}
	if b.healerIdleSit(walk, ms, 50, 50, 58, 50, now.Add(20*time.Second)) {
		t.Error("seated 14 s, the leader walks off: stand and follow")
	}
}
