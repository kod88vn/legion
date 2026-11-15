package main

// CL16 (owner 2026-11-14): a caster (Wizard, Mage, Sage) whose SP sits full most of the time is a bug - it spends SP
// on spells in every fight and rests only to refill.
//
// Measured before (Zen.fox, Wizard lv77, SP 1399, Ayla's party in pay_dun01, legion-core TRACE 02:15-02:23 UTC):
// 45 casts in 8.5 min of fighting (5.3/min: 21 Storm Gust, 23 Jupitel Thunder, 1 Fire Wall), every SP reading
// 1303-1399 (93-100%), nine "AOE none: 3 mobs near (0 foreign)" at 1303-1399 SP. Ayla's log showed the same: 1-7
// casts a minute, none for whole minutes while the party fought. Causes:
//   1. fight() bolts only the one monster it was handed (the tank's / leader's / attacker's target). That one is
//      often out of range 9 or dies to the melee first; a Mage out of range while walking fell through to closeIn
//      and chased the monster for a melee swing with its Wand instead of casting at the monster next to it.
//   2. With no group (3 in a 4-cell area), nothing else was cast: monsters 6-10 cells apart around the party got
//      nothing while SP sat full (no "help kill").
//   3. Between the party's fights (nobody targeting anything) the member loop went looting / "with Ayla" even with
//      monsters in casting range around the group.
//   4. Fire Wall only for the caster itself: a monster walking in on the priest got nothing.
//
// casterActive (from fight, after aoeCast - groups still get the area spell first, CL07): a Mage-line SP caster with
// SP above caster_bolt_floor_pct casts a single-target spell at a party-relevant monster in range (the handed one if
// it can, else one on a member, else one a member fights), picks Jupitel Thunder unless the monster resists Wind (then
// the counter-element bolt), never chases into melee while it can cast, and walls a monster off the priest.
// casterHelp (party member loop, before looting): with SP above caster_help_sp_pct and nothing to assist, open on a
// monster in casting range near the group (no idling at full SP). Both off with tuning.json caster_active_off.
// casterMeter: share of fight time at >=95% SP and casts per minute, logged every 5 min ("CASTERS ...").

import (
	"log"
	"sort"
	"sync"
	"time"
)

const (
	wzJupitel       = 84
	casterFullPct   = 95 // CL16 metric: SP at or above this share of max counts as "full"
	casterMeterSpan = 5 * time.Minute
)

// casterSpell: the single-target spell for a monster of element mobElem. Jupitel Thunder (Wind, 12 hits at lv10, the
// shortest cast) unless the monster resists Wind; else the counter-element bolt (boltFor). learned = skill levels.
func casterSpell(mob *Mob, learned func(id int) int) (int, int) {
	elem := ""
	if mob != nil {
		elem = mob.Element
	}
	if l := learned(wzJupitel); l > 0 && elemMul("Wind", elem) >= 1 {
		return wzJupitel, l
	}
	want := boltFor(mob)
	if mob != nil && mob.Element == "Wind" && learned(wzEarthSpike) > 0 {
		want = wzEarthSpike
	}
	for _, id := range []int{want, mgFire, mgCold, mgLight} {
		if l := learned(id); l > 0 {
			return id, l
		}
	}
	if l := learned(wzJupitel); l > 0 { // only Jupitel known: better a resisted spell than none
		return wzJupitel, l
	}
	return 0, 0
}

// casterCastMs: the engine's busy time for a single-target spell (cast + after-cast delay, DEX ~60)
func casterCastMs(id, lv int) int {
	switch id {
	case wzJupitel:
		return 2500 + 100*lv/2
	case wzEarthSpike:
		return 1200 + 300*lv
	case mgFire, mgCold, mgLight:
		return 700 + 400*lv
	}
	return 3000
}

func casterSpellName(id int) string {
	switch id {
	case wzJupitel:
		return "Jupitel Thunder"
	case mgFire:
		return "Fire Bolt"
	case mgCold:
		return "Cold Bolt"
	case mgLight:
		return "Lightning Bolt"
	case wzEarthSpike:
		return "Earth Spike"
	}
	return skillName(id)
}

// boltCand: a monster the caster could hit with a single-target spell.
type boltCand struct {
	e        *Entity
	x, y     int
	onSupp   bool // it is attacking a support (priest) of ours
	onMember bool // it is attacking one of us
	fought   bool // a party member is fighting it (its target / the handed target)
	los      bool // a clear line from where we stand
}

// pickBoltTarget: index of the monster to cast at from (mx,my) with range rng; prefer = the monster fight() was
// handed. In range and in line only, and only monsters already in the fight. Order: the handed one; then on a
// support; on a member; one the party fights; nearest first. -1 = none in range.
func pickBoltTarget(c []boltCand, mx, my, rng int, prefer uint32) int {
	best, bestKey := -1, [3]int{}
	for i, m := range c {
		d := dist(mx, my, m.x, m.y)
		if d > rng || !m.los {
			continue
		}
		pri := 0
		switch {
		case m.e != nil && m.e.ID == prefer:
			pri = 4
		case m.onSupp:
			pri = 3
		case m.onMember:
			pri = 2
		case m.fought:
			pri = 1
		}
		if pri == 0 {
			continue // a monster nobody fights: casting at it mid-fight pulls more (casterHelp opens fights when idle)
		}
		key := [3]int{pri, -d, -i}
		if best < 0 || key[0] > bestKey[0] || key[0] == bestKey[0] && key[1] > bestKey[1] {
			best, bestKey = i, key
		}
	}
	return best
}

// casterSpendOK: SP above the floor (in %) and the cast leaves the area-spell reserve (aoe.go aoeReserve).
func casterSpendOK(sp, maxSP, cost, floorPct int) bool {
	return maxSP > 0 && sp*100 >= floorPct*maxSP && sp-cost >= aoeReserve(maxSP)
}

// wallCell: where a Fire Wall stops a monster at (ex,ey) walking to (sx,sy): one cell in front of the monster toward
// its prey (ok=false when they are too close or too far apart for a wall to help).
func wallCell(ex, ey, sx, sy int) (int, int, bool) {
	d := dist(ex, ey, sx, sy)
	if d < 2 || d > 6 {
		return 0, 0, false
	}
	return ex + sign(sx-ex), ey + sign(sy-ey), true
}

// casterOn (b.mu held): CL16 applies to this bot (Mage-line SP caster, flag on).
func (b *Bot) casterOn() bool {
	return !tune().CasterActiveOff && family(b.Class) == famMage && b.MaxSP > 0 && b.spCaster() && !b.isHealer()
}

// casterCands (b.mu held): monsters within 14 cells that are ours to hit - no monster someone outside the party
// fights (no kill stealing), no plants. members: the party's published states (nil solo).
func (b *Bot) casterCands(members []pubState, handed *Entity, mx, my int, now time.Time) []boltCand {
	friend := b.partyFriend()
	supp, fought := map[uint32]bool{}, map[uint32]bool{}
	for _, m := range members {
		if m.Support && m.AID != 0 {
			supp[m.AID] = true
		}
		if m.Target != 0 && m.Map == b.MapName {
			fought[m.Target] = true
		}
	}
	if handed != nil {
		fought[handed.ID] = true
	}
	gm := maps[b.MapName]
	var out []boltCand
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		ox, oy := o.pos(now)
		if dist(mx, my, ox, oy) > 14 {
			continue
		}
		if mb := mobs[o.Class]; mb != nil && mb.worthless() {
			continue
		}
		if h := claims.holder(b.MapName, o.ID); h != 0 && !friend(h) {
			continue
		}
		recent := now.Sub(o.TargetAt) < 6*time.Second
		if t := b.ents[o.Target]; t != nil && t.Type == objPC && recent && !friend(o.Target) {
			continue
		}
		c := boltCand{e: o, x: ox, y: oy, fought: fought[o.ID]}
		c.onMember = recent && friend(o.Target) || now.Sub(o.LastHitMe) < 4*time.Second
		c.onSupp = recent && supp[o.Target]
		c.los = gm == nil || gm.LineWalkable(mx, my, ox, oy)
		out = append(out, c)
	}
	return out
}

// casterActive (b.mu held, from fight after aoeCast): true = a spell was sent, or the caster holds (walks to casting
// range) instead of chasing e into melee.
func (b *Bot) casterActive(e *Entity, moving bool, now time.Time) bool {
	if !b.casterOn() || b.Sitting {
		return false
	}
	casterMeterNote(b.P.Name, b.SP, b.MaxSP, b.skillReady, now)
	if now.Before(b.skillReady) {
		return false
	}
	var members []pubState
	if p := partyOf(b); p != nil {
		_, _, members = b.partyView(p, now)
	}
	mx, my := b.myPos(now)
	cands := b.casterCands(members, e, mx, my, now)
	if b.casterWall(cands, members, mx, my, now) {
		return true
	}
	learned := func(id int) int {
		if si := b.skills[id]; si != nil {
			return si.Lv
		}
		return 0
	}
	floor := tune().CasterBoltFloorPct
	i := pickBoltTarget(cands, mx, my, 9, e.ID)
	if i >= 0 {
		t := cands[i].e
		id, lv := casterSpell(mobs[t.Class], learned)
		if id == 0 || now.Before(b.skillFailUntil[id]) {
			return false
		}
		cost := 0
		if si := b.skills[id]; si != nil {
			cost = si.SP
		}
		if !casterSpendOK(b.SP, b.MaxSP, max(cost, 1), floor) {
			return false // low: the old path (bolts down to fight's reserve, then rest)
		}
		b.skillReady = now.Add(time.Duration(casterCastMs(id, lv)+b.P.ReactionMs/2) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		b.setState("casting " + casterSpellName(id) + " on " + mobName(t.Class))
		go b.useSkill(id, lv, t.ID)
		return true
	}
	// nothing in range: walk into casting range of the handed monster - never chase it for a melee swing while there
	// is SP to cast (before: moving + out of range fell through to closeIn)
	if b.SP*100 < floor*b.MaxSP {
		return false
	}
	ex, ey := e.pos(now)
	if dist(mx, my, ex, ey) <= 9 {
		return false // in range but no line: fight's own path steps around
	}
	if !moving {
		go b.walkTo(ex+sign(mx-ex)*min(7, abs(mx-ex)), ey+sign(my-ey)*min(7, abs(my-ey)))
	}
	b.setState("moving into casting range of " + mobName(e.Class))
	return true
}

// casterWall (b.mu held): Fire Wall in front of a monster walking in on our support (or on us when no tank holds it;
// aoe.go already walls for the caster itself when solo/hit).
func (b *Bot) casterWall(cands []boltCand, members []pubState, mx, my int, now time.Time) bool {
	si := b.skills[mgFireWall]
	if si == nil || si.Lv <= 0 || b.SP-max(si.SP, 1) < aoeReserve(b.MaxSP) {
		return false
	}
	if b.buffAt == nil {
		b.buffAt = map[string]time.Time{}
	}
	key := "aoe:" + itoa(mgFireWall) // shared with aoe.go's own wall
	if now.Before(b.buffAt[key]) || now.Before(b.skillFailUntil[mgFireWall]) {
		return false
	}
	gm := maps[b.MapName]
	for _, c := range cands {
		if !c.onSupp {
			continue
		}
		var sx, sy int
		found := false
		for _, m := range members {
			if m.AID == c.e.Target {
				sx, sy, found = m.LX, m.LY, true
				if sx == 0 && sy == 0 {
					sx, sy = m.X, m.Y
				}
				break
			}
		}
		if !found {
			continue
		}
		wx, wy, ok := wallCell(c.x, c.y, sx, sy)
		if !ok || dist(mx, my, wx, wy) > 9 || gm != nil && (!gm.Walkable(wx, wy) || !gm.LineWalkable(mx, my, wx, wy)) {
			continue
		}
		ms := (1760-160*si.Lv)*7/10 + 400 + 300
		b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
		b.buffAt[key] = now.Add(5 * time.Second)
		b.targetAt, b.attackSentFor = now, 0
		b.setState("Fire Wall in front of " + mobName(c.e.Class))
		go b.send(newPkt(0x0366).u16(uint16(si.Lv)).u16(mgFireWall).u16(uint16(wx)).u16(uint16(wy)).b)
		return true
	}
	return false
}

// casterHelp (b.mu held, party member loop before looting): nothing to assist and SP to spare - open on a monster in
// casting range near the group instead of looting / standing by with full SP. lead: the party leader's state.
func (b *Bot) casterHelp(lead pubState, members []pubState, leaderLeaving, moving bool, now time.Time) bool {
	if !b.casterOn() || leaderLeaving || b.wantSit || b.MapName != lead.Map || isTown(b.MapName) ||
		b.SP*100 < tune().CasterHelpSPPct*b.MaxSP || now.Before(b.skillReady) {
		return false
	}
	mx, my := b.myPos(now)
	if dist(mx, my, lead.X, lead.Y) > partyLeash()+4 {
		return false
	}
	maxLv := b.BaseLvOr() + 5
	var pick *Entity
	pd := 99
	for _, c := range b.casterCands(members, nil, mx, my, now) {
		mb := mobs[c.e.Class]
		if mb == nil || mb.Boss || mb.Level > maxLv || !c.los {
			continue
		}
		d := dist(mx, my, c.x, c.y)
		if d > 9 || dist(lead.X, lead.Y, c.x, c.y) > partyLeash()+4 {
			continue
		}
		if d < pd {
			pick, pd = c.e, d
		}
	}
	if pick == nil || !claims.try(b.MapName, pick.ID, b.aid, false) && !b.partyClaim(pick.ID) {
		return false
	}
	b.target, b.targetAt = pick.ID, time.Time{}
	b.setState("fighting " + mobName(pick.Class) + " near " + lead.Name)
	b.fight(pick, moving, now)
	return true
}

// ---------------------------------------------------------------- CL16 metric

type casterSample struct {
	last     time.Time
	fight    time.Duration // time in fight() (sampled each tick, gaps capped)
	full     time.Duration // of that, at >= casterFullPct SP
	casts    int
	since    time.Time
	lastFull bool
	ready    time.Time // the last skillReady seen
}

var casterMeter = struct {
	sync.Mutex
	m     map[string]*casterSample
	logAt time.Time
}{m: map[string]*casterSample{}}

// casterMeterNote: a fight tick of a caster. A cast (area spell, bolt, wall - by any path) shows as a new skillReady.
func casterMeterNote(name string, sp, maxSP int, skillReady time.Time, now time.Time) {
	casterMeter.Lock()
	defer casterMeter.Unlock()
	s := casterMeter.m[name]
	if s == nil {
		s = &casterSample{since: now}
		casterMeter.m[name] = s
	}
	if skillReady.After(s.ready) {
		if !s.ready.IsZero() || skillReady.After(now) {
			s.casts++
		}
		s.ready = skillReady
	}
	if !s.last.IsZero() {
		dt := now.Sub(s.last)
		if dt > 0 && dt <= 3*time.Second { // a gap = we left the fight; don't count it
			s.fight += dt
			if s.lastFull {
				s.full += dt
			}
		}
	}
	s.last, s.lastFull = now, maxSP > 0 && sp*100 >= casterFullPct*maxSP
	if casterMeter.logAt.IsZero() {
		casterMeter.logAt = now
	}
	if now.Sub(casterMeter.logAt) >= casterMeterSpan {
		casterMeter.logAt = now
		if line := casterMeterLineLocked(now); line != "" {
			go log.Print(line)
		}
		casterMeter.m = map[string]*casterSample{}
	}
}

// casterFullShare: share of fight time at full SP (0..1) and casts per fight minute.
func (s *casterSample) casterFullShare() (float64, float64) {
	if s.fight <= 0 {
		return 0, 0
	}
	return float64(s.full) / float64(s.fight), float64(s.casts) / s.fight.Minutes()
}

// casterMeterLineLocked: "CASTERS 5m: n=12 fight 34m, SP>=95% 18% of fight time, 9.1 casts/fight-min; worst: a 61%, b 40%"
func casterMeterLineLocked(now time.Time) string {
	var fight, full time.Duration
	casts, n := 0, 0
	type w struct {
		name  string
		share float64
	}
	var worst []w
	for name, s := range casterMeter.m {
		if s.fight < 20*time.Second {
			continue
		}
		n++
		fight += s.fight
		full += s.full
		casts += s.casts
		sh, _ := s.casterFullShare()
		worst = append(worst, w{name, sh})
	}
	if n == 0 {
		return ""
	}
	sort.Slice(worst, func(i, j int) bool { return worst[i].share > worst[j].share })
	line := "CASTERS 5m: n=" + itoa(n) + " fight " + itoa(int(fight.Minutes())) + "m, SP>=95% " +
		itoa(int(100*float64(full)/float64(fight))) + "% of fight time, " +
		itoa(int(10*float64(casts)/fight.Minutes())/10) + " casts/fight-min; worst:"
	for i, x := range worst {
		if i == 3 {
			break
		}
		line += " " + x.name + " " + itoa(int(100*x.share)) + "%"
	}
	return line
}
