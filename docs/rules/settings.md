# Server settings and owner decisions

One line per setting: ID, value, date. Owner words in `archive.md`.

- ST01 / G10. Server caps at 2nd classes: Novice, 1st, 2nd (and Super Novice). No rebirth: no High Novice, no high 1st, no
  transcendent classes; no 3rd or 4th jobs, no Expanded Super Novice. The NPCs are unloaded in
  `rathena/npc-custom/scripts_custom.conf` (needs a rathena recreate to apply; `docs/handover/builds.md` (6f4)). Bots plan builds
  to their 2nd class. Characters already reborn are left as they are. (2026-11-14, rebirth removed 2026-11-15)
- ST04. Base and job EXP rate 5x (live since 2026-11-14 12:23).
- ST05. Drop rates 5x for all items (common, healing, usable, equipment, cards, boss/MVP, treasure) in the rathena config. (2026-11-14)
- ST06. Capacity as is: about 1,024 connections, Legion 960 normal bot slots with headroom for guild leaders, Ayla's party and
  humans. No rebuild. (2026-11-14)
- EC07. All shops are at Prontera's south gate, the market place. (2026-11-13)
- ST02 / G09. The official rAthena Renewal NPC shops (for example the Extended Tool Dealers) stay as shipped; bots use them. Only
  items we would add are off-limits. (2026-11-14)
- ST03. The NPC price of the Butterfly Wing is lowered (300z, `rathena/db-import/item_db.yml`) so bots can afford it; live only
  after a rathena recreate (`docs/handover/startup-old.md` (0.6)). (2026-11-14)
- AY14. The old seed test bot AIBot (bot1) is off: `ro-openkore` runs only under the compose profile `legacy` and is never
  brought back by restarts. (2026-11-14)
- AY12 (setting part). `guild_emperium_check: no` in `rathena/conf/import/battle_conf.txt`; guild name Starveil is the owner's choice. (2026-11-14)
- Guild name of Ayla's guild: Starveil (owner's choice, 2026-11-14).
- ST07. rathena logs chat (party, guild, whisper, global) so the owner's GM can use `@partyspy` and the dashboard can show it; set in `rathena/conf/import/`, kept across recreates. (2026-11-15)
