package main

import (
	"testing"
	"time"
)

// no back-and-forth Kafra trips (owner rule; QA 2026-11-14 LilGhostgirl prontera -> Geffen -> Prontera in 2 min), but
// the second hop of one trip, respawns, wing escapes and the way out after a town run stay free (QA after 07:44)
func TestKafraHold(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{}} // no buffAt: no event (charName needs a character)
	if b.kafraHold("geffen", "", now) {
		t.Fatal("no teleport yet: nothing to hold")
	}
	b.kafraLast = kafraHop{At: now.Add(-2 * time.Minute), From: "payon", To: "prontera"}
	b.kafraHops = []time.Time{b.kafraLast.At}
	if b.kafraHold("geffen", "", now) {
		t.Fatal("second hop of one trip (payon -> prontera -> geffen) must be free")
	}
	if !b.kafraHold("payon", "", now) {
		t.Fatal("straight back to payon 2 min later must be held")
	}
	// a respawn or a wing escape lifts only the hourly cap, never the A -> B -> A block (QA 2026-11-14)
	b.diedAt = now.Add(-time.Minute)
	if !b.kafraHold("payon", "", now) {
		t.Fatal("a respawn must not reopen the A->B->A hop")
	}
	b.diedAt = time.Time{}
	b.escWingAt, b.BaseLv = now.Add(-time.Minute), 40
	if !b.kafraHold("payon", "", now) {
		t.Fatal("a wing escape must not reopen the A->B->A hop")
	}
	b.BaseLv = 0
	b.escWingAt = time.Time{}
	b.kafraLast.ForTrip = true
	if b.kafraHold("payon", "", now) {
		t.Fatal("the way out after a town run is free")
	}
	b.kafraLast.ForTrip = false
	b.trip = &Trip{}
	if b.kafraHold("geffen", "", now) {
		t.Fatal("a town run is a real need")
	}
	if !b.kafraHold("payon", "", now) {
		t.Fatal("even a town run never goes X -> Y -> X within 10 min (QA 19:00Z Yuno laps)")
	}
	b.trip = nil
	b.kafraHops = []time.Time{now.Add(-50 * time.Minute), now.Add(-30 * time.Minute), now.Add(-2 * time.Minute)}
	if !b.kafraHold("geffen", "", now) {
		t.Fatal("a 4th teleport within the hour must be held")
	}
	b.diedAt = now.Add(-time.Minute)
	if b.kafraHold("geffen", "", now) {
		t.Fatal("after a respawn the hourly cap doesn't apply")
	}
	b.diedAt = time.Time{}
	b.escWingAt, b.BaseLv = now.Add(-time.Minute), 40
	if b.kafraHold("geffen", "", now) {
		t.Fatal("after a wing escape the hourly cap doesn't apply")
	}
	b.escWingAt, b.BaseLv = time.Time{}, 5
	if !b.kafraHold("geffen", "", now) {
		t.Fatal("a beginner's wing escape doesn't lift the cap")
	}
	b.BaseLv = 0
	pt := &Party{Name: "zz test pt", Leader: b, Members: []*Bot{b}, Map: "mjo_dun02"}
	parties.Lock()
	parties.ofBot[b] = pt
	parties.Unlock()
	b.Goal = Goal{Mode: "hunt", Map: "mjo_dun02", Set: "party-leader"}
	held := b.kafraHold("geffen", "", now)
	b.Goal = Goal{Mode: "party", Map: "mjo_dun02", Set: "party"}
	heldM := b.kafraHold("geffen", "", now)
	parties.Lock()
	delete(parties.ofBot, b)
	parties.Unlock()
	b.Goal = Goal{}
	if held || heldM {
		t.Fatal("a party move (leader or member) lifts the hourly cap")
	}
	b.kafraHops = b.kafraHops[1:]
	b.kafraLast.At = now.Add(-55 * time.Minute)
	if b.kafraHold("payon", "", now) {
		t.Fatal("back to payon after 55 min is fine")
	}
}

// QA 2026-11-14 19:20Z: TheShadowsenpai (gear-rated ~51) walked aldebaran -> mjolnir_12 (lv63-65) while the Kafra was
// refused by the hard limit; the hard limit records when it clears so travel can wait in town.
func TestKafraHardUntil(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{}}
	b.kafraLast = kafraHop{At: now.Add(-2 * time.Minute), From: "geffen", To: "aldebaran"}
	b.kafraHops = []time.Time{now.Add(-8 * time.Minute), now.Add(-4 * time.Minute), b.kafraLast.At}
	if !b.kafraHoldQ("prontera", "", now, true) || !b.kafraHardUntil.IsZero() {
		t.Fatal("4th teleport in 10 min must be held; a quiet estimate must not record the limit (farmshare.go)")
	}
	if !b.kafraHoldQ("prontera", "", now, false) {
		t.Fatal("4th teleport in 10 min must be held")
	}
	if want := now.Add(2 * time.Minute); b.kafraHardUntil.Sub(want) > time.Second || want.Sub(b.kafraHardUntil) > time.Second {
		t.Fatalf("hard limit clears at %v, want ~%v (oldest of the 3 + 10 min)", b.kafraHardUntil, want)
	}
}

// QA 2026-11-15 (core): Zendancer84 waited in aldebaran for the Kafra toward payon (Ayla there): its 3 hops in the
// last 10 min chased Ayla's earlier towns (other goals), so the same-goal chain failed and the hard cap held the
// aldebaran -> geffen/izlude -> payon chain. A member on its leader's leg chains on; others still hit the cap.
func TestKafraChainFollowsMovedLeader(t *testing.T) {
	now := time.Now()
	b := &Bot{P: &Persona{}, MapName: "aldebaran", Goal: Goal{Map: "payon", Mode: "party", Set: "human"}}
	b.kafraLast = kafraHop{At: now.Add(-2 * time.Minute), From: "geffen", To: "aldebaran", Goal: "aldebaran"}
	b.kafraHops = []time.Time{now.Add(-8 * time.Minute), now.Add(-5 * time.Minute), b.kafraLast.At}
	parties.Lock()
	parties.ofBot[b] = &Party{Human: "Ayla"}
	parties.Unlock()
	defer func() { parties.Lock(); delete(parties.ofBot, b); parties.Unlock() }()
	if !healerStickOn() {
		t.Skip("healer stick off")
	}
	if !b.kafraHold("geffen", "payon", now) {
		t.Fatal("straight back still held")
	}
	if b.kafraHold("izlude", "payon", now) {
		t.Fatal("aldebaran -> izlude toward Ayla in payon must not be held by the 3-in-10-min cap")
	}
	if !b.kafraChainOK(b.kafraLast, "izlude", "payon", 3) {
		t.Fatal("a member following a human leader whose map moved: the next hop chains on")
	}
	if b.kafraChainOK(b.kafraLast, "geffen", "payon", 3) {
		t.Fatal("never straight back to the town the last hop left")
	}
	if b.kafraChainOK(b.kafraLast, "izlude", "payon", kafraChainMax) {
		t.Fatal("never past kafraChainMax hops in 10 min")
	}
	solo := &Bot{P: &Persona{}, MapName: "aldebaran", Goal: Goal{Map: "payon", Mode: "hunt"}}
	if solo.kafraChainOK(b.kafraLast, "izlude", "payon", 3) {
		t.Fatal("a solo bot with a changed goal keeps the plain cap")
	}
}
