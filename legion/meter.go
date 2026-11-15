package main

// meter.go (redesign package M, 2026-11-14): the always-on meter of the two key metrics per bot - kills per minute
// and zeny per hour - plus deaths and a time budget. Read-only for behaviour: nothing here changes what a bot does.
// Definitions: docs/design/meter.md.
//
// Lock discipline (lessons.md, the 2026-11-14 API wedge):
//   - every per-bot counter lives in the Bot (b.meter) and is written only by the hooks below, which run inside
//     handle() / think() / noteKafraHop() with b.mu ALREADY held - no new lock, no global lock, no file I/O;
//   - the hooks read only lock-free shared state: tune()-style atomic loads (meterTune, meterPriceTab), immutable
//     game data (mobs, items) and an optional sync.Map registration of a newly seen item id (meterNoteItem);
//   - the aggregator (meterapi.go) locks one bot at a time just long enough to sum its ring into three windows, then
//     computes everything (parties, cohorts, percentiles) with no lock held; files are written by that one goroutine.

import (
	"sync"
	"sync/atomic"
	"time"
)

// time buckets
const (
	mbFight = iota
	mbSearch
	mbTravel
	mbRest
	mbTown
	mbDead
	mbWait
	mbStuck
	mbIdle
	mbN
)

var meterBucketNames = [mbN]string{"fighting", "searching", "travelling", "resting", "town", "dead", "waiting", "stuck", "idle"}

const (
	meterRingMin = 60              // minutes kept per bot (the 1-hour window)
	meterMaxDt   = 5 * time.Second // a think gap longer than this (offline, stall) is not counted as time
)

// meterSlot: one minute (or the session total) of one bot's counters.
type meterSlot struct {
	Min    int64 // unix minute (ring slots); 0 = empty
	Kills  int32 // killing blows on its own target (any level)
	Fair   int32 // ... of which on a monster at least the fair level (meterFairLv)
	Deaths int32
	Loot   int64 // market value of items picked up
	Cons   int64 // market value of consumables used (potions, ammo, gemstones...)
	Travel int64 // Kafra fares + Fly / Butterfly Wings used
	ZIn    int64 // purse increases (information only, see meter.md)
	ZOut   int64 // purse decreases
	Sec    [mbN]float32
	Duty   float32 // seconds flagged "on guild duty" (meterDutyHook)
}

func (s *meterSlot) add(o *meterSlot) {
	s.Kills += o.Kills
	s.Fair += o.Fair
	s.Deaths += o.Deaths
	s.Loot += o.Loot
	s.Cons += o.Cons
	s.Travel += o.Travel
	s.ZIn += o.ZIn
	s.ZOut += o.ZOut
	for i := range s.Sec {
		s.Sec[i] += o.Sec[i]
	}
	s.Duty += o.Duty
}

// meterCounters: embedded in Bot (field b.meter); guarded by b.mu.
type meterCounters struct {
	ring     [meterRingMin]meterSlot
	sess     meterSlot // since this engine started (or since the account switched character)
	sessFrom time.Time
	char     string // the character these counters belong to
	lastTick time.Time
	lastZeny int
	zenyOK   bool
	bucket   int // bucket of the last tick (shown in the API)
}

// slot returns the ring slot for minute m, clearing it when it held an older minute (window roll-over).
func (c *meterCounters) slot(m int64) *meterSlot {
	s := &c.ring[int(m%meterRingMin)]
	if s.Min != m {
		*s = meterSlot{Min: m}
	}
	return s
}

// sumWin: the last n minutes up to nowMin (the current partial minute included).
func (c *meterCounters) sumWin(nowMin int64, n int) meterSlot {
	var out meterSlot
	for i := range c.ring {
		s := &c.ring[i]
		if s.Min > nowMin-int64(n) && s.Min <= nowMin {
			out.add(s)
		}
	}
	return out
}

// record applies f to the current minute's slot and to the session total.
func (c *meterCounters) record(now time.Time, f func(*meterSlot)) {
	if c.sessFrom.IsZero() {
		c.sessFrom = now
	}
	f(c.slot(now.Unix() / 60))
	f(&c.sess)
}

// reset forgets everything (the account logged in with another character).
func (c *meterCounters) reset(char string, now time.Time) {
	*c = meterCounters{char: char, sessFrom: now}
}

// ---------------------------------------------------------------- hooks (all with b.mu held)

// meterLogin: a session starts (bot.go run). Another character than the metered one starts fresh counters; the purse
// baseline is the character list's zeny, so a change made while offline is not counted.
func (b *Bot) meterLogin(now time.Time) {
	c := &b.meter
	if n := b.charName(); n != c.char {
		c.reset(n, now)
	}
	c.lastTick = time.Time{}
	c.lastZeny, c.zenyOK = b.Zeny, true
}

// meterTick: think() start. The time since the previous tick goes to the bucket of b.State, which the previous
// think() set: it is what the bot was doing during that interval (the context - map, target - is read now).
func (b *Bot) meterTick(now time.Time) {
	c := &b.meter
	dt := now.Sub(c.lastTick)
	last := c.lastTick
	c.lastTick = now
	bk := meterBucketOf(b.State, b.meterCtx(now))
	c.bucket = bk
	if last.IsZero() || dt <= 0 || dt > meterMaxDt {
		return
	}
	sec := float32(dt.Seconds())
	duty := false
	if h := meterDutyHook; h != nil {
		duty = h(b)
	}
	c.record(now, func(s *meterSlot) {
		s.Sec[bk] += sec
		if duty {
			s.Duty += sec
		}
	})
}

func (b *Bot) meterCtx(now time.Time) meterCtx {
	town := isTown(b.MapName)
	return meterCtx{
		dead:      b.Dead,
		town:      town,
		onHunt:    !town && b.MapName != "" && (b.Goal.Map == "" || b.Goal.Map == b.MapName),
		hasTarget: b.target != 0,
		stuck:     !b.stuckSince.IsZero() && now.Sub(b.stuckSince) >= 30*time.Second,
	}
}

// meterKill: our target died (bot.go 0x0080 type 1, id == b.target). e may be nil (never seen: level unknown).
func (b *Bot) meterKill(e *Entity, now time.Time) {
	fair := false
	if e != nil {
		if m := mobs[e.Class]; m != nil {
			fair = meterIsFair(m.Level, b.BaseLvOr(), meterTune().FairLvGap)
		}
	}
	b.meter.record(now, func(s *meterSlot) {
		s.Kills++
		if fair {
			s.Fair++
		}
	})
}

// meterIsFair: a kill counts as fair when the monster's level is at least the bot's base level minus the gap.
func meterIsFair(mobLv, botLv, gap int) bool {
	return mobLv >= botLv-gap
}

func (b *Bot) meterDeath(now time.Time) {
	b.meter.record(now, func(s *meterSlot) { s.Deaths++ })
}

// meterLoot: n of item id picked up (0x0b41 ok).
func (b *Bot) meterLoot(id, n int) {
	if n <= 0 {
		return
	}
	v := int64(meterValue(id)) * int64(n)
	b.meter.record(time.Now(), func(s *meterSlot) { s.Loot += v })
}

// meterUsed: n of item id used up (0x01c8 ok: the count dropped by n). Wings are travel, the rest consumables.
func (b *Bot) meterUsed(id, n int) {
	if n <= 0 || n > 1000 {
		return
	}
	v := int64(meterValue(id)) * int64(n)
	wing := id == itemFlyWing || id == itemButterflyWing
	b.meter.record(time.Now(), func(s *meterSlot) {
		if wing {
			s.Travel += v
		} else {
			s.Cons += v
		}
	})
}

// meterRemoved: 0x07fa ZC_DELETE_ITEM_FROM_BODY (reason, index, amount), before the inventory is updated. Reason 1
// (used by a skill: gemstones, bottles) is a consumable; reason 0 counts only for ammo (arrows burned per shot).
// Sales, storage, cart moves and trades have their own reasons and are not costs.
func (b *Bot) meterRemoved(op uint16, pk []byte) {
	if op != 0x07fa || len(pk) < 8 {
		return
	}
	reason, idx, amt := int(le.Uint16(pk[2:])), int(le.Uint16(pk[4:])), int(le.Uint16(pk[6:]))
	it := b.inv[idx]
	if it == nil || amt <= 0 {
		return
	}
	x := items[it.ItemID]
	if reason == 1 || reason == 0 && x != nil && x.Type == "Ammo" {
		b.meterUsed(it.ItemID, min(amt, it.Count))
	}
}

// meterTravel: a Kafra teleport was taken (kafra.go noteKafraHop).
func (b *Bot) meterTravel(fare int) {
	if fare <= 0 {
		return
	}
	b.meter.record(time.Now(), func(s *meterSlot) { s.Travel += int64(fare) })
}

// meterZeny: a purse update from the server (0x00b1 type 20), before b.Zeny takes it. Information only.
func (b *Bot) meterZeny(z int) {
	c := &b.meter
	if !c.zenyOK {
		c.lastZeny, c.zenyOK = z, true
		return
	}
	d := int64(z - c.lastZeny)
	c.lastZeny = z
	if d == 0 {
		return
	}
	c.record(time.Now(), func(s *meterSlot) {
		if d > 0 {
			s.ZIn += d
		} else {
			s.ZOut -= d
		}
	})
}

// meterDutyHook: set by the guild-direction package (feat/guild) to flag time spent on a guild block. Called with
// b.mu held: it must not lock b.mu or any lock that is ever held while taking a bot lock.
var meterDutyHook func(b *Bot) bool

// ---------------------------------------------------------------- prices (lock-free for the hooks)

// meterPriceTab: item id -> market value (priceOf), rebuilt by the aggregator goroutine; read with one atomic load.
var meterPriceTab atomic.Pointer[map[int]int]

// meterSeen: item ids met by the hooks that the table doesn't price yet (the aggregator prices them next round).
var meterSeen sync.Map

// meterValue: the market value of one item. A miss falls back to the NPC sell price (pure data, no lock) and asks the
// aggregator to price the item.
func meterValue(id int) int {
	if t := meterPriceTab.Load(); t != nil {
		if v, ok := (*t)[id]; ok {
			return v
		}
	}
	meterSeen.LoadOrStore(id, struct{}{})
	return sellPrice(id)
}
