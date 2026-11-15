package main

// casterkite.go - a Mage-line caster keeps melee monsters off itself (QA bug b1794711155x4s, 2026-11-15: solo Mages
// TheRyukun lv43 on moc_fild01 - killed by Peco Peco twice in 5 min, 643 HP - and Holy z43 lv24-25 on prt_fild00, 343
// HP, died 2x each; deaths_h ~2.4, zph a third of par). Cause: Mages had no answer to a monster standing next to
// them. kite.go steps back for bow users only, aoe.go walls only a monster 2-5 cells out that is still walking in,
// and danger.go escapes only once losing (fast HP loss / 3+ attackers / low HP). So a solo Mage kept casting bolts
// with a monster beating on it: each hit cancels the cast (no Phen), SP drained, HP fell, wing or death, then a
// long rest. A real Mage walls it or backs off and casts again.
//
// casterKite (from fight, before aoeCast, b.mu held): a monster within 2 cells that is our target or hit us in the
// last 2 s, 1-2 of them (3+ is danger.go's escape), no party tank holding it:
//  1. Fire Wall on the cell between us (the monster's own cell when adjacent) - it blocks and knocks it back;
//  2. else step 4-6 cells straight away (kite.go kiteCell), at most every 1.5 s and 3 times per monster, then fight
//     it out (potions, danger.go) - a faster monster can't be outrun forever.
// Not while a cast is in the air (moving cancels it). casterHelp (casteractive.go) also needs 70% HP.

import (
	"sync"
	"time"
)

const (
	casterKiteEvery = 1500 * time.Millisecond
	casterKiteMax   = 3 // back-off steps per monster
)

// casterKites: per bot, the monster it last backed off from and how often
var casterKites sync.Map // bot name -> casterKiteState

type casterKiteState struct {
	mob uint32
	n   int
}

// casterKiteWall: where a Fire Wall stops a monster at (ex,ey) next to us at (x,y): the cell between (d 2), its own
// cell (d 1). ok=false when it stands on us or beyond 2.
func casterKiteWall(x, y, ex, ey int) (int, int, bool) {
	switch dist(x, y, ex, ey) {
	case 1:
		return ex, ey, true
	case 2:
		return x + sign(ex-x), y + sign(ey-y), true
	}
	return 0, 0, false
}

// casterKiteNext: count one back-off step from mob; false = the budget for this monster is spent.
func casterKiteNext(name string, mob uint32) bool {
	st := casterKiteState{mob: mob}
	if v, ok := casterKites.Load(name); ok && v.(casterKiteState).mob == mob {
		st = v.(casterKiteState)
	}
	if st.n >= casterKiteMax {
		return false
	}
	st.n++
	casterKites.Store(name, st)
	return true
}

func (b *Bot) casterKite(e *Entity, moving bool, now time.Time) bool {
	if !b.casterOn() || b.Sitting || moving || b.hpFrac() <= 0 || now.Before(b.skillReady) {
		return false
	}
	if b.buffAt == nil {
		b.buffAt = map[string]time.Time{}
	}
	if now.Before(b.buffAt["ckite"]) {
		return false
	}
	gm := maps[b.MapName]
	if gm == nil {
		return false
	}
	if n, _, _, _ := b.attackers(now); n >= 3 {
		return false // swarmed: danger.go escape owns it
	}
	mx, my := b.myPos(now)
	var close []*Entity
	var near *Entity
	nd, cx, cy := 99, 0, 0
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		ox, oy := o.pos(now)
		d := dist(mx, my, ox, oy)
		if d > 2 || !(o.ID == e.ID || now.Sub(o.LastHitMe) < 2*time.Second) {
			continue
		}
		if mb := mobs[o.Class]; mb != nil && mb.worthless() {
			continue // plants never walk up
		}
		close = append(close, o)
		cx, cy = cx+ox, cy+oy
		if d < nd {
			near, nd = o, d
		}
	}
	if near == nil || b.tankHolding(close, now) {
		return false
	}
	cx, cy = cx/len(close), cy/len(close)
	// 1. Fire Wall between us
	if si := b.skills[mgFireWall]; si != nil && si.Lv > 0 && b.SP >= max(si.SP, 1) {
		key := "aoe:" + itoa(mgFireWall) // shared with aoe.go / casteractive.go
		if now.After(b.buffAt[key]) && now.After(b.skillFailUntil[mgFireWall]) {
			ex, ey := near.pos(now)
			if wx, wy, ok := casterKiteWall(mx, my, ex, ey); ok && gm.Walkable(wx, wy) {
				ms := (1760-160*si.Lv)*7/10 + 400 + 300
				b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
				b.buffAt[key] = now.Add(5 * time.Second)
				b.buffAt["ckite"] = now.Add(casterKiteEvery)
				b.targetAt, b.attackSentFor = now, 0
				b.setState("Fire Wall against " + mobName(near.Class))
				go b.send(newPkt(0x0366).u16(uint16(si.Lv)).u16(mgFireWall).u16(uint16(wx)).u16(uint16(wy)).b)
				return true
			}
		}
	}
	// 2. back off and cast again
	if !casterKiteNext(b.P.Name, near.ID) {
		return false
	}
	x, y, ok := kiteCell(gm, mx, my, cx, cy)
	if !ok {
		return false // cornered: fight it out
	}
	b.buffAt["ckite"] = now.Add(casterKiteEvery)
	b.attackSentFor = 0
	b.setState("backing off from " + mobName(near.Class))
	go b.walkTo(x, y)
	return true
}
