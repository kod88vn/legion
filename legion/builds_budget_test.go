package main

import "testing"

// Build budgets (rebalance 2026-11-15; ST01 / G10: no rebirth, so a second job is the last class): every plan fits the
// stat points a character has at base level 99 and the skill points of its two job lines, and lists its key skills.
const (
	budgetStatPoints  = 1273 // statPoints(99)
	budgetSkillPoints = 88   // first job 39 + second job 49; the Novice Basic Skill (9) is not part of a plan
)

// statCostTo: points to raise one stat from 1 to v (each point from v costs (v-1)/10+2, persona.go statPoints).
func statCostTo(v int) int {
	t := 0
	for x := 1; x < v; x++ {
		t += (x-1)/10 + 2
	}
	return t
}

// keySkills: the skills each build is named for. The plan must list each one at that level or more.
// Skill IDs are the server's (db/re/skill_db.yml); the reach through the skill tree is checked in tests/tools.
var keySkills = map[string][]SkillGoal{
	"knight":           {{60, 10}, {62, 10}},                                               // Two-Hand Quicken, Bowling Bash
	"knight_spear":     {{57, 10}, {56, 10}},                                               // Brandish Spear, Pierce
	"knight_pierce":    {{56, 10}, {59, 5}},                                                // Pierce, Spear Boomerang
	"crusader":         {{253, 10}, {249, 5}, {254, 4}, {255, 1}},                          // Holy Cross, Auto Guard, Grand Cross, Devotion
	"crusader_gc":      {{254, 10}, {255, 1}},                                              // Grand Cross, Devotion
	"crusader_spear":   {{258, 5}, {253, 10}, {254, 5}},                                    // Spear Quicken, Holy Cross, Grand Cross
	"wizard":           {{17, 5}, {18, 5}, {21, 3}, {89, 10}, {85, 10}, {83, 10}, {91, 5}}, // Fire Ball, Fire Wall, Thunder Storm, Storm Gust, Lord of Vermilion, Meteor Storm, Heaven's Drive
	"wizard_fire":      {{17, 10}, {18, 5}, {83, 10}, {89, 10}, {85, 10}, {91, 5}},         // Fire Ball, Fire Wall, Meteor Storm, Storm Gust, Lord of Vermilion, Heaven's Drive
	"wizard_frost":     {{84, 10}, {89, 10}, {85, 10}},                                     // Jupitel Thunder, Storm Gust, Lord of Vermilion
	"sage":             {{278, 10}, {91, 5}, {90, 3}},                                      // Free Cast, Heaven's Drive, Earth Spike
	"sage_support":     {{289, 5}, {285, 3}, {286, 3}, {287, 3}, {288, 1}},                 // Dispell, Volcano, Deluge, Whirlwind, Magnetic Earth
	"sage_hindsight":   {{279, 5}, {278, 10}},                                              // Auto Spell, Free Cast
	"hunter":           {{46, 10}, {47, 10}, {127, 1}},                                     // Double Strafe, Arrow Shower, Falcon
	"hunter_blitz":     {{129, 5}, {46, 10}},                                               // Blitz Beat, Double Strafe
	"hunter_agi":       {{46, 10}, {47, 5}},                                                // Double Strafe, Arrow Shower
	"hunter_trap":      {{123, 5}, {116, 5}},                                               // Claymore Trap, Land Mine
	"bard":             {{319, 10}, {320, 10}, {321, 10}, {322, 10}},                       // Whistle, Assassin Cross, Bragi, Apple of Idun
	"bard_arrow":       {{46, 10}, {320, 10}},                                              // Double Strafe, Assassin Cross
	"dancer":           {{327, 10}, {328, 10}, {329, 10}, {330, 10}},                       // Humming, Don't Forget Me, Fortune's Kiss, Service for You
	"dancer_arrow":     {{46, 10}, {324, 5}},                                               // Double Strafe, Throw Arrow
	"priest":           {{78, 1}, {79, 10}, {54, 1}, {12, 1}},                              // Lex Aeterna, Magnus Exorcismus, Resurrection, Safety Wall
	"priest_me":        {{78, 1}, {79, 10}, {54, 1}, {12, 1}},                              // Lex Aeterna, Magnus Exorcismus, Resurrection, Safety Wall
	"priest_battle":    {{66, 5}},                                                          // Impositio
	"monk":             {{271, 5}, {263, 10}, {272, 5}, {273, 3}},                          // Asura Strike, Triple Attack, Chain Combo, Combo Finish
	"monk_asura":       {{271, 5}},                                                         // Asura Strike
	"monk_occult":      {{266, 5}, {268, 5}},                                               // Occult Impaction, Steel Body
	"blacksmith":       {{110, 5}, {111, 5}, {153, 1}},                                     // Hammer Fall, Adrenaline Rush, Cart Revolution
	"blacksmith_mace":  {{110, 5}, {111, 5}, {153, 1}},                                     // Hammer Fall, Adrenaline Rush, Cart Revolution
	"alchemist":        {{230, 5}, {229, 5}, {153, 1}},                                     // Acid Terror, Demonstration, Cart Revolution
	"alchemist_battle": {{230, 5}, {229, 5}, {153, 1}},                                     // Acid Terror, Demonstration, Cart Revolution
	"alchemist_potion": {{231, 5}, {153, 1}},                                               // Aid Potion (Potion Pitcher), Cart Revolution
	"assassin":         {{48, 10}, {136, 10}, {134, 10}},                                   // Double Attack, Sonic Blow, Katar Mastery
	"assassin_sonic":   {{136, 10}, {134, 10}},                                             // Sonic Blow, Katar Mastery
	"assassin_dagger":  {{138, 10}, {141, 10}},                                             // Enchant Poison, Venom Splasher
	"rogue":            {{212, 10}, {214, 5}},                                              // Back Stab, Raid
	"rogue_bow":        {{46, 10}, {44, 10}},                                               // Double Strafe, Vulture's Eye
	"rogue_strip":      {{212, 10}, {218, 5}},                                              // Back Stab, Strip Weapon
}

// TestBuildBudgets: every plan fits 1,273 stat points and 88 skill points, and lists its key skills.
func TestBuildBudgets(t *testing.T) {
	if got := statPoints(99); got != budgetStatPoints {
		t.Fatalf("statPoints(99) = %d, want %d", got, budgetStatPoints)
	}
	for id := range keySkills {
		if builds[id] == nil {
			t.Errorf("key skills for %q: no such build", id)
		}
	}
	for id, b := range builds {
		goal := [6]int{1, 1, 1, 1, 1, 1}
		for _, g := range b.Stats {
			goal[g.Stat] = max(goal[g.Stat], min(g.Value, 99))
		}
		cost := 0
		for _, v := range goal {
			cost += statCostTo(v)
		}
		if cost > budgetStatPoints {
			t.Errorf("%s: stat goals cost %d points, budget %d", id, cost, budgetStatPoints)
		}

		lv := map[int]int{}
		for _, g := range b.Skills {
			lv[g.ID] = max(lv[g.ID], g.Lv)
		}
		pts := 0
		for _, v := range lv {
			pts += v
		}
		if pts > budgetSkillPoints {
			t.Errorf("%s: skill goals list %d points, budget %d", id, pts, budgetSkillPoints)
		}
		for _, k := range keySkills[id] {
			if lv[k.ID] < k.Lv {
				t.Errorf("%s: key skill %d listed at %d, want %d", id, k.ID, lv[k.ID], k.Lv)
			}
		}
	}
}
