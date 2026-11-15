package main

// gearpower.go: a bot's EFFECTIVE level - what it can really fight, from its base level AND its gear.
// QA 2026-11-14 (06:26-06:33Z, 145 deaths in 7 min): with 10x EXP the population levelled into lv20-46 while the gear
// stayed at the starter Knife + Cotton Shirt. The level-only map picker sent a Swordman lv40 with a Knife to the Wolf
// packs of moc_fild03, a Thief lv46 to the Orc Village, Merchants lv37 to mjolnir_02 - and they died with 5-23 Fly
// Wings and 15-54 potions in the bag. A lv25 with a Knife is not a lv25 with a weapon: every map choice (solo, bot
// party, guild-leader party, Kafra trips, the "easier map" fallbacks), the monster level filter (targetMaxLv) and
// losingFight's "outmatched" test now use powerLv().

import (
	"fmt"
	"time"
)

// expectedWeaponPower: the weapon attack a character of this family normally holds at level lv (NPC weapons of the
// level: melee Main Gauche/Blade ~45 at lv20, Pike/Dirk ~75 at lv40; bows Composite 29 / Great Bow 50; casters'
// MATK Wand 45 / Staff 70).
func expectedWeaponPower(f, lv int) float64 {
	switch f {
	case famArcher:
		return 10 + 1.0*float64(lv)
	case famMage:
		return 20 + 1.2*float64(lv)
	}
	return 15 + 1.5*float64(lv)
}

// weaponPowerRatio: how the weapon compares to the expected one for the level (0 = bare hands, 1 = at par or better).
func weaponPowerRatio(f, lv int, w *Item) float64 {
	if w == nil {
		return 0
	}
	r := float64(w.Atk) / expectedWeaponPower(f, lv)
	switch f {
	case famMage:
		r = float64(w.Matk) / expectedWeaponPower(famMage, lv)
	case famAcolyte: // maces hit, rods add MATK for Holy Light: whichever is better
		r = max(r, float64(w.Matk)/expectedWeaponPower(famMage, lv))
	}
	return min(1, max(0, r))
}

// armorPowerRatio: body armor DEF against the expected one (Adventurer's Suit 20 at lv17, Mantle/Coat ~35-42 at lv40).
func armorPowerRatio(lv, def int) float64 {
	return min(1, max(0, float64(def)/(10+0.6*float64(lv))))
}

// effectiveLv: the level a character with this weapon and body-armor DEF fights at. Starter gear drops one or more
// level bands: Knife + Cotton Shirt at lv40 fights like a lv24-25, at lv25 like a lv17. Beginners (< lv12) are not
// penalised (everyone starts with the Knife). Never below half the base level.
func effectiveLv(f, lv int, w *Item, armorDef int) int {
	if lv < 12 {
		return lv
	}
	k := 0.4 // melee lives by its weapon
	switch f {
	case famArcher:
		k = 0.35 // DEX does much of the work
	case famMage, famAcolyte:
		k = 0.25 // spells / support skills scale with INT more than with the weapon
	}
	pen := (1-weaponPowerRatio(f, lv, w))*float64(lv)*k + (1-armorPowerRatio(lv, armorDef))*float64(lv)*0.12
	return max(lv-int(pen+0.5), (lv+1)/2, 1)
}

// powerLv (b.mu held): this bot's effective level from its equipped weapon and body armor.
func (b *Bot) powerLv() int {
	lv := b.BaseLvOr()
	if !b.invReady() {
		return lv
	}
	if gearScoreOn() {
		return b.gearPowerLv(lv) // gearscore.go: the whole kit + measured performance (tuning gear_score_on)
	}
	var w *Item
	if e := b.equipped[0x2]; e != nil {
		w = items[e.ItemID]
	}
	def := 0
	if e := b.equipped[0x10]; e != nil {
		if x := items[e.ItemID]; x != nil {
			def = x.Def
		}
	}
	return effectiveLv(family(b.Class), lv, w, def)
}

// peekPower: another bot's effective level without its lock (the published snapshot), base level if unknown.
func peekPower(m *Bot) int {
	if s, ok := peek(m.P.Name); ok && s.Power > 0 {
		return s.Power
	}
	return peekLv(m)
}

// underGeared (b.mu held): the weapon is far below the level (ratio < 0.45 from lv20): a weapon step is a NECESSARY
// buy like the class weapon (builds.go classWeaponNeed). Returns the current weapon (nil = bare hands).
func (b *Bot) underGeared() (bool, *Item) {
	lv := b.BaseLvOr()
	var w *Item
	if e := b.equipped[0x2]; e != nil {
		w = items[e.ItemID]
	}
	if lv < 20 || w == nil {
		return false, w
	}
	return weaponPowerRatio(family(b.Class), lv, w) < 0.45, w
}

// tradeSafe (b.mu held): nothing hits us and HP is at least 70%: only then does a bot open (or keep open) a trade
// window - the window freezes walking and item use in rAthena (pc_cant_act: state.trading). QA 2026-11-14: "trade
// completed" was the last event before 38 of 181 deaths (party gifts handed over in the field while mobs hit).
func (b *Bot) tradeSafe(now time.Time) bool {
	return !b.Dead && b.MaxHP > 0 && b.hpFrac() >= 0.7 && !b.underAttack(now) && !b.mobTargetsMe()
}

// mobTargetsMe: a monster in sight is attacking / chasing us (hit us in the last 8 s).
func (b *Bot) mobTargetsMe() bool {
	now := time.Now()
	for _, e := range b.ents {
		if e.Type == objMob && now.Sub(e.LastHitMe) < 8*time.Second {
			return true
		}
	}
	return false
}

// tradeGuard (b.mu held, top of think): an open or pending trade is dropped the moment we are hit (or HP < 70% with
// the window open) - cancel 0x00ed for an open window, refuse 0x00e6(4) a pending request, close a market/gift deal.
// Survival then runs in the same tick.
func (b *Bot) tradeGuard(now time.Time) {
	hit := b.underAttack(now)
	low := b.MaxHP > 0 && b.hpFrac() < 0.7
	if s := b.mkt; s != nil && s.open && (hit || low) { // not open yet: marketThink fights / waits (tradeSafe gates the request)
		why := "hit by a monster"
		if !hit {
			why = "HP too low to trade"
		}
		if s.d.pause(now) { // economy.go: cancel the window only; trade again once safe (marketThink waits for tradeSafe)
			go b.send(newPkt(0x00ed).b)
			s.resetWindow(now)
			econCount("trade window paused: " + why)
			return
		}
		s.d.close(why)
		b.endMarket(now) // sends 0x00ed when the window is open
		return
	}
	if b.tradeFrom != "" && (hit || low) {
		if !b.tradeOpen.IsZero() {
			go b.send(newPkt(0x00ed).b)
		} else {
			go b.send(newPkt(0x00e6).u8(4).b)
		}
		b.event("info", "", "cancelled a trade with "+b.tradeFrom+" (monster / low HP)")
		b.tradeFrom, b.tradeOpen = "", time.Time{}
	}
}

// mapTooStrong: most of the map's monsters are above maxLv (a bot's targetMaxLv). Maps without monster spawns
// (interiors, towns) are never too strong.
func mapTooStrong(m string, maxLv int) bool {
	for _, s := range spawnsByMap[m] {
		if mb := mobs[s.MobID]; mb != nil && !mb.Boss && s.Count > 0 {
			return fitShare(m, maxLv) < minFitShare
		}
	}
	return false
}

// gearStepReady (b.mu held): may a NECESSARY weapon step (class weapon / weapon far below the level) start a trip or
// call a party town run now? QA 02:14 (741 online): the first build sent the whole Legion to town in the same minute
// (fighting 27%, travel/town 31%, kills/min 1,093 vs 1,750+). Now: a random 0-15 min delay after the session starts,
// and at most one such trip per 60 min. A bot with no weapon at all (weaponNeed) is not gated.
func (b *Bot) gearStepReady(now time.Time) bool {
	t0, ok := b.buffAt["gearstepok"]
	if !ok {
		t0 = now
		if b.rng != nil { // test bots carry no rng
			t0 = now.Add(time.Duration(b.rng.Intn(15*60)) * time.Second)
		}
		b.buffAt["gearstepok"] = t0
	}
	if now.Before(t0) {
		return false
	}
	if last, ok := b.buffAt["gearstep"]; ok && now.Sub(last) < 60*time.Minute {
		return false
	}
	return true
}

// noteGearStep (b.mu held): a necessary weapon step started (a trip, a party town run call, or a party-chat ask).
func (b *Bot) noteGearStep(now time.Time) { b.buffAt["gearstep"] = now }

// weaponWhy (b.mu held): the necessity named in the trip line (the sentinel tells necessary trips by it).
func (b *Bot) weaponWhy(it *Item) string {
	if w := b.equipped[0x2]; w != nil {
		if x := items[w.ItemID]; x != nil && !isClassWeapon(family(b.Class), x) {
			return "class weapon"
		}
		if x := items[w.ItemID]; x != nil {
			return fmt.Sprintf("weapon far below level: %s at lv%d", x.Name, b.BaseLvOr())
		}
	}
	return "class weapon"
}
