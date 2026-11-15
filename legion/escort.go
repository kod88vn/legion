package main

// Escorted crossings (owner 2026-11-14, via the map agent brief): a party's hunting map whose way in crosses a map
// too dangerous for some member (mjo_dun01/02 behind mjolnir_02's Elder Willow / Stem Worm trains: priests Onirogue 7
// deaths, xXYukiqueenXx 5, Wolf Bless, walking to their party alone) is crossed TOGETHER:
//   - a member travelling to the party never walks into such a map alone: it goes to the last safe map before it
//     (the "safe point"), waits there, and says so in party chat ("waiting at mjolnir_01 ...");
//   - the party leader comes back for it: when a member waits at a safe point, the leader (if it is not already on
//     its way somewhere it must be) walks to that safe point, waits until the waiting members are within
//     escortGatherCells, then leads them across - they are "escorted" for escortWindow and follow the leader's map;
//   - while escorted (the leader on the member's map within escortGatherCells, or recently), the member travels on.
// The danger test is partyMapTooHardHere at the member's own rating (soloRateLv: mages / alchemists 6 weaker), the
// same rule every map picker uses.

import (
	"fmt"
	"time"
)

const (
	escortGatherCells  = 20              // "together" (RULES.md 3)
	escortWindow       = 4 * time.Minute // escorted: free to follow the leader across for this long
	escortSayGap       = 3 * time.Minute // the waiting line is repeated at most this often
	escortLeaderMax    = 6 * time.Minute // a leader gives an escort up after this (the member is left to the fit check)
	escortMemberMax    = 3 * time.Minute // a member waits at most this long, then leaves the party (class QA 20:05: 22-24 bots stuck waiting; was 8 min)
	escortApproachHops = 3               // a danger at most this many maps before the hunting map is its approach
)

// crossingDanger (b.mu held): on the walk from here to dest, the first map (dest and towns excluded) too hard for this
// bot, and the map just before it (the safe point; our own map when the danger is next). Cached per (map, dest).
func (b *Bot) crossingDanger(dest string) (safe, danger string) {
	if dest == "" || dest == b.MapName {
		return "", ""
	}
	key := b.MapName + "|" + dest
	if b.escKey == key {
		return b.escSafe, b.escDanger
	}
	rate := b.soloRateLv(b.powerLv())
	from := b.MapName
	safe, danger = "", ""
	route := RouteMaps(b.MapName, b.X, b.Y, dest, b.powerLv())
	near := false
	for i, w := range route {
		if w.To != dest && !isTown(w.To) && partyMapTooHardHere(w.To, rate) {
			safe, danger = from, w.To
			near = len(route)-1-i <= escortApproachHops // the danger is on the hunting map's approach
			break
		}
		from = w.To
	}
	b.escKey, b.escSafe, b.escDanger, b.escNear = key, safe, danger, near
	return safe, danger
}

// escortThink (b.mu held, from think before travel): the member's hold at a safe point and the leader's escort.
// True when it took this tick.
func (b *Bot) escortThink(gm *GameMap, moving bool, now time.Time) bool {
	p := partyOf(b)
	if p == nil || p.Human != "" || gm == nil {
		b.escWaitAt = ""
		return false
	}
	if p.Leader == b {
		return b.escortLead(p, gm, moving, now)
	}
	b.escWaitAt = ""
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	// only on the way to the party's HUNTING map, never on a town run / rally / errand (QA after the 15:00 deploy:
	// members on a party town run to izlude / payon / alberta "waited for an escort" at aldebaran and pay_fild03, and
	// their leaders walked across the world to fetch them)
	if b.Goal.Mode != "party" || b.Goal.Map == "" || b.Goal.Map == b.MapName || b.Goal.Map != pm || isTown(b.Goal.Map) ||
		b.trip != nil || now.Before(b.escUntil) {
		b.escWaitSince = time.Time{}
		return false
	}
	safe, danger := b.crossingDanger(b.Goal.Map)
	// only a danger on the APPROACH (a few maps before the hunting map), from a field: from a town, or with the danger
	// far out on a long walk, the Kafra goes around it (swarm.go swarmPenalty counts too-hard maps)
	if danger == "" || isTown(safe) || townOf(b.MapName) != "" || !b.escNear {
		b.escWaitSince = time.Time{}
		return false
	}
	// the leader is here, close: it is taking us across
	if s, ok := peek(p.Leader.P.Name); ok && s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= escortGatherCells {
		b.escUntil = now.Add(escortWindow)
		b.event("party", "", fmt.Sprintf("crossing %s with %s", danger, s.Name))
		return false
	}
	if b.MapName != safe {
		// walk to the safe point first (the rest of the route is the same)
		saved := b.Goal
		b.Goal = Goal{Mode: "party", Map: safe, Who: saved.Who, Set: saved.Set}
		b.travel(gm, moving)
		b.Goal = saved
		b.setState(fmt.Sprintf("going to %s to cross %s with the party", safe, danger))
		return true
	}
	// at the safe point: wait for the escort, say so - at most escortMemberMax, then leave the party politely (the map
	// is beyond us; partyFitCheck also moves parties off maps too hard for a member)
	if b.escWaitSince.IsZero() {
		b.escWaitSince = now
	}
	if w := now.Sub(b.escWaitSince); w > escortMemberMax {
		b.escWaitSince = time.Time{}
		b.event("party", "", fmt.Sprintf("no escort through %s for %s: leaving the party", danger, w.Round(time.Minute)))
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("cant get to you guys, gonna lvl somewhere else. cya!", "ok i cant make it through "+mapTalk(danger)+", going solo for now. ty")))
		go p.drop(b)
		return true
	}
	b.escWaitAt = safe
	b.target = 0
	b.setState(fmt.Sprintf("waiting at %s for an escort through %s", safe, danger))
	if now.Sub(b.escSaidAt) > escortSayGap {
		b.escSaidAt = now
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("waiting at "+mapTalk(safe)+", "+mapTalk(danger)+" is too much for me alone, come get me?",
			"im at "+mapTalk(safe)+", cant cross "+mapTalk(danger)+" solo. someone walk me through pls")))
	}
	if !b.underAttack(now) && !b.Sitting && !moving && now.Sub(b.sitSentAt) > 20*time.Second && b.basicLv() >= 3 {
		b.sitSentAt = now
		go b.sit()
	}
	return true
}

// escortLead (b.mu held, b leads p): a member waits at a safe point -> go there, gather, lead across.
func (b *Bot) escortLead(p *Party, gm *GameMap, moving bool, now time.Time) bool {
	if b.trip != nil || b.underAttack(now) || now.Before(b.escUntil) {
		return false // on a run / in a fight / leading the party across right now
	}
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	if pm == "" || isTown(pm) || b.Goal.Map != pm || isTown(b.Goal.Map) {
		b.escLeadAt = time.Time{}
		return false // not leading the party to its hunting map (town run, rally, hold): no escorts
	}
	// who waits, and where
	at := ""
	var waiting []pubState
	for _, m := range p.Members {
		if m == b {
			continue
		}
		// only a safe point on the party map's approach (a few maps away), never a town
		if s, ok := peek(m.P.Name); ok && s.Online && s.EscortAt != "" && !isTown(s.EscortAt) && (at == "" || s.EscortAt == at) &&
			mapHops(s.Map, s.X, s.Y, pm) <= escortApproachHops+1 {
			at = s.EscortAt
			waiting = append(waiting, s)
		}
	}
	if at == "" {
		b.escLeadAt = time.Time{}
		return false
	}
	if b.escLeadAt.IsZero() {
		b.escLeadAt = now
		b.event("party", "", fmt.Sprintf("going back to %s to escort %d member(s)", at, len(waiting)))
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("coming to "+mapTalk(at)+", wait there", "omw back to "+mapTalk(at)+", well cross together")))
	}
	if now.Sub(b.escLeadAt) > escortLeaderMax {
		return false // given up for now: the fit check decides (the map may be too hard for that member)
	}
	if b.MapName != at {
		saved := b.Goal
		b.Goal = Goal{Mode: "hunt", Map: at, Set: saved.Set}
		b.travel(gm, moving)
		b.Goal = saved
		b.setState("going back to " + at + " to escort the party across")
		return true
	}
	// on the safe point: gather the waiting members, then lead on (they see us within escortGatherCells -> escorted)
	far := 0
	for _, s := range waiting {
		if s.Map != at || dist(b.X, b.Y, s.X, s.Y) > escortGatherCells {
			far++
			if !moving && s.Map == at {
				b.stepAlong(gm, s.X, s.Y)
			}
		}
	}
	if far > 0 {
		b.setState(fmt.Sprintf("gathering %d member(s) at %s to cross together", far, at))
		return true
	}
	b.escLeadAt = time.Time{}
	b.escUntil = now.Add(escortWindow)
	typing := b.P.Typing
	go b.partySay(styled(typing, pick("ok stay close, crossing now", "everyone here, follow me through")))
	return false // travel on to the party map; the members (escorted) follow
}
