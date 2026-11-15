# Market mind restart state (run 13, cycle 10/60 reached)

## Where it stands
- Market feed has been empty (0 open shops) since the last deploy restart; minds API resets connections at times.
- Runs 1-12 done. Run 13 started; cycles 1-10 heartbeats posted, no shop actions possible yet.

## Pending checks once shops reopen
1. `python tools/legionctl.py market`: count OPEN shops; confirm they reopened after the deploy (report to main if not).
2. Healing-first titles: lead with Orange/Yellow Potions, herbs, Apple/Carrot where held; de-lead Red Potion (glut).
3. Gear listings: titles that name gear the vendor holds (Round Buckleer, Chain Mail, Stiletto, Tsurugi, Silver Guard, etc.).
4. Prices: NPC-buyable goods priced under NPC buy price and above NPC sell price (owner rule, new).
5. Creamy Card and Belt: highlight in titles/ads when anyone sells one (top priority item).
6. Guild traders (from `/api/guild-stock`, field `trader`): LilVexsong25 Goods (guild07), Mint Goods (guild02), Rinclaw Goods (guild06), Silentdragon Shop (guild08), Ravenclaw10 Shop (guild04), yukishot Shop (guild01), LilMintfire48Vend (guild03), Mochirose Shop (guild05), Moonblade Bazaar (guild10), LilykunStore (guild09). Give their shops good titles.

## Rules to apply (owner, recorded in RULES.md section 2)
- Healing first; Red Potions glutted; Orange/Yellow/herbs favoured.
- Two-check rule before retitling (stock flips between checks).
- Keep #trade ads sparse (engine posts market talk ~1/min); point buyers to Prontera.
- Heartbeat thought every cycle; report bugs to main via SendMessage with names/time/evidence.

## Known bugs already reported to main
- Failed deals often exceed completed ones.
- Guild trader carts do not reflect guild surplus (yukishot Shop, Silentdragon Shop).
- Shop stock churns between checks; signs change without action.
- Economy counters reset at each deploy.
- Minds API resets/timeouts during deploys.
