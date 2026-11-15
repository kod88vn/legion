package main

import (
	"testing"
	"time"
)

// resetLfgStats: what a process start looks like (zero counters), for the restart round trip.
func resetLfgStats() {
	lfgStats.Lock()
	defer lfgStats.Unlock()
	lfgStats.counted = time.Now()
	lfgStats.set, lfgStats.met, lfgStats.expired, lfgStats.changed = map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	lfgStats.posts, lfgStats.dissolved, lfgStats.leaves = map[string]int{}, map[string]int{}, map[string]int{}
	lfgStats.join = [nJoin][nBands]int{}
	lfgStats.gate = [nGate][nBands]int{}
	lfgStats.party = [nParty]int{}
	lfgStats.inParty, lfgStats.leftCD = 0, 0
	lfgStats.joins, lfgStats.formed, lfgStats.local = 0, 0, 0
}

// TestLfgStatsSurviveRestart: the counters saved before a restart are back after it, and a count after the restart
// adds to the saved one (the measurement must not reset at each deploy).
func TestLfgStatsSurviveRestart(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	resetLfgStats()
	lfgCount(lfgStats.dissolved, "3h cap")
	lfgCount(lfgStats.dissolved, "3h cap")
	lfgCount(lfgStats.leaves, "member left")
	tl := &lfgTally{}
	tl.addJoin(jMap, 30)
	tl.addJoin(jMap, 35)
	tl.addGate(gDead, 90)
	tl.party[pTown] = 2
	tl.inParty = 7
	tl.flush()
	lfgStats.Lock()
	lfgStats.joins, lfgStats.formed, lfgStats.local = 5, 2, 1
	since := lfgStats.counted
	lfgStats.Unlock()
	lfgStatsSave(time.Now())

	resetLfgStats() // restart: counters are zero again
	lfgStatsLoad()
	lfgStats.Lock()
	if lfgStats.dissolved["3h cap"] != 2 || lfgStats.leaves["member left"] != 1 || lfgStats.joins != 5 ||
		lfgStats.formed != 2 || lfgStats.local != 1 || lfgStats.join[jMap][1] != 2 || lfgStats.gate[gDead][4] != 1 ||
		lfgStats.party[pTown] != 2 || lfgStats.inParty != 7 {
		t.Fatalf("counters after restart: dissolved=%v leaves=%v joins=%d formed=%d local=%d join=%v gate=%v party=%v",
			lfgStats.dissolved, lfgStats.leaves, lfgStats.joins, lfgStats.formed, lfgStats.local, lfgStats.join[jMap], lfgStats.gate[gDead], lfgStats.party)
	}
	if !lfgStats.counted.Equal(since) {
		t.Fatalf("counted_since lost: got %v want %v", lfgStats.counted, since)
	}
	lfgStats.Unlock()

	lfgCount(lfgStats.dissolved, "3h cap") // a count after the restart adds to the saved one
	lfgStatsSave(time.Now())
	resetLfgStats()
	lfgStatsLoad()
	lfgStats.Lock()
	defer lfgStats.Unlock()
	if lfgStats.dissolved["3h cap"] != 3 {
		t.Fatalf("second restart: dissolved 3h cap = %d, want 3", lfgStats.dissolved["3h cap"])
	}
}

// TestLfgStatsMissingFile: no file yet (first start) leaves the counters at zero and does not fail.
func TestLfgStatsMissingFile(t *testing.T) {
	old := *dataDir
	*dataDir = t.TempDir()
	defer func() { *dataDir = old }()
	resetLfgStats()
	lfgStatsLoad()
	lfgStats.Lock()
	defer lfgStats.Unlock()
	if lfgStats.joins != 0 || len(lfgStats.dissolved) != 0 || lfgStats.join[jMap][0] != 0 {
		t.Fatalf("counters changed without a file: joins=%d dissolved=%v", lfgStats.joins, lfgStats.dissolved)
	}
}

// TestLfgFitWhyNames: each failed check gives its own reason, in the lfgFits order; a fit gives -1.
func TestLfgFitWhyNames(t *testing.T) {
	// the level check is PT35's (exp-top, spread) and needs map data; stub it for the other checks, keep it for "spread"
	real := lfgLvFit
	defer func() { lfgLvFit = real }()
	lfgLvFit = func(c *lfgCand, P *lfgParty) bool { return true }
	P := lfgTestParty("prt_fild08", 30, map[int]int{roleMelee: 1}, true)
	ok := &lfgCand{lv: 31, role: roleMelee, g: &farmGoal{Kind: "level"}, at: "prt_fild08"}
	if why := lfgFitWhy(ok, P); why != -1 {
		t.Fatalf("a fitting candidate got reason %d", why)
	}
	lfgLvFit = real
	band := &lfgCand{lv: 80, role: roleMelee, g: &farmGoal{Kind: "level"}, at: "prt_fild08"}
	if why := lfgFitWhy(band, P); why != jSpread {
		t.Errorf("lv80 into lv30 party: reason %d, want spread", why)
	}
	lfgLvFit = func(c *lfgCand, P *lfgParty) bool { return true }
	none := &lfgCand{lv: 31, role: roleNone, g: &farmGoal{Kind: "level"}, at: "prt_fild08"}
	if why := lfgFitWhy(none, P); why != jRole {
		t.Errorf("role none: reason %d, want role", why)
	}
	gear := &lfgCand{lv: 31, role: roleMelee, g: &farmGoal{Kind: "gear", Maps: []string{"pay_fild04"}, Map: "pay_fild04"}, at: "prt_fild08"}
	if why := lfgFitWhy(gear, P); why != jServes && why != jExpPoor {
		t.Errorf("gear goal off its map: reason %d, want serves (or exp_poor on this map)", why)
	}
}

// TestLfgTallyReport: checks land in their reason and band cell, and the API report names them "reason band".
func TestLfgTallyReport(t *testing.T) {
	resetLfgStats()
	tl := &lfgTally{}
	tl.addJoin(jSpread, 35)
	tl.addJoin(jSpread, 35)
	tl.addJoin(jMap, 90)
	tl.addGate(gSocial, 35)
	tl.flush()
	lfgStats.Lock()
	defer lfgStats.Unlock()
	rep := lfgCountsReport()
	jc := rep["join_checks"].(map[string]int)
	if jc["spread lv21-40"] != 2 || jc["map lv81+"] != 1 || len(jc) != 2 {
		t.Fatalf("join_checks = %v", jc)
	}
	if gc := rep["gate_checks"].(map[string]int); gc["social lv21-40"] != 1 || len(gc) != 1 {
		t.Fatalf("gate_checks = %v", gc)
	}
}

// TestLfgGaugeSaved: the gauge is the last tick's population and is reported; it is not persisted.
func TestLfgGaugeSaved(t *testing.T) {
	resetLfgStats()
	lfgStats.Lock()
	lfgStats.gauge = lfgGauge{At: time.Now(), Online: 900, InParty: 300, Free: 600, Candidates: 120, OpenParties: 40}
	g := lfgCountsReport()["gauge"].(map[string]interface{})
	lfgStats.Unlock()
	if g["online"] != 900 || g["open_parties"] != 40 {
		t.Fatalf("gauge = %v", g)
	}
}

// TestPartyEndClearsGoal: when a party ends, a member's party goals are cleared so it picks its own ground again,
// while its own solo goal is kept. A goal left as "party-restore" or "help" used to stay forever.
func TestPartyEndClearsGoal(t *testing.T) {
	for _, set := range []string{"party", "party-leader", "party-leader-hold", "party-restore", "help"} {
		if !partyGoalEnds(Goal{Mode: "hunt", Map: "pay_fild04", Set: set}) {
			t.Errorf("goal set %q should end with the party", set)
		}
	}
	for _, set := range []string{"auto", "auto (nowhere reachable)", "market", "api"} {
		if partyGoalEnds(Goal{Mode: "hunt", Map: "pay_fild04", Set: set}) {
			t.Errorf("goal set %q must survive the party ending", set)
		}
	}
}

// TestDisbandClearsPartyGoal runs the real disband(): a member left with "party-restore" picks again, a member
// with its own "auto" goal keeps it. Reverting the disband() hunk fails this test (the goal stays "party-restore").
func TestDisbandClearsPartyGoal(t *testing.T) {
	stale := &Bot{P: &Persona{Name: "zzdis_stale", Chars: []CharSpec{{Name: "zzdis_stale"}}}, Goal: Goal{Mode: "hunt", Map: "pay_fild04", Set: "party-restore"}}
	solo := &Bot{P: &Persona{Name: "zzdis_solo", Chars: []CharSpec{{Name: "zzdis_solo"}}}, Goal: Goal{Mode: "hunt", Map: "prt_fild08", Set: "auto"}}
	p := &Party{Name: "zzdis", Leader: stale, Members: []*Bot{stale, solo}}
	parties.Lock()
	parties.byName[p.Name] = p
	parties.ofBot[stale], parties.ofBot[solo] = p, p
	parties.Unlock()
	disband(p)
	// disband resets the goals in its own goroutine (after the leave packet, which has no connection here)
	deadline := time.Now().Add(3 * time.Second)
	for {
		stale.mu.Lock()
		done := stale.Goal.Set == ""
		stale.mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stale.mu.Lock()
	if stale.Goal.Set != "" {
		t.Errorf("member with party-restore goal kept %+v after the party ended", stale.Goal)
	}
	stale.mu.Unlock()
	solo.mu.Lock()
	if solo.Goal.Set != "auto" {
		t.Errorf("member with its own auto goal lost it: %+v", solo.Goal)
	}
	solo.mu.Unlock()
}

// TestLeaveGamePartyClearsPartyGoal: a bot that leaves its in-game party (keeper path) drops the party's goal too.
func TestLeaveGamePartyClearsPartyGoal(t *testing.T) {
	b := &Bot{P: &Persona{Name: "zzleave", Chars: []CharSpec{{Name: "zzleave"}}}, Goal: Goal{Mode: "hunt", Map: "pay_fild04", Set: "help"}}
	b.leaveGameParty()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Goal.Set != "" {
		t.Errorf("goal help kept after leaving the party: %+v", b.Goal)
	}
}

// TestLfgPerfHistogramAndDefaults: the default turns the full map count on, and the step histogram groups each bot
// party's leader step by the party's level band (measurement only).
func TestLfgPerfHistogramAndDefaults(t *testing.T) {
	if !defaultTuning.LfgCountMaps {
		t.Fatal("lfg_count_maps must default to on for the first live run")
	}
	lead := &Bot{P: &Persona{Name: "zzperf_lead"}, BaseLv: 45}
	mem := &Bot{P: &Persona{Name: "zzperf_mem"}, BaseLv: 47}
	p := &Party{Name: "zzperf", Leader: lead, Members: []*Bot{lead, mem}}
	parties.Lock()
	parties.byName[p.Name] = p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.byName, p.Name)
		parties.Unlock()
		gsPerfReset()
	}()
	gsPerfSet(lead.P.Name, "pay_fild04", -9)
	h := lfgPerfHistogram(time.Now())
	if h["lv41-60"]["-9"] != 1 {
		t.Fatalf("histogram = %v, want one lv41-60 party at step -9", h)
	}
}
