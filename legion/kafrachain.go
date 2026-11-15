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

// kafraChainOK (b.mu held): kafraChainHop, or the next leg of a party follow whose goal moved. QA 2026-11-15 (core):
// Zendancer84 sat "waiting in aldebaran for the Kafra (the walk to payon is too dangerous)" while Ayla was in Payon:
// its earlier hops in the last 10 min chased Ayla's previous towns (another goal each time, or a goal-less hop read
// back from the diary after a restart), so the same-goal chain test failed, the 3-in-10-min cap held every
// destination and the aldebaran -> geffen/izlude -> payon chain was never planned. A member following its leader
// (kafraPartyMove: a human leader's leg, a bot party move) chains on to the leader's current map; never straight back
// (the X -> Y -> X block) and never past kafraChainMax hops in 10 min.
func (b *Bot) kafraChainOK(l kafraHop, dest, goal string, n10 int) bool {
	if kafraChainHop(l, dest, goal, n10) {
		return true
	}
	if tune().KafraChainOff || n10 >= kafraChainMax || goal == "" || !b.kafraPartyMove() {
		return false
	}
	return dest == "" || dest != l.From && nearestTown(dest) != l.From
}
