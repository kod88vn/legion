<!-- Moved unchanged from HANDOVER.md lines 209-322 (as of commit 63ca923). Index: HANDOVER.md. -->

## 3. How to operate (Windows host + WSL Ubuntu-24.04, Docker runs inside WSL)

- Shell quirks: PowerShell 5.1 and Git Bash are available. In Git Bash use `export MSYS_NO_PATHCONV=1`.
  `wsl.exe -- bash -c '...'` mangles `$`, `$(...)` and awk fields: **write a script file and run
  `wsl -d Ubuntu-24.04 -- bash /mnt/c/.../script.sh`**. Processes started inside one `wsl` call die when it returns,
  so long-lived things (Xvfb, client, keeper) must run as their own long-running/background command.
- **Tune first, deploy second**: many behaviour numbers (party leash, tank pull radius, priest SP thresholds,
  beginner level, crowding, Kafra/gear money rules, chat rates...) live in `legion/data/tuning.json` and are re-read
  every 30 s by both instances - edit the file, no restart (fields: `legion/README.md` "Live tuning", `tuning.go`).
  Check the log line `tuning: <field> old -> new`. Move a constant there when you expect to tune it again.
- **Two Legion instances** (`legion/instances.go`): `legion` (main, ~900 bots, API :8090, dashboard, minds bus) runs
  with `-exclude` the core party; `legion-core` (container `ro-legion-core`, API 127.0.0.1:8091) runs only Ayla's
  friends (`LEGION_CORE`, default `Zendancer84,Echobunny,Zen.fox`) and owns `data/human-parties.json`. :8090 forwards
  `/api/human-party`, `/api/bots/<core member>`, `/api/memory/<core member>` to the core and merges Ayla's party into
  `/api/parties/detail`, so the tools keep using :8090.
- **Bot parties survive restarts** (2026-11-14, `legion/partypersist.go`): the main instance saves every bot-led party
  to `data/parties.json` (on change + every minute) and restores it at startup (members log in on the saved character,
  goals come back once the leader is online, members missing after 10 min are dropped). `botPartyKeeper` keeps the
  in-game party in step (create/invite/leave leftovers). Party names must be letters/digits/spaces only (rAthena
  `char_name_option: 1`): before this fix NO bot party existed in-game ("Name's pt" was refused).
- **Need-driven parties** (2026-11-14, owner rule RULES.md 3 "Parties come from need, not permanence"; `legion/lfg.go`,
  **built, behind `goal_parties_on` in data/tuning.json, default false** = the old permanent grouping keeps running).
  With it on (main instance only; Ayla's human party is never touched):
  - Goals (every bot, re-checked every ~2 min): `gear` (nextBuildPiece dropped on a map safe for the bot: soloMapOK =
    not deadly/too hard/swarm/boss/empty, has room, <= 6 maps away, beginners <= 3 maps from home), `card` (band card
    for a free slot, 60 min max), `zeny` (shop/trade piece: its price + reserve), `level` (+2, +1 at lv70+), `heal`
    (Acolyte-line healers). gear/card are "fixed" (need a drop map), the rest flexible. Solo bots hunt on their fixed
    goal's map (ai.go chooseHuntingGround). Ends: met / expired / changed -> next goal.
  - LFG/LFM (every 20 s, after a 3-min boot pause): free bots post one #global line per 10 min ("LFP payon cave 2,
    farming Ninja Suit, lv52 assassin"), parties without a healer post "LFM priest for ...". Server-wide budget
    `lfg_global_per_min` (default 6). Matching needs one side to have posted: answerer whispers, joins (keeper invites)
    or a new party forms (poster leads; a healer poster joins the best solo farmer). Party wants: healer first (1 per
    party), then Monk, then missing roles; caps tank 2 / ranged 3 / melee 2, 5 non-healers + 1 healer, level band PT35
    (lfg.go partyLvOK/lfgLvFits: spread <= tuning `party_lv_spread`, default 99 = rAthena party_share_level, and the
    party map still gives the newcomer real exp, mapExpPoor; the old band was +-10 around the average),
    partyMapOK with the newcomer (goalMapSafe for an item-goal anchor: safe is enough, exp-poor is fine).
  - Ends: a member whose goal is met and whose next goal is elsewhere leaves ("got my X! gl guys"), a fixed goal not
    served by the party map for 10 min leaves, a healer >12 levels off the party leaves, the leader leaving ends the
    party (endGoalParty), 3 h cap. Restored parties from before (no `need` in parties.json) end 20-70 min after the
    switch-on, staggered. Party.Anchor (item-goal map) is kept by leaderPartyPick / partyFitCheck while safe.
    Every party formed after boot (API/minds too) is a need party.
  - Guild leaders (`leaderGoalParty`, guildlead.go): when not in a party, pick their goal's drop map if safe for the
    group (else bestPartySpot), recruit guild mates whose goals that map serves, announce in guild chat; parties end
    with the leader's goal and re-form a minute later around the next. Leaders never join others' parties via LFG
    but may anchor/post. `leader-party` / `party` (legionctl) still work; those parties also end with their goals.
  - partyManager: no 90-min refresh and no same-map auto grouping while on. chatter.go: no fake LFP lines while on.
  - Measure: `GET /api/lfg[?bot=Name]` (goals now by kind, goals set/met/expired/changed, posts by kind, posts/min,
    joins, parties formed/dissolved by reason, members_left by reason, open posts); `/api/parties/detail` rows have
    `need` + `purpose`. Before (2026-11-14 16:20, old system): 352 bot parties, 865 members, sizes 2:227 3:90 4:34 5:1,
    44% with an Acolyte-line/Monk (37% a healer), median age 67 min (all restored at the last deploy); #global 7.8
    lines/min (all grats/milestones, 0 LFG), #trade 5.4/min. After: TODO once switched on (measure 30-60 min).
  - Switch on: set `"goal_parties_on": true` in data/tuning.json (live-reloaded, no restart). Off again = same file. Switched ON
    2026-11-14 ~11:40 (owner: goal parties for everyone).
  - Live fixes after switch-on: goals were 754 zeny / 0 gear-card (865 of 1,010 next pieces are trade-only) -> card
    goals now come first after a zeny goal and half the time otherwise, and no longer need a free slot (cardGoal);
    the first 19 posts were all "LFM priest" from old healer-less parties -> posts round-robin over priorities.
  - **Incident 2026-11-14 17:00-17:13Z, flag set off 17:13**: a Legion deploy then a rathena recreate ~3 min later
    took every bot offline; partyManager disbanded all ~350 parties (`on < 2`) and the saver wrote the empty set, so
    nothing was restored. The need system then only re-formed 43 parties in 11 min (formation was gated by #global
    posts, 6/min): 80% solo, deaths 5.8/min (41 of 58 solo). Fixes (built 12:30 local): a party with < 2 online is
    kept 10 min (LowSince); parties.json saves that would drop below half of the last count are held 10 min, the old
    file rotates to parties.json.bak1..3 (every 10 min and before a big drop), and the restore falls back to the
    fullest backup < 2 h old when the file holds far fewer parties; restore always logs "restored N of M".
    Matching is now direct (lfgMatch): open parties take fitting free bots, then same-map parties form via local chat,
    then cross-map via whispers (60/tick) - no #global post needed (posts are flavour). Offline cold start
    (TestLfgColdStartSim, 1,000 solo lv20-90 bots, 85% sociable): 29% partied at +20 s, 85% (= every sociable bot) at
    +60 s, 163 parties, 73% with a healer/Monk. Solo bots (flag on) skip party guide spots and maps partyMapTooHard
    for them. Card/gear goals must fit the level band (monster >= lv-20 from lv40) unless valuable (price >= max(20k,
    lv x 400)); goal maps skip crowded and exp-poor maps (goalMapSafeItem / soloMapOKFor), so lv60+ parties no
    longer camp prt_fild08 for a Pupa card. Core goal turns run whatever the flag says.
  - **Ayla's party (core instance, `legion/lfg_human.go`)** - owner: "they should still have need base rule ... they
    function like other parties", "if it's someone else's turn to farm gears, then go". The party stays together; its
    farm map rotates in turns among members' goals (equal time, "party is one mind for gear"). Friends get build goals
    (the priest too: no "heal" goal inside a human party); Ayla's goal is set by her mind. A turn = a member with a
    gear/card drop map safe for the party; it lasts until that goal is met or 45 min, then the member with the fewest
    turn-minutes. The engine does not move Ayla: her mind reads the turn and moves the party (`ayla.sh map NAME`).
    Friends announce turns and met goals in party chat. State in data/party-turns.json (core). Until the mind sets
    a goal, her current lockMap stands for it (kind "hunt").
    Owner rule 2026-11-14 "Farming should target the best map for the party level and equipments": a turn map must
    be safe and not exp-poor for the party (Ayla's level counts); her goal on such a map gives no turn (buy it). With
    no mappable goal the turn map is `best_map` (bestFarmMap: guide spot that suits the weakest member, real exp,
    room, scored by level band + MapZenyHour - distance). goalValuable is now always false: no item justifies an
    exp-poor map for any goal party. Guild leaders only ever lead (leadersOut in formPartyOpt, joinGoalParty refuses,
    formRoleParties / POST /api/party skip them; TestGuildLeadersOnlyLead); a party leader whose goal map isn't the
    party map takes the party there if safe, else levels with it (leaderGoalSync).
    Owner rule 2026-11-14 (RULES.md 5) "they should prioritize getting good gears, either farming or buying ... for
    each member": friends' goals in a human party are gear-first (humanPartyGearGoal: next build piece -> "buy" when
    they can pay, "gear" farm on its drop map (the turn checks the party fits), else "zeny"); Ayla's goal is picked
    from her build's gear table (plan `assassin` by default, `owned` list kept by her mind; "met" adds the item),
    buy when her zeny + the friends' spare zeny cover it. While anyone saves zeny, best_map is the best zeny/h map.
    GET adds `gear` (per member: next, price, source, how buy/farm/zeny/done, map, have, pooled), `gear_pool_turn`
    (gearpool.go), `plan`, `owned`; POST `{"human":"Ayla","owned":[...]}` / `{"plan":"assassin"}`.
    API (main forwards to core): `GET /api/party-turn` -> turn, item, map, why, until, order, served_min,
    human_goal, member_goals, history. `POST /api/party-turn {"human":"Ayla","item":"Soldier Skeleton Card",
    "map":"pay_dun01"}` sets her goal; `{"human":"Ayla","met":true}` when she gets it; `{"human":"Ayla","next":true}`
    ends the current turn. Needs a legion-core deploy (only main batches core deploys).
- **Map server capacity (2026-11-14):** rAthena's map-server is a select() build, MAXCONN = FD_SETSIZE = 1024, so it
  holds ~1013 clients in total (Legion + legion-core + Ayla + humans). Legion keeps `-reserve` (default 40) of
  `-max-online` free for guild leaders/pinned/core/humans, and a guild leader that can't get onto the map bumps one
  ordinary bot (legion/leaderroom.go). Going past ~1000 online needs a rathena rebuild with epoll and a bigger MAXCONN
  (owner decision: image rebuild + rathena restart).
- Deploy main Legion: `cd /mnt/g/RO/legion && docker compose build legion && docker compose up -d legion` (restarts
  ~900 bots for ~1 min; **Ayla's party stays online**). Vet first: `docker run --rm -v /mnt/g/RO/legion/legion:/src -w /src golang:1.22 sh -c 'gofmt -l -w . && go vet ./...'`.
- Deploy core: `docker compose up -d legion-core` (after the build above; same image). **Only when party-relevant code
  changed** (party.go, partyroles.go, humanparty.go, ai.go fighting/healing, ...) and you want Ayla's friends on it -
  it logs the three out for ~1 min. Otherwise the core keeps running the older build, which is fine.
- Rotating a member of Ayla's party: automatic since AY15 (`legion/aylarotate.go`, tuning `ayla_rotation_hours`
  default 3, `ayla_rotation_off`): one non-priest, non-party-leader member every N hours is swapped for an online bot
  of the same role and an untested class/build; the accounts move between the instances live through
  `data/instance_handoff.json` (core writes) + `data/instance_handoff_main.json` (main writes) - no container recreate,
  no `.env` edit (both instances apply the overrides at startup too). Log: `data/ayla_rotation.json`; candidates:
  `data/rotation_candidates.json` (main, every 2 min); each swap is an `INFO AY15 rotation` item in the bug queue.
  Manual rotation (old way): change `LEGION_CORE` in `.env` (both services read it - they must match, or a bot is
  double-logged or orphaned), update `data/human-parties.json`, then `up -d legion-core legion`. Tools that read
  `LEGION_CORE` (sentinel.py, progress_report.py, watch/*, wrongbuilds.py PARTY) do not follow live rotations.
- rAthena config / NPC changes need `docker compose restart rathena` (kicks everyone, including Ayla; she reconnects).
- DB: `docker exec -i ro-db mariadb -uragnarok -pragnarok ragnarok`. Bot accounts `lg0001..` (password `legion`),
  account ids 2100001-2199999. Owner account `player`/`player` (id 2000000, group 99) holds Admin, kitty (200001, Doram)
  and Ayla (150010). Re-seeding: `docker compose run --rm legion seed ...` (see legion/README.md; always `-keep` the party).
- Dashboard: <http://127.0.0.1:8090/dashboard>. Health JSON: `/api/overview`. Parties: `/api/parties/detail`.
  Guilds: `/api/guilds/detail`. Minds bus: `/api/minds...` (see AGENTS.md).

---------------------------------------------------------------------------------------------------------------------
