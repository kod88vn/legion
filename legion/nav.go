package main

// Map walkability (rAthena map_cache.dat), A* pathfinding, and cross-map routing over the warp graph.

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"
)

type GameMap struct {
	Name   string
	W, H   int
	cells  []byte // gat type per cell; 1 and 5 are not walkable
	walkXY [][2]int
	once   sync.Once
	comp   []uint16 // connected-region label per cell (lazy; shared by all bots)
	wmask  []byte   // warp trigger cells (lazy): 2 = inside a warp's area, 1 = the ring around it
}

func (m *GameMap) Walkable(x, y int) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return false
	}
	c := m.cells[x+y*m.W]
	return c != 1 && c != 5
}

// RandomWalkable picks any walkable cell (used for spawn points and roaming).
func (m *GameMap) RandomWalkable(r *rand.Rand) (int, int) {
	m.once.Do(func() {
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				if m.Walkable(x, y) {
					m.walkXY = append(m.walkXY, [2]int{x, y})
				}
			}
		}
	})
	if len(m.walkXY) == 0 {
		return m.W / 2, m.H / 2
	}
	p := m.walkXY[r.Intn(len(m.walkXY))]
	return p[0], p[1]
}

var maps = map[string]*GameMap{}

func loadMapCache(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// header: u32 file_size, u16 map_count, (2 bytes padding)
	count := int(le.Uint16(data[4:]))
	p := 8
	for i := 0; i < count && p+20 <= len(data); i++ {
		name := cstr(data[p : p+12])
		xs, ys := int(int16(le.Uint16(data[p+12:]))), int(int16(le.Uint16(data[p+14:])))
		n := int(le.Uint32(data[p+16:]))
		p += 20
		if p+n > len(data) {
			return fmt.Errorf("map cache truncated at %s", name)
		}
		zr, err := zlib.NewReader(bytes.NewReader(data[p : p+n]))
		if err != nil {
			return fmt.Errorf("map %s: %v", name, err)
		}
		cells, err := io.ReadAll(zr)
		zr.Close()
		if err != nil || len(cells) < xs*ys {
			return fmt.Errorf("map %s: bad cell data", name)
		}
		maps[name] = &GameMap{Name: name, W: xs, H: ys, cells: cells}
		p += n
	}
	return nil
}

// ---------------------------------------------------------------- A*

var dirs8 = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func octile(x0, y0, x1, y1 int) int {
	dx, dy := abs(x1-x0), abs(y1-y0)
	if dx < dy {
		dx, dy = dy, dx
	}
	return 10*(dx-dy) + 14*dy
}

// searchBuf holds reusable A* arrays (generation-stamped so they never need clearing).
type searchBuf struct {
	gen     uint32
	seenGen []uint32 // == gen: g/parent valid for this search
	doneGen []uint32 // == gen: closed
	g       []int32
	parent  []int32
	heap    []uint64 // (f << 32 | cell), min-heap, lazy deletion
}

var bufPool = sync.Pool{New: func() interface{} { return &searchBuf{} }}

func (s *searchBuf) ensure(n int) {
	if len(s.g) < n {
		s.seenGen, s.doneGen = make([]uint32, n), make([]uint32, n)
		s.g, s.parent = make([]int32, n), make([]int32, n)
		s.gen = 0
	}
	s.gen++
	if s.gen == 0 { // wrapped: reset stamps
		for i := range s.seenGen {
			s.seenGen[i], s.doneGen[i] = 0, 0
		}
		s.gen = 1
	}
	s.heap = s.heap[:0]
}

func (s *searchBuf) push(v uint64) {
	h := append(s.heap, v)
	i := len(h) - 1
	for i > 0 {
		p := (i - 1) / 2
		if h[p] <= h[i] {
			break
		}
		h[p], h[i] = h[i], h[p]
		i = p
	}
	s.heap = h
}

func (s *searchBuf) pop() uint64 {
	h := s.heap
	top := h[0]
	n := len(h) - 1
	h[0] = h[n]
	h = h[:n]
	i := 0
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		if r := l + 1; r < n && h[r] < h[l] {
			l = r
		}
		if h[i] <= h[l] {
			break
		}
		h[i], h[l] = h[l], h[i]
		i = l
	}
	s.heap = h
	return top
}

// FindPath returns cells from start (exclusive) to goal (inclusive), or nil. Diagonal moves may not cut corners
// (same rule as rAthena). maxNodes bounds the search (closed nodes).
func (m *GameMap) FindPath(sx, sy, gx, gy, maxNodes int) [][2]int {
	if !m.Walkable(gx, gy) || sx < 0 || sy < 0 || sx >= m.W || sy >= m.H {
		return nil
	}
	if sx == gx && sy == gy {
		return [][2]int{}
	}
	s := bufPool.Get().(*searchBuf)
	defer bufPool.Put(s)
	s.ensure(m.W * m.H)
	W := m.W
	start, goal := sx+sy*W, gx+gy*W
	wm := m.warpMask()
	// the warp we head for is exempt; the one we stand next to only for its ring (v=1), never its trigger area
	// (QA 2026-11-14: bots arriving in prontera at 47,228 from prt_in stood in the ring of the door warp 50,228; with
	// the whole box exempt their path to the Kafra went straight through the door - prontera <-> prt_in loop)
	exempt := m.warpBoxesAt(gx, gy)
	exemptStart := m.warpBoxesAt(sx, sy)
	startOnWarp := wm[start] == 2 // standing inside a trigger area already: may walk out through it
	goalOnWarp := wm[goal] == 2   // heading into a warp (the route's portal); a goal merely beside a door gets its ring only
	ringExempt := append(append([]Warp{}, exempt...), exemptStart...)
	s.seenGen[start], s.g[start], s.parent[start] = s.gen, 0, -1
	s.push(uint64(octile(sx, sy, gx, gy))<<32 | uint64(start))
	closed := 0
	for len(s.heap) > 0 && closed < maxNodes {
		v := s.pop()
		c := int(uint32(v))
		if s.doneGen[c] == s.gen {
			continue
		}
		s.doneGen[c] = s.gen
		closed++
		if c == goal {
			var out [][2]int
			for k := c; k != start; k = int(s.parent[k]) {
				out = append(out, [2]int{k % W, k / W})
			}
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out
		}
		cx, cy := c%W, c/W
		cg := s.g[c]
		for _, d := range dirs8 {
			nx, ny := cx+d[0], cy+d[1]
			if !m.Walkable(nx, ny) {
				continue
			}
			cost := int32(10)
			if d[0] != 0 && d[1] != 0 {
				if !m.Walkable(cx+d[0], cy) || !m.Walkable(cx, cy+d[1]) {
					continue
				}
				cost = 14
			}
			nk := nx + ny*W
			if s.doneGen[nk] == s.gen {
				continue
			}
			if wm[nk] != 0 && !(goalOnWarp && inWarpBoxes(exempt, nx, ny)) && !(wm[nk] == 1 && inWarpBoxes(ringExempt, nx, ny)) {
				if wm[nk] == 2 && !(startOnWarp && inWarpBoxes(exemptStart, nx, ny)) {
					continue // walking across a warp teleports the bot away
				}
				cost += 40 // ring around a warp: allowed (narrow passages), but avoided
			}
			ng := cg + cost
			if s.seenGen[nk] == s.gen && s.g[nk] <= ng {
				continue
			}
			s.seenGen[nk], s.g[nk], s.parent[nk] = s.gen, ng, int32(c)
			s.push(uint64(int(ng)+octile(nx, ny, gx, gy))<<32 | uint64(nk))
		}
	}
	return nil
}

// LineWalkable reports whether a straight line between two cells crosses only walkable cells (cheap check
// used for short roaming moves instead of a full A*).
func (m *GameMap) LineWalkable(x0, y0, x1, y1 int) bool {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		if !m.Walkable(x0, y0) || m.OnWarp(x0, y0) {
			return false
		}
		if x0 == x1 && y0 == y1 {
			return true
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

// ---------------------------------------------------------------- warp graph

type Warp struct {
	From     string
	X, Y     int
	To       string
	ToX, ToY int
	XS, YS   int    // trigger span: rAthena warps fire on any cell within X±XS, Y±YS
	NPC      string // ferry.go: not a warp but a talk NPC at X,Y; the menu option to pick (prefix)
	Fare     int    // ferry.go: zeny the NPC charges
}

var warpsFrom = map[string][]Warp{}

// Warp avoidance. A warp fires on every cell of its area, not just its centre, and the server warps a bot that merely
// walks across it. A* used to ignore warps, so a path that brushed past one teleported the bot away: ~50 Alberta
// Novices landed at alberta 102,153 (exit of alberta_in), walked west towards pay_fild03, crossed the 5x5 area of
// the warp at 98,153 and were sent straight back into alberta_in 185,89, forever. Now FindPath never steps into a
// warp area (except the one it is heading for or standing in) and pays extra for the ring around it, because the
// server re-paths each short walk segment on its own and could otherwise clip a corner.
var wmaskMu sync.Mutex

func (m *GameMap) warpMask() []byte {
	wmaskMu.Lock()
	defer wmaskMu.Unlock()
	if m.wmask != nil {
		return m.wmask
	}
	mask := make([]byte, m.W*m.H)
	for _, w := range warpsFrom[m.Name] {
		for y := w.Y - w.YS - 1; y <= w.Y+w.YS+1; y++ {
			for x := w.X - w.XS - 1; x <= w.X+w.XS+1; x++ {
				if x < 0 || y < 0 || x >= m.W || y >= m.H {
					continue
				}
				v := byte(1)
				if abs(x-w.X) <= w.XS && abs(y-w.Y) <= w.YS {
					v = 2
				}
				if k := x + y*m.W; mask[k] < v {
					mask[k] = v
				}
			}
		}
	}
	m.wmask = mask
	return mask
}

// warpBoxesAt: the warps (with their ring) covering (x, y) on this map; a search may use those cells freely.
func (m *GameMap) warpBoxesAt(x, y int) []Warp {
	var out []Warp
	for _, w := range warpsFrom[m.Name] {
		if abs(x-w.X) <= w.XS+1 && abs(y-w.Y) <= w.YS+1 {
			out = append(out, w)
		}
	}
	return out
}

func inWarpBoxes(ws []Warp, x, y int) bool {
	for _, w := range ws {
		if abs(x-w.X) <= w.XS+1 && abs(y-w.Y) <= w.YS+1 {
			return true
		}
	}
	return false
}

// OnWarp reports whether stepping on (x, y) would trigger a warp.
func (m *GameMap) OnWarp(x, y int) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return false
	}
	return m.warpMask()[x+y*m.W] == 2
}

// ---------------------------------------------------------------- connected regions
// A map can be split into regions that can't reach each other on foot (walls, cliffs, interiors behind
// warps). Routing works on (map, region) nodes so a route never leads into a pocket with no way on.

var compMu sync.Mutex

func (m *GameMap) components() []uint16 {
	compMu.Lock()
	defer compMu.Unlock()
	if m.comp != nil {
		return m.comp
	}
	lab := make([]uint16, m.W*m.H)
	var next uint16
	stack := make([]int, 0, 1024)
	for i := range lab {
		if lab[i] != 0 || !m.Walkable(i%m.W, i/m.W) {
			continue
		}
		next++
		lab[i] = next
		stack = append(stack[:0], i)
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cx, cy := c%m.W, c/m.W
			for _, d := range dirs8 {
				nx, ny := cx+d[0], cy+d[1]
				if !m.Walkable(nx, ny) {
					continue
				}
				if d[0] != 0 && d[1] != 0 && (!m.Walkable(cx+d[0], cy) || !m.Walkable(cx, cy+d[1])) {
					continue
				}
				if k := nx + ny*m.W; lab[k] == 0 {
					lab[k] = next
					stack = append(stack, k)
				}
			}
		}
	}
	m.comp = lab
	return lab
}

// Region returns the region id at (x,y); warp cells themselves are often unwalkable, so look around them.
func (m *GameMap) Region(x, y int) uint16 {
	lab := m.components()
	for r := 0; r <= 3; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				nx, ny := x+dx, y+dy
				if nx >= 0 && ny >= 0 && nx < m.W && ny < m.H {
					if l := lab[nx+ny*m.W]; l != 0 {
						return l
					}
				}
			}
		}
	}
	return 0
}

// ---------------------------------------------------------------- danger

// mapDanger: how risky crossing/hunting a map is for a character level: aggressive monsters above the level,
// plus recent deaths reported by any bot (decays with a ~20 minute half-life).
var deathLog = struct {
	sync.Mutex
	m map[string]float64
	t map[string]time.Time
}{m: map[string]float64{}, t: map[string]time.Time{}}

func reportDeath(m string) {
	deathLog.Lock()
	defer deathLog.Unlock()
	deathLog.m[m] = recentDeathsLocked(m) + 1
	deathLog.t[m] = time.Now()
}

func recentDeathsLocked(m string) float64 {
	v := deathLog.m[m]
	if v == 0 {
		return 0
	}
	return v * math.Pow(0.5, time.Since(deathLog.t[m]).Minutes()/20)
}

func recentDeaths(m string) float64 {
	deathLog.Lock()
	defer deathLog.Unlock()
	return recentDeathsLocked(m)
}

func mapDanger(m string, level int) float64 {
	d := 0.0
	for _, s := range spawnsByMap[m] {
		mb := mobs[s.MobID]
		if mb == nil || !mb.Aggressive || s.Count <= 0 {
			continue
		}
		if over := mb.Level - level; over > -5 {
			d += float64(s.Count) * float64(over+5) / 20
		}
	}
	return d + recentDeaths(m)*3
}

// RouteMaps returns the warps to take from (a, ax, ay) to any walkable spot on map b, nil if unreachable.
// Dijkstra over (map, region) nodes; each map entered costs 10 + its danger for the given level, so routes
// bend around death traps when a reasonable detour exists.
// badWarps: warps found to loop (a bot bounced A->B->A->B through them, e.g. alberta 102,153 <-> alberta_in
// 185,89 kept ~30 Alberta Novices "travelling" forever). Skipped by RouteMaps until the expiry.
var badWarps sync.Map // "map:x:y" -> time.Time (expiry)

func warpKey(m string, x, y int) string { return fmt.Sprintf("%s:%d:%d", m, x, y) }

func warpBad(w Warp) bool {
	if v, ok := badWarps.Load(warpKey(w.From, w.X, w.Y)); ok {
		return time.Now().Before(v.(time.Time))
	}
	return false
}

// RouteMapsPoint: like RouteMaps, but arrives in the part of map b that can reach (bx, by) - for buildings with
// separate rooms. It may route out of map a and back in (a == b allowed).
func RouteMapsPoint(a string, ax, ay int, b string, bx, by int, level ...int) []Warp {
	mb := maps[b]
	if mb == nil {
		return nil
	}
	want := nearRegionCell(mb, bx, by)
	return routeMaps(a, ax, ay, b, want, level...)
}

// nearRegionCell: the walkable region of the cell, or of the nearest walkable cell (NPCs stand on blocked cells).
func nearRegionCell(gm *GameMap, x, y int) uint16 {
	for r := 0; r <= 3; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if gm.Walkable(x+dx, y+dy) {
					return gm.Region(x+dx, y+dy)
				}
			}
		}
	}
	return gm.Region(x, y)
}

func RouteMaps(a string, ax, ay int, b string, level ...int) []Warp {
	return routeMaps(a, ax, ay, b, 0, level...)
}

// routeMaps: wantRegion 0 = any region of b. Warps marked bad (loop detector) are avoided, but when one is the only
// way on (izlude_in 65,84 is the only door of the Izlude tool shop: a bounce marked it bad and Thief Ryu99 sat
// "no route" / roaming indoors for minutes) a route through it beats no route at all.
func routeMaps(a string, ax, ay int, b string, wantRegion uint16, level ...int) []Warp {
	return routeMapsSkip(a, ax, ay, b, wantRegion, nil, level...)
}

// routeMapsSkip: routeMaps that also avoids the warps skip() reports (one bot's own "this portal got me nowhere"
// list, travelStalled); like bad warps they are still used when nothing else gets there.
func routeMapsSkip(a string, ax, ay int, b string, wantRegion uint16, skip func(Warp) bool, level ...int) []Warp {
	if skip != nil {
		if r := routeMapsBad(a, ax, ay, b, wantRegion, false, skip, level...); r != nil {
			return r
		}
	}
	if r := routeMapsBad(a, ax, ay, b, wantRegion, false, nil, level...); r != nil {
		return r
	}
	return routeMapsBad(a, ax, ay, b, wantRegion, true, nil, level...)
}

func routeMapsBad(a string, ax, ay int, b string, wantRegion uint16, ignoreBad bool, skip func(Warp) bool, level ...int) []Warp {
	if a == b && (wantRegion == 0 || maps[a] == nil || maps[a].Region(ax, ay) == wantRegion) {
		return []Warp{}
	}
	ma := maps[a]
	if ma == nil {
		return nil
	}
	lv := 50
	if len(level) > 0 {
		lv = level[0]
	}
	type key struct {
		m string
		r uint16
	}
	type nodeT struct {
		k    key
		cost float64
		prev int
		via  Warp
	}
	nodes := []nodeT{{k: key{a, ma.Region(ax, ay)}, prev: -1}}
	index := map[key]int{nodes[0].k: 0}
	done := map[int]bool{}
	danger := map[string]float64{}
	for {
		// pick cheapest open node (graphs are small; linear scan is fine)
		cur, bc := -1, math.MaxFloat64
		for i := range nodes {
			if !done[i] && nodes[i].cost < bc {
				cur, bc = i, nodes[i].cost
			}
		}
		if cur < 0 || bc > 5000 {
			return nil
		}
		done[cur] = true
		c := nodes[cur]
		if c.k.m == b && (wantRegion == 0 || c.k.r == wantRegion) && cur != 0 {
			var out []Warp
			for i := cur; nodes[i].prev >= 0; i = nodes[i].prev {
				out = append(out, nodes[i].via)
			}
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out
		}
		cm := maps[c.k.m]
		for _, w := range routeEdges(c.k.m) { // ferry.go: warps + Sailor boats
			if (!ignoreBad && warpBad(w)) || (skip != nil && skip(w)) {
				continue
			}
			if cm == nil || cm.Region(w.X, w.Y) != c.k.r {
				continue // that warp isn't reachable from where we'd be standing
			}
			tm := maps[w.To]
			if tm == nil {
				continue
			}
			nk := key{w.To, tm.Region(w.ToX, w.ToY)}
			dg, ok := danger[w.To]
			if !ok {
				dg = mapDanger(w.To, lv) + swarmDanger(w.To, lv) // swarm.go: dense low aggressive packs too
				if lv > 0 && mapAggroTopLv(w.To) >= lv+10 && !isTown(w.To) {
					dg += 150 // aggressive monsters 10+ levels over us: worth a long detour (QA 2026-11-14 mjolnir_10/12 deaths)
				}
				danger[w.To] = dg
			}
			// danger bends routes around death traps but never makes travel impossible (a level 1 Novice still
			// walks from Morroc to Prontera, carefully)
			nc := c.cost + 10 + math.Min(dg, 300) + float64(w.Fare)/10
			if i, ok := index[nk]; ok {
				if !done[i] && nc < nodes[i].cost {
					nodes[i].cost, nodes[i].prev, nodes[i].via = nc, cur, w
				}
				continue
			}
			index[nk] = len(nodes)
			nodes = append(nodes, nodeT{k: nk, cost: nc, prev: cur, via: w})
		}
	}
}

// escapeNoRoute (under b.mu): no route from here to the goal (a pocket, an interior whose only door is marked bad,
// missing warp data). Don't drop the goal and roam indoors: fly home with a Butterfly Wing (the route is planned
// again from the save town), else walk out of the nearest door reachable from here. False = nothing to try.
func (b *Bot) escapeNoRoute(gm *GameMap, moving bool, now time.Time) bool {
	if gm == nil {
		return false
	}
	if idx := b.invIndex(itemButterflyWing); idx >= 0 && now.Sub(b.wingAt) > time.Minute {
		b.wingAt = now
		b.setState("no route to " + b.Goal.Map + ": flying home")
		b.event("info", "", fmt.Sprintf("no route from %s %d,%d to %s: Butterfly Wing home", b.MapName, b.X, b.Y, b.Goal.Map))
		b.wingEvent(itemButterflyWing, "no route", now)
		go b.useItem(idx)
		return true
	}
	reg := gm.Region(b.X, b.Y)
	var best *Warp
	for i, w := range warpsFrom[b.MapName] {
		if w.To == b.MapName || gm.Region(w.X, w.Y) != reg {
			continue
		}
		if best == nil || dist(b.X, b.Y, w.X, w.Y) < dist(b.X, b.Y, best.X, best.Y) {
			best = &warpsFrom[b.MapName][i]
		}
	}
	if best == nil {
		return false
	}
	b.setState("no route to " + b.Goal.Map + ": leaving " + b.MapName)
	if moving {
		return true
	}
	b.walkToPortal(gm, *best)
	return true
}

// walkToPortal walks to portal w on this map and steps in. b.lastWarp is set BEFORE the walk: safeWalkDest (the
// guard on every walk) lets a walk enter a trigger area only for b.lastWarp, so callers that set it only within 1
// cell of the centre never got in when the path entered the area 2 cells out - the bot stood at the door re-sending
// the walk (QA 2026-11-14: Thorn.rose at geffen 98-101,138 beside door 98,141 "town trip: go" for 5+ min, stuckWatch
// 15 times; Kimchiz izlude 112,179, Kaifire alberta 114,41, Holyshot payon 138,156).
func (b *Bot) walkToPortal(gm *GameMap, w Warp) {
	if w.NPC != "" {
		b.ferryStep(gm, w, time.Now()) // ferry.go: a boat, not a portal
		return
	}
	b.lastWarp = w
	tx, ty := portalCell(gm, b.X, b.Y, w)
	if dist(b.X, b.Y, tx, ty) <= 1 {
		go b.walkTo(tx, ty)
		return
	}
	b.stepAlong(gm, tx, ty)
}

// boxHitsWarp: does the box spanned by (x0,y0)-(x1,y1) contain a warp trigger cell that isn't part of the warp at
// (gx, gy) (the portal being walked to) or of a warp area the bot is standing in already.
func (m *GameMap) boxHitsWarp(x0, y0, x1, y1, gx, gy int) bool {
	exempt := m.exemptWarps(x0, y0, gx, gy)
	for y := min(y0, y1); y <= max(y0, y1); y++ {
		for x := min(x0, x1); x <= max(x0, x1); x++ {
			if m.foreignTrigger(x, y, exempt) {
				return true
			}
		}
	}
	return false
}

// exemptWarps: the warps whose trigger cells a walk from (x0, y0) toward (gx, gy) may use (the target portal, the
// area we stand in).
func (m *GameMap) exemptWarps(x0, y0, gx, gy int) []Warp {
	var exempt []Warp
	for _, w := range warpsFrom[m.Name] {
		if (abs(gx-w.X) <= w.XS && abs(gy-w.Y) <= w.YS) || (abs(x0-w.X) <= w.XS && abs(y0-w.Y) <= w.YS) {
			exempt = append(exempt, w)
		}
	}
	return exempt
}

// foreignTrigger: (x, y) is a warp trigger cell of a warp not in exempt.
func (m *GameMap) foreignTrigger(x, y int, exempt []Warp) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H || m.warpMask()[x+y*m.W] != 2 {
		return false
	}
	for _, w := range exempt {
		if abs(x-w.X) <= w.XS && abs(y-w.Y) <= w.YS {
			return false
		}
	}
	return true
}

// directLine: the cells (start excluded) of rAthena's "easy path" from (x0,y0) to (x1,y1): diagonal first, then
// straight. path.cpp path_search walks it whenever every cell on it is passable, before trying A*. ok=false: a
// cell is blocked, so the server plans its own A* path instead.
func (m *GameMap) directLine(x0, y0, x1, y1 int) ([][2]int, bool) {
	var out [][2]int
	x, y := x0, y0
	for (x != x1 || y != y1) && len(out) < 64 {
		x, y = x+sign(x1-x), y+sign(y1-y)
		if !m.Walkable(x, y) {
			return nil, false
		}
		out = append(out, [2]int{x, y})
	}
	return out, x == x1 && y == y1
}

// segHitsWarp: would a walk request from (x0,y0) to (x1,y1) put the bot on a trigger cell of a warp other than the
// one at (gx, gy) or the one it stands in. When the server's direct path is open only its cells count: the old box
// test also refused steps that pass beside a door without entering it (2026-11-14: bots landing at prontera 47,226,
// next to the west door 50,228 (x 48-52, y 226-230), had every step - even 48,225 - refused for ~11 min). When the
// direct path is blocked the server's A* may use any cell of the box, so the box test stands.
func (m *GameMap) segHitsWarp(x0, y0, x1, y1, gx, gy int) bool {
	line, ok := m.directLine(x0, y0, x1, y1)
	if !ok {
		return m.boxHitsWarp(x0, y0, x1, y1, gx, gy)
	}
	exempt := m.exemptWarps(x0, y0, gx, gy)
	for _, c := range line {
		if m.foreignTrigger(c[0], c[1], exempt) {
			return true
		}
	}
	return false
}

// warpTaken: the portal on map `from` that lands on map `to` nearest the arrival cell (x, y).
func warpTaken(from, to string, x, y int) (Warp, bool) {
	var best Warp
	bd := 1 << 30
	for _, w := range warpsFrom[from] {
		if w.To == to {
			if d := dist(x, y, w.ToX, w.ToY); d < bd {
				best, bd = w, d
			}
		}
	}
	return best, bd <= 6
}

// portalCell: the cell to walk to for portal w: its centre when walkable, else the walkable trigger cell of its area
// nearest its centre in the region of (x, y) (field-edge portals often sit on a blocked centre cell, and FindPath to a
// blocked goal returns nil: bots then only nudged randomly in a ~10-cell box for minutes, QA 2026-11-14).
func portalCell(m *GameMap, x, y int, w Warp) (int, int) {
	if m.Walkable(w.X, w.Y) {
		return w.X, w.Y
	}
	reg := m.Region(x, y)
	bx, by, bd := w.X, w.Y, 1<<30
	for dy := -w.YS; dy <= w.YS; dy++ {
		for dx := -w.XS; dx <= w.XS; dx++ {
			cx, cy := w.X+dx, w.Y+dy
			if !m.Walkable(cx, cy) || (reg != 0 && m.Region(cx, cy) != reg) {
				continue
			}
			if d := dist(w.X, w.Y, cx, cy); d < bd { // nearest the centre: a fixed target (no re-plans as we move)
				bx, by, bd = cx, cy, d
			}
		}
	}
	return bx, by
}

// travel progress watchdog. stuckWatch only sees a bot that doesn't move at all; a bot shuffling inside a ~15-cell
// box (random nudges around an unreachable target, a server path that keeps bending back) dodged it for 7-9 minutes
// (QA 2026-11-14: Shadow07 on moc_fild02, Sun.queen and Star claw on prt_fild08).
// Progress is measured along the planned walk (cells left on b.path), not in a straight line: Mjolnir's cliff maps
// wind (mjolnir_01 98,94 -> portal 378,256 is 918 path cells for 280 straight; mjolnir_02 270,250 -> 28,258 walks
// 666 cells without getting nearer as the crow flies), so bots walking a perfectly good path were declared stuck
// at ~214,129 every time: 156 "still stuck ... Butterfly Wing home" in 50 min, each one a trip home and back.
// No progress for 60 s: re-plan from scratch. 120 s: avoid that portal (this bot only, 10 min) and re-plan - another
// exit, or a Kafra town. 180 s: Butterfly Wing home; without one mark the portal bad for everyone (30 min).
// Routing still uses an avoided/bad portal when it is the only way on.
type travelProg struct {
	key    string
	best   int
	byPath bool // best counts cells left on the planned path (else straight distance, until a path exists)
	rebase bool // re-planned: the next path's length becomes the yardstick (not progress; the clock keeps running)
	since  time.Time
	last   time.Time // last call: travel paused (fighting, resting) restarts the clock
	tries  int
}

var travelProgs sync.Map // *Bot -> *travelProg

// travelAvoid: portals one bot gave up on (travelStalled try 2); "map:x:y" -> expiry. Used under b.mu.
var travelAvoid sync.Map // *Bot -> map[string]time.Time

func (b *Bot) avoidsWarp(w Warp) bool {
	v, ok := travelAvoid.Load(b)
	if !ok {
		return false
	}
	exp, ok := v.(map[string]time.Time)[warpKey(w.From, w.X, w.Y)]
	return ok && time.Now().Before(exp)
}

// avoidedOn: how many portals of map m this bot avoids right now (after two, stop trying exits and wing home).
func (b *Bot) avoidedOn(m string, now time.Time) int {
	v, ok := travelAvoid.Load(b)
	if !ok {
		return 0
	}
	n := 0
	for k, exp := range v.(map[string]time.Time) {
		if strings.HasPrefix(k, m+":") && now.Before(exp) {
			n++
		}
	}
	return n
}

// routeFor: RouteMaps from where the bot stands, skipping the portals this bot gave up on (when there is another way).
func (b *Bot) routeFor(dest string) []Warp {
	if _, ok := travelAvoid.Load(b); !ok {
		return RouteMaps(b.MapName, b.X, b.Y, dest, b.powerLv())
	}
	return routeMapsSkip(b.MapName, b.X, b.Y, dest, 0, b.avoidsWarp, b.powerLv())
}

// pathLeft: cells left on the planned walk to (tx, ty), -1 when there is no such path yet.
func (b *Bot) pathLeft(tx, ty int) int {
	if n := len(b.path); n > 0 && b.path[n-1] == [2]int{tx, ty} {
		return n
	}
	return -1
}

func (b *Bot) travelStalled(tx, ty int, now time.Time) bool {
	key := fmt.Sprintf("%s:%d:%d", b.MapName, tx, ty)
	d := dist(b.X, b.Y, tx, ty)
	left := b.pathLeft(tx, ty)
	v, _ := travelProgs.LoadOrStore(b, &travelProg{key: key, best: d, since: now})
	p := v.(*travelProg)
	if p.key != key || now.Sub(p.last) > 5*time.Second {
		*p = travelProg{key: key, best: d, since: now, last: now}
		if left >= 0 {
			p.best, p.byPath = left, true
		}
		return false
	}
	p.last = now
	prog := d
	switch {
	case left >= 0 && (!p.byPath || p.rebase): // first (or re-planned) path: count along it from now on
		if !p.byPath {
			p.since = now
		}
		p.best, p.byPath, p.rebase = left, true, false
		return false
	case left >= 0:
		prog = left
	case p.byPath:
		prog = 1 << 30 // path dropped (re-planning): no progress until a new one exists
	}
	if prog < p.best-2 {
		p.best, p.since, p.tries = prog, now, 0
		return false
	}
	if now.Sub(p.since) < 60*time.Second {
		return false
	}
	p.since = now
	p.tries++
	w := b.lastWarp
	if left >= 0 {
		d = left
	}
	switch p.tries {
	case 1:
		b.event("info", "", fmt.Sprintf("no progress toward the portal at %s %d,%d for 60s (%d cells away): re-planning", b.MapName, tx, ty, d))
		b.route, b.path = nil, nil
		p.rebase = true
		return true
	case 2:
		if w.From == b.MapName && b.avoidedOn(b.MapName, now) < 2 {
			m, _ := travelAvoid.Load(b)
			am, _ := m.(map[string]time.Time)
			if am == nil {
				am = map[string]time.Time{}
			}
			for k, exp := range am {
				if now.After(exp) {
					delete(am, k)
				}
			}
			am[warpKey(w.From, w.X, w.Y)] = now.Add(10 * time.Minute)
			travelAvoid.Store(b, am)
			b.event("info", "", fmt.Sprintf("still stuck %d cells from the portal at %s %d,%d: trying another way", d, w.From, w.X, w.Y))
			b.route, b.path = nil, nil
			p.rebase = true
			b.kafraNext = time.Time{} // a Kafra town may be the other way
			return true
		}
	}
	if gm := maps[b.MapName]; gm != nil && gm.enclosed(b.X, b.Y) {
		return false // walled in: stepAlong uses a Fly Wing; a Butterfly Wing could land on the same cell again
	}
	// QA 2026-11-14 10:17 UTC: this was a Butterfly Wing home (a town trip, then the whole walk again). A Fly Wing
	// (same map, a new cell, re-planned from there) is enough for a slow route; no wing = mark the portal bad below.
	if p.tries == 3 && now.Sub(b.wingAt) > time.Minute && b.escItemOK(itemFlyWing, now) {
		b.wingAt = now
		b.event("info", "", fmt.Sprintf("still stuck %d cells from the portal at %s %d,%d: Fly Wing and re-plan", d, b.MapName, tx, ty))
		b.route, b.path = nil, nil
		p.rebase = true
		b.useEscapeItem(itemFlyWing, now, "travel stalled 180s")
		return true
	}
	if w.From == b.MapName {
		ban := 30 * time.Minute
		if townGate(w) {
			ban = 3 * time.Minute // a town gate is never broken for long: one bot stuck near it must not cut a town off
		}
		badWarps.Store(warpKey(w.From, w.X, w.Y), now.Add(ban))
		hopCacheReset()
		b.event("info", "", fmt.Sprintf("still stuck %d cells from the portal at %s %d,%d: marked bad, trying another way", d, w.From, w.X, w.Y))
	}
	b.route, b.path = nil, nil
	return true
}

// safeWalkDest is the last guard before every walk request (bot.go walkTo). FindPath/stepAlong avoid warp areas, but
// many walks bypass them: closeIn chasing a monster, looting, following, fleeing, roaming. A monster or a drop
// inside a portal's area (they don't trigger warps, players do) pulled the bot in and the server warped it away the
// moment it landed (QA 2026-11-14 after the Alberta fix: 508 loops in 8 min - alberta <-> pay_fild03, landing 388,63
// next to the area of payf005 392,63 (x 390-394, y 56-70); geffen <-> gef_fild04). Rule: a walk may end in or cross a
// trigger area only when it is the portal we mean to take (b.lastWarp on this map) or the one we stand in. A
// destination inside another area moves to the nearest safe cell; a straight walk whose box clips an area follows
// the A* path (which avoids it) as far as the box stays clear. Returns ok=false when there is no safe walk.
func (b *Bot) safeWalkDest(x, y int) (rx, ry int, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer func() {
		if !ok && b.buffAt != nil {
			b.buffAt["walkrefused"] = time.Now() // rally.go stuckWatch: a refused walk counts as trying to walk
			if b.tracing() {
				b.tracef("walk to %d,%d refused (warp area in the way) at %d,%d lastWarp=%v", x, y, b.X, b.Y, b.lastWarp)
			}
		}
	}()
	gm := maps[b.MapName]
	if gm == nil || len(warpsFrom[b.MapName]) == 0 {
		return x, y, true
	}
	mx, my := b.myPos(time.Now())
	w := b.lastWarp
	intended := w.From == b.MapName && abs(x-w.X) <= w.XS && abs(y-w.Y) <= w.YS
	if intended {
		return x, y, true // stepping into the route's portal
	}
	if gm.OnWarp(x, y) && !gm.sameWarpArea(mx, my, x, y) {
		bx, by, bd := 0, 0, 1<<30
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				cx, cy := x+dx, y+dy
				if gm.Walkable(cx, cy) && !gm.OnWarp(cx, cy) {
					if d := dist(x, y, cx, cy)*100 + dist(mx, my, cx, cy); d < bd {
						bx, by, bd = cx, cy, d
					}
				}
			}
		}
		if bd == 1<<30 {
			return 0, 0, false
		}
		x, y = bx, by
	}
	if x == mx && y == my {
		return 0, 0, false // nowhere safe to go (target inside a warp area right next to us)
	}
	if !gm.segHitsWarp(mx, my, x, y, x, y) {
		return x, y, true
	}
	p := gm.FindPath(mx, my, x, y, 20000)
	for n := min(len(p), 14); n > 0; n-- {
		if !gm.segHitsWarp(mx, my, p[n-1][0], p[n-1][1], x, y) {
			return p[n-1][0], p[n-1][1], true
		}
	}
	if dx, dy, ok := gm.detourStep(mx, my, x, y); ok {
		return dx, dy, true // every step refused: a short safe step instead of standing still
	}
	return 0, 0, false
}

// detourStep: when every step toward (x, y) would cross a foreign warp area, a safe cell within 3 cells of (mx, my)
// (walkable, same region, off any trigger area, reachable without crossing one): nearest the target, ties farthest
// from the warp areas. Standing still re-sent the same refused walk until stuckWatch fired (30 s+ per try).
func (m *GameMap) detourStep(mx, my, x, y int) (int, int, bool) {
	reg := m.Region(mx, my)
	wm := m.warpMask()
	bx, by, bd := 0, 0, 1<<30
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			cx, cy := mx+dx, my+dy
			if (dx == 0 && dy == 0) || !m.Walkable(cx, cy) || m.OnWarp(cx, cy) || (reg != 0 && m.Region(cx, cy) != reg) ||
				m.segHitsWarp(mx, my, cx, cy, cx, cy) {
				continue
			}
			d := dist(cx, cy, x, y) * 10
			if wm[cx+cy*m.W] == 1 { // the ring next to an area
				d += 5
			}
			if d < bd {
				bx, by, bd = cx, cy, d
			}
		}
	}
	return bx, by, bd != 1<<30
}

// sameWarpArea: (x0,y0) and (x1,y1) are in the trigger area of one warp (walking out of / within the area we stand in).
func (m *GameMap) sameWarpArea(x0, y0, x1, y1 int) bool {
	for _, w := range warpsFrom[m.Name] {
		if abs(x0-w.X) <= w.XS && abs(y0-w.Y) <= w.YS && abs(x1-w.X) <= w.XS && abs(y1-w.Y) <= w.YS {
			return true
		}
	}
	return false
}

// talkCell: where to stand to talk to the NPC at (nx, ny): the walkable cell in the region of (x, y) nearest the NPC
// (ties: nearest to us), within 6 cells of it. Shop NPCs stand behind counters; the old target "one cell beside the
// NPC" was often a wall or the far side of the counter, and FindPath to it failed every 60 s for minutes (QA
// 2026-11-14: izlude_in 61,126, alberta_in 189,20, geffen_in 31,177). rAthena talks to an NPC from up to
// AREA_SIZE+1 cells without a line-of-sight check, so a cell in front of the counter is enough. ok=false: the NPC
// can't be approached from this region (wrong room).
func talkCell(m *GameMap, x, y, nx, ny int) (int, int, bool) {
	reg, lab := m.Region(x, y), m.components()
	bx, by, bd := 0, 0, 1<<30
	for dy := -6; dy <= 6; dy++ {
		for dx := -6; dx <= 6; dx++ {
			cx, cy := nx+dx, ny+dy
			if !m.Walkable(cx, cy) || m.OnWarp(cx, cy) || (reg != 0 && lab[cx+cy*m.W] != reg) {
				continue
			}
			if d := dist(nx, ny, cx, cy)*1000 + dist(x, y, cx, cy); d < bd {
				bx, by, bd = cx, cy, d
			}
		}
	}
	return bx, by, bd != 1<<30
}

// enclosed: (x, y) is a cell no step can leave (a blocked cell walled in on all sides, with the same no-corner-cutting
// rule as FindPath and rAthena). Seeded Novices spawned at "town spawn +-3" without a walkability check: geffen
// 116,56 is inside the fountain's wall, and Silver_storm / xXSun07Xx21 / TheKimchisoul stood there "travel ->
// gef_fild07" for 11+ minutes (stuckWatch 33 tries). Only a teleport gets out.
func (m *GameMap) enclosed(x, y int) bool {
	if m.Walkable(x, y) {
		return false
	}
	for _, d := range dirs8 {
		nx, ny := x+d[0], y+d[1]
		if !m.Walkable(nx, ny) {
			continue
		}
		if d[0] != 0 && d[1] != 0 && (!m.Walkable(x+d[0], y) || !m.Walkable(x, y+d[1])) {
			continue
		}
		return false
	}
	return true
}

// regroup progress per member: failed path searches toward the leader and the last call for help
type regroupProg struct {
	fails  int
	waitTo time.Time // waiting (asked the leader to come) until then; then try again
	askAt  time.Time
}

var regroupProgs sync.Map // *Bot -> *regroupProg

// regroupStep (b.mu held, not moving): walk toward the leader on this map. QA 2026-11-14: Sun2k and LilPuddingheart
// logged "no path on prt_fild04 from 194,342 to 58,139 (regrouping with theravenheart)" 5x each - prt_fild04 is two
// walk regions (12,773 and 25,629 cells, split by the river) and the leader stood in the other one, so FindPath
// failed forever. Now: another region -> go round through portals (RouteMapsPoint plans out and back in, travel keeps
// the plan across the maps); a leader cell that is blocked -> the nearest walkable cell of its region; after 2 failed
// searches (or no route round) stand, say so in party chat and wait 30 s before trying again (owner rule: members ask
// for help, the leader collects them).
func (b *Bot) regroupStep(gm *GameMap, lead pubState, now time.Time) {
	v, _ := regroupProgs.LoadOrStore(b, &regroupProg{})
	rp := v.(*regroupProg)
	if now.Before(rp.waitTo) {
		b.setState("regrouping with " + lead.Name + ": waiting (can't get there)")
		return
	}
	reg, lr := gm.Region(b.X, b.Y), nearRegionCell(gm, lead.X, lead.Y)
	if reg != 0 && lr != 0 && reg != lr {
		if len(b.route) == 0 || b.route[0].From != b.MapName || b.route[len(b.route)-1].To != b.MapName {
			b.route = RouteMapsPoint(b.MapName, b.X, b.Y, b.MapName, lead.X, lead.Y, b.BaseLvOr())
			b.path = nil
			if len(b.route) > 0 {
				b.event("info", "", fmt.Sprintf("%s is across %s from me (other walk region): going round via %d portals", lead.Name, b.MapName, len(b.route)))
			}
		}
		if n := len(b.route); n > 0 && n <= 4 {
			b.setState("regrouping with " + lead.Name + " (going round)")
			b.walkToPortal(gm, b.route[0])
			return
		}
		b.route = nil
		b.regroupFailed(rp, lead, "no way round to its side of "+b.MapName, now)
		return
	}
	tx, ty := lead.X, lead.Y
	if !gm.Walkable(tx, ty) { // a stale or off-mesh position: the nearest walkable cell of our region next to it
		lab, bd := gm.components(), 1<<30
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				cx, cy := lead.X+dx, lead.Y+dy
				if gm.Walkable(cx, cy) && (reg == 0 || lab[cx+cy*gm.W] == reg) && dist(lead.X, lead.Y, cx, cy) < bd {
					tx, ty, bd = cx, cy, dist(lead.X, lead.Y, cx, cy)
				}
			}
		}
	}
	before := b.replanAt
	b.stepAlong(gm, tx, ty)
	if b.path == nil && b.replanAt != before {
		rp.fails++
		if rp.fails >= 2 {
			b.regroupFailed(rp, lead, fmt.Sprintf("no path from %d,%d to %d,%d", b.X, b.Y, tx, ty), now)
		}
	} else if b.path != nil {
		rp.fails = 0
	}
}

func (b *Bot) regroupFailed(rp *regroupProg, lead pubState, why string, now time.Time) {
	rp.fails, rp.waitTo = 0, now.Add(30*time.Second)
	b.path = nil
	b.setState("regrouping with " + lead.Name + ": waiting (can't get there)")
	if now.Sub(rp.askAt) > 2*time.Minute {
		rp.askAt = now
		b.event("info", "", fmt.Sprintf("can't reach %s on %s (%s): waiting, asking to be collected", lead.Name, b.MapName, why))
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("cant get to u, can u come to me?", "stuck, come get me pls", "cant reach u from here, wait for me or come", "can't find a way to u, come here?")))
	}
}

// sameWalkRegion: (x, y) on this map can be walked to from where the bot stands (same connected walk region). A
// leader "collecting" a member across a river walked into FindPath failures while the member waited for it.
func (b *Bot) sameWalkRegion(gm *GameMap, x, y int) bool {
	if gm == nil {
		return true
	}
	r, t := gm.Region(b.X, b.Y), nearRegionCell(gm, x, y)
	return r == 0 || t == 0 || r == t
}

// answerStrayDialog (b.mu held): an NPC dialog that no feature is driving (not a Kafra trip in its dialog stage, not
// the alchemist's Guild Dealer) is clicked through: next -> next, menu -> cancel, close -> close. rAthena's
// pc_cant_act() refuses walking AND item use while sd->npc_id is set, so a dialog left open froze the bot: stuckWatch
// (rally.go) only closed it every 20 s, and its Butterfly Wings were refused too (QA 2026-11-14: "npc 110000000" in
// every stuck report - rAthena's fake NPC that runs item scripts - and Silver_storm's two wing uses at geffen 116,56
// left the wing count unchanged).
func (b *Bot) answerStrayDialog(op uint16) {
	if k := b.kafra; k != nil && k.Stage == "dialog" {
		return
	}
	if v, ok := alchStates.Load(b); ok && v.(*alchState).Stage == "dealer" {
		return
	}
	if b.resetDialogOwned() {
		return // reset.go: the Hypnotist
	}
	if b.ferryDialogOwned(time.Now()) {
		return // ferry.go: the Sailor
	}
	id := b.dialogNPC
	if id == 0 {
		return
	}
	switch op {
	case 0x00b5:
		go b.send(newPkt(0x00b9).u32(id).b)
	case 0x00b6:
		b.dialogNPC = 0
		go b.send(newPkt(0x0146).u32(id).b)
	case 0x00b7:
		ans := byte(255)
		if c := b.dietrichMenu(); c != 0 {
			ans = c // dietrich.go: "View basic smelting ores"
		}
		if c := b.pecoMenu(); c != 0 {
			ans = c // peco.go: "Rent Peco Peco"
		}
		go b.send(newPkt(0x00b8).u32(id).u8(ans).b)
	}
}

// unwall (b.mu held): the bot stands on a cell no step can leave (gm.enclosed). Every 30 s: close any NPC dialog
// first (rAthena refuses item use while sd->npc_id is set - QA after the 01:15 deploy: 23 "walled in ...: wing" in a
// row, 0 wings consumed), stand up, then a Fly Wing (random cell of this map; never a Butterfly Wing - the save
// point can be the same blocked spawn cell). No Fly Wing, or still walled in after 2 wings: log out and stay out 3
// minutes - rAthena's pc_setpos keeps a blocked login position (it only re-rolls 0,0 / out-of-bounds), so
// tools/unwall.py moves the offline char's row to a walkable cell meanwhile.
type unwallProg struct {
	at    time.Time
	tries int
}

var unwallProgs sync.Map // *Bot -> *unwallProg

func (b *Bot) unwall() {
	now := time.Now()
	v, _ := unwallProgs.LoadOrStore(b, &unwallProg{})
	up := v.(*unwallProg)
	if now.Sub(up.at) < 30*time.Second {
		return
	}
	if now.Sub(up.at) > 10*time.Minute {
		up.tries = 0 // a new episode
	}
	up.at = now
	up.tries++
	b.setState("walled in: getting out")
	idx := b.invIndex(itemFlyWing)
	if idx < 0 || up.tries > 2 {
		b.event("info", "", fmt.Sprintf("walled in at %s %d,%d (fly wings %d, tries %d): logging out 3 min so tools/unwall.py can move me",
			b.MapName, b.X, b.Y, b.countItem(itemFlyWing), up.tries))
		up.tries = 0
		b.unwallUntil = now.Add(3 * time.Minute)
		go func() {
			b.wmu.Lock()
			if b.conn != nil {
				b.conn.Close() // the run loop sees the read error, logs off and waits until unwallUntil
			}
			b.wmu.Unlock()
		}()
		return
	}
	npc, sitting := b.dialogNPC, b.Sitting
	b.dialogNPC = 0
	b.event("info", "", fmt.Sprintf("walled in at %s %d,%d (blocked cell, no way out): Fly Wing (try %d, npc %d)", b.MapName, b.X, b.Y, up.tries, npc))
	b.wingEvent(itemFlyWing, "walled in", now)
	go func() {
		if npc != 0 { // whatever state the dialog is in: next, cancel the menu, close
			b.send(newPkt(0x00b9).u32(npc).b)
			b.send(newPkt(0x00b8).u32(npc).u8(255).b)
			b.send(newPkt(0x0146).u32(npc).b)
		}
		b.send(newPkt(0x00e3).b) // leave a chat room (also blocks items)
		if sitting {
			b.stand()
		}
		time.Sleep(time.Second)
		b.useItem(idx)
	}()
}
