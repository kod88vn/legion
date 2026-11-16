package main

// protectsupport.go - AY26 (owner 2026-11-15, "Must always protect support"): the party always protects its support.
// The frontlines stay between the support and the monsters, the ranged keep the support out of harm's way, and no one
// leaves the support behind. Flags: party_protect_support_on (default OFF) and party_protect_support_off (default OFF,
// wins over everything: a live revert). An engine party (ovEngineParty) has it unless the off flag is set. Flags off and
// not an engine party: nothing here runs and the party keeps the old rules.
//
// The hook sits in the member flow of partyThink AFTER the tank's and melee's own jobs (tankFallBack, protectSupport,
// tankTakeAggro, tankGoHelp, meleePeel, the fight a member is already in) and before the assist / ranged fight. So it
// adds to those rules and never preempts them:
//   - a member that is not the support walks back to it when leashed (supportLeashCells, not while being hit: PT04);
//   - a frontline steps between the support and a REAL threat: a monster within supportThreatCells of the support that
//     targets it, or while the support is being hit, called for help or low on HP (supportInDanger);
//   - a ranged member steps behind the support only while a monster is chasing it (targets it, within supportRangedAct);
//   - a member already in melee with its target, or whose target is hitting the support, keeps that fight;
//   - at most one repositioning per supportSpotCooldown per member (no ping-pong with the normal fight);
//   - every spot must be walkable.

import (
	"strings"
	"sync/atomic"
	"time"
)

// supportLeashCells: a member this far (or farther) from its support on the same map walks back to it first. Not tighter
// than the tank's pull range (tank_pull_radius 12, togetherCells-4 = 16 from the leader).
const supportLeashCells = 18

// supportThreatCells: a monster this near the support can be a threat to it.
const supportThreatCells = 3

// supportLowHP: a support under this HP fraction counts as threatened when a monster is near it.
const supportLowHP = 0.7

// supportGuardGap: the frontline stands this many cells from the support, on the monster's side.
const supportGuardGap = 2

// supportBackGap: the ranged member stands this many cells from the support, on the side away from the chaser.
const supportBackGap = 3

// supportRangedAct: a chaser this close to a ranged member makes it step behind the support.
const supportRangedAct = 6

// supportTargetAge: a monster's target counts as current for this long after it was set.
const supportTargetAge = 4 * time.Second

// supportSpotCooldown: a member repositions at most once per this span (hysteresis; no ping-pong with the fight).
const supportSpotCooldown = 3 * time.Second

// protectSpotMoves counts the repositioning walks issued (tests and the log).
var protectSpotMoves atomic.Int64

// partyProtectOn: the AY26 rule applies to this party.
func partyProtectOn(p *Party) bool {
	if p == nil || tune().PartyProtectSupportOff {
		return false
	}
	return tune().PartyProtectSupportOn || ovEngineParty(p)
}

// supportOf: the nearest online support of the party on this map, other than the member itself (live positions).
func supportOf(members []pubState, me, mapName string, mx, my int) (pubState, bool) {
	var best pubState
	bd, found := 1<<30, false
	for _, s := range members {
		if !s.Support || !s.Online || strings.EqualFold(s.Name, me) || mapName == "" || s.Map != mapName {
			continue
		}
		sx, sy := livePosOf(s)
		if sx|sy == 0 {
			continue
		}
		if d := dist(mx, my, sx, sy); d < bd {
			best, bd, found = s, d, true
		}
	}
	return best, found
}

// supportLeashed: the member at (mx,my) on mapM is at least supportLeashCells from the support at (sx,sy) on mapS. A
// support on another map is not a leash (the travel logic brings the party together).
func supportLeashed(mapM string, mx, my int, mapS string, sx, sy int) bool {
	if mapM == "" || mapM != mapS || mx|my == 0 || sx|sy == 0 {
		return false
	}
	return dist(mx, my, sx, sy) >= supportLeashCells
}

// supportInDanger: is a monster at distance d from the support a real threat to it? Only a monster close to the support
// that targets it, or a support being hit, called for help or low on HP.
func supportInDanger(sup pubState, targetsSup bool, d int) bool {
	if d > supportThreatCells {
		return false
	}
	return targetsSup || sup.Mobbed > 0 || sup.Help || sup.HPFrac > 0 && sup.HPFrac < supportLowHP
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

// backSpot: the point gap cells from the support (sx,sy) on the side away from the chaser (tx,ty): where a ranged member
// keeps the support between itself and the monster, still inside a heal's reach.
func backSpot(sx, sy, tx, ty, gap int) (int, int, bool) {
	d := dist(sx, sy, tx, ty)
	if d == 0 {
		return sx, sy, false
	}
	return sx - (tx-sx)*gap/d, sy - (ty-sy)*gap/d, true
}

// protectSpot: where a member of this role goes to protect its support (pure). ok=false: it stays where it is.
//   - leashed: walk to the support (any role);
//   - threat (tank, melee): to guardSpot between the support and the threat at (tx,ty);
//   - chased (ranged): to backSpot away from the chaser at (tx,ty).
//
// A spot within 2 cells of the member is not worth a walk.
func protectSpot(role, mx, my, sx, sy, tx, ty int, threat, chased, leashed bool) (x, y int, why string, ok bool) {
	if leashed {
		return sx, sy, "catching up with the support", true
	}
	switch role {
	case roleTank, roleMelee:
		if !threat {
			return 0, 0, "", false
		}
		x, y, ok = guardSpot(sx, sy, tx, ty, supportGuardGap)
		why = "standing between the support and the monsters"
	case roleRanged:
		if !chased {
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

// supportEnemies (b.mu held): the monster that threatens the support (nearest to it, supportInDanger) and the monster
// chasing this member (targets it, nearest, within supportRangedAct).
func (b *Bot) supportEnemies(sup pubState, sx, sy, mx, my int, now time.Time) (tx, ty int, threat bool, cx, cy int, chased bool) {
	tBest, cBest := supportThreatCells+1, supportRangedAct+1
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		ex, ey := e.pos(now)
		fresh := now.Sub(e.TargetAt) <= supportTargetAge
		targetsSup := fresh && sup.AID != 0 && e.Target == sup.AID
		if d := dist(sx, sy, ex, ey); d < tBest && supportInDanger(sup, targetsSup, d) {
			tx, ty, threat, tBest = ex, ey, true, d
		}
		if dc := dist(mx, my, ex, ey); fresh && b.aid != 0 && e.Target == b.aid && dc < cBest {
			cx, cy, chased, cBest = ex, ey, true, dc
		}
	}
	return
}

// engagedNow (b.mu held): this member is in melee with its own target, or its target is hitting the support: that fight
// goes on (the peel and fight rules own it).
func (b *Bot) engagedNow(sup pubState, mx, my int, now time.Time) bool {
	e := b.ents[b.target]
	if b.target == 0 || e == nil || e.Type != objMob {
		return false
	}
	ex, ey := e.pos(now)
	return dist(mx, my, ex, ey) <= 2 || sup.AID != 0 && e.Target == sup.AID && now.Sub(e.TargetAt) <= supportTargetAge
}

// protectOwnSupport (b.mu held, party.go member flow, after the tank's and melee's own jobs): the AY26 move for a member
// that is not the support. True when it owns the tick. With partyProtectOn false it returns before touching anything.
func (b *Bot) protectOwnSupport(p *Party, members []pubState, gm *GameMap, moving bool, now time.Time) bool {
	if gm == nil || !partyProtectOn(p) {
		return false
	}
	mx, my := b.livePos(now)
	sup, ok := supportOf(members, b.charName(), b.MapName, mx, my)
	if !ok {
		return false
	}
	sx, sy := livePosOf(sup)
	leashed := supportLeashed(b.MapName, mx, my, sup.Map, sx, sy)
	if leashed {
		if b.mobbedNow() > 0 {
			return false // being hit far from the support: fight what hits it first (PT04), then walk back
		}
	} else {
		if b.engagedNow(sup, mx, my, now) {
			return false
		}
		if b.buffAt != nil && now.Before(b.buffAt["protspot"]) {
			return false // repositioned a moment ago: the normal fight decides (no ping-pong)
		}
	}
	tx, ty, threat, cx, cy, chased := b.supportEnemies(sup, sx, sy, mx, my, now)
	if partyRole(b) == roleRanged {
		tx, ty = cx, cy // a ranged member steps away from the monster chasing it
	}
	x, y, why, ok := protectSpot(partyRole(b), mx, my, sx, sy, tx, ty, threat, chased, leashed)
	if !ok || !gm.Walkable(x, y) {
		return false
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
	}
	if !leashed {
		if b.buffAt == nil {
			b.buffAt = map[string]time.Time{}
		}
		b.buffAt["protspot"] = now.Add(supportSpotCooldown)
	}
	protectSpotMoves.Add(1)
	b.target = 0
	b.setState(why + " (" + sup.Name + ")")
	if !moving {
		b.stepAlong(gm, x, y)
	}
	return true
}
