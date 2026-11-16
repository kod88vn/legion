package main

import "time"

// Conservative party map choice (owner 2026-11-15, "party not strong enough, choose the right map"; PR40 / PR16, the
// same as Ayla's partylead.pl for her party). A party starts on a map ~10 levels under its gear-rated level
// (partyConsStart), one death steps it down 5 at once (partyConsDeathStep), and a step up needs 20+ minutes of measured
// success on the map (gearscore.go offsetCons, perfVerdict). The rule applies to:
//   - every bot party, when tuning party_conservative_on is on (default OFF until confirmed on Ayla's party);
//   - always, an engine party: its leader is pinned to the human's character named in engine_parties (engineparty.go).

const (
	partyConsStart     = -10
	partyConsDeathStep = -5
	partyConsMin       = -20
	partyConsUpAfter   = 20 * time.Minute
)

// partyConsFor: the conservative rule applies to this party (ms[0] = the leader). It takes no party or bot lock: the
// leader's pin names the human's character for an engine party (engineparty.go startEnginePartyFor).
func partyConsFor(ms []*Bot) bool {
	if len(ms) == 0 {
		return false
	}
	if tune().PartyConservativeOn {
		return true
	}
	return ms[0].pin != nil && enginePartyOn(ms[0].pin.Char)
}
