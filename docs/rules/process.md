# Process: who does what

One line per rule: ID, statement, date. The owner's words are in `archive.md`. Hard gates are in `RULES.md` (core) and
`gates.md`; they are repeated here only by ID.

## Roles
- PR10. Two classes of agents, puppet masters and system agents: see Constitution 2.5 and 2.6. The puppet masters here: guild leaders (guild01-10, Ayla for Starveil), build/class, chat; the market is an algorithm engine, not a puppet master (EC46). The dispatcher and its one-shot agents are from the approved redesign brief, not an owner quote. (2026-11-14)
- PR12. Puppet masters run as their own agents outside the main session. One-shot runs woken by a dispatcher script are from the approved redesign brief, not an owner quote. (2026-11-14)
- PR11 / G14. See Constitution 1.1 to 1.3. (2026-11-14)
- PR23 / G13. See Constitution 1.3. The owner's own instructions to Ayla (AY20) and owner-ordered restarts of her OpenKore go through an agent. (2026-11-14, amended 2026-11-15)
- PR31 / G24. Only the deploy agent runs go vet/test and image builds; fixers hand over "code ready". The deploy agent may stop
  stray golang build/test containers that block a deploy, never rathena, Ayla, legion or the db. (2026-11-14)
- PR36. Scripts (sentinel, partycheck, league analyst) watch alongside the system agents and file into the bug queue; they replace no agent (Constitution 2.6, 2.9, 3.8). (2026-11-14, amended 2026-11-15: the retirement of the watcher, QA and watchdog agents is void)
- PR24. See Constitution 3.3. Reports go through the bug queue with `legionctl bug` (it arrives with the puppet package). (2026-11-14)
- PR18. Ayla is the test system, so a dedicated agent observes her party and reports bugs. Scripted checks (`partycheck`) run every
  minute; an AI watcher is woken by violations and for a periodic review. Restart it whenever it stops. (2026-11-14, restated)
- PR22. The main session keeps an up-to-date list of open questions only the owner can answer and raises it under its own
  heading in every update until answered. (2026-11-14)

## World and goals
- PR04. Society model: see Constitution 2.3 and 2.4. (2026-11-14)
- PR08. See Constitution 2.5 and 2.9. (2026-11-14)
- PR09. See Constitution 2.3 and 2.5. (2026-11-14)
- PR16. See Constitution 2.8 (every fix and lesson goes to the entire Legion): it goes into the shared engine code and every OpenKore instance, and is verified on bots outside her party. (owner repeated 2026-11-14)
- PR07. See Constitution 3.2. (2026-11-14)
- PR06. See Constitution 3.2 and 3.6. (2026-11-14)
- PR26. Main priority: Ayla's party running well. (2026-11-13)

## How to work
- PR05. Spend LLM tokens only where judgement or words are needed. Most agents run on Haiku; the model of the main session
  is for the owner to decide. (2026-11-14)
- PR17. Observe, find problems and fix them without waiting for the owner. Probe classes (`tools/legion/classprobe.py`)
  after every change. (2026-11-14)
- PR25. Self-improving loop: see Constitution Article 3. Lessons are recorded in `legion/data/lessons.md`. (2026-11-14)
- PR27. See Constitution 3.5. (2026-11-13)
- PR28. See Constitution 3.6. (2026-11-13)
- AY08. Every agent looks up game data (`legionctl drops`, `mob`, `mapinfo`) before choosing where to farm or answering a drop
  question; never "I could not find the data". (2026-11-14)
- PR21. Class and build rules live in `docs/classes/`; the build/class master keeps them current. (2026-11-14)
- PR35. Write everything down for the next AI in `HANDOVER.md`, and keep the rule files current. (2026-11-13)
- PR13. The AI harness lives in the git repository `G:\RO\legion`. (2026-11-14)

- PR37. Void since the owner's shutdown of 2026-11-15 and the restart from the constitution. What runs is decided by the owner again, part by part (G21). (2026-11-14, voided 2026-11-15)

## Recording rules and deploys
- PR02. When the owner states a rule ("must", "always", "never", "should", "apply to all ..."), the same turn: one short line
  in the right file under `docs/rules/` (class rules in `docs/classes/`), the owner's words with the date in `archive.md`
  under the same ID, and a row in `coverage.md`; a new hard gate also gets a G ID in `gates.md` and the core. A new rule that
  replaces an old one edits the old line. (2026-11-14)
- PR01. Rules are short plain lines with an ID and a date. They never repeat the owner's prompt. (2026-11-14)
- PR45. See Constitution Article 4. The main session's model is the owner's choice. (2026-11-15, moved into the constitution 2026-11-15)
- PR47. An agent that only runs an algorithm is a script, not an AI agent (Constitution 2.7, 2.9); every such script comes with tests. This includes the deploy agent (G17 then names the deploy script). (2026-11-15)
- PR48. GitHub Copilot can take over this project at any time: its instruction files under `.github/` stay current and point to the constitution, `RULES.md`, `STATUS.md`, `HANDOVER.md` and `AGENTS.md` (pointers, not copies, PR42), and a test checks that they do. (2026-11-15)
- PR49. For builds, skills and similar game knowledge the owner has no view on: research it (online, the server data) and decide; do not ask the owner. (2026-11-15)
- PR46. `.env` is not tracked by git; the repository holds an `example.env` with the same keys and no secrets. (2026-11-15)
- PR44. Every change is verified live as soon as it is made, and committed as soon as it is verified. Nothing waits on a later session or the owner when an agent can check it now (for example a fresh-session test via a headless `claude -p`). (2026-11-15)
- PR43. Every reply of the main session ends with its pending tasks: each task still running or waiting, who has it, and what it waits on. (2026-11-15)
- PR42. Nothing is written twice. A rule, fact or instruction lives in one place and everything else points to it; every agent reads the constitution first and exactly once, and no prompt, brief, skill, rule or reply restates it. (2026-11-15)
- G15 / G16 / G17. Deploy rules: legion/legion-core only with `--no-deps`; rathena restart at most once an hour; deploys every
  3 hours as one batch assigned by the main session, run by the deploy agent only, for both legion and legion-core (excepted: critical unblocking fixes, Ayla fixes (AY21) and owner-asked dashboard changes (DB03), deployed at once by the deploy agent with --critical); working subagents report "built" and never deploy (PR32). Details in `gates.md`; the deploy scripts are described in `docs/handover/startup-old.md` (0.4 and 0.5) and `tools/deploy/`.
- Engineering notes that behave like rules (Windows git, engine flags default off, lessons about locks): `docs/design/REDESIGN.md` section 2.

## Dashboard
- DB01. The dashboard shows world data, the minds (the owner can talk to them there), guilds, and parties with each member's HP, SP
  and zeny. (2026-11-14)
- DB02. The owner can close and reopen the mind chats. (2026-11-14)
- DB04. Only agents the owner has retired sit under a collapsed "Retired" section on the dashboard; nothing is removed. None is retired now (Constitution 2.9). (2026-11-15, amended 2026-11-15)
- DB05. The dashboard is one page with the server status always on top and tabs below it: Ayla's party, Legion, Economy, Puppet master decisions, Leaderboard, System (with every agent's status). No clutter; nothing is removed (G08), it moves into its tab. (2026-11-15)
- DB06. The dashboard page is always on, also when the stack or the agents are down; it then shows what is down. (2026-11-15)
- DB07. A dashboard tab inspects any player in the database: search by name, then the full character (stats, gear, inventory, skills, zeny, guild, party, history). Read-only. (2026-11-15)
- DB10. A dashboard tool lets the owner guide Ayla: set her farm map, her guild's direction, party orders and other directives, see the puppet masters' current directives next to his overrides, set an expiry, and clear an override (AY25). (2026-11-15)
- DB11. The dashboard shows the facts of the market engine, the farmers, pricing, the crier notices and the verified game facts behind them. Every fact is read from its source (engine API, data files, constants the engine publishes, the item and mob data), never typed into the page, so it follows every later change; a test fails when a shown fact is hard-coded. Nothing is removed (G08); offline it shows the last saved data (DB08). (2026-11-15)
- DB09. A dashboard Builds tab lists every class and build path the build agent plans, with its end goal: equipment and cards, stats, skills, and how it plays when farming solo, in a party, against MVPs and in PvP. It shows what the engine actually uses; gaps are shown as gaps. (2026-11-15)
- DB08. Everything that exists on disk is shown when the stack is down (files, logs, reports, the last saved world data with its age); "engine down, no data" only where nothing exists. (2026-11-15)
- PR38. See Constitution 2.7, 2.8 and 2.9. Ayla's Haiku helper steers her only through guarded commands (AY05); until the goal of 2.9 is reached AY05 and PR18 run. (2026-11-14)
- PR39. See Constitution 3.3; it covers every system agent, script and watchdog (sentinel, partycheck, Ayla's watchdog and Haiku helper, qa, watcher, fixers that notice side bugs). (2026-11-14)
- DB03. Dashboard changes the owner asks for deploy right away, like Ayla fixes (AY21); they never wait for the 3-hour batch. (2026-11-14)
- PR40. See Constitution 2.8 (new behaviour is proven on Ayla's party first). Once confirmed working it is deployed to the entire Legion; fixes of shared data or code (maps, portals, routes, bugs) go to the whole Legion in the same deploy (PR16), only new behaviour is staged. (2026-11-15)
- PR41. Every new rule and every instruction to a fixer is checked against the existing rules for conflicts first; a full conflict review of all rule files runs after each batch of new rules, and conflicts go to the owner or are resolved by the newer owner rule. (2026-11-15)
