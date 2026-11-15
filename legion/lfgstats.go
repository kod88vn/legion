package main

// lfgstats.go: the LFG counters (lfgStats in lfg.go) saved to data/lfgstats.json every 5 min and at shutdown, and
// loaded at start, so the counts can be measured across restarts (bug b1794773272q31).
//
// Three kinds of count, all in fixed arrays (no allocation per check):
//   - join checks (lfgMatch): a failed pair of a free bot and an open party (or forming anchor), by check and band.
//     A pair count: one bot counts once for each party it was checked against in the tick.
//   - gate checks (lfgGather): a free bot that did not become a candidate, by the first gate it failed, by band.
//     One count per bot per tick, so these are bot counts.
//   - party checks (lfgGather): an open party set aside, by reason, per tick.
// Plus a gauge of the latest tick (online, in a party, free, candidates, open parties) to read the counts against.
// Only the main instance counts LFG, so only it saves.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const lfgStatsEvery = 5 * time.Minute

// level bands of the counters (index = band)
const nBands = 5

var bandNames = [nBands]string{"lv1-20", "lv21-40", "lv41-60", "lv61-80", "lv81+"}

func lfgBandIdx(lv int) int {
	switch {
	case lv <= 20:
		return 0
	case lv <= 40:
		return 1
	case lv <= 60:
		return 2
	case lv <= 80:
		return 3
	}
	return 4
}

// join checks (pairs)
const (
	jFull = iota
	jSpread
	jExpPoor
	jServes
	jRole
	jBeginner
	jHops
	jMap
	nJoin
)

var joinNames = [nJoin]string{"full", "spread", "exp_poor", "serves", "role", "beginner", "hops", "map"}

// gate checks (bots, in lfgGather order)
const (
	gOffline = iota
	gNoGoal
	gPinned
	gVending
	gTrip
	gDead
	gVendor
	gLowLv
	gGoalMode
	gGoalSet
	gNoMap
	gSocial
	gTownGoal
	nGate
)

var gateNames = [nGate]string{"offline", "no_goal", "pinned", "vending", "trip", "dead", "vendor", "low_lv",
	"goal_mode", "goal_set", "no_map", "social", "town_goal"}

// party checks (open parties set aside, per tick)
const (
	pHuman = iota
	pNoMap
	pTown
	pFull
	pRestoring
	pBoss
	pLeaderOffline
	pNoLevels
	nParty
)

var partyNames = [nParty]string{"human", "no_map", "town", "full", "restoring", "boss", "leader_offline", "no_levels"}

// lfgTally: one tick's checks, on the stack; merged into lfgStats once per tick under one lock.
type lfgTally struct {
	join    [nJoin][nBands]int
	gate    [nGate][nBands]int
	party   [nParty]int
	inParty int // bots already in a party (not per band)
	leftCD  int // bots in the 3 min pause after leaving a party (not per band)
}

func (t *lfgTally) addJoin(r int, lv int) { t.join[r][lfgBandIdx(lv)]++ }

func (t *lfgTally) addGate(r int, lv int) { t.gate[r][lfgBandIdx(lv)]++ }

// flush merges the tick's checks into the cumulative counters.
func (t *lfgTally) flush() {
	lfgStats.Lock()
	defer lfgStats.Unlock()
	for r := range t.join {
		for b := range t.join[r] {
			lfgStats.join[r][b] += t.join[r][b]
		}
	}
	for r := range t.gate {
		for b := range t.gate[r] {
			lfgStats.gate[r][b] += t.gate[r][b]
		}
	}
	for r := range t.party {
		lfgStats.party[r] += t.party[r]
	}
	lfgStats.inParty += t.inParty
	lfgStats.leftCD += t.leftCD
}

// lfgGauge: the latest gather's population (not cumulative, not saved).
type lfgGauge struct {
	At          time.Time
	Online      int
	InParty     int
	Free        int
	Candidates  int
	OpenParties int
}

// lfgSnap: the file layout. counted_since is when the counting first started (kept across restarts).
type lfgSnap struct {
	CountedSince time.Time          `json:"counted_since"`
	SavedAt      time.Time          `json:"saved_at"`
	GoalsSet     map[string]int     `json:"goals_set"`
	GoalsMet     map[string]int     `json:"goals_met"`
	GoalsExpired map[string]int     `json:"goals_expired"`
	GoalsChanged map[string]int     `json:"goals_changed"`
	Posts        map[string]int     `json:"posts"`
	Joins        int                `json:"joins"`
	Formed       int                `json:"parties_formed"`
	FormedLocal  int                `json:"parties_formed_local"`
	Dissolved    map[string]int     `json:"parties_dissolved"`
	Leaves       map[string]int     `json:"members_left"`
	Join         [nJoin][nBands]int `json:"join_checks"`
	Gate         [nGate][nBands]int `json:"gate_checks"`
	Party        [nParty]int        `json:"party_checks"`
	InParty      int                `json:"in_party_skips"`
	LeftCD       int                `json:"left_cooldown_skips"`
}

func lfgStatsFile() string { return filepath.Join(*dataDir, "lfgstats.json") }

func copyCounts(m map[string]int) map[string]int {
	o := make(map[string]int, len(m))
	for k, v := range m {
		o[k] = v
	}
	return o
}

// lfgStatsSave writes the counters to lfgStatsFile (atomic). Errors are logged, never fatal.
func lfgStatsSave(now time.Time) {
	lfgStats.Lock()
	s := lfgSnap{CountedSince: lfgStats.counted, SavedAt: now, GoalsSet: copyCounts(lfgStats.set),
		GoalsMet: copyCounts(lfgStats.met), GoalsExpired: copyCounts(lfgStats.expired), GoalsChanged: copyCounts(lfgStats.changed),
		Posts: copyCounts(lfgStats.posts), Joins: lfgStats.joins, Formed: lfgStats.formed, FormedLocal: lfgStats.local,
		Dissolved: copyCounts(lfgStats.dissolved), Leaves: copyCounts(lfgStats.leaves),
		Join: lfgStats.join, Gate: lfgStats.gate, Party: lfgStats.party, InParty: lfgStats.inParty, LeftCD: lfgStats.leftCD}
	lfgStats.Unlock()
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(lfgStatsFile(), b); err != nil {
		log.Printf("lfgstats: save failed: %v", err)
	}
}

// lfgStatsLoad adds the saved counters to the (zero) in-memory ones at start. Missing or broken file: nothing.
func lfgStatsLoad() {
	b, err := os.ReadFile(lfgStatsFile())
	if err != nil {
		return
	}
	var s lfgSnap
	if err := json.Unmarshal(b, &s); err != nil {
		log.Printf("lfgstats: %s unreadable, starting from zero: %v", lfgStatsFile(), err)
		return
	}
	lfgStats.Lock()
	defer lfgStats.Unlock()
	if !s.CountedSince.IsZero() {
		lfgStats.counted = s.CountedSince
	}
	add := func(dst map[string]int, src map[string]int) {
		for k, v := range src {
			dst[k] += v
		}
	}
	add(lfgStats.set, s.GoalsSet)
	add(lfgStats.met, s.GoalsMet)
	add(lfgStats.expired, s.GoalsExpired)
	add(lfgStats.changed, s.GoalsChanged)
	add(lfgStats.posts, s.Posts)
	add(lfgStats.dissolved, s.Dissolved)
	add(lfgStats.leaves, s.Leaves)
	lfgStats.joins += s.Joins
	lfgStats.formed += s.Formed
	lfgStats.local += s.FormedLocal
	for r := range s.Join {
		for b := range s.Join[r] {
			lfgStats.join[r][b] += s.Join[r][b]
		}
	}
	for r := range s.Gate {
		for b := range s.Gate[r] {
			lfgStats.gate[r][b] += s.Gate[r][b]
		}
	}
	for r := range s.Party {
		lfgStats.party[r] += s.Party[r]
	}
	lfgStats.inParty += s.InParty
	lfgStats.leftCD += s.LeftCD
}

// lfgCountsMap: the nonzero cells of a [reason][band] array as "reason band" keys (for the API).
func lfgCountsMap(names []string, cells [][nBands]int) map[string]int {
	out := map[string]int{}
	for r := range cells {
		for b := range cells[r] {
			if cells[r][b] != 0 {
				out[names[r]+" "+bandNames[b]] = cells[r][b]
			}
		}
	}
	return out
}

// lfgCountsReport: the counters for /api/lfg (call with lfgStats locked).
func lfgCountsReport() map[string]interface{} {
	jc := make([][nBands]int, nJoin)
	copy(jc, lfgStats.join[:])
	gc := make([][nBands]int, nGate)
	copy(gc, lfgStats.gate[:])
	party := map[string]int{}
	for i, n := range partyNames {
		if lfgStats.party[i] != 0 {
			party[n] = lfgStats.party[i]
		}
	}
	g := lfgStats.gauge
	return map[string]interface{}{
		"join_checks":         lfgCountsMap(joinNames[:], jc), // pairs: a bot counts once per party it was checked against
		"gate_checks":         lfgCountsMap(gateNames[:], gc), // bots: the first gate a free bot failed, per tick
		"party_checks":        party,                          // open parties set aside, per tick
		"in_party_skips":      lfgStats.inParty,
		"left_cooldown_skips": lfgStats.leftCD,
		"gauge": map[string]interface{}{"at": g.At, "online": g.Online, "in_party": g.InParty, "free": g.Free,
			"candidates": g.Candidates, "open_parties": g.OpenParties},
		"note": "join_checks count pairs; gate_checks count bots per tick; gauge is the last tick",
	}
}

// avgMs: the mean of total over n, in milliseconds (0 when n is 0).
func avgMs(total time.Duration, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(total.Microseconds()) / 1000 / float64(n)
}

// lfgPerfHistogram: the party performance step (partyPerf, gearscore.go, keyed by the party leader) of every bot party,
// by the party's level band: {band: {step: parties}}. "no_state" = the leader has no step yet. Measurement only: the
// step itself is not changed here (AY07). Takes parties and the members' locks, never with lfgStats held.
func lfgPerfHistogram(now time.Time) map[string]map[string]int {
	type snap struct {
		lead *Bot
		mem  []*Bot
	}
	parties.Lock()
	var list []snap
	for _, p := range parties.byName {
		if p.Human == "" && p.Leader != nil {
			list = append(list, snap{p.Leader, append([]*Bot(nil), p.Members...)})
		}
	}
	parties.Unlock()
	out := map[string]map[string]int{}
	for _, p := range list {
		sum, n := 0, 0
		for _, m := range p.mem {
			m.mu.Lock()
			if m.BaseLv > 0 {
				sum += m.BaseLvOr()
				n++
			}
			m.mu.Unlock()
		}
		if n == 0 {
			continue
		}
		band := bandNames[lfgBandIdx(sum/n)]
		partyPerf.mu.Lock()
		step := "no_state"
		if s := partyPerf.s[p.lead.P.Name]; s != nil {
			step = itoa(s.off)
		}
		partyPerf.mu.Unlock()
		if out[band] == nil {
			out[band] = map[string]int{}
		}
		out[band][step]++
	}
	return out
}

// Cost guard for the full map count (lfg_count_maps). When the average lfgTick over the last lfgCostWindow ticks is
// above lfgCostLimit, every pair goes back to the best-candidate-only count, logged once; it stays off until the
// engine restarts. The tuning flag remains the manual switch.
const (
	lfgCostWindow = 20
	lfgCostLimit  = 2 * time.Second
)

var lfgMapAutoOff atomic.Bool

// lfgCountMaps: count the map check of every fitting pair now (tuning on and the guard has not tripped).
func lfgCountMaps() bool {
	return tune().LfgCountMaps && !lfgMapAutoOff.Load()
}

// lfgNoteTick records one lfgTick duration and trips the guard when the window's average is over the limit.
func lfgNoteTick(d time.Duration) {
	lfgStats.Lock()
	defer lfgStats.Unlock()
	lfgStats.tickLast = d
	lfgStats.tickSum += d
	lfgStats.tickRing[lfgStats.ticks%lfgCostWindow] = d
	lfgStats.ticks++
	if lfgStats.ticks < lfgCostWindow || lfgMapAutoOff.Load() {
		return
	}
	var sum time.Duration
	for _, t := range lfgStats.tickRing {
		sum += t
	}
	if avg := sum / lfgCostWindow; avg > lfgCostLimit {
		lfgMapAutoOff.Store(true)
		log.Printf("lfgstats: full map count switched off: lfgTick averages %v over the last %d ticks (limit %v)", avg, lfgCostWindow, lfgCostLimit)
	}
}
