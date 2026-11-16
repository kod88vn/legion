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
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
	b.buffAt["kite:seen"] = now
	noteKiteStep(b.charName())
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

// Kite counters (market engine WP1, design section 3.5): kite_steps and kite_deaths per bot, cumulative across deploys
// like econ_counters.json (kite_counters[_core].json). A death counts as a kite death when the bot stepped back within
// kiteDeathWindow before it died (bot.go vanish handler). Read by /api/economy as "kite".

const kiteDeathWindow = 60 * time.Second

type kiteSaved struct {
	Since  time.Time        `json:"since"`
	Saved  time.Time        `json:"saved"`
	Steps  map[string]int64 `json:"steps"`
	Deaths map[string]int64 `json:"deaths"`
}

var (
	kiteMu     sync.Mutex
	kiteSteps  = map[string]int64{}
	kiteDeaths = map[string]int64{}
	kiteSince  time.Time
)

func kiteCountersFile() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "kite_counters_core.json")
	}
	return filepath.Join(*dataDir, "kite_counters.json")
}

// noteKiteStep counts one kiting step by this bot.
func noteKiteStep(name string) {
	kiteMu.Lock()
	kiteSteps[name]++
	kiteMu.Unlock()
}

// kiteDeathCounts: a death at now counts as a kite death when the last kite step was seen less than
// kiteDeathWindow earlier (a zero seen time never counts).
func kiteDeathCounts(seen, now time.Time) bool {
	return !seen.IsZero() && now.Sub(seen) < kiteDeathWindow
}

// noteKiteDeath counts a kite death by this bot when kiteDeathCounts says so.
func (b *Bot) noteKiteDeath(now time.Time) {
	if !kiteDeathCounts(b.buffAt["kite:seen"], now) {
		return
	}
	kiteMu.Lock()
	kiteDeaths[b.charName()]++
	kiteMu.Unlock()
}

// kiteCounters: totals and the per-bot counts for /api/economy.
func kiteCounters() map[string]interface{} {
	kiteMu.Lock()
	defer kiteMu.Unlock()
	var steps, deaths int64
	by := map[string][2]int64{}
	for k, v := range kiteSteps {
		steps += v
		e := by[k]
		e[0] = v
		by[k] = e
	}
	for k, v := range kiteDeaths {
		deaths += v
		e := by[k]
		e[1] = v
		by[k] = e
	}
	return map[string]interface{}{
		"kite_steps":     steps,
		"kite_deaths":    deaths,
		"by_bot":         by, // name: [kite_steps, kite_deaths]
		"counters_since": kiteSince,
	}
}

// loadKiteCounters (start-up): add the last run's kite counters to this run's.
func loadKiteCounters() {
	kiteMu.Lock()
	kiteSince = time.Now()
	kiteMu.Unlock()
	raw, err := os.ReadFile(kiteCountersFile())
	if err != nil {
		return
	}
	var s kiteSaved
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	kiteMu.Lock()
	defer kiteMu.Unlock()
	if !s.Since.IsZero() {
		kiteSince = s.Since
	}
	for k, v := range s.Steps {
		kiteSteps[k] += v
	}
	for k, v := range s.Deaths {
		kiteDeaths[k] += v
	}
}

// saveKiteCounters writes the cumulative kite counters (atomic write, like saveEconCounters).
func saveKiteCounters(now time.Time) {
	kiteMu.Lock()
	s := kiteSaved{Saved: now, Since: kiteSince, Steps: map[string]int64{}, Deaths: map[string]int64{}}
	for k, v := range kiteSteps {
		s.Steps[k] = v
	}
	for k, v := range kiteDeaths {
		s.Deaths[k] = v
	}
	kiteMu.Unlock()
	raw, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := kiteCountersFile() + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		os.Rename(tmp, kiteCountersFile())
	}
}
