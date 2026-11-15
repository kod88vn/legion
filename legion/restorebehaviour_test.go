package main

import (
	"testing"
	"time"
)

// audit 2026-11-14: kin get only a short first refusal, urgent wants buy at once
func TestExchangeFirstRefusalWindow(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ExchangeTiersOn = true }) // defaults: 180 s ask stage, 40 s first refusal
	resetExch()
	defer resetExch()
	exch.Lock()
	exch.rel = map[string]exRel{"Buyer": {party: "P", guild: "g1"}, "Mate": {party: "P", guild: "g1"}, "Guildie": {guild: "g1"},
		"Solo": {}, "Other": {}}
	exch.Unlock()
	now := time.Now()
	buyer := &econSnap{name: "Buyer"}
	w := &Want{ItemID: 2301, Created: now.Add(-10 * time.Second), Why: "for my build"}
	if exchangeEligible(w, buyer, &econSnap{name: "Other"}, now) || !exchangeEligible(w, buyer, &econSnap{name: "Mate"}, now) {
		t.Error("10 s: party first refusal")
	}
	w.Created = now.Add(-25 * time.Second)
	if !exchangeEligible(w, buyer, &econSnap{name: "Guildie"}, now) || exchangeEligible(w, buyer, &econSnap{name: "Other"}, now) {
		t.Error("25 s: guild first refusal")
	}
	w.Created = now.Add(-45 * time.Second)
	if !exchangeEligible(w, buyer, &econSnap{name: "Other"}, now) {
		t.Error("45 s: a stranger may sell (was 360 s)")
	}
	// guild only: the whole 40 s window is the guild's
	w = &Want{ItemID: 2301, Created: now.Add(-30 * time.Second)}
	exch.Lock()
	exch.rel["Gbuyer"] = exRel{guild: "g1"}
	exch.Unlock()
	if exchangeEligible(w, &econSnap{name: "Gbuyer"}, &econSnap{name: "Other"}, now) {
		t.Error("guild-only buyer, 30 s: still the guild's refusal")
	}
	// urgent: potions in the field, arrows
	for _, u := range []*Want{{ItemID: itemRedPotion, Why: "potions", Created: now}, {ItemID: itemArrow, Created: now}} {
		if !exchangeEligible(u, buyer, &econSnap{name: "Other"}, now) {
			t.Errorf("urgent %d: anyone sells at once", u.ItemID)
		}
		if exchangeAsk(u, &econSnap{name: "Buyer"}, now) {
			t.Errorf("urgent %d: not held back for a party / guild ask", u.ItemID)
		}
	}
	// no party / guild: unchanged (#trade at once)
	if !exchangeEligible(&Want{Created: now}, &econSnap{name: "Solo"}, &econSnap{name: "Other"}, now) {
		t.Error("solo buyer: anyone")
	}
}

func TestExKinStage(t *testing.T) {
	if d := exKinStage(180*time.Second, 40*time.Second, true, true); d != 20*time.Second {
		t.Errorf("party+guild: %v", d)
	}
	if d := exKinStage(180*time.Second, 40*time.Second, false, true); d != 40*time.Second {
		t.Errorf("guild only: %v", d)
	}
	if d := exKinStage(10*time.Second, 40*time.Second, true, true); d != 10*time.Second {
		t.Errorf("capped by the ask stage: %v", d)
	}
}

// within a relationship tier the nearest seller wins; the tier still comes first
func TestExchangePreferNearest(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.ExchangeTiersOn = true })
	resetExch()
	defer resetExch()
	exch.Lock()
	exch.rel = map[string]exRel{"Buyer": {party: "P", guild: "g1"}, "NearG": {guild: "g1"}, "FarG": {guild: "g1"},
		"NearS": {}, "FarS": {}, "Mate": {party: "P", guild: "g1"}}
	exch.spare = map[string]map[int]bool{"FarG": {100: true}}
	exch.Unlock()
	buyer := &econSnap{name: "Buyer", mapN: "prontera", x: 150, y: 150}
	nearG := &econSnap{name: "NearG", mapN: "prontera", x: 152, y: 150}
	farG := &econSnap{name: "FarG", mapN: "prontera", x: 50, y: 50}
	if r := exchangePrefer(buyer, nearG, 100, farG, 100); r != 1 {
		t.Errorf("same tier: the nearer guild mate wins over a farther one with a spare, got %d", r)
	}
	if r := exchangePrefer(buyer, farG, 100, nearG, 100); r != -1 {
		t.Errorf("mirror: got %d", r)
	}
	nearS := &econSnap{name: "NearS", mapN: "prontera", x: 151, y: 150}
	farS := &econSnap{name: "FarS", mapN: "prontera", x: 20, y: 20}
	if r := exchangePrefer(buyer, nearS, 100, farS, 100); r != 1 {
		t.Errorf("strangers: nearest, got %d", r)
	}
	if r := exchangePrefer(buyer, farS, 100, nearG, 100); r != -1 {
		t.Errorf("tier first: a guild mate beats a stranger, got %d", r)
	}
	if r := exchangePrefer(buyer, &econSnap{name: "Mate", mapN: "gef_fild10"}, 100, nearG, 100); r != 1 {
		t.Errorf("tier first: party beats a nearer guild mate, got %d", r)
	}
	// equal distance: the spare breaks the tie
	sameA := &econSnap{name: "FarG", mapN: "prontera", x: 152, y: 150}
	if r := exchangePrefer(buyer, sameA, 100, nearG, 100); r != 1 {
		t.Errorf("tie: the spare wins, got %d", r)
	}
	// same-map beats another map
	if exNearCmp(buyer, nearS, &econSnap{mapN: "prt_fild08"}) != -1 {
		t.Error("same map is nearer")
	}
}

// GD02: the guild block owns the map choice only inside the duty window
func TestDutyWindowOK(t *testing.T) {
	now := time.Now()
	set := now.Add(-20 * time.Minute)
	k := &dutyBlock{Set: set, Target: 60, Done: 20, Until: now.Add(time.Hour)}
	if !dutyWindowOK(k, set, 20, 120, now) {
		t.Error("a running block owns the map choice")
	}
	if dutyWindowOK(k, set, 120, 120, now) {
		t.Error("today's duty done: window closed")
	}
	if dutyWindowOK(k, set.Add(time.Second), 20, 120, now) {
		t.Error("another block's goal")
	}
	if dutyWindowOK(nil, set, 0, 120, now) {
		t.Error("no block")
	}
	k2 := *k
	k2.Done = 60
	if dutyWindowOK(&k2, set, 20, 120, now) {
		t.Error("minutes done")
	}
	k3 := *k
	k3.Ended = now
	if dutyWindowOK(&k3, set, 20, 120, now) {
		t.Error("ended")
	}
	k4 := *k
	k4.Until = now.Add(-time.Minute)
	if dutyWindowOK(&k4, set, 20, 120, now) {
		t.Error("past its deadline")
	}
}

func TestDutyWindowRestoresOwnGoal(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.GuildDutyOn = true })
	now := time.Now()
	b := &Bot{P: &Persona{Name: "WinTest", Chars: []CharSpec{{Name: "WinTest"}}}}
	own := &farmGoal{Kind: "gear", Map: "pay_fild04", Maps: []string{"pay_fild04"}, Set: now.Add(-time.Hour), Until: now.Add(time.Hour)}
	g := &farmGoal{Kind: "guild", Map: "prt_fild08", Maps: []string{"prt_fild08"}, Set: now.Add(-3 * time.Hour), Until: now.Add(-time.Minute)}
	b.fgoal = g
	gduty.Lock()
	oldM := gduty.byChar[b.charName()]
	gduty.byChar[b.charName()] = &dutyMember{Char: b.charName(), Block: &dutyBlock{Set: g.Set, Target: 60, Until: g.Until, prev: own}}
	gduty.Unlock()
	defer func() {
		gduty.Lock()
		if oldM == nil {
			delete(gduty.byChar, b.charName())
		} else {
			gduty.byChar[b.charName()] = oldM
		}
		gduty.Unlock()
	}()
	if b.guildBlockHunt() {
		t.Fatal("an expired block must not override the map choice")
	}
	if b.fgoal != own {
		t.Errorf("own gear goal back, got %+v", b.fgoal)
	}
}

func TestDutyKickApplies(t *testing.T) {
	k := &dutyKicked{Char: "Bob", Account: "acc1", Stage: "cooldown"}
	if !dutyKickApplies(k, "acc1", 0) {
		t.Error("the kicked character itself")
	}
	if dutyKickApplies(k, "acc2", 0) {
		t.Error("another account with the same name was never kicked")
	}
	if dutyKickApplies(k, "acc1", 7) {
		t.Error("already in a guild: not decided here")
	}
	if dutyKickApplies(nil, "acc1", 0) || dutyKickApplies(&dutyKicked{Account: "acc1", Stage: "joined"}, "acc1", 0) {
		t.Error("no record / joined")
	}
}

// GD05: a leader's / treasurer's storage holds guild stock while guild storage is locked
func TestGuildStoreLedger(t *testing.T) {
	if guildStoreKeeper(false, true, true) || guildStoreKeeper(true, false, false) || !guildStoreKeeper(true, true, false) ||
		!guildStoreKeeper(true, false, true) {
		t.Error("guildStoreKeeper")
	}
	gstore.Lock()
	gstore.held = map[string]map[int]int{}
	gstore.Unlock()
	guildStoreAdd("Treasurer", map[int]int{itemRedPotion: 30})
	guildStoreAdd("Treasurer", map[int]int{itemRedPotion: 5})
	guildStoreAdd("", map[int]int{itemRedPotion: 5})
	v := guildStoreView([]string{"Treasurer", "Nobody"})
	if v["Treasurer"][itemName(itemRedPotion)] != 35 || len(v) != 1 {
		t.Errorf("view: %v", v)
	}
	gstore.Lock()
	gstore.held = map[string]map[int]int{}
	gstore.Unlock()
}
