package main

// meterparty.go: which party a meter row belongs to, for party credit (metercohort.go meterDerive) and Ayla's party
// (meterapi.go meterNeo).
//
// Bug b1794711155fmf (2026-11-14): the engine's b.PartyID is a placeholder, not the server's id - the party member
// list (0x0fb/0x0a44/0x0ae5) and member info (0x0ae4) packets set b.PartyID = 1 for every bot in any party. Keying
// the party credit on PartyID pooled ALL party bots of both instances into one "party", so every party bot showed
// the same kpm/zph (2.469 / 22376) equal to its cohort's par. Party names are unique on the server, so the name is
// the key; a real id (> 1, e.g. from an accepted invite) is the fallback; the placeholder alone pools nothing.

import (
	"strconv"
	"strings"
)

// meterPartyKey: "" when the row has no identifiable party (then it keeps its own numbers).
func meterPartyKey(partyName string, partyID uint32) string {
	if n := strings.ToLower(strings.TrimSpace(partyName)); n != "" {
		return "n:" + n
	}
	if partyID > 1 {
		return "#" + strconv.FormatUint(uint64(partyID), 10)
	}
	return ""
}

func (r *meterRow) partyKey() string { return meterPartyKey(r.Party, r.PartyID) }
