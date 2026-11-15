package main

import "time"

// rallyleave (live 2026-11-15 01:37): the priest and Echobunny sat in "rally: waiting at the Kafra in payon" after Ayla
// had left Payon for pay_dun02 - nothing ended the members' rally when the human leader moved on, so she had to walk
// back to collect them. Now, once the rally has run 20 s, the human leader standing outside the rally town (not in it,
// not in one of its buildings) ends it; the members then follow her (humanPartySync: her current map).

const rallyLeaveGrace = 20 * time.Second

// humanLeftRally: the human leader is on map herMap; the party's rally is in town (started at); true when she has
// left that town after the grace
func humanLeftRally(town, herMap string, started, now time.Time) bool {
	if town == "" || herMap == "" || now.Sub(started) < rallyLeaveGrace {
		return false
	}
	return herMap != town && townOf(herMap) != town
}
