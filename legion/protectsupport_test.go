package main

import (
	"testing"
	"time"
)

// AY26 flag off and not an engine party: nothing applies, and the member flow is untouched (no state, no target change).
func TestProtectSupportOffByDefault(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyProtectSupportOn = false; tn.EngineParties = "" })
	lead := stickBot("Lead", "pay_fild01", 100, 100)
	fr := stickBot("Front", "pay_fild01", 100, 100)
	sup := stickBot("Priest", "pay_fild01", 130, 100) // 30 cells away: would be leashed with the flag on
	stickPub(sup)
	stickParty(t, lead, fr, sup)
	p := partyOf(fr)
	if partyProtectOn(p) {
		t.Fatal("protect-support applied to a bot party with the flag off")
	}
	fr.target = 77
	gm := &GameMap{Name: "pay_fild01"}
	members := []pubState{{Name: "Front", Map: "pay_fild01", X: 100, Y: 100, Online: true}, {Name: "Priest", Map: "pay_fild01", X: 130, Y: 100, Online: true, Support: true}}
	if fr.protectOwnSupport(p, members, gm, true, time.Now()) {
		t.Fatal("flag off: protectOwnSupport acted")
	}
	if fr.target != 77 {
		t.Fatalf("flag off changed the target to %d", fr.target)
	}
}

// An engine party (human in engine_parties, Party.Engine) has the rule with the flag off.
func TestProtectSupportEnginePartyOn(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyProtectSupportOn = false; tn.EngineParties = "AylaOwn" })
	lead := stickBot("AylaOwn", "pay_fild01", 100, 100)
	sup := stickBot("Priest", "pay_fild01", 105, 100)
	stickParty(t, lead, sup)
	p := partyOf(lead)
	if partyProtectOn(p) {
		t.Fatal("a party that is not an engine party got the rule with the flag off")
	}
	p.Engine = true
	if !partyProtectOn(p) {
		t.Fatal("engine party with the flag off: the rule did not apply")
	}
	p.Engine = false
	withTuning(t, func(tn *Tuning) { tn.PartyProtectSupportOn = true; tn.EngineParties = "" })
	if !partyProtectOn(p) {
		t.Fatal("flag on: the rule did not apply to a bot party")
	}
}

func TestSupportLeashed(t *testing.T) {
	cases := []struct {
		name   string
		mapM   string
		mx, my int
		mapS   string
		sx, sy int
		want   bool
	}{
		{"within 15", "m", 100, 100, "m", 115, 100, false},
		{"exactly 15 is not leashed", "m", 100, 100, "m", 115, 115, false},
		{"16 cells is leashed", "m", 100, 100, "m", 116, 100, true},
		{"other map is the travel logic's", "m", 100, 100, "n", 200, 100, false},
		{"no position yet", "m", 0, 0, "m", 200, 100, false},
	}
	for _, c := range cases {
		if got := supportLeashed(c.mapM, c.mx, c.my, c.mapS, c.sx, c.sy); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestGuardAndBackSpots(t *testing.T) {
	// monster due east of the support: the frontline stands 2 cells east of it, the ranged 3 cells west of it
	if x, y, ok := guardSpot(100, 100, 110, 100, supportGuardGap); !ok || x != 102 || y != 100 {
		t.Fatalf("guardSpot east: %d,%d %v", x, y, ok)
	}
	if x, y, ok := backSpot(100, 100, 110, 100, supportBackGap); !ok || x != 97 || y != 100 {
		t.Fatalf("backSpot east: %d,%d %v", x, y, ok)
	}
	// a diagonal monster: the spot lies on the line through the support
	if x, y, ok := guardSpot(100, 100, 108, 108, 2); !ok || x != 102 || y != 102 {
		t.Fatalf("guardSpot diagonal: %d,%d %v", x, y, ok)
	}
	// a monster closer than the gap: the frontline stands on the monster's cell side, no further than the monster
	if x, y, ok := guardSpot(100, 100, 101, 100, 2); !ok || x != 101 || y != 100 {
		t.Fatalf("guardSpot near monster: %d,%d %v", x, y, ok)
	}
	if _, _, ok := guardSpot(100, 100, 100, 100, 2); ok {
		t.Fatal("guardSpot on the support's own cell must not pick a spot")
	}
	if _, _, ok := backSpot(100, 100, 100, 100, 3); ok {
		t.Fatal("backSpot on the support's own cell must not pick a spot")
	}
}

func TestProtectSpotRoles(t *testing.T) {
	// leashed: every role walks back to the support
	if x, y, _, ok := protectSpot(roleRanged, 0, 0, 200, 200, 0, 0, false, true); !ok || x != 200 || y != 200 {
		t.Fatalf("leashed ranged: %d,%d %v", x, y, ok)
	}
	// frontline: a monster 8 cells east of the support -> stand between at 2 cells, from 40 cells away
	if x, y, _, ok := protectSpot(roleTank, 60, 100, 100, 100, 108, 100, true, false); !ok || x != 102 || y != 100 {
		t.Fatalf("tank between: %d,%d %v", x, y, ok)
	}
	// ranged: the threat within supportRangedAct of it -> behind the support
	if x, y, _, ok := protectSpot(roleRanged, 104, 100, 100, 100, 108, 100, true, false); !ok || x != 97 || y != 100 {
		t.Fatalf("ranged behind: %d,%d %v", x, y, ok)
	}
	// ranged far from the threat: it is not in danger, it stays
	if _, _, _, ok := protectSpot(roleRanged, 80, 100, 100, 100, 108, 100, true, false); ok {
		t.Fatal("ranged far from the threat moved")
	}
	// already at its spot (within 2): no walk
	if _, _, _, ok := protectSpot(roleTank, 102, 100, 100, 100, 108, 100, true, false); ok {
		t.Fatal("frontline already between moved again")
	}
	// no threat near the support and not leashed: nothing to do
	if _, _, _, ok := protectSpot(roleTank, 60, 100, 100, 100, 0, 0, false, false); ok {
		t.Fatal("no threat: moved")
	}
	// the support's own role is never moved by this rule
	if _, _, _, ok := protectSpot(roleSupport, 60, 100, 100, 100, 108, 100, true, false); ok {
		t.Fatal("support role moved")
	}
}

func TestSupportOfPicksOtherSupport(t *testing.T) {
	members := []pubState{
		{Name: "Me", Map: "m", X: 10, Y: 10, Online: true, Support: true},
		{Name: "Priest", Map: "m", X: 20, Y: 10, Online: true, Support: true},
	}
	s, ok := supportOf(members, "Me")
	if !ok || s.Name != "Priest" {
		t.Fatalf("support of Me: %+v %v", s, ok)
	}
	if _, ok := supportOf(members[:1], "Me"); ok {
		t.Fatal("a party with only the member itself has a support")
	}
}

// protectOwnSupport with the flag on: a ranged member 30 cells from its support walks back (moving=true: no walk call
// in the test) and drops its target; nothing is done with the flag off (covered above).
func TestProtectOwnSupportLeashOn(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyProtectSupportOn = true; tn.EngineParties = "" })
	lead := stickBot("Lead", "pay_fild01", 100, 100)
	arch := stickBot("Archer", "pay_fild01", 100, 100)
	sup := stickBot("Priest", "pay_fild01", 130, 100)
	stickParty(t, lead, arch, sup)
	p := partyOf(arch)
	members := []pubState{
		{Name: "Lead", Map: "pay_fild01", X: 100, Y: 100, Online: true},
		{Name: "Archer", Map: "pay_fild01", X: 100, Y: 100, Online: true},
		{Name: "Priest", Map: "pay_fild01", X: 130, Y: 100, Online: true, Support: true},
	}
	arch.target = 77
	gm := &GameMap{Name: "pay_fild01"}
	if !arch.protectOwnSupport(p, members, gm, true, time.Now()) {
		t.Fatal("flag on, archer 30 cells from its priest: the leash did not act")
	}
	if arch.target != 0 {
		t.Fatalf("leashed member kept target %d", arch.target)
	}
}
