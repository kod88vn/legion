package main

import (
	"testing"
	"time"
)

// owner rule 2026-11-14 "Tanks are active": the tank engages first, takes every monster targeting a member, pulls
// around the group's front and stays inside together_cells.
func TestTankLead(t *testing.T) {
	now := time.Now()
	// front: 5 cells ahead of a walking leader, none for a standing one or a human (no published walk)
	walking := pubState{Name: "lead", Map: "m", X: 100, Y: 100, LX: 100, LY: 100, DX: 112, DY: 100, Online: true}
	if fx, fy, ok := tankFront(walking); !ok || fx != 105 || fy != 100 {
		t.Errorf("front of a leader walking east: %d,%d %v, want 105,100", fx, fy, ok)
	}
	standing := walking
	standing.DX, standing.DY = 101, 100
	if _, _, ok := tankFront(standing); ok {
		t.Error("a leader 1 cell from its walk's end has no front")
	}
	human := pubState{Name: "Ayla", Map: "m", X: 100, Y: 100, Online: true}
	if _, _, ok := tankFront(human); ok {
		t.Error("a human leader publishes no walk: no front")
	}
	// pull range: 12 around the leader, or 12 around the front while within together-4 (16) of the leader
	if !tankPullOK(standing, 112, 100, 12) || tankPullOK(standing, 113, 100, 12) {
		t.Error("standing leader: pull radius is tank_pull_radius (12)")
	}
	if !tankPullOK(walking, 116, 100, 12) {
		t.Error("walking leader: a monster 16 cells ahead (11 from the front) is pullable")
	}
	if tankPullOK(walking, 117, 100, 12) {
		t.Error("never beyond together_cells-4 from the leader")
	}
	if tankPullOK(walking, 84, 100, 12) {
		t.Error("16 cells behind a walking leader is not the front")
	}

	// peel: the nearest monster targeting another member (not one on us, not a stale target, not 13+ cells away)
	b := &Bot{P: &Persona{BaseLevel: 40, Chars: []CharSpec{{Name: "tank"}}}, aid: 1, X: 100, Y: 100, DestX: 100, DestY: 100,
		MapName: "m", ents: map[uint32]*Entity{}, buffAt: map[string]time.Time{}}
	members := []pubState{
		{Name: "tank", AID: 1, Map: "m", X: 100, Y: 100, Online: true},
		{Name: "priest", AID: 2, Map: "m", X: 104, Y: 100, Online: true, Support: true},
		{Name: "wiz", AID: 3, Map: "m", X: 96, Y: 100, Online: true, Target: 77},
	}
	b.ents[10] = &Entity{ID: 10, Type: objMob, X: 101, Y: 100, Target: 1, TargetAt: now}                       // on me
	b.ents[11] = &Entity{ID: 11, Type: objMob, X: 106, Y: 100, Target: 2, TargetAt: now}                       // on the priest
	b.ents[12] = &Entity{ID: 12, Type: objMob, X: 103, Y: 100, Target: 3, TargetAt: now.Add(-6 * time.Second)} // stale
	b.ents[13] = &Entity{ID: 13, Type: objMob, X: 114, Y: 100, Target: 3, TargetAt: now}                       // too far
	if e, who, _ := b.tankPeelTarget(members, now); e == nil || e.ID != 11 || who != "priest" {
		t.Fatalf("peel: got %v %q, want the monster on the priest", e, who)
	}
	delete(b.ents, 11)
	if e, _, _ := b.tankPeelTarget(members, now); e != nil {
		t.Errorf("nothing on a member within 12 cells: got %d", e.ID)
	}
	// a member fighting near us keeps the tank from sitting down for its own rest
	if !b.membersFighting(members) {
		t.Error("the wizard is fighting 4 cells away: members are fighting")
	}
	members[2].Target = 0
	if b.membersFighting(members) {
		t.Error("nobody else fighting")
	}
}

// party watcher 2026-11-14 (pay_dun01): Echobunny Fly-Winged away from 8-12 monsters at 46-48% HP, leaving the priest.
// Owner: "knight can pull but must use AOE instead of escaping" - a party tank with potions or a priest stays.
func TestTankHoldsCrowd(t *testing.T) {
	crowd := fightView{hp: 0.46, n: 12, maxLv: 60, lv: 70, base: 70, pots: 40, tank: true}
	if ok, _ := losingFight(crowd); !ok {
		t.Fatal("12 monsters at 46% is a lost fight for anyone else")
	}
	if !tankHolds(crowd, true) || !tankHolds(crowd, false) {
		t.Error("party tank with potions (and/or a priest) holds a crowd at 46%")
	}
	low := crowd
	low.hp = 0.18
	if !tankHolds(low, true) {
		t.Error("18% with potions and the priest near: still holds")
	}
	if tankHolds(low, false) {
		t.Error("18% without a priest near: last resort, escape")
	}
	low.pots = 0
	if tankHolds(low, true) {
		t.Error("18% with no potions: escape")
	}
	dying := crowd
	dying.hp = 0.08
	if tankHolds(dying, true) {
		t.Error("under 10%: escape whatever")
	}
	strong := crowd
	strong.maxLv = 90
	if tankHolds(strong, true) {
		t.Error("a monster far above us: escape")
	}
	none := crowd
	none.pots = 0
	if tankHolds(none, false) {
		t.Error("no potions, no priest: escape as before")
	}

	now := time.Now()
	b := &Bot{P: &Persona{BaseLevel: 70, Chars: []CharSpec{{Name: "tank"}}}, aid: 1, MaxSP: 200, SP: 200,
		ents: map[uint32]*Entity{}, skills: map[int]*SkillInfo{}}
	if b.tankCrowdCost() != 0 {
		t.Error("no crowd skill learned: cost 0")
	}
	b.skills[62] = &SkillInfo{ID: 62, Lv: 10, SP: 22}
	if c := b.tankCrowdCost(); c != 22 {
		t.Errorf("Bowling Bash cost %d, want 22", c)
	}
	for i := uint32(0); i < 3; i++ {
		b.ents[80+i] = &Entity{ID: 80 + i, Type: objMob, Target: 1, TargetAt: now}
	}
	b.ents[90] = &Entity{ID: 90, Type: objMob, Target: 7, TargetAt: now}
	if h := b.tankHeld(now); h != 3 {
		t.Errorf("held %d, want 3", h)
	}
}

// Wizard lure: the pack goes 4 cells in front of the caster, never on top of it; inside 2-7 cells the tank stays.
func TestWizLureSpot(t *testing.T) {
	if x, y, walk := wizLureSpot(110, 100, 100, 100, nil); !walk || x != 104 || y != 100 {
		t.Errorf("tank 10 cells east: lure to 104,100, got %d,%d walk=%v", x, y, walk)
	}
	if _, _, walk := wizLureSpot(105, 103, 100, 100, nil); walk {
		t.Error("tank 5 cells from the caster: already in front, fight there")
	}
	if x, y, walk := wizLureSpot(100, 100, 100, 100, nil); !walk || dist(x, y, 100, 100) < wizZoneMin {
		t.Errorf("tank on the caster's cell: step out in front, got %d,%d walk=%v", x, y, walk)
	}
	onlyThree := func(x, y int) bool { return x == 103 }
	if x, _, _ := wizLureSpot(110, 100, 100, 100, onlyThree); x != 103 {
		t.Errorf("4 cells out unwalkable: take 3, got x=%d", x)
	}
}

// The tank holds while the caster's spell is in the air, and lures to the caster rather than the priest.
func TestTankWizardLure(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "tank"}}}, aid: 1, X: 112, Y: 100, DestX: 112, DestY: 100,
		MapName: "m", ents: map[uint32]*Entity{}, buffAt: map[string]time.Time{}}
	wiz := pubState{Name: "wiz", AID: 3, Map: "m", X: 100, Y: 100, Online: true, AoE: true}
	members := []pubState{{Name: "tank", AID: 1, Map: "m", X: 112, Y: 100, Online: true}, wiz}
	if done, _ := b.tankWizardLure(nil, members, 112, 100, 4, false, now); !done || b.State != "luring 4 monsters to wiz" {
		t.Fatalf("crowd on the tank 12 cells from the wizard: lure, got done=%v state=%q", done, b.State)
	}
	members[1].CastUntil = now.Add(3 * time.Second)
	if done, owns := b.tankWizardLure(nil, members, 107, 100, 4, false, now); !done || owns {
		t.Fatalf("wizard casting, tank 7 cells away: hold (done, not walking), got %v %v", done, owns)
	}
	members[1].AoE = false
	if done, _ := b.tankWizardLure(nil, members, 112, 100, 4, false, now); done {
		t.Fatal("no area caster: the priest rule decides")
	}
}
