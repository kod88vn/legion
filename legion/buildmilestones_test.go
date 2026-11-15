package main

import (
	"sync/atomic"
	"testing"
)

// EC39 test kit: a Swordman build with three weapon and three armor steps, one per band (MinLv 1, 21, 41).
var msItems = map[int]*Item{
	9101: {ID: 9101, Name: "Sword I", Type: "Weapon", Loc: 0x2, Atk: 43, WLv: 1, MinLv: 1},
	9102: {ID: 9102, Name: "Sword II", Type: "Weapon", Loc: 0x2, Atk: 80, WLv: 1, MinLv: 21},
	9103: {ID: 9103, Name: "Sword III", Type: "Weapon", Loc: 0x2, Atk: 120, WLv: 1, MinLv: 41},
	9201: {ID: 9201, Name: "Armor I", Type: "Armor", Loc: 0x10, Def: 20, MinLv: 1},
	9202: {ID: 9202, Name: "Armor II", Type: "Armor", Loc: 0x10, Def: 37, MinLv: 21},
	9203: {ID: 9203, Name: "Armor III", Type: "Armor", Loc: 0x10, Def: 55, MinLv: 41},
}

func msBuild() *Build {
	return &Build{ID: "test_ec39_sword", Class: 7, First: 1, Weapons: []int{9101, 9102, 9103}, Armors: []int{9201, 9202, 9203}}
}

func withMsItems(t *testing.T) {
	saved := map[int]*Item{}
	for id, x := range msItems {
		saved[id] = items[id]
		items[id] = x
	}
	t.Cleanup(func() {
		for id, x := range saved {
			if x == nil {
				delete(items, id)
			} else {
				items[id] = x
			}
		}
	})
}

// msBot: a lv60 Swordman wearing the given weapon and body armor.
func msBot(weapon, armor int, cards ...int) *Bot {
	return &Bot{P: &Persona{Name: "ec39bot", Risk: 0.5, BaseLevel: 60, Chars: []CharSpec{{Name: "ec39bot"}}}, BaseLv: 60, Class: 1, invSeen: true,
		equipped: gsEquip(gsPiece{weapon, 0x2, 0, cards}, gsPiece{armor, 0x10, 0, nil})}
}

func TestMilestonesReadTheRoutes(t *testing.T) {
	withMsItems(t)
	ms := milestonesFor(msBuild(), nil)
	if len(ms) != len(bandEdges) {
		t.Fatalf("%d milestone bands, want %d", len(ms), len(bandEdges))
	}
	want := map[int][2]int{1: {9101, 9201}, 21: {9102, 9202}, 41: {9103, 9203}}
	for _, m := range ms {
		if w, ok := want[m.Min]; ok && (m.WeaponItem != w[0] || m.ArmorItem != w[1]) {
			t.Errorf("band %d-%d: weapon %d armor %d, want %d and %d", m.Min, m.Max, m.WeaponItem, m.ArmorItem, w[0], w[1])
		}
	}
	if ms[2].Cards != 1 || ms[0].Cards != 0 {
		t.Errorf("cards per band: 1-20 %d, 41-60 %d; want 0 and 1", ms[0].Cards, ms[2].Cards)
	}
}

// a lv60 bot with lv20-level gear (Sword I, Armor I) gets a ceiling of 20 and its next gear step as the goal
func TestLowGearLv60GetsSafeMapAndGearGoal(t *testing.T) {
	withMsItems(t)
	b := msBot(9101, 9201)
	p := msBuild()
	raw := gearEffectiveLv(famSword, 60, rateGear(famSword, 60, b.equipped))
	gate, unmet := gearGateOf(b, p, 60)
	if gate != 20 {
		t.Fatalf("gate = %d, want 20 (band 21-40 unmet)", gate)
	}
	if unmet == nil || unmet.Min != 21 || unmet.WeaponItem != 9102 {
		t.Fatalf("unmet milestone = %+v, want band 21-40 with the goal Sword II (9102)", unmet)
	}
	if got := gearGatedLv(b, p, 60, raw); got > 20 {
		t.Fatalf("gated effective level %d (raw %d), want <= 20", got, raw)
	}
	t.Logf("lv60 with Sword I + Armor I: raw rating %d, gated %d, goal %d", raw, gearGatedLv(b, p, 60, raw), unmet.WeaponItem)
}

// the bot never idles: a gated level still has hunting spots (the solo fallback takes one)
func TestGearGatedBotNeverIdles(t *testing.T) {
	swapFairTables(t)
	fairWorld()
	huntSpots = append(huntSpots, HuntSpot{Map: "pay_fild07", MinLv: 10, MaxLv: 30, Mobs: 41}) // reaches a lv20 ceiling
	mobs[1004] = &Mob{ID: 1004, Level: 12}
	spawnsByMap["pay_fild07"] = append(spawnsByMap["pay_fild07"], Spawn{"pay_fild07", 1004, 40})
	withMsItems(t)
	b := msBot(9101, 9201)
	spots := b.spotsFor(20)
	if len(spots) == 0 {
		t.Fatal("a gated lv60 bot got no hunting spot at its ceiling")
	}
}

// once the gear meets a band's milestone the ceiling moves up, and at the full set the rating is not capped
func TestGearMilestoneLiftsTheCeiling(t *testing.T) {
	withMsItems(t)
	p := msBuild()
	step := func(name string, weapon, armor, wantGate int, wantUnmet bool) {
		b := msBot(weapon, armor)
		raw := gearEffectiveLv(famSword, 60, rateGear(famSword, 60, b.equipped))
		gate, unmet := gearGateOf(b, p, 60)
		if gate != wantGate || (unmet != nil) != wantUnmet {
			t.Fatalf("%s: gate %d unmet %v, want gate %d unmet %v", name, gate, unmet, wantGate, wantUnmet)
		}
		got := gearGatedLv(b, p, 60, raw)
		if got > gate {
			t.Fatalf("%s: effective %d over the gate %d", name, got, gate)
		}
		t.Logf("%s: raw %d gate %d effective %d", name, raw, gate, got)
	}
	step("Sword I + Armor I", 9101, 9201, 20, true)
	step("Sword II + Armor II", 9102, 9202, 40, true)
	// the 41-60 band also asks for one card in the worn gear
	b := msBot(9103, 9203, 4006)
	raw3 := gearEffectiveLv(famSword, 60, rateGear(famSword, 60, b.equipped))
	if g, u := gearGateOf(b, p, 60); g != 60 || u != nil {
		t.Fatalf("Sword III + Armor III + card: gate %d unmet %v, want 60 and none", g, u)
	}
	if got := gearGatedLv(b, p, 60, raw3); got <= 40 {
		t.Fatalf("carded full milestone gear rated %d (raw %d): should move up past lv40", got, raw3)
	}
	full := msBot(9103, 9203, 4006)
	raw := gearEffectiveLv(famSword, 60, rateGear(famSword, 60, full.equipped))
	if got := gearGatedLv(full, p, 60, raw); got <= 40 {
		t.Fatalf("full milestone gear rated %d (raw %d): should move up past lv40", got, raw)
	}
}

// a party's map follows its weakest-geared member: a gated member caps the party's minimum level
func TestPartyFollowsWeakestGear(t *testing.T) {
	withMsItems(t)
	strong := &Bot{P: &Persona{Name: "ec39strong", Risk: 0.5}, BaseLv: 75}
	weak := &Bot{P: &Persona{Name: "ec39weak", Risk: 0.5}, BaseLv: 60}
	for _, c := range []struct {
		b *Bot
		s pubState
	}{
		{strong, pubState{Name: "ec39strong", Online: true, Known: true, Lv: 75, Power: 75}},
		{weak, pubState{Name: "ec39weak", Online: true, Known: true, Lv: 60, Power: 20, Gate: 20}},
	} {
		v := &atomic.Value{}
		v.Store(c.s)
		pubs.Store(c.b.P.Name, v)
		t.Cleanup(func() { pubs.Delete(c.b.P.Name) })
	}
	minLv, _, known, _ := partyFitLv([]*Bot{strong, weak})
	if known != 2 {
		t.Fatalf("known %d, want 2", known)
	}
	if minLv > 20 {
		t.Fatalf("party minimum level %d, want <= the weak member's gear ceiling 20", minLv)
	}
}

// tuning gear_gate_on = false: no ceiling at all (the old rating)
func TestGearGateTuningOff(t *testing.T) {
	withMsItems(t)
	swapFairTables(t)
	off := *tune()
	off.GearGateOn = false
	curTuning.Store(&off)
	b := msBot(9101, 9201)
	if g, _ := gearGateOf(b, msBuild(), 60); g != 0 {
		t.Fatalf("flag off: gate %d, want 0", g)
	}
}
