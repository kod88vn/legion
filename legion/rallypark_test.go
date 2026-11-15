package main

import (
	"testing"
	"time"
)

// Zen.fox, Geffen 2026-11-14: rallyThink stood it up every second, kafraWaitInTown sat it down 3 s after. With the
// waiter's claim the stand-up keeps off, so one sit is sent and it stays seated.
func TestWaitSitSingleOwner(t *testing.T) {
	b := &Bot{P: &Persona{}}
	t0 := time.Now()
	sits, stands := 0, 0
	sitting := false
	last := time.Time{}
	for i := 0; i < 60; i++ { // 30 s of 500 ms ticks
		now := t0.Add(time.Duration(i) * 500 * time.Millisecond)
		// rallyThink's stand-up (member not in the rally town) - now guarded by the waiter's claim
		if sitting && now.Sub(last) > time.Second && !b.waitSitHeld(now) {
			last, sitting = now, false
			stands++
		}
		// kafraWaitInTown's waiter
		waitSits.Store(b, now)
		if waitSitDue(sitting, false, last, now) {
			last, sitting = now, true
			sits++
		}
	}
	waitSits.Delete(b)
	if sits != 1 || stands != 0 {
		t.Fatalf("one owner: want 1 sit 0 stands, got %d sits %d stands", sits, stands)
	}
	// the claim runs out 2 s after the waiter stops: the usual stand-up works again
	waitSits.Store(b, t0)
	if !b.waitSitHeld(t0.Add(time.Second)) || b.waitSitHeld(t0.Add(3*time.Second)) {
		t.Fatal("claim must hold 2 s and then lapse")
	}
	waitSits.Delete(b)
}

func TestRallyLegPlan(t *testing.T) {
	cap := 12 * time.Minute
	base := rallyLegIn{sinceRally: time.Minute, cap: cap, here: "geffen", town: "yuno", hopsHere: 6, hopsSaved: 3}
	if p := rallyLegPlan(base); p != "" {
		t.Fatalf("not blocked: travel as usual, got %q", p)
	}
	in := base
	in.blocked = true
	if p := rallyLegPlan(in); p != "out" {
		t.Fatalf("blocked, no wing: leave the rally and wait, got %q", p)
	}
	in.wing, in.saved = true, "aldebaran"
	if p := rallyLegPlan(in); p != "wing" {
		t.Fatalf("blocked, wing to a nearer save town, got %q", p)
	}
	in.wingUsed = true
	if p := rallyLegPlan(in); p != "out" {
		t.Fatalf("wing used already this rally: out, got %q", p)
	}
	in.wingUsed, in.hopsSaved = false, 8
	if p := rallyLegPlan(in); p != "out" {
		t.Fatalf("save town farther than here: no wing, got %q", p)
	}
	in.saved, in.hopsSaved = "geffen", 6
	if p := rallyLegPlan(in); p != "out" {
		t.Fatalf("saved where we stand: no wing, got %q", p)
	}
	late := base
	late.sinceRally = 13 * time.Minute
	if p := rallyLegPlan(late); p != "giveup" {
		t.Fatalf("over the cap: give the rally up, got %q", p)
	}
}

func TestParkStep(t *testing.T) {
	cap := 12 * time.Minute
	t0 := time.Now()
	r := &parkRec{}
	fired := 0
	for s := 0; s <= 13*60; s++ {
		if parkStep(r, "izlude", t0.Add(time.Duration(s)*time.Second), cap) {
			fired++
			if s < 12*60 {
				t.Fatalf("fired before the cap at %d s", s)
			}
		}
	}
	if fired != 1 || r.gaveUp != "izlude" {
		t.Fatalf("park cap: want exactly one give-up of izlude, got %d (%q)", fired, r.gaveUp)
	}
	// a new goal restarts the clock
	now := t0.Add(14 * time.Minute)
	if parkStep(r, "yuno_fild04", now, cap) || r.since != now {
		t.Fatal("a new goal must restart the clock")
	}
	// a break over 30 s restarts it as well
	r2 := &parkRec{}
	parkStep(r2, "izlude", t0, cap)
	parkStep(r2, "izlude", t0.Add(11*time.Minute), cap)
	if parkStep(r2, "izlude", t0.Add(12*time.Minute+time.Second), cap) {
		t.Fatal("a 30 s+ break must restart the park clock")
	}
}

func TestParkFarm(t *testing.T) {
	if got := parkFarm("izlude", "yuno_fild04"); got != "yuno_fild04" {
		t.Fatalf("given-up town -> farm map, got %q", got)
	}
	if got := parkFarm("izlude", ""); got != "izlude" {
		t.Fatalf("no farm map known: keep, got %q", got)
	}
	if got := parkFarm("izlude", "prontera"); got != "izlude" {
		t.Fatalf("a town is no farm map, got %q", got)
	}
}
