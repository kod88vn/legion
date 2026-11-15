package main

import "testing"

// Replays of the QA death probe (2026-11-14, main legion 06:01-06:06 UTC). Before the survival batch every "should
// escape" case below returned false at the HP the bot was seen at, and the bot died.
func TestLosingFightDeathProbe(t *testing.T) {
	cases := []struct {
		name string
		v    fightView
		want bool
	}{
		// lord86, Acolyte lv7, no weapon, "defending vs Willow" (Renewal lv 8): 0.88 -> 0.66 -> 0.42 -> dead in 12 s
		{"lord86 at 66% (lost 22% in 4 s)", fightView{hp: 0.66, drop: 0.22, n: 1, maxLv: 8, lv: 7, pots: 11, weak: true}, true},
		{"lord86 at 42%", fightView{hp: 0.42, drop: 0.24, n: 1, maxLv: 8, lv: 7, pots: 11, weak: true}, true},
		// QA 2026-11-14 14:05-14:26 UTC: one monster at our level with potions (and a weapon) = drink and fight
		// (survive's potion line is 70% on a fast drop); only the last-resort line (lv < 20: under 25%) leaves
		{"Holy_lord lv7 (gear lv2) vs a lv6 at 31%", fightView{hp: 0.31, drop: 0.35, n: 1, maxLv: 6, lv: 2, base: 7, pots: 40}, false},
		{"Holy_lord at 22%", fightView{hp: 0.22, drop: 0.2, n: 1, maxLv: 6, lv: 2, base: 7, pots: 40}, true},
		// QA 2026-11-14 38 A->B->A Kafra hops in 19 min: "lost 47% HP in seconds (1 on me, lv 8 vs 1)"
		{"novice lv3 (gear 1) vs a lv8 at 53% after 47%", fightView{hp: 0.53, drop: 0.47, n: 1, maxLv: 8, lv: 1, base: 3, pots: 100}, false},
		{"acolyte lv8 vs a lv12 at 40% after 35%", fightView{hp: 0.40, drop: 0.35, n: 1, maxLv: 12, lv: 2, base: 8, pots: 60}, false},
		{"novice lv3 vs a lv8 at 20%", fightView{hp: 0.2, drop: 0.3, n: 1, maxLv: 8, lv: 1, base: 3, pots: 100}, true},
		// QA 2026-11-14: TheShadowsenpai (Alchemist 69, gear 51) winged at 84% from ONE lv63-65 monster on mjolnir_12
		{"Alchemist 69 (gear 51) vs one lv65 at 84%", fightView{hp: 0.84, drop: 0.05, n: 1, maxLv: 65, lv: 51, base: 69, pots: 40}, false},
		{"Alchemist 69 (gear 51) vs one lv65 at 33%", fightView{hp: 0.33, drop: 0.05, n: 1, maxLv: 65, lv: 51, base: 69, pots: 0}, true},
		// QA 2026-11-14 19:41-19:49 UTC (core): lv72-77 rejoining through pay_dun01 fled lv30s skeletons and died
		{"Wizard 76 (gear 60) rejoining, 4 lv34 skeletons at 70%", fightView{hp: 0.7, drop: 0.1, n: 4, maxLv: 34, lv: 60, base: 76, pots: 30, transit: true}, false},
		{"Knight 74 rejoining, 3 lv34 at 85%", fightView{hp: 0.85, drop: 0.05, n: 3, maxLv: 34, lv: 70, base: 74, pots: 30, transit: true, tank: true}, false},
		{"lv46 in transit, 3 lv26 Spores (the swarm rule still holds)", fightView{hp: 0.9, n: 3, maxLv: 26, lv: 46, pots: 20, transit: true}, true},
		{"novice lv3 vs a lv18 Boa at 50% after 40%", fightView{hp: 0.5, drop: 0.4, n: 1, maxLv: 18, lv: 1, base: 3, pots: 100}, true},
		{"lv7 vs two lv6 at 45%", fightView{hp: 0.45, drop: 0.24, n: 2, maxLv: 6, lv: 7, pots: 11}, true},
		// Lily 07, Merchant lv21 on moc_fild03: a level 47 monster; teleported only at 0% HP
		{"Lily 07 after the first hit", fightView{hp: 0.8, drop: 0.2, n: 1, maxLv: 47, lv: 21, pots: 20}, true},
		// two attackers at 30%
		{"two attackers at 30%", fightView{hp: 0.3, drop: 0.05, n: 2, maxLv: 5, lv: 10, pots: 30}, true},
		// a weaponless Novice lv4 against a lv4 monster at 55%
		{"weaponless novice", fightView{hp: 0.55, drop: 0.1, n: 1, maxLv: 4, lv: 4, pots: 5, weak: true}, true},
		// must NOT flee: a normal fight going fine
		{"swordman winning", fightView{hp: 0.8, drop: 0.1, n: 1, maxLv: 18, lv: 20, pots: 30}, false},
		{"mage slowly hurt", fightView{hp: 0.55, drop: 0.08, n: 1, maxLv: 12, lv: 15, pots: 30}, false},
		{"acolyte with Heal ready vs equal mob at 50%", fightView{hp: 0.5, drop: 0.1, n: 1, maxLv: 8, lv: 8, pots: 8, weak: true, canHeal: true}, false},
		{"party priest with SP at 60% after a big hit", fightView{hp: 0.6, drop: 0.25, n: 1, maxLv: 50, lv: 56, pots: 8, canHeal: true}, false},
		{"party priest with SP at 40% falling fast", fightView{hp: 0.4, drop: 0.25, n: 1, maxLv: 50, lv: 56, pots: 8, canHeal: true}, true},
		{"nobody hitting us", fightView{hp: 0.1, drop: 0.5, n: 0}, false},
		// QA 2026-11-14 10:17 UTC (wings working): with potions a 20-34% drop drinks first, leaves at 50% (tank 40%)
		{"lv40 hunter with potions at 60% after a 25% drop", fightView{hp: 0.6, drop: 0.25, n: 1, maxLv: 38, lv: 40, pots: 40}, false},
		{"lv40 hunter with potions at 45% after a 25% drop", fightView{hp: 0.45, drop: 0.25, n: 1, maxLv: 38, lv: 40, pots: 40}, true},
		{"lv45 knight with potions at 45% after a 25% drop", fightView{hp: 0.45, drop: 0.25, n: 1, maxLv: 44, lv: 45, pots: 40, tank: true}, false},
		{"lv45 knight with potions at 38% after a 25% drop", fightView{hp: 0.38, drop: 0.25, n: 1, maxLv: 44, lv: 45, pots: 40, tank: true}, true},
		{"lv40 with potions, one 40% hit at 55%", fightView{hp: 0.55, drop: 0.4, n: 1, maxLv: 38, lv: 40, pots: 40}, true},
	}
	for _, c := range cases {
		if got, why := losingFight(c.v); got != c.want {
			t.Errorf("%s: losingFight = %v (%q), want %v", c.name, got, why, c.want)
		}
	}
}

func TestPotLineFastDrop(t *testing.T) {
	b := &Bot{P: &Persona{Risk: 1}}
	if l := b.potLine(0); l != 0.5 {
		t.Errorf("potLine(no drop) = %v, want 0.5", l)
	}
	if l := b.potLine(0.25); l != 0.7 {
		t.Errorf("potLine(fast drop) = %v, want 0.7", l)
	}
}
