package main

// variantskill.go: fight AI for the build variants of run 6 (owner 2026-11-14: "the builds of the legions still lack
// variety"; CLASSES.md "Build variety across the Legion"): the Divest (strip) Rogue and the Occult Impaction / Mental
// Strength (Steel Body) Monk. Generic where it can be: any character with the skill learned uses it the same way.
//
// Divest (RG_STRIPWEAPON / RG_STRIPSHIELD, iRO Wiki Rogue, rAthena status.cpp): on a monster Divest Weapon takes 25% of
// its ATK and Divest Shield 15% of its DEF for the duration. A sensible strip Rogue strips the monsters that matter -
// bosses, champions and monsters at or above its level (strongMob) - not trash: the weapon first (the whole party
// takes less damage), then the shield (everyone hits harder). At most 2 tries per skill per monster (the chance
// grows with DEX; a failure is just a miss), SP above the non-caster floor.
//
// Occult Impaction (MO_INVESTIGATE, 1 sphere, range 2): damage grows with the target's DEF - the Occult Monk's main
// attack on armoured monsters (DEF >= 20 or strongMob), auto-attack on soft trash. Mental Strength (MO_STEELBODY, 5
// spheres, 200 SP, DEF/MDEF 90 for 30*lv s, slower ASPD and walk): the tank Monk's answer to a mob (3+ attackers) or a
// boss on it, never on trash; no other skill while it lasts (rAthena refuses them).

import (
	"strconv"
	"time"
)

const (
	rgStripWeapon = 215
	rgStripShield = 216
	moInvestigate = 266
	moSteelBody   = 268
)

// stripWorth: a monster worth a Divest (boss, champion, at/above our level, or 15k+ HP).
func stripWorth(mb *Mob, plv int) bool { return strongMob(mb, plv) }

// stripTarget (b.mu held, from fight): one Divest on a strong target if due; true = a skill was sent.
func (b *Bot) stripTarget(e *Entity, now time.Time) bool {
	if b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting || family(b.Class) != famThief {
		return false
	}
	mb := mobs[e.Class]
	if !stripWorth(mb, b.BaseLvOr()) {
		return false
	}
	ex, ey := e.pos(now)
	if clientDist(b.X, b.Y, ex, ey) > 1 {
		return false // closeIn walks us there first (range 1)
	}
	for _, id := range []int{rgStripWeapon, rgStripShield} {
		si := b.skills[id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[id]) {
			continue
		}
		key := "strip:" + strconv.Itoa(id) + ":" + strconv.FormatUint(uint64(e.ID), 10)
		if b.stripTries == nil || len(b.stripTries) > 200 {
			b.stripTries = map[string]int{}
		}
		if b.stripTries[key] >= 2 {
			continue
		}
		cost := max(si.SP, 12)
		if (b.SP-cost)*100/b.MaxSP <= skillSPFloor+b.keyReserve(now)*100/b.MaxSP {
			return false
		}
		b.stripTries[key]++
		b.skillReady = now.Add(time.Duration(1000+b.P.ReactionMs/2) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		b.setState(skillName(id) + " on " + mobName(e.Class))
		go b.useSkill(id, si.Lv, e.ID)
		return true
	}
	return false
}

// occultMonk (b.mu held): this Monk fights with Occult Impaction (plan monk_occult, or Occult 5 without the combo).
func (b *Bot) occultMonk() bool {
	if family(b.Class) != famAcolyte {
		return false
	}
	si := b.skills[moInvestigate]
	if si == nil || si.Lv <= 0 {
		return false
	}
	if p := b.plan(); p != nil && p.ID == "monk_occult" {
		return true
	}
	return false
}

// steelBodyOn (b.mu held): Mental Strength is up (no skills while it lasts).
func (b *Bot) steelBodyOn(now time.Time) bool {
	return now.Before(b.buffAt["self:"+strconv.Itoa(moSteelBody)])
}

// occultFight (b.mu held, from fight): Mental Strength when mobbed / on a boss, else Occult Impaction on an armoured
// target; true = a skill was sent this tick (or Mental Strength holds the skills).
func (b *Bot) occultFight(e *Entity, now time.Time) bool {
	if !b.occultMonk() || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	if b.steelBodyOn(now) {
		return false // auto-attack only (the server refuses skills under Mental Strength)
	}
	mb := mobs[e.Class]
	ex, ey := e.pos(now)
	d := clientDist(b.X, b.Y, ex, ey)
	if sb := b.skills[moSteelBody]; sb != nil && sb.Lv > 0 && b.spiritCasts >= 5 && b.SP >= 200 && !now.Before(b.skillFailUntil[moSteelBody]) {
		n, _, _, _ := b.attackers(now)
		if n >= 3 || (mb != nil && (mb.Boss || mb.Champion) && d <= 2) {
			b.buffAt["self:"+strconv.Itoa(moSteelBody)] = now.Add(time.Duration(30*sb.Lv)*time.Second + 2500*time.Millisecond)
			b.skillReady = now.Add(time.Duration(2700+b.P.ReactionMs/2) * time.Millisecond) // 2.5 s fixed cast
			b.targetAt, b.attackSentFor = now, 0
			b.setState("Mental Strength (mobbed)")
			go b.useSkill(moSteelBody, sb.Lv, b.aid)
			return true
		}
	}
	si := b.skills[moInvestigate]
	if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[moInvestigate]) || b.spiritCasts < 1 || d > 2 {
		return false
	}
	if mb == nil || !(mb.Def >= 20 || strongMob(mb, b.BaseLvOr())) {
		return false // soft trash: auto-attack, keep the spheres and SP
	}
	cost := max(si.SP, 10)
	if (b.SP-cost)*100/b.MaxSP <= skillSPFloor+b.keyReserve(now)*100/b.MaxSP {
		return false
	}
	b.skillReady = now.Add(time.Duration(1100+b.P.ReactionMs/2) * time.Millisecond) // 0.5 s fixed cast + 0.5 s delay
	b.targetAt, b.attackSentFor = now, 0
	go b.useSkill(moInvestigate, si.Lv, e.ID)
	return true
}
