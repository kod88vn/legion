package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
)

// rosterDrift (main instance, every 10 min): a human-party member listed in human-parties.json that THIS instance runs
// is not in its -exclude list - the core instance (which plays the human's party) doesn't have it, so it lives its own
// life here (live 2026-11-15: Monk LilLilyblade joined Ayla's roster at 04:36Z but kept "roaming" in aldebaran with its
// old party's goal until both instances were redeployed with the new LEGION_CORE at 05:07Z). Logged loudly so the
// main session redeploys both instances with LEGION_CORE updated.
func rosterDrift(byName map[string]*Bot) {
	if ownsHumanParties() {
		return // the core instance: its -only list is the roster it plays
	}
	for _, n := range rosterDriftNames(byName) {
		log.Printf("ROSTER DRIFT: %s is in a human party roster (human-parties.json) but runs in the main instance - add it to LEGION_CORE and redeploy legion + legion-core", n)
	}
}

// rosterDriftNames: roster members (human-parties.json, not alumni) that this instance runs
func rosterDriftNames(byName map[string]*Bot) []string {
	raw, err := os.ReadFile(humanPartyFile())
	if err != nil {
		return nil
	}
	var saved []savedHumanParty
	if json.Unmarshal(raw, &saved) != nil {
		return nil
	}
	return driftIn(saved, byName)
}

func driftIn(saved []savedHumanParty, byName map[string]*Bot) []string {
	var out []string
	for _, s := range saved {
		for _, n := range s.Members {
			if byName[strings.ToLower(n)] != nil {
				out = append(out, n)
			}
		}
	}
	return out
}
