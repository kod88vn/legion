# Process: who does what

One line per rule: ID, statement, date. The owner's words are in `archive.md`. Hard gates are in `RULES.md` (core) and
`gates.md`; they are repeated here only by ID.

## Roles
- PR10. (the dispatcher and one-shot agents below are from the approved redesign brief, not an owner quote) Two classes of agents. Puppet masters act inside the world through data and direction: guild leaders (guild01-10,
  Ayla for Starveil), economy/market, build/class, chat. System agents sit outside the world and never steer the game:
  sentinel and other scripts, party watcher, class QA, fixers, deploy agent, analyst. (2026-11-14)
- PR12. Puppet masters run as their own agents outside the main session. One-shot runs woken by a dispatcher script are from the approved redesign brief, not an owner quote. (2026-11-14)
- PR11 / G14. The main session listens to the owner and to bug reports and assigns fixers. It does no coding. All coding,
  fixes and deploys are done by subagents. (2026-11-14)
- PR23 / G13. The main session never sends orders to Ayla. Owner-reported problems go to a fixer as root-cause bugs. (2026-11-14)
- PR31 / G24. Only the deploy agent runs go vet/test and image builds; fixers hand over "code ready". The deploy agent may stop
  stray golang build/test containers that block a deploy, never rathena, Ayla, legion or the db. (2026-11-14)
- PR36. (from the approved redesign brief, not an owner quote) Scripts do the watching (sentinel, partycheck, league analyst, dispatcher and sentinel heartbeats). An AI agent is
  woken only by a violation, an outage, a Neo request, or a scheduled review. A script reports problems into the bug queue.
  (2026-11-14, replaces the always-running watcher, QA and watchdog agents)
- PR24. Every agent that sees a bot misbehave (stuck, looping, wrong map, unpaid deal, ignored help call, broken API) sends a
  short bug report with names, map, time and evidence at once (through the bug queue, `legionctl bug`, which arrives with the
  puppet package), then carries on; main hands it to a fixer. (2026-11-14)
- PR18. Ayla is the test system, so a dedicated agent observes her party and reports bugs. Scripted checks (`partycheck`) run every
  minute; an AI watcher is woken by violations and for a periodic review. Restart it whenever it stops. (2026-11-14, restated)
- PR22. The main session keeps an up-to-date list of open questions only the owner can answer and raises it under its own
  heading in every update until answered. (2026-11-14)

## World and goals
- PR04. Society model: most bots run their routine; roles come from guild leaders and market demand; demand comes from each
  bot's need to reach its ultimate build; the class engine varies the builds; guild leaders look after members; Ayla's
  party is the test system. (2026-11-14)
- PR08. The whole system keeps running without AI agents. AI adds direction and words, and is never required. (2026-11-14)
- PR09. Every bot has one goal (its ultimate build) and works toward it through the engine without AI. Puppet masters only
  influence priorities (where to farm, what to prioritise). (2026-11-14)
- PR16. Whatever is learned from Ayla's party goes into the shared engine code and is verified on bots outside her party.
  (2026-11-13)
- PR07. The two key metrics per bot are kills per minute and zeny per hour; system improvement work targets them. (2026-11-14)
- PR06. Agents and fixes are measured by numbers (economy growth, productivity, open issues, stuck rate, deaths) and
  adjusted when the numbers do not improve. (2026-11-14)
- PR26. Main priority: Ayla's party running well. (2026-11-13)

## How to work
- PR05. Spend LLM tokens only where judgement or words are needed. Most agents run on Haiku; the model of the main session
  is for the owner to decide. (2026-11-14)
- PR17. Observe, find problems and fix them without waiting for the owner. Probe classes (`tools/legion/classprobe.py`)
  after every change. (2026-11-14)
- PR25. Self-improving loop: observe, wake, fix the cause, record the lesson in `legion/data/lessons.md`, observe. (2026-11-14)
- PR27. If something keeps failing, stop retrying and investigate the root cause. (2026-11-13)
- PR28. Say a fix works only after data shows it (probe, logs, screenshots). (2026-11-13)
- AY08. Every agent looks up game data (`legionctl drops`, `mob`, `mapinfo`) before choosing where to farm or answering a drop
  question; never "I could not find the data". (2026-11-14)
- PR21. Class and build rules live in `docs/classes/`; the build/class master keeps them current. (2026-11-14)
- PR35. Write everything down for the next AI in `HANDOVER.md`, and keep the rule files current. (2026-11-13)
- PR13. The AI harness lives in the git repository `G:\RO\legion`. (2026-11-14)

## Recording rules and deploys
- PR02. When the owner states a rule ("must", "always", "never", "should", "apply to all ..."), the same turn: one short line
  in the right file under `docs/rules/` (class rules in `docs/classes/`), the owner's words with the date in `archive.md`
  under the same ID, and a row in `coverage.md`; a new hard gate also gets a G ID in `gates.md` and the core. A new rule that
  replaces an old one edits the old line. (2026-11-14)
- PR01. Rules are short plain lines with an ID and a date. They never repeat the owner's prompt. (2026-11-14)
- G15 / G16 / G17. Deploy rules: legion/legion-core only with `--no-deps`; rathena restart at most once an hour; deploys every
  3 hours as one batch assigned by the main session, run by the deploy agent only, for both legion and legion-core (critical fixes
  excepted); working subagents report "built" and never deploy (PR32). Details in `gates.md`; the deploy scripts are described in `docs/handover/startup-old.md` (0.4 and 0.5) and `tools/deploy/`.
- Engineering notes that behave like rules (Windows git, engine flags default off, lessons about locks): `docs/design/REDESIGN.md` section 2.

## Dashboard
- DB01. The dashboard shows world data, the minds (the owner can talk to them there), guilds, and parties with each member's HP, SP
  and zeny. (2026-11-14)
- DB02. The owner can close and reopen the mind chats. (2026-11-14)
