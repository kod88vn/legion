package main

// trap.go: the trapper Hunter (build variant hunter_trap, run 6; owner 2026-11-14: "the builds of the legions still
// lack variety", CLASSES.md lists "Hunter: trapper"). iRO Wiki Hunter / Renewal trap builds: Claymore Trap (fire, 5x5
// splash, 2 Traps) and Land Mine (earth, 1 Trap) are laid where the monster will step - Renewal trap damage grows
// with DEX, INT and base level, not with the bow, so a DEX/INT trapper kills packs with traps and uses Double Strafe
// in between.
//
// Use (trapCast, in fight() after kiteStep): a monster walking at us is trapped on its walk's end cell (the cell next
// to us it is heading for), within the trap's range 3, when it is worth a trap: 2+ monsters around the cell, or a
// strong / tough one (strongMob, HP >= 20 x our level). One trap per 2.5 s; never with fewer than the cast's Traps.
// Anyone with a trap skill and Traps in the bag does this (a main-plan Hunter that learned them too).
//
// Supply: a Trap (1065, 50z, weight 2) is sold only by a few Tool Dealers (Izlude, Prontera's south gate Goods
// Merchant, Hugel...). Every shop visit that lists Traps tops a trapper up (town.go buy list, trapsWanted); a trapper
// with fewer than trapLow left and the zeny for trapTripMin makes a supply trip to the nearest seller (solo: a town
// trip; in a party: an errand, RULES.md 3 "buying its class weapon or arrows"), at most every 30 min.

import (
	"fmt"
	"time"
)

const (
	htLandMine    = 116
	htBlastMine   = 122
	htClaymore    = 123
	trapLow       = 10  // a supply trip below this
	trapTripMin   = 30  // ... only when it can buy at least this many
	trapReserve   = 800 // zeny kept back (potions first)
	trapPrice     = 50
	trapEveryMs   = 2500
	trapTripEvery = 30 * time.Minute
)

// trapper (b.mu held): this character's plan is the trap Hunter (it buys Traps and makes supply trips).
func (b *Bot) trapper() bool {
	p := b.plan()
	if p == nil || p.ID != "hunter_trap" || family(b.Class) != famArcher {
		return false
	}
	return b.trapSkill() != 0
}

// trapSkill (b.mu held): the best damage trap learned (Claymore > Land Mine), 0 = none.
func (b *Bot) trapSkill() int {
	for _, id := range []int{htClaymore, htLandMine} {
		if si := b.skills[id]; si != nil && si.Lv > 0 {
			return id
		}
	}
	return 0
}

// trapCost: Traps one cast uses (rAthena re/skill_db.yml ItemCost: Claymore 2, Land Mine 1).
func trapCost(id int) int {
	if id == htClaymore {
		return 2
	}
	return 1
}

// trapTarget: Traps a trapper keeps (one per kill or two for a while: ~an hour of trapping), within the weight room.
func (b *Bot) trapTarget() int { return min(120, 40+b.BaseLvOr()) }

// trapsWanted (b.mu held): Traps to buy at this price now (0 = none: not a trapper, stocked, poor or no room).
func (b *Bot) trapsWanted(price, budget int) int {
	if !b.trapper() || price <= 0 {
		return 0
	}
	have := b.countItem(itemTrap)
	need := b.trapTarget() - have
	if need < b.trapTarget()/3 {
		return 0
	}
	return max(0, min(need, (budget-trapReserve)/price, b.potRoom(itemTrap)))
}

// trapCast (b.mu held, from fight): lay a trap where the target will step; true = sent.
func (b *Bot) trapCast(e *Entity, now time.Time) bool {
	if b.MaxSP <= 0 || b.Sitting || now.Before(b.skillReady) || now.Before(b.buffAt["trap"]) || family(b.Class) != famArcher {
		return false
	}
	id := b.trapSkill()
	if id == 0 && b.skills[htBlastMine] != nil && b.skills[htBlastMine].Lv > 0 {
		id = htBlastMine
	}
	if id == 0 || now.Before(b.skillFailUntil[id]) || b.countItem(itemTrap) < trapCost(id) {
		return false
	}
	si := b.skills[id]
	if b.SP-max(si.SP, 10) < b.MaxSP*skillSPFloor/100+b.keyReserve(now) {
		return false
	}
	mb := mobs[e.Class]
	if mb == nil || mb.worthless() {
		return false
	}
	// where it is heading: its walk's end, when that end is next to us (it comes for us); a standing monster gets the
	// trap under it only when it is already next to us (it will swing from there and step back onto it as it chases)
	cx, cy := e.DX, e.DY
	ex, ey := e.pos(now)
	if cx == ex && cy == ey && dist(b.X, b.Y, ex, ey) > 1 {
		return false
	}
	if dist(b.X, b.Y, cx, cy) > 3 {
		return false // the trap's range
	}
	if cx == b.X && cy == b.Y {
		return false // never under ourselves (NoFootSet)
	}
	n := 0
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		if ox, oy := o.pos(now); dist(ox, oy, cx, cy) <= 3 {
			n++
		}
	}
	if !(n >= 2 || strongMob(mb, b.BaseLvOr()) || mb.HP >= 20*b.BaseLvOr()) {
		return false // one weak monster: a Double Strafe or two, keep the Traps
	}
	b.buffAt["trap"] = now.Add(trapEveryMs * time.Millisecond)
	b.skillReady = now.Add(time.Duration(1100+b.P.ReactionMs/2) * time.Millisecond) // 0.3 s fixed cast + 1 s delay
	b.targetAt, b.attackSentFor = now, 0
	b.setState(skillName(id) + " for " + mobName(e.Class))
	go b.send(newPkt(0x0366).u16(uint16(si.Lv)).u16(uint16(id)).u16(uint16(cx)).u16(uint16(cy)).b) // UseSkillToPos
	return true
}

// trapShop (b.mu held): the Trap seller fewest warps away.
func (b *Bot) trapShop() *Shop {
	var best *Shop
	bl := 999
	for _, s := range gearShops(itemTrap) {
		hops := 0
		if s.Map != b.MapName {
			r := RouteMaps(s.Map, s.X, s.Y, b.MapName)
			if r == nil {
				continue
			}
			hops = len(r)
		}
		if hops < bl || hops == bl && s.Map == "prontera" {
			best, bl = s, hops
		}
	}
	return best
}

// trapTripDue (b.mu held): a trapper low on Traps that can afford a real restock, at most every 30 min.
func (b *Bot) trapTripDue(now time.Time) bool {
	return b.trapper() && b.countItem(itemTrap) < trapLow && now.After(b.buffAt["trap:trip"]) &&
		(b.Zeny-trapReserve)/trapPrice >= trapTripMin && b.potRoom(itemTrap) >= trapTripMin
}

// startTrapTrip (b.mu held): the supply trip to the nearest Trap seller; false = none started.
func (b *Bot) startTrapTrip(now time.Time) bool {
	b.buffAt["trap:trip"] = now.Add(trapTripEvery)
	s := b.trapShop()
	if s == nil {
		return false
	}
	town := townOf(s.Map)
	if town == "" {
		town = nearestTown(s.Map)
	}
	b.trip = &Trip{Stage: "go", Town: town, Shop: s, Started: now, At: now, Why: "traps"}
	b.target, b.path, b.route = 0, nil, nil
	b.event("info", "", fmt.Sprintf("heading to %s for Traps (%d left, %dz)", s.Map, b.countItem(itemTrap), b.Zeny))
	return true
}

// trapThink (b.mu held, from think): a solo trapper's supply trip (a partied one goes on an errand, errand.go).
func (b *Bot) trapThink(now time.Time) {
	if b.trip != nil || b.errand != nil || partyOf(b) != nil || b.target != 0 || !b.trapTripDue(now) {
		return
	}
	b.startTrapTrip(now)
}

// trapBuy answers a Trap seller's buy list on a "traps" trip (b.mu held; town.go onDeal).
func (b *Bot) trapBuy(pk []byte) {
	if pr := listedPrice(pk, itemTrap); pr > 0 {
		if n := b.trapsWanted(pr, b.Zeny); n > 0 {
			go b.send(newPkt(0x00c8).u16(0).u16(uint16(n)).u32(itemTrap).varlen())
			b.event("info", "", fmt.Sprintf("bought %d Traps (%dz)", n, n*pr))
		} else {
			go b.send(newPkt(0x09d4).b)
		}
	} else {
		go b.send(newPkt(0x09d4).b) // close the shop window (town.go: sd->npc_shopid freezes walking)
		b.event("info", "", "this shop sells no Traps")
	}
	if t := b.trip; t != nil {
		t.Stage, t.At = "done", time.Now()
	}
	b.npcDeal = nil
}
