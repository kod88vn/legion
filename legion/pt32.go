package main

import "time"

// PT32 / PR16 (owner 2026-11-14): fight in place near the party. A party member does not run around pulling more
// attackers: it takes monsters one or a few at a time where the priest can cover, and stays back rather than charging
// into groups. A NEW target (whatever is already hitting it is always fair game - hunt() picks those first) must be
// within pt32Reach cells of the bot or of a party mate, with no other aggressive monster within 2 cells of the straight
// way to it, and none is started while 2+ monsters are already on the bot. Flag: tuning party_fight_in_place_on.

const pt32Reach = 8

type fipFilter struct {
	bx, by    int
	refs      [][2]int
	aggr      map[uint32][2]int // other aggressive monsters in view
	attackers int               // monsters that hit the bot in the last 4 s
}

// fightInPlace builds the filter for this tick; nil when off or not in a party.
func (b *Bot) fightInPlace(now time.Time, refs [][2]int) *fipFilter {
	if !tune().PartyFightInPlaceOn || partyOf(b) == nil || isTown(b.MapName) {
		return nil
	}
	bx, by := b.livePos(now)
	f := &fipFilter{bx: bx, by: by, refs: refs, aggr: map[uint32][2]int{}}
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		if now.Sub(e.LastHitMe) < 4*time.Second {
			f.attackers++
		}
		if mb := mobs[e.Class]; mb != nil && mb.Aggressive {
			x, y := e.pos(now)
			f.aggr[e.ID] = [2]int{x, y}
		}
	}
	return f
}

func (f *fipFilter) allows(id uint32, x, y int) bool {
	others := make([][2]int, 0, len(f.aggr))
	for oid, p := range f.aggr {
		if oid != id {
			others = append(others, p)
		}
	}
	return pt32Allows(f.bx, f.by, x, y, f.refs, others, f.attackers)
}

// pt32Allows: may a bot at (bx,by) start a new fight with a monster at (x,y)? refs = party mates' cells, others =
// the other aggressive monsters' cells, attackers = monsters on the bot now.
func pt32Allows(bx, by, x, y int, refs, others [][2]int, attackers int) bool {
	if attackers >= 2 {
		return false
	}
	near := dist(bx, by, x, y) <= pt32Reach
	for _, r := range refs {
		if dist(r[0], r[1], x, y) <= pt32Reach {
			near = true
		}
	}
	if !near {
		return false
	}
	n := dist(bx, by, x, y)
	for _, o := range others {
		if dist(bx, by, o[0], o[1]) <= 1 || (o[0] == x && o[1] == y) {
			continue // beside us already (on us, not on the way) / the target's own cell
		}
		for i := 1; i <= n; i++ {
			px, py := bx+(x-bx)*i/n, by+(y-by)*i/n
			if dist(px, py, o[0], o[1]) <= 2 {
				return false
			}
		}
	}
	return true
}
