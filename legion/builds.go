package main

// Character builds: every Legion character works toward a concrete endgame build, the way a real player plans one:
//   - a final class (2nd job; later the transcendent and 3rd-job versions of it),
//   - stat milestones (spend points in this order: "AGI to 30, then STR to 20, then AGI to 50 ..."),
//   - a skill order per job stage (prerequisites first),
//   - an equipment route (buyable NPC gear per level range: weapon, armor, headgear, garment, shoes, shield),
//   - and the levelling route from strategy.go (Renewal guide spots by level/class).
// Everyone starts as a level 1 Novice; progress is earned by playing.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	stSTR = iota
	stAGI
	stVIT
	stINT
	stDEX
	stLUK
)

type StatGoal struct{ Stat, Value int }
type SkillGoal struct{ ID, Lv int }

type Build struct {
	ID     string
	Base   string // a build variant (owner 2026-11-14: "all varieties of builds"): the main plan it varies; "" = itself
	Name   string // "Crit Assassin"
	Class  int    // final 2nd job
	First  int    // 1st job (Job_Swordman=1 .. Job_Thief=6)
	Branch int    // which 2nd job (read by rathena/npc-custom/legion.txt)
	Stats  []StatGoal
	Skills []SkillGoal // in order; prerequisites first
	Legacy []SkillGoal // an earlier plan's goals: characters built under it stay valid (tools/wrongbuilds.py caps)
	// LegacyStats: an earlier stat plan; characters whose stats fit it are not wrong builds (tools/wrongbuilds.py)
	LegacyStats []StatGoal
	Weapons     []int  // weapon route, weakest first (NPC-buyable)
	Armors      []int  // body armor route
	Extras      []int  // headgear / garment / shoes / shield / accessory routes, weakest first per slot
	Summary     string // one line for memory / the LLM minds
}

func st(pairs ...int) []StatGoal {
	var out []StatGoal
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, StatGoal{pairs[i], pairs[i+1]})
	}
	return out
}

func sk(pairs ...int) []SkillGoal {
	var out []SkillGoal
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, SkillGoal{pairs[i], pairs[i+1]})
	}
	return out
}

// shared routes
var (
	armorLight  = []int{2301, 2305, 2321, 2307, 2309}       // cotton shirt, adventurer's suit, silk robe, mantle, coat
	armorHeavy  = []int{2301, 2305, 2328, 2312, 2314, 2316} // ... wooden mail, padded armor, chain mail, full plate
	armorThief  = []int{2301, 2305, 2307, 2309, 2312, 2314} // ... mantle, coat, padded, chain mail (Thief Clothes is Comodo-only)
	armorRobe   = []int{2301, 2305, 2321, 2332}             // ... silk robe, silver robe (mage line)
	armorHoly   = []int{2301, 2305, 2321, 2325}             // ... silk robe, Saint's Robe (prt_church; acolyte line)
	extrasMelee = []int{2220, 2226, 2228, 2501, 2503, 2505, 2401, 2403, 2405, 2627, 2607, 2608}
	extrasCast  = []int{2220, 2226, 2232, 2501, 2503, 2505, 2401, 2403, 2405, 2627, 2607, 2608}
	extrasHoly  = []int{2220, 2216, 2501, 2503, 2401, 2403, 2405, 2627, 2607, 2608} // Biretta, Rosary (prt_church)
	shields     = []int{2101, 2103, 2105}
)

// novice basics first in every plan
var noviceSkills = sk(1, 9)

// vendorFirst: a vendor character's first Merchant skills: Enlarge Weight Limit 5 -> Pushcart 3 -> Vending 3
var vendorFirst = sk(36, 5, 39, 3, 41, 3)

var builds = map[string]*Build{
	"knight": {ID: "knight", Name: "Two-Hand Sword Knight", Class: 7, First: 1, Branch: 0,
		Stats: st(stSTR, 20, stAGI, 20, stVIT, 20, stDEX, 15, stSTR, 40, stAGI, 40, stVIT, 35, stDEX, 25, stSTR, 60, stAGI, 60, stVIT, 50, stSTR, 80, stAGI, 75, stDEX, 40, stVIT, 60, stSTR, 90, stAGI, 85),
		// owner 2026-11-14 ("knights must follow the official path to get peco as soon as possible"): Riding 1 is the
		// FIRST Knight skill (prereq Endure 1, in the Swordman part); peco.go rents the bird at once.
		// build review 2026-11-14 (iRO Wiki Knight, 2H sword): Two-Hand Quicken -> Riding + Cavalier Mastery 5 (peco.go;
		// run 4: right after the core buff - with Riding at job ~26 only 24 of 105 Knights had it, median job 19; Cavalier
		// Mastery 5 straight after so the ride costs no ASPD) -> Auto Counter -> Bowling Bash -> Magnum Break 10, HP
		// Recovery 10, Endure 10 (Renewal: +MDEF). 2nd-job order isn't wrong-build checked; the old order is Legacy.
		Skills:  sk(2, 1, 5, 10, 3, 10, 7, 5, 6, 5, 8, 1, 4, 7, 63, 1, 60, 10, 64, 5, 61, 5, 62, 10, 7, 10, 4, 10, 8, 10),
		Legacy:  sk(2, 1, 5, 10, 3, 10, 7, 5, 6, 5, 8, 1, 4, 7, 60, 10, 61, 5, 62, 10, 63, 1, 64, 5, 7, 10, 4, 10, 8, 10),
		Weapons: []int{1201, 1116, 1151, 1154, 1157}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "AGI/STR two-hand sword knight: Two-Hand Quicken + Bowling Bash, end goal Lord Knight"},
	"crusader": {ID: "crusader", Name: "VIT Holy Cross Crusader", Class: 14, First: 1, Branch: 1,
		Stats: st(stVIT, 20, stSTR, 20, stDEX, 15, stINT, 10, stVIT, 40, stSTR, 40, stDEX, 30, stINT, 25, stVIT, 60, stSTR, 60, stDEX, 45, stVIT, 80, stSTR, 75, stDEX, 55, stINT, 40),
		// build review 2026-11-14 (iRO Wiki Crusader): Faith -> Holy Cross -> Auto Guard, then Riding + Cavalier Mastery
		// 5 (peco.go rents the Grand Peco once riding costs no ASPD), Shield Boomerang, Reflect Shield, Grand Cross
		// run 4: Riding + Cavalier Mastery 5 right after Holy Cross 10 (only 4 of 81 Crusaders had Riding at job ~38)
		Skills:  sk(5, 10, 6, 5, 2, 10, 8, 1, 4, 10, 7, 3, 63, 1, 248, 7, 253, 10, 64, 5, 248, 10, 249, 10, 250, 3, 251, 5, 252, 5, 254, 4, 255, 1),
		Legacy:  sk(5, 10, 6, 5, 2, 10, 8, 1, 4, 10, 7, 3, 248, 7, 253, 10, 248, 10, 249, 10, 250, 3, 251, 5, 252, 5, 254, 4, 63, 1, 64, 1, 255, 1),
		Weapons: []int{1201, 1104, 1107, 1110, 1113, 1126, 1119, 1129}, Armors: armorHeavy, Extras: append(append([]int{}, extrasMelee...), shields...),
		Summary: "VIT/STR shield crusader: Holy Cross and Auto Guard, end goal Paladin"},
	"wizard": {ID: "wizard", Name: "INT/DEX Wizard", Class: 9, First: 2, Branch: 0,
		Stats: st(stINT, 20, stDEX, 15, stVIT, 10, stINT, 40, stDEX, 30, stVIT, 20, stINT, 60, stDEX, 50, stVIT, 35, stINT, 80, stDEX, 65, stVIT, 50, stINT, 99, stDEX, 80, stVIT, 60),
		// run 6: Storm Gust 10 right after Jupitel 3 (trace 13:15: lv56-62 Wizards had Earth Spike / Heaven's Drive and
		// no Storm Gust, the Renewal Wizard's farming spell); Earth Spike / Heaven's Drive follow.
		// AoE build (owner 2026-11-14: "mage/wiz build should focus on AOE spells"). Mage: Fire Bolt 5 / Lightning Bolt 5
		// (prerequisites and early single-target), Fire Ball 5, Thunder Storm 5, Sight + Fire Wall 5 (kiting), Cold Bolt
		// 5 + Frost Diver / Napalm Beat / Stone Curse (Wizard prerequisites) = 34; Wizard: Jupitel 3, Earth Spike 3,
		// Heaven's Drive 5, Storm Gust 10, Jupitel 5, Lord of Vermilion 10, Sightrasher 2, Meteor Storm 10, Quagmire 3;
		// a Mage's remaining 15 points fill Thunder Storm / Fire Bolt / Lightning Bolt to 10 (the Wizard goals above are
		// not learnable by a Mage, so it skips them). Existing bolt Mages (Legacy) put every new point into the first
		// unmet goals here, i.e. area spells; they are not wrong builds (tools/wrongbuilds.py accepts Legacy caps).
		Skills:  sk(19, 5, 20, 5, 17, 5, 21, 5, 10, 1, 18, 5, 14, 5, 15, 1, 11, 1, 16, 1, 84, 3, 89, 10, 90, 3, 91, 5, 84, 5, 85, 10, 81, 2, 83, 10, 92, 3, 21, 10, 19, 10, 20, 10, 17, 10, 14, 10, 84, 10, 18, 10),
		Legacy:  sk(19, 10, 14, 10, 20, 10, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 84, 10, 89, 10, 90, 3, 91, 3, 21, 1, 85, 10, 18, 5, 81, 2, 83, 5),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/DEX AoE wizard: Fire Ball, Thunder Storm and Fire Wall as a Mage, then Storm Gust, Lord of Vermilion, Meteor Storm and Heaven's Drive on grouped monsters; end goal High Wizard"},
	"sage": {ID: "sage", Name: "INT/DEX Battle Sage", Class: 16, First: 2, Branch: 1,
		Stats: st(stINT, 20, stDEX, 15, stVIT, 10, stINT, 40, stDEX, 30, stVIT, 20, stINT, 60, stDEX, 50, stVIT, 35, stINT, 80, stDEX, 65, stVIT, 50, stINT, 99, stDEX, 80, stVIT, 60),
		// run 6 (owner rule "Mages are AoE builds"; QA 13:10: Sage-plan Mages lv40-50 had bolts 10/10/10 and no area spell
		// but Napalm until job ~40): the Mage part is now bolts 5 -> Fire Ball 5 -> Thunderstorm 5 -> bolts 10 -> Fire
		// Wall 5 (49 points); the old bolts-first order stays valid through Legacy, its new points go to the area spells.
		// build review run 3 (2026-11-14, iRO Wiki Sage, caster build): Heaven's Drive (AoE, aoe.go) and Earth Spike
		// (Wind monsters, skills.go boltFor) first, then Free Cast, Magic Rod / Spell Breaker (vs casters), Advanced
		// Book 10, the endows (need their elemental stones - no endow AI yet), Dispell. Auto Spell is a melee-Sage skill:
		// left out (existing Sages that took it are not wrong builds: 2nd-job skills aren't checked; Legacy below).
		Skills:  sk(19, 5, 14, 5, 20, 5, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 21, 5, 19, 10, 14, 10, 20, 10, 18, 5, 274, 5, 283, 1, 90, 5, 91, 5, 275, 1, 278, 4, 276, 1, 277, 5, 274, 10, 278, 10, 280, 2, 281, 2, 282, 2, 289, 5),
		Legacy:  sk(19, 10, 14, 10, 20, 10, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 274, 5, 283, 1, 90, 5, 91, 5, 275, 1, 278, 4, 279, 10, 276, 3, 280, 2, 281, 2, 278, 10, 274, 10),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/DEX caster sage: bolts, Earth Spike, Heaven's Drive on groups, Free Cast; end goal Professor"},
	"hunter": {ID: "hunter", Name: "DEX/AGI Bow Hunter", Class: 11, First: 3, Branch: 0,
		Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stAGI, 20, stVIT, 10, stLUK, 10, stDEX, 60, stAGI, 40, stLUK, 20, stVIT, 20, stDEX, 80, stAGI, 60, stLUK, 30, stDEX, 99, stAGI, 75, stVIT, 30, stLUK, 40),
		// build review 2026-11-14 (iRO Wiki Hunter: Blitz Beat / Arrow Shower builds): falcon first (legion.txt rents it),
		// then the passives and area damage the engine really uses - Beast Bane 10, Arrow Shower 10 (5x5 from lv6,
		// weaponskill.go), Owl's Eye 10 - before the traps (no trap AI yet; Trap items are sold in few towns)
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 126, 1, 127, 1, 129, 5, 128, 10, 126, 10, 47, 10, 43, 10, 115, 1, 117, 3, 116, 5, 120, 1, 121, 1, 119, 1, 122, 1, 118, 1, 123, 5),
		Legacy:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 126, 1, 127, 1, 129, 5, 128, 10, 115, 1, 117, 3, 116, 5, 120, 1, 121, 1, 119, 1, 122, 1, 118, 1, 123, 5, 126, 10, 43, 8),
		Weapons: []int{1701, 1704, 1707, 1710, 1714, 1718}, Armors: armorLight, Extras: extrasMelee,
		Summary: "DEX/AGI hunter: Double Strafe on one monster, Arrow Shower on groups, falcon Blitz Beat; end goal Sniper"},
	"bard": {ID: "bard", Name: "DEX/INT Bard", Class: 19, First: 3, Branch: 1,
		Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stAGI, 25, stVIT, 15, stDEX, 60, stAGI, 40, stVIT, 25, stINT, 20, stDEX, 80, stAGI, 55, stVIT, 40, stINT, 40, stDEX, 99),
		// support Bard (main 2026-11-14): after the Archer basics the party songs come first - Dissonance 3 opens them;
		// A Poem of Bragi, Assassin Cross of Sunset, A Whistle, Apple of Idun (song.go plays them with an instrument in
		// a party; solo the bow build stays). Music Lessons 10 / Musical Strike 5 come after; existing Bards already
		// have them (2nd-job skills are not wrong-build checked; Legacy keeps the old order).
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 304, 1, 305, 1, 315, 1, 317, 3, 321, 10, 320, 10, 319, 10, 322, 10, 315, 10, 316, 5),
		Legacy:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 315, 10, 316, 5, 304, 1, 305, 1, 317, 3, 321, 10, 319, 5),
		Weapons: []int{1701, 1704, 1707, 1710, 1714}, Armors: armorLight, Extras: extrasMelee,
		Summary: "DEX bard: Double Strafe solo; in a party a support singer (Bragi, Assassin Cross, Whistle, Apple of Idun); end goal Clown"},
	"dancer": {ID: "dancer", Name: "DEX/INT Dancer", Class: 20, First: 3, Branch: 1,
		Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stAGI, 25, stVIT, 15, stDEX, 60, stAGI, 40, stVIT, 25, stINT, 20, stDEX, 80, stAGI, 55, stVIT, 40, stINT, 40, stDEX, 99),
		// support Dancer (main 2026-11-14): Ugly Dance 3 opens the dances - Service for You, Humming, Please Don't Forget
		// Me, Fortune's Kiss (song.go, whip in hand in a party); Dance Lessons 10 / Throw Arrow 5 after.
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 304, 1, 305, 1, 323, 1, 325, 3, 330, 10, 327, 10, 328, 10, 329, 10, 323, 10, 324, 5),
		Legacy:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 43, 10, 323, 10, 324, 5, 304, 1, 305, 1, 325, 3, 330, 10, 327, 5),
		Weapons: []int{1701, 1704, 1707, 1710, 1714}, Armors: armorLight, Extras: extrasMelee,
		Summary: "DEX dancer: Double Strafe solo; in a party a support dancer (Service for You, Humming, Please Don't Forget Me, Fortune's Kiss); end goal Gypsy"},
	// acolyte-line plans learn Heal first (QA 2026-11-14: a monk-plan acolyte in a party had Divine Protection 6 and no
	// Heal, so the party's only support could not heal); the priest takes Heal to 5 before anything else
	"priest": {ID: "priest", Name: "Full Support Priest", Class: 8, First: 4, Branch: 0,
		// owner 2026-11-14 ("acolyte build seems wrong. It should max out intelligence"): INT first to 99, then DEX
		// (cast time) and VIT. Characters built under the old INT/VIT/DEX steps (LegacyStats) aren't wrong builds:
		// every new point goes to INT until 99.
		Stats:       st(stINT, 99, stDEX, 40, stVIT, 40, stDEX, 70, stVIT, 70, stDEX, 90, stVIT, 90),
		LegacyStats: st(stINT, 20, stVIT, 15, stDEX, 10, stINT, 40, stVIT, 30, stDEX, 25, stINT, 60, stVIT, 50, stDEX, 35, stINT, 80, stVIT, 70, stDEX, 45, stINT, 99, stVIT, 85),
		Skills:      sk(28, 5, 22, 5, 34, 10, 29, 10, 28, 10, 35, 1, 24, 1, 33, 2, 73, 10, 74, 3, 66, 3, 67, 3, 70, 7, 75, 1, 26, 2, 27, 4, 25, 1, 31, 1, 68, 4, 76, 3, 74, 5, 72, 1, 9, 4, 54, 1, 77, 3, 76, 5, 78, 1, 12, 1, 79, 10, 75, 5),
		Weapons:     []int{1601, 1604, 1607, 1610}, Armors: armorHoly, Extras: append(append([]int{}, extrasHoly...), shields...),
		Summary: "INT/VIT/DEX support priest: Heal, Blessing, Increase AGI, Kyrie, Magnificat; end goal High Priest"},
	// ---- build variants (owner 2026-11-14: "make sure we have all varieties of builds with matching gears"): common
	// Renewal alternatives to the main plans above. New characters get one with a realistic spread (planForClass);
	// existing characters keep their plan. Each has its own gear table in data/buildgear.json (falls back to Base).
	"knight_spear": {ID: "knight_spear", Base: "knight", Name: "Brandish Spear Knight", Class: 7, First: 1, Branch: 0,
		Stats:   st(stSTR, 20, stVIT, 20, stDEX, 15, stAGI, 10, stSTR, 40, stVIT, 40, stDEX, 30, stSTR, 60, stVIT, 55, stDEX, 45, stAGI, 20, stSTR, 80, stVIT, 70, stDEX, 55, stSTR, 90, stVIT, 80),
		Skills:  sk(5, 5, 6, 5, 8, 1, 7, 10, 4, 10, 5, 10, 6, 10, 63, 1, 55, 10, 56, 5, 58, 3, 57, 10, 64, 5, 59, 5, 58, 10, 56, 10, 8, 10),
		Weapons: []int{1201, 1401, 1404, 1451, 1454}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "STR/VIT spear knight on a Peco: Brandish Spear on groups, Pierce on large monsters; end goal Lord Knight"},
	"crusader_gc": {ID: "crusader_gc", Base: "crusader", Name: "INT/VIT Grand Cross Crusader", Class: 14, First: 1, Branch: 1,
		Stats:   st(stVIT, 20, stINT, 20, stDEX, 15, stSTR, 10, stVIT, 40, stINT, 40, stDEX, 30, stVIT, 60, stINT, 60, stDEX, 45, stVIT, 80, stINT, 80, stDEX, 60, stINT, 99),
		Skills:  sk(4, 10, 6, 5, 8, 1, 2, 10, 5, 10, 7, 10, 63, 1, 248, 10, 253, 6, 254, 10, 249, 5, 35, 1, 22, 5, 23, 5, 28, 5, 250, 3, 251, 3, 252, 5, 255, 1, 253, 10, 64, 5),
		Weapons: []int{1201, 1104, 1107, 1110, 1113, 1126, 1119, 1129}, Armors: armorHeavy, Extras: append(append([]int{}, extrasMelee...), shields...),
		Summary: "INT/VIT holy crusader: Grand Cross on undead/demon crowds, Heal; end goal Paladin"},
	"hunter_blitz": {ID: "hunter_blitz", Base: "hunter", Name: "DEX/INT Blitz Hunter", Class: 11, First: 3, Branch: 0,
		Stats:   st(stDEX, 20, stINT, 10, stDEX, 40, stINT, 25, stAGI, 10, stDEX, 60, stINT, 40, stLUK, 20, stDEX, 80, stINT, 55, stVIT, 20, stDEX, 99, stINT, 70, stVIT, 30),
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 126, 1, 127, 1, 129, 5, 128, 10, 126, 10, 47, 10, 43, 10, 115, 1, 117, 3, 116, 5),
		Weapons: []int{1701, 1704, 1707, 1710, 1714, 1718}, Armors: armorLight, Extras: extrasCast,
		Summary: "DEX/INT falcon hunter: Blitz Beat (INT/DEX damage) and Double Strafe; end goal Sniper"},
	"priest_me": {ID: "priest_me", Base: "priest", Name: "Magnus Exorcismus Priest", Class: 8, First: 4, Branch: 0,
		Stats:   st(stINT, 99, stDEX, 60, stVIT, 40, stDEX, 90, stVIT, 70),
		Skills:  sk(28, 5, 22, 5, 34, 10, 29, 10, 28, 10, 35, 1, 24, 1, 33, 2, 73, 5, 31, 1, 66, 3, 68, 4, 70, 3, 12, 1, 72, 1, 9, 4, 54, 1, 76, 5, 77, 3, 78, 1, 79, 10, 74, 5, 73, 10, 70, 7, 75, 5),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorHoly, Extras: append(append([]int{}, extrasHoly...), shields...),
		Summary: "INT/DEX exorcist priest: heals the party and clears undead/demons with Magnus Exorcismus; end goal High Priest"},
	"monk": {ID: "monk", Name: "AGI/STR Combo Monk", Class: 15, First: 4, Branch: 1,
		Stats: st(stSTR, 20, stAGI, 20, stVIT, 15, stDEX, 15, stSTR, 40, stAGI, 40, stVIT, 25, stDEX, 25, stSTR, 60, stAGI, 60, stVIT, 40, stDEX, 35, stINT, 15, stSTR, 80, stAGI, 80, stVIT, 50, stDEX, 45, stSTR, 90),
		// build review run 3: the combo first (Triple Attack 5 opens Chain Combo, Chain Combo 3 opens Combo Finish), then
		// Triple Attack 10 - 66 of 75 Monks had Triple Attack and no Chain Combo; 2nd-job order isn't wrong-build checked
		Skills:  sk(28, 3, 22, 10, 23, 10, 29, 10, 34, 6, 259, 5, 261, 5, 265, 5, 263, 5, 272, 5, 273, 5, 263, 10, 262, 1, 270, 3, 259, 10, 266, 3, 267, 3, 271, 5),
		Legacy:  sk(28, 3, 22, 10, 23, 10, 29, 10, 34, 6, 259, 5, 261, 5, 265, 5, 263, 10, 272, 5, 273, 5, 262, 1, 270, 3, 259, 10, 266, 3, 267, 3, 271, 5),
		Weapons: []int{1501, 1504, 1801, 1803, 1805, 1807}, Armors: armorHoly, Extras: extrasHoly,
		Summary: "AGI/STR combo monk: Triple Attack > Chain Combo > Combo Finish, end goal Champion"},
	"blacksmith": {ID: "blacksmith", Name: "STR/DEX Axe Blacksmith", Class: 10, First: 5, Branch: 0,
		Stats:   st(stSTR, 20, stDEX, 15, stVIT, 10, stAGI, 10, stSTR, 40, stDEX, 30, stVIT, 25, stAGI, 25, stSTR, 60, stDEX, 45, stVIT, 40, stAGI, 45, stSTR, 80, stDEX, 55, stAGI, 60, stVIT, 50, stLUK, 20, stSTR, 90),
		Skills:  sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 110, 2, 111, 5, 105, 1, 107, 2, 112, 3, 113, 5, 107, 10, 112, 5, 114, 5, 110, 5, 108, 1, 109, 5, 39, 10),
		Weapons: []int{1201, 1301, 1351, 1354, 1357, 1360}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "STR/DEX axe blacksmith: Adrenaline Rush + Power Thrust, vends on the side; end goal Whitesmith"},
	"alchemist": {ID: "alchemist", Name: "STR/INT Alchemist", Class: 18, First: 5, Branch: 1,
		Stats: st(stSTR, 20, stDEX, 15, stINT, 10, stVIT, 15, stSTR, 40, stDEX, 30, stVIT, 30, stINT, 25, stSTR, 60, stDEX, 45, stVIT, 45, stINT, 40, stSTR, 75, stDEX, 60, stVIT, 60),
		// Potion Research 5 -> Pharmacy 5 first as an Alchemist (alchemy.go: the guild alchemist brews potions), then Axe Mastery
		Skills:  sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 227, 5, 228, 5, 226, 10, 231, 5, 238, 1, 244, 1, 243, 1, 247, 1, 227, 10, 228, 10, 229, 5, 230, 5),
		Weapons: []int{1201, 1301, 1351, 1354, 1357}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "STR/INT alchemist: Acid Terror / Demonstration and potions, end goal Creator"},
	"assassin": {ID: "assassin", Name: "Crit Katar Assassin", Class: 12, First: 6, Branch: 0,
		// a katar right after the Main Gauche (build review 2026-11-14): Sonic Blow and Katar Mastery need one, and
		// all 31 online Assassins (lv51-76) held a dagger, saving for an 8,500z Dirk - Sonic Blow was never cast
		Stats:   st(stAGI, 20, stSTR, 20, stDEX, 10, stAGI, 40, stSTR, 35, stLUK, 15, stVIT, 15, stAGI, 60, stSTR, 50, stLUK, 30, stDEX, 25, stVIT, 30, stAGI, 80, stSTR, 70, stLUK, 40, stDEX, 35, stAGI, 90, stSTR, 85, stVIT, 40),
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 2, 52, 10, 53, 1, 51, 3, 134, 4, 136, 10, 134, 10, 135, 2, 137, 5, 138, 5, 139, 5, 135, 5, 140, 5),
		Weapons: []int{1201, 1207, 1250, 1254, 1252}, Armors: armorThief, Extras: extrasMelee,
		Summary: "AGI/LUK/STR crit katar assassin, dodges hits so can tank for a party: Double Attack, Katar Mastery, Sonic Blow; end goal Assassin Cross"},
	"rogue": {ID: "rogue", Name: "AGI/STR Dagger Rogue", Class: 17, First: 6, Branch: 1,
		Stats:   st(stAGI, 20, stSTR, 20, stDEX, 15, stAGI, 40, stSTR, 35, stVIT, 20, stDEX, 25, stAGI, 60, stSTR, 50, stVIT, 30, stDEX, 40, stAGI, 75, stSTR, 70, stVIT, 40, stDEX, 50, stAGI, 85, stSTR, 80),
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 1, 52, 10, 53, 1, 51, 3, 210, 4, 211, 4, 212, 4, 213, 2, 214, 5, 212, 10, 219, 5, 44, 10, 46, 5, 225, 4),
		Weapons: []int{1201, 1207, 1210, 1213, 1216, 1222}, Armors: armorThief, Extras: append(append([]int{}, extrasMelee...), shields...),
		Summary: "AGI/VIT evasion rogue (party dodge tank): Double Attack + Backstab and Raid, end goal Stalker"},
	// ---- build variants, run 4 (build-and-strategy agent 2026-11-14; iRO Wiki class pages / Renewal build guides).
	// The 1st-job part copies the main plan where it can (same early character), the 2nd-job part is the variant.
	"sage_support": {ID: "sage_support", Base: "sage", Name: "Support Sage", Class: 16, First: 2, Branch: 1,
		Stats: st(stINT, 20, stDEX, 15, stVIT, 15, stINT, 40, stDEX, 30, stVIT, 30, stINT, 60, stDEX, 45, stVIT, 45, stINT, 80, stDEX, 60, stVIT, 60, stINT, 99, stDEX, 70, stVIT, 70),
		// bolts as a Mage (same as the battle Sage), then Earth Spike / Heaven's Drive to fight, Free Cast, Spell Breaker
		// and Dispell (vs casters / buffed bosses), the endows for the party's melee, Volcano / Deluge / Whirlwind and
		// Magnetic Earth (party ground buffs; a Blue Gemstone each), Study 10. VIT for a tougher support.
		Skills:  sk(19, 5, 14, 5, 20, 5, 11, 1, 15, 1, 16, 1, 10, 1, 17, 5, 21, 5, 19, 10, 14, 10, 20, 10, 18, 5, 274, 5, 283, 1, 90, 3, 91, 3, 275, 1, 278, 4, 276, 1, 277, 3, 289, 5, 280, 2, 281, 2, 282, 2, 285, 3, 286, 3, 287, 3, 288, 1, 274, 10, 283, 3),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/VIT/DEX support sage: endows, Dispell, Volcano / Deluge / Whirlwind and Magnetic Earth for the party, bolts and Heaven's Drive to fight; end goal Professor"},
	"monk_asura": {ID: "monk_asura", Base: "monk", Name: "Asura Strike Monk", Class: 15, First: 4, Branch: 1,
		Stats: st(stSTR, 20, stINT, 15, stDEX, 15, stVIT, 15, stSTR, 40, stINT, 30, stDEX, 30, stVIT, 25, stSTR, 60, stINT, 45, stDEX, 45, stVIT, 40, stSTR, 80, stINT, 60, stDEX, 55, stVIT, 50, stSTR, 90, stINT, 70),
		// Acolyte part as the combo Monk (Iron Fists needs Divine Protection 10 + Demon Bane 10); Monk: the Asura chain
		// first (Iron Fists 5, Spirit Spheres 5, Absorb 1, Fury 3, Occult Impaction 3, Throw Spirit Sphere 3, Asura 5),
		// then Dodge 5 -> Root 2 -> Spiritual Cadence 5 (SP regen while sitting with spheres), Fury 5, Iron Fists 10,
		// Triple Attack 5. weaponskill.go asuraBurst: Fury, then Asura Strike at high SP on a tough monster.
		Skills:  sk(28, 3, 22, 10, 23, 10, 29, 10, 34, 6, 259, 5, 261, 5, 262, 1, 270, 3, 266, 3, 267, 3, 271, 5, 265, 5, 269, 2, 260, 5, 270, 5, 259, 10, 263, 5),
		Weapons: []int{1501, 1504, 1801, 1803, 1805, 1807}, Armors: armorHoly, Extras: extrasHoly,
		Summary: "STR/INT/DEX Asura monk: Fury + Asura Strike burst on tough monsters, sits with Spiritual Cadence to refill SP; end goal Champion"},
	"alchemist_battle": {ID: "alchemist_battle", Base: "alchemist", Name: "STR/DEX Battle Alchemist", Class: 18, First: 5, Branch: 1,
		Stats: st(stSTR, 20, stDEX, 15, stVIT, 15, stAGI, 10, stSTR, 40, stDEX, 30, stVIT, 30, stAGI, 20, stSTR, 60, stDEX, 45, stVIT, 45, stAGI, 30, stSTR, 80, stDEX, 55, stVIT, 55, stSTR, 90),
		// Merchant part as the main plan; Alchemist: Axe Mastery 10 first, then Pharmacy 5 for Acid Terror / Bomb
		// (progress.go: only with the bottle in the bag), homunculus, Aid Potion, Potion Research / Pharmacy 10 last
		Skills:  sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 226, 10, 227, 5, 228, 5, 230, 5, 229, 5, 238, 1, 244, 1, 243, 1, 247, 1, 231, 5, 227, 10, 228, 10),
		Weapons: []int{1201, 1301, 1351, 1354, 1357}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "STR/DEX battle alchemist: Axe Mastery, Acid Terror and Bomb, homunculus; end goal Creator"},
	"alchemist_potion": {ID: "alchemist_potion", Base: "alchemist", Name: "INT/DEX/LUK Potion Alchemist", Class: 18, First: 5, Branch: 1,
		Stats: st(stINT, 20, stDEX, 20, stSTR, 15, stVIT, 15, stINT, 40, stDEX, 40, stSTR, 30, stVIT, 25, stLUK, 15, stINT, 60, stDEX, 55, stLUK, 30, stVIT, 35, stINT, 80, stDEX, 70, stLUK, 45, stINT, 90, stDEX, 80, stLUK, 60),
		// the guild's brewer (Renewal brew rate: Potion Research, Pharmacy, job level, INT, DEX, LUK): Merchant with
		// Vending early (sells its potions) and Overcharge; Potion Research 10 + Pharmacy 10 first, Aid Potion, then the
		// homunculus, Bomb / Acid Terror, Axe Mastery last. alchemy.go prefers it as a guild's alchemist.
		Skills:  sk(36, 5, 39, 3, 41, 3, 42, 5, 37, 3, 38, 10, 36, 10, 42, 10, 227, 5, 228, 5, 227, 10, 228, 10, 231, 5, 238, 1, 244, 1, 243, 1, 247, 1, 229, 5, 230, 5, 226, 10),
		Weapons: []int{1201, 1301, 1351, 1354, 1357}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "INT/DEX/LUK potion alchemist: brews and sells the guild's potions (SP potions too), Aid Potion for the party; end goal Creator"},
	"blacksmith_mace": {ID: "blacksmith_mace", Base: "blacksmith", Name: "VIT/STR Mace Blacksmith", Class: 10, First: 5, Branch: 0,
		Stats: st(stSTR, 20, stVIT, 15, stDEX, 15, stSTR, 40, stVIT, 30, stDEX, 30, stSTR, 60, stVIT, 45, stDEX, 40, stAGI, 15, stSTR, 80, stVIT, 60, stDEX, 50, stSTR, 90, stVIT, 70),
		// a sturdier battle smith with a mace (Adrenaline Rush works with maces): Merchant part as the main plan; Hammer
		// Fall 5 first (stuns the crowd around the target), Adrenaline Rush, Power-Thrust, Maximize Power, Skin
		// Tempering early. No forging (no forge AI).
		Skills:  sk(36, 5, 39, 3, 42, 10, 41, 3, 37, 3, 38, 10, 36, 10, 110, 5, 111, 5, 105, 1, 107, 2, 112, 3, 113, 5, 114, 5, 109, 5, 107, 10, 112, 5, 108, 1, 39, 10),
		Weapons: []int{1201, 1501, 1504, 1507, 1510, 1519, 1513}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "VIT/STR mace blacksmith: Hammer Fall stuns, Adrenaline Rush + Power Thrust, tough enough to off-tank; end goal Whitesmith"},
	"assassin_sonic": {ID: "assassin_sonic", Base: "assassin", Name: "STR/AGI Sonic Blow Assassin", Class: 12, First: 6, Branch: 0,
		Stats: st(stAGI, 20, stSTR, 20, stDEX, 15, stAGI, 40, stSTR, 40, stDEX, 25, stVIT, 15, stAGI, 60, stSTR, 60, stDEX, 35, stVIT, 30, stAGI, 80, stSTR, 80, stDEX, 45, stAGI, 90, stSTR, 90, stVIT, 40),
		// non-crit katar: Sonic Blow is the damage (STR/DEX, no LUK). Thief part as the main plan; Katar Mastery 4 ->
		// Sonic Blow 10 -> Katar Mastery 10 -> Enchant Poison 5 (skillbuff.go) -> Cloaking 2 -> Grimtooth 5 ...
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 2, 52, 10, 53, 1, 51, 3, 134, 4, 136, 10, 134, 10, 138, 5, 135, 2, 137, 5, 132, 5, 138, 10, 139, 3),
		Weapons: []int{1201, 1207, 1250, 1254, 1252}, Armors: armorThief, Extras: extrasMelee,
		Summary: "STR/AGI/DEX katar assassin: Sonic Blow on everything, Enchant Poison, Grimtooth; end goal Assassin Cross"},
	"assassin_dagger": {ID: "assassin_dagger", Base: "assassin", Name: "AGI/STR Dagger Assassin", Class: 12, First: 6, Branch: 0,
		Stats: st(stAGI, 20, stSTR, 20, stDEX, 15, stAGI, 40, stSTR, 35, stDEX, 25, stVIT, 15, stAGI, 60, stSTR, 55, stDEX, 35, stLUK, 10, stVIT, 30, stAGI, 80, stSTR, 75, stDEX, 50, stLUK, 20, stAGI, 90, stSTR, 80),
		// dagger + Double Attack (procs only with a dagger) and poison: Righthand Mastery, Enchant Poison 10, Poison
		// React, Venom Dust, Lefthand Mastery (dual daggers later - the engine equips one weapon today), Venom Splasher.
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 2, 52, 10, 53, 1, 51, 3, 132, 5, 138, 10, 139, 5, 140, 5, 135, 3, 133, 5, 141, 10, 135, 5, 139, 9),
		Weapons: []int{1201, 1207, 1210, 1213, 1216, 1222}, Armors: armorThief, Extras: extrasMelee,
		Summary: "AGI/STR dagger assassin: Double Attack, Enchant Poison and Envenom with a dagger; end goal Assassin Cross"},
	"rogue_bow": {ID: "rogue_bow", Base: "rogue", Name: "DEX/AGI Bow Rogue", Class: 17, First: 6, Branch: 1,
		Stats: st(stDEX, 20, stAGI, 15, stDEX, 40, stAGI, 30, stVIT, 15, stDEX, 60, stAGI, 45, stLUK, 10, stVIT, 25, stDEX, 80, stAGI, 60, stVIT, 35, stDEX, 99, stAGI, 70, stLUK, 20),
		// Thief part as the main plan; Rogue: Vulture's Eye 10 -> Double Strafe 10 (progress.go, bow + arrows), then the
		// Back Stab line to Plagiarism 5 (copies a skill monsters cast on it: Gank 4, Mug 4, Back Stab 4, Stalk 2, Raid 5,
		// Snatch 5)
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 1, 52, 10, 53, 1, 51, 3, 44, 10, 46, 10, 210, 4, 211, 4, 212, 4, 213, 2, 214, 5, 219, 5, 225, 5),
		Weapons: []int{1201, 1701, 1704, 1707, 1710, 1713, 1714}, Armors: armorThief, Extras: extrasMelee,
		Summary: "DEX/AGI bow rogue: Double Strafe from range with Vulture's Eye, Plagiarism; end goal Stalker"},
	// ---- build variants, run 5 (owner 2026-11-14: "the builds of the legions still lack variety"; CLASSES.md "Build
	// variety across the Legion"). Every class line gets 2-3 sensible Renewal builds; tools/variety.py spreads them.
	"knight_pierce": {ID: "knight_pierce", Base: "knight", Name: "AGI/STR Pierce Lancer Knight", Class: 7, First: 1, Branch: 0,
		Stats: st(stSTR, 20, stAGI, 15, stVIT, 15, stDEX, 10, stSTR, 40, stAGI, 30, stVIT, 30, stDEX, 20, stSTR, 60, stAGI, 45, stVIT, 45, stDEX, 30, stSTR, 80, stAGI, 60, stVIT, 55, stDEX, 40, stSTR, 90, stAGI, 70, stVIT, 60),
		// a Peco lancer: Pierce (multi-hit on bigger monsters, progress.go offense) with a one-hand spear and a shield,
		// Spear Boomerang, Brandish Spear on crowds later; Riding + Cavalier Mastery early (peco.go)
		Skills:  sk(5, 10, 6, 5, 8, 1, 7, 5, 4, 10, 6, 10, 63, 1, 55, 10, 56, 5, 64, 5, 56, 10, 59, 5, 58, 3, 57, 10, 8, 10),
		Weapons: []int{1201, 1401, 1404, 1407, 1451, 1454}, Armors: armorHeavy, Extras: append(append([]int{}, extrasMelee...), shields...),
		Summary: "AGI/STR lancer knight on a Peco: Pierce and Spear Boomerang with a spear and shield, Brandish Spear on groups; end goal Lord Knight"},
	"crusader_spear": {ID: "crusader_spear", Base: "crusader", Name: "STR/VIT Spear Crusader", Class: 14, First: 1, Branch: 1,
		Stats: st(stSTR, 20, stVIT, 20, stDEX, 15, stSTR, 40, stVIT, 40, stDEX, 30, stAGI, 10, stSTR, 60, stVIT, 55, stDEX, 40, stAGI, 20, stSTR, 80, stVIT, 65, stDEX, 50, stAGI, 30, stSTR, 90, stVIT, 70),
		// Swordman part as the main plan; Crusader: Spear Mastery 10 -> Spear Quicken (two-handed spear ASPD buff,
		// skillbuff.go key buff) -> Riding / Cavalier -> Faith 7 -> Holy Cross 10 (stronger with a two-handed spear)
		Skills:  sk(5, 10, 6, 5, 2, 10, 8, 1, 4, 10, 7, 3, 63, 1, 55, 10, 258, 5, 64, 5, 258, 10, 248, 7, 253, 10, 248, 10, 254, 5),
		Weapons: []int{1201, 1401, 1404, 1407, 1451, 1454}, Armors: armorHeavy, Extras: extrasMelee,
		Summary: "STR/VIT spear crusader on a Grand Peco: Spear Quicken and Holy Cross with a two-handed spear; end goal Paladin"},
	"wizard_fire": {ID: "wizard_fire", Base: "wizard", Name: "INT/DEX Fire Wizard", Class: 9, First: 2, Branch: 0,
		Stats: st(stINT, 20, stDEX, 15, stVIT, 10, stINT, 40, stDEX, 30, stVIT, 20, stINT, 60, stDEX, 50, stVIT, 35, stINT, 80, stDEX, 65, stVIT, 45, stINT, 99, stDEX, 85, stVIT, 55),
		// fire AoE: Fire Bolt / Fire Ball / Fire Wall as a Mage (Thunderstorm 1 for Meteor Storm), then Sightrasher 2 ->
		// Meteor Storm 10, Heaven's Drive for fire-resistant crowds, Lord of Vermilion later (aoe.go picks the element)
		Skills:  sk(19, 10, 17, 5, 10, 1, 18, 5, 20, 4, 21, 1, 17, 10, 11, 1, 16, 1, 18, 10, 14, 1, 81, 2, 83, 5, 90, 3, 91, 5, 83, 10, 92, 3, 84, 5, 85, 10, 21, 10, 20, 10),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/DEX fire wizard: Fire Ball, Fire Wall and Meteor Storm on groups, Heaven's Drive vs fire monsters; end goal High Wizard"},
	"wizard_frost": {ID: "wizard_frost", Base: "wizard", Name: "INT/DEX Frost Wizard", Class: 9, First: 2, Branch: 0,
		Stats: st(stINT, 20, stDEX, 15, stVIT, 10, stINT, 40, stDEX, 30, stVIT, 20, stINT, 60, stDEX, 50, stVIT, 35, stINT, 80, stDEX, 65, stVIT, 50, stINT, 99, stDEX, 80, stVIT, 60),
		// water AoE: Cold Bolt / Frost Diver / Thunderstorm as a Mage, then Jupitel 3 -> Storm Gust 10 (freezes the crowd),
		// Lord of Vermilion, Heaven's Drive / Quagmire
		Skills:  sk(14, 10, 15, 5, 16, 1, 11, 1, 20, 4, 21, 5, 10, 1, 19, 1, 15, 10, 21, 10, 20, 5, 84, 3, 89, 5, 87, 1, 89, 10, 84, 5, 85, 10, 90, 3, 91, 5, 92, 5, 84, 10),
		Weapons: []int{1601, 1604, 1607, 1610}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/DEX frost wizard: Storm Gust freezes the crowd, Lord of Vermilion and Cold Bolt; end goal High Wizard"},
	"sage_hindsight": {ID: "sage_hindsight", Base: "sage", Name: "INT/AGI Hindsight Sage", Class: 16, First: 2, Branch: 1,
		Stats: st(stINT, 20, stDEX, 15, stAGI, 10, stINT, 40, stDEX, 30, stAGI, 20, stSTR, 10, stINT, 60, stDEX, 45, stAGI, 35, stSTR, 20, stINT, 75, stDEX, 60, stAGI, 50, stSTR, 30, stINT, 90, stDEX, 70, stAGI, 60),
		// melee with a book: Hindsight (Auto Spell) casts bolts on its own while it hits (skillbuff.go key buff; bot.go
		// answers the server's spell list 0x0afb); Study 10 = book ASPD; Earth Spike / Heaven's Drive join the list at
		// Hindsight 6 / 9
		Skills:  sk(19, 10, 14, 10, 20, 10, 16, 1, 10, 1, 11, 1, 17, 5, 21, 5, 11, 4, 13, 3, 274, 5, 275, 1, 278, 4, 279, 5, 283, 1, 90, 3, 91, 5, 279, 10, 274, 10, 278, 10, 276, 1, 277, 3, 289, 1),
		Weapons: []int{1201, 1550, 1551, 1552}, Armors: armorRobe, Extras: extrasCast,
		Summary: "INT/AGI melee sage: Hindsight fires bolts on every hit, book in hand; end goal Professor"},
	"hunter_agi": {ID: "hunter_agi", Base: "hunter", Name: "AGI/DEX Auto-Attack Hunter", Class: 11, First: 3, Branch: 0,
		Stats: st(stDEX, 15, stAGI, 15, stDEX, 30, stAGI, 35, stDEX, 45, stAGI, 55, stVIT, 15, stLUK, 10, stDEX, 60, stAGI, 75, stLUK, 20, stDEX, 75, stAGI, 90, stVIT, 30, stDEX, 85),
		// ASPD hunter: auto-attacks fast with a bow, Double Strafe on tough targets, falcon procs (Blitz Beat 5);
		// Owl's Eye / Vulture's Eye / Improve Concentration first
		Skills:  sk(46, 5, 43, 3, 44, 10, 45, 10, 43, 10, 47, 5, 46, 10, 126, 1, 127, 1, 129, 5, 128, 10, 126, 10, 47, 10, 115, 1, 117, 3),
		Weapons: []int{1701, 1704, 1707, 1710, 1714, 1718}, Armors: armorLight, Extras: extrasMelee,
		Summary: "AGI/DEX hunter: fast auto-attacks with a bow, Double Strafe on tough monsters, falcon procs; end goal Sniper"},
	"priest_battle": {ID: "priest_battle", Base: "priest", Name: "STR/INT Battle Priest", Class: 8, First: 4, Branch: 0,
		Stats: st(stSTR, 20, stINT, 15, stVIT, 15, stDEX, 10, stSTR, 40, stINT, 30, stVIT, 30, stDEX, 20, stSTR, 60, stINT, 45, stVIT, 45, stDEX, 30, stSTR, 80, stINT, 60, stVIT, 55, stDEX, 40, stSTR, 90, stINT, 70, stVIT, 60),
		// a mace priest: Heal 3 first, Demon Bane / Divine Protection, Blessing + Increase AGI on itself; Priest: Mace
		// Mastery 10, Impositio Manus 5 (self ATK buff, skillbuff.go), Kyrie, Magnificat, Lex Divina. Heals its party too.
		Skills:  sk(28, 3, 22, 10, 23, 10, 34, 6, 29, 10, 65, 10, 66, 5, 34, 10, 28, 7, 33, 2, 73, 5, 74, 3, 24, 1, 76, 5, 35, 1, 28, 10),
		Weapons: []int{1501, 1504, 1507, 1510, 1519, 1513}, Armors: armorHoly, Extras: append(append([]int{}, extrasHoly...), shields...),
		Summary: "STR/INT battle priest: mace, Blessing / Increase AGI / Impositio on itself, heals the party; end goal High Priest"},
	"bard_arrow": {ID: "bard_arrow", Base: "bard", Name: "DEX/AGI Arrow Bard", Class: 19, First: 3, Branch: 1,
		Stats: st(stDEX, 20, stAGI, 15, stDEX, 40, stAGI, 30, stVIT, 10, stDEX, 60, stAGI, 50, stVIT, 20, stLUK, 10, stDEX, 80, stAGI, 65, stVIT, 30, stDEX, 99, stAGI, 75),
		// a bow DPS bard: the full Archer kit (Arrow Shower 10, Owl's Eye 10), Music Lessons, Melody Strike, Impressive
		// Riff for later; it keeps the bow in a party too (song.go songSupport: no support singing)
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 10, 47, 10, 304, 1, 305, 1, 315, 10, 316, 5, 317, 3, 320, 10),
		Weapons: []int{1701, 1704, 1707, 1710, 1714}, Armors: armorLight, Extras: extrasMelee,
		Summary: "DEX/AGI bard: a Double Strafe / Arrow Shower archer with a bow; end goal Clown"},
	"dancer_arrow": {ID: "dancer_arrow", Base: "dancer", Name: "DEX/AGI Arrow Dancer", Class: 20, First: 3, Branch: 1,
		Stats:   st(stDEX, 20, stAGI, 15, stDEX, 40, stAGI, 30, stVIT, 10, stDEX, 60, stAGI, 50, stVIT, 20, stLUK, 10, stDEX, 80, stAGI, 65, stVIT, 30, stDEX, 99, stAGI, 75),
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 10, 47, 10, 304, 1, 305, 1, 323, 10, 324, 5, 325, 3, 327, 10),
		Weapons: []int{1701, 1704, 1707, 1710, 1714}, Armors: armorLight, Extras: extrasMelee,
		Summary: "DEX/AGI dancer: a Double Strafe / Arrow Shower archer with a bow; end goal Gypsy"},
	// ---- build variants, run 6 (owner 2026-11-14: "the builds of the legions still lack variety"): the builds run 5
	// left out for want of AI. variantskill.go plays them.
	"rogue_strip": {ID: "rogue_strip", Base: "rogue", Name: "DEX/STR Divest Rogue", Class: 17, First: 6, Branch: 1,
		Stats: st(stAGI, 20, stSTR, 20, stDEX, 15, stDEX, 35, stSTR, 35, stAGI, 30, stVIT, 20, stDEX, 55, stSTR, 50, stAGI, 40, stVIT, 30, stDEX, 75, stSTR, 65, stAGI, 50, stVIT, 40, stDEX, 90, stSTR, 80),
		// Thief part as the main plan; Rogue: Gank 4, Mug 4 -> Divest Helm 5 -> Shield 5 -> Armor 5 -> Weapon 5 (the
		// strip line; success grows with DEX), Back Stab 10, Stalk 2, Raid 5, Gangster's Paradise 1, Snatch 3.
		// variantskill.go stripTarget: Divest Weapon (-25% ATK) then Divest Shield (-15% DEF) on strong monsters.
		Skills:  sk(48, 10, 49, 10, 50, 5, 51, 1, 52, 10, 53, 1, 51, 3, 210, 4, 211, 4, 218, 5, 216, 5, 217, 5, 215, 5, 212, 4, 213, 2, 214, 5, 212, 10, 223, 1, 219, 3),
		Weapons: []int{1201, 1207, 1210, 1213, 1216, 1222}, Armors: armorThief, Extras: append(append([]int{}, extrasMelee...), shields...),
		Summary: "DEX/STR strip rogue: Divest Weapon / Shield on bosses and tough monsters so the party takes less and hits harder, Back Stab; end goal Stalker"},
	"hunter_trap": {ID: "hunter_trap", Base: "hunter", Name: "DEX/INT Trapper Hunter", Class: 11, First: 3, Branch: 0,
		Stats: st(stDEX, 20, stAGI, 10, stDEX, 40, stINT, 20, stAGI, 15, stDEX, 60, stINT, 40, stVIT, 15, stDEX, 80, stINT, 55, stVIT, 25, stAGI, 25, stDEX, 99, stINT, 70, stVIT, 35),
		// Archer part as the main plan; Hunter: the trap line first - Skid Trap 1, Land Mine 5, Ankle Snare 1, Flasher /
		// Freezing / Sandman 1 (prerequisites), Blast Mine 5, Shockwave 1, Claymore Trap 5 - then the falcon (Beast Bane 1,
		// Falconry, Blitz Beat 5, Steel Crow 10, Beast Bane 10), Ankle Snare 3. Renewal trap damage grows with DEX / INT /
		// base level; trap.go lays Claymore / Land Mine where monsters step and buys Traps (1065, 50z).
		Skills:  sk(46, 10, 43, 3, 44, 10, 45, 10, 47, 5, 43, 4, 115, 1, 116, 5, 117, 1, 120, 1, 121, 1, 119, 1, 122, 5, 118, 1, 123, 5, 126, 1, 127, 1, 129, 5, 128, 10, 126, 10, 117, 3),
		Weapons: []int{1701, 1704, 1707, 1710, 1714, 1718}, Armors: armorLight, Extras: extrasCast,
		Summary: "DEX/INT trapper hunter: Claymore Trap and Land Mine where monsters step, Double Strafe and the falcon in between; end goal Sniper"},
	"monk_occult": {ID: "monk_occult", Base: "monk", Name: "VIT/STR Occult Tank Monk", Class: 15, First: 4, Branch: 1,
		Stats: st(stSTR, 20, stVIT, 20, stDEX, 15, stAGI, 10, stSTR, 40, stVIT, 40, stDEX, 30, stINT, 15, stSTR, 60, stVIT, 55, stDEX, 40, stINT, 25, stSTR, 80, stVIT, 70, stDEX, 50, stINT, 35, stSTR, 90, stVIT, 80),
		// Acolyte part as the combo Monk; Monk: Iron Fists 5, Spirit Spheres 5, Occult Impaction 5 (damage grows with the
		// target's DEF), Dodge 5, the combo to Combo Finish 3 (Mental Strength's prerequisite), Mental Strength 5 (Steel
		// Body: DEF/MDEF 90 when mobbed or on a boss), Iron Fists 10, Absorb 1, Root 2, Spiritual Cadence 5.
		Skills:  sk(28, 3, 22, 10, 23, 10, 29, 10, 34, 6, 259, 5, 261, 5, 266, 5, 265, 5, 263, 5, 272, 3, 273, 3, 268, 5, 259, 10, 262, 1, 269, 2, 260, 5),
		Weapons: []int{1501, 1504, 1801, 1803, 1805, 1807}, Armors: armorHoly, Extras: extrasHoly,
		Summary: "VIT/STR tank monk: Occult Impaction on armoured monsters, Mental Strength (Steel Body) when mobbed or on a boss; end goal Champion"},
}

// planForClass picks the build for a final class (2nd/trans/3rd -> its 2nd job; 1st job -> a random branch).
func planForClass(class int, sex string, pick func(int) int) *Build {
	base := class
	if b, ok := transToSecond[class]; ok {
		base = b
	}
	var same []*Build
	for _, id := range buildOrder() {
		b := builds[id]
		if b.Class == base && !((b.base() == "bard" && sex == "F") || (b.base() == "dancer" && sex == "M")) {
			same = append(same, b)
		}
	}
	if len(same) > 0 {
		return pickVariant(same, pick)
	}
	first := base
	if first < 1 || first > 6 {
		first = 1 + pick(6)
	}
	var opts []*Build
	for _, id := range []string{"knight", "crusader", "wizard", "sage", "hunter", "bard", "dancer", "priest", "monk", "blacksmith", "alchemist", "assassin", "rogue"} {
		b := builds[id]
		if b.First == first && !((id == "bard" && sex == "F") || (id == "dancer" && sex == "M")) {
			opts = append(opts, b)
		}
	}
	main := opts[pick(len(opts))]
	var vs []*Build
	for _, id := range buildOrder() {
		if v := builds[id]; v.base() == main.ID {
			vs = append(vs, v)
		}
	}
	return pickVariant(vs, pick)
}

// base: the main plan id this build belongs to (itself for a main plan).
func (p *Build) base() string {
	if p == nil {
		return ""
	}
	if p.Base != "" {
		return p.Base
	}
	return p.ID
}

// pickVariant: run 5 (owner 2026-11-14: "the builds of the legions still lack variety"): the main plan 40% of the
// time and its variants share the rest; with one variant 50/50. No build above ~40% of a class with 2+ variants.
func pickVariant(same []*Build, pick func(int) int) *Build {
	var main *Build
	var vars []*Build
	for _, b := range same {
		if b.Base == "" {
			main = b
		} else {
			vars = append(vars, b)
		}
	}
	if main == nil {
		return same[pick(len(same))]
	}
	if len(vars) == 0 {
		return main
	}
	if len(vars) == 1 {
		if pick(2) == 0 {
			return main
		}
		return vars[0]
	}
	if pick(5) < 2 {
		return main
	}
	return vars[pick(len(vars))]
}

// trans / 3rd job -> the 2nd job its build is based on
var transToSecond = map[int]int{
	4008: 7, 4009: 8, 4010: 9, 4011: 10, 4012: 11, 4013: 12, 4015: 14, 4016: 15, 4017: 16, 4018: 17, 4019: 18, 4020: 19, 4021: 20,
	4054: 7, 4057: 8, 4055: 9, 4058: 10, 4056: 11, 4059: 12, 4066: 14, 4070: 15, 4067: 16, 4072: 17, 4071: 18, 4068: 19, 4069: 20,
	4060: 7, 4062: 8, 4061: 9, 4064: 10, 4063: 11, 4065: 12, 4073: 14, 4077: 15, 4074: 16, 4079: 17, 4078: 18, 4075: 19, 4076: 20,
}

func (b *Bot) plan() *Build {
	if p := builds[b.charSpec().Plan]; p != nil {
		return p
	}
	return nil
}

// ---------------------------------------------------------------- stats & skills

// planStat: the next stat to raise according to the plan's milestones (-1 = plan complete -> fallback).
func (b *Bot) planStat() int {
	p := b.plan()
	if p == nil {
		return -1
	}
	capV := 99
	if b.Class >= 4054 {
		capV = 120
	}
	for _, g := range p.Stats {
		if b.stats[g.Stat] < min(g.Value, capV) {
			return g.Stat
		}
	}
	return -1
}

// planSkill: the first skill in the plan's order that still needs points and that the server lets us raise now.
func (b *Bot) planSkill() int {
	p := b.plan()
	if p == nil {
		return -1
	}
	lists := [][]SkillGoal{noviceSkills, p.Skills}
	if b.charRole() == "vendor" {
		lists = [][]SkillGoal{noviceSkills, vendorFirst, p.Skills} // vend.go: a vendor learns Vending before Mammonite
	}
	if t := transFor(p); t != nil && isTransSecond(b.Class) {
		lists = append(lists[:len(lists)-1:len(lists)-1], t, p.Skills) // transplans.go: trans skills before the plan's tail
	}
	for _, list := range lists {
		for _, g := range list {
			if s := b.skills[g.ID]; s != nil && s.Upgradable && s.Lv < g.Lv {
				return g.ID
			}
		}
	}
	return -1
}

// ---------------------------------------------------------------- equipment

// jobKey: item_db "Jobs" key for a class.
func jobKey(class int) string {
	c := class
	if b, ok := transToSecond[c]; ok {
		c = b
	}
	if c >= 4023 && c <= 4029 { // baby / high 1st -> 1st
		c = c - 4022
	}
	if c >= 4002 && c <= 4007 { // high 1st jobs
		c = c - 4001
	}
	switch c {
	case 0, 4001:
		return "Novice"
	case 1:
		return "Swordman"
	case 2:
		return "Mage"
	case 3:
		return "Archer"
	case 4:
		return "Acolyte"
	case 5:
		return "Merchant"
	case 6:
		return "Thief"
	case 7:
		return "Knight"
	case 8:
		return "Priest"
	case 9:
		return "Wizard"
	case 10:
		return "Blacksmith"
	case 11:
		return "Hunter"
	case 12:
		return "Assassin"
	case 14:
		return "Crusader"
	case 15:
		return "Monk"
	case 16:
		return "Sage"
	case 17:
		return "Rogue"
	case 18:
		return "Alchemist"
	case 19, 20:
		return "BardDancer"
	}
	return "Novice"
}

// canEquip: job / level requirements from item_db.
func (b *Bot) canEquip(it *Item) bool {
	if it == nil || it.Loc == 0 {
		return false
	}
	if it.MinLv > b.BaseLvOr() {
		return false
	}
	if it.Jobs == nil {
		return true
	}
	k := jobKey(b.Class)
	if it.Jobs["!"+k] {
		return false
	}
	return it.Jobs["All"] || it.Jobs[k]
}

// gearSlot groups location bits into the slot an item competes for (two-handed weapons count as the weapon slot).
func gearSlot(loc uint32) uint32 {
	switch {
	case loc&0x2 != 0:
		return 0x2
	case loc&0x88 != 0:
		return 0x88
	}
	return loc
}

// gearStyle: what the character's build wants from a weapon. "magic": INT casters and the support priest (MATK
// makes bolts and, in Renewal, Heal stronger); "bow": the archer line; "melee": everyone else (ATK).
// Lesson (2026-11-13): itemScore was Atk*2 + Def*3 for everyone, so a Rod (ATK 15, MATK 30) lost to a Knife (ATK 17)
// and Zen.fox, a level 44 mage with 2,883z, kept a Knife although Payon sells Rods. Casters never upgraded.
func (b *Bot) gearStyle() string {
	if p := b.plan(); p != nil {
		if p.ID == "rogue_bow" {
			return "bow" // build variants run 4: a bow Rogue scores bows like an archer
		}
		if p.ID == "priest_battle" || p.ID == "sage_hindsight" {
			return "melee" // run 5: a mace priest / a book-swinging Hindsight Sage want ATK, not MATK
		}
		switch p.base() {
		case "wizard", "sage", "priest":
			return "magic"
		case "hunter", "bard", "dancer":
			return "bow"
		}
		return "melee"
	}
	switch family(b.Class) {
	case famMage, famAcolyte:
		return "magic"
	case famArcher:
		return "bow"
	}
	return "melee"
}

// itemScore: how good a piece of gear is for THIS character's build, for comparisons within a slot.
func (b *Bot) itemScore(it *Item) int {
	if it == nil {
		return -1
	}
	if it.Type == "Weapon" || gearSlot(it.Loc) == 0x2 {
		if songWeapon(it) && b.songSupport() {
			return 200 + it.Atk // song.go: a partied Bard / Dancer plays support with an instrument / whip in hand
		}
		switch b.gearStyle() {
		case "magic":
			return it.Matk*3 + it.Atk/2 + it.Buy/1000
		case "bow":
			if it.Sub == "Bow" {
				return 50 + it.Atk*2 + it.Buy/1000
			}
			return it.Atk / 2 // a dagger in an archer's hand is a stopgap
		}
		if it.Sub == "Staff" {
			return it.Atk // melee builds don't use the MATK
		}
		if p := b.plan(); p != nil && !planWeaponSub(p.ID, it.Sub) {
			return it.Atk / 3 // a weapon type the build's skills refuse (a Knight's Two-Hand Quicken: 2H swords only)
		}
		return it.Atk*2 + it.Buy/1000
	}
	v := it.Def*3 + it.Buy/1000
	if b.gearStyle() == "magic" {
		v += it.Matk * 2
	}
	// run 7 (owner: "are normal bots wearing their best gear?"; class QA 15:30: 2% of 830 bots wore an accessory, 1%
	// lower headgear): the piece's own script bonuses for this build (cards.go gearBonus: Glasses' DEX, a Ring's STR,
	// a Clip's SP...) - gear scored on DEF alone made every accessory worth ~0
	v += b.gearBonus(it.ID)
	return v
}

// realUpgrade: worth a deliberate trip or the party's pooled money (not a 1-point side-grade).
func (b *Bot) realUpgrade(it *Item, cur *Item) bool {
	if cur == nil {
		return b.itemScore(it) > 0
	}
	n, c := b.itemScore(it), b.itemScore(cur)
	return n >= c+c/5+5
}

// nextGear: the best affordable upgrade on the plan's routes that this character can use now and doesn't own.
func (b *Bot) nextGear() (*Item, *Shop) {
	budget := b.Zeny - b.gearReserve()
	// economy run 7 (owner: each bot works to perfect its build): when the build's next piece is a shop item the bot
	// can't afford yet but soon could (<= 3x its budget, or <= 5,000z), it saves toward it instead of buying lesser gear
	if pc := b.nextBuildPiece(); pc != nil && pc.Source == "shop" && pc.NPC > budget && pc.Gain > 0 &&
		pc.NPC <= max(3*budget, 5000) {
		return nil, nil
	}
	return b.nextGearBudget(budget)
}

// gearReserve: zeny kept back for potions when buying gear. Was 1500 + level*100 - at level 39 that is 5,400, so
// party members holding 4,500 zeny could never afford a 2,000z weapon and stayed on the starter Knife.
func (b *Bot) gearReserve() int {
	tn := tune()
	if b.Zeny > 3000 && b.guideSaving() { // alchemy.go: the guild's fund for the Potion Creation Guide is not gear money
		// run 8: all of it (was Zeny-1000: TheShadowsenpai at 103,062z gave 1,000z to a party gear turn, dropping
		// under the 103k guide line it had just reached)
		return b.Zeny
	}
	if b.isBrewer() && guildAlchemistChar(b.charName()) != "" { // economy run 9: the guild brewer keeps its supply money
		return min(b.Zeny, brewerFund)
	}
	if b.pecoNeed() { // peco.go: a rider's money goes to its Peco Peco first (owner: "Why is it not a priority?")
		// economy run 9: only the rental (+ its reserve) - Echobunny held 14.3k, Peco 2.5k, and showed "have 0, saving"
		_, _, price, _ := pecoBreeder(b.Class)
		return min(b.Zeny, price+pecoReserve+b.BaseLvOr()*tune().GearReservePerLv)
	}
	if b.Zeny > 1000 && poolOf(b) != nil { // guildpool.go: the guild's pooled money is for the boss item
		return b.Zeny - 1000
	}
	return min(tn.GearReserveBase+b.BaseLvOr()*tn.GearReservePerLv, b.Zeny/tn.GearReserveZenyDiv)
}

// lootValue: what the Etc loot in the bag sells for at an NPC (half the buy price).
func (b *Bot) lootValue() int {
	v := 0
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && b.npcSellable(it) { // junkgear.go: junk gear too
			v += x.Buy / 2 * it.Count
		}
	}
	return v
}

// nextGearBudget: the best upgrade on the plan's routes costing at most budget.
func (b *Bot) nextGearBudget(budget int) (*Item, *Shop) {
	p := b.plan()
	if p == nil || b.BaseLvOr() < minGearLevel() || !b.invReady() {
		return nil, nil // new players just fight; shopping trips come later
	}
	var best *Item
	var bestShop *Shop
	bestGain := 0
	teleAcc := b.creamyNeedsSlot()
	for _, route := range [][]int{p.Weapons, p.Armors, p.Extras} {
		for _, id := range route {
			it := items[id]
			if it == nil || !b.canEquip(it) || it.Buy <= 0 || it.Buy > budget || b.gearFailed[id] {
				continue
			}
			slot := gearSlot(it.Loc)
			if slot == 0x20 && (b.equipped[0x2] != nil && items[b.equipped[0x2].ItemID] != nil && items[b.equipped[0x2].ItemID].Loc == 0x22) {
				continue // shield with a two-handed weapon
			}
			cur := b.equipped[slot]
			curScore := 0
			if cur != nil {
				if cur.ItemID == id {
					continue
				}
				curScore = b.itemScore(items[cur.ItemID])
			}
			if b.ownsUnequipped(id) {
				continue
			}
			if t := b.trip; t != nil && t.Slots[slot] {
				continue // one item per slot per trip (QA 2026-11-14: Shoes, then Sandals on the same run)
			}
			curScore = max(curScore, b.bestOwnedScore(slot)) // a better item in the bag (bought, not worn yet) counts
			gain := b.itemScore(it) - curScore
			if teleAcc && it.Loc&accLocMask != 0 && it.Slots > 0 {
				gain = max(gain, 0) + teleAccGain // teleslot.go: the Creamy in the bag needs a slotted accessory first
			}
			if gain <= 0 {
				continue
			}
			if cur == nil && it.Loc&(0x88|0x200|0x1|0x4|0x40|0x100) != 0 && b.bestOwnedScore(slot) == 0 {
				gain += 10 // run 7: an empty slot (accessory, headgear, garment, shoes) is filled cheaply like players do
			}
			if slot == 0x2 && cur == nil {
				gain += 1000 // no usable weapon: a weapon comes first
			} else if slot == 0x2 && isClassWeapon(family(b.Class), it) && !isClassWeapon(family(b.Class), items[cur.ItemID]) {
				gain += 500 // the class weapon (Rod / Bow) before armor: a party run or a gear turn buys it first
			}
			s := b.gearShopNear(id)
			if s == nil && b.farGearOK(it.Buy, budget) {
				s = b.gearShopAny(id) // a real upgrade sold only far away: one planned Kafra trip (maxGearHops doesn't block it)
			}
			if s == nil {
				continue
			}
			if gain > bestGain {
				best, bestShop, bestGain = it, s, gain
			}
		}
	}
	return best, bestShop
}

// bestOwnedScore: the best itemScore of a wearable, unequipped item in the bag for this gear slot
func (b *Bot) bestOwnedScore(slot uint32) int {
	best := 0
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Count <= 0 || it.Equipped || it.Unidentified || it.NoEquip || x.Loc == 0 || gearSlot(x.Loc) != slot || !b.canEquip(x) {
			continue
		}
		best = max(best, b.itemScore(x))
	}
	return best
}

func (b *Bot) ownsUnequipped(id int) bool {
	for _, it := range b.inv {
		if it.ItemID == id && it.Count > 0 && !it.Equipped && !it.Unidentified && !it.NoEquip {
			return true // an unidentified drop or a refused item isn't one we can wear (identify.go)
		}
	}
	return false
}

// Gear shopping. Lesson (2026-11-13): gearShop used to return an arbitrary reachable shop (e.g. Yuno) and level-1
// Novices with starter zeny spent whole sessions walking across the world (trip timeout 12 min, retry 5 min later,
// forever) - 500+ of 900 online bots were "travel -> alberta_in / yuno_in01" and the average level stayed ~1.
// Now: no gear trips before base level 10, only the nearest shop within maxGearHops maps, and a timed-out gear
// trip marks the item as failed for the session. The caches are shared by all bot goroutines, hence the mutex.
// minGearLevel() / maxGearHops(): tune().MinGearLevel / MaxGearHops, live-tunable in data/tuning.json (tuning.go)

var (
	gearMu         sync.Mutex
	gearShopsCache = map[int][]*Shop{}
	hopCache       = map[string]int{}
)

// gearShops: every reachable NPC shop that sells the item.
func gearShops(id int) []*Shop {
	gearMu.Lock()
	if l, ok := gearShopsCache[id]; ok {
		gearMu.Unlock()
		return l
	}
	gearMu.Unlock()
	var found []*Shop
	for _, list := range shopsByMap {
		for _, s := range list {
			for _, x := range s.Items {
				if x == id && shopReachable(s.Map) {
					found = append(found, s)
					break
				}
			}
		}
	}
	gearMu.Lock()
	gearShopsCache[id] = found
	gearMu.Unlock()
	return found
}

type mapVisit struct {
	m   string
	at  time.Time
	via string // warpKey of the portal that brought us here ("" unknown) - ai.go warp-loop detector
}

// hopCacheReset forgets cached route lengths (after a warp was found to loop).
func hopCacheReset() {
	gearMu.Lock()
	hopCache = map[string]int{}
	gearMu.Unlock()
}

// mapHops: how many map changes the walk from a to b takes (99 = unreachable). Cached per map pair.
func mapHops(a string, ax, ay int, b string) int {
	if a == b {
		return 0
	}
	k := a + "|" + b
	gearMu.Lock()
	h, ok := hopCache[k]
	gearMu.Unlock()
	if ok {
		return h
	}
	h = 99
	if r := RouteMaps(a, ax, ay, b); r != nil {
		h = len(r)
	}
	gearMu.Lock()
	hopCache[k] = h
	gearMu.Unlock()
	return h
}

// gearShopNear: the closest shop selling the item, if it is at most maxGearHops maps away from the bot.
func (b *Bot) gearShopNear(id int) *Shop {
	var best *Shop
	bh := maxGearHops() + 1
	town := nearestTown(b.MapName)
	toTown := mapHops(b.MapName, b.X, b.Y, town)
	for _, s := range gearShops(id) {
		h := mapHops(b.MapName, b.X, b.Y, s.Map)
		// or via the Kafra in the nearest town (travel() takes the teleport by itself)
		if len(kafraDests[town]) > 0 {
			h = min(h, toTown+b.kafraTeleCost(town, s.Map))
		}
		if h < bh || h == bh && best != nil && betterTie(town, best, s) {
			// equally far: prefer a shop standing in the town itself (Prontera's south-gate market) over one inside a
			// building (prt_church's Nun, rooms in payon_in01): acolytes kept timing out on "buying Mace" trips
			best, bh = s, h
		}
	}
	return best
}

// farGearOK: may this bot plan ONE deliberate trip to a shop beyond maxGearHops for an item costing price? Only past
// the beginner levels (owner: beginners stay near their home town), with the Kafra fees there and back on top of the
// price, and at most once per gear_trip_cooldown_min (no back-and-forth travel).
func (b *Bot) farGearOK(price, budget int) bool {
	tn := tune()
	if b.BaseLvOr() < tn.GearTripMinLv || time.Since(b.farGearAt) < time.Duration(tn.GearTripCooldownMin)*time.Minute {
		return false
	}
	return price+2*200+tn.KafraKeep <= budget
}

// betterTie: between two equally far shops, s beats best if it is in the bot's nearest town and best isn't (a Payon
// Rod was routed to Prontera's market by Kafra on a tie: a wasted fee), else if it stands outdoors and best doesn't.
func betterTie(town string, best, s *Shop) bool {
	bt, st := townOf(best.Map) == town, townOf(s.Map) == town
	if bt != st {
		return st
	}
	return !isTown(best.Map) && isTown(s.Map)
}

// gearShopAny: the closest reachable shop selling the item, however far (walking or via the nearest town's Kafra).
func (b *Bot) gearShopAny(id int) *Shop {
	var best *Shop
	bh := 99
	town := nearestTown(b.MapName)
	toTown := mapHops(b.MapName, b.X, b.Y, town)
	for _, s := range gearShops(id) {
		h := mapHops(b.MapName, b.X, b.Y, s.Map)
		if len(kafraDests[town]) > 0 {
			h = min(h, toTown+b.kafraTeleCost(town, s.Map))
		}
		if h < bh || h == bh && best != nil && betterTie(town, best, s) {
			best, bh = s, h
		}
	}
	return best
}

// starterWeapons: cheap weapons sold in nearly every town (Rod, Club, Knife, Sword, Bow, Axe): a bot with no usable
// weapon buys the best of these it can use when its plan's route has nothing affordable nearby.
var starterWeapons = []int{1601, 1501, 1201, 1101, 1701, 1301}

// weaponNeed: the character has NO usable weapon (b.mu held). Returns the best cheap weapon it can buy at the nearest
// shop. A necessary town trip at any level (owner 2026-11-14: town trips only when necessary; no weapon is one): 25 of
// 30 sampled main-Legion acolytes (lv 5-19) fought bare-handed - gear trips start at min_gear_level 10 and in a party
// gear never counted as a need, so the weapon-first pick never ran.
func (b *Bot) weaponNeed() (*Item, *Shop) {
	if !b.invReady() || b.equipped[0x2] != nil || b.BaseLvOr() < 2 {
		return nil, nil
	}
	for _, it := range b.inv { // a usable weapon in the bag: equipFromInventory wears it, no trip
		// wearable (identify.go): identified and not refused. A looted Club is unidentified: it used to count as
		// usable here, the equip was refused, and the refusal then blacklisted Clubs from the shopping list
		if x := items[it.ItemID]; x != nil && !it.Equipped && gearSlot(x.Loc) == 0x2 && b.wearable(it, x) {
			return nil, nil
		}
		if x := items[it.ItemID]; x != nil && it.Unidentified && gearSlot(x.Loc) == 0x2 && b.canEquip(x) && b.invIndex(itemMagnifier) >= 0 {
			return nil, nil // identifyThink is about to make it wearable
		}
	}
	var ids []int
	if p := b.plan(); p != nil {
		ids = append(ids, p.Weapons...)
	}
	ids = append(ids, starterWeapons...)
	budget := b.Zeny - 100
	var best *Item
	var bestShop *Shop
	for _, id := range ids {
		it := items[id]
		if it == nil || it.Buy <= 0 || it.Buy > budget || !b.canEquip(it) || b.gearFailed[id] || gearSlot(it.Loc) != 0x2 {
			continue
		}
		if best != nil && b.itemScore(it) <= b.itemScore(best) {
			continue
		}
		if s := b.gearShopNear(id); s != nil {
			best, bestShop = it, s
		}
	}
	return best, bestShop
}

// classWeapons: the weapon types (item_db SubType) that define a class family. A character holding a weapon outside
// its family's list (the starter Knife) is missing its class weapon: an Archer can't Double Strafe without a bow, a
// Mage's bolts need a staff/rod's MATK, an Acolyte fights with a mace or a rod. Buying one is a NECESSARY purchase
// like "no usable weapon". Lesson (2026-11-14): all 123 online Mages and 97 Archers still held the Knife; the Knife
// counted as "a usable weapon", so weaponNeed never fired and gear only rode along on rare real-need trips.
var classWeapons = map[int][]string{
	famMage:    {"Staff", "2hStaff", "Book"},
	famArcher:  {"Bow", "Musical", "Whip"},
	famAcolyte: {"Mace", "2hMace", "Staff", "2hStaff", "Knuckle", "Book"},
	// QA 2026-11-14 (145 deaths in 7 min): Swordmen lv40-42 and Merchants lv37 fought with the starter Knife. A Sword /
	// Spear / Axe / Mace is their class weapon too (Sword 100z, Javelin 150z, Axe 500z, Club 120z).
	famSword:    {"1hSword", "2hSword", "1hSpear", "2hSpear"},
	famMerchant: {"1hAxe", "2hAxe", "Mace", "2hMace", "1hSword"},
	famThief:    {"Dagger", "1hSword", "Katar", "Bow"}, // the Knife is a dagger: its step is an upgrade (underGeared)
}

// weaponSteps: NPC-sold weapons a character steps up through when its gear falls far
// below its level (gearpower.go underGeared): daggers, swords, spears, axes, maces, bows, staves; canEquip filters jobs.
var weaponSteps = []int{1204, 1207, 1210, 1213, 1216, 1101, 1104, 1107, 1110, 1116, 1401, 1404, 1407, 1301, 1351, 1501, 1504,
	1701, 1704, 1707, 1710, 1601, 1604, 1607}

// isClassWeapon: the item is one of the family's class weapons (families without a list: any weapon will do).
func isClassWeapon(f int, x *Item) bool {
	subs, ok := classWeapons[f]
	if !ok || x == nil {
		return true // unknown item: no class-weapon preference
	}
	for _, s := range subs {
		if x.Sub == s {
			return true
		}
	}
	return false
}

// classWeaponNeed: the character holds a weapon but not its class weapon, and none is in the bag (b.mu held).
// Returns the CHEAPEST proper one it can afford (plan route first, then the starter list: Rod, Bow, Club) at the
// nearest shop, or the nearest anywhere when the Kafra fees fit the purse. Upgrades after that ride on real trips.
func (b *Bot) classWeaponNeed() (*Item, *Shop) {
	f := family(b.Class)
	if !b.invReady() || b.BaseLvOr() < 2 {
		return nil, nil
	}
	cur := b.equipped[0x2]
	if cur == nil {
		return nil, nil // bare hands: weaponNeed
	}
	_, hasList := classWeapons[f]
	if x := items[cur.ItemID]; x == nil || !hasList || isClassWeapon(f, x) {
		// a partied singer's instrument / whip is its class step (song.go); while it saves for it nothing else
		if b.songSaving() {
			return b.songWeaponNeed()
		}
		if b.coreSaving() {
			return b.coreWeaponNeed() // coreweapon.go: a katar for a Sonic Blow Assassin before any other gear
		}
		if b.pecoSaving() {
			return nil, nil // the zeny goes to the Peco Peco first (peco.go pecoSaving)
		}
		// the class weapon is in hand: a weapon / body-armor step when the gear is far below the level (gearpower.go)
		return b.gearStepNeed()
	}
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Equipped || gearSlot(x.Loc) != 0x2 || !isClassWeapon(f, x) {
			continue
		}
		if b.wearable(it, x) || it.Unidentified && b.canEquip(x) && b.invIndex(itemMagnifier) >= 0 {
			return nil, nil // equipFromInventory / identifyThink handles it, no trip
		}
	}
	return b.classWeaponPick(b.Zeny - b.gearReserve()) // the same purse rule as every gear buy (bestGearIn agrees)
}

// gearHold: a necessary item comes before ANY other gear (b.mu held): the class weapon is still missing, or a bow
// user has no arrows. Until then no weapon/armour shop sells it anything else - the money waits for the necessary
// item (QA 05:43: Ravenheart72, Archer on a Knife, bought Sandals and a Jacket on party runs with 838-865z while its
// 1000z Bow waited; dark_storm20 had a Bow in the bag, no arrows, and gear_next = Hood).
// Run 4: also while a singer saves for its instrument / whip (song.go songSaving) or a rider for its Peco (peco.go
// pecoSaving) - the build's necessary step is the savings goal.
func (b *Bot) gearHold() bool {
	return b.classWeaponMissing() || b.noArrows() || b.songSaving() || b.pecoSaving() || b.coreSaving() || b.teleSlotSaving()
}

// shopIn: a shop in town (or its buildings) selling the item, nil if none.
func shopIn(id int, town string) *Shop {
	for _, sh := range gearShops(id) {
		if sh.Map == town || townOf(sh.Map) == town {
			return sh
		}
	}
	return nil
}

// classWeaponMissing: holding a weapon outside the family's class list, with no class weapon in the bag (b.mu held).
func (b *Bot) classWeaponMissing() bool {
	f := family(b.Class)
	cur := b.equipped[0x2]
	if !b.invReady() || b.BaseLvOr() < 2 || cur == nil {
		return false
	}
	if _, ok := classWeapons[f]; !ok || isClassWeapon(f, items[cur.ItemID]) {
		return false
	}
	for _, it := range b.inv {
		x := items[it.ItemID]
		if x == nil || it.Equipped || gearSlot(x.Loc) != 0x2 || !isClassWeapon(f, x) {
			continue
		}
		if b.wearable(it, x) || it.Unidentified && b.canEquip(x) && b.invIndex(itemMagnifier) >= 0 {
			return false
		}
	}
	return true
}

// classWeaponLootNeed: the class weapon is missing and the purse can't pay, but the junk loot in the bag (NPC junk
// only: never cards, gear or valuable materials - RULES.md) sells for the shortfall. Returns the weapon and its shop;
// the trip sells at the Tool Dealer first, then buys (tripGear ranks the class weapon first). b.mu held.
// Lesson (2026-11-14): Holy 994, a lv51 Dancer on the starter Knife with 300z, carried ~200 sellable loot items at
// 24% weight - never a "bag full" trip - so it never sold, never afforded its 1000z Bow, and meleed with STR 1.
func (b *Bot) classWeaponLootNeed() (*Item, *Shop) {
	if !b.classWeaponMissing() || !b.hasLoot() {
		return nil, nil
	}
	budget := b.Zeny - b.gearReserve()
	if it, _ := b.classWeaponPick(budget); it != nil {
		return nil, nil // affordable now: classWeaponNeed's plain trip
	}
	it, s := b.classWeaponPick(budget + b.lootValue()*9/10)
	if it == nil || s == nil || potionShop(townOf(s.Map)) == nil {
		return nil, nil // the sale and the buy must be in one town (one trip)
	}
	return it, s
}

// classWeaponPick: the cheapest class weapon costing at most budget, at the nearest shop (or anywhere by Kafra when
// the fees fit). b.mu held.
func (b *Bot) classWeaponPick(budget int) (*Item, *Shop) {
	f := family(b.Class)
	var ids []int
	if p := b.plan(); p != nil {
		ids = append(ids, p.Weapons...)
	}
	ids = append(ids, starterWeapons...)
	var best *Item
	var bestShop *Shop
	for _, id := range ids {
		it := items[id]
		if it == nil || it.Buy <= 0 || it.Buy > budget || !b.canEquip(it) || b.gearFailed[id] || gearSlot(it.Loc) != 0x2 || !isClassWeapon(f, it) {
			continue
		}
		if best != nil && it.Buy >= best.Buy {
			continue // cheapest proper one first
		}
		s := b.gearShopNear(id)
		if s == nil && it.Buy+2*200+100 <= budget {
			s = b.gearShopAny(id) // e.g. bows: Payon / Archer Village / Prontera's market by Kafra
		}
		if s != nil {
			best, bestShop = it, s
		}
	}
	return best, bestShop
}

// invReady: the inventory of this session has arrived (or 20 s passed). Lesson (2026-11-14): the AI loop starts at
// login before the item lists arrive; with an empty bag the bot saw "no weapon, no potions" (an urgent trip) and
// left on a "buying Mace" trip 0.4 s after every login - ThePixelstorm Heals did it 12 times in an hour, each one cut
// short by the next restart. No trip or gear decision is made on a half-loaded character.
func (b *Bot) invReady() bool { return b.invSeen || time.Since(b.invWaitFrom) > 20*time.Second }

// gearIsFar: the shop is beyond maxGearHops (buying there is a planned trip).
func (b *Bot) gearIsFar(id int) bool { return b.gearShopNear(id) == nil }

// equipFromInventory: wear the best usable item we carry for each slot (after buying, or loot that's better).
// A bare weapon hand goes first. gearFailed (a failed shopping trip for that ID) no longer stops wearing an item we
// already carry; the server's refusal of one item is InvItem.NoEquip.
func (b *Bot) equipFromInventory() {
	weaponFirst := b.equipped[0x2] == nil
	for pass := 0; pass < 2; pass++ {
		if pass == 1 || weaponFirst {
			if b.equipPass(pass == 0) {
				return
			}
		}
	}
}

// equipPass sends one equip request (weaponsOnly: weapon-slot items only); true if one was sent.
func (b *Bot) equipPass(weaponsOnly bool) bool {
	for _, it := range b.inv {
		if it.Equipped || it.Count <= 0 {
			continue
		}
		x := items[it.ItemID]
		if x == nil || x.Loc == 0 || !b.wearable(it, x) || weaponsOnly && gearSlot(x.Loc) != 0x2 {
			continue
		}
		if gearSlot(x.Loc) == 0x2 && b.performing(time.Now()) {
			continue // song.go: unequipping ends the song - never swap weapons mid-song
		}
		if isBow(x) && b.equipped[0x2] != nil && b.arrowCount() == 0 {
			continue // a bow without arrows can't attack: keep the melee weapon until arrows are bought (ammo.go)
		}
		slot := gearSlot(x.Loc)
		cur := b.equipped[slot]
		if cur != nil && b.itemScore(items[cur.ItemID]) >= b.itemScore(x) {
			continue
		}
		if cur != nil && hasCardIn(cur, itemCreamyCard) {
			continue // teleslot.go: the accessory with the Teleport card stays on
		}
		// economy run 9 (QA 14:00: xXRinfangXx swapped Goggles / Bandana / Hat / Ribbon every ~5 s): headgear sits in
		// up to three slots, and Goggles (upper+mid) was compared only with what sat under its own key - each piece
		// beat an empty slot in turn. Compare with everything worn on any slot it would cover, and want a clear gain.
		if x.Loc&0x301 != 0 {
			worn := 0
			for s, e := range b.equipped {
				if s == eqpAmmo || e == nil {
					continue
				}
				if ex := items[e.ItemID]; ex != nil && ex.Loc&x.Loc != 0 && ex.Loc&0x301 != 0 {
					worn += b.itemScore(ex)
				}
			}
			if worn > 0 && b.itemScore(x) <= worn+worn/10 {
				continue
			}
		}
		if slot == 0x20 && b.equipped[0x2] != nil && items[b.equipped[0x2].ItemID] != nil && items[b.equipped[0x2].ItemID].Loc == 0x22 {
			continue
		}
		loc := x.Loc
		if loc == 0x88 { // accessory: either side
			loc = 0x8
			if b.equipped[0x88] != nil {
				loc = 0x80
			}
		}
		b.equipWanted, b.equipIdx = it.ItemID, it.Index
		idx := it.Index
		go b.send(newPkt(0x0998).u16(uint16(idx)).u32(loc).b)
		return true // one at a time; the server answers with 0x0999
	}
	return false
}

// planStatus: one-line progress report for memory / API.
func (b *Bot) planStatus() string {
	p := b.plan()
	if p == nil {
		return ""
	}
	var gear []string
	for _, slot := range []uint32{0x2, 0x10, 0x20, 0x100, 0x4, 0x40} {
		if e := b.equipped[slot]; e != nil && items[e.ItemID] != nil {
			gear = append(gear, items[e.ItemID].Name)
		}
	}
	next := ""
	for _, route := range [][]int{p.Weapons, p.Armors} {
		for _, id := range route {
			it := items[id]
			if it == nil || b.canEquip(it) && b.equipped[gearSlot(it.Loc)] != nil && b.equipped[gearSlot(it.Loc)].ItemID == id {
				continue
			}
			if b.itemScore(it) > b.itemScore(b.curIn(gearSlot(it.Loc))) {
				next = fmt.Sprintf("%s (%dz, lv%d)", it.Name, it.Buy, it.MinLv)
				break
			}
		}
		if next != "" {
			break
		}
	}
	return fmt.Sprintf("plan: %s -> %s | now lv %d/%d %s | stats str%d agi%d vit%d int%d dex%d luk%d | gear: %s | next buy: %s",
		p.Name, className(p.Class), b.BaseLvOr(), b.JobLv, className(b.Class), b.stats[0], b.stats[1], b.stats[2], b.stats[3], b.stats[4], b.stats[5],
		strings.Join(gear, ", "), next)
}

func (b *Bot) curIn(slot uint32) *Item {
	if e := b.equipped[slot]; e != nil {
		return items[e.ItemID]
	}
	return nil
}

// basicLv: Novice Basic Skill level (sit at 3, join a party at 5, create one at 7). Classes past Novice have it maxed.
func (b *Bot) basicLv() int {
	if b.Class != 0 && b.Class != 4001 && b.Class != 4023 {
		return 9
	}
	if s := b.skills[1]; s != nil {
		return s.Lv
	}
	return 0
}

var shopReach = map[string]bool{}

// shopReachable: whether a shop's map can be walked to from Prontera (Comodo, Morroc etc. can't on this server).
func shopReachable(m string) bool {
	gearMu.Lock()
	v, ok := shopReach[m]
	gearMu.Unlock()
	if ok {
		return v
	}
	v = m == "prontera" || RouteMaps("prontera", 156, 191, m, 99) != nil
	gearMu.Lock()
	shopReach[m] = v // shared by all bot goroutines and the economy manager: guarded
	gearMu.Unlock()
	return v
}

// gearStepNeed (b.mu held): the class weapon is in hand, but the weapon (or the body armor) is far below the level -
// a lv40 Thief with the Knife, a Merchant lv37 in a Cotton Shirt (QA 2026-11-14: they died on the maps their level
// fits). One NECESSARY step, like the class weapon: the best affordable NPC weapon that lifts the power ratio by 0.2+,
// else body armor that lifts the DEF ratio by 0.25+, within the purse rule (gearReserve). Bag items win (no trip).
func (b *Bot) gearStepNeed() (*Item, *Shop) {
	f, lv := family(b.Class), b.BaseLvOr()
	if lv < 20 {
		return nil, nil
	}
	budget := b.Zeny - b.gearReserve()
	pick := func(ids []int, slot uint32, score func(*Item) float64, floor float64) (*Item, *Shop) {
		for _, it := range b.inv { // already carrying something better: equipFromInventory / identifyThink, no trip
			if x := items[it.ItemID]; x != nil && !it.Equipped && gearSlot(x.Loc) == slot && score(x) >= floor &&
				(b.wearable(it, x) || it.Unidentified && b.canEquip(x) && b.invIndex(itemMagnifier) >= 0) {
				return nil, nil
			}
		}
		var best *Item
		var bestShop *Shop
		for _, id := range ids {
			it := items[id]
			if it == nil || it.Buy <= 0 || it.Buy > budget || !b.canEquip(it) || b.gearFailed[id] || gearSlot(it.Loc) != slot ||
				score(it) < floor || (slot == 0x2 && !isClassWeapon(f, it)) {
				continue
			}
			if best != nil && (score(it) < score(best) || score(it) == score(best) && it.Buy >= best.Buy) {
				continue
			}
			s := b.gearShopNear(id)
			if s == nil && it.Buy+2*200+100 <= budget {
				s = b.gearShopAny(id)
			}
			if s != nil {
				best, bestShop = it, s
			}
		}
		return best, bestShop
	}
	// only the WEAPON is a necessary step; armor and accessories ride along on that trip or a real-need trip (town.go
	// chains up to 2 more affordable steps in the same town). QA 02:15: Adventurer's Suit / Guard / Goggles trips of
	// their own broke "town only for a real need".
	if weak, w := b.underGeared(); weak {
		ws := weaponPowerRatio(f, lv, w)
		var ids []int
		if p := b.plan(); p != nil {
			ids = append(ids, p.Weapons...)
		}
		ids = append(ids, weaponSteps...)
		return pick(ids, 0x2, func(x *Item) float64 { return weaponPowerRatio(f, lv, x) }, ws+0.2)
	}
	return nil, nil
}
