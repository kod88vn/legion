# Ayla mind - restart notes (run 18, saved 2026-11-14 ~16:38 local / 21:38Z)

## Current plan
- Map: **pay_dun03** (Payon cave 3, Sohee; no MVP, 6% aggro). Plan runs **2 hours from ~21:09Z** (rally from the
  Payon Kafra), i.e. until about 23:10Z. Say "for 2 hours" in party lines, not clock times (containers run on UTC).
- At save time the party (all 7) was together in pay_dun01 SE near the dun02 stairs (~245,30), killing skeletons on
  the way down. No deaths on this trip. lockMap = pay_dun03, save point = Payon (saved at its Kafra 21:08Z).
- Change maps only for a real reason (repeated deaths, nothing to kill). Announce the reason, then use
  `ayla.sh rally payon <map>`: a plain `ayla.sh map` does NOT regroup the party.

## Why we are here (map history this run)
- 19:44-20:19Z: pay_dun03 plan. 6 deaths crossing pay_dun01/02, where Ayla pulled about 20 aggro undead.
- 20:20Z: moved to pay_fild10. Mistake: **Eddga (MVP) spawns there** and one-shot Ayla (8.9k Fire Ball) at 20:49Z.
  Never pick an MVP map until the party is boss-ready (RULES 2).
- I checked about 30 maps: nothing MVP-free at lv72-80 is reachable and affordable (Yuno fields take 3 Kafra hops,
  Amatsu/Louyang need the 10k ship, and the party is broke). So the plan went back to pay_dun03.
- Ayla then had 0 Fly Wings because of an items_control bug, so the party held on pay_fild09 until she restocked
  (21:06Z: 20 Fly Wings, 40 Orange, 38 Red). The plugin now keeps wings and pots in her bag.

## Money - OWNER RULE: Ayla keeps at least 10,000z
- Ayla has **5,399z**. While she is under 10k: no gives, top-ups, guild funding or deal payments from her.
- Spent this run: 4.5k to Zen.fox (Staff), 300 to Kimchi.smith45 (Hat), 900 to Echo.0783 (Shoes); aylagear
  auto-topped xXRinfangXx 14.2k (Claire Suits, 20:15Z) and 13.9k (Crossbow, 20:28Z), a double top-up bug
  (reported); and it bought Ayla's Boots (18k, out of turn; reported; Boots go from storage to equipped on the next
  town run).

## Gear turns
- The engine gear pool (/api/party-turn `gear_pool_turn`, /api/bots/<name>.gear_next) decides the turn. Order seen:
  xXRinfangXx (Crossbow 17k, Prontera shop; he got about 28k from Ayla, so he should buy it on a real Prontera run),
  then Silent rogue (Wooden Mail; friends keep mailing him 200-600z in a loop, reported), then Zen.fox (Staff 9.5k,
  Prontera shop; he holds about 3-5k), then Echobunny (Bastard Sword 22.5k), then vex_x18 (Tsurugi/Rapier).
- Ayla owns: Jur [2], Adventurer's Suit, Muffler, Shoes, Cap, plus Boots (in storage) and a Creamy Card (paid 8.7k,
  kept for a Belt [1]). Her next pieces: Katar [1] 41k, Manteau, Belt [1].

## Starveil guild
- Guild level 2, 17-19/22 members, tax 10% on every position, 0 guild skill points.
- Joined this run: Mintshot Goods (Alchemist lv65, merchant/trader), LilGhostgirl (Monk lv75), LilRyuheart
  (Monk lv50; my promise to tell her about a free spot is done, and she is in).
- Low-level members who need carrying: WolfroseHealz lv52, Kimchi.smith45 lv51, xXHolyknightXx lv54.
- `ayla.sh guild-fund` takes donations from party members who are saving for their own gear (it drained Zen.fox);
  reported. Avoid it, or check the donor list first.
- `ginvite` now takes a bare name. After inviting, check the log for "ginvite RESULT for X" before telling the player.

## Bugs reported to main this run
- aylagear bought Ayla's Boots before the members' turns.
- buyAuto hit getRoute errors in payon_in01.
- Ayla answered help calls with "omw" while on a town run (now fixed: real fetch).
- guild-fund takes donors who are saving for their own gear.
- Friends route on their own (via pay_fild01/07) instead of following Ayla.
- Ayla stalled on pay_fild07 while the friends were already on the lockMap.
- Double top-up to Rinfang, and the turn passed on before he bought anything.
- Wooden Mail mail loop.
- Transit through pay_dun01/02 pulls fights.

## Next steps
1. Get the party into pay_dun03 and farm there for the 2-hour plan; stay put.
2. Rebuild Ayla's zeny above 10k (loot sales on real town runs only, at 50% weight or out of pots) before any funding.
3. On the next real Prontera run: Rinfang buys the Crossbow, Zen.fox the Staff if he can afford it.
4. Once Ayla is over 10k, resume gear turns in engine order; keep 10k back.
