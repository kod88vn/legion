package main

import (
	"strings"
	"testing"
	"time"
)

// QA 2026-11-15 (ko0): the rest regex matched "rest"/"sit" anywhere in a line. Only a whole rest clause is a rest.
func TestRestClauseOnlyRealRests(t *testing.T) {
	for _, line := range []string{"k, sit, we cover u", "np, rest up", "tell me if u need a break", "the rest of you go on", "sit tight"} {
		if humanRestClause(line) {
			t.Errorf("%q is not a rest line", line)
		}
	}
	for _, line := range []string{"need sp, resting", "resting", "rest", "sit down", "brb sp", "need a break", "sitting down.",
		"gotta rest a bit", "need to sit", "resting for a sec", "gotta sit down", "ok resting a bit", "got to rest now"} {
		if !humanRestClause(line) {
			t.Errorf("%q is a rest line", line)
		}
	}
	// through the party chat path: a false rest does not hold the friends
	p := &Party{Human: "AylaRestTest"}
	now := time.Now()
	humanRestHeard(p, "AylaRestTest : k, sit, we cover u", now)
	if humanResting("AylaRestTest", now) {
		t.Error("a false rest line held the party")
	}
	humanRestHeard(p, "AylaRestTest : need sp, resting", now)
	if !humanResting("AylaRestTest", now) {
		t.Error("a real rest line was not heard")
	}
}

// QA 2026-11-15 (qqx): the trip trigger and the escort hold share one restock test, and a restock hold times out.
func TestEscortRestockAgreesAndTimesOut(t *testing.T) {
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "escortrestock"}}}, invSeen: true, Zeny: 1000,
		MaxWeight: 20000, MapName: "payon", MaxHP: 3500, HP: 3500, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{},
		inv: map[int]*InvItem{3: {Index: 3, ItemID: itemRedPotion, Count: 15}}}
	// 15 potions: above the trip line (3), so no trip and no restock hold, whatever the potion minimum says
	if b.outOfPotions() || b.needTrip() == "out of potions" {
		t.Fatal("15 potions must neither start a trip nor hold a friend in town")
	}
	b.inv[3].Count = 2
	if !b.outOfPotions() || b.needTrip() != "out of potions" {
		t.Fatal("2 potions: the trip and the escort hold must both fire")
	}
	t0 := time.Now()
	if !escortRestockOK(b, t0) || !escortRestockOK(b, t0.Add(escortRestockMax-time.Second)) {
		t.Fatal("restock hold must hold within its time")
	}
	if escortRestockOK(b, t0.Add(escortRestockMax+time.Second)) {
		t.Fatal("restock hold must end after escortRestockMax")
	}
	escortRestockClear(b)
	if !escortRestockOK(b, t0.Add(time.Hour)) {
		t.Fatal("a cleared hold starts afresh")
	}
	escortRestockClear(b)
}

// QA 2026-11-15 (triage P7): an idle "restocking while waiting" errand needs a real potion need, not SP or 14 potions;
// a heavy-bag trip is not repeated within heavyTripGap.
func TestIdleRestockAndHeavyRepeat(t *testing.T) {
	tn := tune()
	if got := idleErrandWhy(tn, time.Hour, true, 10, 10, false, false, 5000); got != "" {
		t.Errorf("14 potions must not start a restock errand: %q", got)
	}
	if got := idleErrandWhy(tn, time.Hour, true, 10, 10, false, true, 5000); got != "restocking while waiting" {
		t.Errorf("a real potion need starts the errand: %q", got)
	}
	if heavyTripGap < 5*time.Minute {
		t.Fatal("heavyTripGap must block the back-to-back trips seen in QA")
	}
}

// QA 2026-11-15 (h90): a member does not cross dungeon floors alone; floors are crossed with a friend on the floor.
func TestDungeonFloorNeedsFriend(t *testing.T) {
	if dungeonFamily("pay_dun03") != "pay_dun" || dungeonFamily("payon") != "" {
		t.Fatalf("dungeonFamily: %q / %q", dungeonFamily("pay_dun03"), dungeonFamily("payon"))
	}
	now := time.Now()
	human := "EscortFloorHuman"
	humanLocs.Store(human, humanSeen{Map: "pay_dun03", At: now})
	defer humanLocs.Delete(human)
	m := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "escortfloor"}}}, MapName: "pay_dun00"}
	p := &Party{Human: human, Members: []*Bot{m}}
	if friendOnFloor(p, m, "pay_dun00", now) {
		t.Fatal("alone on pay_dun00: no friend on the floor")
	}
	if !friendOnFloor(p, m, "pay_dun03", now) {
		t.Fatal("the leader is on pay_dun03")
	}
	// the leader's position on the member's floor, recently seen, counts as together
	humanLocs.Store(human, humanSeen{Map: "pay_dun00", At: now})
	if !friendOnFloor(p, m, "pay_dun00", now) {
		t.Fatal("the leader on the same floor is together")
	}
	if friendOnFloor(p, m, "pay_dun00", now.Add(3*time.Minute)) {
		t.Fatal("an old sighting of the leader does not count")
	}
}

// Review of 133adda (Major 1, 2): escortGate itself. A member alone on a dungeon floor holds there while the leader is
// elsewhere; a portal to the next floor (one floor apart) is followed; a member deeper than her holds too.
func TestEscortGateFloorsDriven(t *testing.T) {
	now := time.Now()
	human := "EscortDriveHuman"
	defer humanLocs.Delete(human)
	mk := func(name, floor string) (*Bot, *Party) {
		b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: name}}}, MapName: floor,
			MaxHP: 3500, HP: 3500, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
		return b, &Party{Human: human, Members: []*Bot{b}}
	}
	cases := []struct {
		name, member, leader string
		hold                 bool // the member must hold on its own floor
	}{
		{"portal next floor", "pay_dun00", "pay_dun01", false},
		{"she is far below", "pay_dun00", "pay_dun03", true},
		{"deeper than her, two floors", "pay_dun03", "pay_dun01", true},
		{"deeper than her, one floor", "pay_dun02", "pay_dun01", false},
	}
	for i, c := range cases {
		b, p := mk("escortdrive"+string(rune('a'+i)), c.member)
		humanLocs.Store(human, humanSeen{Map: c.leader, At: now})
		town, why := b.escortGate(p, c.leader, now)
		held := town == c.member && strings.HasPrefix(why, "waiting on this floor")
		if held != c.hold {
			t.Errorf("%s: escortGate(%s -> %s) = %q %q, hold %v", c.name, c.member, c.leader, town, why, c.hold)
		}
		if !c.hold && (town != "" || why != "") {
			t.Errorf("%s: a portal lag must not send the member anywhere: %q %q", c.name, town, why)
		}
	}
	// hurt alone on a floor: never the hold (it goes to town to heal)
	b, p := mk("escortdrivehurt", "pay_dun00")
	b.HP = 500
	humanLocs.Store(human, humanSeen{Map: "pay_dun03", At: now})
	if _, why := b.escortGate(p, "pay_dun03", now); strings.HasPrefix(why, "waiting on this floor") {
		t.Error("a hurt member alone on a floor must not hold there")
	}
}
