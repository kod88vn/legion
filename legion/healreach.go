package main

// healreach.go: a Heal only goes out when the server will take it.
//
// QA 2026-11-15 03:35: Zendancer84 (party priest) sent Heal to Ayla (33% HP) ~50 times a minute from geffen 112,38
// while she sat at 118,39 in a gap of the fountain wall, 6 cells away. rAthena drops a targeted skill WITHOUT any
// reply when battle_check_range fails: circular distance (distance_client) over the skill range, or a wall cell on
// the line between caster and target (path_search_long, CELL_CHKWALL). Not one heal landed, and healFirst, which
// runs before everything else, held the priest in place re-sending it every 900 ms.
//   - healReachable: the server's own range + line check, before we send
//   - healSpot: when the target is in sight but not reachable, walk to the nearest cell that is
//   - healUnacked: a heal the server never confirmed 3 times in a row (a check we got wrong, a stale position):
//     stop sending to that target for a few seconds and walk next to it instead (under 2 cells needs no line)

import (
	"math"
	"time"
)

// shootLine ports rAthena path_search_long(..., CELL_CHKWALL): only wall cells (gat type 1, not walkable and not
// shootable) block; cliffs (5) and water are shootable. The two end cells are not checked.
func (m *GameMap) shootLine(x0, y0, x1, y1 int) bool {
	if m == nil || len(m.cells) < m.W*m.H {
		return true // no map data: let the server decide
	}
	wall := func(x, y int) bool {
		if x < 0 || y < 0 || x >= m.W || y >= m.H {
			return true
		}
		return m.cells[x+y*m.W] == 1
	}
	dx := x1 - x0
	if dx < 0 {
		x0, x1, y0, y1 = x1, x0, y1, y0
		dx = -dx
	}
	dy := y1 - y0
	weight := abs(dy)
	if dx > abs(dy) {
		weight = dx
	}
	wx, wy := 0, 0
	for x0 != x1 || y0 != y1 {
		wx += dx
		wy += dy
		if wx >= weight {
			wx -= weight
			x0++
		}
		if wy >= weight {
			wy -= weight
			y0++
		} else if wy < 0 {
			wy += weight
			y0--
		}
		if (x0 != x1 || y0 != y1) && wall(x0, y0) {
			return false
		}
	}
	return true
}

// distClient: rAthena distance_client, the circular distance a player's skill range is checked with.
func distClient(dx, dy int) int {
	d := math.Sqrt(float64(dx*dx+dy*dy)) - 0.1
	if d < 0 {
		d = 0
	}
	return int(d)
}

// healReachable: rAthena battle_check_range for a player caster at (x0,y0) and a target at (x1,y1).
func healReachable(m *GameMap, x0, y0, x1, y1, rng int) bool {
	if distClient(x0-x1, y0-y1) > rng {
		return false
	}
	d := dist(x0, y0, x1, y1)
	if d < 2 {
		return true
	}
	if d > 14 { // AREA_SIZE
		return false
	}
	return m.shootLine(x0, y0, x1, y1)
}

// healSpot: the walkable cell nearest to (bx,by), within 12 cells of us, from which (tx,ty) can be healed.
func healSpot(m *GameMap, bx, by, tx, ty, rng int) (int, int, bool) {
	if m == nil {
		return 0, 0, false
	}
	bestX, bestY, best := 0, 0, math.MaxInt
	for y := ty - rng; y <= ty+rng; y++ {
		for x := tx - rng; x <= tx+rng; x++ {
			if !m.Walkable(x, y) || dist(bx, by, x, y) > 12 || !healReachable(m, x, y, tx, ty, rng) {
				continue
			}
			// prefer short walks; between equals, a cell a bit closer to the target (it may still move)
			c := 10*dist(bx, by, x, y) + dist(x, y, tx, ty)
			if c < best {
				bestX, bestY, best = x, y, c
			}
		}
	}
	return bestX, bestY, best != math.MaxInt
}

// healPend: heals sent and not yet confirmed by the server, per caster (b.mu held by the caller for its own bot).
var healPend = struct {
	n      map[string]int
	target map[string]uint32
}{n: map[string]int{}, target: map[string]uint32{}}

// healSent records a heal we just sent; healUnacked is true when 3 heals in a row to this target got no reply.
func healSent(caster string, target uint32) {
	healStats.Lock()
	defer healStats.Unlock()
	if healPend.target[caster] != target {
		healPend.target[caster], healPend.n[caster] = target, 0
	}
	healPend.n[caster]++
}

func healUnacked(caster string, target uint32) bool {
	healStats.Lock()
	defer healStats.Unlock()
	return healPend.target[caster] == target && healPend.n[caster] >= 3
}

func healAcked(caster string) {
	healStats.Lock()
	defer healStats.Unlock()
	healPend.n[caster] = 0
	healStats.at[caster] = time.Now()
}

// healResetPend: forget the unconfirmed count (after we gave up on that target for a while).
func healResetPend(caster string) {
	healStats.Lock()
	defer healStats.Unlock()
	healPend.n[caster] = 0
}

// healedLately: the server confirmed a heal by this caster within d (chatter.go: say "healing" only then).
func healedLately(caster string, d time.Duration) bool {
	healStats.Lock()
	defer healStats.Unlock()
	at, ok := healStats.at[caster]
	return ok && time.Since(at) < d
}

// healApproach (b.mu held): walk toward a spot from which the target can be healed. True = this think was spent.
func (b *Bot) healApproach(name string, tx, ty int, adjacent bool, now time.Time) bool {
	gm := maps[b.MapName]
	x, y, ok := tx, ty, adjacent
	if !adjacent {
		x, y, ok = healSpot(gm, b.X, b.Y, tx, ty, healRange)
	}
	if !ok {
		return false
	}
	if now.After(b.buffAt["healwalk"]) {
		b.buffAt["healwalk"] = now.Add(1200 * time.Millisecond)
		if b.Sitting {
			b.sitSentAt, b.wantSit = now, false
			go b.stand()
		} else {
			go b.walkTo(x, y)
		}
	}
	b.setState("moving to heal " + name)
	return true
}

// humanHPFrac: a human party member's HP from her own client's status file when it is fresh (< 5 s) and she is on
// our map; the party HP packet (0x080e) only comes while she is in our area and her HP changes, so it goes stale.
func humanHPFrac(name, ourMap string, cur float64, now time.Time) float64 {
	st, ok := humanStatusOf(name)
	if !ok || st.Map != ourMap || st.MaxHP <= 0 || now.Sub(time.Unix(st.TS, 0)) > 5*time.Second {
		return cur
	}
	return float64(st.HP) / float64(st.MaxHP)
}

// supportLine (chatter.go): a party healer's ambient line. It says it heals only right after a heal the server
// confirmed; before, it picked "healing" / "inc agi on u" at random while casting nothing (owner, 2026-11-15).
func supportLine(b *Bot) string {
	if healedLately(b.charName(), 8*time.Second) {
		return pick("healing", "heal up guys", "healing, stay close")
	}
	return pick("dont run off guys", "stay close pls", "call if u need heal", "watch my sp pls")
}
