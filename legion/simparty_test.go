package main

// Headless party simulation (pilot). An hour of party life runs in milliseconds on a virtual clock: no rAthena, no
// sockets, no time.Now. The party's decisions go through the engine's own helpers (together.go regroupNeeded /
// needsCollect, nav.go FindPath / Region / nearRegionCell, tuning.go values); the loop around them mirrors the bot
// leader's collect (ai.go "a bot party's leader collects stragglers one by one") and the member's regroup (party.go
// "regroup first"). Chaos (frozen members, sealed pockets, deaths) is injected from a seed, so every run replays
// exactly, and the checks are invariants over many seeds instead of one live observation.

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	simTick     = 150 * time.Millisecond // one cell per tick: a bot's walk speed (150 ms per cell)
	simFightFor = 2 * time.Second        // time to kill one monster
	simRespawn  = 30 * time.Second       // a killed monster comes back somewhere else
	simMobs     = 40
	simMapW     = 120
	simMapH     = 120
	simMembers  = 4 // plus the leader
	simMap      = "sim_fild"
	// ruleTogetherCells: the owner's rule PT06 (docs/rules/parties.md), the yardstick for "apart". Fixed on purpose:
	// measured against the tunable together_cells, a looser tuning would loosen its own check.
	ruleTogetherCells = 20
)

// simPolicy: the leader's straggler rule under test.
type simPolicy int

const (
	policyEngine simPolicy = iota // today's rule (ai.go): collect any same-region straggler, again and again
	policyLadder                  // candidate: collect, and a member that stays frozen after the leader reached it is left to catch up
)

func (p simPolicy) String() string { return [...]string{"engine", "ladder"}[p] }

// simChaos: how often (mean time per member) things go wrong. Zero = never.
type simChaos struct {
	name                  string
	freezeEvery, pocketEv time.Duration
	deathEvery            time.Duration
	ambushEvery           time.Duration // pinned in a fight away from the party: HP drains unless the party is near
}

type simAgent struct {
	name       string
	x, y       int
	path       [][2]int
	goal       [2]int
	replanAt   time.Time
	frozenTo   time.Time // can't move: a server-side dialog, overweight, a stuck client (rally.go stuckWatch cases)
	pocketTo   time.Time // sealed in a spot with no way out on foot (another walk region)
	deadTo     time.Time // dead and offline until respawn
	fightTo    time.Time
	fightMob   int
	regrouping bool
	lastMoveAt time.Time
	hp         float64
	ambushTo   time.Time // in a fight it can't leave (Target set); loses HP while alone
	waitAt     time.Time // party.go PT32b: holding still for the leader's collect since
	waitD      int
	reachedAt  time.Time // ladder: when the leader last stood next to it during a collect
	leftTo     time.Time // ladder: the leader does not collect it until then
}

type simMob struct {
	x, y    int
	aliveAt time.Time
}

type simResult struct {
	Kills      int
	ApartPct   float64       // share of online member time spent beyond together_cells of the leader
	MobileApar float64       // the same, counting only members able to walk (not frozen, not sealed in)
	MaxStall   time.Duration // longest stretch without a party kill
	CollectPct float64       // share of leader time spent collecting
	Deaths     int
}

type simWorld struct {
	gm      *GameMap
	rng     *rand.Rand // the world (map, monsters, roaming)
	crng    *rand.Rand // chaos only: both policies on one seed face the same events
	now     time.Time
	policy  simPolicy
	chaos   simChaos
	lead    *simAgent
	members []*simAgent
	mobs    []simMob
	pocket  [2]int
	// leader collect state, as on Bot (collectName / collectAt)
	collectName string
	collectAt   time.Time
	// metrics
	kills, apart, online, collecting, ticks int
	mobile, mobileApart, deaths             int
	lastKill                                time.Time
	maxStall                                time.Duration
}

// simGameMap: border walls, random wall blocks, only the largest walk region kept, plus one sealed 3x3 pocket.
func simGameMap(rng *rand.Rand) (*GameMap, [2]int) {
	gm := &GameMap{Name: simMap, W: simMapW, H: simMapH, cells: make([]byte, simMapW*simMapH)}
	set := func(x, y int, c byte) { gm.cells[x+y*gm.W] = c }
	for x := 0; x < gm.W; x++ {
		set(x, 0, 1)
		set(x, gm.H-1, 1)
	}
	for y := 0; y < gm.H; y++ {
		set(0, y, 1)
		set(gm.W-1, y, 1)
	}
	for i := 0; i < 70; i++ {
		x0, y0, w, h := 10+rng.Intn(gm.W-20), 10+rng.Intn(gm.H-20), 2+rng.Intn(10), 2+rng.Intn(10)
		for y := y0; y < min(y0+h, gm.H-1); y++ {
			for x := x0; x < min(x0+w, gm.W-1); x++ {
				set(x, y, 1)
			}
		}
	}
	lab := gm.components()
	count := map[uint16]int{}
	for _, l := range lab {
		count[l]++
	}
	best, bn := uint16(0), 0
	for l, n := range count {
		if l != 0 && n > bn {
			best, bn = l, n
		}
	}
	for i, l := range lab {
		if l != best {
			gm.cells[i] = 1
		}
	}
	for y := 1; y <= 7; y++ { // the pocket: a walled 3x3 room in the top-left corner
		for x := 1; x <= 7; x++ {
			c := byte(1)
			if x >= 3 && x <= 5 && y >= 3 && y <= 5 {
				c = 0
			}
			set(x, y, c)
		}
	}
	gm.comp = nil
	return gm, [2]int{4, 4}
}

func newSimWorld(seed int64, policy simPolicy, chaos simChaos) *simWorld {
	rng := rand.New(rand.NewSource(seed))
	gm, pocket := simGameMap(rng)
	w := &simWorld{gm: gm, rng: rng, crng: rand.New(rand.NewSource(seed ^ 0x5eed)), policy: policy, chaos: chaos, pocket: pocket,
		now: time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)}
	w.lastKill = w.now
	sx, sy := w.mainCell()
	w.lead = &simAgent{name: "Leader", x: sx, y: sy, lastMoveAt: w.now}
	for i := 0; i < simMembers; i++ {
		w.members = append(w.members, &simAgent{name: fmt.Sprintf("Member%d", i+1), x: sx + i%2, y: sy + i/2, lastMoveAt: w.now, hp: 1})
	}
	for i := 0; i < simMobs; i++ {
		x, y := w.mainCell()
		w.mobs = append(w.mobs, simMob{x: x, y: y, aliveAt: w.now})
	}
	return w
}

// mainCell: a random walkable cell of the main walk region (never the pocket).
func (w *simWorld) mainCell() (int, int) { return w.cellFrom(w.rng) }

func (w *simWorld) cellFrom(rng *rand.Rand) (int, int) {
	pr := w.gm.Region(w.pocket[0], w.pocket[1])
	for {
		x, y := 1+rng.Intn(w.gm.W-2), 1+rng.Intn(w.gm.H-2)
		if w.gm.Walkable(x, y) && w.gm.Region(x, y) != pr {
			return x, y
		}
	}
}

func (w *simWorld) can(a *simAgent) bool {
	return !w.now.Before(a.deadTo) && !w.now.Before(a.frozenTo) && !w.now.Before(a.pocketTo)
}

// pub: what the leader reads about a member (party.go pubState), as published after the member's think.
func (w *simWorld) pub(a *simAgent) pubState {
	s := pubState{Name: a.name, Map: simMap, X: a.x, Y: a.y, LX: a.x, LY: a.y, HPFrac: a.hp, Online: !w.now.Before(a.deadTo)}
	if w.now.Before(a.fightTo) || w.now.Before(a.ambushTo) {
		s.Target = uint32(a.fightMob + 1)
	}
	return s
}

// sameWalkRegion: nav.go Bot.sameWalkRegion from (x, y).
func (w *simWorld) sameWalkRegion(x, y, tx, ty int) bool {
	r, t := w.gm.Region(x, y), nearRegionCell(w.gm, tx, ty)
	return r == 0 || t == 0 || r == t
}

// walkToward: one step along a path to (tx, ty); replans when the goal moved, at most once a second.
func (w *simWorld) walkToward(a *simAgent, tx, ty int) {
	if dist(a.goal[0], a.goal[1], tx, ty) > 2 || len(a.path) == 0 {
		if w.now.Before(a.replanAt) && len(a.path) > 0 {
			goto step
		}
		a.replanAt = w.now.Add(time.Second)
		a.goal = [2]int{tx, ty}
		a.path = w.gm.FindPath(a.x, a.y, tx, ty, 80000)
	}
step:
	if len(a.path) > 0 {
		a.x, a.y = a.path[0][0], a.path[0][1]
		a.path = a.path[1:]
		a.lastMoveAt = w.now
	}
}

// fightNear: start or finish a fight with the nearest live monster within r cells of (cx, cy); true while busy.
func (w *simWorld) fightNear(a *simAgent, cx, cy, r int) bool {
	if w.now.Before(a.fightTo) {
		return true
	}
	if a.fightMob >= 0 && !a.fightTo.IsZero() {
		if m := &w.mobs[a.fightMob]; !w.now.Before(m.aliveAt) {
			m.aliveAt = w.now.Add(simRespawn)
			m.x, m.y = w.mainCell()
			w.kills++
			if st := w.now.Sub(w.lastKill); st > w.maxStall {
				w.maxStall = st
			}
			w.lastKill = w.now
		}
		a.fightTo, a.fightMob = time.Time{}, -1
	}
	best, bd := -1, r+1
	for i, m := range w.mobs {
		if !w.now.Before(m.aliveAt) && dist(cx, cy, m.x, m.y) <= r {
			if d := dist(a.x, a.y, m.x, m.y); d < bd {
				best, bd = i, d
			}
		}
	}
	if best < 0 {
		return false
	}
	if bd <= 1 {
		a.fightMob, a.fightTo = best, w.now.Add(simFightFor)
		return true
	}
	w.walkToward(a, w.mobs[best].x, w.mobs[best].y)
	return true
}

// leaderThink mirrors ai.go's bot-party leader: keep the committed straggler for leader_collect_sec, otherwise pick the
// nearest one that needsCollect in the same walk region; nobody to collect -> hunt.
func (w *simWorld) leaderThink() {
	l := w.lead
	lx, ly := l.x, l.y
	var tgt *simAgent
	if w.collectName != "" && w.now.Sub(w.collectAt) < time.Duration(tune().LeaderCollectSec)*time.Second {
		for _, m := range w.members {
			if m.name == w.collectName && needsCollect(simMap, lx, ly, w.pub(m), true) && w.sameWalkRegion(lx, ly, m.x, m.y) {
				tgt = m
			}
		}
	}
	if tgt == nil {
		w.collectName = ""
		bd := 1 << 30
		for _, m := range w.members {
			if w.policy == policyLadder && w.now.Before(m.leftTo) && !memberInDanger(w.pub(m)) {
				continue
			}
			if needsCollect(simMap, lx, ly, w.pub(m), false) && w.sameWalkRegion(lx, ly, m.x, m.y) {
				if d := dist(lx, ly, m.x, m.y); d < bd {
					tgt, bd = m, d
				}
			}
		}
		if tgt != nil {
			w.collectName, w.collectAt = tgt.name, w.now
		}
	}
	if tgt != nil {
		w.collecting++
		if w.policy == policyLadder {
			w.ladder(tgt)
		}
		w.walkToward(l, tgt.x, tgt.y)
		return
	}
	if !w.fightNear(l, lx, ly, 30) {
		x, y := w.mainCell()
		w.walkToward(l, x, y)
	}
}

// ladder (candidate exception rule): a straggler the leader has already reached, and that still has not moved for a
// minute, is left to catch up on its own for 5 minutes; the party keeps hunting. A member that moves is never left.
func (w *simWorld) ladder(m *simAgent) {
	if memberInDanger(w.pub(m)) {
		return // never leave a member that is losing a fight (as together.go leftAloneOnTrip)
	}
	if dist(w.lead.x, w.lead.y, m.x, m.y) <= togetherCells() { // a committed collect runs until collect_stop_cells
		if m.reachedAt.Before(m.lastMoveAt) {
			m.reachedAt = w.now
		}
	}
	if !m.reachedAt.IsZero() && !m.reachedAt.Before(m.lastMoveAt) && w.now.Sub(m.lastMoveAt) >= time.Minute {
		m.leftTo = w.now.Add(5 * time.Minute)
		w.collectName = ""
	}
}

// memberThink mirrors party.go: beyond together_cells -> regroup (walk to the leader, or wait when there is no way);
// otherwise fight near the leader.
func (w *simWorld) memberThink(m *simAgent) {
	if w.now.Before(m.ambushTo) {
		return // pinned in its fight
	}
	d := dist(m.x, m.y, w.lead.x, w.lead.y)
	if regroupNeeded(d, m.regrouping) && !w.now.Before(m.fightTo) {
		m.regrouping = true
		if w.collectName == m.name { // PT32b: the leader is coming - hold still for 30 s, longer while it gets 5+ closer
			if m.waitAt.IsZero() || w.now.Sub(m.waitAt) > 90*time.Second {
				m.waitAt, m.waitD = w.now, d
			}
			if w.now.Sub(m.waitAt) < 30*time.Second || d <= m.waitD-5 {
				if d <= m.waitD-5 {
					m.waitAt, m.waitD = w.now, d
				}
				return
			}
		}
		if w.sameWalkRegion(m.x, m.y, w.lead.x, w.lead.y) {
			w.walkToward(m, w.lead.x, w.lead.y)
		}
		return
	}
	m.regrouping = false
	if !w.fightNear(m, w.lead.x, w.lead.y, 8) && d > 4 {
		w.walkToward(m, w.lead.x, w.lead.y)
	}
}

func (w *simWorld) chance(mean time.Duration) bool {
	return mean > 0 && w.crng.Float64() < float64(simTick)/float64(mean)
}

// inject: chaos for one member this tick.
func (w *simWorld) inject(m *simAgent) {
	if !w.can(m) {
		if !w.now.Before(m.pocketTo) && m.x <= 7 && m.y <= 7 { // out of the pocket: stuckWatch's last resort (a wing)
			m.x, m.y = w.cellFrom(w.crng)
			m.path = nil
		}
		return
	}
	switch {
	case w.chance(w.chaos.freezeEvery):
		m.frozenTo = w.now.Add(time.Duration(2+w.crng.Intn(19)) * time.Minute)
	case w.chance(w.chaos.pocketEv):
		m.x, m.y, m.path = w.pocket[0], w.pocket[1], nil
		m.pocketTo = w.now.Add(time.Duration(2+w.crng.Intn(9)) * time.Minute)
	case w.chance(w.chaos.deathEvery):
		w.kill(m)
	case w.chance(w.chaos.ambushEvery) && !w.now.Before(m.ambushTo):
		m.ambushTo = w.now.Add(time.Duration(40+w.crng.Intn(80)) * time.Second)
		m.x, m.y = w.cellFrom(w.crng) // pulled away by the fight (a chase, a mob train)
		m.path = nil
	}
}

func (w *simWorld) kill(m *simAgent) {
	w.deaths++
	m.deadTo = w.now.Add(time.Duration(30+w.crng.Intn(90)) * time.Second)
	m.x, m.y = w.cellFrom(w.crng) // the respawn point
	m.path, m.fightTo, m.ambushTo, m.hp = nil, time.Time{}, time.Time{}, 1
}

// health: an ambushed member alone loses its HP in about 40 s; with the party (or the leader) near, the ambush is
// over; out of a fight HP comes back in about 30 s.
func (w *simWorld) health(m *simAgent) {
	if w.now.Before(m.deadTo) {
		return
	}
	if w.now.Before(m.ambushTo) {
		if dist(m.x, m.y, w.lead.x, w.lead.y) <= togetherCells() {
			m.ambushTo = w.now
			return
		}
		if m.hp -= float64(simTick) / float64(40*time.Second); m.hp <= 0 {
			w.kill(m)
		}
		return
	}
	m.hp = min(1, m.hp+float64(simTick)/float64(30*time.Second))
}

func (w *simWorld) step() {
	for _, m := range w.members {
		w.inject(m)
		w.health(m)
	}
	w.leaderThink()
	for _, m := range w.members {
		if !w.now.Before(m.deadTo) {
			w.online++
			far := dist(m.x, m.y, w.lead.x, w.lead.y) > ruleTogetherCells
			if far {
				w.apart++
			}
			if w.can(m) {
				w.mobile++
				if far {
					w.mobileApart++
				}
			}
		}
		if w.can(m) {
			w.memberThink(m)
		}
	}
	w.ticks++
	w.now = w.now.Add(simTick)
}

func runSim(seed int64, policy simPolicy, chaos simChaos, d time.Duration) simResult {
	w := newSimWorld(seed, policy, chaos)
	w.lead.fightMob = -1
	for _, m := range w.members {
		m.fightMob = -1
	}
	for end := w.now.Add(d); w.now.Before(end); {
		w.step()
	}
	if st := w.now.Sub(w.lastKill); st > w.maxStall {
		w.maxStall = st
	}
	return simResult{Kills: w.kills, MaxStall: w.maxStall,
		ApartPct:   100 * float64(w.apart) / float64(max(w.online, 1)),
		MobileApar: 100 * float64(w.mobileApart) / float64(max(w.mobile, 1)),
		CollectPct: 100 * float64(w.collecting) / float64(max(w.ticks, 1)), Deaths: w.deaths}
}

// simBatch: many seeds in parallel; results come back in seed order.
func simBatch(seeds int, policy simPolicy, chaos simChaos, d time.Duration) []simResult {
	out := make([]simResult, seeds)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := 0; i < seeds; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			out[i] = runSim(int64(1000+i), policy, chaos, d)
			<-sem
		}(i)
	}
	wg.Wait()
	return out
}

type simSummary struct {
	KillsPerHour, ApartPct, MobileApart, CollectPct, DeathsPerHour float64
	P95Stall                                                       time.Duration
}

func summarize(rs []simResult, d time.Duration) simSummary {
	var s simSummary
	stalls := make([]time.Duration, len(rs))
	for i, r := range rs {
		s.KillsPerHour += float64(r.Kills) / d.Hours()
		s.ApartPct += r.ApartPct
		s.MobileApart += r.MobileApar
		s.DeathsPerHour += float64(r.Deaths) / d.Hours()
		s.CollectPct += r.CollectPct
		stalls[i] = r.MaxStall
	}
	n := float64(len(rs))
	s.KillsPerHour, s.ApartPct, s.MobileApart, s.CollectPct = s.KillsPerHour/n, s.ApartPct/n, s.MobileApart/n, s.CollectPct/n
	s.DeathsPerHour /= n
	sort.Slice(stalls, func(i, j int) bool { return stalls[i] < stalls[j] })
	s.P95Stall = stalls[len(stalls)*95/100]
	return s
}

func (s simSummary) String() string {
	return fmt.Sprintf("kills/h %4.0f  deaths/h %4.2f  apart %4.1f%% (able to walk %4.1f%%)  collecting %4.1f%%  p95 stall %v",
		s.KillsPerHour, s.DeathsPerHour, s.ApartPct, s.MobileApart, s.CollectPct, s.P95Stall.Round(time.Second))
}

var (
	chaosCalm   = simChaos{name: "calm"}
	chaosFrozen = simChaos{name: "frozen", freezeEvery: 20 * time.Minute}
	chaosAmbush = simChaos{name: "ambush", ambushEvery: 10 * time.Minute}
	chaosRough  = simChaos{name: "rough", freezeEvery: 30 * time.Minute, pocketEv: 30 * time.Minute, deathEvery: 15 * time.Minute, ambushEvery: 15 * time.Minute}
)

// simSize: seeds and simulated time per run. SIM_SEEDS=n overrides the seed count (deeper nightly runs).
func simSize() (int, time.Duration) {
	if n, err := strconv.Atoi(os.Getenv("SIM_SEEDS")); err == nil && n > 0 {
		return n, time.Hour
	}
	if testing.Short() {
		return 16, time.Hour
	}
	return 64, time.Hour
}

// The same seed replays the same hour exactly: a failing seed is a reproducible bug report.
func TestSimPartyDeterministic(t *testing.T) {
	a := runSim(7, policyEngine, chaosRough, 20*time.Minute)
	b := runSim(7, policyEngine, chaosRough, 20*time.Minute)
	if a != b {
		t.Fatalf("seed 7 replayed differently: %+v vs %+v", a, b)
	}
}

// Invariants of today's rule with nothing going wrong: the party stays together and keeps killing.
func TestSimPartyCalmInvariants(t *testing.T) {
	seeds, d := simSize()
	s := summarize(simBatch(seeds, policyEngine, chaosCalm, d), d)
	t.Logf("engine/calm   %v", s)
	if s.ApartPct > 5 {
		t.Errorf("members spend %.1f%% of the time beyond together_cells on a calm map (want <= 5%%)", s.ApartPct)
	}
	if s.P95Stall > 2*time.Minute {
		t.Errorf("p95 longest stretch without a kill is %v on a calm map (want <= 2m)", s.P95Stall)
	}
}

// A/B on the same seeds: the candidate rule must not change calm play (no regression) and must win back the time a
// frozen member costs the party.
func TestSimPartyLadderAB(t *testing.T) {
	seeds, d := simSize()
	for _, c := range []simChaos{chaosCalm, chaosFrozen, chaosAmbush, chaosRough} {
		e := summarize(simBatch(seeds, policyEngine, c, d), d)
		l := summarize(simBatch(seeds, policyLadder, c, d), d)
		t.Logf("%-7s engine %v", c.name, e)
		t.Logf("%-7s ladder %v", c.name, l)
		if c.name == "calm" && e != l {
			t.Errorf("ladder changed calm play: %v vs %v", l, e)
		}
		if l.KillsPerHour < e.KillsPerHour*0.98 {
			t.Errorf("%s: ladder kills/h %.0f below engine %.0f", c.name, l.KillsPerHour, e.KillsPerHour)
		}
		if l.DeathsPerHour > e.DeathsPerHour*1.1+0.05 {
			t.Errorf("%s: ladder deaths/h %.2f above engine %.2f", c.name, l.DeathsPerHour, e.DeathsPerHour)
		}
		if l.MobileApart > e.MobileApart+2 {
			t.Errorf("%s: ladder leaves members who can walk apart %.1f%% of the time (engine %.1f%%)", c.name, l.MobileApart, e.MobileApart)
		}
	}
}
