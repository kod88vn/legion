package main

import "testing"

func TestRosterDriftIn(t *testing.T) {
	saved := []savedHumanParty{{Human: "Ayla", Members: []string{"Zendancer84", "LilLilyblade"}, Alumni: []string{"Silent rogue"}}}
	byName := map[string]*Bot{"lillilyblade": {}, "silent rogue": {}}
	got := driftIn(saved, byName)
	if len(got) != 1 || got[0] != "LilLilyblade" {
		t.Fatalf("drift = %v, want [LilLilyblade] (alumni don't count)", got)
	}
}
