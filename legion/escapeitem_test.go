package main

import (
	"testing"
	"time"
)

// QA 2026-11-14 14:12 UTC trace (vexcat on gef_fild03): Fly Wing ack, 0x0147, then "SEND skill 28" (Heal) went out
// before "SEND skill 26": rAthena cleared the item skill and the teleport did nothing. While a wing is pending, only
// the teleport may be cast, and the bot must not sit.
func TestEscHoldBlocksOtherSkills(t *testing.T) {
	b := &Bot{}
	defer escHold.Delete(b)
	if b.escHolding(28) {
		t.Fatal("no wing pending: skills must go out")
	}
	escHold.Store(b, time.Now().Add(2*time.Second))
	if !b.escHolding(28) || !b.escHolding(0) {
		t.Fatal("wing pending: Heal and sitting must wait")
	}
	if b.escHolding(26) {
		t.Fatal("the wing's own teleport (26) must go out")
	}
	escHold.Store(b, time.Now().Add(-time.Second))
	if b.escHolding(28) {
		t.Fatal("expired hold must not block")
	}
}

// QA 2026-11-14 16:20 UTC (core): Zen.fox left Payon at ~45% HP after a respawn and died walking into pay_dun01.
// After a death, on the way to another map, the rest line is 90% (heal.go lowHPGuard).
func TestRejoinAfterDeath(t *testing.T) {
	now := time.Now()
	b := &Bot{MapName: "payon", Goal: Goal{Map: "pay_dun01", Mode: "party"}}
	if b.rejoinAfterDeath(now) {
		t.Fatal("no death: normal rest lines")
	}
	b.diedAt = now.Add(-2 * time.Minute)
	if !b.rejoinAfterDeath(now) {
		t.Fatal("respawned 2 min ago and heading to pay_dun01: rest to 90% first")
	}
	b.diedAt = now.Add(-11 * time.Minute)
	if b.rejoinAfterDeath(now) {
		t.Fatal("a death 11 min ago no longer counts")
	}
	b.diedAt = now.Add(-time.Minute)
	b.Goal.Map = "payon"
	if b.rejoinAfterDeath(now) {
		t.Fatal("no trip and no party elsewhere: nothing to rejoin")
	}
}

// QA 2026-11-14 11:02-11:28 deploy measurement: party tanks (Monks too) Fly-Winged "losing: N monsters" at 33-48% HP.
// tanklead.go tankHolds keeps them in the fight while they have potions or a priest near; only a far stronger
// monster, under 10%, or under 20% with nothing to keep them alive sends them out.
func TestTankHoldsSamples(t *testing.T) {
	cases := []struct {
		name   string
		v      fightView
		priest bool
		want   bool
	}{
		{"Sunheart 7 on me at 48%, potions, priest near", fightView{hp: 0.48, n: 7, maxLv: 55, lv: 49, pots: 40, tank: true}, true, true},
		{"2 on me at 33%, potions, no priest", fightView{hp: 0.33, n: 2, maxLv: 47, lv: 49, pots: 40, tank: true}, false, true},
		{"out of potions, no priest, 31%", fightView{hp: 0.31, n: 1, maxLv: 47, lv: 49, pots: 0, tank: true}, false, false},
		{"a level 59 monster on a lv49 tank", fightView{hp: 0.4, n: 1, maxLv: 59, lv: 49, pots: 40, tank: true}, true, false},
		{"under 10%", fightView{hp: 0.08, n: 3, maxLv: 47, lv: 49, pots: 40, tank: true}, true, false},
	}
	for _, c := range cases {
		if got := tankHolds(c.v, c.priest); got != c.want {
			t.Errorf("%s: tankHolds = %v, want %v", c.name, got, c.want)
		}
	}
}

// QA 2026-11-14 16:12 UTC: LilMoonsage Bless (lv6) alberta -Butterfly Wing-> ... -> pay_fild03 -> alberta on a shop
// trip was called a warp loop and the way to the shop avoided. A teleport leg (ai.go via "tele") is no portal bounce.
func TestWarpLoopIgnoresTeleports(t *testing.T) {
	h := []mapVisit{{m: "pay_fild03"}, {m: "alberta", via: "tele"}, {m: "pay_fild03", via: "alberta:117:38"}, {m: "alberta", via: "pay_fild03:392:63"}}
	if warpLoopLegs(h) {
		t.Fatal("a Butterfly Wing / Kafra leg must not make a warp loop")
	}
	h[1].via = "pay_fild03:392:63"
	if !warpLoopLegs(h) {
		t.Fatal("the same portal twice is still a loop")
	}
}

// QA 2026-11-14 15:24:50 / 16:16 UTC (Onimage): a potion or a ground skill between the wing and its teleport voids it.
func TestEscBlocksPotionAndGroundSkill(t *testing.T) {
	b := &Bot{}
	defer escHold.Delete(b)
	defer escHoldIdx.Delete(b)
	potion := newPkt(0x0439).u16(7).u32(1).b
	wing := newPkt(0x0439).u16(3).u32(1).b
	ground := newPkt(0x0366).u16(10).u16(89).u16(100).u16(100).b
	if b.escBlocks(potion) || b.escBlocks(ground) {
		t.Fatal("no wing pending: nothing blocked")
	}
	escHold.Store(b, time.Now().Add(2*time.Second))
	escHoldIdx.Store(b, 3)
	if !b.escBlocks(potion) || !b.escBlocks(ground) {
		t.Fatal("wing pending: potion and ground skill must wait")
	}
	if b.escBlocks(wing) {
		t.Fatal("the wing itself must go out")
	}
}

// QA 2026-11-14 17:00 UTC (core, pay_dun01): Zen.fox ran 4 cells into a dead end; every cell straight away from the
// skeletons was a wall and runFrom sent nothing for 12 s. fleeCell must find another way out.
func TestFleeCellDeadEnd(t *testing.T) {
	m := testMap("fleetest", 40, 40)
	m.open(5, 20, 30, 20) // an east-west corridor
	m.open(30, 5, 30, 20) // turning north at its east end
	maps[m.Name] = m
	defer delete(maps, m.Name)
	// at the corridor's west dead end (5,20), attackers to the east (8,20): straight away (west) is a wall
	x, y, ok := fleeCell(m, 5, 20, 8, 20)
	if !ok {
		t.Fatal("no flee cell found in a dead end")
	}
	if !m.Walkable(x, y) {
		t.Fatalf("flee cell %d,%d not walkable", x, y)
	}
	// in the corridor with attackers west: run east
	x, _, ok = fleeCell(m, 15, 20, 12, 20)
	if !ok || x <= 15 {
		t.Fatalf("flee from the west should go east, got x=%d ok=%v", x, ok)
	}
	// the step toward the party follows the corridor round the corner
	tx, ty := towardOnPath(m, 10, 20, 30, 6, 12)
	if !m.Walkable(tx, ty) || dist(10, 20, tx, ty) > 12 {
		t.Fatalf("towardOnPath gave %d,%d", tx, ty)
	}
}

// QA 2026-11-14 (TheShadowsenpai, Alchemist 69 with lv51 gear, mjolnir_12): 15 Fly Wings in 2 min with one lv63-65
// monster on it, 84% -> 33% HP. Every escape now counts for the throttle (b.escAll), even fights included.
func TestWingThrottleSequence(t *testing.T) {
	now := time.Now()
	b := &Bot{}
	n := 0
	for i := 0; i < 15; i++ { // an escape every 5 s
		ts := now.Add(time.Duration(i*5) * time.Second)
		b.escAll = append(b.escAll, ts)
		for len(b.escAll) > 0 && ts.Sub(b.escAll[0]) > 2*time.Minute {
			b.escAll = b.escAll[1:]
		}
		again := len(b.escAll)
		hp := 0.84 - float64(i)*0.035
		if (again <= 4 || hp < 0.5) && (again <= 6 || hp < 0.25) {
			n++
		}
	}
	if n > 9 {
		t.Fatalf("%d Fly Wings in 75 s at 84%%->35%% HP: the throttle must stop most of them", n)
	}
}

// QA 2026-11-14 19:41-19:49 UTC (core): party members Fly-Winged 7-10 times inside pay_dun00/01 when Ayla was over 40
// cells away. fleeCell / towardOnPath must still give a walkable leg toward a far party on a long winding path.
func TestTowardOnPathFar(t *testing.T) {
	m := testMap("fartest", 120, 60)
	m.open(2, 2, 110, 2)
	m.open(110, 2, 110, 50)
	m.open(10, 50, 110, 50)
	maps[m.Name] = m
	defer delete(maps, m.Name)
	x, y := towardOnPath(m, 2, 2, 10, 50, 12) // ~250 path cells away
	if !m.Walkable(x, y) || dist(2, 2, x, y) > 12 || x <= 2 {
		t.Fatalf("first leg toward a far party = %d,%d", x, y)
	}
}
