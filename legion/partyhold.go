package main

import "time"

// Party members' detour hold point (party.go partyGoalSync -> detourHold): one fixed regroup Kafra, no flipping, no
// chasing. QA 2026-11-14 19:27-20:06Z (life mind): ryudancer pt's "waiting in" target flipped izlude -> prt_in ->
// prontera within a minute (the checkpoint / the leader's map as it walked into a building); Kaiboy and xXFrostroseXx
// waited in alberta while their leaders went elsewhere; Yukiqueen (Nova hunter pt) took the Kafra prontera -> alberta
// chasing its leader's hops. Owner rule: parties regroup at the Kafra nearest the destination. So a member that has to
// wait waits in that one town - the party's checkpoint (nearestTown of the party map) - never in a building and never
// in whatever town the leader passes through; and the choice stays for 5 minutes while the party map is the same.
// Staying put, reaching the party map, or following the leader across a field portal are unchanged.

// stableHold (b.mu held): the hold point to use, given detourHold's pick.
func (b *Bot) stableHold(pick, partyMap, checkpoint, myMap string, lead pubState, now time.Time) string {
	if pick == "" || pick == myMap || pick == partyMap || pick == lead.Map && townOf(pick) == "" {
		b.holdTown, b.holdFor = "", ""
		return pick // stay / the party map / right behind the leader on a field: fine as it is
	}
	if checkpoint == "" || townOf(checkpoint) == "" {
		checkpoint = nearestTown(partyMap)
	}
	if t := townOf(checkpoint); t != "" {
		checkpoint = t // never a building: its town (where the Kafra is)
	}
	if townOf(pick) != "" {
		pick = checkpoint // any town wait is the regroup town, not the leader's current stop
	}
	if b.holdFor == partyMap && b.holdTown != "" && now.Sub(b.holdAt) < 5*time.Minute {
		return b.holdTown // keep the town chosen a moment ago: no flipping
	}
	b.holdTown, b.holdFor, b.holdAt = pick, partyMap, now
	return pick
}
