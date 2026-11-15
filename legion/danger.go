package main

// Danger sense: bots notice when a fight is going wrong and get out, like a careful player.
//   - losing: low HP while several monsters hit us, or one much stronger than us, or no potions left
//     -> Fly Wing (random teleport on the map), Butterfly Wing back to town when nearly dead, else run away
//   - close calls count toward the map's danger; after a few escapes on one map, move somewhere easier
//   - target choice avoids packs of aggressive monsters when alone

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// attackers: monsters that hit us in the last few seconds, and the strongest one's level.
func (b *Bot) attackers(now time.Time) (n, maxLv int, cx, cy int) {
	for _, e := range b.ents {
		if e.Type == objMob && now.Sub(e.LastHitMe) < 4*time.Second {
			n++
			cx += e.X
			cy += e.Y
			if mb := mobs[e.Class]; mb != nil && mb.Level > maxLv {
				maxLv = mb.Level
			}
		}
	}
	if n > 0 {
		cx, cy = cx/n, cy/n
	}
	return
}

// packAround: aggressive monsters within 4 cells of e (including e).
func (b *Bot) packAround(e *Entity) int {
	n := 0
	for _, o := range b.ents {
		if o.Type != objMob || dist(o.X, o.Y, e.X, e.Y) > 4 {
			continue
		}
		if mb := mobs[o.Class]; mb != nil && mb.Aggressive {
			n++
		}
	}
	return n
}

// hpSample: one HP reading (survive keeps the last few seconds to see how fast HP drops)
type hpSample struct {
	at time.Time
	hp float64
}

// noteHP (b.mu held): record the HP fraction, keeping ~5 s of history.
func (b *Bot) noteHP(hp float64, now time.Time) {
	if n := len(b.hpHist); n > 0 && b.hpHist[n-1].hp == hp && now.Sub(b.hpHist[n-1].at) < time.Second {
		return
	}
	b.hpHist = append(b.hpHist, hpSample{now, hp})
	i := 0
	for i < len(b.hpHist)-1 && now.Sub(b.hpHist[i].at) > 5*time.Second {
		i++
	}
	b.hpHist = b.hpHist[i:]
}

// hpDrop: how much HP (fraction) we lost from the highest reading within the window until now.
func (b *Bot) hpDrop(hp float64, window time.Duration, now time.Time) float64 {
	top := hp
	for _, s := range b.hpHist {
		if now.Sub(s.at) <= window && s.hp > top {
			top = s.hp
		}
	}
	return top - hp
}

// weakFighter: no weapon in hand, or a caster/healer still on the starter Knife (or any non-class weapon): such a
// character loses to monsters at or above its level and should leave rather than fight it out.
func (b *Bot) weakFighter() bool {
	w := b.equipped[0x2]
	if w == nil {
		return b.invReady()
	}
	f := family(b.Class)
	if f != famMage && f != famAcolyte {
		return false
	}
	x := items[w.ItemID]
	return x != nil && !isClassWeapon(f, x)
}

// survive (b.mu held): THE survival check. Runs at the top of think() for every bot in every state where it can take
// damage (hunt, party defend/assist, "fighting off X on the way", regroup, travel, rally, town trip), before anything
// else. QA 2026-11-14 (26 deaths in 5 min): party members "defending vs Willow" and "fighting off X on the way" went
// 88% -> 0 in 12 s with 11 potions and Fly Wings in the bag: potions lived only in hunt(), and escape() fired at <25-35%
// HP, too late against one big hit (a Merchant lv21 teleported at 0% HP from a level 47 monster).
//  1. a potion below the potion line (earlier while HP falls fast)
//  2. escape(): Fly Wing / Butterfly Wing / run when losing - now also on fast HP loss, 2+ attackers at <=30%,
//     a monster far above our level, or a weak fighter facing a monster at its level
//  3. while running away on foot: keep running until nothing hits us
//
// True while it owns the bot this tick.
func (b *Bot) survive(gm *GameMap, moving bool, now time.Time) bool {
	if b.Dead || b.MaxHP <= 0 {
		return false
	}
	hp := b.hpFrac()
	b.noteHP(hp, now)
	if !b.underAttack(now) {
		b.fleeUntil = time.Time{}
		return false
	}
	drop := b.hpDrop(hp, 4*time.Second, now)
	if now.After(b.potAt) && hp < b.potLine(drop) {
		if idx := b.potionIndex(); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
		}
	}
	if now.Before(b.fleeUntil) {
		if !moving {
			if b.fleeTo != [2]int{} {
				x, y := towardOnPath(gm, b.X, b.Y, b.fleeTo[0], b.fleeTo[1], 12)
				go b.walkTo(x, y) // running to the party (escape), a walkable leg at a time
			} else {
				_, _, cx, cy := b.attackers(now)
				b.runFrom(gm, cx, cy)
			}
		}
		return true
	}
	return b.escape(gm, hp, moving, now)
}

// potLine: drink an HP potion under this HP fraction while something hits us.
func (b *Bot) potLine(drop float64) float64 {
	line := max(0.5, 0.25+0.2*(1-b.P.Risk))
	if b.potionCount() > 30 { // plenty of (starter) potions: drink earlier and keep going
		line = 0.6
	}
	if hs := b.healUse(); hs != nil && b.SP >= hs.SP {
		line = 0.3 // the acolyte line heals itself (healFirst); its few potions are for emergencies
	}
	if drop >= 0.2 {
		line = max(line, 0.7) // HP falling fast: one more hit like that and we're done
	}
	return line
}

// runFrom: walk straight away from (cx, cy).
func (b *Bot) runFrom(gm *GameMap, cx, cy int) {
	if gm == nil {
		return
	}
	if x, y, ok := fleeCell(gm, b.X, b.Y, cx, cy); ok {
		go b.walkTo(x, y)
	}
}

// towardOnPath: the cell up to k steps along the walking path from (x0, y0) to (tx, ty) - a walk request the server
// accepts (a straight 40-cell request through dungeon corridors is refused). The target itself when no path is found.
func towardOnPath(gm *GameMap, x0, y0, tx, ty, k int) (int, int) {
	if gm == nil {
		return tx, ty
	}
	p := gm.FindPath(x0, y0, tx, ty, 40000)
	if len(p) == 0 {
		return tx, ty
	}
	c := p[min(k-1, len(p)-1)] // the path starts with the first step
	return c[0], c[1]
}

// fleeCell: a walkable cell 3-10 steps away, in the walk region we stand in, as far from (cx, cy) as possible -
// straight away first, then any other direction. QA 2026-11-14 17:00 UTC (core, pay_dun01): Zen.fox "running away (3
// monsters on me)" walked 4 cells into a dead end, then every straight-away cell was a wall and runFrom sent nothing:
// it stood 12 s under 3 Soldier Skeletons and died.
func fleeCell(gm *GameMap, x0, y0, cx, cy int) (int, int, bool) {
	reg := gm.Region(x0, y0)
	dx, dy := sign(x0-cx), sign(y0-cy)
	if dx == 0 && dy == 0 {
		dx = 1
	}
	bx, by, best := 0, 0, -1
	for _, k := range []int{10, 7, 5, 3} {
		for ddx := -1; ddx <= 1; ddx++ {
			for ddy := -1; ddy <= 1; ddy++ {
				if ddx == 0 && ddy == 0 {
					continue
				}
				x, y := x0+ddx*k, y0+ddy*k
				if !gm.Walkable(x, y) || (reg != 0 && gm.Region(x, y) != reg) {
					continue
				}
				score := dist(x, y, cx, cy)*4 + k
				if ddx == dx && ddy == dy {
					score += 20 // straight away from them is best
				}
				if score > best {
					bx, by, best = x, y, score
				}
			}
		}
	}
	return bx, by, best >= 0
}

// escape: returns true if it took an evasive action this tick.
func (b *Bot) escape(gm *GameMap, hp float64, moving bool, now time.Time) bool {
	b.escCheck(now) // escapeitem.go: did the last Fly / Butterfly Wing work?
	if now.Before(b.escapeAt) {
		return false
	}
	n, maxLv, cx, cy := b.attackers(now)
	if n == 0 {
		return false
	}
	if handled, fled := b.bossEscape(hp, now); handled { // bosshunt.go: hold the line in a boss fight
		return fled
	}
	drop := b.hpDrop(hp, 4*time.Second, now)
	canHeal := false
	if hs := b.healUse(); hs != nil && b.SP >= hs.SP {
		canHeal = true
	}
	fv := fightView{hp: hp, drop: drop, n: n, maxLv: maxLv, lv: b.powerLv(), base: b.BaseLvOr(), pots: b.potionCount(),
		weak: b.weakFighter(), canHeal: canHeal, transit: b.transitSwarm() || b.rejoining(now), tank: partyRole(b) == roleTank} // Monk / dodge Thief too
	losing, why := losingFight(fv)
	if !losing || b.tankStands(fv) { // tanklead.go: a party tank holds (falls back to the priest) until really low
		return false
	}
	even := fv.even()
	beginner := max(fv.lv, fv.base) < beginnerLv()
	if beginner && n < 3 {
		even = true // a beginner's fight with one or two monsters is never danger to the map (no "too dangerous" Kafra hops)
	}
	b.escapeAt = now.Add(4 * time.Second)
	b.escAll = append(b.escAll, now)
	for len(b.escAll) > 0 && now.Sub(b.escAll[0]) > 2*time.Minute {
		b.escAll = b.escAll[1:]
	}
	if !even {
		// QA 2026-11-14 14:05-14:26 UTC: one monster at our own level is no danger to the map: Holy_lord (lv6-8) fled
		// a lv6 monster, three such escapes said "too dangerous", and beginners Kafra-hopped pay_fild01 <-> pay_fild03
		// (123 "too dangerous" from 60 bots in 21 min). Only real danger (2+ attackers, a stronger monster) counts
		// toward the map's danger and the 3-escapes rule.
		b.escapes = append(b.escapes, now)
		reportCloseCall(b.MapName) // a close call teaches the map's danger, a quarter of a death
	}
	b.target, b.path = 0, nil
	if b.mkt != nil { // a fetch / delivery deal is dropped when we have to flee (escapeitem.go)
		b.mkt.d.close("fled from danger")
		b.endMarket(now)
	}
	// QA 2026-11-14: a Fly Wing lands on the same map: on moc_fild03 (Argos everywhere) mochi_rose winged at 30% and
	// died 4 s later. Against a monster 10+ levels above our effective level, or when a Fly Wing already failed in the
	// last 20 s, go back to the save town with a Butterfly Wing.
	// QA 2026-11-14 10:17 UTC (wings work since 05:06): every Butterfly Wing is a town trip (fare back, time, a party
	// split), so it is kept for what a Fly Wing can't fix: a far stronger monster or boss, a swarm / boss map (a random
	// cell there lands in the next pack), a second Fly Wing that landed in a losing fight again while low, or nearly dead
	// with nobody to come back to. Everything else (HP drop, 2+ attackers, weak fighter, a party member) uses a Fly Wing:
	// same map, 60z, and the party regroup brings it back.
	flewRecently := now.Sub(b.buffAt["flywing"]) < 20*time.Second
	lv := b.powerLv()
	farAbove := maxLv > lv+8 && !fv.even() // = losingFight's "outmatched": 8 of 16 Fly Wing escapes from one died within 60 s (10:17-10:35 UTC), 0 of 5 Butterfly Wings
	badMap := swarmMap(b.MapName, min(lv, b.BaseLvOr())) || bossRecent(b.MapName, lv)
	inParty := partyOf(b) != nil
	// escapes in the last 2 minutes, this one included: Fly Wings keep landing in fights (LilLunamage on gef_fild03,
	// 10:20 UTC: 6 Fly Wings among the orcs in 3 min, then dead). b.escAll counts every escape: b.escapes skips even
	// fights (map danger), and the throttle never engaged for TheShadowsenpai (Alchemist 69, gear 51, mjolnir_12,
	// QA 2026-11-14): one lv63-65 monster is "even" by base level, so 15 Fly Wings in 2 min at 84% -> 33% HP.
	again := len(b.escAll)
	town := farAbove || badMap || again >= 3 || (flewRecently && hp < 0.3) || (hp < 0.15 && !inParty)
	if even {
		town = hp < 0.15 && !inParty // one monster at our level: a Fly Wing (or a potion) is enough
	}
	if inParty {
		// QA 2026-11-14: guild03 leader Blaze.mage (Alchemist lv34-38) Butterfly-winged out of 4 mobs at 49% HP on
		// mjolnir_02, respawned in Geffen, walked ~10 min back while its party waited, was swarmed again 26 s after
		// arriving - a Butterfly Wing splits a party for 10+ min. In a party a Fly Wing (same map, the regroup brings it
		// back) is the escape; the save point only when about to die: a far stronger monster at under half HP, a boss,
		// a Fly Wing that just landed in another losing fight while low, or Fly Wings that keep failing.
		town = farAbove && hp < 0.5 || bossRecent(b.MapName, lv) || flewRecently && hp < 0.3 || again >= 4 && hp < 0.35 ||
			hp < 0.15 && !b.escItemOK(itemFlyWing, now)
	}
	// QA 2026-11-14 16:12 UTC (core, pay_dun01): Zen.fox Fly-Winged out of 3 monsters with the party close by, landed
	// ~80 cells away among Drainliars and died 11 s later. With the party's healer (else leader) within 15 cells, run
	// to them (they heal and take the monsters) instead of a Fly Wing to a random cell; nearly dead, wing anyway.
	// QA 2026-11-14 20:20-20:22 UTC: TheShadowsenpai (Alchemist 69, poisoned, one lv47 monster) used 12 Fly Wings in
	// 83 s at 31% -> 20% HP and died; nothing healed it between the wings. Two Fly Wings within 30 s and still under 40%:
	// the Butterfly Wing out. And with only 1-2 monsters on us, drink first (once per 6 s) before burning a wing.
	if now.Sub(b.buffAt["flywing2"]) < 30*time.Second && hp < 0.4 {
		town = true
	}
	if n <= 2 && hp >= 0.15 && hp < 0.35 && !farAbove && now.Sub(b.buffAt["escpot"]) > 6*time.Second {
		if idx := b.potionIndex(); idx >= 0 {
			b.buffAt["escpot"], b.potAt = now, now.Add(time.Second)
			b.escapeAt = now.Add(1500 * time.Millisecond)
			go b.useItem(idx)
			b.setState(fmt.Sprintf("drinking before escaping (%d%%, %s)", int(hp*100), why))
			return true
		}
	}
	// partystick.go PT31: with party mates on this map no Fly Wing / Teleport to a random cell - run to the tank /
	// priest / leader, call for help, Butterfly Wing to the save Kafra only when about to die (announced)
	sHandled, sFled := b.stickEscape(gm, hp, n, farAbove && hp < 0.35 || bossRecent(b.MapName, lv) && hp < 0.35, moving, now, why)
	anchor, ad, aok := b.partyAnchor(now)
	reach := 15
	if strings.Contains(b.MapName, "_dun") {
		// QA 2026-11-14 17:00 UTC (core, pay_dun01, just after a relog): Zen.fox (24 cells from Ayla) and the priest
		// Zendancer84 both Fly-Winged out of the skeletons; Zen.fox landed 180 cells off and died, the priest fought
		// alone 80 cells off. In a dungeon a random cell is worse than the party: run to it from up to 40 cells.
		// QA 2026-11-14 19:41-19:49 UTC (core): beyond 40 cells they still winged - xXRinfangXx 7, Zendancer84 10 Fly
		// Wings in pay_dun00/01, every landing farther from Ayla, then 3 deaths to Archer Skeletons. With the party on
		// this dungeon map: no Fly Wing at any distance unless under 20% HP or a monster far above - run to the party.
		reach = 1 << 30
	}
	minHP := 0.15
	if reach > 40 {
		minHP = 0.2
	}
	toParty := aok && ad <= reach && (ad > 2 || reach > 40) && hp >= minHP && !farAbove
	switch {
	case sHandled:
		// PT31 done above
	case toParty:
		if n >= 3 && now.After(b.buffAt["helpcall"]) {
			b.buffAt["helpcall"] = now.Add(45 * time.Second)
			typing := b.P.Typing
			line := fmt.Sprintf(pick("help! %d on me at %d,%d, coming to u", "%d mobs on me at %d,%d, running to u", "help pls, %d on me (%d,%d)"), n, b.X, b.Y)
			go b.partySay(styled(typing, line))
		}
		if !moving && gm != nil {
			x, y := towardOnPath(gm, b.X, b.Y, anchor.X, anchor.Y, 12)
			go b.walkTo(x, y)
		}
		b.fleeUntil, b.fleeTo = b.escapeAt, [2]int{anchor.X, anchor.Y}
		b.setState("running to " + anchor.Name + " (" + why + ")")
	case town && b.escItemOK(itemButterflyWing, now):
		b.useEscapeItem(itemButterflyWing, now, "losing: "+why)
		b.setState("fled to town (" + why + ")")
		b.remember("fled to town from %s on %s (%s)", why, b.MapName, "Butterfly Wing")
	case b.canSkillTele(now) && (again <= 4 || hp < 0.5) && (again <= 6 || hp < 0.25):
		// teleport.go (owner 2026-11-14: Teleport "to avoid danger"): the Teleport skill (Creamy Card / Acolyte) goes
		// before a Fly Wing - same rules, no item spent
		b.buffAt["flywing2"], b.buffAt["flywing"] = b.buffAt["flywing"], now
		b.skillTele(now, "losing: "+why)
		b.event("wing", "", "Teleport skill: losing: "+why)
		b.setState("teleported away (" + why + ")")
		b.remember("teleported away from %s on %s", why, b.MapName)
	case b.escItemOK(itemFlyWing, now) && (again <= 4 || hp < 0.5) && (again <= 6 || hp < 0.25):
		// QA 2026-11-14 16:13-16:16 UTC: Sunheart (Monk, out of potions and SP, on its way to town) used a Fly Wing every
		// 5 s for 3 min at 25-33% HP against one lv47 monster on mjolnir_10. After 6 escapes in 2 min: only under 25%.
		// QA 2026-11-14 11:11-11:14 UTC: Kimchi07 (lv39, no Butterfly Wing left, party on a lv59 map) used 30 Fly Wings
		// in 3 min at 50-86% HP, every one leading back to the same fight. After 4 escapes in 2 min, fly only when low.
		b.buffAt["flywing2"], b.buffAt["flywing"] = b.buffAt["flywing"], now
		b.useEscapeItem(itemFlyWing, now, "losing: "+why)
		b.setState("teleported away (" + why + ")")
		b.remember("teleported away from %s on %s", why, b.MapName)
	case (hp < 0.3 || hp < 0.5 && !now.After(b.buffAt[escFailKey(itemFlyWing)])) && b.escItemOK(itemButterflyWing, now):
		// no Fly Wing left, or it just did nothing (escapeitem.go escFailed): town is better than dying
		b.useEscapeItem(itemButterflyWing, now, "losing, no Fly Wing: "+why)
		b.setState("fled to town (" + why + ")")
		b.remember("barely escaped %s on %s (%s)", why, b.MapName, "Butterfly Wing")
	default:
		// run directly away from the attackers, and keep running while they still hit us (survive)
		if !moving {
			b.runFrom(gm, cx, cy)
		}
		b.fleeUntil, b.fleeTo = b.escapeAt, [2]int{}
		b.setState("running away (" + why + ")")
		if inParty && n >= 3 && now.After(b.buffAt["helpcall"]) {
			// QA 2026-11-14 17:01-17:03 UTC (core): the priest ran from 8 monsters on pay_dun00 for 2+ min and said
			// nothing; the party only learns about it from party chat (owner: members ask for help)
			b.buffAt["helpcall"] = now.Add(45 * time.Second)
			typing := b.P.Typing
			line := fmt.Sprintf(pick("help! %d mobs on me, %s %d,%d", "need help, %d on me at %s %d,%d", "help pls, %d mobs chasing me (%s %d,%d)"), n, b.MapName, b.X, b.Y)
			go b.partySay(styled(typing, line))
		}
	}
	// three close calls here in ten minutes: this map is too much for now, find an easier one
	recent := 0
	for _, t := range b.escapes {
		if now.Sub(t) < 10*time.Minute {
			recent++
		}
	}
	if len(b.escapes) > 20 {
		b.escapes = b.escapes[len(b.escapes)-20:]
	}
	if recent >= 3 && b.Goal.Set == "auto" && !beginner { // beginners stay near home (owner rule): fight, pot, sit
		b.Goal = Goal{}
		b.escapes = nil
		// QA 2026-11-14: the next pick (level only) often chose the same map or one like it. The map is now off
		// the list for 30 minutes, and every pick rates the EFFECTIVE level (gearpower.go powerLv)
		b.badMap, b.badMapUntil = b.MapName, now.Add(30*time.Minute)
		b.event("info", "", "too dangerous on "+b.MapName+", looking for an easier map")
	} else if p := partyOf(b); recent >= 3 && p != nil && p.Human == "" && b.MapName == p.Map {
		// a bot party member (or its leader) keeps fleeing on the party map (QA 2026-11-14: Kimchi07, lv39 in a party
		// on a lv59 map, 30 Fly Wings in 3 min; the party stayed): the party moves to a map its weakest member farms
		b.escapes = nil
		b.event("party", "", "too dangerous for me on "+b.MapName+": asking the party to move to an easier map")
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("this map is too much for me, can we go somewhere easier?", "i keep running away here, lets move pls")))
		leaderNoteBad(partyLeaderName(b), b.MapName, leaderFleeBan) // party.go: survives a re-formed party
		go p.relocate(b.MapName, b.X, b.Y, b.charName()+" keeps fleeing")
	}
	if sHandled {
		return sFled // next to the party mate: fight back here
	}
	return true
}

// fightView: what escape() looks at to decide whether a fight is lost.
type fightView struct {
	hp, drop      float64 // HP fraction now, HP fraction lost within the last 4 s
	n, maxLv, lv  int     // attackers, the strongest attacker's level, our effective (gear-rated) level
	base          int     // our base level
	pots          int     // HP potions carried
	weak, canHeal bool    // weakFighter(), Heal ready with the SP for it
	transit       bool    // only passing through a swarm map (swarm.go): 3+ attackers = leave, whatever the HP
	tank          bool    // Swordman line: high HP and defence, it is meant to take hits
}

// even: one attacker no more than 2 levels above our base (or effective) level - a normal fight, not danger.
func (v fightView) even() bool { return v.n == 1 && v.maxLv <= max(v.lv, v.base)+2 }

// losingFight: true (and why, for the state line) when the bot should teleport / run now.
func losingFight(v fightView) (bool, string) {
	hp, n, lv, maxLv := v.hp, v.n, v.lv, v.maxLv
	outmatched := maxLv > lv+8
	if v.even() {
		outmatched = false // one monster at our base level: a weak gear rating alone doesn't make it a lost fight (TheShadowsenpai left at 84%)
	}
	mobbed := n >= 3 || (n >= 2 && hp < 0.5)
	losing := (hp < 0.35 && (mobbed || outmatched || v.pots == 0)) || (hp < 0.2 && v.pots < 3) || (hp < 0.5 && n >= 4) ||
		(hp < 0.3 && maxLv > lv+3) || (lv < 20 && hp < 0.25) // QA 2026-11-14: low-level bots died 7-8 times in 6 min
	// QA 2026-11-14 death probe: the checks above fired too late against big hits
	fast := v.drop >= 0.2 && hp < 0.7                          // HP falling 20%+ within 4 s: a few more seconds of this and we're dead
	if v.pots >= 5 && v.drop < 0.35 && v.lv >= 20 && !v.weak { // low levels: tiny HP, weak potions (lord86 died at 11 pots)
		// QA 2026-11-14 10:17 UTC, wings working: "lost 2x% HP in seconds" was the top Fly Wing trigger. With potions the
		// potion line rises to 70% on such a drop (potLine), so drink first and leave at 50% (a tank at 40%); one
		// 35%+ hit still leaves at once.
		fast = v.drop >= 0.2 && hp < 0.5
		if v.tank {
			fast = v.drop >= 0.2 && hp < 0.4
		}
	}
	if v.canHeal {
		fast = v.drop >= 0.2 && hp < 0.45 // a healer with SP heals itself first (healFirst); a party priest must not wing off
	}
	weakLine := 0.6
	if v.canHeal {
		weakLine = 0.35 // it can still heal itself (healFirst)
	}
	weak := v.weak && maxLv >= lv-2 && hp < weakLine
	// a weaponless / Knife-caster bot can't win even fights: it still leaves. From lv20 the tuned lines above apply
	// (a lv56 priest at 40% losing 25% per 4 s must still go); even fights there only stop counting as map danger.
	calm := v.even() && v.pots >= 3 && !v.weak && max(v.lv, v.base) < 20
	// QA 2026-11-14 (38 A->B->A Kafra hops in 19 min): beginners lost 35-47% of a tiny HP pool to ONE lv8 Fabre/Willow
	// ("lv 8 vs 1": the gear-rated level of a starter kit is 1) and winged out. Under lv20, one monster up to 10 levels
	// over us is a normal beginner fight: potions, not wings.
	calm = calm || v.n == 1 && v.pots >= 3 && !v.weak && max(v.lv, v.base) < 20 && v.maxLv <= max(v.lv, v.base)+10
	if calm {
		// one monster at our level and potions in the bag: drink and fight (survive's potion line rises to 70% on a fast
		// drop); only the last-resort lines above (under 20-25%) still leave. QA 2026-11-14 14:05-14:26 UTC: lv6-8
		// beginners fled lv6 monsters at 31% after "lost 35% HP" and ping-ponged between home fields by Kafra.
		fast, weak, outmatched = false, false, false
	}
	losing = losing || fast || (n >= 2 && hp <= 0.3) || (outmatched && hp < 0.9) || weak || (hp < 0.4 && v.drop >= 0.15 && !calm)
	// a swarm map in transit: 11 Spores hit a lv38 within 35 s. QA 2026-11-14 16:14 UTC: LilLilyblade (Monk 69) flew at
	// 68% HP from 2 Spores 18 levels under it, 6 times in 2 min: 2 attackers need low HP or a fast drop.
	// QA 2026-11-14 19:41-19:49 UTC (core, pay_dun01): lv72-77 friends ran from lv30s skeletons (the rejoin transit rule:
	// 3+ attackers = leave), got chased by trains, winged, scattered and died. Monsters 25+ levels under us are trash:
	// the transit rule is for packs that can hurt us (100 lv26 Spores vs a lv38-48 bot); trash is fought.
	trash := maxLv > 0 && maxLv <= max(lv, v.base)-25
	losing = losing || (v.transit && !trash && (n >= 3 || (n >= 2 && (hp < 0.5 || v.drop >= 0.15))))
	if !losing || n == 0 {
		return false, ""
	}
	switch {
	case outmatched:
		return true, fmt.Sprintf("a level %d monster on me", maxLv)
	case fast:
		return true, fmt.Sprintf("lost %d%% HP in seconds", int(v.drop*100))
	case weak:
		return true, fmt.Sprintf("too strong for me (level %d)", maxLv)
	}
	return true, fmt.Sprintf("%d monsters on me", n)
}

// close calls and a 10-minute window of real deaths per map (humanparty.go's checkpoint hold)
var deathTimes = struct {
	sync.Mutex
	m map[string][]time.Time
}{m: map[string][]time.Time{}}

// reportCloseCall: an escape adds a quarter of a death to the map's decaying danger (it counted as a full death: a few
// Fly Wings in a busy dungeon made the map look deadly and held a human's party members in town).
func reportCloseCall(m string) {
	deathLog.Lock()
	defer deathLog.Unlock()
	deathLog.m[m] = recentDeathsLocked(m) + 0.25
	deathLog.t[m] = time.Now()
}

// noteDeathAt: a real death on map m (for deathsWithin).
func noteDeathAt(m string) {
	deathTimes.Lock()
	defer deathTimes.Unlock()
	now := time.Now()
	ts := append(deathTimes.m[m], now)
	i := 0
	for i < len(ts) && now.Sub(ts[i]) > 30*time.Minute {
		i++
	}
	deathTimes.m[m] = ts[i:]
}

// deathsWithin: real deaths on map m in the last d.
func deathsWithin(m string, d time.Duration) int {
	deathTimes.Lock()
	defer deathTimes.Unlock()
	n := 0
	for _, t := range deathTimes.m[m] {
		if time.Since(t) <= d {
			n++
		}
	}
	return n
}

// fightBackOnWay (b.mu held): a member walking to its party (travel / regroup) with a train on it turns and fights
// instead of walking on (ai.go, party.go regroupWalkOn): a tank with 3+ attackers (owner: tanks fight crowds with
// AoE), anyone with 4+. QA 2026-11-14 (build agent): a lv65 Crusader was mobbed in mjolnir_02 while travelling and
// never fought back. losingFight still decides when it must leave instead.
func (b *Bot) fightBackOnWay(now time.Time) bool {
	n, _, _, _ := b.attackers(now)
	return n >= 4 || n >= 3 && partyRole(b) == roleTank
}
