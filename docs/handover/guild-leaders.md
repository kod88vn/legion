<!-- Moved unchanged from HANDOVER.md lines 814-837 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6d. Guild leaders: always online, levelling first, leaderboard (2026-11-14, `legion/guildlead.go`)
- **One leader per guild, stable** (2026-11-14 fix): `data/guild_leaders.json` (guild id -> persona account) keeps the
  picks across restarts; the in-game guild master (0x016c `isMaster`, `noteGuildMaster`) always wins and swaps with
  the old pick. DB check 2026-11-14: Silver Lanterns master ThePuddinglord (guild02), Obsidian Edge master Sunheart
  (guild07) = engine picks; the other 8 guilds aren't founded yet (leaders found them at Base 15). `/api/bots` gives a
  leader `guild` = the guild it leads + `guild_leader: true`; `legionctl guild` prints a LEADER line; the legion-guild
  skill no longer says "highest-level member is the leader".
- Leaders = `pickGuildLeaders` (guilds.go), for guilds without a saved leader: the 10 most ambitious unpinned, non-`-keep` personas of the main instance,
  one per `guildDefs` entry (`guildLeader(id)`, `b.leaderVision()`). `b.alwaysOnline()` is true for them.
- Always online: bot.go `run` ignores the schedule and online slots for them, zero `sessionEnd` and no hang-out; ai.go
  never starts "calling it a day" and `logoutSignal` never fires; `kickSession` (human-party pins) is a no-op; after
  any disconnect the run loop logs straight back in (counted as `relogs`). They play their `main` character (pickChar).
- Levelling: `leaderPartyManager` (every 60 s) gives an online leader (basic skill >= 7) a tank/priest/ranged party of
  free guild members (in-game roster, then persona guild) within 10 levels, at `bestPartySpot` (strategy.go guide).
  party.go still disbands bot parties after 90 min; the manager re-forms one with a fresh spot a minute later.
  With `goal_parties_on` (lfg.go, see "Need-driven parties"): leaderGoalParty instead - the party forms around the
  leader's goal and ends with it.
- Leaderboard: levels of every Legion char seen online are kept in `data/levels.json` (offline chars keep their place).
  API: `GET /api/leaderboard?limit=N`, `GET /api/guild-leaders[/{guildNN}]`, `POST /api/guild-leaders/{guildNN}/party
  {"map","members"}`; `/api/guilds` and `/api/guilds/detail` gained leader/online/rank fields; dashboard: "Leaderboard"
  card + "[always on · #rank · pt N map]" in the guild table. legionctl: `leader`, `leader-party`, `leaderboard`.
- Before (2026-11-14, by /api/bots level): ranks of 3997 - guild02 #8 (lv28 Mage), guild07 #125, guild01 #656, the
  other 7 leaders lv1 Novices (#746), 6 of them offline by schedule.

