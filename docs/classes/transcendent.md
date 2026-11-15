# Transcendent classes

General class rules: `all.md`. Owner words: `docs/rules/archive.md`.

**Not available on this server (ST01 / G10, owner 2026-11-15: "change the server to not allow any rebirth").**
No character can be reborn: the Valkyrie rebirth NPC and the paid Book of Ymir entry (1,285,000z) are gone. The stock
`npc/jobs/valkyrie.txt` is unloaded and replaced by `rathena/npc-custom/valkyrie_norebirth.txt` (the way into Valhalla
for reborn characters stays), and the Legion's class script no longer rebirths a 99/50 2nd job
(`rathena/npc-custom/legion.txt`). No bot plans or seeks a transcendent class: every build ends
at its 2nd job (`legion/builds.go`, `planForClass`). The section below is the data for characters that were reborn
before ST01: they keep their class and their trans skills (`legion/transplans.go`). Nothing here is a new target.

Transcendent classes (the server cap; 2026-11-14, rebirth removed 2026-11-15)
- CL12 (see ST01, G10, settings.md). The transcendent 2nd job is the last class on the server, for everyone. The 3rd-job, Expanded Super Novice
  and 4th-job quest NPCs are unloaded (`rathena/npc-custom/scripts_custom.conf`, `docs/handover/builds.md` (6f4)). Never plan or advise 3rd jobs. (2026-11-14)
- **Path (closed 2026-11-15; kept for characters reborn before):** 2nd job at base 99 / job 50 with 1,285,000z -> rebirth (High Novice, base 1, 100 status points; legion.txt
  stands in for the Sage Castle / Valkyrie quest at the stock price) -> High 1st job at job 10 -> trans 2nd job at
  job 40 (same branch). Trans caps: base 99, job 70 (69 skill points: ~20 more than the 2nd job, for trans skills).
- **Stats:** the same plan as the 2nd job, from scratch (stat cap stays 99).
- **Skills** (`legion/transplans.go`; the High 1st job follows Build.Skills like the 1st job did; once trans, these come
  first, then the rest of Build.Skills). Prefix-compatible: Build.Skills is untouched.
  - Lord Knight: Two-Hand Sword Mastery 10, Two-Hand Quicken 10, Bowling Bash 10, Magnum Break 5, Aura Blade 5, Frenzy
    (Berserk) 1, Provoke 5, Parrying 10, HP Recovery 5, Riding 1, Spear Mastery 5, Concentration 5, then Pierce 5 / Spear
    Stab 5 / Spiral Pierce 5 (spear option).
  - Paladin: Faith 10, Holy Cross 10, Guard 10, Smite 2, Shield Boomerang 5, Shield Chain 5, Endure 5, Gloria Domini
    (Pressure) 5, Devotion 3, Martyr's Reckoning 5, Divine Protection 3, Demon Bane 5, Battle Chant (Gospel) 10.
  - High Wizard: Heaven's Drive 5, Storm Gust 10, Mystical Amplification 5, Lord of Vermilion 10, Meteor Storm 10,
    Mystical Amplification 10, SP Recovery 5, Soul Strike 7, Soul Drain 10, Napalm Beat 5, Napalm Vulcan 5.
  - Professor: Earth Spike 5, Heaven's Drive 5, Free Cast 10, Hindsight 1 (prerequisite), Double Casting 5, SP Recovery 3,
    Magic Rod 3, Cast Cancel 5, Dispell 3, Soul Siphon 2, Mind Breaker 5, Indulge 5, Soul Siphon 5.
  - Sniper: Blitz Beat 5, Steel Crow 10, Arrow Shower 10, Improve Concentration 10, Focused Arrow Strike (Sharp
    Shooting) 5, Falcon Assault 5, Owl's Eye 10, Vulture's Eye 10, Falcon Eyes (True Sight) 10, Wind Walker 5.
  - Clown / Gypsy: Music / Dance Lessons 10, Dissonance / Ugly Dance 3, Bragi / Service for You 10, Assassin Cross /
    Humming 10, Musical Strike / Throw Arrow 1, Vulcan Arrow 10, Apple of Idun / Fortune's Kiss 10, Tarot Card 5,
    Marionette Control 1.
  - High Priest: Heal 10, Kyrie 10, Magnificat 5, Impositio 3, SP Recovery 5, Aspersio 3, Lex Divina 5, Meditatio 10,
    Angelus 1, Assumptio 5, Gloria 2, Basilica 5.
  - Champion: Iron Fists 7, Spirit Spheres 5, Triple Attack 5, Chain Combo 5, Combo Finish 5, Triple Attack 10, Glacier
    Fist (Tiger Fist) 5, Chain Crush Combo 10, Fury 5, Zen 1, Raging Palm Strike 5.
  - Whitesmith: Adrenaline Rush 5, Power-Thrust 5, Weapon Perfection 5, Maximum Power Thrust 5, Skin Tempering 3, Hilt
    Binding 1, Weaponry Research 5, Shattering Strike (Meltdown) 10, Pushcart 5, Mammonite 10, Hammer Fall 5, Cart Boost
    1, Cart Termination 10 (Cart Boost needs the quest skills Cart Revolution + Change Cart: no quest-skill errand yet,
    so it stays unlearned until one exists).
  - Creator: Axe Mastery 10, Bomb 5, Acid Terror 5, Acid Demonstration 10, Aid Potion 5, Aid Condensed Potion 5.
  - Assassin Cross: Katar Mastery 10, Sonic Blow 10, Advanced Katar Mastery 5, Envenom 10, Detoxify 1, Enchant Poison 6,
    Create Deadly Poison 1, Enchant Deadly Poison 5, Double Attack 5, Cloaking 3, Soul Destroyer 5, Right Hand Mastery
    3, Meteor Assault 5, Soul Destroyer 10, Meteor Assault 10.
  - Stalker: Back Stab 10, Raid 5, Counter Instinct (Reject Sword) 5, Divest Weapon 5, Full Strip (Divest All) 5,
    Plagiarism 10, Preserve 1.
- **Skill use (run 4, `legion/transfight.go`, built 2026-11-14):** Lord Knight Aura Blade (key buff, 30% SP floor) and
  Frenzy (only vs a boss / champion, HP >= 90%, a party healer within 12 cells); High Wizard Mystical Amplification
  right before an area spell (3+ grouped around the target); Professor Double Casting kept up above 50% SP; Assassin
  Cross Soul Destroyer first in the Thief attack list (range 4); High Priest Assumptio on the Kyrie targets (tank
  first, party.go). Not wired yet: Acid Demonstration, Vulcan Arrow, Meteor Assault, Falcon Assault / Focused Arrow
  Strike, Raging Palm / Glacier Fist, Cart Termination, Full Strip, Gospel / Basilica. No bot near rebirth (max lv 80).

