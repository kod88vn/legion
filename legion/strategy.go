package main

// Leveling strategy distilled from the iRO Wiki Renewal guides (irowiki.org/wiki/Leveling_Spots,
// irowiki.org/wiki/Experience, class pages; fetched 2026-11) and checked against this server's own data
// (rAthena re mob_db + npc/re/mobs spawns: exp/HP ratio, monster level, element, aggressiveness).
// See research/STRATEGY.md and research/strategy_data.json for the full digest.
//
// Every map below has monster spawns AND is reachable on foot (warp graph + walkable regions, verified with
// RouteMaps from prontera/izlude/payon/geffen/alberta). Byalan (iz_dun*), Ant Hell, the Culvert, Sunken Ship,
// Sphinx, the far Sograt desert, Comodo, Umbala, Amatsu... need NPC transport or a desert crossing that
// Legion can't do yet, so they are left out (they are listed in strategy_data.json with "reachable": false).
//
// Renewal rules that shape the table:
//   - EXP level modifier: monsters 1-10 levels ABOVE you give 105-140% exp, +16 or more drops to 40%;
//     monsters 6-20 below give 85-95%, 21+ below 60% -> 10%. So a spot's band starts a few levels under the
//     monsters' level and ends ~15 above it.
//   - party exp is shared evenly while the party's base-level spread is within party_share_level: 15 in stock
//     rAthena, 99 on this server (rathena/conf/import/inter_conf.txt), i.e. at any spread (PT35, lfg.go partyLvOK).
//   - mob_count_rate is 200% on this server: fields are twice as crowded as on iRO, aggressive maps hurt more.
// Bots prefer these spots for their level band, then fall back to the generic spawn-table search (SpotsFor).

type GuideSpot struct {
	Min, Max int
	Map      string
	Aggro    bool   // mostly aggressive monsters: only for risk-tolerant / sturdy characters
	Mobby    bool   // monsters bunch up: suits AoE classes (mages, Magnus, Arrow Shower, Bowling Bash...)
	Ranged   bool   // only sensible for ranged classes (e.g. stationary Mandragoras that shred melee)
	Party    bool   // dungeon / dangerous spot meant for a party (tank + priest), soloed only by bold, over-levelled players
	Element  string // attack element that works best here ("" = anything)
	Note     string
}

var guideSpots = []GuideSpot{
	// ---- 1-14: Novice and early first job (Poring/Fabre/Pupa die in 1-3 hits; exp/HP is the best in the game)
	{1, 12, "prt_fild08", false, false, false, false, "", "Poring/Lunatic/Fabre/Pupa south of Prontera, next to Izlude: the classic Novice field"},
	{1, 12, "gef_fild07", false, false, false, false, "", "Pupa (never fight back) and Porings just west of Geffen: free exp"},
	{1, 12, "gef_fild00", false, false, false, false, "", "Chonchon swarm north of Geffen; quick kills, they hit a little"},
	{1, 12, "prt_fild06", false, false, false, false, "Fire", "Fabre/Lunatic east of Prontera"},
	{1, 12, "pay_fild03", false, false, false, false, "Fire", "Fabre/Willow/Pupa between Payon and Alberta"},
	{1, 14, "pay_fild01", false, false, false, false, "Fire", "Willow outside Payon: slow earth plants, fire bolts one-shot them"},
	{2, 12, "prt_fild01", false, false, false, false, "", "Lunatic meadow north-west of Prontera, quiet"},
	// ---- 8-28
	{8, 20, "prt_fild05", false, false, false, false, "", "Hornet/Thief Bug west of Prontera: fast insects, keep red potions"},
	{8, 18, "gef_fild04", false, false, true, false, "Fire", "Mandragora don't move: free kills for archers/mages, deadly for melee"},
	{9, 22, "mjolnir_09", false, false, false, false, "Fire", "Savage Babe/Hornet/Willow at the foot of Mt. Mjolnir, all passive"},
	{10, 24, "prt_fild07", false, false, false, false, "Fire", "Rocker/Savage Babe, passive; Vocal (mini boss) spawns rarely"},
	{10, 24, "gef_fild01", false, false, false, false, "Wind", "Roda Frog/Ambernite, water monsters: lightning bolts; only the rare Toad aggroes"},
	{10, 24, "prt_fild00", false, false, false, false, "Wind", "Roda Frog/Ambernite north of Prontera"},
	{12, 26, "pay_fild06", false, false, false, false, "Fire", "Wormtail/Boa/Spore near Payon, passive"},
	{13, 27, "pay_fild08", false, false, false, false, "Wind", "Spore (water plant) field next to Payon, passive"},
	{13, 27, "pay_fild02", false, false, false, false, "Fire", "Boa (earth snake) field near Payon, passive"},
	{14, 28, "prt_fild04", false, false, false, false, "Wind", "Ambernite snails: high DEF, water; better for magic/archers"},
	// ---- 18-36
	{18, 32, "pay_dun00", true, false, false, false, "Holy", "Payon Cave 1: slow aggro Zombies, Skeletons, Familiars; Holy Light / fire works, acolytes love it"},
	{18, 32, "prt_fild02", false, false, false, false, "Earth", "Stainer/Creamy/Poporing east of Prontera, wind insects"},
	{18, 32, "gef_fild05", false, false, false, false, "Earth", "Creamy/Stainer/Smokie south of Geffen, passive"},
	{18, 34, "moc_fild01", false, false, false, false, "", "Muka cactus + Peco Peco (fire, assist each other) south of Prontera"},
	{19, 34, "moc_fild02", false, false, false, false, "Water", "Peco Peco field: they assist, cold bolts / water against them"},
	{20, 35, "mjolnir_01", false, false, false, false, "Fire", "Caramel/Bigfoot earth brutes, passive"},
	{20, 35, "pay_fild07", false, false, false, false, "Fire", "Bigfoot/Caramel west of Payon, passive"},
	{20, 36, "mjo_dun01", false, false, false, false, "Holy", "Mjolnir Coal Mine 1: Tarou/Familiar (dark) and Martin"},
	{22, 36, "pay_fild04", false, false, false, false, "", "Poporing/Marin: poison/water jellies, any element works"},
	{22, 36, "mjolnir_06", true, true, false, false, "Fire", "Poison Spore (aggro, poison) / Smokie; AoE recommended"},
	// ---- 28-48
	{28, 42, "pay_fild09", false, false, false, false, "Fire", "Horn/Coco near Payon; the few Elder Willows aggro (cold bolts)"},
	{28, 44, "gef_fild09", false, false, false, false, "Fire", "Coco squirrels, passive, south of Geffen"},
	{28, 44, "gef_fild02", false, false, false, false, "Fire", "Coco/Horn/Poporing east of Geffen"},
	{28, 42, "prt_fild10", true, false, false, false, "Water", "Elder Willow grove (aggro fire plants): cold bolts / water"},
	{28, 42, "mjolnir_02", true, false, false, false, "Water", "Elder Willow/Yoyo on Mt. Mjolnir, mostly aggro"},
	{30, 44, "prt_fild03", true, false, false, false, "Fire", "Yoyo monkeys (aggro, steal loot) and Horns"},
	{32, 48, "pay_dun01", true, false, false, true, "Holy", "Payon Cave 2: Soldier/Archer Skeletons (aggro undead); party with a priest"},
	{34, 50, "mjo_dun02", true, false, false, false, "Fire", "Coal Mine 2: Martin/Giearth/Skeleton Worker"},
	// ---- 40-60
	{40, 56, "gef_fild10", true, true, false, false, "Fire", "Orc Village: Orc Warriors/Ladies all aggro and assist; AoE or a party; beware Orc Lord"},
	{40, 56, "gef_fild03", true, true, false, false, "Fire", "Orc Lady field next to the village, all aggro"},
	{40, 55, "moc_fild03", false, false, false, false, "Fire", "Wolf packs (assist), a few aggro Argos/Steel Chonchons"},
	{40, 55, "gef_fild11", true, false, false, false, "", "Goblin camp: five Goblin types, each a different element, all aggro"},
	{40, 56, "gef_dun00", true, false, false, true, "Fire", "Geffen Dungeon 1: Hunter Flies (fast, aggro, drain HP) - bring a party"},
	{42, 56, "mjo_dun03", true, false, false, true, "Holy", "Coal Mine 3: aggro Skeleton Workers and Mysts"},
	{42, 58, "mjolnir_10", true, false, false, false, "", "Argos (aggro poison spiders) / Beetle King / Flora"},
	{44, 60, "orcsdun01", true, true, false, true, "Holy", "Orc Dungeon 1: mobs of Orc Zombies - priests Magnus/Turn Undead, wizards fire AoE"},
	{44, 60, "pay_dun02", true, false, false, true, "Holy", "Payon Cave 3: Munak/Bongun/Soldier Skeletons, all undead"},
	{44, 60, "moc_fild13", true, false, false, false, "Earth", "Steel Chonchon swarm (aggro wind insects)"},
	// ---- 50-72
	{50, 66, "prt_fild09", false, false, false, false, "Wind", "Magnolia (passive water demons) + some aggro Metallers"},
	{50, 66, "pay_fild10", false, false, false, false, "Water", "Greatest General/Beetle King/Savage: everything passive"},
	{50, 66, "mjolnir_08", false, false, false, false, "Fire", "Beetle King/Savage/Flora, passive"},
	{52, 68, "mjolnir_07", false, false, false, false, "Fire", "Savage boars (earth), passive"},
	{50, 66, "orcsdun02", true, false, false, true, "Holy", "Orc Dungeon 2: Zenorc/Orc Skeleton/Drainliar"},
	{52, 70, "pay_dun03", false, false, false, false, "Wind", "Payon Cave 4: Sohee (passive water demons), a few Whispers"},
	{52, 68, "prt_fild11", true, false, false, false, "", "Goblin Archers/Steamriders and Rotar Zairo, aggro"},
	{54, 72, "gef_dun01", true, false, false, true, "Holy", "Geffen Dungeon 2: Ghoul/Jakk/Nightmare, all aggro - party"},
	// ---- 58-84
	{58, 74, "mjolnir_12", false, false, false, false, "Earth", "Dustiness (passive wind insects) + aggro Hunter Fly/Mantis; on the way to Al De Baran"},
	{60, 76, "mjolnir_04", true, false, false, false, "Fire", "Driller/Mantis/Side Winder, all aggro"},
	{62, 78, "yuno_fild01", false, false, false, false, "Fire", "Geographer (passive earth plants) / Dustiness near Juno"},
	{62, 78, "yuno_fild12", false, false, false, false, "Fire", "Geographers near Juno"},
	{64, 80, "mjolnir_11", true, false, false, false, "Fire", "Argiope (aggro, poison) - iRO guide favourite 60-95"},
	{64, 80, "mjolnir_03", true, false, false, false, "", "Side Winder/Mantis/Driller, all aggro"},
	{64, 82, "gef_fild08", true, true, false, false, "Earth", "Petite (Sky, wind dragons) bunch in the corners: AoE"},
	{66, 82, "mjolnir_05", true, false, false, false, "Fire", "Argiope/Mantis/Driller, all aggro"},
	// ---- 70-92
	{70, 86, "yuno_fild08", false, false, false, false, "Water", "Grand Peco/Goat (fire, passive) near Juno"},
	{70, 86, "yuno_fild09", false, false, false, false, "Water", "Grand Peco + some aggro Sleepers"},
	{72, 88, "yuno_fild11", false, false, false, false, "Water", "Goat field (fire), passive"},
	{72, 88, "c_tower1", true, false, false, true, "", "Clock Tower 1F (Al De Baran): Rideword/Punk aggro"},
	{74, 90, "c_tower2", true, false, false, true, "Wind", "Clock Tower 2F: Clock/Punk"},
	{72, 90, "gl_church", true, false, false, true, "Holy", "Glast Heim Chivalry/Church: Evil Druid/Wraith undead - priest party"},
	{72, 90, "gl_step", true, false, false, false, "Earth", "Glast Heim Steps: Wind Ghosts"},
	{74, 92, "gef_fild06", true, false, false, false, "Fire", "Petite (Ground, earth dragons), aggro"},
	{74, 92, "yuno_fild04", true, false, false, false, "Earth", "Harpy (silence!) / Goat; green potions if INT is low"},
	{74, 92, "yuno_fild07", false, false, false, false, "Water", "Goat/Grand Peco + some Harpies"},
	{75, 92, "ein_fild08", false, false, false, false, "", "Metaling (passive) / Porcellio near Einbroch"},
	{75, 92, "ein_fild07", false, false, false, false, "", "Metaling/Porcellio/Holden, passive"},
	{76, 94, "ein_fild06", false, false, false, false, "Fire", "Holden moles (earth), passive"},
	{76, 94, "lhz_fild01", true, false, false, false, "Fire", "Rafflesia/Stem Worm (aggro plants) near Lighthalzen"},
	// ---- 80-104
	{80, 100, "gl_chyard", true, false, false, true, "Holy", "Glast Heim Churchyard: Dark Priest/Wraith - Holy/Fire, priest party"},
	{80, 100, "ra_fild12", true, false, false, false, "Earth", "Roween (wind wolves) near Rachel: earth weapons"},
	{80, 100, "c_tower3", true, false, false, true, "", "Clock Tower 3F: Alarm (aggro)"},
	{80, 98, "lhz_fild02", true, false, false, false, "Earth", "Stem Worm/Breeze (wind), aggro"},
	{80, 98, "alde_dun02", true, false, false, true, "Water", "Clock Tower B2: High Orc/Orc Archer, aggro"},
	{82, 100, "gl_prison", true, false, false, true, "Holy", "Glast Heim Prison: Zombie Prisoner/Injustice"},
	{82, 100, "hu_fild01", true, false, false, false, "", "Novus (small aggro dragons) near Hugel"},
	{82, 100, "hu_fild05", true, false, false, false, "", "Novus field near Hugel"},
	{82, 100, "ein_fild03", true, false, false, false, "", "Teddy Bear/Venomous/Noxious, aggro"},
	{82, 100, "ein_dun01", false, false, false, true, "Fire", "Einbroch Mine 1: Pitman (passive), Noxious"},
	{84, 104, "gl_prison1", true, false, false, true, "Holy", "Glast Heim Underprison: Skel Prisoner/Injustice - iRO guide 80-111"},
	{84, 102, "lhz_fild03", true, false, false, false, "Earth", "Breeze (wind), aggro"},
	{86, 104, "ra_fild08", false, false, false, false, "", "Anacondaq (passive) + Roween/Hill Wind"},
	{88, 106, "ein_dun02", true, false, false, true, "", "Einbroch Mine 2: Mineral/Obsidian/Old Stove"},
	{88, 108, "ra_fild01", true, false, false, false, "Water", "Desert Wolf (fire, aggro): water weapons"},
	// ---- 92-130
	{92, 110, "ve_fild07", false, false, false, false, "Fire", "Stapo/Drosera/Muscipular near Veins, passive"},
	{92, 110, "ice_dun01", false, false, false, false, "Wind", "Ice Cave 1: Siroma (passive water)"},
	{94, 112, "ve_fild02", false, false, false, false, "Fire", "Drosera (passive earth plants)"},
	{94, 112, "ve_fild01", false, false, false, false, "Fire", "Stapo/Drosera/Muscipular, passive"},
	{94, 112, "mag_dun01", true, false, false, true, "Water", "Nogg Road 1: Explosion/Lava Golem/Kaho (fire), aggro"},
	{95, 112, "gl_sew02", true, false, false, true, "Earth", "Glast Heim Culvert 2: Gargoyles (ranged, wind)"},
	{95, 112, "ra_fild03", true, false, false, false, "Earth", "Hill Wind (wind), aggro"},
	{96, 114, "ve_fild04", false, false, false, false, "Fire", "Muscipular field, passive"},
	{98, 116, "ra_fild05", true, false, false, false, "", "Kobolds (3 elements), aggro; reduction cards help"},
	{98, 116, "ra_fild06", true, false, false, false, "", "Kobold Archers + Kobolds, aggro"},
	{98, 118, "mag_dun02", true, true, false, true, "Holy", "Nogg Road 2: extremely mobby (Nightmare Terror/Deleter) - AoE party, fire armor"},
	{100, 118, "gl_sew03", true, false, false, true, "Fire", "Glast Heim Culvert 3: Sting/Gargoyle"},
	{100, 120, "ve_fild03", false, true, false, false, "Water", "Magmaring (passive looters bunch on drops) - AoE them"},
	{100, 120, "ice_dun02", true, false, false, true, "Fire", "Ice Cave 2: Snowier/Siroma/Gazeti"},
	{102, 122, "gl_dun01", true, false, false, true, "Fire", "Glast Heim Cave 1: Arclouze/Sting"},
	{104, 125, "gl_knt01", true, false, false, true, "Holy", "Glast Heim Knight Hall 1: Raydric (dark) - party"},
	{104, 126, "gl_sew04", true, false, false, true, "Wind", "Glast Heim Culvert 4: Anolian"},
	{106, 128, "gl_knt02", true, false, false, true, "Holy", "Glast Heim Knight Hall 2: Raydric/Khalitzburg"},
	{106, 128, "ice_dun03", true, false, false, true, "Fire", "Ice Cave 3: Ice Titan/Gazeti"},
	{108, 130, "juperos_01", true, true, false, true, "", "Juperos 1: Venatu, extremely mobby, 2-cell attack range"},
	{110, 132, "gl_cas02", true, false, false, true, "Holy", "Glast Heim Castle 2: Wanderer/Raydric"},
	// ---- 118+: few reachable maps; the generic spawn search covers the rest
	{118, 145, "thor_v02", false, false, false, true, "Water", "Thor Volcano 2: Knocker (passive) / Imp / Kaho"},
	{120, 145, "thor_v01", true, false, false, true, "Water", "Thor Volcano 1: Imps (fire)"},
	{150, 185, "ra_fild11", true, false, false, false, "", "Ashen Goat/Baby Gray Wolf, aggro (Rachel)"},
	{150, 185, "ra_fild10", true, false, false, false, "", "Ashen Goat/Baby Gray Wolf, aggro"},
	{155, 190, "verus03", true, false, false, true, "", "Verus: Exploration Rover/Repair Robot"},
	{158, 195, "ver_eju", true, false, false, true, "", "Verus Eju: Recon Robots"},
}

// aoeFamily: classes that clear mobby spots well
func aoeFamily(class int) bool {
	f := family(class)
	return f == famMage || class == 4054 || class == 4060 || class == 4056 || class == 4062
}

// guideSpotsFor returns guide spots that exist on this server for the level, filtered by the character's
// risk tolerance and class strengths. Spots that suit the character especially well (party spots for a
// party, holy/undead dungeons for acolytes, mobby fields for AoE classes) are listed twice, which doubles
// their chance in chooseHuntingGround's random pick.
func (b *Bot) guideSpotsFor(level int) []GuideSpot {
	var out []GuideSpot
	inParty := b.PartyID != 0
	maxLv := b.targetMaxLv() + level - b.powerLv() // level is an effective level (gearpower.go)
	for _, g := range guideSpots {
		if level < g.Min || level > g.Max+3 || maps[g.Map] == nil || fitShare(g.Map, maxLv) < minFitShare || swarmVeto(g.Map, level) ||
			mapReadsEmpty(g.Map) { // seen.go: bots there see no monsters
			continue
		}
		if g.Aggro && b.P.Risk < 0.35 && level < g.Max && !inParty { // cautious solo players avoid aggressive spots early
			continue
		}
		if g.Mobby && !aoeFamily(b.Class) && b.P.Risk < 0.6 && !inParty {
			continue
		}
		if g.Ranged && !rangedFamily(b.Class) { // e.g. Mandragora fields shred melee characters
			continue
		}
		// poison.go: a solo bot with no cure (Cure skill, Green Potions) picks poisoner-heavy maps less often - poison
		// stops HP regen, so sitting never heals there (QA 2026-11-14: mjolnir_06's Poison Spores, 17 of 51 deaths)
		if !inParty && !b.canCure() && poisonShare(g.Map) >= 0.3 && b.rng.Float64() < 0.6 {
			continue
		}
		// party dungeons: soloed only by bold players who out-level the spot
		if g.Party && !inParty && !(b.P.Risk >= 0.65 && level >= g.Min+6) {
			continue
		}
		// lfg.go need parties (live 2026-11-14 12:13: 41 of 58 deaths were solo bots after parties broke up): with goal
		// parties on, a solo character never takes a party spot or a map too hard for it alone - it finds a party first
		// QA 2026-11-14 17:05 (goal parties off): mjolnir_02 had 19 bots and 10 deaths in 9 min - solo bots never saw
		// the train / aggression rule (partyMapTooHard). A solo character is checked like a party of one (mages and
		// alchemists rated partyFragileLv weaker), always.
		if !inParty && (goalPartiesOn() && g.Party || partyMapTooHard(g.Map, b.soloRateLv(level)) ||
			g.Map != b.MapName && routeCrossesTooHard(b.MapName, b.X, b.Y, g.Map, b.soloRateLv(level))) { // QA: Elder Willow / train on mjolnir_02 on the way
			continue
		}
		out = append(out, g)
		switch {
		case g.Party && inParty,
			g.Element == "Holy" && family(b.Class) == famAcolyte,
			g.Mobby && aoeFamily(b.Class):
			out = append(out, g)
		}
	}
	return out
}

// rangedFamily: classes that fight from range (archers, mages).
func rangedFamily(class int) bool {
	f := family(class)
	return f == famArcher || f == famMage
}
