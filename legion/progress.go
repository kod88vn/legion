package main

// Character progression: spend stat and skill points as they come in, know which class family a job belongs
// to (so a Lord Knight still fights like a swordsman), and pick the best attack/support skills the character
// has actually learned. Job changes themselves are done server-side by rathena/npc-custom/legion.txt.

import (
	"strings"
	"time"
)

type SkillInfo struct {
	ID, Lv, SP, Range int
	Upgradable        bool
}

const (
	famNovice = iota
	famSword
	famMage
	famArcher
	famAcolyte
	famMerchant
	famThief
)

var familyOf = map[int]int{}

func init() {
	add := func(f int, ids ...int) {
		for _, i := range ids {
			familyOf[i] = f
		}
	}
	add(famNovice, 0, 4001, 23, 4190)
	add(famSword, 1, 7, 14, 4002, 4008, 4015, 4054, 4060, 4066, 4073, 4252, 4258)
	add(famMage, 2, 9, 16, 4003, 4010, 4017, 4055, 4061, 4067, 4074, 4255, 4261)
	add(famArcher, 3, 11, 19, 20, 4004, 4012, 4020, 4021, 4056, 4062, 4068, 4069, 4075, 4076, 4257, 4262, 4263)
	add(famAcolyte, 4, 8, 15, 4005, 4009, 4016, 4057, 4063, 4070, 4077, 4256, 4264)
	add(famMerchant, 5, 10, 18, 4006, 4011, 4019, 4058, 4064, 4071, 4078, 4253, 4259)
	add(famThief, 6, 12, 17, 4007, 4013, 4018, 4059, 4065, 4072, 4079, 4254, 4260)
}

// baseLevelCap: the base level a 2nd-job character levels to. The server has no rebirth (ST01 / G10, 2026-11-15), so
// nothing above 99 exists for it.
const baseLevelCap = 99

// levelGoalTarget: the base level a level goal aims at. A 2nd-job character never aims at 100 (a 99 bot keeps its job
// and gear goals instead; pickFarmGoal still takes the gear and card goals first). Already-reborn classes (High Novice
// 4001 .. transcendent 2nd 4021, kept for characters reborn before ST01) keep lv+step.
func levelGoalTarget(class, lv, step int) int {
	if class >= 4001 && class <= 4022 {
		return lv + step
	}
	return min(lv+step, baseLevelCap)
}

// capZenyStep: the zeny a capped 2nd-job bot saves toward its next piece of gear before it picks again.
const capZenyStep = 50000

// levelCapped: a 2nd-job character at the base cap has no level left to aim at (ST01 / G10). Already-reborn classes
// are not capped here (levelGoalTarget).
func levelCapped(class, lv int) bool {
	return lv >= baseLevelCap && !(class >= 4001 && class <= 4022)
}

// capGoal (b.mu held): the goal of a capped 2nd-job bot in place of a level goal: zeny for its gear and supplies, on
// the money farm map when there is one. A level goal at 99 was met the moment it was set (lfg.go goalStatus), which
// made the goal loop re-pick every tick; this goal is met only after capZenyStep more zeny.
func (b *Bot) capGoal(now time.Time) *farmGoal {
	g := &farmGoal{Kind: "zeny", Target: b.Zeny + capZenyStep, Set: now, Until: now.Add(2 * time.Hour)}
	if mg := b.MoneyFarmGoal(now); mg != nil && len(mg.Maps) > 0 {
		g.Sell, g.SellID, g.Mob, g.Map, g.Maps = mg.Item, mg.ItemID, mg.Mob, mg.Maps[0], mg.Maps
	}
	return g
}

func family(class int) int {
	if f, ok := familyOf[class]; ok {
		return f
	}
	return famNovice
}

// offensive skills by preference (first known one with enough SP is used)
var offense = map[int][]SkillUse{
	famSword: {
		{ID: 2006, Lv: 5, SP: 35, Range: 1, CastMs: 1200}, // RK Ignition Break (area)
		{ID: 62, Lv: 10, SP: 22, Range: 1, CastMs: 1000},  // KN Bowling Bash
		{ID: 253, Lv: 10, SP: 30, Range: 1, CastMs: 1000}, // CR Holy Cross
		{ID: 56, Lv: 10, SP: 7, Range: 2, CastMs: 700},    // KN Pierce (spear; 1-3 hits by target size - run 5 lancer variant)
		{ID: 5, Lv: 10, SP: 15, Range: 1, CastMs: 700},    // SM Bash
	},
	famMage: {
		{ID: 84, Lv: 10, SP: 47, Range: 9, CastMs: 3000},                      // WZ Jupitel Thunder
		{ID: mgFire, Lv: 10, SP: 32, Range: 9, CastMs: 4000, Elemental: true}, // bolts (element-matched)
	},
	famArcher: {
		{ID: 382, Lv: 5, SP: 30, Range: 9, CastMs: 1500}, // SN Sharp Shooting
		{ID: 46, Lv: 10, SP: 12, Range: 9, CastMs: 900},  // AC Double Strafe
	},
	famMerchant: {
		{ID: 230, Lv: 5, SP: 15, Range: 9, CastMs: 1200}, // AM Acid Terror
		{ID: 42, Lv: 10, SP: 5, Range: 1, CastMs: 900},   // MC Mammonite
	},
	famThief: {
		{ID: 379, Lv: 10, SP: 20, Range: 4, CastMs: 1300}, // ASC Soul Destroyer (transfight.go: Assassin Cross)
		{ID: 136, Lv: 10, SP: 24, Range: 1, CastMs: 2000}, // AS Sonic Blow (katar)
		{ID: 46, Lv: 10, SP: 12, Range: 9, CastMs: 900},   // AC Double Strafe: a bow Rogue (rogue_bow; bow only, skillWeapon)
		{ID: 212, Lv: 10, SP: 16, Range: 1, CastMs: 800},  // RG Back Stab
		{ID: 52, Lv: 10, SP: 12, Range: 1, CastMs: 800},   // TF Envenom (any weapon; poison) - every Thief's attack skill
	},
}

// skillWeapon: attack skills the server refuses without this weapon type in the right hand (item_db SubType)
var skillWeapon = map[int]string{136: "Katar", 46: "Bow", 382: "Bow", 56: "Spear"}

// weaponFits: the weapon in hand (item_db SubType) is what skillWeapon asks for ("Spear" = one- or two-handed).
func weaponFits(want, sub string) bool {
	if want == "Spear" {
		return sub == "1hSpear" || sub == "2hSpear"
	}
	return sub == want
}

// skillItem: attack skills that consume an item per cast (rAthena re/skill_db.yml ItemCost): Acid Terror an Acid
// Bottle, Demonstration a Bottle Grenade. Without one the server refuses every cast (build review 2026-11-14).
var skillItem = map[int]int{230: 7136, 229: 7135}

// weaponSub: SubType of the weapon in the right hand ("" barehanded / unknown)
func (b *Bot) weaponSub() string {
	if w := b.equipped[0x2]; w != nil {
		if it := items[w.ItemID]; it != nil {
			return it.Sub
		}
	}
	return ""
}

// bestAttack returns the best attack skill the character has learned (level from the server's skill list).
func (b *Bot) bestAttack() *SkillUse {
	now := time.Now()
	for _, s := range offense[family(b.Class)] {
		if si := b.skills[s.ID]; si != nil && si.Lv > 0 {
			// QA 2026-11-14: Thieves (Envenom not listed), Assassins without a katar (Sonic Blow refused every cast)
			// and poor Merchants never spent SP. Skip what the weapon can't do, what the server just refused, and
			// a Mammonite we can't pay for; the next skill in the list (or auto-attack) takes over.
			if w := skillWeapon[s.ID]; w != "" && !weaponFits(w, b.weaponSub()) {
				continue
			}
			if now.Before(b.skillFailUntil[s.ID]) {
				continue
			}
			if s.Range > 3 && b.meleeCaster() {
				continue // run 5 Hindsight Sage: it hits in melee and Hindsight fires the bolts
			}
			if id := skillItem[s.ID]; id != 0 && b.countItem(id) == 0 {
				continue
			}
			u := s
			u.Lv = si.Lv
			if si.SP > 0 {
				u.SP = si.SP
			}
			if s.ID == mcMammo {
				// Mammonite costs 100 zeny per level: a cheap level for a poor merchant (lv1-3 = 100-300z), more with a
				// fuller purse; nothing under 800 zeny (the money reserve). QA 03:57: most merchants have a few hundred.
				// Economy batch 13: the guild alchemist saving for the 100k Potion Creation Guide never pays for it
				// (lilyrogue's fund fell 11,166 -> 4,766 in 6 min with nothing in its diary: Mammonite lv7 = 700z a
				// hit), and others keep 2,000z for gear: Mammonite level = one per 2,000z above that.
				if b.Zeny < 2500 || b.needsGuide() {
					continue
				}
				u.Lv = min(si.Lv, max(1, (b.Zeny-2000)/2000))
				u.SP = max(1, si.SP*u.Lv/si.Lv)
			}
			if u.Elemental {
				// any bolt learned works; boltFor picks the element per target
				if b.skills[mgFire] == nil && b.skills[mgCold] == nil && b.skills[mgLight] == nil {
					continue
				}
			}
			return &u
		}
	}
	return nil
}

// skill points: offensive skills first, then core support/passives, then anything else the server allows.
var skillPriority = map[int][]int{
	famNovice:   {1},
	famSword:    {5, 2, 3, 7, 62, 55, 56, 253, 2006, 2001, 2003},
	famMage:     {19, 14, 20, 9, 13, 84, 90, 85, 89, 2201},
	famArcher:   {46, 43, 44, 382, 2233, 129},
	famAcolyte:  {28, 34, 29, 22, 23, 25, 54, 66, 67, 2041},
	famMerchant: {42, 36, 37, 39, 41, 230, 2234},
	famThief:    {48, 49, 50, 136, 137, 212, 2022},
}

const (
	alBlessing = 34
	alIncAgi   = 29
	mcPushcart = 39
	mcVending  = 41
)

// progress spends stat/skill points; called from think() (with b.mu held).
func (b *Bot) progress(now time.Time) {
	if now.Before(b.progressAt) {
		return
	}
	b.progressAt = now.Add(1500 * time.Millisecond)
	if b.resetHoldPoints() {
		return // reset.go: points wait for the new plan; gear stays off while it is stored
	}
	if b.StatPts > 0 {
		st := b.planStat()
		if st < 0 {
			st = b.nextStat()
		}
		if st >= 0 {
			go b.send(newPkt(0x00bb).u16(uint16(13 + st)).u8(1).b)
		}
	}
	if b.SkillPts > 0 && now.After(b.skillUpAt) {
		pick := b.planSkill()
		for _, id := range skillPriority[family(b.Class)] {
			if pick >= 0 {
				break
			}
			if s := b.skills[id]; s != nil && s.Upgradable {
				pick = id
				break
			}
		}
		if pick < 0 {
			for id, s := range b.skills {
				if s.Upgradable && (pick < 0 || id < pick) {
					pick = id
				}
			}
		}
		if pick >= 0 {
			// one request at a time, and the skill stays upgradable: it used to be marked non-upgradable until the
			// server confirmed, so a request the server ignored (skill-point count not yet updated) left the plan's
			// skill blocked for the rest of the session and the next points went to later plan entries or the
			// fallback list (Mammonite before Vending, Sword Mastery before Bash; audit 2026-11-14)
			b.skillUpAt = now.Add(3 * time.Second)
			go b.send(newPkt(0x0112).u16(uint16(pick)).b)
		}
	}
	b.ammoThink(now) // ammo.go: arrows into the ammo slot; no arrows -> melee weapon until bought
	// wear better gear we carry (bought or looted), one piece at a time
	// (a bot with an empty weapon hand equips even mid-fight: busy grinders rarely have target 0)
	if b.equipWanted == 0 && now.After(b.equipAt) && (b.target == 0 || b.equipped[0x2] == nil) {
		b.equipAt = now.Add(4 * time.Second)
		b.equipFromInventory()
	} else if b.equipWanted != 0 && now.After(b.equipAt.Add(10*time.Second)) {
		// no answer (the server ignores equip requests during an NPC dialog): retry once later, then skip that
		// item. It used to blacklist the item ID for the session at the first silence.
		if it := b.inv[b.equipIdx]; it != nil && it.ItemID == b.equipWanted {
			it.equipTries++
			if it.equipTries >= 2 {
				it.NoEquip = true
			}
		}
		b.equipWanted = 0
		b.equipAt = now.Add(20 * time.Second)
	}
	b.identifyThink(now) // identify.go: a Magnifier on an unidentified upgrade
	// keep the plan's progress in memory for the minds (every few minutes)
	if now.After(b.planNoteAt) {
		b.planNoteAt = now.Add(3 * time.Minute)
		if s := b.planStatus(); s != "" {
			memoryOf(b.P).setProgress(b.charName(), s)
		}
	}
}

// nextStat picks the stat to raise from the persona's build ("str-agi" = mostly STR, then AGI), keeping a little
// VIT/DEX for survivability like real players do.
func (b *Bot) nextStat() int {
	idx := map[string]int{"str": 0, "agi": 1, "vit": 2, "int": 3, "dex": 4, "luk": 5, "crit": 5}
	parts := strings.Split(b.build(), "-")
	main, sec := idx[parts[0]], 2
	if len(parts) > 1 {
		sec = idx[parts[1]]
	}
	capV := 99
	if b.Class >= 4054 {
		capV = 120
	}
	st := b.stats
	switch {
	case st[main] < capV && st[main] < st[sec]*2+20:
		return main
	case st[sec] < capV:
		return sec
	case st[2] < 60:
		return 2
	case st[4] < 70:
		return 4
	case st[main] < capV:
		return main
	}
	return -1
}

// meleeCaster (b.mu held): a mage-line build that fights in melee (run 5 Hindsight Sage): no bolts from range.
func (b *Bot) meleeCaster() bool {
	p := b.plan()
	return p != nil && p.ID == "sage_hindsight" && family(b.Class) == famMage && b.skills[saAutoSpell] != nil && b.skills[saAutoSpell].Lv > 0
}
