package main

import (
	"testing"
	"time"
)

// Off by default: a bot party without the flag and not an engine party does not get the rule.
func TestPartyConsOffByDefault(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyConservativeOn = false; tn.EngineParties = "" })
	if partyConsFor([]*Bot{{}, {}}) {
		t.Fatal("a bot party with party_conservative_on off got the conservative rule")
	}
}

// The flag turns the rule on for every bot party.
func TestPartyConsFlagOn(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyConservativeOn = true; tn.EngineParties = "" })
	if !partyConsFor([]*Bot{{}, {}}) {
		t.Fatal("party_conservative_on on: the rule did not apply to a bot party")
	}
}

// An engine party (leader pinned to a character named in engine_parties) has the rule with the flag off.
func TestPartyConsEngineParty(t *testing.T) {
	withTuning(t, func(tn *Tuning) { tn.PartyConservativeOn = false; tn.EngineParties = "AylaOwn" })
	if !partyConsFor([]*Bot{{pin: &Pin{Char: "AylaOwn"}}, {}}) {
		t.Fatal("engine party with flag off: the rule did not apply")
	}
	if partyConsFor([]*Bot{{pin: &Pin{Char: "SomeoneElse"}}, {}}) {
		t.Fatal("a party led by a character not in engine_parties got the rule")
	}
}

// A party starts ~10 levels under its gear-rated level and a step up needs 20+ minutes of measured success.
func TestPartyConservativeSteps(t *testing.T) {
	tr := &perfTracker{s: map[string]*perfState{}}
	now := time.Now()
	if o := tr.offsetCons("lead", "pay_dun02", false, 5, 0, 0, 0, now, true); o != partyConsStart {
		t.Fatalf("start %d, want %d", o, partyConsStart)
	}
	// 10 min of easy farming: no step up yet
	if o := tr.offsetCons("lead", "pay_dun02", false, 5, 60, 0, 0, now.Add(10*time.Minute), true); o != partyConsStart {
		t.Fatalf("after 10 min %d, want still %d", o, partyConsStart)
	}
	// 21 min, 5 members, 120 kills (> 0.8 per member-minute), no deaths: step up
	if o := tr.offsetCons("lead", "pay_dun02", false, 5, 120, 0, 0, now.Add(21*time.Minute), true); o <= partyConsStart {
		t.Fatalf("after 21 min of success %d, want above %d", o, partyConsStart)
	}
	before := tr.s["lead"].off
	// one death: down 5 at once
	if o := tr.offsetCons("lead", "pay_dun02", false, 5, 125, 0, 1, now.Add(22*time.Minute), true); o != before+partyConsDeathStep {
		t.Fatalf("after a death %d, want %d", o, before+partyConsDeathStep)
	}
}

// Repeated deaths stop at the conservative floor, not the solo floor.
func TestPartyConservativeFloor(t *testing.T) {
	tr := &perfTracker{s: map[string]*perfState{}}
	now := time.Now()
	tr.offsetCons("lead", "pay_dun02", false, 5, 0, 0, 0, now, true)
	var o int
	for i := 1; i <= 4; i++ {
		o = tr.offsetCons("lead", "pay_dun02", false, 5, 0, 0, i, now.Add(time.Duration(i)*time.Minute), true)
	}
	if o != partyConsMin {
		t.Fatalf("four deaths: %d, want the floor %d", o, partyConsMin)
	}
}

// Without the rule (the solo path) the start stays 0.
func TestPartyConsOffKeepsOldStart(t *testing.T) {
	tr := &perfTracker{s: map[string]*perfState{}}
	if o := tr.offset("p", "pay_dun02", false, 5, 0, 0, 0, time.Now()); o != 0 {
		t.Fatalf("start without the rule %d, want 0", o)
	}
}
