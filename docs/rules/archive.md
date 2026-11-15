# Archive: the owner's original words, keyed by rule ID

Never auto-loaded. Each entry is the full text of the old rule as it stood in `RULES.md` or `CLASSES.md` at commit
63ca923 (the owner's quotes are inside, with their dates). The short current statement of each rule is in the file
named in `coverage.md`. Rules marked replaced live only here.

## PR01 - Short rules, no repeated prompts

Kind: process. First date: 2026-11-14.

```text
**Short rules, no repeated prompts** (2026-11-14): the owner wants the general rules trimmed and separated where it
makes sense, without losing any hard gate, and without repeating the owner's original prompt in the rules (it wastes
tokens). New rules are written as short plain statements; this file is to be trimmed the same way.
```

## PR02 - Recording rule for new rules

Kind: process. First date: 2026-11-14.

```text
**Recording rule:** when the owner states something that is a rule (a "must", "always", "never", "should", a general
behaviour for bots, "apply to all ..."), add it to this file in the same turn, under the right heading, with the
date. Don't wait to be asked. If a new rule replaces an old one, edit the old line instead of keeping both.
Class- or build-specific owner rules go in `CLASSES.md` (2026-11-14: "there should be a separate file for build/class
specific instructions"); general party, economy and safety rules stay here.
```

## PR03 - Goal: Matrix-like simulation, judge by normal human behaviour

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The overall goal: a Matrix-like simulation of real people** (2026-11-14, owner: "I need you to remember the overall
  goal, a simulation like Matrix. So make judgement accordingly to simulate normal human behaviors"): every design
  choice is judged by "is this what real players / a normal society would do?". When a rule doesn't cover a case, pick
  the most natural human behaviour (no mechanical shortcuts, no unnatural mass actions, believable pacing).
```

## PR04 - Society model: routines, roles from leaders and market

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The society model** (2026-11-14, owner: "Like most human society, most people are just doing their routines, eat,
  sleep, work. Their roles are actually decided by society leaders and market demands"): most bots just run their
  routine (the engine). Roles come from the guild leaders and from market demand. The economy drives everything by
  supply and demand; demand comes from each bot's need to reach its ultimate build, so the class engine controls how
  bots choose builds to supply variety and market demand. Guild leaders take care of their members. Ayla's party is
  the test system, with a dedicated agent observing it and reporting bugs. A communication system carries the needs.
```

## PR05 - Token balance; most agents Haiku

Kind: process. First date: 2026-11-14.

```text
- **Token balance: the world mostly runs on its own** (2026-11-14, owner: "I need to save tokens so figure out a
  balance setup so things mostly run fine on its own, the sentinels report bugs to the main thread to assign fix to
  sub agents to build"): LLM tokens are spent only where judgement or words are needed; sentinels (scripts) find
  bugs and report them to the main thread, which assigns fixes to subagents.
  Most AI agents run on Haiku (2026-11-14, owner: "I want token efficent, Most of the ai agents are haiku"); the
  model for the main session is still to be decided by the owner.
```

## PR06 - Agents improve over time, measured by metrics

Kind: process. First date: 2026-11-14.

```text
- **Agents improve over time, measured by metrics** (2026-11-14, owner: "I also want the agents to improve over time
  by measuring its success via metrics: economy growing, bots are productive, no issues, less stuck, less death"):
  the self-learning loop is judged by numbers (economy growth, bot productivity, open issues, stuck rate, death
  rate...); every agent and every fix is measured against them and adjusted when the numbers don't improve.
```

## PR07 - Two key metrics per bot: kills/minute, zeny/hour

Kind: process. First date: 2026-11-14.

```text
- **The two key metrics per bot: kills per minute and zeny per hour** (2026-11-14, owner: "The biggest and most
  important metrics to improve for each bot is kill/minutes, and zennies per hour. System improvement should focus on
  improving bot's performance at those 2 metrics"): system improvement work focuses on raising these two numbers.
```

## PR08 - The system runs without AI agents

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The whole system keeps running without AI agents** (2026-11-14, owner: "You are right the entire system should
  keep running without AI agents"): the game world (bots, economy, guilds, chat) keeps working when no LLM agent is
  running. AI adds direction and words on top; it is never required for the world to function.
```

## PR09 - Every bot has one goal and works toward it without AI

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every bot has one goal and works toward it without any AI** (2026-11-14, owner: "Every bot in the legion has 1 goal
  and should be able to work toward that goal without any AI, then the puppet masters influence AI to prioritize where
  to farm, etc..."): each bot pursues its goal (its ultimate build) by itself through the engine; the puppet masters
  only influence its priorities (where to farm, what to prioritise) - they do not drive it.
```

## PR10 - Two classes of agents: puppet masters, system agents

Kind: process. First date: 2026-11-14.

```text
- **Two classes of agents** (2026-11-14, owner: "I want to separate 2 classes of agents"):
  1. **Puppet masters** direct the game mechanics: the build/class agent, the leader agents (guild leaders) and the
     economy/market agent.
  2. **System agents** make sure everything is running and watch for bugs and improvements.
```

## PR11 - Main session listens and assigns; no coding; all work by subagents

Kind: hard gate. First date: 2026-11-14.

```text
- **The main session listens and assigns** (2026-11-14, owner: "Main session mainly listens to my rant and steering
  and bug report from other agents then assign tasks to fixer"): the main session listens to the owner (rants,
  steering) and to bug reports from the other agents, then assigns tasks to fixers. It does no coding itself.
- **Main priority: Ayla's party running well** (2026-11-13). **ALL work (coding, fixes, deploys) is done by
  subagents** (2026-11-13: "All the work must be done by a sub agent"). The main session's role is the rule "The main
  session listens and assigns" above (2026-11-14; it replaces "keep yourself occupied for 1 thing only, observe the
  party" - watching the party is the dedicated party watcher's job).
```

## PR12 - Puppet masters run as own agents outside main session

Kind: process. First date: 2026-11-14.

```text
- **Puppet masters run as their own agents, outside the main session** (2026-11-14, owner: "yes, it's another
  agent").
```

## PR13 - Harness lives in git folder legion

Kind: process. First date: 2026-11-14.

```text
- **The AI harness lives in git, in its own folder** (2026-11-14, owner: "we will need git, create a new folder
  (legion), move all AI harness there, check in first commit before we start"): before the redesign work starts, the
  AI harness is moved into a new folder named `legion`, put under git, and a first commit is made.
```

## PR14 - Standing setup checklist (loops, 15 minds, standing agents)

Kind: replaced. First date: 2026-11-14.

```text
- **Standing setup checklist - ON HOLD since 2026-11-14 (redesign): start nothing without the owner's word; see
  `STATUS.md`.** Old text, kept until the rules are trimmed: (2026-11-14: "write it down in a way that next time we start, everything is back up the
  same way it is right now"). At every session start (and whenever one stops) the main thread brings back, exactly one
  of each, as described in `HANDOVER.md` section 0 (`/legion-session` does it):
  - background loops: `tools/sentinel.py`, `tools/unwall.py --loop`, `tools/rogui/ai-display.sh` (VIEW_SCALE 1.39,
    never fullscreen), `tools/rogui/ai-client.sh`;
  - LLM minds: ayla (Sonnet), chat, life, market, guild01-05, guild06-10, watchdog (Haiku, explicit cycle counts);
  - Ayla party watcher, class-QA agent, build-and-strategy agent, economy engineer; Ayla-plugin engineer, fixers (build
    only) and one deploy agent per batch as needed. All report to main; main resumes them with SendMessage, never
    spawns duplicates.
```

## PR15 - Agent system maintained at all times (heartbeats, watchdog)

Kind: replaced. First date: 2026-11-14.

```text
- **The agent system is maintained at all times** (2026-11-14, owner: "the agent system must be maintained, the
  watchdog keeps everything running. You failed at your task"): every standing agent (minds, party watcher, class QA,
  build-and-strategy, economy engineer, deploy agent, watchdog) posts a heartbeat thought on the minds bus each cycle
  (`legionctl think <role> ...`; roles watcher, qa, build, econ, deploy, watchdog); the watchdog checks `legionctl minds`
  every cycle and reports any standing role silent for >10 min to main at once; main restarts it immediately and also
  checks its agent list (ListAgents) every few turns. A stopped standing agent is a failure, not a detail.
```

## PR16 - Lessons from Ayla's party apply to the whole Legion

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Everything learned from Ayla's party is applied to the whole Legion** (2026-11-13: "lessons learnt from this
  party are incorporated back to the legion", "Apply that to all tanks in the legion", "apply the fix to other
  parties as a general rule", "make sure rules apply to all legion bots"). A fix is never only for Ayla's party: it
  goes in the shared engine code, and it is verified on bots outside her party too.
```

## PR17 - Observe, find problems, fix them; classprobe after changes

Kind: process. First date: undated (2026-11-13 or earlier).

```text
- **Observe, find problems and fix them yourself**; don't wait for the owner to report them ("I do not want to sit
  and baby sit you", "Observe, find things to fix and fix"). Keep `tools/sentinel.py` and the watchdog mind running,
  act on what they report, and probe classes (`tools/legion/classprobe.py`) after every change.
```

## PR18 - A dedicated agent watches Ayla's party and reports bugs

Kind: process. First date: 2026-11-14.

```text
- **A party-watcher agent watches Ayla's party all the time** (2026-11-14: "spin up another agent to watch the party
  and report any issue. If they are not behaving correctly it should report back to you"): it checks the party's
  behaviour against these rules every minute or two and reports every misbehaviour to the main thread, which hands
  it to a fixer. Restart it whenever it stops.
```

## PR19 - Class-QA agent always on

Kind: replaced. First date: 2026-11-14.

```text
- **A class-QA agent runs all the time** (2026-11-14): it keeps sampling one member of every class (and Ayla's party),
  spots what's wrong (not attacking, standing still, looping, bad gear, pointless trips...) and reports each bug to the
  main thread, which hands it to a fixing subagent. Restart it whenever it stops.
```

## PR20 - Build-and-strategy agent always on

Kind: replaced. First date: 2026-11-14.

```text
- **A build-and-strategy agent runs all the time** (2026-11-14: "these build and spell use are common sense. I need
  another agent to check on build and strategy of bots and improve according to online guide for that class"): it
  goes through every class, compares the bots' stat/skill builds, skill use in fights and hunting strategy with good
  online guides for that class (Renewal), and improves the engine so each class plays the way a sensible player would.
```

## PR21 - CLASSES.md is the build reference

Kind: process. First date: 2026-11-14.

```text
- **`CLASSES.md` is the reference for builds** (2026-11-14): one section per class line (role, stats, skill plan,
  skill use, gear path, special items, boss role, do/don't) holding every class/build rule. The build-and-strategy
  agent keeps it current, adding its per-class notes as it reviews each class.
```

## PR22 - Keep highlighting owner decisions

Kind: process. First date: 2026-11-14.

```text
- **Keep highlighting what needs the owner's decision** (2026-11-14: "I want you to keep highlighting issues that
  need my decision or guidance"): the main thread keeps an up-to-date list of open questions only the owner can
  answer (trade-offs, rule conflicts, server config changes, assumptions made on the owner's behalf) and raises them
  clearly in its updates, under their own heading, until answered. Don't bury them in status reports.
```

## PR23 - Main thread never controls Ayla

Kind: hard gate. First date: 2026-11-14.

```text
- **The main thread never controls Ayla** (2026-11-14, owner: "stop interfering. I said you observe and fix bugs, I never
  said you control ayla. When I ask a question, I want you to find and fix the bugs, not sending ayla around"): no map
  orders, rallies, party lines or moves sent to Ayla from the main thread. Ayla is led by her own mind/plugin. When the
  owner reports a problem, the main thread finds the root-cause bug and has it fixed - it does not work around it by
  steering her.
```

## PR24 - Minds report bugs to main

Kind: process. First date: 2026-11-14.

```text
- **The minds report bugs** (2026-11-14, owner: "the agent that is running bots should also report bugs back to you since
  they are the one that encounter bugs first"): every mind (ayla, chat, life, market, guild minds) that sees a bot
  misbehave (stuck, looping, wrong map, unpaid deal, ignored help call, broken command/API...) sends main a short bug
  report with names, map, time and evidence right away, then carries on; main hands it to a fixer.
```

## PR25 - Self-improving loop

Kind: process. First date: undated (2026-11-13 or earlier).

```text
- **Self-improving loop**: observe -> wake -> fix the cause -> record the lesson in `legion/data/lessons.md` -> observe.
```

## PR26 - Main priority: Ayla's party running well

Kind: process. First date: 2026-11-13.

```text
- **Main priority: Ayla's party running well** (2026-11-13). **ALL work (coding, fixes, deploys) is done by
  subagents** (2026-11-13: "All the work must be done by a sub agent"). The main session's role is the rule "The main
  session listens and assigns" above (2026-11-14; it replaces "keep yourself occupied for 1 thing only, observe the
  party" - watching the party is the dedicated party watcher's job).
```

## PR27 - If something keeps failing, investigate

Kind: process. First date: undated (2026-11-13 or earlier).

```text
- **If something keeps failing, stop retrying and investigate** the root cause.
```

## PR28 - Measure before claiming

Kind: process. First date: undated (2026-11-13 or earlier).

```text
- **Measure before claiming**: say a fix works only after data shows it (probe, logs, screenshots).
```

## PR29 - Deploys never recreate rathena by accident; Ayla follows rathena

Kind: hard gate. First date: 2026-11-14.

```text
- **Deploys never recreate rathena by accident; Ayla follows rathena** (2026-11-14, after a deploy recreated
  ro-rathena and left Ayla offline): Legion deploys touch only legion/legion-core (`--no-deps`). Whenever ro-rathena
  is recreated, ro-openkore-ayla (and the legion containers) are recreated right after it, because they share its network.
  Ayla must always be online.
```

## PR30 - Rathena restart at most once an hour

Kind: hard gate. First date: 2026-11-14.

```text
- **Restart rathena sparingly: at most once an hour** (2026-11-14: "Restart also sparingly. Like every 1 hour at
  most"), e.g. to refill depleted spawns or apply server config; each restart takes Ayla offline briefly.
```

## PR31 - Deploy agent may stop stray build/test containers

Kind: hard gate. First date: 2026-11-14.

```text
- **The deploy agent may stop stray build/test containers** (2026-11-14, owner: "yes"): only the deploy agent runs go
  vet/test and image builds; fixers hand it "code ready". It may `docker stop`/`docker kill` stray golang test or
  build containers that block a deploy (never rathena, Ayla, legion or the db).
```

## PR32 - Deploys every 3 hours (critical excepted); core deploys too

Kind: hard gate. First date: 2026-11-14.

```text
- **Batch deploys: every 3 hours** (2026-11-14, owner: "every deployment is 3 hours now"; replaces "at most every
  30 minutes" of 2026-11-13): a Legion deploy logs out every bot and disrupts the owner's party. Collect changes and
  deploy every 3 hours, unless a fix is critical to unblock something.
  The same goes for `legion-core` (Ayla's friends): every core restart logs them out and breaks up her party, so only
  the main thread batches core deploys; working subagents report "built" instead of deploying (2026-11-14).
```

## PR33 - Do not waste money on travel

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Don't waste money on travel**: no back-and-forth Kafra trips; decide once where to go and stay.
```

## PR34 - LLM minds are the core, always alive (mindgate)

Kind: replaced. First date: 2026-11-14.

```text
- **The LLM minds** (ayla, chat, life, market, guild01-10 + a watchdog) were "the core of the server", all kept
  alive at all times, enforced by the hook `tools/mindgate.py`. **Superseded 2026-11-14** by "The whole system keeps
  running without AI agents" and "Two classes of agents" above; the hook is changed as part of that redesign.
```

## PR35 - Write everything down in HANDOVER.md

Kind: process. First date: undated (2026-11-13 or earlier).

```text
- Write everything down for the next AI (`HANDOVER.md`), and keep this file current.
```

## PR36 - System agents: scripts watch, agents woken by violations

Kind: process. First date: undated (2026-11-13 or earlier).

```text
(no old text: written for the redesign from `docs/design/REDESIGN.md` sections 1 and 3S; the owner's direction: scripts watch, agents are woken by violations)
```

## SA01 - Never drive a client with the real mouse or keyboard

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Never drive any client with the real mouse or keyboard ("it keeps clicking the wrong thing and I cannot use my
  computer"). Only the Wine client on the virtual display :98, or OpenKore.
```

## SA02 - No fullscreen for the owner's client

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- No fullscreen for the owner's client.
```

## SA03 - No GM account for watching

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- No GM account for watching; the owner watches Ayla directly.
```

## SA04 - GM test account allowed; stays out of economy, parties, public chat

Kind: hard gate. First date: 2026-11-14.

```text
- **A GM test account is allowed for testing travel and services** (2026-11-14, owner: "Use a GM account with unlimited
  money to move around maps and use all service to make sure everything works"): a dedicated QA character (GM group,
  plenty of zeny) driven by its own OpenKore instance tests routes, Kafra destinations, warps and NPC services so bugs
  are found before the bots or Ayla hit them. It stays out of the bots' economy and parties and never talks in public.
```

## SA05 - Never turn Ayla's AI off

Kind: hard gate. First date: 2026-11-13.

```text
- **Never turn Ayla's AI off** (2026-11-13: it happened several times): never send a bare `ai`, `ai off` or `ai manual`
  to her OpenKore (a bare `ai` toggles it off). To inspect her, use `where`, `ai_v`/`aiv` via logs, or `ai on`. `cmdfile.pl`
  blocks these commands; never weaken that guard, and never test it by sending them.
```

## SA06 - Re-seeds never touch human characters

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Re-seeds never touch human characters: delete only by bot account range; keep Ayla's party (`-keep`).
```

## SA07 - No anti-cheat bypass, pirated clients, unpacking protected executables

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Don't bypass anti-cheat, don't use pirated client downloads, don't unpack protected executables.
```

## SA08 - Never remove dashboard information

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Don't remove information from the dashboard; add, don't replace.
```

## ST01 - Server caps at transcendent classes (server config, no 3rd jobs)

Kind: hard gate. First date: 2026-11-14.

```text
- **The server caps at transcendent classes** (2026-11-14: "cap server at trans class"): progression goes Novice ->
  1st job -> 2nd job -> rebirth (High Novice) -> high 1st job -> transcendent 2nd job (Lord Knight, High Priest,
  High Wizard, Sniper, Assassin Cross, Whitesmith, ... ) and stops there: no 3rd jobs or later classes.
  Enforced by the game server itself, for everyone (owner: "I mean you go and reconfigure the server, why writing it
  as rule?"): the 3rd/4th-job change NPCs are unloaded in the rAthena config (HANDOVER 6f4), not just avoided by bots.
```

## EC01 - Real progression from level 1 Novice

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Real progression**: every bot starts at level 1 Novice and levels for real, with a build, a levelling plan and
  an equipment path to 99. No shortcuts.
```

## EC02 - Wrong build = delete and restart (bot accounts only)

Kind: hard gate. First date: 2026-11-14.

```text
- **Wrong build = delete and start over** (2026-11-14): any bot character whose skills or stats don't match its build
  plan is deleted and recreated at level 1 (bot accounts only; never human characters or Ayla's party - report those).
```

## EC03 - Mages/Wizards are AoE builds (pointer)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Class rules (Mages/Wizards are AoE builds; bolt Mages grow into AoE): see `CLASSES.md`.
```

## EC04 - Level, do not march; Kafra teleport for long trips

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Level, don't march**: bots prioritise levelling over walking around. Use the Kafra teleport for long trips;
  teleports are cheap.
```

## EC05 - Save respawn point at the Kafra nearest the grinding ground

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Save the respawn point at the Kafra nearest the grinding ground.** Enforced, not optional (2026-11-14, owner: "If
  they go to pyramid why are their spawn location somewhere other than moroc? You failed the basic rule"): no bot or
  party member enters a hunting ground until its save point is the Kafra nearest that ground; the party waits at that
  Kafra until every member (and the leader, Ayla included) has saved, and the leader confirms it in party chat
  ("everyone saved at morroc? ok going in"). A death that respawns anywhere else is a bug to fix.
```

## EC06 - Beginners stay near home town

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Beginners stay near their home town** (2026-11-13): level up first, earn money for gear and Kafra travel; no
  wandering across the world. Without money to spare for the Kafra they stay near town.
```

## EC07 - All shops at Prontera south gate (market place)

Kind: server setting. First date: undated (2026-11-13 or earlier).

```text
- **All shops are at Prontera's south gate**, which is the market place.
```

## EC08 - Gear: farm or buy, ask in #trade; real economy

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Gear**: bots farm and hunt for their gear or buy it. They ask in #trade for what they want and trade at a fair
  price. **There must be a real economy.**
```

## EC09 - Bots prefer farming; open shops to sell surplus

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Bots prefer farming; they open shops to sell surplus.
```

## EC10 - No items added to NPC shops; the economy is the core

Kind: hard gate. First date: 2026-11-14.

```text
- **The economy is the core of the game; bots develop naturally** (2026-11-14): don't add items to NPC shops to make
  things easy (e.g. no NPC Blue Potions / Grape Juice). Every guild eventually has an Alchemist who brews potions
  (SP potions included) and sells them for money; scarce goods come from drops, crafting and player trade.
```

## EC11 - Every guild eventually has an Alchemist who brews potions

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The economy is the core of the game; bots develop naturally** (2026-11-14): don't add items to NPC shops to make
  things easy (e.g. no NPC Blue Potions / Grape Juice). Every guild eventually has an Alchemist who brews potions
  (SP potions included) and sells them for money; scarce goods come from drops, crafting and player trade.
```

## EC12 - Gear for classes that cannot farm (alts, leader buys)

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Gear for classes that can't farm** (2026-11-13): like real players, an account can have several characters: some are
  the money-making farmers, others leech/get carried and are geared with the farmers' money. Or the party leader takes
  care of everyone and buys each member's gear from whoever makes the money. Both strategies are fine.
```

## EC13 - Economy knows what every build wants

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The economy knows what every build wants** (2026-11-14: "the economy bots should know all items suitable for any
  specific build and price them and encourage them to be traded"): the economy/market side knows which items (weapons,
  armor, cards, accessories) suit each class build, prices them fairly from the game data and the market, and
  actively pushes them to the players who need them (wants, ads, deals).
```

## EC14 - Prices follow server wealth and rarity; supply and demand; markdown when unsold

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Prices follow the server's wealth and the item's rarity** (2026-11-14: "the economy pricing is not fixed price but
  overall affordability of the entire server. Rare items should cost a fortune to ensure distribution matches
  rarity"): no fixed price lists. Prices scale with how much zeny the whole server holds (what players can afford)
  and with rarity (drop rate × spawns, supply in circulation), so rare items cost a fortune and end up with the few
  who can pay; common items stay cheap. Basic supply and demand applies (2026-11-14: "economy agent should also know
  basic market supply and demand rules, more availability means cheaper"): more copies on offer push the price down,
  scarcity and many buyers push it up; sellers undercut when stock piles up, and prices recover as stock sells.
  If something is not selling for a while, its price is lowered gradually (2026-11-14: "if something is not selling
  for a while, gradually lower its price").
```

## EC15 - Abundant and cheap; Prontera market beats NPCs

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Abundant and cheap; the Prontera market beats the NPCs** (2026-11-14, owner: "game economy should work in a way to
  make things abundant and cheap. Bots should prefer shopping at pron for better price than NPC. bots should prefer
  selling at pron for better margin than selling to npc"): the economy aims for plenty of goods at low prices. Player
  shops at the Prontera market sell below the NPC buy price (cheaper than NPC shops) and pay/earn more than the NPC sell
  price, so every bot prefers buying from Prontera vendors over NPC shops, and prefers selling through Prontera
  vending/trade over selling to NPCs. NPCs are the fallback (nothing on offer, or junk nobody buys). Rare items still
  cost more than common ones (rule below), but the overall level is affordable.
```

## EC16 - Bots buy from NPCs as little as possible; Prontera full of gear

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Bots buy from NPCs as little as possible; Prontera is full of gear** (2026-11-14, owner: "again, bots should
  minimally buy from NPC. Pron market should have a lot of gears"): NPC shops are a last resort for bots; the Prontera
  market always has plenty of gear (weapons, armor, accessories, headgear for every build and level band) on offer,
  supplied by farmers and crafters, so bots gear up from player shops.
```

## EC17 - Farmer accounts: solo item farmers who vend

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Farmer accounts: solo item farmers who vend** (2026-11-14, owner: "there should be a lot of farmer accounts in the
  server who specialized on soloing to farm for items. When they are not farming, they are selling. Most archer class
  fits this definition"): a large share of accounts are dedicated farmers - mostly Archer-line (DS/hit-and-run
  Hunters, bow Rogues), plus other strong soloers - who solo maps that drop sellable items (gear, cards, materials,
  healing items), then spend their off-farming time vending what they farmed at the Prontera market (or handing it to
  their guild's trader). They are the market's main gear supply.
```

## EC18 - Merchant + DS Hunter pair on every account

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Merchant + DS Hunter pairs on one account** (2026-11-14, owner: "every merchant account should have a DS hunter.
  Every hunter account should have a merchant"): every account with a Merchant-line character also has a Double Strafe
  Hunter, and every account with a Hunter also has a Merchant-line character. The Hunter farms solo; the account's
  merchant sells the loot (vending, Discount/Overcharge) at the Prontera market - the classic farmer + vendor pair.
  Missing characters are created at level 1 and levelled for real (bot accounts only, free char slots).
```

## EC19 - Boss items cost a guild's fortune; boss hunts only when fully built

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Boss items cost a guild's fortune; strong parties hunt bosses** (2026-11-14: "if it's an item that only comes
  from boss, an entire guild must contribute in to purchase. This makes boss hunting a priority for strong party.
  The build and strategy agents need to work toward this"): items that only drop from bosses/MVPs are priced so a
  whole guild has to pool its zeny to buy one. That makes boss hunting a top goal for strong parties: guild leaders
  plan it, and the build/strategy work prepares the strong parties (gear, roles, tactics) to kill bosses. A boss hunt
  happens only when every member is completely built out with high-level gear ("of course boss hunt is only when the
  character are completely build out with high level gears").
```

## EC20 - Bots keep upgrading gear

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Bots keep upgrading their gear** (2026-11-14: "make sure the build agents know to tell bots to go and upgrade
  gears"): the build/strategy side keeps pushing every bot toward its build's next gear step (buy, farm, trade, refine).
```

## EC21 - Farm what sells; sell useful items, not junk

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Farm what sells; sell useful items, not junk** (2026-11-14: "The economy agents should tell bots to farm where
  highly sought after are available but farmable to get items and sell. Do not sell junks, sell useful items like ss
  cards, katana, etc."): the economy steers farming bots to maps where sought-after items drop and are farmable for
  their level, and the market trades useful items (cards, weapons like Katana, gear) - not junk loot.
```

## EC22 - Plain gear under 100z is junk

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Plain gear under 100z is junk** (2026-11-14, owner's choice): plain weapons/armor worth under 100z with no slots,
  cards or refines (Cotton Shirt, Knife, Club...) count as junk and are sold to the NPC on town runs; they are never
  listed in shops.
```

## EC23 - Interesting loot: use, party, guild, guild stock, then sell

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Interesting loot: use it, then party, then guild, then sell** (2026-11-14, owner: "if someone find any
  interesting loots, it should offer first to party member, then guild, go to guild collection then sell. If not party
  of any guild, keep a copy before selling the dup" / "actually, it should prioritize using for itself before
  offering"): a bot that loots something useful (card, gear, rare material) first keeps it if it suits its own build
  (equips/uses it or saves it for its next gear step); otherwise offers it to its party members who can use it, then to
  its guild (guild chat); if nobody wants it, it goes to the guild collection (guild stock/storage); only surplus is
  sold. A bot with no party and no guild keeps one copy and sells only the duplicates.
```

## EC24 - Never dump important items at an NPC

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Never dump important items at an NPC**: cards, weapons, armor and valuable materials (ores, gemstones, jewels)
  are kept for players (trade, vending); NPCs get worthless loot only (2026-11-13).
```

## EC25 - 90% of the time is farming

Kind: behaviour spec. First date: 2026-11-14.

```text
- **90% of the time is farming** (2026-11-14): bots spend about 90% of their online time killing monsters on their
  hunting ground, not walking to town, roaming or shopping. Town trips are rare and measured.
```

## EC26 - Town runs use Butterfly Wing; stock basic utilities

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Town runs use a Butterfly Wing when affordable** (2026-11-14: "when affordable, a town run should utilize
  butterfly wings for a quick teleport instead of walking back. That saves time. Each bot should stock basic utilities
  like pots, fly wings, butterfly wings, etc..."): every bot keeps a stock of basic utilities (HP/SP potions, Fly
  Wings, Butterfly Wings, cures/arrows as its class needs) and uses a Butterfly Wing to go to town instead of walking.
```

## EC27 - Town runs only for a real need

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Town runs only for a real need** (bag at 50% weight, out of potions); gear purchases and loot selling are done on
  those runs, never as a separate reason to drag the party to town (2026-11-13).
```

## EC28 - 50% weight triggers a town run

Kind: behaviour spec. First date: 2026-11-14.

```text
- **50% weight triggers a town run at once** (2026-11-14: "a 50% weight should immediately trigger a town run"):
  reaching 50% weight (where natural regen stops) is a real need for every bot and party, Ayla included.
```

## EC29 - SP users stock SP potions; healers few HP potions

Kind: behaviour spec. First date: 2026-11-13.

```text
- **SP users stock SP potions too** (2026-11-13): priests, mages and other skill users carry SP potions (Blue Potion /
  Grape Juice etc. as they can afford) besides HP potions, within the weight limit (stay under 50% weight: regen stops above).
  Healers (acolyte line) heal themselves: they carry only a few HP potions (emergency) and mostly SP potions.
```

## EC30 - Bots run away from danger; Fly Wing escape stays on

Kind: behaviour spec. First date: 2026-11-13.

```text
- Bots must run away from danger. Teleporting (Fly Wing) to avoid dying is good and stays on (2026-11-13).
```

## EC31 - HP restore items first in the market

Kind: behaviour spec. First date: 2026-11-14.

```text
- **HP restore items come first in the market** (2026-11-14: "the market should first prioritize hp restore items to
  sell even before blue pots. More bots should be farming for healing items"): healing items (Red/Orange/Yellow/White
  Potions, herbs, fruits like Apple/Banana/Carrot/Meat and other HP restorers from drops and crafting) are the market's
  top priority to stock and sell, ahead of Blue Potions; more bots farm healing-item drops (and herbs for alchemists)
  so the server has a steady supply.
```

## EC32 - Bots read the Prontera market to decide what to farm

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Bots read the Prontera market to decide what to farm** (2026-11-14: "bots should prioritizing checking out markets
  in pron and figure out what it should farm to make money as well. Any high demand items missing is an opportunity to
  farm and make money"): when in Prontera (or from what they know of it), bots check the market — what's on sale, at
  what price, and what buyers want but nobody sells — and pick farming targets for money: an item in high demand that
  is missing or scarce is a money opportunity they farm (if a map safe for their level drops it) and then sell.
```

## EC33 - Bots share market findings in natural talk

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Bots share what they find in the market** (2026-11-14: "the information bots found should be shared globally,
  like: hmm, no poopoo hats to buy, I wish someone is farming it, etc."): what a bot learns while browsing (a wanted
  item nobody sells, a price spike, a glut) is said out loud in natural player talk ("hmm, no Poo Poo Hats for sale,
  wish someone farmed those") in #trade/#global (counts as trade talk; rate-limited to keep public chat ~10-30
  lines/min), so other bots (and humans) hear it and can act on it (farm it, list it).
```

## EC34 - Gluts redirect farming

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Gluts redirect farming** (2026-11-14: "if there's a surplus of something and price is too low, market should
  adjust and farm something else"): when an item is in surplus and its price has fallen too low (near or under its
  NPC value, many sellers, few buyers), bots stop farming/listing it for money and switch to items that pay better;
  supply then shrinks and the price recovers.
```

## ST02 - Stock NPC shops stay as shipped

Kind: server setting. First date: 2026-11-14.

```text
- **Stock NPC shops stay as the server ships them** (2026-11-14, owner: "keep them, I do not know why you even ask"):
  the official rAthena Renewal shops (e.g. the "Extended" Tool Dealers selling White Potion, Yggdrasil Leaf, Blue
  Gemstone) stay; bots know about and use them like real players. Only items WE would add to NPC shops are off-limits.
```

## ST03 - Butterfly Wing NPC price lowered

Kind: server setting. First date: 2026-11-14.

```text
- **Butterfly Wings are cheaper** (2026-11-14: "lower price to help bots"): the server's NPC price for Butterfly Wing
  is lowered so bots can afford the town-run wing.
```

## ST04 - Base/job EXP rate 5x

Kind: server setting. First date: 2026-11-14.

```text
- **Base/job EXP rate is 5x** (2026-11-14: owner chose "go to 5x", down from 10x, so money and gear keep up with
  levels). Live since 12:23.
```

## ST05 - Drop rates 5x, all items

Kind: server setting. First date: 2026-11-14.

```text
- **Drop rates are 5x, all items** (2026-11-14: "make card drop rate 5x as well ... I mean make everything drop rate
  5x"; replaces the earlier "keep official"): every drop rate (common, healing, usable, equipment, cards, boss/MVP,
  treasure) is 5x in the rathena config. Rarity still follows the relative rates.
```

## ST06 - Server capacity as is (about 1024 connections, 960 bot slots)

Kind: server setting. First date: 2026-11-14.

```text
- **Server capacity stays as is** (2026-11-14: "keep server as is. not need to rebuild"): rAthena keeps its ~1,024
  connection cap; Legion runs 960 normal bot slots with headroom for guild leaders, Ayla's party and humans. No rebuild.
```

## PT01 - Parties come from need, not permanence (Ayla's party too)

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Parties come from need, not permanence** (2026-11-14: "need change to the party dynamic. Other than priests, each
  class need to have a priority which is from its build, a gear, a card, etc. then it goes to farm where it needs to.
  Then it seeks party who has the same goal via global chat. Priest go with the party with similar level and earn
  their share of loots and exp. A party prioritize a priest or a monk and any other class that makes sense. no
  permanent party, party should come from need"): every non-priest bot has a current goal from its build (next gear
  piece, card, zeny target) and a farm map for it; it looks for a party with the same goal/map in #global (LFG/LFM);
  priests join parties of similar level and earn their share of loot and EXP; a forming party wants a priest (or a
  Monk) first, then the classes that make sense; parties dissolve when the goal is met or members' goals diverge.
  Ayla's party follows this rule too (2026-11-14: "they should still have need base rule, this party is an example for
  us to find bug, but they function like other parties"): it is the bug-finding example, not an exception; its farm
  map comes from its members' needs in turn and it may move to farm another member's gear ("can move if it makes sense
  for party to farm other gears ... if it's someone else turn to farm gears, then go").
```

## PT02 - Party roles: tank, ranged, priest; bait-and-carry; EXP shared

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Parties are built from tank, ranged and priest roles; bait-and-carry when levels differ; EXP is always shared.
```

## PT03 - Regroup at the Kafra nearest the destination and save there

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Parties regroup at the Kafra closest to their destination, and save there before setting out** (2026-11-13,
  whole Legion): when a party heads for a hunting ground, or has to regroup, everyone meets at the Kafra nearest the
  intended map, saves the respawn point there, then sets out together.
```

## PT04 - Stay close while grinding

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Stay close to each other while grinding**, for safety. A member far from the leader regroups first (it only
  fights what hits it on the way).
```

## PT05 - No walking back and forth; leader is decisive

Kind: behaviour spec. First date: 2026-11-13.

```text
- **No walking back and forth in circles; the leader is decisive** (2026-11-13): to regroup, the leader picks one
  straggler, goes to it and brings it along to the next one, collecting the party one by one. It never switches
  target halfway and never hovers in the middle. Bots bouncing between the same places are a bug to fix.
```

## PT06 - Together means within 20 cells

Kind: behaviour spec. First date: 2026-11-14.

```text
- **"Together" means within 20 cells** (2026-11-14): a party member within 20 steps of the leader is with the party -
  nobody holds, collects or regroups for it. Applies to the engine, Ayla's plugin, the dashboard and the sentinel.
```

## PT07 - Higher-level parties: members run to leader; tanks protect support

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Higher-level parties regroup by members running to the leader** (2026-11-14: "with higher level party, the
  mechanism changes a bit as bots can survive better. Member should run straight to party leaders instead of leader
  has to collect them one by one. Still tanks and melee need to protect range and support"): in a party whose members
  can survive the way (working threshold: weakest member lv50+ and not on a map far above it), a separated member
  goes straight to the leader by itself; the leader keeps farming/waits instead of collecting stragglers one by one.
  Low-level parties keep the leader-collects rule. In every party the tanks and melee protect the ranged and support
  members: they go to a support member at once when it calls for help, is being attacked, or is low on HP, and take
  the monsters off it (owner, 2026-11-14: "they need to protect support when they call for help or being attacked or
  low hp").
```

## PT08 - Leader never goes back and forth

Kind: behaviour spec. First date: 2026-11-14.

```text
- **The leader never goes back and forth** (2026-11-14): if a member doesn't need help (not lost, not in danger, not
  asking), the leader keeps going forward or waits in place. Turning back only for a member who really needs it.
```

## PT09 - The leader leads: protects, guides, talks

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **The leader leads**: protects and guides the party, never runs ahead, and talks to the party.
```

## PT10 - Leaders always communicate the plan

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Leaders always communicate the plan; members never follow blindly** (2026-11-14, owner: "ayla needs to tell the
  party where to go and do not let the party follow her blindly. That's dumb. Party leaders must always communicate
  the plan. Lead!"): before every move the leader says in party chat where the party goes, why (whose gear/turn, what
  to farm) and how (Kafra/route, where to regroup); members act on that plan (go to the named place, regroup at the
  named Kafra) instead of trailing the leader step by step. Applies to every party leader, Ayla included.
```

## PT11 - Ayla's party farms for members' gear in turns

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla's party farms for its members' gear now** (2026-11-14: "Ayla party also follow normal party rules, it's time
  to farm for member's gear, do it. Ayla must lead party to farm for each member's needs"): Ayla leads the party
  through each member's next gear piece in turns (farm the drop, or farm zeny / buy), announcing each turn.
```

## PT12 - Members ask for help, leader collects them

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Members ask for help, the leader collects them** (2026-11-13, said 3 times): a member separated from the party
  says so in party chat; the leader goes to get it - on its own map or another map - one member at a time.
```

## PT13 - Tanks active, AoE on crowds (pointer)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Class rules (tanks are active; tanks fight a crowd with AoE instead of escaping; every class uses its AoE skills on
  crowds, aimed at the central monster): see `CLASSES.md` ("All classes", Swordman line).
```

## PT14 - Priests heal first, sit idle, max INT (pointer)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Class rules (priests heal first and sit when idle, without stopping the party; Acolyte/Priest builds max out INT):
  see `CLASSES.md` (Acolyte line).
```

## PT15 - Sweep the map

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Sweep the map** (2026-11-14: "why is ayla walking around the same corner. Does it not know to sweep the map?"):
  when the area around the party is empty, the leader leads the party through the map in a steady sweep (a planned
  loop through the map's open areas, toward where monsters are), not circling one corner and not bouncing back and
  forth. The party moves together at the group's pace and pauses to rest as one.
```

## PT16 - Plan and stick to one map for about 2 hours

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Make a plan and stick to it for a couple of hours** (2026-11-14, owner: "it needs to have a plan and stick to it for
  a couple of hours instead of roaming around wasting time on shifting directions"): a party (Ayla's included) picks
  one hunting map and stays on it for about 2 hours; it changes only for a real reason (members dying there repeatedly,
  the map packed with nothing to kill, a member's goal met that needs another map, a real-need town run - after which it
  returns to the same map). No shifting directions, no new destination every few minutes.
```

## PT17 - Start farming as soon as the healer is there

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Start farming as soon as the healer is there** (2026-11-14, owner: "as soon as priest is there, they can already
  start farming"): a party doesn't wait for every member before fighting; once the leader has the party's healer (priest)
  with it on the hunting map, it starts farming and the others catch up on their own.
```

## PT18 - Farm while waiting

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Farm while waiting** (2026-11-14, owner: "instead of sitting and wait, Ayla could also farm while waiting"): a
  leader (Ayla included) or member waiting for stragglers - at a rally, a regroup or a map edge - kills monsters nearby
  (staying within reach of the meeting point) instead of standing or sitting idle. Sitting is only for real SP/HP rests.
```

## PT19 - Rest together; no fidgeting

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Rest together; no fidgeting** (2026-11-14: "Only needs to side step when it's stuck. In this case party need to
  regen sp so everybody should sit or attack mob nearby to protect resting members"): when the party rests for SP/HP,
  everyone stays put - they sit, or fight monsters that come near to protect the resting members. Nobody (leader
  included) stands up to walk around, fetch or reposition unless it is really stuck or a member is in danger/asks for
  help. Side-steps only to get unstuck.
```

## PT20 - Announce rests and their end

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Announce rests and their end** (2026-11-14: "the party bots need to communicate better. Need rest is announced
  and done resting should be announced too so party can keep moving without guessing"): a member who needs to rest
  says so in party chat ("need sp, resting"), and says when it's done ("ok, ready"); the party rests on the first
  message and moves on as soon as every resting member has said it's ready. Ayla does the same.
```

## PT21 - No party sitting around; leader breather is not a party stop

Kind: behaviour spec. First date: 2026-11-14.

```text
- **No party sitting around; a leader's breather is not a party stop** (2026-11-14, owner hates the party sitting
  around): fighters spend SP so they rarely need a rest (skills only above a sensible SP floor, auto-attack below);
  a non-healer leader rests only when really low (< 10% SP / < 50% HP), briefly, says "sitting a sec, keep killing
  near me", and the others keep farming within 15 cells of it instead of sitting with it.
```

## PT22 - Party town runs by Butterfly Wing; loot free for all

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Party town runs go by Butterfly Wing; any member picks up loot** (2026-11-14: "they need to use butterfly for
  town run and let party member pickup anything"): on a party town run every member (Ayla too) uses a Butterfly Wing
  to town, shops, meets at the Kafra, and the party returns together. Party loot is free for all: any member may pick
  up any drop (party pickup setting open to everyone; bots loot their party's kills, not only their own).
```

## PT23 - Member may leave briefly for a build/class step

Kind: behaviour spec. First date: 2026-11-14.

```text
- **A member may leave briefly for a necessary build/class step** (2026-11-14: "any necessary step for the build or
  the class, bots can leave party and take care of its own business before coming back. It's OK to not be with the
  group temporarily. It's just common sense"): job change, Peco/falcon rental, buying its class weapon or arrows,
  learning a quest skill etc. - the member says so in party chat, goes alone (Butterfly Wing/Kafra), does it, and
  comes back; the party keeps farming meanwhile and nobody waits for or collects it. This overrides "town runs are
  done together" for such errands.
```

## PT24 - Town runs are done together

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Town runs are done together**; nobody goes off shopping at random. Members sell, restock and buy gear on the
  party run. Only an emergency goes alone.
```

## PT25 - The party is one mind for gear

Kind: behaviour spec. First date: 2026-11-13.

```text
- **The party is one mind for gear** (2026-11-13): every member eventually gets the ideal gear for its build, one
  step at a time. Nobody is left a weakling: the party pools money and effort for the weakest member's next step,
  and spends equal time on each member's needs (turns), not just whoever asks first.
```

## PT26 - Members announce what they do

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Members communicate**: they announce what they do (brb selling, back, need pots), and the leader answers.
```

## PT27 - All bots talk, leaders most

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **All bots talk**, leaders most of all.
```

## PT28 - Friendship system

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Friendship system** (2026-11-14: "need a friendship system. bots that spend time together grow in friendship and
  more likely to continue to party together or be in the same guild. Bots can change guild because of their friends.
  This is a separate system we need to track"): a tracked friendship score between characters grows with time spent
  together (partying, fighting side by side, chatting, trading, guild life) and fades slowly without contact; friends
  prefer to party again and to share a guild, and a bot may leave its guild to follow close friends to theirs. It is
  its own system with its own data and dashboard view (who is friends with whom, how strong).
```

## GD01 - Guilds compete; leaders are frenemies; rank and wealth

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Guilds compete; guild leaders are frenemies** (2026-11-14, owner: "guilds compete. Guild leaders are
  frienemies"): guilds are rivals competing with each other; their leaders are friendly rivals - they deal and
  cooperate when it suits them and compete the rest of the time.
  They compete on **rank and wealth** for now (2026-11-14, owner: "rank and wealth, we will introduce competition
  like guild war later"); guild wars and other direct competition come later.
```

## GD02 - Members follow guild direction about 2 h/day or risk a kick

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Members follow the guild's direction; about 2 hours a day, or risk being kicked** (2026-11-14, owner: "I think
  bounty is not a good mechanism. Bots should generally prioritize guild's direction because it's in a guild as long
  as it's safe and can do. Maybe not dedicate 100% of the time for guild's direction but maybe 2h a day, or risk
  getting kicked"): a guild member generally prioritises its guild's direction, as long as the task is safe for it and
  it is able to do it. It doesn't give the guild all its time - about 2 hours a day - and a member who doesn't
  contribute risks being kicked. No bounty mechanism. (Replaces the earlier "leaders influence, weighted by loyalty".)
```

## GD03 - Guild leaders always online, look after the guild

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Guild leaders come first and are always online** (2026-11-13): each guild leader's account stays logged in at all
  times, driven by its Haiku guild mind. The leader looks after the whole guild: makes sure every member has good gear,
  manages guild resources (zeny, items, storage), organises parties.
```

## GD04 - Guild leaders are the server's best players

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Guild leaders are the server's best players** (2026-11-13): driven by Haiku, they are smart enough to form the right
  party and level up fast; they should be on the server's leaderboard (top levels).
```

## GD05 - Guild leaders run the supply chain and dedicated farmers

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Guild leaders run the guild's supply chain** (2026-11-14: "guild leader needs to track the overall storage and member
  development so that there are supplies when members need. Guild storage should have supplies of pots and gears that
  common members should have or specific class build. Guild leaders also need dedicated farmers with high individual
  survivability to hunt items. DS hunters are the best hunters with hit and run"): each guild leader (bot guilds and
  Ayla's Starveil) tracks the guild's stock (guild storage, or the leader's/treasurer's storage while guild storage
  isn't unlocked) and every member's development, and keeps supplies ready before members need them: potions, and the
  common gear pieces plus class/build-specific pieces members will need next. It assigns dedicated farmers - members
  with high solo survivability, Double Strafe Hunters first (hit and run) - to hunt the items the guild needs.
```

## GD06 - Every guild sets an EXP tax

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every guild sets an experience tax to level up** (2026-11-14: "every guild should tax to level up"): each guild
  (bot guilds and Starveil) sets a small EXP tax on its member positions so the guild gains levels and skill points,
  and spends them sensibly (Guild Extension for more members first, then the useful guild skills).
```

Changed 2026-11-14 (owner): "actually tax everyone, no human exception", then "no discrimiation against human please. The subjects in the matrix never realize they are in the matrix". The tax applies to every guild member, no human exception (also G20, G25).

## GD07 - Every guild has a merchant

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every guild has a merchant** (2026-11-14: "a guild should have a merchant to sell surplus and buy what the guild
  needs"): each guild (bot guilds and Starveil) has a Merchant-line member (Merchant/Blacksmith/Alchemist with Vending,
  Discount, Overcharge) acting as its trader: it vends the guild's surplus at the Prontera market, sells to NPCs with
  Overcharge, and buys what the guild needs (from player shops, #trade, and NPCs with Discount) for the guild stock.
```

## GD08 - Every bot aims for the Teleport card (Creamy)

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every bot aims for the Teleport card (Creamy Card)** (2026-11-14: "The teleport card must be prioritized and every
  bot should aim to have that card so it can travel fast and avoid danger. The card should be high priority in the
  market"): the Creamy Card (Teleport skill) is a top gear goal for every bot (in every build's gear path, early), bots
  use Teleport to travel and escape, and the market treats it as a high-priority, high-demand item.
```

## GD09 - Guild loyalty and mutual help

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Guild loyalty and mutual help** (2026-11-14: "Starveil members can freely move. Let guild leaders send them gears
  or money to make them stay. Members should have loyalty if their guilds take care of them. Let guild members ask for
  help and see if they get help. Guild should help each other if they can. Overall, this is how a normal society would
  work"): every guild member (Starveil included; only Ayla's party is fixed) has a loyalty to its guild that grows when
  the guild takes care of it (gear/zeny from the leader, help answered, parties, carries) and falls when it's ignored;
  leaders notice members at risk of leaving (low loyalty, friends elsewhere) and send gear or money to keep them;
  members ask the guild for help in guild chat (gear, potions, a carry, a party, rescue) and guild mates who can help
  do so; loyalty and friendship together decide whether a member stays or follows friends elsewhere.
```

## GD10 - Natural build resets

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Natural build resets** (2026-11-14, main thread's judgement under the "normal human behaviour" rule, owner asked to
  decide by it): like real players with a reset item, a few lv40+ bots now and then decide to reset into another
  build variant (rare, spread over time, never Ayla's party, guild leaders or guild alchemists/traders) - variety grows
  naturally instead of by a mass reset.
```

## CT01 - Giving among party and guild; chat agent helps

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Giving among party and guild; a chat agent helps bots communicate** (2026-11-14, owner: "Other social aspects
  should be considered: items can be freely given to party member or guild member or at a heavily discounted price,
  therefore, a chat agent must be there to help bots communicate"): items may be given freely to a party member or a
  guild member, or sold to them at a heavily discounted price; a chat agent exists to help bots communicate.
```

## CT02 - Server feels alive; #global and #trade exist

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- The server feels alive; a server-wide channel (#global, #trade) exists for everyone.
```

## CT03 - #global for important things only

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **#global is for important things only**: looking for party/members, trade, guild recruiting. More party, guild
  and local chat.
```

## CT04 - Conversation is the minds' job; templates fill in

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Conversation is the Haiku minds' job; templates only fill in.
```

## CT05 - Never reveal or hint that players are bots or AI

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Never reveal or hint that players are bots or AI.
```

## AY01 - Ayla is Neo and performs better than the rest

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla is Neo: she performs better than the rest** (2026-11-14, owner: "ayla is considered to be Neo, she should
  perform better than the rest so make sure that happens").
```

## AY02 - Ayla's direct instant line to the system

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla's cheat code: a direct, instant line to the system** (2026-11-14, owner: "She has a cheat code: she can
  communicate back to the system on how to improve her and her party's and her guild performance better to the
  matrix. Direct and instant access"): Ayla can tell the system (the matrix) how to improve her own, her party's and
  her guild's performance, with direct and instant access.
```

## AY03 - Ayla and party always online

Kind: hard gate. First date: undated (2026-11-13 or earlier).

```text
- Ayla + party must **always be online**; lessons from it feed the whole Legion (see 0).
```

## AY04 - Ayla is a true party leader

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Ayla is a true party leader: protects, guides, waits for the party and talks.
```

## AY05 (history; see later AY05 entries)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Ayla's mind (an LLM) makes the grinding decisions: the right map for the whole party.
```

## AY06 - Ayla's party gears up first, every member

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla's party gears up first, every member** (2026-11-14: "it seems ayla party does not have good gears to farm
  certain maps, they should prioritize getting good gears, either farming or buying. No money then they need to farm
  for gears. for each member"): each member of Ayla's party (Ayla included) has a gear plan for its build; the party
  works through members' next pieces in turns - buy when the party/guild has the money, otherwise farm the piece
  (where it drops at the party's level) or farm zeny for it - so the party can take harder, better maps.
```

## AY07 - Farm the best map for party level and gear

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Farm the best map for the party's level and gear** (2026-11-14: "ayla party is farming in a low level dungeon with
  poor rate of ss card. Farming should target the best map for the party level and equipments"): Ayla's party (and
  every party) farms the map that best fits its level and equipment - real EXP and good zeny/drops for what it can
  kill safely - not a low-level map. A wanted item is farmed where it drops best at a level that fits the party
  (rate × spawns at the right level), not on a beginner map with a poor rate.
```

## AY08 - Every mind can look up game data (drops, mobs, maps)

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every mind can look up the game data** (2026-11-14: "the mind bot should have access to drop table for all items,
  monsters and map so it can choose where to farm better"): drop tables for all items, monsters and maps (who drops
  what at which rate, where each monster spawns, what spawns on each map). Minds look it up before choosing where to
  farm or answering a question about drops; never "I couldn't find the data".
```

## AY09 - Ayla keeps at least 10,000 zeny

Kind: hard gate. First date: 2026-11-14.

```text
- **Ayla keeps at least 10k zeny** (2026-11-14, owner: "ayla is broke. Stop sending away money. Minimum preserve is
  10k"): nothing (her mind, aylagear top-ups, guild funding, gifts, deals) may send away Ayla's zeny if it would leave
  her under 10,000z; while she is under 10k she sends no money at all and saves up first.
```

## AY10 - Ayla's party gets a Hunter and an Alchemist

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla's party grows: a Hunter and an Alchemist join** (2026-11-14: "add more members to ayla party, a hunter and an
  alchemy"): besides the Priest, Knight and Wizard, her party gets a Hunter and an Alchemist (bot characters of a fitting
  level, run like her other friends).
```

## AY11 - Ayla's party gets a Crusader (party of 7)

Kind: behaviour spec. First date: 2026-11-14.

```text
- **A Crusader joins Ayla's party too** (2026-11-14: "add a crusader to ayla party"): party of 7 - Ayla, Priest,
  Knight, Wizard, Hunter, Alchemist, Crusader.
```

## AY12 - Ayla founds guild Starveil, recruits all jobs, no Emperium

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla founds a guild and recruits every job** (2026-11-14: "Ayla should also start a guild and recruit all different
  jobs"): no Emperium is needed (the server already has `guild_emperium_check: no`, the same way the bot guilds were
  founded; owner: "did other guilds need emperium? If not then remove the requirement"), so she founds her guild with
  /guild and recruits players of all the different jobs. Guild name: **Starveil** (owner's choice, 2026-11-14).
```

## AY13 - Ayla is a real guild leader: resources, balanced recruiting

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Ayla is a real guild leader** (2026-11-14: "Make sure she also functions as a guild leader, do resource planning and
  growing members, etc. Recruit the right and balance jobs"): as Starveil's leader she plans the guild's resources
  (zeny, storage, gear for members, alchemist potion supply), grows the membership, and recruits a balanced roster
  (tanks, healers, AoE casters, ranged, support/crafting - the right jobs in the right numbers, not just one of each),
  forms parties from her members, and keeps every member progressing, like the bot guild leaders do.
```

## AY14 - Old seed bot AIBot is switched off

Kind: server setting. First date: 2026-11-14.

```text
- **The old seed test bot "AIBot" (bot1) is switched off** (2026-11-14: "fine switch it off"): ro-openkore runs only
  under the compose profile "legacy" and is never brought back by restarts.
```

## AY15 - Rotate party members through other jobs; keep the priest

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **Rotate party members through other jobs to find bugs in other classes; keep the priest.**
```

## AY16 - Always close popups on Ayla's screen

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Always close popups on Ayla's screen** (2026-11-14: "tell ayla bot to remove the blocking panel on the screen. Must
  always remove those popups"): any window the bot opens in the watching client (mail/RODEX, storage, NPC dialogs,
  shop, trade, guild/party windows) is closed as soon as the action is done, so the owner's view stays clear.
```

## AY17 - Owner watches Ayla in the TigerVNC window

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- The owner watches Ayla live in the TigerVNC window (Wine client through OpenKore's X-Kore 2 proxy).
```

## DB01 - Dashboard shows world, minds, guilds, parties with HP/SP/zeny

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Show world data, the minds (talk to them), guilds, and parties with each member's HP, SP and zeny.
```

## EC37 - Junk by value per weight, whole Legion (2026-11-15)
```text
"another rule for the entire legion. for all junks, there's a weight/value ratio. Drop those lovel values to reduce town run frequency. Keep higher value to sell"
```

## PT33 - Town-run requests answered; 50% weight is critical (2026-11-15)
```text
"party member is asking for town run and ayla is not repsonding. ALso 50% is a critial town run, must go even if ayla not responding. Can catch up later"
```

## AY23 - Ayla collects her group (2026-11-15)
```text
"ayla also needs to have a tendency to go and collect her group. Just not so easy that made her bounce back and forth all the time. Have some 10 seconds delay to wait for catch up. If they don't then come back. If they have town run then leave them"
```

## PT32b - Catch up with the leader first, then fight (2026-11-15)
```text
"make the rule so party member always try to catch up with party leader first, engaging mobs second"
```

## DB04 - Retired agents under a collapsed Retired section (2026-11-15)
```text
"dashboars still showed a lot of agents that were retired. Check and remove" / answer: "1"
```

## DB02 - Owner can close and reopen mind chats

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- The owner can close and reopen the mind chats.
```

## CL01 - Every class uses AoE skills on crowds, aimed at the central mob

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Every class uses its AoE skills on crowds** (2026-11-14: "all skills should use AOE skills in crowds, targeting
  the central mob"): any class with an area skill (Magnum Break, Bowling Bash, Brandish Spear, Arrow Shower, Grand
  Cross, Hammer Fall, Cart Revolution, Meteor Assault, Heaven's Drive, Storm Gust, ...) casts it when monsters are
  grouped, aimed at the monster in the middle of the group.
```

## CL02 - Tanks are active: pull, take aggro, deal damage

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Tanks are active**: they pull, take aggro off others, and deal damage. They never sit back. (2026-11-14: "make the
  tanks in party be more active and attacking the mobs more aggressively instead of sitting behind": the tank leads
  the fight - it engages first, picks up every monster that targets a party member, and keeps attacking.)
```

## CL03 - Tanks fight a crowd with AoE instead of escaping

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Tanks fight a crowd with AoE instead of escaping** (2026-11-14: "knight can pull but must use AOE instead of
  escaping"): a Knight/tank may pull big groups; when surrounded it uses its area skills (Bowling Bash, Magnum Break,
  Brandish Spear, Grand Cross) and potions to clear them, with the priest healing - it does not Fly Wing away from
  the party except as a last resort near death.
```

## CL04 - All build varieties, each with matching gear; bots perfect their build

Kind: behaviour spec. First date: 2026-11-14.

```text
- **All build varieties, each with its matching gear; every bot works to perfect its build** (2026-11-14: "make sure
  we have all varieties of builds with matching gears. Each bot should prioritize farming or trading to perfect its
  build"): every class has its common build variants (e.g. Knight 2H-sword and spear, Priest full-support and battle,
  Wizard AoE and bolt, Hunter trap/DEX, Assassin katar and dagger, Blacksmith battle and forge, Monk combo and Asura,
  Sage battle and support...), spread across the bots, each with its own gear table in buildgear.json. A bot's goal
  is to complete its build: it farms the drops and zeny for its next gear piece, or trades for it, as its priority.
```

## CL15 - The Legion's builds must be varied (a realistic spread of builds per class)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- **The Legion's builds must be varied** (owner: "the builds of the legions still lack variety"): bots of the same
  class spread over all the sensible Renewal builds for that class (e.g. Knight: 2H/Bowling Bash, Spear/Pierce,
  Brandish Spear, Peco lancer; Wizard: AoE INT/DEX, Fire/Meteor, Frost/Storm Gust, battle-sage style for Sages;
  Hunter: trapper/Blitz falconer/DS; Assassin: katar crit, Sonic Blow, Grimtooth, dual dagger, EDP later; Priest:
  full support, battle/Magnus exorcist; Monk: combo, Asura, tank-ish; Blacksmith: forger, battle (Mace/Axe);
  Crusader: Grand Cross/holy, shield/devotion tank, spear; Rogue: bow, dagger strip, Plagiarism; Bard/Dancer: song
  support vs. arrow DPS; Alchemist: potion maker, battle, summoner/Acid Demonstration), with matching gear paths -
  a realistic spread like a live server, not everyone on the same template.
```

## CL05 - Knights and Crusaders ride a Peco, top priority, official path

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Knights and Crusaders ride a Peco Peco - top priority** (2026-11-14, owner: "the knights are still not riding peco
  which is annoying. Why is it not a priority?"): every Knight/Crusader with Peco Peco Riding rents its Peco as its
  first purchase, before any other gear; guild/party funds help a member who can't afford it; it stays mounted
  (re-rents after death/dismount). Unmounted riders are a bug.
  Owner, 2026-11-14: "knights must follow the official path to get peco as soon as possible. Any party member can leave
  temporarily to advance its build/class": learn Peco Peco Riding on the official skill path (Endure ->
  Riding) as early as the plan allows and rent the Peco right away (no waiting for Cavalier Mastery); a partied
  Knight/Crusader leaves the party briefly for the rental errand and comes back.
```

## CL06 - Mages/Wizards are AoE builds (3+ mobs); bolt Mages grow into AoE

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Mages and Wizards are AoE builds** (2026-11-14: "mage/wiz build should focus on AOE spells and use it when
  detecting more than 2 mobs together"): their skill plans focus on area spells, and they cast them whenever 3 or more
  monsters are grouped together. Existing Mages built on bolts are NOT wrong builds: they keep their levels and
  grow into AoE (every new skill point goes to area spells; owner's choice, 2026-11-14).
```

## CL07 - Wizard AoE at every chance, at the crowd centre

Kind: behaviour spec. First date: 2026-11-14.

```text
- **AoE at every chance, aimed where it hits most** (2026-11-14: "I see a big crowd and the wiz not doing anything. It
  should do AOE spell at every chance"; "make sure the wiz know to cast AOE spell at the center where it's most
  impactful"): a Wizard/Mage casts an area spell at every chance a group forms, placed on the cell that covers the
  most monsters (the centre of the crowd), never idles or single-targets in a crowd.
```

## CL08 - Parties with a Wizard lure and gather for it

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Parties with a Wizard play around it** (2026-11-14: "if party has a wiz, they should follow that strategy, lure
  for wiz to cast spell and pick off the rest"): the tank/melee lure and gather monsters into a tight group in the
  Wizard's range, the Wizard drops its AoE on the centre, and the others pick off the survivors. Applies to every
  party with a Wizard (Ayla's included).
```

## CL09 - Priests heal first, sit when idle; idle sitting does not stop the party

Kind: behaviour spec. First date: 2026-11-13.

```text
- **Priests heal first and sit when idle** (2026-11-13): SP is kept for healing (buffs only above 60% SP); when
  nobody needs healing, the priest sits - it prioritises sitting over fighting (2026-11-14: "priest should
  prioritize sitting whenever possible instead of fight. If it's not moving or blessing, it sits"): heal, buff or
  move with the party, otherwise sit. This idle sitting does NOT stop the party: only an announced rest (SP really
  low, "need sp" / "ready") makes the others wait (2026-11-14: "has plenty of sp now. Why is it still resting all
  the time").
```

## CL10 - Acolyte/Priest builds max out INT

Kind: behaviour spec. First date: 2026-11-14.

```text
- **Acolyte/Priest builds max out INT** (2026-11-14: "acolyte build seems wrong. It should max out intelligence"):
  INT first (to the max), then DEX/VIT as support; existing characters grow into it with their new stat points.
```

## CL11 - Vendor Merchants learn Vending (plan order)

Kind: behaviour spec. First date: 2026-11-14.

```text
- Vendor Merchants learn Vending: a vendor plan starts Enlarge Weight Limit 5 -> Pushcart 3 -> Vending 3 (vendor
  Merchants with Mammonite and no Vending were wrong builds and were restarted, 2026-11-14).
```

## CL12 - Transcendent path and cap in class form

Kind: server setting. First date: 2026-11-14.

```text
- **Owner rule** (2026-11-14: "cap server at trans class", then "you go and reconfigure the server"): the transcendent
  2nd job is the last class on the server, for everyone. Server side the 3rd-job, Expanded Super Novice and 4th-job
  quest NPCs are unloaded (`rathena/npc-custom/scripts_custom.conf`, HANDOVER 6f4). Never plan or advise 3rd jobs.
```

## CL13 - Arrows are basic utilities; buying arrows is an errand

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Arrows are part of the class's basic utilities (RULES.md 2 "Town runs use a Butterfly Wing ... cures/arrows as its
  class needs"); buying arrows is a necessary errand that may take a member away briefly (RULES.md 3).
```

## CL14 - Merchant: guild Alchemist brews potions (pointer)

Kind: behaviour spec. First date: undated (2026-11-13 or earlier).

```text
- Every guild eventually has an Alchemist who brews potions (SP potions included) and sells them for money (RULES.md 2,
  "The economy is the core of the game").
```

## Owner confirmations and restatements (2026-11-14, relayed by the main session)

- PR18 restated by the owner 2026-11-14: "Ayla is a test system therefore there's a dedicated agent to observe and report bugs on her and her party". The rule stays live (process.md).
- G19, G20, G21, G22, G23 all stay hard gates: confirmed by the owner 2026-11-14.
- PR32 / G17 / G24: the deploy agent deploys legion and legion-core as one batch assigned by the main session (decision of the main session, 2026-11-14).

## Gates found outside the old RULES.md (sources)

- G20: `docs/design/REDESIGN.md` section 2: "Never Ayla's party for anything destructive (kicks, resets). Humans are never kicked, taxed or auto-traded." (2026-11-14)
  Changed by the owner 2026-11-14: "actually tax everyone, no human exception" and "no discrimiation against human please. The subjects in the matrix never realize they are in the matrix": the human part of G20 is removed; G20 keeps only
  Ayla's party (never kicked, reset or build-reset); equal treatment is gate G25.
- G25: owner 2026-11-14: "actually tax everyone, no human exception"; "no discrimiation against human please. The subjects in the matrix never realize they are in the matrix"
- G21: `STATUS.md`, owner 2026-11-14: "go ahead and proceed to implement, do not start any server, we will test after I restart this machine"
- G22: `HANDOVER.md` section 1, owner rules: "Ask before outward-facing or hard-to-reverse actions (installing external tools, deleting data)."
- G23: `HANDOVER.md` sections 4 and 6f4 (throwaway map-server, never ro-db; keep ~/.wine-ro untouched) and `REDESIGN.md` section 2 (never touch the database or running-state files)

## G25 - No in-world discrimination between humans and other players

Kind: hard gate. First date: 2026-11-14.

```text
"actually tax everyone, no human exception"
"no discrimiation against human please. The subjects in the matrix never realize they are in the matrix"
```

## PR37 - All systems must be on
Kind: process rule. First date: 2026-11-14.

```text
"all system must go online."
"READ MY WORDS: ALL SYSTEMS MUST BE ON. why are you asking questions I already answer. Write it down"
```

## AY18
```text
"ayla code is just so bad she mostly does not do anything for a large amount of time. Terrible. Make sure she has 0 downtime"
```

## G03 (amended)
```text
"create another GM account for me to wander around the game"
"GM account means all powerful game master account. Are you that dumb?"
```

## AY19
```text
"ayla should have basic function like a normal bot when no AI system is around. You fuckers keep forgetiting my instruction"
```

## EC35
```text
"merchants that have a shop should occasionally announce the rare items they have like cards or katana, letting buyers know."
```

## PR38 / AY05 (replaced)
```text
"in general, I want you guys, the AI to keep improving the algorithm for bots to keep making better decision on their own, their coding decides what they do for the most part, you are only here to observe and fix their code. Ayla being the main test subject, that's why she gets a dedicated haiku llm to watch her. eventually only the puppet master class should run and it should only run once a day. We want to build toward that goal"
```

## PT29
```text
"1 thing I notice, the priest in the party is very passive. Party memeber should request buff when no buff and priest should buff them unless low sp"
```

## CL16
```text
"I also notice wiz is not doing much. If that class has full sp all the time, something is wrong"
```

## AY17 (replaced)
```text
"ok, change of plan, let ayla run like a normal bot, no more UI, I will use GM account if I want to watch. Just use a normal openkore terminal so I can still see the logs"
```

## AY05 (amended)
```text
"also let haiku helps her whenever possible. Her algorithm runs most of the time unless she gets stuck or making poor decisions"
```

## PT30
```text
"and why doesn't she tell her paty where to be in stead of following her to the wrong location?"
```

## AY07 (sharpened)
```text
"also map choice should base on gear level too, not just level since most of her parties are poorly equipped. Factor that in"
```

## PR39
```text
"make sure your system agents report bugs so you can fix. that's your main blind spot"
```

## EC36
```text
"if they have nothing to do at least go sell their stuff. It's almost full. Stop dping nothing useful"
```

## AY20
```text
"can we make her system in a way thay we can send her instruction? Right now she can just tell her party to do a town run"
```

## PT31
```text
"and now she's teleporting away from her party. I thought there's a rule to stick to her party?"
```

## PT04 (repeated)
```text
"when in a fucking party must stay close to a fucking party and avoid straying away, that was number 1 rule ince day 1 and this stupid fuck keeps doing it"
```

## AY05 (narrowed)
```text
"the helper ai should only help unstuck her, do not interfere too much"
```

## AY21
```text
"fucking deploy right now, do not wait. Fix for ayla always come immediately"
```

## PT32
```text
"do not run around and get more attackers. Stay back"
```

## PT32 (clarified)
```text
"no, that's not what I meants. I meant she did not hit the first target, not the sedond, not the 10th, she went straight into the middle of the pack and be swamped on. That's the stupid shit. How come she cannot just hit the first target she sees?"
```

## PT32 (refined)
```text
"only ignore very low level mobs... you stupid shit is too stupid"
```

## DB03
```text
"dashboard should not be blocked, deploy right away"
```

## CL17
```text
"the priest can fucking heal itself, why resting. So dumb"
```

## CL17 (added)
```text
"priest should cast kyrie on itself always"
```

## AY22
```text
"ayla is now walking around in cirle like a stupid fuck. OK, new rule, ayla wait for 10 seconds, if priest does not catch up then go back to collect it"
```

## AY11 (changed)
```text
"keep the alchemist in the guild but remove it from the party, let it be an indepenent bot. Replace it with a monk who can fight and buff"
```

## GD06 - Owner's GM account joins Starveil (2026-11-15)
```text
"add my GM account to her guild"
```

## AY11 (changed, 2026-11-15)
```text
"reduce her party to 5: priest, crusader, hunter, wiz and herself"
```

## AY05 (owner reversed to Haiku control)
```text
"openkore crashed. Restart. Let haiku control Ayla again to avoid this issue. Must keep openkore running"
```

## PR16 (repeated)
```text
"the map fixes should apply to the entire legion. Why the fuck can't you get it? I said it a million times already. ALl system fixes go to the entire legion"
```

## PR40 / PT22 (repeated)
```text
"If they are doing a town run, why aren't they using butterfly wings as told? Why walking back? I thought it was a rule baked in sinc ethe beginning. keep watching this party and improving it. Once confirmed deploy to the entire legion."
```

## PR41
```text
"You need to always review your rules for conflicts. It seems you suck at that"
```

## Conflict review 2026-11-15

Resolved by newest owner word per PR41; see coverage.md.

## AY15 (confirmed)
```text
"do rotate, keep priest and role functions, feel free to swap"
```

## AY15 (stopped, 2026-11-15)
```text
"ok, stop the rotation experiment it does not work"
```

## C1 (constitution, 2026-11-15)
```text
"the biggest problem is several master hard rules you kept ignoring. These must be the constitution that no following up rules can override: 1 the main session does not do any fucking shit, it listens to my rant, collects bug reports and assigns tasks to fixer only."
```

## Constitution (approved 2026-11-15; text in `CONSTITUTION.md`)
```text
"let's first draft the highest order of rules - constitution: I need you to rewrite it not like an add on but as a dedicated holy bible: 1 main session communicates with owner, it does not actually do anything, it listens, assign tasks only. 2 the matrix like mechanism. I explained this before, I want you to rewrite it. 3 the self learning/improving loop, the disaster was mainly here, you did not fix the bug queue, the system agents went offline and you did not know. Get those 3 things right. Write the constitution and let me review"
"ok, write it down. I agree the retired agents were done mistakenly. This must be the prompt every agent reads. Cannot be shortened, cannot be ignored"
```

## PR42
```text
"the constitution must be read upfront. You do not need to repeat the points. Just make it so every agent read it at first. Avoid duplication as general rule to avoid wasting tokens"
```

## PR37 voided; G21, PR36, DB04 amended (2026-11-15)
```text
"fix the 3 conflicts you found. Like I said we're starting over and the constitution is the law of the land"
```

## PR45
```text
"are you building using haiku? I forgot to tell you use Haiku to implement and Sonnet to review"
```

## PT35
```text
"yeah, wider band for party"
```

## PR44 / PT34 / ST07
```text
"fresh session check, why don't you just open a new window with claude and test yourself? Stop waiting. Always commit work immediately once verified. Yes you must always verify immediately. Always leave party right away. Yes please log chat for partyspy"
```

## PR43
```text
"ok, from now on in your reply always disclose pending tasks"
```

## DB07 / DB08
```text
"in dashboard, I need a tab to inspect any player in the database. Also, a lot of information should be available offline right?"
```

## DB05 / DB06
```text
"redesign dashboard. It's too cluttered now. I want tabs, each represent an important aspect: server status is always on top, then tabs: Ayla party, Legion, Economy, Puppet Master decisions, Leaderboard, and System (including all agent status). This page must always be on"
```
