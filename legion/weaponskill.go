package main

// Area weapon skills for the non-caster classes (Mage/Wizard area spells live in aoe.go). Online class guides
// (iRO Wiki Archer/Hunter, 2026-11-14 build review): an Archer levels with Double Strafe on single monsters and
// Arrow Shower on groups. QA 2026-11-14: 4 traced Archers/Hunters fired only Double Strafe (39 casts in 4 min from
// one Archer), never Arrow Shower, although every Archer plan learns it.
//
// weaponAoE (from fight, b.mu held): an Archer-line character with a bow and arrows casts Arrow Shower (ground, 3x3
// up to lv5, 5x5 from lv6, knock-back 2) on the cell where 3+ monsters stand, keeping the non-caster SP floor.
// Same courtesy as aoe.go: never on a group holding a monster someone outside our party is fighting, and solo never
// on a boss/champion or a monster far above our power.

import "time"

const (
	acConcentration = 45 // AC Improve Concentration (self buff: AGI/DEX +2+lv%)
	acShower        = 47 // AC Arrow Shower
	arrowShowerMin  = 3  // monsters in the area (Arrow Shower lv5 200% each vs Double Strafe lv10 2x200% single)
)

// arrowShowerRadius: rAthena re/skill_db.yml AC_SHOWER SplashArea.
func arrowShowerRadius(lv int) int {
	if lv >= 6 {
		return 2
	}
	return 1
}

// bestCluster: the centre (a monster within reach of bx,by) with the most monsters inside radius; ok=false when
// none reaches minN or every big enough group holds a foreign/risky monster.
func bestCluster(in []aoeMob, bx, by, reach, radius, minN int, solo bool) (cx, cy int, ce *Entity, ok bool) {
	bn := 0
	for _, c := range in {
		if dist(bx, by, c.x, c.y) > reach {
			continue
		}
		n, bad := 0, false
		for _, m := range in {
			if dist(c.x, c.y, m.x, m.y) > radius {
				continue
			}
			n++
			if m.foreign || (solo && m.risky) {
				bad = true
			}
		}
		if !bad && n >= minN && n > bn {
			bn, cx, cy, ce, ok = n, c.x, c.y, c.e, true
		}
	}
	return
}

// viewMobs: monsters within 14 cells, flagged like aoe.go (foreign = someone outside our party fights it).
func (b *Bot) viewMobs(now time.Time) []aoeMob {
	pcFriend := b.partyFriend()
	plv := b.powerLv()
	var in []aoeMob
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
			if mb.worthless() {
				continue // plants don't count as a group
			}
		}
		in = append(in, m)
	}
	return in
}

// ---------------------------------------------------------------- crowd skills (every class but the mage line)
//
// Owner rule 2026-11-14 (RULES.md 3): "all skills should use AOE skills in crowds, targeting the central mob". One
// table for the weapon / support classes; aoe.go keeps the Mage/Wizard/Sage spells. Each entry: the class family
// that has it, how it is aimed (ground cell / the monster at the centre / around ourselves), the radius it hits
// (rAthena re/skill_db.yml SplashArea), its reach, what it needs (weapon, cart, gemstone, a race of target) and its
// pacing. crowdCast casts the first learned, affordable entry whose best cluster holds 3+ monsters, aimed at the
// monster in the middle of the cluster; self-centred skills only count what stands around us (no stepping in).
// Same courtesy as aoe.go: no group with a monster someone outside our party fights; solo, no boss/champion group.
// Magnum Break (ai.go, tanks: 2+ around) and Hammer Fall / Grand Cross (skillbuff.go) keep their own rules.

type crowdAim int

const (
	aimGround crowdAim = iota // UseSkillToPos on the cluster centre
	aimTarget                 // on the monster at the centre (its splash hits the rest)
)

type crowdSkill struct {
	id      int
	fam     int
	aim     crowdAim
	radius  func(lv int) int
	reach   int      // cells from us to the centre
	weapons []string // weapon SubTypes (nil = any)
	cart    bool     // needs a pushcart
	gem     int      // item consumed per cast (0 = none)
	races   []string // only when the centre group is of these races (Magnus: undead / demon)
	castMs  int      // cast + after-cast delay, for pacing
	cdMs    int      // own cooldown
	minSP   int      // SP cost fallback when the server didn't tell us
}

const itemBlueGem = 717

func fixedR(r int) func(int) int { return func(int) int { return r } }

var crowdSkills = []crowdSkill{
	// Archer line: Arrow Shower, ground, 3x3 (5x5 from lv6), needs a bow and arrows
	{id: acShower, fam: famArcher, aim: aimGround, radius: arrowShowerRadius, reach: 9, weapons: []string{"Bow"}, castMs: 700, minSP: 15},
	// Knight: Brandish Spear (spear, 5x5 in front) and Bowling Bash (5x5 splash around the target, knock-back)
	{id: 57, fam: famSword, aim: aimTarget, radius: fixedR(2), reach: 2, weapons: []string{"1hSpear", "2hSpear"}, castMs: 1200, cdMs: 1000, minSP: 12},
	{id: 62, fam: famSword, aim: aimTarget, radius: fixedR(2), reach: 2, castMs: 1000, minSP: 13},
	// Merchant line: Cart Revolution (3x3 around the target, needs the cart)
	{id: 153, fam: famMerchant, aim: aimTarget, radius: fixedR(1), reach: 1, cart: true, castMs: 800, minSP: 12},
	// Priest: Magnus Exorcismus on undead / demons (7x7, a Blue Gemstone, long cast - only with 4+ of them)
	{id: 79, fam: famAcolyte, aim: aimGround, radius: fixedR(3), reach: 9, gem: itemBlueGem, races: []string{"Undead", "Demon"}, castMs: 4500, cdMs: 6000, minSP: 40},
}

// crowdMin: monsters needed in the area (owner: 3+; Magnus costs a gemstone and a long cast: 4+)
func (c crowdSkill) crowdMin() int {
	if c.gem != 0 {
		return 4
	}
	return arrowShowerMin
}

func raceOf(class int) string {
	lookupOnce.Do(loadLookup)
	if ex := mobExtras[class]; ex != nil {
		return ex.Race
	}
	return ""
}

// weaponAoE (from fight, b.mu held): the crowd skill for this tick; true = one was sent.
func (b *Bot) weaponAoE(e *Entity, now time.Time) bool {
	f := family(b.Class)
	if f == famMage || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	var view []aoeMob
	for _, c := range crowdSkills {
		if c.fam != f {
			continue
		}
		si := b.skills[c.id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[c.id]) || now.Before(b.buffAt["crowd:"+itoa(c.id)]) {
			continue
		}
		if c.weapons != nil && !b.weaponOK(c.weapons, true) || c.id == acShower && b.noArrows() || c.cart && !b.hasCart ||
			c.gem != 0 && b.countItem(c.gem) == 0 {
			continue
		}
		cost := max(si.SP, c.minSP)
		floor := b.MaxSP * skillSPFloor / 100
		if b.isHealer() {
			floor = b.MaxSP / 2 // heals first
		}
		floor += b.keyReserve(now) // skillbuff.go: a due key self-buff first (Two-Hand Quicken before Bowling Bash)
		if b.SP-cost < floor {
			continue
		}
		if view == nil {
			view = b.viewMobs(now)
		}
		in := view
		if c.races != nil {
			in = nil
			for _, m := range view {
				r := raceOf(m.e.Class)
				for _, want := range c.races {
					if r == want {
						in = append(in, m)
					}
				}
			}
		}
		cx, cy, ce, ok := bestCluster(in, b.X, b.Y, c.reach, c.radius(si.Lv), c.crowdMin(), partyOf(b) == nil)
		if !ok {
			continue
		}
		lv := si.Lv
		b.skillReady = now.Add(time.Duration(c.castMs+b.P.ReactionMs/2) * time.Millisecond)
		b.buffAt["crowd:"+itoa(c.id)] = now.Add(time.Duration(max(c.cdMs, c.castMs)) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0 // resume auto-attack after the skill
		b.setState("area skill " + itoa(c.id) + " on a group")
		if c.aim == aimGround {
			go b.send(newPkt(0x0366).u16(uint16(lv)).u16(uint16(c.id)).u16(uint16(cx)).u16(uint16(cy)).b) // UseSkillToPos
		} else {
			go b.useSkill(c.id, lv, ce.ID)
		}
		return true
	}
	return false
}

// healAttack (from fight, b.mu held): an acolyte-line character with Heal casts it on an Undead-element monster -
// Heal deals holy damage to the undead (iRO Wiki Acolyte: "Heal undead monsters" is how a support Acolyte levels
// solo; strategy.go already sends acolytes to the undead dungeons). Before this, no bot ever used Heal as an attack:
// support Acolytes with STR 1 hit Zombies with a Rod. In a party the healer keeps half its SP for heals.
func (b *Bot) healAttack(e *Entity, now time.Time) bool {
	if family(b.Class) != famAcolyte || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	if b.isHealer() && partyOf(b) != nil {
		return false // owner 2026-11-14: a party healer heals, buffs, follows or sits - it doesn't fight
	}
	mb := mobs[e.Class]
	if mb == nil || mb.Element != "Undead" {
		return false
	}
	si := b.skills[alHeal]
	if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[alHeal]) {
		return false
	}
	cost := max(si.SP, 10+3*si.Lv)
	floor := b.MaxSP * 20 / 100
	if partyOf(b) != nil && b.isHealer() {
		floor = b.MaxSP / 2
	}
	if b.SP-cost < floor {
		return false
	}
	ex, ey := e.pos(now)
	if dist(b.X, b.Y, ex, ey) > healRange {
		return false
	}
	b.skillReady = now.Add(time.Duration(1000+b.P.ReactionMs/2) * time.Millisecond)
	b.targetAt, b.attackSentFor = now, 0
	b.setState("Heal on " + mobName(e.Class))
	go b.useSkill(alHeal, si.Lv, e.ID)
	return true
}

// ---------------------------------------------------------------- Monk combos
//
// iRO Wiki Monk (combo build): Triple Attack procs on auto-attacks; during its combo window Chain Combo follows, and
// after Chain Combo, Combo Finish (1 spirit sphere). The window is short (rAthena combo delay, ~0.3-1 s), so the next
// skill is sent the moment the server shows our previous one hitting (0x01de from bot.go). Before this the combo
// skills (Chain Combo 5, Combo Finish 5 in the plan) were never cast.
const (
	moTriple = 263
	moChain  = 272
	moFinish = 273
	moAsura  = 271
)

// comboNext (b.mu held): our skill skillID just hit dst - chain the next combo step if we can.
func (b *Bot) comboNext(skillID int, dst uint32, now time.Time) {
	var next int
	switch skillID {
	case moTriple:
		next = moChain
	case moChain:
		next = moFinish
	case moFinish:
		// Asura Strike (Guillotine Fist) as the combo's finisher - only on a tough target (boss / 5,000+ HP), with
		// nearly full SP (it spends all SP) and 4+ spirit spheres left; never on trash (build review 2026-11-14)
		e := b.ents[dst]
		mb := (*Mob)(nil)
		if e != nil {
			mb = mobs[e.Class]
		}
		if mb == nil || !(mb.Boss || mb.HP >= 5000) || b.MaxSP <= 0 || b.SP*100/b.MaxSP < 85 || b.spiritCasts < 4 {
			return
		}
		next = moAsura
	default:
		return
	}
	si := b.skills[next]
	if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[next]) || b.MaxSP <= 0 {
		return
	}
	if next == moFinish && b.spiritCasts < 1 {
		return
	}
	cost := max(si.SP, 11)
	if next != moAsura && b.SP-cost < b.MaxSP*skillSPFloor/100 {
		return
	}
	// run 6: the server refuses a combo step sent during the previous step's delay (canact_tick; the SKILLINTERVAL
	// failure is not even shown without @showdelay) and opens the combo for only ~300 ms after it (SC_COMBO lasts
	// delay + 300 ms). It announces the delay (0x01d2, sent just before the 0x01de): cast right after it. Before this,
	// 0 Chain Combos in 6 traced Monks - every one was sent the moment Triple Attack hit.
	wait := 0 * time.Millisecond
	if !b.comboWaitAt.IsZero() && now.Sub(b.comboWaitAt) < time.Second {
		wait = b.comboWait - now.Sub(b.comboWaitAt) + 40*time.Millisecond
	}
	wait = max(wait, 0)
	b.skillReady = now.Add(wait + 700*time.Millisecond)
	b.targetAt, b.attackSentFor = now, 0
	lv := si.Lv
	go func() {
		time.Sleep(wait)
		// run 7: Chain Combo often kills (1,000-1,450 damage): a Combo Finish at a dead monster is dropped silently
		// (78% of Combo Finishes were). Only while the target is still there.
		b.mu.Lock()
		alive := b.ents[dst] != nil
		b.mu.Unlock()
		if alive {
			b.useSkill(next, lv, dst)
		}
	}()
}

// ---------------------------------------------------------------- Asura Monk (build variant monk_asura, run 4)
//
// iRO Wiki Monk (Asura build): Fury (Critical Explosion: 5 spirit spheres, 3 min) opens Asura Strike; Asura Strike
// (Guillotine Fist) spends 5 spheres and ALL SP for one huge hit, damage growing with the SP spent. The Asura Monk
// keeps 5 spheres up (skillbuff.go Call Spirits), enters Fury when a fight starts at high SP, resummons, and fires
// Asura Strike at 85%+ SP on a monster worth it (boss / champion, or HP >= 20 x its base level), then auto-attacks
// while SP refills (Spiritual Cadence sitting regen comes from the rest logic). The combo Monk keeps comboNext only.
const moFury = 270

func (b *Bot) asuraBurst(e *Entity, now time.Time) bool {
	p := b.plan()
	if p == nil || p.ID != "monk_asura" || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	fury, asura := b.skills[moFury], b.skills[moAsura]
	if fury == nil || fury.Lv <= 0 || asura == nil || asura.Lv <= 0 || b.spiritCasts < 5 || b.SP*100/b.MaxSP < 85 {
		return false
	}
	mb := mobs[e.Class]
	if mb == nil || !(mb.Boss || mb.HP >= 20*b.BaseLvOr()) {
		return false // trash: auto-attack, keep the SP
	}
	if now.After(b.buffAt["asura:fury"]) {
		if now.Before(b.skillFailUntil[moFury]) {
			return false
		}
		b.buffAt["asura:fury"] = now.Add(175 * time.Second) // Fury lasts 180 s
		b.skillReady = now.Add(1200 * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		b.setState("Fury (Asura Strike next)")
		go b.useSkill(moFury, fury.Lv, b.aid)
		return true // the spheres it spent come back through Call Spirits (skillbuff.go)
	}
	if now.Before(b.skillFailUntil[moAsura]) {
		return false
	}
	ex, ey := e.pos(now)
	if dist(b.X, b.Y, ex, ey) > 2 {
		return false // closeIn walks us there first
	}
	b.skillReady = now.Add(4 * time.Second) // cast + after-cast delay
	b.targetAt, b.attackSentFor = now, 0
	b.setState("Asura Strike on " + mobName(e.Class))
	go b.useSkill(moAsura, asura.Lv, e.ID)
	return true
}
