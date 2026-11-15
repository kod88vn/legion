# Ayla and her party

One line per rule: ID, statement, date. Owner words in `archive.md`. Hard gates for Ayla (G05, G12, G13, G19, G20) are in
`RULES.md`.

- AY01. Ayla is Neo: she, her party and her guild Starveil perform better than the rest, measured on the two key metrics against
  comparable bots. (2026-11-14)
- AY02. Ayla has a direct, instant line to the system (`legionctl neo`, which arrives with the puppet package) to say how to improve her own, her party's and her guild's
  performance; the request jumps the bug queue. (2026-11-14)
- AY03 / G19. Ayla and her party are always online. Lessons from them feed the whole Legion (PR16). (2026-11-13)
- SA05 / G05. To inspect Ayla use only `where`, `ai_v`/`aiv` via the logs, or `ai on`. Never send a bare `ai`, `ai off` or `ai manual`
  (a bare `ai` toggles her AI off); `cmdfile.pl` blocks them and the guard is never weakened or tested by sending them. (2026-11-13)
- AY04. Ayla is a true party leader: protects, guides, waits for the party and talks. (2026-11-13)
- AY05. Ayla's own code makes her decisions (AY19). Her dedicated Haiku helper only gets her unstuck (cancel a stuck rally, respawn off a cell she can't leave) and files the code gap; it does not pick maps or otherwise interfere with her decisions. (2026-11-14, narrowed by the owner)
- PT01. Ayla's party follows the normal need-based party rules; her farm map comes from the members' needs in turn. (2026-11-14)
- PT11. Ayla leads the party through each member's next gear piece in turns (farm the drop, or farm zeny and buy), announcing each
  turn. (2026-11-14)
- AY06. Every member (Ayla included) has a gear plan for its build; the party works through the next pieces in turns, buying when
  the party or guild has the money, otherwise farming the piece (where it drops at the party's level and gear) or zeny, so it can take harder maps. (2026-11-14)
- AY07. Ayla's party, like every party, farms the map that best fits its level and its gear (a poorly equipped party farms an easier map than its level suggests; gear counts as much as level); a wanted item is farmed where it
  drops best (rate x spawns) at a fitting level, not on a beginner map. (2026-11-14)
- AY09 / G12. Nothing (her mind, aylagear top-ups, guild funding, gifts, deals) may send away Ayla's zeny if it would leave her under
  10,000z; under 10,000z she sends none and saves up. (2026-11-14)
- AY11. The party has seven members: Ayla, Priest, Knight, Wizard, Hunter, Alchemist, Crusader (bot characters of a
  fitting level, run like her other friends). Also AY10 (Hunter and Alchemist join). (2026-11-14)
- AY12. Ayla founds the guild Starveil with /guild (no Emperium needed, `guild_emperium_check: no`) and recruits players of every
  job. (2026-11-14)
- AY13. As Starveil's leader Ayla plans resources (zeny, storage, member gear, alchemist potion supply), grows the membership,
  recruits a balanced roster (tanks, healers, AoE casters, ranged, support/crafting), forms parties from her members and keeps
  every member progressing. (2026-11-14)
- AY15. Party members rotate through other jobs to find bugs in other classes; the priest stays. (2026-11-14)
- AY16. Any window the bot opens in the watching client (mail, storage, NPC dialog, shop, trade, guild, party) is closed as soon as
  the action is done. (2026-11-14)
- AY17. Ayla runs like a normal bot with no watching client (no Wine client, no VNC window); the owner watches the world with his GM account, and Ayla's OpenKore log is shown in a plain terminal window. (2026-11-14, replaces "the owner watches Ayla live in the TigerVNC window")
- AY18. Ayla has zero downtime: she is never idle or stuck for any length of time; any idle stretch is a bug whose root cause is fixed. (2026-11-14)
- AY19. Ayla has the basic behaviour of a normal bot on her own (pick a fitting map, farm, loot, sell, restock, keep the party moving) with no AI system around; her mind only adds to it, and nothing about her stops when the mind, dispatcher or any agent is down. (2026-11-14)
- AY20. The owner can give Ayla instructions at any time in plain words (for example "do a town run with the party"); her Haiku helper carries them out at once through her guarded commands, ahead of everything else. The main session still never steers her on its own (G13). (2026-11-14)
- AY21. Fixes for Ayla deploy immediately as soon as they are built (plugin reload or critical deploy); they never wait for the 3-hour batch. (2026-11-14)
