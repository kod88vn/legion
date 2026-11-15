package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ovHookParty: an engine party (or a human-less bot party when engine is false) led by "AylaOwn" with one member, on
// good_map. Cleaned up after the test.
func ovHookParty(t *testing.T, name string, engine bool) (*Party, *Bot, *Bot) {
	t.Helper()
	leader := engineMk(41, "AylaOwn")
	mem := engineMk(42, "AylaMate")
	p := &Party{Name: name, Leader: leader, Members: []*Bot{leader, mem}, Map: "good_map", Engine: engine, Formed: time.Now()}
	leader.MapName = "good_map"
	leader.Goal = Goal{Mode: "hunt", Map: "good_map", Set: "party-leader"}
	mem.MapName = "good_map"
	parties.Lock()
	parties.byName[name] = p
	parties.ofBot[leader] = p
	parties.ofBot[mem] = p
	parties.Unlock()
	engineClean(t, p)
	return p, leader, mem
}

// ovHookStore: a fresh override store for the test (aylaDirective and the API read it).
func ovHookStore(t *testing.T) *aylaOvStore {
	t.Helper()
	s, err := loadAylaOverrideStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ovUse(t, s)
	return s
}

// TestOvFarmMapWinsAndSurvivesRelocate: an owner farm map is where the engine party goes, a relocate to any other map
// (fit check, roam give-up) does not move it off, and the party stays there while the override holds.
func TestOvFarmMapWinsAndSurvivesRelocate(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	ovHookStore(t)
	p, _, _ := ovHookParty(t, "ovhook-map", true)
	now := time.Now()
	if _, err := aylaOv().Set(ovFarmMap, "low_map", "test", 2, 0, 0, true, now); err != nil {
		t.Fatal(err)
	}
	p.relocate("good_map", -1, -1, "too rough here for some of us")
	if p.Map != "low_map" {
		t.Fatalf("owner map not taken: party on %s", p.Map)
	}
	p.relocate("low_map", -1, -1, "")
	p.relocate("low_map", -1, -1, "too easy for us")
	if p.Map != "low_map" {
		t.Fatalf("owner map lost on relocate: party on %s", p.Map)
	}
	if m, ok := ovPartyMap(p, time.Now()); !ok || m != "low_map" {
		t.Fatalf("ovPartyMap %q %v", m, ok)
	}
}

// TestOvExpiryReturnsControl: an expired or cleared farm map hands control back to the puppet master, then the default.
func TestOvExpiryReturnsControl(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	p, _, _ := ovHookParty(t, "ovhook-expiry", true)
	now := time.Now()
	// cleared: control returns at once
	o, err := s.Set(ovFarmMap, "low_map", "test", 1, 0, 0, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if d := aylaDirective(ovFarmMap, now, "pup_map", "def_map"); d.Source != "owner" {
		t.Fatalf("in force: %+v", d)
	}
	if _, ok := ovPartyMap(p, now); !ok {
		t.Fatal("engine party does not see the owner map")
	}
	if _, err := s.Clear(o.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := ovPartyMap(p, now); ok {
		t.Fatal("cleared override still in force for the party")
	}
	if d := aylaDirective(ovFarmMap, now, "pup_map", "def_map"); d.Source != "puppet" || d.Value != "pup_map" {
		t.Fatalf("after clear: %+v", d)
	}
	// expired: an hour-long override is up two hours on, and the puppet master's directive is back
	if _, err := s.Set(ovFarmMap, "low_map", "test", 1, 0, 0, true, now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Hour)
	if d := aylaDirective(ovFarmMap, later, "pup_map", "def_map"); d.Source != "puppet" || d.Value != "pup_map" {
		t.Fatalf("after expiry: %+v", d)
	}
	if _, ok := ovPartyMap(p, later); ok {
		t.Fatal("expired override still in force for the party")
	}
}

// TestOvDeathLoopKeepsMap: two deaths on the owner's map keep the map and log it, and the party does not move.
func TestOvDeathLoopKeepsMap(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	ovHookStore(t)
	p, l, _ := ovHookParty(t, "ovhook-death", true)
	now := time.Now()
	if _, err := aylaOv().Set(ovFarmMap, "low_map", "test", 2, 0, 0, true, now); err != nil {
		t.Fatal(err)
	}
	p.relocate("good_map", -1, -1, "")
	if p.Map != "low_map" {
		t.Fatalf("setup: party on %s", p.Map)
	}
	_, before := events.since(0)
	l.deathLog = []mapVisit{{m: "low_map", at: now}, {m: "low_map", at: now}}
	l.deathLoopCheck(now)
	if p.Map != "low_map" {
		t.Fatalf("death loop moved the owner's party to %s", p.Map)
	}
	evs, _ := events.since(before)
	kept := false
	for _, e := range evs {
		if strings.Contains(e.Text, "override kept, party died") {
			kept = true
		}
	}
	if !kept {
		t.Fatal("no 'override kept, party died' line")
	}
}

// TestOvGuildDirOutranksPuppet: the owner's guild direction for Ayla's guild comes first, the puppet one after; with
// engine_parties empty nothing changes.
func TestOvGuildDirOutranksPuppet(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	now := time.Now()
	gduty.Lock()
	save := gduty.dirs[humanGuildID]
	defer func() { gduty.Lock(); gduty.dirs[humanGuildID] = save; gduty.Unlock() }()
	gduty.dirs[humanGuildID] = []*GuildDirection{{ID: "P1", Guild: humanGuildID, Kind: dirLevel, Map: "good_map", SetBy: "leader", Expires: now.Add(time.Hour)}}
	act := activeDirsLocked(humanGuildID, now)
	gduty.Unlock()
	if len(act) == 0 || act[0].ID != "P1" {
		t.Fatalf("setup: %+v", act)
	}
	if _, err := s.Set(ovGuildDir, "level@low_map", "test", 1, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	gduty.Lock()
	act = activeDirsLocked(humanGuildID, now)
	gduty.Unlock()
	if len(act) < 2 || act[0].SetBy != "owner" || act[0].Map != "low_map" || act[1].ID != "P1" {
		t.Fatalf("owner direction does not outrank the puppet one: %+v", act)
	}
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "" })
	gduty.Lock()
	act = activeDirsLocked(humanGuildID, now)
	gduty.dirs[humanGuildID] = save
	gduty.Unlock()
	if len(act) == 0 || act[0].SetBy == "owner" {
		t.Fatalf("engine_parties empty still takes the owner direction: %+v", act)
	}
}

// TestOvOrdersFireOnce: town_run and party_order fire once per engine party; a second round does nothing.
func TestOvOrdersFireOnce(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	p, _, mem := ovHookParty(t, "ovhook-order", true)
	now := time.Now()
	// the member is on another map, so the leader's party order calls it
	v := &atomic.Value{}
	v.Store(pubState{Name: mem.P.Name, Map: "low_map", Online: true, Known: true})
	pubs.Store(mem.P.Name, v)
	t.Cleanup(func() { pubs.Delete(mem.P.Name) })

	town, err := s.Set(ovTownRun, "now", "test", 1, 0, 0, false, now)
	if err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now)
	if !ovDone(p, ovTownRun+"|"+town.ID) || p.TownRunTown != nearestTown("good_map") {
		t.Fatalf("town run not fired: done=%v town=%q", ovDone(p, ovTownRun+"|"+town.ID), p.TownRunTown)
	}
	parties.Lock()
	p.TownRunAt, p.TownRunTown = time.Time{}, ""
	parties.Unlock()
	ovFireOrders(now)
	if p.TownRunTown != "" {
		t.Fatalf("town run fired twice: %q", p.TownRunTown)
	}

	po, err := s.Set(ovPartyOrder, "follow_me", "test", 1, 0, 0, false, now)
	if err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now)
	if !ovDone(p, ovPartyOrder+"|"+po.ID) {
		t.Fatal("party order not fired")
	}
	// fired: a second round does not call again (the leader's per-round state is not touched either)
	ovFireOrders(now)
	if len(p.OvDone) != 2 {
		t.Fatalf("OvDone %v", p.OvDone)
	}
}

// TestOvOffForOtherParties: an ordinary (human-led or non-engine) party never takes an owner override, and with
// engine_parties empty the engine party is unchanged.
func TestOvOffForOtherParties(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	other, _, _ := ovHookParty(t, "ovhook-other", false)
	eng, _, _ := ovHookParty(t, "ovhook-eng", true)
	now := time.Now()
	if _, err := s.Set(ovFarmMap, "low_map", "test", 1, 0, 0, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ovTownRun, "now", "test", 1, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := ovPartyMap(other, now); ok {
		t.Fatal("non-engine party took the owner farm map")
	}
	ovFireOrders(now)
	if other.OvDone != nil || other.TownRunTown != "" {
		t.Fatalf("non-engine party took an owner order: %v %q", other.OvDone, other.TownRunTown)
	}
	if _, ok := ovPartyMap(eng, now); !ok {
		t.Fatal("engine party did not take the owner farm map")
	}
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "" })
	if _, ok := ovPartyMap(eng, now); ok {
		t.Fatal("engine_parties empty: owner farm map still applies")
	}
	if od := ovGuildDirFor(humanGuildID, now); od != nil {
		t.Fatal("engine_parties empty: owner guild direction applies")
	}
}

// TestOvTownRunNowOnly: town_run "now" runs the party's town; any other value is refused for good, once.
func TestOvTownRunNowOnly(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	p, _, _ := ovHookParty(t, "ovhook-townnow", true)
	now := time.Now()
	if _, err := s.Set(ovTownRun, "prontera", "test", 1, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now)
	if p.TownRunTown != "" {
		t.Fatalf("town_run other than now ran: %q", p.TownRunTown)
	}
}

// TestOvRallyTownAndCancel: a rally to a listed town starts at its Kafra; "cancel" ends the running rally; an unlisted
// value is refused.
func TestOvRallyTownAndCancel(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn"; tn.RallyEnabled = true })
	s := ovHookStore(t)
	p, _, _ := ovHookParty(t, "ovhook-rally", true)
	now := time.Now()
	if _, err := s.Set(ovRally, "prontera", "test", 1, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now)
	if p.Rally.Town != "prontera" {
		t.Fatalf("rally to prontera not started: town=%q", p.Rally.Town)
	}
	if _, err := s.Set(ovRally, "cancel", "test", 1, 0, 0, false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now.Add(time.Second))
	if p.Rally.Town != "" {
		t.Fatalf("rally not cancelled: town=%q", p.Rally.Town)
	}
	if _, err := s.Set(ovRally, "lutie", "test", 1, 0, 0, false, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	ovFireOrders(now.Add(2 * time.Second))
	if p.Rally.Town != "" {
		t.Fatalf("rally to lutie (no Kafra in the engine) started: %q", p.Rally.Town)
	}
}

// TestOvPartyOrderFollowMeOnly: party_order "follow_me" calls the members to the leader; other values are refused.
func TestOvPartyOrderFollowMeOnly(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	p, _, mem := ovHookParty(t, "ovhook-follow", true)
	v := &atomic.Value{}
	v.Store(pubState{Name: mem.P.Name, Map: "low_map", Online: true, Known: true})
	pubs.Store(mem.P.Name, v)
	t.Cleanup(func() { pubs.Delete(mem.P.Name) })
	now := time.Now()
	if !ovFireOne(p, ovPartyOrder, "come", now) {
		t.Fatal("other party_order value should be refused for good (done), not retried")
	}
	if !ovFireOne(p, ovPartyOrder, "follow_me", now) {
		t.Fatal("follow_me not carried out by the leader hunting on its map")
	}
}

// TestOvGuildFreeTextDirection: the owner's guild direction is free text: a level direction carrying the owner's words,
// ahead of the puppet master's direction.
func TestOvGuildFreeTextDirection(t *testing.T) {
	ovWorld(t)
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	s := ovHookStore(t)
	now := time.Now()
	text := "keep the stock topped up, mind the gear"
	if _, err := s.Set(ovGuildDir, text, "test", 1, 0, 0, false, now); err != nil {
		t.Fatal(err)
	}
	gduty.Lock()
	act := activeDirsLocked(humanGuildID, now)
	gduty.Unlock()
	if len(act) == 0 || act[0].SetBy != "owner" || act[0].Kind != dirLevel || act[0].Note != text {
		t.Fatalf("free-text owner direction: %+v", act)
	}
}
