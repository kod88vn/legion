package main

// Package E (ayla-parity): the engine must play Ayla's party as well as OpenKore did. Every case runs under the
// engine_parties switch (ovEngineParty: an engine party led by a human named in engine_parties); the human's name is
// never special-cased (G25). Tests use in-memory state only (G23): no live data, no files outside t.TempDir().

import (
	"sync/atomic"
	"testing"
	"time"
)

// parityPub publishes a member's public state for the test (peek reads it).
func parityPub(t *testing.T, s pubState) {
	t.Helper()
	v := &atomic.Value{}
	v.Store(s)
	pubs.Store(s.Name, v)
	t.Cleanup(func() { pubs.Delete(s.Name) })
}

// TestParityRallyLeavesTownRunMemberAlone (AY23): a member on a town run is not waited for by the rally. Engine party:
// it is left out of the rally members. Off the switch the old list is kept.
func TestParityRallyLeavesTownRunMemberAlone(t *testing.T) {
	for _, on := range []bool{true, false} {
		eng := "AylaOwn"
		if !on {
			eng = ""
		}
		withTuning(t, func(tn *Tuning) { tn.EngineParties = eng })
		p, lead, mem := ovHookParty(t, "parity-rally-"+map[bool]string{true: "on", false: "off"}[on], true)
		parityPub(t, pubState{Name: lead.P.Name, Map: "good_map", X: 100, Y: 100, Online: true, Known: true})
		parityPub(t, pubState{Name: mem.P.Name, Map: "good_map", X: 110, Y: 100, Online: true, Known: true, Trip: true})
		got := p.rallyMembers(time.Now())
		inRally := false
		for _, m := range got {
			if m.name == mem.P.Name {
				inRally = true
			}
		}
		if on && inRally {
			t.Error("engine party: a member on a town run is still waited for by the rally (AY23)")
		}
		if !on && !inRally {
			t.Error("switch off: the rally member list changed (old behaviour must hold)")
		}
	}
}

// TestParityTownRunCalledOnce: a party town run is called once per gap; a second call inside the gap is refused, so
// the party does not run to town twice (AY23 "no bouncing"; callTownRunGap, party.go).
func TestParityTownRunCalledOnce(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	p, _, _ := ovHookParty(t, "parity-town-once", true)
	now := time.Now()
	if !p.callTownRun("prontera", "AylaOwn", now) {
		t.Fatal("the first town run of the party was refused")
	}
	if p.callTownRun("prontera", "AylaOwn", now.Add(time.Minute)) {
		t.Fatal("a second town run inside the gap was called")
	}
}

// TestParityG12TradePayment (G12): Ayla's trade payment never takes her under 10,000z. Exact floor: 10,000 left is
// allowed, one zeny more is not.
func TestParityG12TradePayment(t *testing.T) {
	a := engineMk(61, "Ayla")
	a.Zeny = 11000
	if a.zenyPayOK(2000) {
		t.Error("G12: a 2,000z trade payment from 11,000z leaves Ayla under 10,000z")
	}
	if !a.zenyPayOK(1000) {
		t.Error("G12: a 1,000z trade payment from 11,000z leaves exactly 10,000z and must be allowed")
	}
	a.Zeny = 9999
	if a.zenyPayOK(1) {
		t.Error("G12: Ayla under 10,000z must send no zeny at all")
	}
	other := engineMk(62, "ParityHuman")
	other.Zeny = 500
	if !other.zenyPayOK(500) {
		t.Error("the floor applies to Ayla only: another player may pay its whole purse")
	}
}

// TestParityG12MailGift: a mail gift (the gear pool's way to send zeny) keeps Ayla at 10,000z, fee included.
func TestParityG12MailGift(t *testing.T) {
	const amt = 1000
	if aylaSpendOK("Ayla", 10000+mailCost(amt)-1, mailCost(amt)) {
		t.Error("G12: a gift with its fee leaves Ayla under 10,000z")
	}
	if !aylaSpendOK("Ayla", 10000+mailCost(amt), mailCost(amt)) {
		t.Error("G12: a gift that leaves exactly 10,000z must be allowed")
	}
}

// TestParityG12GearGiftCap: the gear pool's gift to a mate never takes Ayla under the floor: giftableZeny is capped
// at what is left above 10,000z.
func TestParityG12GearGiftCap(t *testing.T) {
	a := engineMk(63, "Ayla")
	a.mu.Lock()
	a.Zeny = 12000
	got := a.giftableZeny()
	a.mu.Unlock()
	if got > 2000 {
		t.Errorf("G12: Ayla with 12,000z may give %d to the gear pool, at most 2,000 is allowed", got)
	}
}

// TestParityCollectLagGrace (AY23): the leader gives a lagging member 10 s to catch up before it goes back to collect
// it. Inside the 10 s no collect; at 10 s one collect; a lag that clears and comes back is a new episode (5 s unseen).
func TestParityCollectLagGrace(t *testing.T) {
	b := engineMk(64, "ParityLead")
	t0 := time.Now()
	if b.collectLagDue("ParityMate", t0) {
		t.Fatal("collect due at the first sighting of a lag: the 10 s grace was skipped")
	}
	for _, d := range []time.Duration{3, 6, 9} {
		if b.collectLagDue("ParityMate", t0.Add(d*time.Second)) {
			t.Fatalf("collect due after %ds: the grace is about 10 s", d)
		}
	}
	if !b.collectLagDue("ParityMate", t0.Add(10*time.Second)) {
		t.Fatal("no collect at 10 s: a member still lagging after the grace must be collected")
	}
	// the member caught up for a while, then fell behind again: a new 10 s grace starts
	if b.collectLagDue("ParityMate", t0.Add(30*time.Second)) {
		t.Fatal("a new lag episode (after 5 s unseen) must start a new grace, not collect at once")
	}
}

// TestParityLeftAloneOnTrip (AY23): a member on a town run is not collected under the engine switch; off the switch
// the old rule is kept.
func TestParityLeftAloneOnTrip(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.EngineParties = "AylaOwn" })
	p, _, _ := ovHookParty(t, "parity-trip-collect", true)
	if !leftAloneOnTrip(p, pubState{Name: "TripMate", Trip: true}) {
		t.Error("engine party: a member on a town run must be left alone")
	}
	if leftAloneOnTrip(p, pubState{Name: "TripMate"}) {
		t.Error("engine party: a member not on a town run is collected as before")
	}
	old := &Party{Name: "parity-trip-old", Engine: false}
	if leftAloneOnTrip(old, pubState{Name: "TripMate", Trip: true}) {
		t.Error("a party off the engine switch keeps the old collect rule")
	}
}
