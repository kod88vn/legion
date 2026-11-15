package main

import (
	"fmt"
	"time"
)

// PT22 for bot parties (queue b179471240048f, owner rule): on a party town run every member wings to town, shops, then
// meets at the town's Kafra, and the party returns together - members used to walk back to the party map one by one.
// After its shopping a member of a bot party (not a human-led one: Ayla's plugin runs that meeting) walks to the Kafra
// and waits until the party members in town are there (or 3 min), then goes back to the party.

const kafraMeetCap = 3 * time.Minute

type kafraMeetState struct {
	Town  string
	X, Y  int
	Until time.Time
	Said  bool
}

// startKafraMeet (b.mu held): the trip that just ended was part of the party's town run in this town
func (b *Bot) startKafraMeet(why string, now time.Time) {
	p := partyOf(b)
	if p == nil || p.Human != "" || why == "calling it a day" {
		return
	}
	town := townOf(b.MapName)
	if town == "" {
		town = b.MapName
	}
	at, rt, _ := p.townRun()
	if at.IsZero() || now.Sub(at) > 10*time.Minute || rt != town {
		return
	}
	x, y, _, ok := rallySpot(town, town)
	if !ok {
		return
	}
	b.kafraMeet = &kafraMeetState{Town: town, X: x, Y: y, Until: now.Add(kafraMeetCap)}
}

// kafraMeetThink (b.mu held): true while the bot waits at the Kafra for the party
func (b *Bot) kafraMeetThink(gm *GameMap, moving bool, now time.Time) bool {
	k := b.kafraMeet
	if k == nil {
		return false
	}
	p := partyOf(b)
	if p == nil || now.After(k.Until) || b.underAttack(now) || townOf(b.MapName) != k.Town && b.MapName != k.Town {
		b.kafraMeet = nil
		return false
	}
	if b.MapName != k.Town { // inside a building: out to the town first
		return false
	}
	_, _, ms := b.partyView(p, now)
	if kafraMeetDone(ms, b.charName(), k.Town, k.X, k.Y) {
		b.kafraMeet = nil
		b.event("party", "", fmt.Sprintf("everyone met at the %s Kafra after the town run: back together", k.Town))
		return false
	}
	b.setState("waiting at the kafra in " + k.Town)
	if !k.Said {
		k.Said = true
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("at the kafra, waiting for u", "done, waiting at the kafra", "kafra, then we go back together")))
	}
	if !moving && gm != nil && dist(b.X, b.Y, k.X, k.Y) > 4 {
		b.stepAlong(gm, k.X, k.Y)
	}
	return true
}

// kafraMeetDone: every online party member who is in that town (or one of its buildings) stands within 6 cells of the
// Kafra; members elsewhere (still on the field, gone home) are not waited for
func kafraMeetDone(ms []pubState, me, town string, x, y int) bool {
	for _, s := range ms {
		if s.Name == me || !s.Online || townOf(s.Map) != town && s.Map != town {
			continue
		}
		if s.Map != town || dist(s.X, s.Y, x, y) > 6 {
			return false
		}
	}
	return true
}

// partyWingStock: a party member below its 2-wing stock buys them before saving for gear
func partyWingStock(inParty bool, have int) bool { return inParty && have < 2 }

// noAheadOfLeader: a member on the leader's map (herMap) whose target is another map would go ahead of her - not while
// she is outside a town (in a town a meeting / town run may send members to a building or the Kafra town)
func noAheadOfLeader(herMap, target string) bool {
	return herMap != "" && target != herMap && !isTown(herMap) && townOf(herMap) == ""
}
