package main

import (
	"testing"
	"time"
)

// live 2026-11-15 12:07-12:17Z: bots hopping off a map had a median 3.25 kills/min there; they should stay
func TestRoamStayDecision(t *testing.T) {
	w := 5 * time.Minute
	cases := []struct {
		name string
		in   roamStayIn
		want bool
	}{
		{"pays 3 kpm: stay", roamStayIn{kills: 16, window: w, kpm: 1, sinceKill: 20 * time.Second}, true},
		{"exactly 1 kpm: stay", roamStayIn{kills: 5, window: w, kpm: 1, sinceKill: time.Minute}, true},
		{"under 1 kpm: leave", roamStayIn{kills: 4, window: w, kpm: 1, sinceKill: time.Minute}, false},
		{"no kills: leave", roamStayIn{kills: 0, window: w, kpm: 1, sinceKill: 1 << 62}, false},
		{"empty map, no kill for 2 min: leave", roamStayIn{kills: 9, window: w, kpm: 1, seesNothing: true, sinceKill: 2 * time.Minute}, false},
		{"empty for a minute but just killed: stay", roamStayIn{kills: 9, window: w, kpm: 1, seesNothing: true, sinceKill: 30 * time.Second}, true},
		{"kpm 0: never", roamStayIn{kills: 99, window: w, kpm: 0, sinceKill: time.Second}, false},
	}
	for _, c := range cases {
		if got := roamStayDecision(c.in); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// withTravelTuning runs f with a copy of the current tuning changed by mod, then restores it.
func withTravelTuning(t *testing.T, mod func(*Tuning)) {
	t.Helper()
	old := tune()
	nt := *old
	mod(&nt)
	curTuning.Store(&nt)
	t.Cleanup(func() { curTuning.Store(old) })
}

func TestMapKillLog(t *testing.T) {
	withTravelTuning(t, func(tu *Tuning) { tu.RoamStayOn = true })
	b := &Bot{P: &Persona{}, MapName: "mjolnir_07"}
	defer mapKillLogs.Delete(b)
	now := time.Now()
	for i := 7; i >= 0; i-- {
		b.noteMapKill(now.Add(time.Duration(-i) * time.Minute)) // one kill a minute, in time order
	}
	if n, last := b.mapKillsSince(5*time.Minute, now.Add(time.Second)); n != 5 || last.IsZero() {
		t.Fatalf("kills in 5 min = %d, want 5", n)
	}
	b.MapName = "mjolnir_08" // another map: its own count
	if n, _ := b.mapKillsSince(5*time.Minute, now); n != 0 {
		t.Fatalf("kills on a new map = %d", n)
	}
	b.noteMapKill(now)
	if n, _ := b.mapKillsSince(5*time.Minute, now); n != 1 {
		t.Fatalf("restart on map change: %d", n)
	}
	for i := 0; i < 2*mapKillCap; i++ {
		b.noteMapKill(now)
	}
	v, _ := mapKillLogs.Load(b)
	if l := len(v.(*mapKillLog).at); l > mapKillCap {
		t.Fatalf("log grew to %d", l)
	}
}

// roamStay on a bot, with roam_stay_on: kills restart the roam clocks; no kills means the give-up goes ahead
func TestRoamStayBot(t *testing.T) {
	withTravelTuning(t, func(tu *Tuning) { tu.RoamStayOn = true })
	b := &Bot{P: &Persona{}, MapName: "mjolnir_08"}
	defer mapKillLogs.Delete(b)
	now := time.Now()
	b.roamSince = now.Add(-3 * time.Minute)
	if b.roamStay(now) {
		t.Fatal("no kills here: give the map up")
	}
	for i := 9; i >= 0; i-- {
		b.noteMapKill(now.Add(time.Duration(-i*25) * time.Second))
	}
	if !b.roamStay(now) || !b.roamSince.Equal(now) {
		t.Fatal("2 kills/min here: stay, roam clock restarted")
	}
}

// roam_stay_on is off by default: the new rule never keeps a bot, and records no kills
func TestRoamStayDefaultOff(t *testing.T) {
	withTravelTuning(t, func(tu *Tuning) { tu.RoamStayOn = false })
	b := &Bot{P: &Persona{}, MapName: "mjolnir_08"}
	defer mapKillLogs.Delete(b)
	now := time.Now()
	b.roamSince = now.Add(-3 * time.Minute)
	for i := 9; i >= 0; i-- {
		b.noteMapKill(now.Add(time.Duration(-i*25) * time.Second))
	}
	if _, ok := mapKillLogs.Load(b); ok {
		t.Fatal("roam_stay_on false: no kill log is kept")
	}
	if b.roamStay(now) {
		t.Fatal("roam_stay_on false: old behaviour, the map is given up")
	}
	if defaultTuning.RoamStayOn {
		t.Fatal("default must be off")
	}
}

// travel_share_off switches the loop fixes off: the old pickers and the old give-up path
func TestTravelShareOffRestoresOldPickers(t *testing.T) {
	withTravelTuning(t, func(tu *Tuning) { tu.TravelShareOff = true })
	b := &Bot{P: &Persona{}, MapName: "izlude", Zeny: 76}
	defer walkBans.Delete(b)
	boat := []Warp{{From: "izlude", To: "izlu2dun", NPC: "Byalan Island", Fare: 150}}
	if b.fareShort(boat) {
		t.Fatal("travel_share_off: the fare check is not applied")
	}
	b.walkBan("pay_fild10", time.Now())
	if b.pickSkip("pay_fild10") {
		t.Fatal("travel_share_off: banned maps are picked as before")
	}
}

func TestFareShortAndPickSkip(t *testing.T) {
	b := &Bot{P: &Persona{}, MapName: "izlude", Zeny: 76}
	defer walkBans.Delete(b)
	boat := []Warp{{From: "izlude", To: "izlu2dun", NPC: "Byalan Island", Fare: 150}, {From: "izlu2dun", To: "iz_dun00"}}
	if !b.fareShort(boat) {
		t.Fatal("76z can't pay the 150z boat: skip the map")
	}
	b.Zeny = 150
	if b.fareShort(boat) || b.fareShort([]Warp{{From: "a", To: "b"}}) {
		t.Fatal("affordable or free routes stay open")
	}
	if b.pickSkip("pay_fild10") {
		t.Fatal("not banned")
	}
	b.walkBan("pay_fild10", time.Now())
	if !b.pickSkip("pay_fild10") {
		t.Fatal("a walk-banned map is skipped by the SpotsFor picker")
	}
}

// Iron z / ryuPrist (izlude, 4-124z): "can't pay the boat" every tick; a solo bot gives the goal up once
func TestFerryBrokeSolo(t *testing.T) {
	b := &Bot{P: &Persona{}, MapName: "izlude", Zeny: 76, Goal: Goal{Mode: "hunt", Map: "iz_dun01", Set: "auto"}}
	defer walkBans.Delete(b)
	defer ferryBrokeAt.Delete(b)
	st := &ferryState{w: ferries[0]}
	if !b.ferryBroke(st, time.Now()) {
		t.Fatal("solo goal: handled")
	}
	if b.Goal.Map != "" || !b.walkBanned("iz_dun01") {
		t.Fatalf("goal dropped and banned, got %+v", b.Goal)
	}
	// a party / API goal is not ours to drop: first time falls through (event), then quiet re-plans for a minute
	a := &Bot{P: &Persona{}, MapName: "izlude", Zeny: 76, Goal: Goal{Mode: "hunt", Map: "iz_dun01", Set: "api"}}
	defer ferryBrokeAt.Delete(a)
	now := time.Now()
	if a.ferryBroke(st, now) {
		t.Fatal("first time: the old give-up path with its event")
	}
	if !a.ferryBroke(st, now.Add(time.Second)) || a.Goal.Map != "iz_dun01" {
		t.Fatal("within a minute: quiet, goal kept")
	}
}
