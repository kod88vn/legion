package main

// Kafra chains (owner 2026-11-14: "Ayla's party is walking instead of using the Kafra"; EC04 long trips go by Kafra
// teleport). QA 02:46-02:52Z: on the move payon -> geffen (gl_church) Zen.fox and Echobunny had already taken 3 Kafra
// teleports in 10 min - aldebaran -> izlude -> payon -> prontera, each one chasing the town Ayla was crossing - so the
// 3-in-10-min hard cap refused the hop prontera -> geffen and they walked prt_fild05 -> gef_fild00 (4-5 min) while
// vex_x18, Silent rogue and xXRinfangXx (fewer hops) took the same Kafra in 10 s.
//
// The cap is there against lapping (prontera <-> Morocc 12x in 2.5 min); the X -> Y -> X block catches lapping on its
// own. So the next hop of ONE trip (the same goal map as the last hop, not back to the town it left) may go up to
// kafraChainMax hops in 10 minutes. Tuning kafra_chain_off restores the plain cap.

const kafraChainMax = 5

// kafraChainHop: a teleport toward goal (to dest, "" = any) continues the trip of the last hop l.
func kafraChainHop(l kafraHop, dest, goal string, n10 int) bool {
	if tune().KafraChainOff || n10 >= kafraChainMax || goal == "" || l.Goal == "" || l.Goal != goal {
		return false
	}
	if dest != "" && (dest == l.From || nearestTown(dest) == l.From) {
		return false // straight back: never a chain
	}
	return true
}
