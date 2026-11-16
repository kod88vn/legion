# Ayla and her party

One line per rule: ID, statement, date. Owner words in `archive.md`. Hard gates for Ayla (G05, G12, G13, G19, G20) are in
`RULES.md`.

- AY24. Ayla is played by the Legion engine, with the same code and features as every bot; OpenKore is retired for her. She is a test subject: her only extra is a Haiku watcher that studies her and her party closely and has their code improved first, and every improvement goes to the whole Legion (Constitution 2.8). OpenKore-specific lines (SA05/G05, AY05 "guarded commands", AY16, AY17, AY21 "plugin reload") apply only until the switch is live, then read as engine equivalents. (2026-11-15)
- AY25. The owner can override any puppet-master directive for Ayla (farm map, guild direction, party orders and the like) from a dashboard tool. The owner's override wins over the daily puppet-master directive until he clears it or it expires; every override and clear is logged. (2026-11-15)
- AY01. Ayla is Neo: she, her party and her guild Starveil perform better than the rest, measured on the two key metrics against
  comparable bots. (2026-11-14)
- AY02. Ayla has a direct, instant line to the system (`legionctl neo`, which arrives with the puppet package) to say how to improve her own, her party's and her guild's
  performance; the request jumps the bug queue. (2026-11-14)
- AY03 / G19. Ayla and her party are always online. Lessons from them feed the whole Legion (PR16). (2026-11-13)
- SA05 / G05. To inspect Ayla use only `where`, `ai_v`/`aiv` via the logs, or `ai on`. Never send a bare `ai`, `ai off` or `ai manual`
  (a bare `ai` toggles her AI off); `cmdfile.pl` blocks them and the guard is never weakened or tested by sending them. (2026-11-13)
- AY04. Ayla is a true party leader: protects, guides, waits for the party and talks. (2026-11-13)
- AY05. Ayla is controlled by her dedicated Haiku LLM again (owner, 2026-11-14 night): it steers her (map, rally, party commands) through her guarded commands whenever her code gets stuck, crashes or decides badly, keeps her OpenKore running (OpenKore parts: AY24), and files every code gap it covers; her own code still runs underneath (AY19). (replaces the earlier "unstick only" version) A map or rally the helper sets holds like a PT16 plan (about 2 h, changes only for a PT16 reason).
- PT01. Ayla's party follows the normal need-based party rules, except that it never dissolves; its roster is fixed (AY11); her farm map comes from the members' needs in turn. (2026-11-14)
- PT11. Ayla leads the party through each member's next gear piece in turns (farm the drop, or farm zeny and buy), announcing each
  turn; a member's turn lasts about 2 h (PT16). (2026-11-14)
- AY06. Every member (Ayla included) has a gear plan for its build; the party works through the next pieces in turns, buying when
  the party or guild has the money, otherwise farming the piece (where it drops at the party's level and gear) or zeny, so it can take harder maps; a member's turn lasts about 2 h (PT16). (2026-11-14)
- AY07. Ayla's party, like every party, farms the map that best fits its level and its gear (a poorly equipped party farms an easier map than its level suggests; gear counts as much as level); a wanted item is farmed where it
  drops best (rate x spawns) at a fitting level, not on a beginner map. (2026-11-14)
- AY09 / G12. Nothing (her mind, aylagear top-ups, guild funding, gifts, deals) may send away Ayla's zeny if it would leave her under
  10,000z; under 10,000z she sends none and saves up. Under the engine the floor follows the engine party (`engine_parties`), not her name. (2026-11-14, amended 2026-11-15)
- AY23. Ayla collects her group: when a member lags, she waits about 10 s for it to catch up, then goes back to collect it if it hasn't; she does not bounce back and forth. A member on a town run is left to come back on its own. (2026-11-15)
- GD06. The owner's GM account `wander` (Wanderer) joins Ayla's guild Starveil. It is not a party member and does not trade or talk in public. (2026-11-15)
- AY11. The party has five members, Ayla included: two frontlines (melee or tank; Ayla, an Assassin, is one), two ranged, one support: Ayla, Crusader, Hunter, Wizard, Priest (bot characters of a fitting level, run like her other friends; members who leave stay in Starveil as independent bots; a replacement keeps the composition). Earlier rosters of seven (Knight, Monk, Alchemist; AY10) and the briefly ordered four are superseded. (2026-11-15, amended twice 2026-11-15)
- AY26. The party always protects its support: the frontlines stay between the support and the monsters, the ranged keep the support out of harm's way, and no one leaves the support behind. (2026-11-15)
- AY12. Ayla founds the guild Starveil with /guild (no Emperium needed, `guild_emperium_check: no`) and recruits players of every
  job. (2026-11-14)
- AY13. As Starveil's leader Ayla plans resources (zeny, storage, member gear, alchemist potion supply), grows the membership,
  recruits a balanced roster (tanks, healers, AoE casters, ranged, support/crafting), forms parties from her members and keeps
  every member progressing. (2026-11-14)
- AY15. No member rotation in Ayla's party: the rotation experiment is stopped; her party keeps its current members (ayla_rotation_off). (2026-11-15, replaces the rotation rule)
- AY16. Any dialog Ayla's OpenKore opens (NPC talk, storage, deal, mail) is closed as soon as the action is done. (2026-11-14)
- AY17. Ayla runs like a normal bot with no watching client (no Wine client, no VNC window); the owner watches the world with his GM account, and Ayla's OpenKore log is shown in a plain terminal window (OpenKore parts: AY24). (2026-11-14, replaces "the owner watches Ayla live in the TigerVNC window")
- AY18. Ayla has zero downtime: she is never idle or stuck for any length of time; any idle stretch is a bug whose root cause is fixed. (2026-11-14)
- AY19. Ayla has the basic behaviour of a normal bot on her own (pick a fitting map, farm, loot, sell, restock, keep the party moving) with no AI system around; her mind only adds to it, and nothing about her stops when the mind, dispatcher or any agent is down. (2026-11-14)
- AY20. The owner can give Ayla instructions at any time in plain words (for example "do a town run with the party"); her Haiku helper carries them out at once through her guarded commands, ahead of everything else. (2026-11-14)
- AY21. Fixes for Ayla never wait for the 3-hour batch: each deploys as soon as it is built (plugin reload or critical deploy); who does it and the verification are in Constitution 1.4 and 3.5. (2026-11-14, amended 2026-11-15)
- AY22. When the priest is more than 15 cells behind or on another map, Ayla holds up to 10 s fighting what comes near (PT18), then goes back to collect it; she travels at the priest's pace (continues only while it is within 6 cells). For the priest this overrides PT07/PT08. (2026-11-14/15)
