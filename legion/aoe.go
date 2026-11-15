package main

// AoE casters (owner rule 2026-11-14: "Mages and Wizards are AoE builds ... cast them whenever 3 or more monsters are
// grouped together"). QA before this file: in 10 minutes of traces no Mage/Wizard ever sent Fire Ball, Thunder Storm,
// Storm Gust, Lord of Vermilion or Meteor Storm - Wizards with Storm Gust 5-8 cast only Jupitel Thunder, a Mage cast
// 81 Fire Bolts with 34 monsters around. fight() only knew bestAttack (single target).
//
// aoeCast (from fight, b.mu held): when 3+ monsters stand inside an area spell's radius around one cell in casting
// range, cast the best learned and affordable area spell there (ground spells by UseSkillToPos 0x0366, Fire Ball on
// the monster at the centre), keeping an SP reserve for escapes. Skipped when the cluster holds a monster someone
// outside our party is fighting (no kill stealing), or - solo - a boss, a monster far above our power or an
// aggressive swarm bigger than we can finish (danger.go handles running from those). While monsters hit us and no
// party tank holds them, only quick spells (a long cast is interrupted by every hit).
// Fire Wall: a solo caster drops it between itself and a monster walking in on it (kiting/blocking).

import "time"

type aoeSpell struct {
	id     int
	ground bool // UseSkillToPos; false = cast on the monster at the centre (Fire Ball's splash)
	radius int  // cells around the centre that are hit (Chebyshev)
	castMs func(lv int) int
	cdMs   int    // own cooldown (skill_db Cooldown / Duration), so the server never refuses it (bot.go 0x0110 -> 60 s off)
	elem   string // the spell's element (a crowd that resists it gets the next spell)
}

const (
	mgFireBall   = 17
	mgFireWall   = 18
	mgThunder    = 21
	wzMeteor     = 83
	wzVermilion  = 85
	wzStormGust  = 89
	wzHeavenDrv  = 91
	aoeMinMobs   = 3    // owner: "3 or more monsters grouped together"
	aoeQuickMs   = 3200 // a spell this fast may be cast while being hit
	aoeSoloSwarm = 6    // solo: more aggressive monsters than this in the area is a swarm we can't finish
)

// rAthena re/skill_db.yml: variable cast (scaled ~0.7 for a caster's DEX) + fixed cast + after-cast delay
var aoeSpells = []aoeSpell{ // best first
	{id: wzStormGust, ground: true, radius: 4, castMs: func(lv int) int { return (4300+200*lv)*7/10 + 1500 + 1000 }, cdMs: 6500, elem: "Water"},
	{id: wzVermilion, ground: true, radius: 5, castMs: func(lv int) int { return (6500-200*lv)*7/10 + 1500 + 1000 }, cdMs: 5500, elem: "Wind"},
	{id: wzMeteor, ground: true, radius: 3, castMs: func(lv int) int { return 6300*7/10 + 1500 + 1000 }, cdMs: 2000, elem: "Fire"},
	{id: mgThunder, ground: true, radius: 2, castMs: func(lv int) int { return (2500+200*lv)*7/10 + 1500 + 2000 }, elem: "Wind"},
	{id: wzHeavenDrv, ground: true, radius: 2, castMs: func(lv int) int { return (900+200*lv)*7/10 + 800 + 500 }, cdMs: 1000, elem: "Earth"},
	{id: mgFireBall, radius: 2, castMs: func(lv int) int { return 800*7/10 + 200 + 700 }, elem: "Fire"},
}

// elemMul: rough Renewal element table (level-1 monsters) for the area spells' elements. Same element is resisted,
// the counter element hurts more; Fire burns Undead.
func elemMul(spell, mob string) float64 {
	if spell == "" || mob == "" {
		return 1
	}
	if spell == mob {
		return 0.25
	}
	switch spell + ">" + mob {
	case "Water>Fire", "Wind>Water", "Earth>Wind", "Fire>Earth":
		return 1.5
	case "Fire>Undead":
		return 1.25
	case "Fire>Water", "Water>Wind", "Wind>Earth", "Earth>Fire":
		return 0.5
	}
	return 1
}

// aoeReserve: SP kept back from area spells (escape skills, a bolt to finish one off).
func aoeReserve(maxSP int) int { return max(maxSP*15/100, 30) }

type aoeMob struct {
	e       *Entity
	x, y    int
	foreign bool // someone outside our party is fighting it (claimed by another bot, or hitting/hit by another player)
	risky   bool // boss/champion or far above our power
	aggro   bool
	strong  bool   // boss/champion, at or above our level, or a big HP pool: 2 of them already make a group
	elem    string // the monster's element (mob_db)
}

const aoeCastRange = 9

// pickAoE: which spell and where. mobs = monsters in view; quickOnly: being hit without a tank in front;
// solo: no party. Returns the spell index (-1 none), the centre and the monster at (nearest) the centre.
func pickAoE(mobsIn []aoeMob, bx, by int, lv func(id int) (lv, sp int), sp, maxSP int, quickOnly, solo bool, ready func(id int) bool) (int, int, int, *Entity) {
	return pickAoECell(mobsIn, bx, by, lv, sp, maxSP, quickOnly, solo, ready, nil)
}

// pickAoECell: pickAoE with a check for ground cells (walkable, in line of sight; nil = any cell). Owner 2026-11-14:
// "cast AOE spell at the center where it's most impactful" - every cell in casting range is scored by the monsters
// its area covers (weighted by the spell's element against each), not just the cells monsters stand on.
func pickAoECell(mobsIn []aoeMob, bx, by int, lv func(id int) (lv, sp int), sp, maxSP int, quickOnly, solo bool, ready func(id int) bool, okCell func(x, y int) bool) (int, int, int, *Entity) {
	for i, s := range aoeSpells {
		l, cost := lv(s.id)
		if l <= 0 || !ready(s.id) || sp-cost < aoeReserve(maxSP) || (quickOnly && s.castMs(l) > aoeQuickMs) {
			continue
		}
		if cx, cy, ce, ok := bestAoECell(mobsIn, bx, by, s, solo, okCell); ok {
			return i, cx, cy, ce
		}
	}
	return -1, 0, 0, nil
}

// bestAoECell: the centre for spell s that covers the most monsters (element-weighted; ties: the most central
// cell). A centre qualifies with 3+ monsters in the area (2+ when one is strong), no monster someone outside the
// party fights, most of them not resisting the element, and - solo - no boss/risky monster and no swarm.
func bestAoECell(in []aoeMob, bx, by int, s aoeSpell, solo bool, okCell func(x, y int) bool) (int, int, *Entity, bool) {
	near := 0
	for _, m := range in {
		if dist(bx, by, m.x, m.y) <= aoeCastRange+s.radius {
			near++
		}
	}
	if near < 2 {
		return 0, 0, nil, false
	}
	bestScore, bestSum := -1.0, 0
	var bcx, bcy int
	var bce *Entity
	eval := func(cx, cy int, onMob *Entity) {
		n, swarm, sumd, cd := 0, 0, 0, 99
		score, strong := 0.0, false
		var ce *Entity
		for _, m := range in {
			d := dist(cx, cy, m.x, m.y)
			if m.aggro && d <= s.radius+3 { // the swarm around the area wakes up too
				swarm++
			}
			if d > s.radius {
				continue
			}
			if m.foreign || (solo && m.risky) {
				return
			}
			n++
			sumd += d
			score += elemMul(s.elem, m.elem)
			strong = strong || m.strong
			if d < cd {
				ce, cd = m.e, d
			}
		}
		if (solo && swarm > aoeSoloSwarm) || n < 2 || (n < aoeMinMobs && !strong) || score < 0.75*float64(n) {
			return
		}
		if onMob != nil {
			ce = onMob
		}
		if score > bestScore || (score == bestScore && sumd < bestSum) {
			bestScore, bestSum, bcx, bcy, bce = score, sumd, cx, cy, ce
		}
	}
	if s.ground {
		for x := bx - aoeCastRange; x <= bx+aoeCastRange; x++ {
			for y := by - aoeCastRange; y <= by+aoeCastRange; y++ {
				if x < 0 || y < 0 || (okCell != nil && !okCell(x, y)) {
					continue
				}
				eval(x, y, nil)
			}
		}
	} else { // cast on a monster: its splash is centred on it
		for _, m := range in {
			if dist(bx, by, m.x, m.y) <= aoeCastRange {
				eval(m.x, m.y, m.e)
			}
		}
	}
	return bcx, bcy, bce, bestScore > 0 && bce != nil
}

// partyFriend (b.mu held): is this account one of us - ourselves, a party member (Legion bot or the human), or a
// member the server sent party HP for. Before, only party HP packets counted, so a monster held by a partymate the
// server had not yet sent HP for counted as someone else's fight and blocked every area spell on the crowd.
func (b *Bot) partyFriend() func(id uint32) bool {
	ids := map[uint32]bool{b.aid: true}
	for id := range b.partyHP {
		ids[id] = true
	}
	if p := partyOf(b); p != nil {
		for _, m := range p.Members {
			if m != b && m.aid != 0 {
				ids[m.aid] = true
			}
		}
		if p.Human != "" {
			if h, ok := humanAt(p.Human); ok && h.AID != 0 {
				ids[h.AID] = true
			}
		}
	}
	return func(id uint32) bool { return id != 0 && ids[id] }
}

// strongMob: a monster that makes a group of 2 worth an area spell.
func strongMob(mb *Mob, plv int) bool {
	return mb != nil && (mb.Boss || mb.Champion || mb.Level >= plv || mb.HP >= 15000)
}

// aoeCast (b.mu held, from fight): true = an area spell (or a Fire Wall) was sent this tick.
func (b *Bot) aoeCast(e *Entity, now time.Time) bool {
	if family(b.Class) != famMage || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	p := partyOf(b)
	tank := false
	if p != nil {
		for _, m := range p.Members {
			if m != b && partyRole(m) == roleTank {
				tank = true
				break
			}
		}
	}
	solo := p == nil
	pcFriend := b.partyFriend()
	plv := b.powerLv()
	var in []aoeMob
	hitMe := false
	var walker *Entity
	wd := 99
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		ox, oy := o.pos(now)
		if dist(b.X, b.Y, ox, oy) > 14 {
			continue
		}
		m := aoeMob{e: o, x: ox, y: oy}
		if !claims.free(b.MapName, o.ID, b.aid) {
			m.foreign = true
			claims.mu.Lock()
			if cl, ok := claims.m[b.MapName][o.ID]; ok && pcFriend(cl.bot) {
				m.foreign = false
			}
			claims.mu.Unlock()
		}
		if t := b.ents[o.Target]; t != nil && t.Type == objPC && now.Sub(o.TargetAt) < 10*time.Second && !pcFriend(o.Target) {
			m.foreign = true
		}
		if mb := mobs[o.Class]; mb != nil {
			m.risky = mb.Boss || mb.Champion || mb.Level > plv+10
			m.aggro = mb.Aggressive
			m.strong = strongMob(mb, plv)
			m.elem = mb.Element
			if mb.worthless() {
				continue // plants don't make a group
			}
		}
		if now.Sub(o.LastHitMe) < 2*time.Second {
			hitMe = true
		}
		// a monster walking in on us (Fire Wall)
		if (o.Target == b.aid || now.Sub(o.LastHitMe) < 4*time.Second) && !m.foreign {
			if d := dist(b.X, b.Y, ox, oy); d >= 2 && d <= 5 && d < wd {
				walker, wd = o, d
			}
		}
		in = append(in, m)
	}
	if b.buffAt == nil {
		b.buffAt = map[string]time.Time{}
	}
	cdKey := func(id int) string { return "aoe:" + itoa(id) }
	ready := func(id int) bool {
		return now.After(b.buffAt[cdKey(id)]) && now.After(b.skillFailUntil[id])
	}
	lv := func(id int) (int, int) {
		si := b.skills[id]
		if si == nil || si.Lv <= 0 {
			return 0, 0
		}
		return si.Lv, max(si.SP, 1)
	}
	// run 7: range and line of sight from where we really are (b.X is a walk's start): rAthena drops a ground spell
	// whose cell is out of range from the server's position without a packet (Storm Gust 43% silent)
	mx, my := b.myPos(now)
	var okCell func(x, y int) bool
	if gm := maps[b.MapName]; gm != nil {
		okCell = func(x, y int) bool { return gm.Walkable(x, y) && gm.LineWalkable(mx, my, x, y) }
	}
	i, cx, cy, ce := pickAoECell(in, mx, my, lv, b.SP, b.MaxSP, hitMe && !tank, solo, ready, okCell)
	if i < 0 && b.tracing() {
		// measurement: a crowd in range and no area spell - why (lessons.md 2026-11-14, Zen.fox)
		n, foreign := 0, 0
		for _, m := range in {
			if dist(b.X, b.Y, m.x, m.y) <= aoeCastRange+2 {
				n++
				if m.foreign {
					foreign++
				}
			}
		}
		if n >= aoeMinMobs && now.Sub(b.aoeWhyAt) > 3*time.Second {
			b.aoeWhyAt = now
			b.tracef("AOE none: %d mobs near (%d foreign) sp=%d/%d quickOnly=%v solo=%v", n, foreign, b.SP, b.MaxSP, hitMe && !tank, solo)
		}
	}
	if i >= 0 {
		s := aoeSpells[i]
		l, _ := lv(s.id)
		ms := s.castMs(l)
		b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
		b.buffAt[cdKey(s.id)] = now.Add(time.Duration(max(ms, s.cdMs)) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		b.setState("casting " + skillName(s.id) + " on a group")
		b.aoeCastUntil = now.Add(time.Duration(ms) * time.Millisecond)
		if s.ground {
			go b.send(newPkt(0x0366).u16(uint16(l)).u16(uint16(s.id)).u16(uint16(cx)).u16(uint16(cy)).b) // UseSkillToPos
		} else {
			go b.useSkill(s.id, l, ce.ID)
		}
		return true
	}
	// Fire Wall: a monster walking in on us 2-5 cells away (solo, or being hit with a tank in the party too: the tank
	// isn't holding it) -> a wall 2 cells out toward it
	if walker != nil && (!tank || hitMe) {
		if l, cost := lv(mgFireWall); l > 0 && ready(mgFireWall) && b.SP-cost >= aoeReserve(b.MaxSP) {
			wx, wy := walker.pos(now)
			fx, fy := b.X+sign(wx-b.X)*min(2, abs(wx-b.X)), b.Y+sign(wy-b.Y)*min(2, abs(wy-b.Y))
			ms := (1760-160*l)*7/10 + 400 + 300
			b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
			b.buffAt[cdKey(mgFireWall)] = now.Add(5 * time.Second)
			b.targetAt, b.attackSentFor = now, 0
			go b.send(newPkt(0x0366).u16(uint16(l)).u16(mgFireWall).u16(uint16(fx)).u16(uint16(fy)).b)
			return true
		}
	}
	return false
}

func skillName(id int) string {
	switch id {
	case wzStormGust:
		return "Storm Gust"
	case wzVermilion:
		return "Lord of Vermilion"
	case wzMeteor:
		return "Meteor Storm"
	case mgThunder:
		return "Thunder Storm"
	case wzHeavenDrv:
		return "Heaven's Drive"
	case mgFireBall:
		return "Fire Ball"
	case mgFireWall:
		return "Fire Wall"
	}
	return "skill " + itoa(id)
}

// hasAoESpell (b.mu held): a Mage-line caster that knows an area spell (the party tank gathers crowds for it).
func (b *Bot) hasAoESpell() bool {
	if family(b.Class) != famMage {
		return false
	}
	for _, s := range aoeSpells {
		if si := b.skills[s.id]; si != nil && si.Lv > 0 {
			return true
		}
	}
	return false
}
