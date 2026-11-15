# Economy, progression and farming

One line per rule: ID, statement, date. Owner words in `archive.md`.

## Progression
- EC01. Every bot starts at level 1 Novice and levels for real with a build, a levelling plan and a gear path to 99. (2026-11-14)
- EC02 / G11. A bot character whose skills or stats do not match its build plan is deleted and restarted at level 1; bot
  accounts only, never humans or Ayla's party (report those). (2026-11-14)
- EC03. Mages and Wizards are AoE builds: see `docs/classes/mage.md` (CL06). (2026-11-14)
- EC04. Bots level rather than march; long trips go by Kafra teleport. (2026-11-13)
- EC06. Beginners stay near their home town, level first, earn money for gear and Kafra travel. (2026-11-13)
- PR33. No back-and-forth Kafra trips: decide once where to go and stay. (2026-11-14)
- EC25. About 90% of online time is spent killing monsters on the hunting ground. Town trips are rare and measured. (2026-11-14)
- EC30. Bots run away from danger; a Fly Wing escape to avoid dying stays on (solo only; in a party PT31 applies). (2026-11-13)

## Town runs and supplies
- EC26. Every bot keeps basic utilities (HP/SP potions, Fly Wings, Butterfly Wings, cures/arrows as its class needs) and uses a
  Butterfly Wing for a town run when affordable. (2026-11-14)
- EC27. Town runs only for a real need (bag at 50% weight, out of potions); gear buying and loot selling happen on those runs, never as a separate reason to drag the party to town.
  Arrows are a necessary supply (CL13). (2026-11-13)
- EC28. 50% weight triggers a town run at once, for every bot and party; the member at 50% weight sells on the next party town run, or goes alone like PT23 if it can't wait. (2026-11-14)
- EC29. SP users carry SP potions as well as HP potions, within the weight limit (stay under 50% weight: regen stops above); healers carry few HP potions and mostly SP
  potions. (2026-11-13)

## Economy design
- EC10 / G09. The economy is the core; bots develop naturally. No items are added to NPC shops (no NPC Blue Potions or Grape
  Juice). Scarce goods come from drops, crafting and player trade. (2026-11-14)
- EC11. Every guild eventually has an Alchemist who brews potions (SP potions included) and sells them. Also CL14. (2026-11-14)
- EC08. Bots farm for their gear or buy it, ask in #trade and trade at a fair price. There must be a real economy. (2026-11-13)
- EC09. Bots prefer farming; they open shops to sell surplus. (2026-11-13)
- EC12. Classes that cannot farm are geared with the farmers' money (alts on one account, or the party leader buys for
  everyone). (2026-11-13)
- EC13. The economy knows which items (weapons, armor, cards, accessories) suit each build, prices them from game data and the
  market, and pushes them to the players who need them. (2026-11-14)
- EC14. Prices follow the server's total wealth and the item's rarity (drop rate x spawns, supply); rare items cost a fortune,
  common items stay cheap. More supply means cheaper, scarcity and many buyers mean dearer; sellers undercut when stock piles up and prices recover as stock sells; unsold items are marked down
  gradually; no fixed price lists. (2026-11-14)
- EC15. Goods are abundant and cheap. Prontera player shops undercut NPC buy prices and pay more than NPC sell prices, so bots
  prefer buying and selling through Prontera; NPCs are the fallback. (2026-11-14)
- EC16. Bots buy from NPCs as little as possible; the Prontera market always holds plenty of gear for every build and level
  band. (2026-11-14)
- EC17. A large share of accounts are solo item farmers, mostly Archer line (DS Hunters, bow Rogues); when not farming they vend
  in Prontera or hand goods to their guild's trader. They are the market's main gear supply. (2026-11-14)
- EC18. Every account with a Merchant-line character also has a DS Hunter, and every account with a Hunter also has a
  Merchant-line character; the Hunter farms, the merchant sells. Missing characters are created at level 1 (bot accounts,
  free slots). (2026-11-14)
- EC19. Items that drop only from bosses cost a whole guild's pooled zeny. Guild leaders plan boss hunts and the build/strategy
  work prepares the strong parties (gear, roles, tactics); a hunt happens only when every member is completely built out with
  high-level gear. (2026-11-14)
- EC20. The build side keeps pushing every bot to its build's next gear step (buy, farm, trade, refine). (2026-11-14)

## Loot and selling
- EC21. The economy steers farmers to maps where sought-after items drop and are farmable at their level; the market trades
  useful items (cards, weapons, gear), not junk. (2026-11-14)
- EC22. Plain gear worth under 100z with no slots, cards or refines is junk: sold to the NPC on town runs, never listed. (2026-11-14)
- EC23. Loot order: use it if it suits the bot's build, then offer to party, then guild, then guild stock, then sell the surplus.
  A bot with no party or guild keeps one copy and sells only duplicates. (2026-11-14)
- EC24. Never dump cards, weapons, armor or valuable materials (ores, gemstones, jewels) at an NPC; NPCs get worthless loot.
  (2026-11-13)

## Market behaviour
- EC31. HP restore items (potions, herbs, fruit) are the market's top priority to stock and sell, ahead of Blue Potions, and herbs go to alchemists for brewing; more bots farm healing drops.
  (2026-11-14)
- EC32. Bots read the Prontera market (what sells, at what price, what buyers want that nobody sells) and farm the gaps, but only if a map safe for their level drops the item. (2026-11-14)
- EC33. Bots say what they find in natural #trade/#global talk, rate-limited to about 10-30 public lines a minute, so other bots and humans hear it and can act on it (farm it, list it, buy it). (2026-11-14)
- EC34. A glut (price near or under NPC value, many sellers) sends bots to farm items that pay better, so the price recovers. (2026-11-14)
- EC37. Junk is judged by value per weight: low-value, heavy items are dropped or left on the ground, and high-value items are kept to sell. This applies to the whole Legion, bots and Ayla's party alike, and cuts town runs. (2026-11-15)
- EC35. Merchants with an open shop occasionally announce its rare items (cards, katana and the like) in #trade so buyers know; natural wording, not spam, within the EC33 rate limit. (2026-11-14)
- EC36. A solo bot, or a party gathering at a Kafra before setting out, uses idle time for errands (sell, store, restock). A party member does errands only on the party town run (PT22/PT24) or at 50% weight (EC28); otherwise it farms near the party (PT18). (2026-11-14)
