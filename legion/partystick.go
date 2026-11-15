package main

// partystick.go - the party stays together (PT04 "stay close while grinding", PT06 "together = within 20 cells of the
// leader", PT31 "no solo random teleport escapes; retreat to the tank / priest, ask for help, Butterfly Wing to the
// party's save Kafra only when about to die"; owner 2026-11-14: "staying close to the party is rule number one").
//
// One invariant for every grinding move of a party member (and of a bot party's leader):
//   - walkTo (bot.go) clamps any walk that would take the bot beyond together_cells of its party to the part of the way
//     that keeps it within; a leader stays within together_cells of every member near it (it slows down / waits
//     instead of running ahead). A member already beyond is party.go's regroup (it may detour round walls), a lost
//     member the leader's collect.
//   - target and loot choice (ai.go hunt) skip monsters / drops beyond together_cells of the party - except what is
//     hitting us (a member fights only what hits it).
//   - escape (danger.go): with party mates on the map no Fly Wing / Teleport skill to a random cell: run to the party
//     (tank / priest / leader), drink, call for help; Butterfly Wing only when about to die, announced in party chat.
// "Grinding" = in a party, on a non-town map, not travelling to another map and not on an errand / town trip / Kafra
// walk / rally / market deal / escorted crossing. Flags: party_stick_on, party_stick_escape_on (tuning.json, default ON).

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// counters (for tests and the log): walks clamped, runs to the party instead of a random teleport, announced wings,
// near-death holds (drink / ask the healer / stay with the party instead of a Butterfly Wing)
var stickVetoes, stickRuns, stickWings, stickHolds atomic.Int64

// stickHealer (b.mu held): the party's healer on this map within reach (40 cells; any distance in a dungeon, where
// a wing to town is the worst split), with its live position. A healer bot counts itself (it can Heal itself).
// ok=false: no healer of the party here.
func (b *Bot) stickHealer(now time.Time) (pubState, int, bool) {
	p := partyOf(b)
	if p == nil {
		return pubState{}, 0, false
	}
	me := b.charName()
	if b.isHealer() {
		return pubState{Name: me, Map: b.MapName, X: b.X, Y: b.Y, Online: true, Support: true}, 0, true
	}
	reach := 40
	if strings.Contains(b.MapName, "_dun") {
		reach = 1 << 30
	}
	_, _, members := b.partyView(p, now)
	var best pubState
	bd, found := 1<<30, false
	for _, s := range members {
		if s.Name == me || !s.Online || !s.Support || s.Map != b.MapName || s.X|s.Y == 0 || s.X >= 1000 || s.Y >= 1000 {
			continue
		}
		if s.LX|s.LY != 0 {
			s.X, s.Y = s.LX, s.LY
		}
		if d := dist(b.X, b.Y, s.X, s.Y); d <= reach && d < bd {
			best, bd, found = s, d, true
		}
	}
	return best, bd, found
}

// stickRefs (b.mu held): the party positions a grinding move of this bot must stay near - for a member its leader (a
// human party's leader too), for a bot party's leader every online member on its map. ok=false: no limit right now.
func (b *Bot) stickRefs(now time.Time) (refs [][2]int, ok bool) {
	if !tune().PartyStickOn || b.Dead {
		return nil, false
	}
	p := partyOf(b)
	if p == nil || isTown(b.MapName) || b.MapName == "" {
		return nil, false
	}
	if b.Goal.Map != "" && b.Goal.Map != b.MapName {
		return nil, false // travelling to another map: the route decides
	}
	if b.regrouping || b.collectName != "" {
		return nil, false // a member walking back / a leader fetching a straggler may detour (party.go owns that walk)
	}
	if b.trip != nil || b.errand != nil || b.kafra != nil || b.mkt != nil || b.npcDeal != nil || b.reset != nil ||
		now.Before(b.escUntil) || !b.rallyID.IsZero() && now.Sub(b.rallyID) < 20*time.Minute {
		return nil, false // PT22-PT24 town runs / errands / Kafra walks / rallies go on their own rules
	}
	lead, lok, members := b.partyView(p, now)
	me := b.charName()
	if lok && lead.Name != me {
		if !lead.Online || lead.Map != b.MapName || lead.X|lead.Y == 0 {
			return nil, false // the leader isn't here: travel / regroup logic brings us together
		}
		lx, ly := livePosOf(lead)
		if mx, my := b.livePos(now); dist(mx, my, lx, ly) > togetherCells() {
			return nil, false // already apart: party.go regroups first (fighting only what hits it), round walls too
		}
		return [][2]int{{lx, ly}}, true
	}
	if p.Leader != b {
		return nil, false
	}
	for _, s := range members {
		if s.Name == me || !s.Online || s.Map != b.MapName || s.X|s.Y == 0 || s.Trip || s.Errand {
			continue // a member off on a town trip / errand (PT23/PT24) doesn't hold the leader
		}
		sx, sy := livePosOf(s)
		if mx, my := b.livePos(now); dist(mx, my, sx, sy) > togetherCells() {
			continue // a lost member: the leader's collect (ai.go) fetches it; it doesn't freeze the leader
		}
		refs = append(refs, [2]int{sx, sy})
	}
	return refs, len(refs) > 0
}

// stickAllows: a move from (fx,fy) to (x,y) keeps the party together - within t of every reference, or (for a
// reference already beyond t) no farther from it than now.
func stickAllows(fx, fy, x, y int, refs [][2]int, t int) bool {
	for _, r := range refs {
		dn := dist(x, y, r[0], r[1])
		if dn <= t || dn <= dist(fx, fy, r[0], r[1]) {
			continue
		}
		return false
	}
	return true
}

// stickClamp: the farthest point on the straight way from (fx,fy) toward (x,y) that stickAllows (and is walkable).
// ok=false: no step that way keeps the party together.
func stickClamp(gm *GameMap, fx, fy, x, y int, refs [][2]int, t int) (int, int, bool) {
	n := dist(fx, fy, x, y)
	for k := n - 1; k >= 1; k-- {
		cx, cy := fx+(x-fx)*k/n, fy+(y-fy)*k/n
		if !stickAllows(fx, fy, cx, cy, refs, t) {
			continue
		}
		if gm != nil && !gm.Walkable(cx, cy) {
			continue
		}
		return cx, cy, true
	}
	return 0, 0, false
}

// stickDest (walkTo, b.mu NOT held): the walk target the party invariant allows. Short side-steps (<= 3 cells, PT19:
// getting unstuck) and runs to the party (danger.go fleeTo) always pass.
func (b *Bot) stickDest(x, y int) (int, int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if now.Before(b.fleeUntil) && b.fleeTo != [2]int{} {
		return x, y, true
	}
	refs, ok := b.stickRefs(now)
	if !ok {
		return x, y, true
	}
	fx, fy := b.livePos(now)
	if dist(fx, fy, x, y) <= 3 {
		return x, y, true
	}
	t := togetherCells()
	if stickAllows(fx, fy, x, y, refs, t) {
		return x, y, true
	}
	cx, cy, ok := stickClamp(maps[b.MapName], fx, fy, x, y, refs, t)
	stickVetoes.Add(1)
	if b.buffAt != nil && now.After(b.buffAt["stickveto"]) {
		b.buffAt["stickveto"] = now.Add(time.Minute)
		b.event("party", "", fmt.Sprintf("party: kept the walk to %d,%d within %d cells of the party (now %d,%d)", x, y, t, fx, fy))
	}
	if !ok {
		return 0, 0, false
	}
	return cx, cy, true
}

// stickNear (b.mu held, hunt): a target / drop at (x,y) keeps the party together (true when no limit applies).
func (b *Bot) stickNear(x, y int, now time.Time) bool {
	refs, ok := b.stickRefs(now)
	if !ok {
		return true
	}
	fx, fy := b.livePos(now)
	return stickAllows(fx, fy, x, y, refs, togetherCells())
}

// stickMate (b.mu held, escape): the party mate to run to in danger - partyAnchor (healer / tank / leader on this map),
// for a leader the nearest online member on its map. ok=false: nobody of the party on this map (solo rules apply).
func (b *Bot) stickMate(now time.Time) (pubState, int, bool) {
	if a, d, ok := b.partyAnchor(now); ok {
		return a, d, true
	}
	p := partyOf(b)
	if p == nil {
		return pubState{}, 0, false
	}
	_, _, members := b.partyView(p, now)
	me := b.charName()
	var best pubState
	bd, found := 1<<30, false
	for _, s := range members {
		if s.Name == me || !s.Online || s.Map != b.MapName || s.X|s.Y == 0 || s.X >= 1000 || s.Y >= 1000 {
			continue
		}
		sx, sy := livePosOf(s)
		if d := dist(b.X, b.Y, sx, sy); d < bd {
			best, bd, found = s, d, true
			best.X, best.Y = sx, sy
		}
	}
	return best, bd, found
}

// stickEscape (b.mu held, escape / boss flee / boss hunt): PT31 for a bot with party mates on its map. handled=false:
// no party mate here (or the flag is off) - the caller's solo rules apply. fled: it took an evasive action this tick.
// deadly: about to die (the caller's own judgement, e.g. a far stronger monster at low HP).
func (b *Bot) stickEscape(gm *GameMap, hp float64, n int, deadly, moving bool, now time.Time, why string) (handled, fled bool) {
	if !tune().PartyStickEscapeOn || partyOf(b) == nil {
		return false, false
	}
	mate, d, ok := b.stickMate(now)
	if !ok {
		return false, false
	}
	if hp < 0.2 || deadly {
		// QA 12:37:37 (bug b1794746339362-336): four of Ayla's six members (Zen.fox, xXRinfangXx, Echo.0783,
		// Silversage13) Butterfly-winged to Payon at 10-28% HP with the priest on their map and sat there while the leader
		// farmed pay_dun03 alone. Real players with a priest next to them back off to it, drink, shout for a heal and
		// keep fighting with the party (PT29, PT31, PT04). The wing-out is only for a party bot with no healer on its
		// map and no potion left.
		healer, _, hok := b.stickHealer(now)
		pot := b.potionIndex()
		if hok || pot >= 0 {
			if pot >= 0 && now.After(b.potAt) {
				b.potAt = now.Add(time.Second)
				go b.useItem(pot)
			}
			if hok && healer.Name != b.charName() && now.After(b.buffAt["healcall"]) {
				b.buffAt["healcall"] = now.Add(20 * time.Second)
				typing, first := b.P.Typing, healer.Name
				line := pick("heal pls", "heal!! im low", "need heal", "heal me pls "+first, first+" heal pls", "low hp, heal?")
				go b.partySay(styled(typing, line))
			}
			if hok && healer.Name != b.charName() {
				mate, d = healer, dist(b.X, b.Y, healer.X, healer.Y)
			}
			stickHolds.Add(1)
		} else if b.escItemOK(itemButterflyWing, now) {
			// about to die, no healer here and nothing to drink: Butterfly Wing to the save Kafra and say so
			town := b.savedTown
			if town == "" {
				town = "town"
			}
			b.useEscapeItem(itemButterflyWing, now, "about to die: "+why)
			b.setState("winged to " + town + " (" + why + ")")
			b.remember("winged out of %s on %s, about to die", why, b.MapName)
			if now.After(b.buffAt["wingsay"]) {
				b.buffAt["wingsay"] = now.Add(time.Minute)
				typing, m := b.P.Typing, b.MapName
				go b.partySay(styled(typing, fmt.Sprintf(pick("almost died, winged to %s kafra, coming back to %s", "winged to %s kafra (nearly dead), meet me there or ill come back to %s", "had to wing to %s kafra, omw back to %s"), town, m)))
			}
			stickWings.Add(1)
			return true, true
		}
	}
	if n >= 2 && now.After(b.buffAt["helpcall"]) {
		b.buffAt["helpcall"] = now.Add(45 * time.Second)
		typing := b.P.Typing
		line := fmt.Sprintf(pick("help, %d on me", "help! %d on me, coming to u", "%d mobs on me, need help"), n)
		go b.partySay(styled(typing, line))
	}
	if d <= 2 {
		return true, false // next to the mate: fight back here (potions, the priest heals, the tank takes them)
	}
	if !moving && gm != nil {
		x, y := towardOnPath(gm, b.X, b.Y, mate.X, mate.Y, 12)
		go b.walkTo(x, y)
	}
	b.fleeUntil, b.fleeTo = now.Add(4*time.Second), [2]int{mate.X, mate.Y}
	b.setState("running to " + mate.Name + " (" + why + ")")
	stickRuns.Add(1)
	return true, true
}

// stickBoss (b.mu held, bossnote.go boss in sight): with party mates on the map nobody wings off alone - the party pulls
// back together: the leader announces it and walks away from the boss (the members follow it), a member says so and
// runs to its tank / priest / leader. false: no party mate here (solo rules).
func (b *Bot) stickBoss(gm *GameMap, near *Entity, nx, ny int, moving bool, now time.Time, why string) bool {
	p := partyOf(b)
	if p == nil || !tune().PartyStickEscapeOn {
		return false
	}
	if _, _, ok := b.stickMate(now); !ok {
		return false
	}
	if now.Sub(b.buffAt["bossparty"]) > 30*time.Second {
		b.buffAt["bossparty"] = now
		typing, mob := b.P.Typing, mobName(near.Class)
		line := pick(mob+" here! pull back with me", mob+" coming, everyone back off this way", "careful, "+mob+"! fall back together")
		if p.Leader != b {
			line = pick(mob+" here! coming to u", "theres a "+mob+", falling back to u", mob+" near me, pulling back")
		}
		go b.partySay(styled(typing, line))
	}
	if p.Leader == b {
		if !moving && gm != nil {
			b.runFrom(gm, nx, ny) // walkTo keeps it within together_cells of the members: the party backs off as one
		}
		b.setState("pulling the party back (" + why + ")")
		return true
	}
	h, _ := b.stickEscape(gm, b.hpFrac(), 0, false, moving, now, why)
	return h
}
