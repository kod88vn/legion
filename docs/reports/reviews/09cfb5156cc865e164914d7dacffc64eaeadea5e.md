# Review: fix/town-rest @ 09cfb5156cc865e164914d7dacffc64eaeadea5e (bugs b1794751009271-548, b1794751010971-040)

Scope: regendrop.go, hook in heal.go:271, tests regendrop_test.go / townrest_test.go. `gocheck.sh -run 'Regen|TownRest'`: vet ok, 10 pass.

## Blocker
- regendrop.go:25-44 (EC37, Constitution 2.1): `regenDropPick` ranks every non-equipped, non-potion, non-food item only by `sellPrice/weight`.
  After hasLoot() is false, what is left is exactly what the bot keeps: Cards, valuable Etc (valuableEtc, sell >= 1500), kept/refined/carded/
  unidentified gear (junkGear false), market-demand gear, brew material, wings, gems, quest items. The lowest sell price per weight is
  exactly these:
  - Cards sell for ~5z and weigh 1, so a spare card is dropped first.
  - Buy:0 items (quest/unsellable, `sellPrice` = 0) get ratio 0 and go first.
  - Ammo (arrows, Buy 1 -> sell 0) is dropped first, even for an archer.
  - Gear-path items kept by `keepsGear`/`buildgear` (upgrades, plan items) are not protected.
  - Guild/market goods (marketDemand, guild asks, parcel herbs) are not checked.
  Fix needed: an allow-list of droppable things (junk by npcJunk/junkGear, surplus potions, spare gear copies), never Card, Buy<=0, Ammo of the bot's
  own ammo type, keepsGear/plan items, valuableEtc, market/guild wanted; or store them (storage fee) instead.
- regendrop_test.go:19-21,89-92: the test fixture's "dropped" item `testWeapon1` is a Card (`Heavy Card`), so the test enshrines dropping a card as
  correct. No test for Ammo, Buy 0, kept gear, valuableEtc or a bot with a mix.

## Major
- regendrop.go:36-43 / bag emptying: no stop other than weight < 50%. A bot whose MaxWeight is small relative to an equipped-heavy load (equipment is
  not counted as droppable but weighs) or whose weight is dominated by non-droppable items (equipped, potions, food, which are skipped) still
  gets every other item dropped, in sequence, and then `regenDropPick` returns nil. It can therefore empty the whole bag of everything but potions
  and food, then still be over the line. Needs a cap per visit and a "would dropping reach the line at all" check first.
- regendrop.go:62: `it.Count = 0` locally before the server confirms. If the server refuses (pc_dropitem false: item `nodrop`/untradable flag,
  `cant.drop`, bound items, dead, npc dialog open) rAthena only sends `clif_dropitem(index,0)`; the next inventory list restores the item and the bot
  picks the same item again every 2 s, forever (no failure memory). No loop guard and no per-item cooldown.
- No `tuning.json` flag, so the new behaviour is on by default (reviewer.md step 3). It is an irreversible action (destroys items). Gate it with a flag
  default off, or at least with a conservative item class.
- The dropped stack lies in town; the pickup loop (ai.go:1697 heavyJunk filter) skips only junk, so a dropped card/gear is picked up again by the same or
  another bot when the weight falls under the line. That is the cycle drop -> under line -> pick up -> over line -> drop, bounded only by the sell value.

## Minor
- Packet is correct: rathena `clif_packetdb.hpp:1385` `parseable_packet(0x0363,6,clif_parse_DropItem,2,4)` (index at 2, amount at 4); PACKETVER 20211103
  active block. The handler subtracts 2 from the index (clif.cpp:12044), and `InvItem.Index` is the raw client index (as `useItem` uses), so the
  index is consistent. The list in the file comment also lists the same layout.
- regendrop.go:62-64: a whole stack is dropped in one packet, one stack per 2 s. Pacing is fine for a person tidying a bag; but a player would
  drop in town only after trying the Kafra (store). With under 200z the store fee is the blocker, so this is human enough in principle.
- regendrop_test.go:84-89: the loop's drop counter uses `b.State != ""` and a count-zero check; weak. `b.send` with no connection is silently ok.
- townrest_test.go and regendrop_test.go call the real `lowHPGuard` hook only in `TestRegenPathDrop`; the sell/store/buy tests only call
  `regenLastResort`. Whether the new test fails without the fix: the drop test would (nothing dropped); no test shows the guard keeping valuables.
- Ayla's party: OpenKore is untouched. Legion bots that are party members of Ayla would drop items too (no exclusion).

verdict: do not merge
