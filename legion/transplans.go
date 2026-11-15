package main

// Transcendent skill plans (owner 2026-11-14: "cap server at trans class").
//
// Progression ends at the transcendent 2nd job: Novice -> 1st -> 2nd -> rebirth at 99/50 (High Novice, base 1;
// rathena/npc-custom/legion.txt, at the stock 1,285,000z Book of Ymir price) -> High 1st -> trans 2nd -> STOP.
// 3rd/4th jobs don't exist on this server (their job-change NPCs are unloaded: npc-custom/scripts_custom.conf).
//
// A trans character relearns its build from scratch after rebirth: the High 1st job follows Build.Skills like the
// 1st job did; once it is a trans 2nd job, transSkills[plan] comes first (the 2nd-job core again, then the trans
// skills with their prerequisites), then the rest of Build.Skills. A trans 2nd job has 69 skill points (job 70)
// instead of 49, i.e. ~20 points for the trans skills. Build.Skills itself is unchanged, so no 1st/2nd-job
// character's plan moves (no wrong-build deletions: tools/wrongbuilds.py reads Build.Skills/Legacy only).
// Skills that need a quest (Whitesmith Cart Boost/Termination need Cart Revolution + Change Cart from the merchant
// skill quest) simply stay unlearnable until then: the planner skips goals the server doesn't offer.
var transSkills = map[string][]SkillGoal{
	// Peco Peco Riding 1 first on both riders (owner 2026-11-14: "knights must follow the official path to get peco as
	// soon as possible"; peco.go rents the bird the moment Riding + the fee are there).
	// Lord Knight: Two-Handed Sword Mastery 10, Twohand Quicken 10, Bowling Bash 10, Magnum Break 5, Aura Blade 5, Frenzy 1, Provoke 5, Parrying 10, Increase HP Recovery 5, Peco Peco Riding 1, Spear Mastery 5, Concentration 5, Pierce 5, Spear Stab 5, Spiral Pierce 5
	"knight": sk(63, 1, 3, 10, 60, 10, 62, 10, 7, 5, 355, 5, 359, 1, 6, 5, 356, 10, 4, 5, 55, 5, 357, 5, 56, 5, 58, 5, 397, 5),
	// Paladin: Faith 10, Holy Cross 10, Guard 10, Smite 2, Shield Boomerang 5, Shield Chain 5, Endure 5, Gloria Domini 5, Sacrifice 3, Martyr's Reckoning 5, Divine Protection 3, Demon Bane 5, Battle Chant 10
	"crusader": sk(63, 1, 248, 10, 253, 10, 249, 10, 250, 2, 251, 5, 480, 5, 8, 5, 367, 5, 255, 3, 368, 5, 22, 3, 23, 5, 369, 10),
	// High Wizard: Heaven's Drive 5, Storm Gust 10, Mystical Amplification 5, Lord of Vermilion 10, Meteor Storm 10, Mystical Amplification 10, Increase SP Recovery 5, Soul Strike 7, Soul Drain 10, Napalm Beat 5, Napalm Vulcan 5
	"wizard": sk(91, 5, 89, 10, 366, 5, 85, 10, 83, 10, 366, 10, 9, 5, 13, 7, 364, 10, 11, 5, 400, 5),
	// Professor: Earth Spike 5, Heaven's Drive 5, Free Cast 10, Hindsight 1, Double Casting 5, Increase SP Recovery 3, Magic Rod 3, Cast Cancel 5, Dispell 3, Soul Siphon 2, Mind Breaker 5, Indulge 5, Soul Siphon 5
	"sage": sk(90, 5, 91, 5, 278, 10, 279, 1, 482, 5, 9, 3, 276, 3, 275, 5, 289, 3, 375, 2, 402, 5, 373, 5, 375, 5),
	// Sniper: Blitz Beat 5, Steel Crow 10, Arrow Shower 10, Improve Concentration 10, Focused Arrow Strike 5, Falcon Assault 5, Owl's Eye 10, Vulture's Eye 10, Falcon Eyes 10, Wind Walker 5
	"hunter": sk(129, 5, 128, 10, 47, 10, 45, 10, 382, 5, 381, 5, 43, 10, 44, 10, 380, 10, 383, 5),
	// Clown: Music Lessons 10, Unchained Serenade 3, Magic Strings 10, Impressive Riff 10, Melody Strike 1, Vulcan Arrow 10, Song of Lutie 10, Tarot Card of Fate 5, Marionette Control 1
	"bard": sk(315, 10, 317, 3, 321, 10, 320, 10, 316, 1, 394, 10, 322, 10, 489, 5, 396, 1),
	// Gypsy: Dance Lessons 10, Hip Shaker 3, Gypsy's Kiss 10, Focus Ballet 10, Slinging Arrow 1, Vulcan Arrow 10, Lady Luck 10, Tarot Card of Fate 5, Marionette Control 1
	"dancer": sk(323, 10, 325, 3, 330, 10, 327, 10, 324, 1, 394, 10, 329, 10, 489, 5, 396, 1),
	// High Priest: Heal 10, Kyrie Eleison 10, Magnificat 5, Impositio Manus 3, Increase SP Recovery 5, Aspersio 3, Lex Divina 5, Meditatio 10, Angelus 1, Assumptio 5, Gloria 2, Basilica 5
	"priest": sk(28, 10, 73, 10, 74, 5, 66, 3, 9, 5, 68, 3, 76, 5, 363, 10, 33, 1, 361, 5, 75, 2, 362, 5),
	// Champion: Iron Fists 7, Summon Spirit Sphere 5, Raging Trifecta Blow 5, Raging Quadruple Blow 5, Raging Thrust 5, Raging Trifecta Blow 10, Glacier Fist 5, Chain Crush Combo 10, Fury 5, Zen 1, Raging Palm Strike 5
	"monk": sk(259, 7, 261, 5, 263, 5, 272, 5, 273, 5, 263, 10, 371, 5, 372, 10, 270, 5, 401, 1, 370, 5),
	// Whitesmith: Adrenaline Rush 5, Power-Thrust 5, Weapon Perfection 5, Maximum Power Thrust 5, Skin Tempering 3, Hilt Binding 1, Weaponry Research 5, Shattering Strike 10, Pushcart 5, Mammonite 10, Hammer Fall 5, Cart Boost 1, Cart Termination 10
	"blacksmith": sk(111, 5, 113, 5, 112, 5, 486, 5, 109, 3, 105, 1, 107, 5, 384, 10, 39, 5, 42, 10, 110, 5, 387, 1, 485, 10),
	// Creator: Axe Mastery 10, Bomb 5, Acid Terror 5, Acid Demonstration 10, Aid Potion 5, Aid Condensed Potion 5
	"alchemist": sk(226, 10, 229, 5, 230, 5, 490, 10, 231, 5, 478, 5),
	// Assassin Cross: Katar Mastery 10, Sonic Blow 10, Advanced Katar Mastery 5, Envenom 10, Detoxify 1, Enchant Poison 6, Create Deadly Poison 1, Enchant Deadly Poison 5, Double Attack 5, Cloaking 3, Soul Destroyer 5, Righthand Mastery 3, Meteor Assault 5, Soul Destroyer 10, Meteor Assault 10
	"assassin": sk(134, 10, 136, 10, 376, 5, 52, 10, 53, 1, 138, 6, 407, 1, 378, 5, 48, 5, 135, 3, 379, 5, 132, 3, 406, 5, 379, 10, 406, 10),
	// Stalker: Back Stab 10, Sightless Mind 5, Counter Instinct 5, Divest Weapon 5, Divest All 5, Intimidate 10, Preserve 1
	"rogue": sk(212, 10, 214, 5, 390, 5, 215, 5, 476, 5, 225, 10, 475, 1),
}

// isTransSecond: Lord Knight (4008) .. Gypsy (4021) and the mounted variants.
func isTransSecond(class int) bool { return class >= 4008 && class <= 4021 }

// transFor: a plan's trans list. A build variant without its own list takes only the transcendent skills of its main
// plan's list (the main list repeats the main plan's 2nd-job core, which a variant doesn't share).
func transFor(p *Build) []SkillGoal {
	if p == nil {
		return nil
	}
	if t := transSkills[p.ID]; t != nil {
		return t
	}
	var out []SkillGoal
	for _, g := range transSkills[p.base()] {
		if g.ID >= 355 && g.ID <= 407 || g.ID >= 475 && g.ID <= 490 {
			out = append(out, g)
		}
	}
	return out
}
