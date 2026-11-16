# AI versus algorithm: what can the code decide instead? (2026-11-15)

Read-only survey. Question from the owner: (a) what else can be done with an algorithm instead of AI, and (b) can the
guild leaders' farming decisions be done by an algorithm? Basis: Constitution 2.7 (code decides), 2.9 (goal: only puppet
masters, once a day) and EC43-EC46 (the market is an algorithm engine that gives information; guild leaders decide on
their own from it). Nothing was started, changed or run. Sources are the repo files named in each section.

## 1. Summary table

Models and intervals are from `tools/puppet/dispatch.py` ROLES (all Haiku). "Engine already does" names the code that exists today.

| # | AI use | Runs | What the AI decides or outputs | Engine already does in code | Verdict | Effort | Proof metric |
|---|---|---|---|---|---|---|---|
| 1 | Guild leaders guild01-10 (puppet) | every 25 min each | up to 3 directions (kind, item, map, level band, classes); pardons; recruiting/rally/tease lines; reads rival rank | `autopilotDirs` + `dutyMaps` + `dutyPlan` (guildduty.go), guildstock.go gaps and farmers, duty ledger, warn and kick, tax (guildtax.go), recruiting (`guildThink`), leader parties (`leaderPartyManager`, `bestPartySpot`), relations (guildrank.go) | **Algorithm with a small AI remainder**: farm choice, duty, pardon, recruiting, tax, party all to code; AI keeps words and personality, once a day | M (about 1-2 weeks, 3 work packages) | guild zeny/h, members' kills/min, duty "good" share, idle share, share of directions set by `autopilot`/`leader_algo` |
| 2 | Market master (puppet) | every 30 min | shop titles and markups 0.9-1.3, #trade lines, farm advice to guilds (`--by market`), answers humans about items | `marketTick` supply/demand factors (market.go), `vendPrice` undercut and markdown, `marketOpps`/`MoneyFarmGoal` (opportunity.go), `progress.py` milestones and `short_gear` | **Replace by algorithm now** (EC46 already says so); keep a once-a-day AI only for vendor voice | S-M | zeny/h by band, shops open, trades, glut and gap counts |
| 3 | Build/class master | daily | reads class files and meter, files bugs, writes `think build` guidance | `buildmilestones.go`, `league.py` cohort gaps to par, `qabrief.py` class digest, bug queue | **Replace by algorithm** (milestones and gaps are already scripts); the AI reads numbers the code already ranks | S | cohort prize (gap x n) falls; bugs filed per day that a fixer closes |
| 4 | Chat director | on chat jobs and human lines | turns a template line into a voice line; replies to humans; writes needs and offers via `chat-intent` | `chatjobs.go` templates (said at once if no agent), `leadertalk.go` leader voice, `chatintent.go` parsing | **Keep AI** for human replies (free-form language); **algorithm** for all bot-to-bot template lines | S (templates), M (keyword intent) | human reply latency, no loss of intent parse rate, chat volume per bot |
| 5 | Ambient life (`legion-life`) | skill loop, owner-started | short natural talk between nearby players | `leadertalk.go`, `idleerrand.go`, chat job templates | **Keep AI, once a day** as a corpus generator; code samples from the corpus | M | share of lines from corpus, no repeat rate, no rule breach |
| 6 | Ayla's Haiku watcher | every 10 min + watchdog events | files code bugs with evidence; (OpenKore era) steers her | `aylawatch.py` snapshots, `partycheck.py`, `meter`, sentinel, watchdog | **Algorithm with a small AI remainder**: detection is already code; the AI only turns evidence into a clear bug text and spots new patterns. Run on events and daily, not every 10 min | S | bugs per day with evidence, Ayla kpm/zph, idle share |
| 7 | Class QA | every 3 h + after deploy | samples 8 bots, files faults | `qabrief.py` `suspicious()`, sentinel (stuck share, farming share, deaths), `league.py` | **Algorithm with a small AI remainder**: stuck, idle, standing-still and far-under-par are plain rules; AI only for "does this play like its class" | S | stuck share, bugs filed per deploy, false positive share |
| 8 | Party watcher (`watcher`) | on `partycheck` violation + 30 min | judges leader silence, plan announced, tanks protecting | `partycheck.py`, `leadertalk.go` | **Algorithm with a small AI remainder** | S | partycheck violation count |
| 9 | Fixer, hard-fixer | per queue item | writes the code fix | n/a | **Keep AI** (Constitution 2.6, 4.1) | none | fix verified by live evidence (3.6) |
| 10 | Reviewer (Sonnet) | per change | correctness and rule conflicts | `gatecheck.py`, `reviewcheck.py`, `constcheck.py` | **Keep AI** (4.2); more of the rule checks can become scripts | none | review findings that later broke live |
| 11 | Analyst-review (Sonnet) | before each 3 h deploy | ranks next batch from league and queue | `league.py`, `bugq.py rank` | **Algorithm with a small AI remainder**: the ranking is already a sort (prize); AI picks fixer vs hard-fixer and the root-cause guess | S | batch size, share of items verified |
| 12 | Deploy agent | per batch | runs scripts | `dep3.sh`, `depcore_nobuild.sh` (they refuse unreviewed fixes) | **Replace by script** when the owner allows (a Haiku agent that only runs scripts is a wrapper) | S | deploys without human fix-up, revert rate |
| 13 | Sentinel, watchdog, aylawatch, partycheck, league | continuous | none: plain code already | all | **Already algorithm**; keep | none | heartbeats (3.8) |
| 14 | Ayla owner orders (AY20) | on owner order | turns plain words into a command | `orders.py` | **Keep AI** (owner's free-form words) | none | order done, bug filed when code failed |
| 15 | Main session | always | listens, assigns | n/a | **Keep AI** (Article 1) | none | n/a |

Short answer to (b): yes. About 80 percent of what a guild leader decides is already code. The farm choice is the one
decision still done by AI that code can fully take over, because the inputs (gear gaps, market gaps, member levels) are
numbers the engine already has.

## 2. Guild leaders in detail

### 2.1 What a leader decides today (brief `tools/puppet/briefs/guild.md`)

| Decision | Who decides today | Detail |
|---|---|---|
| Farm direction (item and map) | AI sets `leader_set`, autopilot fills the rest | `guild-direction-set <guild> gear --item .. --map .. --min-lv .. --max-lv .. --classes ..`; 24 h; up to 3 (`guildDirMax`) |
| Autopilot directions | Code | `autopilotDirs`: supply (heal gap), stock (top item gap), carry (lowest member 15+ levels under average), wealth (fill). Recomputed every 10 min, valid 15 min |
| Where a member farms a direction | Code | `dutyMaps` (safe maps by `fitFarms`, `soloMapOK`, `pieceFarms`, `MoneyFarmGoal`) and `dutyPlan`; map checks are the same ones solo bots use |
| Member duty (2 h/day), warnings, kicks | Code | duty ledger, `nextStanding`, `dutyWarn`, `dutyExpel`; leader brief already says "do not kick by hand; the ledger does it" |
| Pardon | AI (rare) | `guild-pardon` with a real reason |
| Recruiting | Code | `guildThink` (guilds.go): invites party members and unguilded players near the level; `dutyPickGuild` for kicked bots (rank, wealth, friends) |
| Tax and guild skills | Code | guildtax.go (10 percent at every position, spends skill points) |
| Leader's own party and its map | Code | `leaderPartyManager`, `bestPartySpot` (guildlead.go), `partyFitCheck` (party.go) |
| Rank and relations | Code computes, AI reads | guildrank.go `guildRankRound`, `relationsFromTrades`; the AI answers rivals "with words or a fair trade" |
| Talk (guild chat, public, party) | AI | `guild-say`, `say`, `party-say`; the engine already says the mechanical lines (`leadertalk.go`, chat job kinds direction, duty_warning, kick, leader_banter) |
| Loyalty care | AI (vague) | "notice members at risk of leaving" |

So the AI's real decisions are: which direction to set, a rare pardon, the words, and "kindness" to at-risk members.

### 2.2 Which of those an algorithm can take over

1. **Farm direction: yes, fully.** Add a `leaderAlgo` direction round (new file, one-line hook beside `dutyAutopilotRound`),
   set_by `leader_algo` (or reuse `autopilot` with a richer first slot). Scoring function per candidate (item, map):

   `score = need_weight * market_gap * member_fit * safe_share / (1 + chasers)`

   - `need_weight`: share of the guild's online members whose next build piece is that item. Source: guildstock.go
     (`nextBuildPiece` from buildgear.go, member wants) and `buildmilestones.go` gear-gate failures per member.
   - `market_gap`: the market engine's `short_gear` (kind "none" before "short", then demand), the server-progress band,
     and `marketOpps` price x demand (opportunity.go). EC43: this is information only, the leader code reads it and
     decides. Needs the same data in Go that `tools/puppet/progress.py` builds in Python today (port it, or serve it
     from an endpoint).
   - `member_fit`: how many guild members can farm that map safely: reuse `dutyMaps` / `soloMapOK` / `fitFarms` per member, and the level band from member levels (`powerLv`, `targetMaxLv`).
   - `safe_share`: `fitShare`, `recentDeathsFor`, `swarmVeto`, `bossRecent` (all used by `bestPartySpot`).
   - `chasers`: opportunity.go's chase cap, plus other guilds' directions on the same item (frenemy spread: no herding).
   - Personality knob per guild (the 10 styles): a weight vector. 07 weights exp/h and zeny, 03 weights wealth and loot, 05 explorers weight unvisited or rarely used maps, 02 mentors weight carry, 04 weights duty and order, 01 weights rank. This keeps the guilds different without an AI.
   Output: top 1-2 candidates become directions (24 h, replaced only when the score changes by a clear margin, so a guild
   does not flip-flop). Everything else (duty blocks, safety, delivery to guild stock) already runs.
2. **Carry and level directions: yes.** `dirCarry` already exists in autopilot. `dirLevel` (autopilot never sets it) can
   come from `bestPartySpot` run over the guild's lowest and median members.
3. **Pardon: yes, with a rule.** Pardon automatically when the ledger shows the member was in a long town run, was
   dead or in a party that left, or was offline for a stated reason. No AI needed.
4. **Loyalty: yes.** `dutyPickGuild` already scores rank, wealth and friends; reuse the same score to flag at-risk members and have the code send a gift or a template line.
5. **Recruiting, tax, duty, rank, relations: already code.**
6. **What stays for AI:** the words and the personality: banter with a rival, a congratulation that fits the guild's voice,
   a recruiting call that sounds like 09 (flowery) or 10 (goofy), pep talks, a human reply in guild chat. Also the rare
   judgement: a guild that is losing the rank race for a reason the numbers do not show, or a human asking for something
   the code cannot parse. Once a day is enough: one call per leader writes a pool of lines (about 20 per kind: win, loss,
   rally, recruit, tease, thanks) and optionally nudges the personality weights. The engine says them from the pool when
   its events fire (the chat job templates already carry the facts).

Effort: three work packages.
- WP-G1 (S): port the progress and `short_gear` data to a Go read model (or an endpoint the leader code calls).
- WP-G2 (M): the scoring round, with guild weight vectors, hysteresis, `tuning.json` flag `guild_algo_on` default off.
- WP-G3 (S): auto pardon and the line pool. Then run the leaders with the AI off for a day and compare.

Proof (Constitution 3.6): compare, per guild, over 24 h with AI directions versus algorithm directions: guild zeny/h,
members' kills/min, share of members whose duty is "good" or "behind", idle share, deaths, and the share of directions
by `leader_algo`. Also check the algorithm does not herd: no more than N members on one map and no two guilds on the same
item unless the gap is large. Keep the AI leader as a fallback until the numbers are equal or better.

## 3. The other AI uses, in detail

### 3.1 Market master (verdict: replace now)
- Decides today: shop titles and markups, ad lines, farm advice, answers to humans.
- Inputs: `market`, `economy`, `potions`, `exchange`, the "Server progress" block (plain code).
- Already code: price factors rise 4 percent and fall 3 percent per tick with demand and supply, undercut 2 percent,
  markdown of unsold stock, MoneyFarmGoal chasers and caps, the progress and short_gear block. The brief even tells the
  AI to "use only its numbers".
- Replace: markup and glut or gap signals by rule (a glut is price under reference by X percent with supply over 2x demand;
  a gap is kind none or short). Directions to guilds are information only under EC43, so the market just publishes a ranked
  gap list at an endpoint and, for EC44, the server-wide broadcast, which is a template line from a pool without a GM.
- Remainder: vendor titles in the vendor's voice (a pool, written once a day) and replies to humans (chat director).
- Effort S-M. Metric: zeny/h by band, shops open, trades per hour, gap count falling.

### 3.2 Build and class master (replace)
Its brief says it works from numbers the scripts already make: class digest, cohort gaps to par (p75), biggest gap x n.
The output is bugs ("class X lv40-60 solo uses no key skill") and `think build` guidance that the guild leaders read.
`league.py` already ranks cohorts by prize and `buildmilestones.go` already generates milestones. A script can file the
bug for "cohort far below par and fighting share low" and can publish `think build` data as structured fields instead of
prose, which the leader scoring (section 2.2) needs anyway. The AI part left is reading a class file against the game
rules ("class file says X, the game does Y"): run weekly, or as a Sonnet review when a class file changes, not daily.
Effort S. Metric: cohort prize, number of build bugs closed with evidence.

### 3.3 Chat director and ambient life
- Human replies need free-form language: keep AI, but only when a human line was heard (event driven). Rate limit as today.
- Bot-to-bot chat jobs have templates and facts already. The agent only varies the wording. Replace by a line pool per
  kind and per personality, generated once a day by AI (the puppet-master daily call), picked by code with the typing-style
  rules (`leadertalk.go` already varies wording).
- `chat-intent` (need or offer parsed from a human line): a keyword parser with the item-name lookup (`parseIntent` already
  takes a lookup) covers "anyone got X", "selling X for N". AI only for lines the parser fails on.
- Ambient life: keep a daily corpus run. Real players repeat themselves anyway, so a few hundred lines per scene (town
  hangout, party, field) with fill-ins for map, monster and item will do. Metric: lines per hour, repeat rate, and no
  rule breach (no hint of bots or AI, G18).

### 3.4 Ayla's Haiku watcher (does improving code need AI?)
Finding a problem does not need AI: idle, standing still, stuck, party split, death loops, route failure and wrong map
are all measurable (`aylawatch.py`, `partycheck.py`, meter, sentinel, `suspicious()`). The code can file the bug with
the numbers. What AI adds is (a) a clear, root-cause-oriented text and where in the code path to look, and (b)
noticing a new kind of fault no rule covers. So: let a script file the evidence-only bug at once, and wake the AI on a
new or repeating pattern and once a day for a trend review. Writing the actual fix is a fixer's job and stays AI
(Constitution 4.1). Effort S. Metric: Ayla kpm, zph, idle share, bugs filed per day and fixes verified.

### 3.5 Fixers, reviewers, QA, analyst, deploy
- Fixers and reviewers: keep. Writing and judging code is the AI's proper job, and this is where AI improves the bots.
  Reduce reviewer load by moving mechanical checks into scripts (`gatecheck`, `reviewcheck`, `constcheck` exist).
- QA: most of "is this bot healthy" is a rule. Keep AI for class-play judgement after a deploy, on a sample.
- Analyst-review: the ranking is a sort on prize. Keep a small AI step for assigning root-cause hypotheses and for choosing
  fixer or hard-fixer.
- Deploy: already driven by scripts that refuse unreviewed fixes. Candidate for a pure script once the owner agrees. It
  must still respect G15-G17.
- Sentinel, watchdog, aylawatch, partycheck, league, dashboard meters: already plain code. Nothing to replace.

## 4. Recommended order of replacement

Each step goes behind a `tuning.json` flag, default off, is reviewed (4.2), compared with a baseline (3.6), and keeps the
AI as a fallback until the numbers are equal or better. Ayla's party proves it first (2.8).

1. **Market to an algorithm engine (EC46).** It feeds everything below, its logic is mostly built, and its AI cost buys
   the least. Publish the ranked `short_gear` and gap list as data.
2. **Guild leader farm choice (WP-G1, G2).** Highest value: it removes the most frequent AI call (10 leaders x every 25 min
   is 400 calls a day) and decides the thing that moves zeny/h.
3. **Auto pardon, loyalty and the line pool for leaders (WP-G3)**, then cut the leader AI to once a day (words only).
4. **Chat jobs from a pool and keyword intent**; keep the AI for humans.
5. **Build/class master to scripts** that file cohort bugs and publish structured guidance; the AI reviews class files
   only when they change.
6. **QA, party watcher and Ayla's watcher: evidence-first scripts**, AI on a new or repeating pattern and once a day.
7. **Analyst-review ranking and deploy as scripts** when the owner agrees (deploy is also a gate matter, G17).
8. **Last: ambient life as a daily corpus.**

End state matches 2.9: the puppet masters run once a day to write words, the pools and the weight nudges; fixers and
reviewers (system agents) keep the code improving; the watchdog keeps it alive.

## 5. Risks and notes
- Herding. Ten algorithm leaders with the same data will pick the same item. Guild weights, the chase cap and a
  "other guild already on it" penalty are needed, or the market gets a glut (the idea of EC45 spreading holds here too).
- Flip-flop. Hysteresis and a minimum 24 h hold, as `leader_set` has today.
- Equal treatment (G25, 2.2): the algorithm and the pool must treat humans in the guild like everyone, and no line may hint
  at bots or AI.
- Constitution 1.3 and 2.5: the market gives information only. The leader code reads it and decides. It must not be wired
  as a market-set direction.
- The leader's AI inputs and outputs listed here come from the briefs and code. No live call log was available (the
  stack is down), so the call counts are computed from the configured intervals, not measured. Verify real token cost
  from `legion/data/puppet_calls.jsonl` once running.
- Open question for the owner: should the guild leaders' personalities be only words, or also a scoring weight (for
  example 07 cold min-maxers chase exp/h, 03 merchants chase zeny)? This report assumes both.
