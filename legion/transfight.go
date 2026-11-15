package main

// transfight.go: fight rules for the transcendent skills (transplans.go; build-and-strategy agent run 4, before the
// first rebirth - max bot level was 80 on 2026-11-14). rAthena re/skill_db.yml durations / costs:
//   - Lord Knight Aura Blade (355): +ATK per hit, any weapon, 20+20*lv s, 10+8*lv SP - a key buff (30% SP floor).
//   - Lord Knight Frenzy (Berserk, 359): 300 s, ASPD/MaxHP up but SP gone and no HP regen - only against a boss /
//     champion, at 90%+ HP, with a party healer near (never solo).
//   - High Wizard Mystical Amplification (366): the next spell's MATK +5*lv% - cast right before an area spell when
//     3+ monsters are grouped around the target and the SP covers both.
//   - Professor Double Casting (482): bolts may cast twice, 90 s, 35+5*lv SP - kept up while fighting above 50% SP.
//   - Assassin Cross Soul Destroyer (379): progress.go offense (range 4, first in the Thief list).
//   - High Priest Assumptio (361): party.go, on the tank after Kyrie (20*lv s).

import "time"

const (
	lkAuraBlade = 355
	lkFrenzy    = 359
	hwMagicAmp  = 366
	pfDoubleCst = 482
	hpAssumptio = 361
)

// transBuffs (b.mu held, from fight before aoeCast): one trans self-buff if due; true = sent.
func (b *Bot) transBuffs(e *Entity, now time.Time) bool {
	if !isTransSecond(b.Class) || b.MaxSP <= 0 || now.Before(b.skillReady) || b.Sitting {
		return false
	}
	ok := func(id int) *SkillInfo {
		si := b.skills[id]
		if si == nil || si.Lv <= 0 || now.Before(b.skillFailUntil[id]) || now.Before(b.buffAt["trans:"+itoa(id)]) {
			return nil
		}
		return si
	}
	spAfter := func(cost int) int { return (b.SP - cost) * 100 / b.MaxSP }
	ex, ey := e.pos(now)
	d := dist(b.X, b.Y, ex, ey)
	cast := func(id, lv int, dur time.Duration, ms int, state string) bool {
		b.buffAt["trans:"+itoa(id)] = now.Add(dur)
		b.skillReady = now.Add(time.Duration(ms+b.P.ReactionMs/2) * time.Millisecond)
		b.targetAt, b.attackSentFor = now, 0
		b.setState(state)
		go b.useSkill(id, lv, b.aid)
		return true
	}
	switch family(b.Class) {
	case famSword:
		if d > 3 || b.weaponSub() == "" {
			return false
		}
		if si := ok(lkAuraBlade); si != nil && spAfter(10+8*si.Lv) > skillSPFloor {
			return cast(lkAuraBlade, si.Lv, time.Duration(17+20*si.Lv)*time.Second, 800, "Aura Blade")
		}
		if si := ok(lkFrenzy); si != nil && b.hpFrac() >= 0.9 {
			mb := mobs[e.Class]
			if mb != nil && (mb.Boss || mb.Champion) && b.healerNear() {
				return cast(lkFrenzy, si.Lv, 300*time.Second, 1000, "Frenzy on "+mobName(e.Class))
			}
		}
	case famMage:
		if d > 10 {
			return false
		}
		if si := ok(pfDoubleCst); si != nil && spAfter(35+5*si.Lv) > 50 {
			return cast(pfDoubleCst, si.Lv, 87*time.Second, 2000, "Double Casting")
		}
		if si := ok(hwMagicAmp); si != nil && spAfter(30+5*si.Lv+60) > 15 {
			n := 0
			for _, o := range b.ents {
				if ox, oy := o.pos(now); o.Type == objMob && dist(ex, ey, ox, oy) <= 3 {
					n++
				}
			}
			if n >= 3 { // aoe.go casts the area spell next tick; the amplification is spent on it
				return cast(hwMagicAmp, si.Lv, 20*time.Second, 1400, "Mystical Amplification")
			}
		}
	}
	return false
}

// healerNear (b.mu held): a party healer on our map within 12 cells (Frenzy stops our HP regen).
func (b *Bot) healerNear() bool {
	p := partyOf(b)
	if p == nil {
		return false
	}
	for _, m := range p.Members {
		if m == b || partyRole(m) != roleSupport {
			continue
		}
		if s, ok := peek(m.P.Name); ok && s.Online && s.Map == b.MapName && dist(b.X, b.Y, s.X, s.Y) <= 12 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- Alchemist Aid Potion (Potion Pitcher)
//
// Run 4 open item: the potion Alchemist (and any Alchemist with Aid Potion) throws a potion at a party member under
// 45% HP within 9 cells: Aid Potion level N throws potion N (1 Red, 2 Orange, 3 Yellow, 4 White; rAthena consumes it
// from our bag), the best one we carry up to the learned level. Once per member per 3 s.
const amAidPotion = 231

var aidPotions = []int{0, 501, 502, 503, 504} // by Aid Potion level

func (b *Bot) aidPotion(now time.Time) bool {
	si := b.skills[amAidPotion]
	if si == nil || si.Lv <= 0 || now.Before(b.skillReady) || b.Sitting || now.Before(b.skillFailUntil[amAidPotion]) ||
		b.MaxSP <= 0 || b.SP < 10 {
		return false
	}
	lv := 0
	for l := min(si.Lv, 4); l >= 1; l-- {
		if b.countItem(aidPotions[l]) > 0 {
			lv = l
			break
		}
	}
	if lv == 0 {
		return false
	}
	var best uint32
	bestPct := 45
	for aid, hp := range b.partyHP {
		if aid == b.aid || hp[1] <= 0 || hp[0] <= 0 || now.Before(b.buffAt["aid:"+itoa(int(aid))]) {
			continue
		}
		e := b.ents[aid]
		if e == nil {
			continue
		}
		ex, ey := e.pos(now)
		if pct := hp[0] * 100 / hp[1]; pct < bestPct && dist(b.X, b.Y, ex, ey) <= 9 {
			best, bestPct = aid, pct
		}
	}
	if best == 0 {
		return false
	}
	b.buffAt["aid:"+itoa(int(best))] = now.Add(3 * time.Second)
	b.skillReady = now.Add(time.Duration(900+b.P.ReactionMs/2) * time.Millisecond)
	b.targetAt, b.attackSentFor = now, 0
	b.setState("Aid Potion on a hurt party member")
	go b.useSkill(amAidPotion, lv, best)
	return true
}
