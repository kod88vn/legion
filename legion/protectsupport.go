package main

// protectsupport.go - AY26 (owner 2026-11-15, "Must always protect support"): the party always protects its support.
// The frontlines stay between the support and the monsters, the ranged keep the support out of harm's way, and no one
// leaves the support behind (AY22/AY23 generalised). Flag: tuning party_protect_support_on (default OFF). An engine
// party (ovEngineParty: p.Engine, human in engine_parties) always has it. Flag off and not an engine party: this file
// does nothing and the party runs the old rules byte for byte.
//
// Order in the member flow (party.go, after the rest hold): a member that is not the support first
//   - walks back to the support when it is beyond supportLeashCells on the support's map (not while being hit: PT04
//     fights what hits it first);
//   - otherwise, when a monster is within supportThreatCells of the support: a frontline steps to the point between
//     them (guardSpot), a ranged member to the point behind the support (backSpot), and only while the monster is
//     close enough to matter. A frontline already in melee range keeps fighting.
// Existing rules still apply first where they already cover AY26: protectSupport / meleePeel (tank and melee take a
// monster off a support in need), tankFallBack, stickEscape (run to the healer), rejoinAfterDeath, PT31.

import (
	"strings"
	"time"
)

// supportLeashCells: a member farther than this from its support walks to the support first (AY22's 15 cells).
const supportLeashCells = 15

// supportThreatCells: a monster this near the support is a threat to it (the frontline and ranged move for it).
const supportThreatCells = 8

// supportGuardGap: the frontline stands this many cells from the support, on the monster's side.
const supportGuardGap = 2

// supportBackGap: the ranged member stands this many cells from the support, on the side away from the monster.
const supportBackGap = 3

// supportRangedAct: a ranged member only steps back when the threat is this close to it (farther, it is not in danger).
const supportRangedAct = 6

// partyProtectOn: the AY26 rule applies to this party.
func partyProtectOn(p *Party) bool {
	if p == nil {
		return false
	}
	return tune().PartyProtectSupportOn || ovEngineParty(p)
}

// supportOf: the party's support on this member's map (not the member itself), from the published states.
func supportOf(members []pubState, me string) (pubState, bool) {
	for _, s := range members {
		if s.Support && s.Online && !strings.EqualFold(s.Name, me) && s.Map != "" && (s.X|s.Y != 0 || s.LX|s.LY != 0) {
			return s, true
		}
	}
	return pubState{}, false
}

// supportLeashed: the member at (mx,my) on mapM is beyond supportLeashCells of the support at (sx,sy) on mapS. A support
// on another map is not a leash (the travel logic brings the party together).
func supportLeashed(mapM string, mx, my int, mapS string, sx, sy int) bool {
	if mapM == "" || mapM != mapS || mx|my == 0 || sx|sy == 0 {
		return false
	}
	return dist(mx, my, sx, sy) > supportLeashCells
}

// guardSpot: the point gap cells from the support (sx,sy) toward the monster (tx,ty): where a frontline stands between
// them. ok=false when the monster is on the support's own cell.
func guardSpot(sx, sy, tx, ty, gap int) (int, int, bool) {
	d := dist(sx, sy, tx, ty)
	if d == 0 {
		return sx, sy, false
	}
	k := min(gap, d)
	return sx + (tx-sx)*k/d, sy + (ty-sy)*k/d, true
}

// backSpot: the point gap cells from the support (sx,sy) on the side away from the monster (tx,ty): where a ranged member
// keeps the support between itself and the monsters, still inside a heal's reach.
func backSpot(sx, sy, tx, ty, gap int) (int, int, bool) {
	d := dist(sx, sy, tx, ty)
	if d == 0 {
		return sx, sy, false
	}
	return sx - (tx-sx)*gap/d, sy - (ty-sy)*gap/d, true
}

// protectSpot: where a member of this role goes to protect its support (pure; the caller checks partyProtectOn and
// that it is not in melee with its own target). ok=false: it stays where it is.
//   - leashed: walk to the support (any role);
//   - else with a threat near the support: a frontline (tank, melee) to guardSpot, a ranged member to backSpot when the
//     threat is within supportRangedAct of it; the spot must be more than 2 cells away to be worth a walk.
func protectSpot(role, mx, my, sx, sy, tx, ty int, hasThreat, leashed bool) (x, y int, why string, ok bool) {
	if leashed {
		return sx, sy, "catching up with the support", true
	}
	if !hasThreat {
		return 0, 0, "", false
	}
	switch role {
	case roleTank, roleMelee:
		x, y, ok = guardSpot(sx, sy, tx, ty, supportGuardGap)
		why = "standing between the support and the monsters"
	case roleRanged:
		if dist(mx, my, tx, ty) > supportRangedAct {
			return 0, 0, "", false
		}
		x, y, ok = backSpot(sx, sy, tx, ty, supportBackGap)
		why = "keeping behind the support"
	}
	if !ok || dist(mx, my, x, y) <= 2 {
		return 0, 0, "", false
	}
	return x, y, why, true
}

// supportThreat (b.mu held): the nearest monster within supportThreatCells of the support (sx,sy).
func (b *Bot) supportThreat(sx, sy int, now time.Time) (int, int, bool) {
	tx, ty, found, bd := 0, 0, false, supportThreatCells+1
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		ex, ey := e.pos(now)
		if d := dist(sx, sy, ex, ey); d < bd {
			tx, ty, found, bd = ex, ey, true, d
		}
	}
	return tx, ty, found
}

// protectOwnSupport (b.mu held, party.go member flow): the AY26 move for a member that is not the support. True when it
// owns the tick. Flag off (partyProtectOn false) returns false before touching anything.
func (b *Bot) protectOwnSupport(p *Party, members []pubState, gm *GameMap, moving bool, now time.Time) bool {
	if gm == nil || !partyProtectOn(p) {
		return false
	}
	sup, ok := supportOf(members, b.charName())
	if !ok {
		return false
	}
	sx, sy := livePosOf(sup)
	mx, my := b.livePos(now)
	leashed := supportLeashed(b.MapName, mx, my, sup.Map, sx, sy)
	if leashed && b.mobbedNow() > 0 {
		return false // being hit far from the support: fight what hits it first (PT04), then walk back
	}
	tx, ty, hasThreat := b.supportThreat(sx, sy, now)
	x, y, why, ok := protectSpot(partyRole(b), mx, my, sx, sy, tx, ty, hasThreat, leashed)
	if !ok {
		return false
	}
	if !leashed && b.target != 0 {
		if e := b.ents[b.target]; e != nil && e.Type == objMob {
			if ex, ey := e.pos(now); dist(mx, my, ex, ey) <= 2 {
				return false // in melee with its target: finish the fight, the spot can wait
			}
		}
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
	}
	b.target = 0
	b.setState(why + " (" + sup.Name + ")")
	if !moving {
		b.stepAlong(gm, x, y)
	}
	return true
}
