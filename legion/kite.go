package main

// kite.go: hit-and-run for bow users (owner 2026-11-14, RULES.md 3b: "Guild leaders need dedicated farmers with high
// individual survivability ... DS hunters are the best hunters with hit and run"). iRO Wiki Hunter / DS leveling: shoot
// (Double Strafe) from range, and when a melee monster reaches you, step back out of its reach before shooting again;
// Ankle Snare roots a chaser (needs a Trap item); the falcon's Blitz Beat procs on its own.
//
// kiteStep runs early in fight() for a bow user (Archer line, bow Rogue): when the target or another monster that hit us
// in the last 2 s stands within 2 cells, it walks 4-6 cells straight away from them (danger.go fleeCell: same walk
// region, any free direction when cornered) and lays an Ankle Snare on the closest chaser when it carries a Trap.
// Limits: one step per 1.2 s; not with 3+ monsters on us (danger.go's escape owns that), not when a party tank holds
// the monster within 3 cells of us (the tank does its job; stepping away would drag the monster off it), not rooted /
// sitting, and never against a monster that doesn't move (plants: their Speed 0 never closes in - we only react to
// hits from adjacent cells, which a plant can't deal).

import (
	"time"
)

const (
	htAnkleSnare = 117
	itemTrap     = 1065
	kiteEvery    = 1200 * time.Millisecond
)

// kiter (b.mu held): this character fights with a bow and kites.
func (b *Bot) kiter() bool {
	return b.weaponSub() == "Bow" && !b.noArrows() && (family(b.Class) == famArcher || family(b.Class) == famThief)
}

// kiteStep (b.mu held, from fight): step back from melee range; true = it moved (or laid a trap) this tick.
func (b *Bot) kiteStep(e *Entity, moving bool, now time.Time) bool {
	if !b.kiter() || b.Sitting || moving || now.Before(b.buffAt["kite"]) || b.hpFrac() <= 0 {
		return false
	}
	gm := maps[b.MapName]
	if gm == nil {
		return false
	}
	n, _, _, _ := b.attackers(now)
	if n >= 3 {
		return false // swarmed: danger.go escape
	}
	// who is on us: the target, or anything that hit us lately, within 2 cells
	var close []*Entity
	cx, cy := 0, 0
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		ox, oy := o.pos(now)
		if dist(b.X, b.Y, ox, oy) > 2 {
			continue
		}
		if o.ID == e.ID || now.Sub(o.LastHitMe) < 2*time.Second {
			if mb := mobs[o.Class]; mb != nil && mb.worthless() {
				continue
			}
			close = append(close, o)
			cx, cy = cx+ox, cy+oy
		}
	}
	if len(close) == 0 {
		return false
	}
	cx, cy = cx/len(close), cy/len(close)
	if b.tankHolding(close, now) {
		return false
	}
	b.buffAt["kite"] = now.Add(kiteEvery)
	// Ankle Snare on the chaser's cell first (it walks into it as it follows us), when we carry a Trap
	if si := b.skills[htAnkleSnare]; si != nil && si.Lv > 0 && b.countItem(itemTrap) > 0 && b.SP >= max(si.SP, 12) &&
		now.After(b.buffAt["snare"]) {
		b.buffAt["snare"] = now.Add(6 * time.Second)
		lv := si.Lv
		go b.send(newPkt(0x0366).u16(uint16(lv)).u16(htAnkleSnare).u16(uint16(cx)).u16(uint16(cy)).b) // UseSkillToPos
	}
	x, y, ok := kiteCell(gm, b.X, b.Y, cx, cy)
	if !ok {
		return false // cornered: fight it out (potions, escape if losing)
	}
	b.setState("kiting " + mobName(e.Class))
	b.attackSentFor = 0 // re-send the attack / Double Strafe once we stand again
	go b.walkTo(x, y)
	return true
}

// kiteCell: a walkable cell 4-6 steps straight away from (cx, cy), in our walk region; fleeCell (any direction,
// 3-10 steps) when the straight line is blocked.
func kiteCell(gm *GameMap, x0, y0, cx, cy int) (int, int, bool) {
	dx, dy := sign(x0-cx), sign(y0-cy)
	if dx == 0 && dy == 0 {
		dx = 1
	}
	reg := gm.Region(x0, y0)
	for k := 6; k >= 4; k-- {
		x, y := x0+dx*k, y0+dy*k
		if gm.Walkable(x, y) && gm.Region(x, y) == reg {
			return x, y, true
		}
	}
	return fleeCell(gm, x0, y0, cx, cy)
}

// tankHolding (b.mu held): a party tank (Swordman line / Monk) stands within 2 cells of these monsters - it holds them.
func (b *Bot) tankHolding(ms []*Entity, now time.Time) bool {
	p := partyOf(b)
	if p == nil {
		return false
	}
	for _, m := range p.Members {
		if m == b || partyRole(m) != roleTank {
			continue
		}
		s, ok := peek(m.P.Name)
		if !ok || !s.Online || s.Map != b.MapName {
			continue
		}
		for _, o := range ms {
			ox, oy := o.pos(now)
			if dist(s.X, s.Y, ox, oy) <= 2 {
				return true
			}
		}
	}
	return false
}
