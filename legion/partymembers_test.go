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

// Fixer round 2 (review of fe0d3d6, minor 1-4). The bag-clear root cause: a trip is a bag-is-heavy need only when the
// sellable loot would bring the bag under the regen line; kept weight (cards, gear) is left for the store path.
func TestRegenTripOnlyWhenSellingClears(t *testing.T) {
	items[7001] = &Item{ID: 7001, Name: "Jellopy", Type: "Etc", Weight: 10, Buy: 0}
	items[7002] = &Item{ID: 7002, Name: "Kept Card", Type: "Card", Weight: 500}
	mk := func(weight int, inv map[int]*InvItem) *Bot {
		return &Bot{P: &Persona{BaseLevel: 50, Chars: []CharSpec{{Name: "regenclear"}}}, invSeen: true,
			MaxWeight: 20000, Weight: weight, MapName: "prontera", inv: inv,
			equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	}
	// 52%: 10,000 weight of junk to sell -> selling leaves 2%: a real trip
	junk := mk(10400, map[int]*InvItem{1: {Index: 1, ItemID: 7001, Count: 1000}})
	if !junk.regenSellClears() || junk.regenWeightNeed() != "bag is heavy" {
		t.Fatalf("junk that clears the line: clears %v, need %q", junk.regenSellClears(), junk.regenWeightNeed())
	}
	// 52% from kept cards with one junk piece: selling leaves the bag over the line, so no repeat trip (store path)
	kept := mk(10400, map[int]*InvItem{1: {Index: 1, ItemID: 7001, Count: 1}, 2: {Index: 2, ItemID: 7002, Count: 20}})
	// the trip still comes at once (owner: 50% triggers a run), but for the store: a plain sell trip would not clear it
	if kept.regenSellClears() {
		t.Fatal("kept cards: selling must not clear the bag")
	}
	if len(kept.storeCandidates()) > 0 && kept.regenWeightNeed() != "bag is heavy (store)" {
		t.Fatalf("kept weight: need %q, want the store trip", kept.regenWeightNeed())
	}
}

// Call site: the restock errand (ai.go idle errand -> idleRestockNeed) is for fewer than 3 real potions only.
func TestRestockErrandCallSite(t *testing.T) {
	tn := tune()
	mk := func(red int) *Bot {
		return &Bot{P: &Persona{BaseLevel: 50, Chars: []CharSpec{{Name: "restockerr"}}}, invSeen: true, Zeny: 5000,
			MaxWeight: 20000, Weight: 2000, MapName: "prontera", MaxHP: 1600, HP: 1600,
			inv:      map[int]*InvItem{3: {Index: 3, ItemID: itemRedPotion, Count: red}},
			equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	}
	b14 := mk(14)
	if idleRestockNeed(b14) {
		t.Fatal("14 potions: no restock errand")
	}
	if got := idleErrandWhy(tn, time.Hour, true, 10, 10, false, idleRestockNeed(b14), b14.Zeny); got != "" {
		t.Fatalf("14 potions started an errand: %q", got)
	}
	b2 := mk(2)
	if got := idleErrandWhy(tn, time.Hour, true, 10, 10, false, idleRestockNeed(b2), b2.Zeny); got != "restocking while waiting" {
		t.Fatalf("2 potions: errand %q", got)
	}
}

// Call site: the gate top-up (escortGate) buys to the survival minimum, only in a town with a potion seller.
func TestTopUpCallSite(t *testing.T) {
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "topupsite"}}}, invSeen: true, Zeny: 1000,
		MaxWeight: 20000, MaxHP: 3500, HP: 3500, MapName: "prontera",
		equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{},
		inv: map[int]*InvItem{3: {Index: 3, ItemID: itemRedPotion, Count: 10}}}
	want := potionShop("prontera") != nil
	if got := potTopUpNeed(b, "prontera"); got != want {
		t.Fatalf("10 potions below potMin %d in prontera: got %v, want %v (potion seller present %v)", b.potMin(), got, want, want)
	}
	b.inv[3].Count = 60
	if potTopUpNeed(b, "prontera") {
		t.Fatal("60 potions: no top-up")
	}
	b.inv[3].Count = 10
	b.Zeny = 100
	if potTopUpNeed(b, "prontera") {
		t.Fatal("too poor to top up")
	}
	if topUpWhy == "out of potions" || topUpWhy == "" {
		t.Fatal("the top-up trip needs its own reason (quiet line), not the emergency one")
	}
}

// Call site: the dungeon-side walk (escortGate -> floorHoldStep). A member alone on pay_dun00 with her on pay_dun03
// holds at once, then after floorHoldMove it moves one floor toward her or asks to be collected; it never leaves the
// dungeon from here, and the timer restarts on a new floor.
func TestFloorHoldWalkCallSite(t *testing.T) {
	if next, ok := nextFloorToward("pay_dun00", "pay_dun03"); !ok || next != "pay_dun01" {
		t.Fatalf("toward pay_dun03 from pay_dun00: %q %v", next, ok)
	}
	if next, ok := nextFloorToward("pay_dun03", "pay_dun01"); !ok || next != "pay_dun02" {
		t.Fatalf("toward pay_dun01 from pay_dun03: %q %v", next, ok)
	}
	if next, ok := nextFloorToward("in_sphinx1", "in_sphinx3"); !ok || next != "in_sphinx2" {
		t.Fatalf("unpadded floors: %q %v", next, ok)
	}
	if _, ok := nextFloorToward("payon", "pay_dun01"); ok {
		t.Fatal("a town is no floor")
	}
	now := time.Now()
	human := "FloorWalkHuman"
	humanLocs.Store(human, humanSeen{Map: "pay_dun03", At: now})
	defer humanLocs.Delete(human)
	b := &Bot{P: &Persona{BaseLevel: 60, Chars: []CharSpec{{Name: "floorwalk"}}}, MapName: "pay_dun00",
		MaxHP: 3500, HP: 3500, equipped: map[uint32]*InvItem{}, gearFailed: map[int]bool{}}
	p := &Party{Human: human, Members: []*Bot{b}}
	town, why := b.escortGate(p, "pay_dun03", now)
	if town != "pay_dun00" || !strings.HasPrefix(why, "waiting on this floor") {
		t.Fatalf("first minute: %q %q, want a hold on the floor", town, why)
	}
	later := now.Add(floorHoldMove + time.Second)
	town, why = b.escortGate(p, "pay_dun03", later)
	if isTown(town) || (town != "pay_dun00" && town != "pay_dun01") {
		t.Fatalf("after %v: %q %q - the member must stay on the dungeon floors", floorHoldMove, town, why)
	}
	// the collection call is at most once per floorHoldAsk: the escortGate call above may have used it already
	if strings.HasPrefix(why, "waiting on this floor") && floorHoldAskDue(b, later.Add(time.Minute)) {
		t.Fatal("collection call: a second one within floorHoldAsk")
	}
	if !floorHoldAskDue(b, later.Add(floorHoldAsk+time.Second)) {
		t.Fatal("collection call: due again after floorHoldAsk")
	}
	// a new floor restarts the timer
	b.MapName = "pay_dun01"
	if d := floorHoldFor(b, later); d != 0 {
		t.Fatalf("new floor: hold timer %v, want 0", d)
	}
	escortRestockClear(b)
}
