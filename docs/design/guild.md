# Guild direction, duty, rank and relations (package G)

Branch `feat/guild`. Owner rule (RULES.md 3b): members generally prioritise the guild's direction when it is safe
and they can do it, about 2 hours a day, or risk being kicked. No bounty or payment. Guilds compete on rank and
wealth; leaders are frenemies.

## Files
- `legion/guildduty_logic.go` - pure rules (unit tested): direction fit, day judgement, standing machine,
  strictness, exemptions, rank/wealth scores, join preference, relation drift.
- `legion/guildduty.go` - engine side: snapshot, directions + autopilot, plan checks / guild blocks, ledger,
  warnings, expels, kicked bots' search, block loot to the guild stock, persistence.
- `legion/guildrank.go` - rank, wealth, hourly history, relations table.
- `legion/guildduty_api.go` - endpoints.
- `legion/guildduty_test.go` - tests.
- `tools/legionctl_guild.py` - legionctl commands.

Hook-ins to shared files (one call each):
- `guilds.go` registerGuilds: `registerGuildDuty(mux, bots)` (endpoints; starts the manager on the main instance).
- `guilds.go` guildThink: first branch of the invitation answer: `b.dutyGuildAnswer(name)`.
- `lfg.go` goalStatus: `if g.Kind == "guild" { return b.guildBlockStatus(g, now) }`.
- `ai.go` chooseHuntingGround: `if b.guildBlockHunt() { return }` before the lfg item-goal map pick (CRLF kept).
- `bot.go`: `0x0839` added to the `0x015a, 0x015c` case (see "Expel packet").
- `tuning.go`: six fields, defaults, sanitize lines, doc lines.
- `tools/legionctl.py`: import + one `elif` + help lines. `.gitignore`: the two new runtime files.

## Directions
`{id, guild, kind, item_id, item, map, min_lv, max_lv, classes, note, set_by: leader|autopilot, set, expires,
exclusive}`. Up to 3 in force per guild: leader-set ones first (newest 3 kept; 24 h default, `hours` overrides),
autopilot fills the free slots (none beside an `exclusive` leader direction). Duplicates (kind+item+map) skipped.

Kinds and how a member works them (all through existing checks):
| kind | what | maps | requested items (-> guild stock) |
|---|---|---|---|
| stock / gear | farm an item | `fitFarms(pieceFarms(item))`, else `map` if `soloMapOK` | the item |
| supply | healing drops | `healFarmItems` (herbs/food) like guildstock.go farmers, any safe map | all healFarmItems |
| wealth | farm a sellable item | the item's drop maps, or the member's own `MoneyFarmGoal` (opportunity.go, registers as a chaser) | that item |
| carry | hunt on the low members' map (party logic / Mentor tactic does the carrying) | `map` if `soloMapOKFor(map, true)` | none |
| level | level on a map | `map` if `soloMapOK`, else up to 3 safe guide spots | none |

Autopilot (no AI, every 10 min, expires after 15 min): `supply` when the guild stock row needs healing items;
`stock` on the most valuable non-healing gap of `gstock.gapIDs`; `carry` on the map of the lowest online member
at least 15 levels under the guild's online average (not in town; carriers min_lv = its level + 10); `wealth` as
the fill while fewer than 3. Never `level`.

Basic fit (`dirBasicFit`): not a vendor, not under `beginner_lv` (stays home), level within min/max, class in
`classes` (names or lines: "knight", "archer line"), healers never get farming kinds (carry/level are fine).

## Duty and blocks
- A member owes `guild_duty_min_per_day` (120) minutes per day, but never more than half the minutes it was online
  that day (`dutyOwed`). Days are calendar days in server local time.
- Plan check: every 10 min per member (max 30 checks a minute server-wide) while duty is outstanding, the flag is
  on, it is not exempt, not dead, not on a trip/Kafra/trade/vending, and has no running block.
  - In a party -> "busy" (counts as an excuse check). Otherwise the first direction in order that passes the basic
    fit and has a safe map becomes the block: `b.fgoal = farmGoal{Kind:"guild", Maps, Target: minutes}` with
    minutes = max(30, still owed today), deadline = minutes + 45 min. The previous goal is kept and restored when the
    block ends. A solo "auto" hunt elsewhere is reset so `chooseHuntingGround` -> `guildBlockHunt` picks the block's
    first map still safe (`soloMapOKFor(m, true)`). Town runs (50% weight), save points, fleeing and rests are not
    touched; minutes off the block's maps simply don't count.
  - Nothing fits -> "unfit" (excuse check), with the reason in `last_plan`.
- Each minute on one of the block's maps (alive) = 1 duty minute. Also counted without a block: a member hunting on
  the explicit map of a carry/level direction it fits (incidental duty) - this is counted even with the flag off.
- A block ends: minutes done ("done" - goalStatus "met"), deadline ("expired"), the goal was replaced by something
  else ("replaced"), the flag went off, the member left the guild, or a restart.
- One guild-chat line per guild per 3 min about blocks ("ill farm Elunium for the guild on ..." / "done with my guild
  shift"), through `guildSay` + `styled` (template lines; the exchange branch's chat jobs can wrap them later).
- Loot: every 10 min a member on (or within 1 h after) a block mails the requested items in its bag to the guild
  trader (`guildTrader`) by RODEX, up to 4 stacks, only stacks worth >= 4 x 100z stamp, the same mail flow as
  parcel.go / lootflow.go. Everything else stays with the bot.

## Ledger and standing
- Per member and day: online_min, on_min (online while the flag was on), duty_min, fit/unfit/busy plan checks,
  result. Today + the last 7 days kept; `data/guild_duty.json` (marshalled under the lock, written after; every 5 min).
- A day closes when the member is first seen on a later date (`closeDay`):
  `unplayed` (online < 30 min) | `off` (flag on for less than half its time) | `met` (duty >= owed - 10) |
  `excused` (no plan check ever found a fitting direction: all unfit/busy) | `short`.
- Standing (`nextStanding`): good -> behind -> warned -> kicked. A short day adds 1, a met day takes 1 off (back to
  good at 0; warned stays warned until 0); excused/unplayed/off change nothing. Warning when short days reach
  `warnAt`; kick after `kickAfter` more short days while warned.
  `warnAt = max(1, round(guild_duty_warn_days / strictness))`, `kickAfter = max(1, round(guild_duty_kick_days / strictness))`.
- Strictness by guild style: guild04 Iron Wolves 1.5, guild07 Obsidian Edge 1.5, guild01 Crimson Oath 1.25,
  guild03/05/09 1.0, guild08 Dawnbreakers 0.7, guild02 Silver Lanterns 0.6, guild10 Pudding Patrol 0.6,
  guild06 Kitty Cafe 0.5, Starveil 0.8. With the defaults 3/2: strict 1.5 -> warn after 2, kick after 1 more;
  normal -> 3 and 2; Kitty Cafe -> 6 and 4.
- Exempt (never warned/kicked, standing "exempt"): guild leaders / in-game masters, the guild alchemist, the guild
  trader and every vendor character, Ayla's party (core instance, names in `-exclude` and env `LEGION_CORE`,
  pinned human-party friends, `seedKeep` accounts). No human exemption (gate G25, owner 2026-11-14: a human member is
  owed the same duty, warnings and kicks); the ledger measures only engine-run characters so far, so a human member is
  not yet in it (open work: `STATUS.md`, "Equal treatment of humans: gaps").

## Warnings and kicks (flag on)
- Warning: the guild's leader bot says it in guild chat (one per guild per 5 min, member online); the member answers
  ("sorry, will do") and remembers it.
- Kick: decided at day close, executed 10-60 min later while member and leader are online. Pacing: at most
  `guild_kicks_per_hour` (2) server-wide, 15 min between any two, one per guild per 2 h. The leader says why in guild
  chat, then sends the expel. Confirmed when the member shows no guild; not confirmed in 3 min -> retried 10 min
  later once, then paused 6 h.
- Starveil: Ayla (OpenKore) is its master, so the engine never warns or expels there; the standing still shows in
  the API (warn_pending / kick_pending) for Ayla's mind.

### Expel packet
CZ_REQ_BAN_GUILD `0x015b`: guild id.L, account id.L, char id.L, reason[40] = 54 bytes (recvpackets.txt `015B 54`;
rAthena `clif_parse_GuildExpulsion`, master only). It was not implemented before; `dutyExpelPacket` builds it (test
checks length and layout). For this client (PACKETVER 2021) rAthena announces an expulsion with
ZC_ACK_BAN_GUILD_SSO `0x0839` (66 bytes, name[24] at offset 2, reason[40]) instead of the old `0x015c` the engine
handled, so `0x0839` was added to the bot.go case (an expelled bot clears its guild, onGuildLeft). Confidence:
high for the request layout (packet table + rAthena source layout), medium that the expelled member itself
receives the 0x0839 (rAthena sends it to the guild before removing the member); if it does not, the member's
GuildName stays set until its next login and the kick shows as "not confirmed". **Unverified live.**

## After a kick
`dutyKicked{stage: cooldown -> seeking -> invited -> joined}`. During `guild_kick_cooldown_h` (24) it refuses every
invitation (guildThink hook), and refuses its old guild for 7 days. Then, online and guild-less, it picks a guild:
not its old one, room left (roster count < `guildMaxMembers`), weighted pick among the top 3 by
`joinPreference = 40 x rank share + 30 x wealth share + 20 x friends inside (max 3) + 1`. That guild's leader bot
sends CZ_REQ_JOIN_GUILD (up to 3 invitations 90 s apart; the hook accepts exactly that guild). Not accepted ->
retry in 1 h. Joining any guild on its own also ends the record. It plays independently meanwhile.

## Rank, wealth, relations (always computed, every 10 min; history hourly, 7 days)
- `score = guild level x 10 + average level of the 10 best members (level book, offline included) + sum over its
  members in the server top 100 of (101 - place) / 10`. Places by score, ties by wealth.
- `wealth = market value (priceOf) of everything its vendors list in open shops + funds` (zeny of its leader and
  vendor characters; offline ones keep their last seen purse for 24 h).
- Relations `-1 rival .. +1 ally`, per guild pair: a member moving from A to B within 3 h (friend moves, joining
  after leaving) -0.08; a trade between members at 60-150% of the market price +0.01; a free hand-over +0.02
  (economy.go trade log, scanned every 5 min). Fades 5%/day toward 0. Labels: rival <= -0.5 < cool <= -0.15 <
  neutral < 0.15 <= friendly < 0.5 <= ally. Feeds briefings / chat tone only.
- `data/guild_rank.json` (history + relations), written off-lock.

## Tuning keys (tuning.json)
| key | default | meaning |
|---|---|---|
| `guild_duty_on` | false | blocks, warnings, kicks, kicked bots' search |
| `guild_duty_min_per_day` | 120 | minutes owed per day (max half the time online) |
| `guild_duty_warn_days` | 3 | short days before the warning (/ strictness) |
| `guild_duty_kick_days` | 2 | short days after the warning before the kick (/ strictness) |
| `guild_kicks_per_hour` | 2 | server-wide expel cap |
| `guild_kick_cooldown_h` | 24 | hours a kicked bot refuses invitations |

## Endpoints (main :8090; the core instance answers with empty data, `"instance":"core"`, POSTs 409)
- `GET /api/guild-direction` -> `{"instance":"main","duty_on":false,"guilds":[{"guild":"guild03","name":"Moonlit
  Bazaar","active":[{"id":"guild03-auto-stock-985","kind":"stock","item_id":985,"item":"Elunium","set_by":"autopilot",
  "set":"...","expires":"..."}],"leader_set":[],"autopilot":[...]}]}`
- `GET /api/guild-direction/{guild}` (guild01..10, a guild name, `starveil`) -> one guild view.
- `POST /api/guild-direction/{guild}` `{"kind":"carry","map":"pay_fild04","min_lv":40,"note":"help the lowbies",
  "hours":6}` -> the guild view; `{"clear":true}`; `{"remove":"guild03-L123456"}`. Items by name (`"item":"elu"`,
  fuzzy via the lookup) or `item_id`. The leader bot announces it in guild chat.
- `GET /api/guild-duty[?guild=guild03]` -> `{"duty_on","date","config":{...,"per_guild":{"guild07":{"strictness":1.5,
  "warn_after_short_days":2,"kick_after_warned_short_days":1}}},"standings":{"guild03":{"good":12,"behind":2}},
  "members":[{"char","guild","standing","short_days","exempt","today_duty_min","today_online_min","today_owed_min",
  "block":{...},"last_plan","days":[...]}],"kicked":[...],"kicks_last_hour","counters"}`
- `POST /api/guild-duty/{guild}/pardon` `{"char":"Name"}`; `POST /api/guild-duty/{guild}/kick` `{"char":"Name",
  "why":"..."}` (409 for exempt members and Starveil; queued, paced, needs the flag on to execute).
- `GET /api/guild-rank[?hours=24]` -> `{"rank":[{"guild","name","place","score","wealth","stock_value","funds",
  "guild_level","members","avg_top10_level"}],"history":{"guild01":[...]}}`
- `GET /api/guild-relations` -> `{"relations":[{"a","b","name_a","name_b","value","label","events","last_event"}]}`

legionctl: `guild-direction [g]`, `guild-direction-set <g> <kind> [--item] [--map] [--min-lv] [--max-lv]
[--classes] [--note] [--hours] [--exclusive]`, `guild-direction-clear <g> [--id]`, `guild-duty [g] [--all]`,
`guild-pardon <g> <char>`, `guild-kick <g> <char> [--why]`, `guild-rank [--hours]`, `guild-relations`.

## Switching on and verifying live
1. Baseline with the flag off: after ~20 min `legionctl guild-direction` shows autopilot directions for every guild,
   `guild-duty` shows members with online minutes and results "off", `guild-rank` has rows, nothing changes in play.
2. Set `"guild_duty_on": true` in `legion/data/tuning.json` (re-read within 30 s).
3. Within 10-20 min: `guild-duty --all` shows blocks (`block stock Elunium @... 3/120m`), events
   `guild duty block (... min)`, guild chat lines; the bots go to the block maps (`/api/bots/<name>` goal map).
   Check deaths/kills on those bots stay normal (meter, if merged).
4. Loot: events `guild duty: mailed ... to the guild stock`, counter `deliveries to the guild stock`.
5. Warnings/kicks need closed days, i.e. at least 2-3 days of play. To test sooner, `guild-kick <g> <char>` on a
   non-exempt member: the leader's guild line, then the member shows guild-less (`/api/bots/<name>` guild empty,
   event "was expelled from the guild"), `guild-duty` -> kicked list stage cooldown. If the member keeps its guild,
   the 0x0839 assumption is wrong (see Expel packet) - check the rathena map-server log for the expulsion.
6. Turning the flag off ends every running block at the next minute and stops warnings/kicks; data stays.

## With no AI at all
Autopilot keeps every guild directed (supply/stock from the stock gaps, carry for the lowest member, wealth as the
fill), members take their daily block by themselves, leaders warn and kick by the rules, kicked bots find new
guilds, rank/wealth/relations keep updating. Guild minds only override with leader directions.

## Open risks
- The 0x0839 receipt by the expelled member and the whole expel path are unverified live.
- Requested items might be sold (NPC / shop / #trade) by the bot's normal economy before the 10-minute delivery
  catches them; healing herbs may also go to the brewer through parcel.go first (both end in the guild anyway).
- Party members are always "busy", so members who party all day are excused; with goal parties on, LFG can pull a
  bot on a block into a party (its block then only counts on the block's maps).
- A kicked bot's persona guild (`P.Guild`) is unchanged, so economy code that falls back to it (guildOfAccount,
  parcel targets) still treats it as its old guild while guild-less; the old leader's on-screen recruiting is refused
  for 7 days by the hook.
- Days close at the member's next login; a member offline for days gets them closed in one go (unplayed days don't
  count, so no burst of shorts from absence).
- Tuning struct and legionctl help lines are edited by other branches too: expect small merge conflicts there.
