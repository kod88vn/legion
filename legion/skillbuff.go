package main

// Fight skills that aren't "hit the target": self-buffs for melee classes (Blacksmith Adrenaline Rush / Over Thrust /
// Weapon Perfection, Knight Two-Hand Quicken, Endure, a Monk's Blessing / Increase AGI on itself and Call Spirits) and
// Blacksmith Hammer Fall on a pack. QA 2026-11-14 03:57: 8/8 Blacksmiths, 6/6 Alchemists and 2/2 Monks fought a whole
// minute with full SP - these skills were never cast (party.go's buff code only runs for the healer).

import (
	"strconv"
	"time"
)

const (
	bsHammerFall = 110
	moCallSpirit = 261
	crGrandCross = 254
)

type selfBuff struct {
	id      int
	weapons []string // weapon SubTypes allowed; nil = any weapon or barehanded
	armed   bool     // needs some weapon in hand
	dur     func(lv int) time.Duration
	near    int              // only with at least this many monsters within 2 cells
	cost    func(lv int) int // SP per level: cast the highest level that keeps the SP rule (nil = learned level)
	key     bool             // the class's core buff (guides: "keep it up at all times"): cast down to the non-caster SP floor, not only above 60%
	shield  bool             // needs a shield in the left hand
}

var axeMace = []string{"1hAxe", "2hAxe", "Mace"}

// rAthena re/skill_db.yml weapon requirements and durations
var selfBuffs = []selfBuff{
	{id: 111, key: true, weapons: axeMace, dur: func(lv int) time.Duration { return time.Duration(30*lv) * time.Second }, cost: func(lv int) int { return 17 + 3*lv }}, // BS Adrenaline Rush (re skill_db 20-32 SP)
	{id: 113, armed: true, dur: func(lv int) time.Duration { return time.Duration(20*lv) * time.Second }},                                                              // BS Over Thrust
	{id: 112, armed: true, dur: func(lv int) time.Duration { return time.Duration(10*lv) * time.Second }},                                                              // BS Weapon Perfection
	// KN Two-Hand Quicken (+30% ASPD): owner 2026-11-14 "does not use skills fitting its class" - 3 traced Knights never
	// cast it in 12 min: they ran at 27-35% SP (Bowling Bash eats it) and the 60% rule never let it through
	// cost (re skill_db 14-50 SP): a lower level that fits beats none - a 220-SP Knight at 80 SP could never pay lv10's 50
	{id: 60, key: true, weapons: []string{"2hSword"}, dur: func(lv int) time.Duration { return time.Duration(30*lv) * time.Second }, cost: func(lv int) int { return 10 + 4*lv }},
	// CR Auto Guard (block chance with a shield, 5 min) and Reflect Shield (5 min): the Crusader tank's buffs
	{id: 249, key: true, shield: true, dur: func(lv int) time.Duration { return 300 * time.Second }, cost: func(lv int) int { return 10 + 2*lv }},
	{id: 252, shield: true, dur: func(lv int) time.Duration { return 300 * time.Second }},
	{id: alBlessing, dur: func(lv int) time.Duration { return time.Duration(40+20*lv) * time.Second }}, // Blessing on itself
	{id: alIncAgi, dur: func(lv int) time.Duration { return time.Duration(40+20*lv) * time.Second }},   // Increase AGI on itself
	// AC Improve Concentration (AGI/DEX +2+lv%, 40+20*lv s, 20+5*lv SP): the Archer guides keep it up all the time;
	// at lv10 it costs 70 SP of an Archer's ~110, so the level is chosen to fit (QA 2026-11-14: never cast)
	{id: acConcentration, key: true, dur: func(lv int) time.Duration { return time.Duration(40+20*lv) * time.Second }, cost: func(lv int) int { return 20 + 5*lv }},
	{id: 8, near: 2, dur: func(lv int) time.Duration { return time.Duration(7+3*lv) * time.Second }}, // SM Endure (when mobbed)
	// AS Enchant Poison (poison weapon element + poison chance, 15+15*lv s): the dagger / Sonic Blow Assassins' buff
	{id: 138, armed: true, dur: func(lv int) time.Duration { return time.Duration(15+15*lv) * time.Second }},
	// run 5 variants: CR Spear Quicken (ASPD with a two-handed spear, 30*lv s, re skill_db 24-60 SP), SA Hindsight /
	// Auto Spell (bolts on melee hits, 120+30*(lv-1) s, 35 SP; bot.go answers the spell list) and PR Impositio Manus
	// (+ATK, 60 s) for the battle priest
	{id: 258, key: true, weapons: []string{"2hSpear"}, dur: func(lv int) time.Duration { return time.Duration(30*lv) * time.Second }, cost: func(lv int) int { return 20 + 4*lv }},
	{id: saAutoSpell, key: true, armed: true, dur: func(lv int) time.Duration { return time.Duration(90+30*lv) * time.Second }, cost: func(lv int) int { return 35 }},
	{id: 66, armed: true, dur: func(lv int) time.Duration { return 60 * time.Second }},
}

const saAutoSpell = 279

func (b *Bot) weaponOK(weapons []string, armed bool) bool {
	w := b.weaponSub()
	if armed && w == "" {
		return false
	}
	if weapons == nil {
		return true
	}
	for _, x := range weapons {
		if x == w {
			return true
		}
	}
	return false
}

// fightSkills (b.mu held, from fight): cast one buff or Hammer Fall if one is due; true = a skill was sent this tick.
// Buffs only with more than 60% SP left after the cast, so the attack skill and emergencies keep their SP.
func (b *Bot) fightSkills(e *Entity, now time.Time) bool {
	// a solo support acolyte buffs itself too (Blessing, Increase AGI): in a party the healer's buff code does it
	if b.MaxSP <= 0 || now.Before(b.skillReady) || (partyRole(b) == roleSupport && partyOf(b) != nil) || b.Sitting || b.steelBodyOn(now) {
		return false
	}
	ex, ey := e.pos(now)
	reach := 3
	if b.weaponSub() == "Bow" { // archers fight from 9 cells: their buffs were never cast (QA 2026-11-14)
		reach = 10
	}
	if dist(b.X, b.Y, ex, ey) > reach { // in the fight, not on the way
		return false
	}
	if b.Deaths != b.buffDeaths { // died: every buff is gone
		b.buffDeaths, b.spiritCasts = b.Deaths, 0
		for _, sb := range selfBuffs {
			delete(b.buffAt, "self:"+strconv.Itoa(sb.id))
		}
		delete(b.buffAt, "self:"+strconv.Itoa(moCallSpirit))
	}
	ok := func(id int) *SkillInfo {
		si := b.skills[id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[id]) {
			return nil
		}
		return si
	}
	nearMe, nearTgt := 0, 0
	for _, o := range b.ents {
		if o.Type != objMob {
			continue
		}
		ox, oy := o.pos(now)
		if dist(b.X, b.Y, ox, oy) <= 2 {
			nearMe++
		}
		if dist(ex, ey, ox, oy) <= 2 {
			nearTgt++
		}
	}
	cast := func(id, lv, ms int) {
		b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0 // resume auto-attack after the skill
		go b.useSkill(id, lv, b.aid)
	}
	// Hammer Fall: stun everything in 5x5 around the target when 2+ monsters are there (3+ never happened in 10 min of 8 traced Blacksmiths) (keeps a 25% SP reserve)
	// run 7: Hammer Fall's range is 1 (re skill_db): cast only next to the target from where we really stand (a cell
	// 2-3 away was dropped by the server without a packet - 33% of Hammer Falls)
	hmx, hmy := b.myPos(now)
	if si := ok(bsHammerFall); si != nil && nearTgt >= 2 && dist(hmx, hmy, ex, ey) <= 1 && ex == e.DX && ey == e.DY &&
		b.weaponOK([]string{"Dagger", "1hSword", "1hAxe", "2hAxe", "Mace"}, true) &&
		b.SP-si.SP >= b.MaxSP/4 && !b.keyBuffDue(now) {
		b.skillReady = now.Add(time.Duration(1000+b.P.ReactionMs/2) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		lv := si.Lv
		go b.send(newPkt(0x0366).u16(uint16(lv)).u16(bsHammerFall).u16(uint16(ex)).u16(uint16(ey)).b) // UseSkillToPos
		return true
	}
	// CR Grand Cross (holy cross around itself, costs 20% HP): the Crusader's area skill (iRO Wiki Crusader), with 3+
	// monsters next to it, healthy (HP >= 70%) and above the SP floor; strongest against undead / demons
	if si := ok(crGrandCross); si != nil && nearMe >= 3 && b.hpFrac() >= 0.7 && (b.SP-max(si.SP, 37))*100/b.MaxSP > skillSPFloor {
		b.setState("Grand Cross on a group")
		cast(crGrandCross, si.Lv, 2000)
		return true
	}
	spOK := func(si *SkillInfo) bool { return (b.SP-max(si.SP, 1))*100/b.MaxSP > 60 }
	for _, sb := range selfBuffs {
		key := "self:" + strconv.Itoa(sb.id)
		si := ok(sb.id)
		if si == nil || now.Before(b.buffAt[key]) || nearMe < sb.near || !b.weaponOK(sb.weapons, sb.armed) ||
			sb.shield && b.equipped[0x20] == nil {
			continue
		}
		lv := si.Lv
		left := 60
		if sb.key {
			left = skillSPFloor
		}
		if sb.cost != nil {
			lv = buffLevelLeft(si.Lv, sb.cost, b.SP, b.MaxSP, left)
		} else if (b.SP-max(si.SP, 1))*100/b.MaxSP <= left {
			lv = 0
		}
		if lv <= 0 {
			continue
		}
		b.buffAt[key] = now.Add(sb.dur(lv) - 3*time.Second)
		cast(sb.id, lv, 1000)
		return true
	}
	// Call Spirits: one sphere per cast up to the skill level, counted from the server's sphere packet (bot.go 0x01d0):
	// a timer-based recast came before the 10-min expiry and was refused with all spheres up (QA 2026-11-14)
	key := "self:" + strconv.Itoa(moCallSpirit)
	// Spheres are what Combo Finish (1 each) and Asura Strike (all 5) spend: a Monk with either learned summons them down
	// to the key-buff floor (run 5 trace: TheZerolord, Combo Finish 4, cast Chain Combo 71x in 5 min and Combo Finish 0x
	// - the combo kept SP under the 60% buff line, so Call Spirits never came and every Combo Finish lacked its sphere)
	if si := ok(moCallSpirit); si != nil && b.spiritCasts < si.Lv && now.After(b.buffAt[key]) && (spOK(si) || b.spheresKey() && (b.SP-max(si.SP, 8))*100/b.MaxSP > skillSPFloor) {
		b.buffAt[key] = now.Add(2500 * time.Millisecond) // the sphere packet arrives after the 1 s cast
		cast(moCallSpirit, si.Lv, 1500)
		return true
	}
	return false
}

// buffLevel: the highest level up to learned whose SP cost leaves more than 60% SP (0 = none fits).
func buffLevel(learned int, cost func(int) int, sp, maxSP int) int {
	return buffLevelLeft(learned, cost, sp, maxSP, 60)
}

// buffLevelLeft: the highest level up to learned whose SP cost leaves more than left% SP (0 = none fits).
func buffLevelLeft(learned int, cost func(int) int, sp, maxSP, left int) int {
	for lv := learned; lv >= 1; lv-- {
		if maxSP > 0 && (sp-cost(lv))*100/maxSP > left {
			return lv
		}
	}
	return 0
}

// isHealer: the party healer role (Acolyte / Priest / High Priest line). Monks and Champions belong to the acolyte
// family but fight in melee: no healer resting, party buffing or party healing for them (QA 2026-11-14 05:41).
func (b *Bot) isHealer() bool {
	return family(b.Class) == famAcolyte && partyRole(b) == roleSupport
}

// spheresKey (b.mu held): Call Spirits is a key skill for this Monk - it has Combo Finish or Asura Strike learned.
func (b *Bot) spheresKey() bool {
	for _, id := range []int{moFinish, moAsura} {
		if si := b.skills[id]; si != nil && si.Lv > 0 {
			return true
		}
	}
	return b.occultMonk() // run 6: Occult Impaction spends a sphere a hit, Mental Strength needs 5
}

// keyBuffDue (b.mu held): a learned "key" self-buff (Adrenaline Rush, Two-Hand Quicken, Auto Guard, Improve
// Concentration) that fits the weapon in hand has run out. Hammer Fall waits for it: run 5 trace, Frost Deals
// (Blacksmith, Adrenaline Rush 5) cast Hammer Fall 29x in 5 min, sat at 29% SP and never cast Adrenaline Rush - the
// stun went first in fightSkills and kept SP under the 30% key floor.
func (b *Bot) keyBuffDue(now time.Time) bool {
	for _, sb := range selfBuffs {
		if !sb.key {
			continue
		}
		si := b.skills[sb.id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[sb.id]) || !b.weaponOK(sb.weapons, sb.armed) ||
			sb.shield && b.equipped[0x20] == nil {
			continue
		}
		if now.After(b.buffAt["self:"+strconv.Itoa(sb.id)]) {
			return true
		}
	}
	return false
}

// keyReserve (b.mu held): SP to hold back for key self-buffs that are due (their lowest level's cost), so attack and
// crowd skills don't spend it first. Run 5 traces: Knights cast Bowling Bash / Provoke / Magnum Break all fight long at
// 27-36% SP and never Two-Hand Quicken (theyukiwing THQ 10: 0 casts in 5 min).
func (b *Bot) keyReserve(now time.Time) int {
	r := 0
	for _, sb := range selfBuffs {
		if !sb.key {
			continue
		}
		si := b.skills[sb.id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[sb.id]) || !b.weaponOK(sb.weapons, sb.armed) ||
			sb.shield && b.equipped[0x20] == nil || !now.After(b.buffAt["self:"+strconv.Itoa(sb.id)]) {
			continue
		}
		c := max(si.SP, 1)
		if sb.cost != nil {
			c = sb.cost(1)
		}
		r += c
	}
	return r
}

// autoSpellPick: the spell a Hindsight Sage picks from the server's list (0x0afb: u16 op, u16 len, int32 ids): the one
// learned at the highest level (Renewal Hindsight casts it at that level, capped by Hindsight), later (stronger) list
// entries winning ties - e.g. Heaven's Drive / Earth Spike over a bolt of the same level.
func autoSpellPick(pk []byte, skills map[int]*SkillInfo) int {
	if len(pk) < 8 {
		return 0
	}
	best, bestScore := 0, -1
	for i, off := 0, 4; off+4 <= len(pk); i, off = i+1, off+4 {
		id := int(le.Uint32(pk[off:]))
		lv := 0
		if si := skills[id]; si != nil {
			lv = si.Lv
		}
		if sc := lv*100 + i; lv > 0 && sc > bestScore {
			best, bestScore = id, sc
		}
	}
	return best
}
