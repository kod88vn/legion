package main

// Sweep the map (owner rule 2026-11-14, RULES.md section 3: "why is ayla walking around the same corner. Does it not
// know to sweep the map?"). When nothing is in view, a bot party leader or a solo bot walks a steady patrol loop over
// the map's open areas instead of random far cells (which circled one corner or bounced back and forth):
//   - patrolRoute: per (map, walk region) waypoints on a patrolStep grid (a bot sees ~14 cells), each moved to a
//     walkable cell of the region away from portals, ordered into one closed loop (nearest neighbour + 2-opt). Built
//     once per map/region and shared by every bot.
//   - a bot joins the loop at the nearest waypoint and keeps one direction; a waypoint it can't reach in
//     patrolLegTimeout is skipped (never turned back on).
//   - fresh monster sightings (seen.go sightNear) along the next waypoints, or close by, are a jump ahead: the bot goes
//     there, then rejoins the loop at the waypoint after the one nearest to it, in the same direction.
//   - group pace (owner: "the leader never runs ahead", "at the group's pace"): a bot party leader paces instead of
//     holding - each walk segment ends at most patrolLeadMax cells from the slowest member within together_cells
//     (live positions), or no farther from it than the leader already is; at least one cell is always walked, so the
//     leader never stops to wait and never turns back. Members follow 4-6 cells behind (party.go); a member beyond
//     together_cells is collected first (ai.go hunt), and a resting member holds the party (together.go).

import (
	"math"
	"strconv"
	"sync"
	"time"
)

const (
	patrolStep       = 24               // grid spacing of the waypoints (cells)
	patrolSnap       = 9                // how far a grid point may move to find a walkable, portal-free cell
	patrolReach      = 4                // a waypoint within this many cells counts as reached
	patrolLegTimeout = 45 * time.Second // a waypoint not reached in this time is skipped
	patrolAhead      = 6                // sightings near the next this-many waypoints are "ahead"
	patrolLeadMax    = 9                // a sweeping leader's step ends at most this far from its slowest member
)

var patrolCache = struct {
	sync.Mutex
	m map[string][][2]int
}{m: map[string][][2]int{}}

func patrolKey(m string, reg uint16) string {
	return m + "#" + strconv.Itoa(int(reg))
}

// patrolRoute: the closed patrol loop of walk region reg on gm (cached)
func patrolRoute(gm *GameMap, reg uint16) [][2]int {
	key := patrolKey(gm.Name, reg)
	patrolCache.Lock()
	r, ok := patrolCache.m[key]
	patrolCache.Unlock()
	if ok {
		return r
	}
	r = buildPatrol(gm, reg)
	patrolCache.Lock()
	patrolCache.m[key] = r
	patrolCache.Unlock()
	return r
}

func buildPatrol(gm *GameMap, reg uint16) [][2]int {
	lab := gm.components()
	wm := gm.warpMask()
	good := func(x, y int) bool {
		if x < 0 || y < 0 || x >= gm.W || y >= gm.H || !gm.Walkable(x, y) || lab[x+y*gm.W] != reg {
			return false
		}
		// keep 5 cells from any portal area: a waypoint next to a warp walks bots off the map
		for dy := -5; dy <= 5; dy += 5 {
			for dx := -5; dx <= 5; dx += 5 {
				if nx, ny := x+dx, y+dy; nx >= 0 && ny >= 0 && nx < gm.W && ny < gm.H && wm[nx+ny*gm.W] != 0 {
					return false
				}
			}
		}
		return true
	}
	var pts [][2]int
	for gy := patrolStep / 2; gy < gm.H; gy += patrolStep {
		for gx := patrolStep / 2; gx < gm.W; gx += patrolStep {
			// the nearest good cell to the grid point, ring by ring
		search:
			for r := 0; r <= patrolSnap; r++ {
				for dy := -r; dy <= r; dy++ {
					for dx := -r; dx <= r; dx++ {
						if max(abs(dx), abs(dy)) != r {
							continue
						}
						if good(gx+dx, gy+dy) {
							pts = append(pts, [2]int{gx + dx, gy + dy})
							break search
						}
					}
				}
			}
		}
	}
	return orderLoop(pts)
}

func edist(a, b [2]int) float64 {
	return math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1]))
}

// orderLoop: a short closed tour through pts (nearest neighbour from the first point, then 2-opt)
func orderLoop(pts [][2]int) [][2]int {
	n := len(pts)
	if n <= 3 {
		return pts
	}
	used := make([]bool, n)
	tour := make([][2]int, 0, n)
	cur := 0
	used[0] = true
	tour = append(tour, pts[0])
	for len(tour) < n {
		bi, bd := -1, math.MaxFloat64
		for i, p := range pts {
			if !used[i] {
				if d := edist(pts[cur], p); d < bd {
					bi, bd = i, d
				}
			}
		}
		used[bi] = true
		tour = append(tour, pts[bi])
		cur = bi
	}
	for pass := 0; pass < 20; pass++ {
		improved := false
		for i := 0; i < n-1; i++ {
			for j := i + 2; j < n; j++ {
				a, b := tour[i], tour[i+1]
				c, d := tour[j], tour[(j+1)%n]
				if i == 0 && j == n-1 {
					continue
				}
				if edist(a, c)+edist(b, d) < edist(a, b)+edist(c, d)-0.01 {
					for l, r := i+1, j; l < r; l, r = l+1, r-1 {
						tour[l], tour[r] = tour[r], tour[l]
					}
					improved = true
				}
			}
		}
		if !improved {
			break
		}
	}
	return tour
}

// nearestWaypoint: index of the waypoint closest to (x, y)
func nearestWaypoint(r [][2]int, x, y int) int {
	bi, bd := 0, 1<<30
	for i, p := range r {
		if d := dist(x, y, p[0], p[1]); d < bd {
			bi, bd = i, d
		}
	}
	return bi
}

func wrapIdx(i, n int) int {
	return ((i % n) + n) % n
}

// paceLimit (b.mu held): for a bot party leader, the test a walk segment's end must pass to keep the group bunched
// (see the header); nil for a solo bot or when no member is within together_cells on this map
func (b *Bot) paceLimit(now time.Time) func(x, y int) bool {
	p := partyOf(b)
	if p == nil || p.Leader != b || p.Human != "" {
		return nil
	}
	lx, ly := b.livePos(now)
	var slow [][2]int
	for _, m := range p.Members {
		if m == b {
			continue
		}
		if s, ok := peek(m.P.Name); ok && s.Online && s.Map == b.MapName && !(s.X == 0 && s.Y == 0) {
			if mx, my := livePosOf(s); dist(lx, ly, mx, my) <= togetherCells() {
				slow = append(slow, [2]int{mx, my})
			}
		}
	}
	return paceTest(slow, lx, ly)
}

// paceTest: a segment end (x, y) passes when, for every member position, it is within patrolLeadMax of it or no
// farther from it than the leader's own position (lx, ly)
func paceTest(members [][2]int, lx, ly int) func(x, y int) bool {
	if len(members) == 0 {
		return nil
	}
	return func(x, y int) bool {
		for _, m := range members {
			if d := dist(x, y, m[0], m[1]); d > patrolLeadMax && d > dist(lx, ly, m[0], m[1]) {
				return false
			}
		}
		return true
	}
}

// visibleQuarry (b.mu held): the nearest monster in view that hunt() would take, wherever it is in view
func (b *Bot) visibleQuarry(gm *GameMap, reg uint16, maxLv, plv int, now time.Time) *Entity {
	var best *Entity
	bd := 99
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		mb := mobs[e.Class]
		if mb == nil || mb.Boss || mb.Level > maxLv || mb.worthless() {
			continue
		}
		x, y := e.pos(now)
		d := dist(b.X, b.Y, x, y)
		if d >= bd || !gm.Walkable(x, y) || gm.Region(x, y) != reg || gm.OnWarp(x, y) || bossSpotNear(b.MapName, x, y, 12, plv) ||
			!(claims.free(b.MapName, e.ID, b.aid) || b.partyClaim(e.ID)) {
			continue
		}
		best, bd = e, d
	}
	return best
}

// sweep (b.mu held, nothing in view): the next step of the patrol loop. False when the region has no loop (tiny
// pocket): the caller falls back to explore's random far cell.
func (b *Bot) sweep(gm *GameMap, now time.Time) bool {
	reg := gm.Region(b.X, b.Y)
	r := patrolRoute(gm, reg)
	if len(r) < 2 {
		return false
	}
	key := patrolKey(gm.Name, reg)
	if b.patrolKey != key {
		b.patrolKey = key
		b.patrolIdx = wrapIdx(nearestWaypoint(r, b.X, b.Y)+1, len(r))
		b.patrolDir = 1
		if b.rng.Intn(2) == 0 {
			b.patrolDir = -1
			b.patrolIdx = wrapIdx(b.patrolIdx-2, len(r))
		}
		b.patrolAt, b.roamDest = now, [2]int{}
	}
	// back to sweeping after a fight / rest: rejoin at the nearest waypoint, or the one after it when we are already
	// past it (between it and the next) - never a step back along the loop
	if now.Sub(b.patrolTick) > 5*time.Second {
		j := nearestWaypoint(r, b.X, b.Y)
		nx := r[wrapIdx(j+b.patrolDir, len(r))]
		if dist(b.X, b.Y, r[j][0], r[j][1]) <= patrolReach || dist(b.X, b.Y, nx[0], nx[1]) <= dist(r[j][0], r[j][1], nx[0], nx[1]) {
			j = wrapIdx(j+b.patrolDir, len(r))
		}
		b.patrolIdx, b.patrolAt, b.roamDest = j, now, [2]int{}
	}
	b.patrolTick = now
	pace := b.paceLimit(now)
	// a detour to a sighting: done when reached or timed out -> rejoin the loop ahead of where we are
	if d := b.roamDest; d != [2]int{} {
		if dist(b.X, b.Y, d[0], d[1]) > patrolReach && now.Sub(b.roamDestAt) < patrolLegTimeout {
			b.stepAlongLimit(gm, d[0], d[1], pace)
			return true
		}
		b.roamDest = [2]int{}
		b.patrolIdx = wrapIdx(nearestWaypoint(r, b.X, b.Y)+b.patrolDir, len(r))
		b.patrolAt = now
	}
	// jump ahead: a fresh sighting near the next waypoints (or within 30 cells) that we can take
	ahead := make([][2]int, 0, patrolAhead)
	for k := 0; k < patrolAhead && k < len(r); k++ {
		ahead = append(ahead, r[wrapIdx(b.patrolIdx+k*b.patrolDir, len(r))])
	}
	maxLv, plv := b.targetMaxLv(), b.powerLv()
	// a monster we can take already in view but beyond the 14-cell pick range: walk to it first (QA 10:20: 13% of the
	// leaders' roaming time had only "far" monsters in view while they walked on toward the next waypoint)
	if e := b.visibleQuarry(gm, reg, maxLv, plv, now); e != nil {
		x, y := e.pos(now)
		b.roamDest, b.roamDestAt = [2]int{x, y}, now
		b.stepAlongLimit(gm, x, y, pace)
		return true
	}
	if x, y, ok := sightNear(b.MapName, b.X, b.Y, 15, 80, maxLv, now, func(x, y int) bool {
		if !gm.Walkable(x, y) || gm.Region(x, y) != reg || gm.OnWarp(x, y) || bossSpotNear(b.MapName, x, y, 12, plv) {
			return false
		}
		if dist(b.X, b.Y, x, y) <= 30 {
			return true
		}
		for _, a := range ahead {
			if dist(a[0], a[1], x, y) <= patrolStep {
				return true
			}
		}
		return false
	}); ok {
		b.roamDest, b.roamDestAt, b.path = [2]int{x, y}, now, nil
		b.stepAlongLimit(gm, x, y, pace)
		return true
	}
	// the loop: next waypoint, same direction; skip one we can't reach in time
	w := r[b.patrolIdx]
	if dist(b.X, b.Y, w[0], w[1]) <= patrolReach || now.Sub(b.patrolAt) > patrolLegTimeout {
		b.patrolIdx = wrapIdx(b.patrolIdx+b.patrolDir, len(r))
		b.patrolAt, b.path = now, nil
		w = r[b.patrolIdx]
	}
	b.stepAlongLimit(gm, w[0], w[1], pace)
	return true
}
