# Rule coverage table

Every rule of the old `RULES.md` and `CLASSES.md` (as of commit 63ca923) and where it lives now. Nothing was deleted:
each ID is kept, merged, moved, or archived as replaced (with the replacing rule named). The owner's original
words for every ID are in `archive.md`. `tools/system/gatecheck.py --coverage` checks that every kept ID appears in
its new file.

Old section numbers (code comments and older notes still cite them, e.g. "RULES.md 3b", "owner rule 3", "RULES 5"):
section 0 -> `process.md`, 1 -> the hard gates (`gates.md`), 2 -> `economy.md` and `settings.md`, 3 -> `parties.md`,
3b -> `guilds.md`, 4 -> `chat.md`, 5 -> `ayla.md`, 6 -> `process.md` (DB01, DB02); class rules -> `docs/classes/`.
Find the rule by the quoted title in the Name column below.

Counts by kind: behaviour spec 109, hard gate 20, process 19, replaced 5, server setting 8, total 161.

| ID | Name | Kind | Old location | New location | Status |
|---|---|---|---|---|---|
| PR01 | Short rules, no repeated prompts | process | RULES 0 hdr (l.6) | docs/rules/process.md | kept |
| PR02 | Recording rule for new rules | process | RULES 0 hdr (l.10) | RULES.md (core) + docs/rules/process.md | kept, reworded (archive holds the owner's words) |
| PR03 | Goal: Matrix-like simulation, judge by normal human behaviour | behaviour spec | RULES 0 (l.17) | RULES.md | kept |
| PR04 | Society model: routines, roles from leaders and market | behaviour spec | RULES 0 (l.21) | docs/rules/process.md | kept |
| PR05 | Token balance; most agents Haiku | process | RULES 0 (l.27) | docs/rules/process.md | kept |
| PR06 | Agents improve over time, measured by metrics | process | RULES 0 (l.33) | docs/rules/process.md | kept |
| PR07 | Two key metrics per bot: kills/minute, zeny/hour | process | RULES 0 (l.37) | docs/rules/process.md | kept |
| PR08 | The system runs without AI agents | behaviour spec | RULES 0 (l.40) | docs/rules/process.md | kept |
| PR09 | Every bot has one goal and works toward it without AI | behaviour spec | RULES 0 (l.43) | docs/rules/process.md | kept |
| PR10 | Two classes of agents: puppet masters, system agents | process | RULES 0 (l.47) | docs/rules/process.md | kept (the added specifics, e.g. the dispatcher and one-shot agents, are from the approved redesign brief, not an owner quote) |
| PR11 | Main session listens and assigns; no coding; all work by subagents | hard gate | RULES 0 (l.51, l.111) | RULES.md as G14 | kept as hard gate |
| PR12 | Puppet masters run as own agents outside main session | process | RULES 0 (l.54) | docs/rules/process.md | kept (the added specifics, e.g. the dispatcher and one-shot agents, are from the approved redesign brief, not an owner quote) |
| PR13 | Harness lives in git folder legion | process | RULES 0 (l.56) | docs/rules/process.md | kept |
| PR14 | Standing setup checklist (loops, 15 minds, standing agents) | replaced | RULES 0 (l.60) | docs/rules/archive.md | archived as replaced by PR08, PR10, PR12 and the STATUS.md hold (G21) |
| PR15 | Agent system maintained at all times (heartbeats, watchdog) | replaced | RULES 0 (l.70) | docs/rules/archive.md | archived as replaced by the plan the owner approved on 2026-11-14 (watchdog replaced by the sentinel script and dispatcher/sentinel heartbeats; see PR08, PR36) |
| PR16 | Lessons from Ayla's party apply to the whole Legion | behaviour spec | RULES 0 (l.76) | docs/rules/process.md | kept |
| PR17 | Observe, find problems, fix them; classprobe after changes | process | RULES 0 (l.80) | docs/rules/process.md | kept (watchdog-mind part replaced by PR36) |
| PR18 | A dedicated agent watches Ayla's party and reports bugs | process | RULES 0 (l.83) | docs/rules/process.md | kept, reworded: scripted checks every minute plus an AI watcher woken by violations and for a periodic review (owner restated it 2026-11-14) |
| PR19 | Class-QA agent always on | replaced | RULES 0 (l.87) | docs/rules/archive.md | archived as replaced by the plan the owner approved on 2026-11-14 (class QA runs after each deploy and on schedule instead of all the time; see PR36) |
| PR20 | Build-and-strategy agent always on | replaced | RULES 0 (l.90) | docs/rules/archive.md | archived as replaced by the plan the owner approved on 2026-11-14 (the build/class agent is a puppet master woken on demand, PR10); its aim is kept in CL04 |
| PR21 | CLASSES.md is the build reference | process | RULES 0 (l.94) | docs/rules/process.md | kept, now docs/classes/ |
| PR22 | Keep highlighting owner decisions | process | RULES 0 (l.97) | docs/rules/process.md | kept |
| PR23 | Main thread never controls Ayla | hard gate | RULES 0 (l.101) | RULES.md as G13 | kept as hard gate |
| PR24 | Minds report bugs to main | process | RULES 0 (l.106) | docs/rules/process.md | kept (through the bug queue) |
| PR25 | Self-improving loop | process | RULES 0 (l.110) | docs/rules/process.md | kept |
| PR26 | Main priority: Ayla's party running well | process | RULES 0 (l.111) | docs/rules/process.md | kept (the observe-only part replaced by PR11) |
| PR27 | If something keeps failing, investigate | process | RULES 0 (l.115) | docs/rules/process.md | kept |
| PR28 | Measure before claiming | process | RULES 0 (l.116) | docs/rules/process.md | kept |
| PR29 | Deploys never recreate rathena by accident; Ayla follows rathena | hard gate | RULES 0 (l.117) | RULES.md as G15 | kept as hard gate |
| PR30 | Rathena restart at most once an hour | hard gate | RULES 0 (l.121) | RULES.md as G16 | kept as hard gate |
| PR31 | Deploy agent may stop stray build/test containers | hard gate | RULES 0 (l.123) | RULES.md as G24 and docs/rules/process.md | kept as hard gate (the limit on what may be stopped) |
| PR32 | Deploys every 3 hours (critical excepted); core deploys too | hard gate | RULES 0 (l.126) | RULES.md as G17 | kept as hard gate; reworded: the old 'only the main thread batches core deploys' became 'the deploy agent deploys legion and legion-core as one batch assigned by the main session', because the main session no longer does the work itself (owner rule: the main session listens and assigns). The 30-minute cadence was already replaced in place. |
| PR33 | Do not waste money on travel | behaviour spec | RULES 0 (l.131) | docs/rules/economy.md | moved |
| PR34 | LLM minds are the core, always alive (mindgate) | replaced | RULES 0 (l.132) | docs/rules/archive.md | archived as replaced by PR08 and PR10 |
| PR35 | Write everything down in HANDOVER.md | process | RULES 0 (l.135) | docs/rules/process.md | kept |
| PR36 | System agents: scripts watch, agents woken by violations | process | REDESIGN 1 and 3S (new) | docs/rules/process.md | new line from the approved redesign brief, not an owner quote; replaces the always-on parts of PR15 and PR19 |
| SA01 | Never drive a client with the real mouse or keyboard | hard gate | RULES 1 (l.138) | RULES.md as G01 | kept as hard gate |
| SA02 | No fullscreen for the owner's client | hard gate | RULES 1 (l.140) | RULES.md as G02 | kept as hard gate |
| SA03 | No GM account for watching | hard gate | RULES 1 (l.141) | RULES.md as G03 | kept as hard gate |
| G03b | Owner GM account allowed for his own wandering | hard gate | owner 2026-11-14 | RULES.md G03 | amended |
| SA04 | GM test account allowed; stays out of economy, parties, public chat | hard gate | RULES 1 (l.142) | RULES.md as G04 | kept as hard gate |
| SA05 | Never turn Ayla's AI off | hard gate | RULES 1 (l.146) | RULES.md as G05 | kept as hard gate |
| SA06 | Re-seeds never touch human characters | hard gate | RULES 1 (l.149) | RULES.md as G06 | kept as hard gate |
| SA07 | No anti-cheat bypass, pirated clients, unpacking protected executables | hard gate | RULES 1 (l.150) | RULES.md as G07 | kept as hard gate |
| SA08 | Never remove dashboard information | hard gate | RULES 1 (l.151) | RULES.md as G08 | kept as hard gate |
| ST01 | Server caps at 2nd classes, no rebirth (amended 2026-11-15; was transcendent) | hard gate | RULES 2 (l.154) | RULES.md as G10 and docs/rules/settings.md | kept as hard gate and server setting, amended |
| EC01 | Real progression from level 1 Novice | behaviour spec | RULES 2 (l.159) | docs/rules/economy.md | kept |
| EC02 | Wrong build = delete and restart (bot accounts only) | hard gate | RULES 2 (l.161) | RULES.md as G11 | kept as hard gate |
| EC03 | Mages/Wizards are AoE builds (pointer) | behaviour spec | RULES 2 (l.163) | docs/classes/mage.md (CL06) | merged into CL06 |
| EC04 | Level, do not march; Kafra teleport for long trips | behaviour spec | RULES 2 (l.164) | docs/rules/economy.md | kept |
| EC05 | Save respawn point at the Kafra nearest the grinding ground | behaviour spec | RULES 2 (l.166) | docs/rules/parties.md | moved (enforced in engine) |
| EC06 | Beginners stay near home town | behaviour spec | RULES 2 (l.171) | docs/rules/economy.md | kept |
| EC07 | All shops at Prontera south gate (market place) | server setting | RULES 2 (l.173) | docs/rules/settings.md | moved |
| EC08 | Gear: farm or buy, ask in #trade; real economy | behaviour spec | RULES 2 (l.174) | docs/rules/economy.md | kept |
| EC09 | Bots prefer farming; open shops to sell surplus | behaviour spec | RULES 2 (l.176) | docs/rules/economy.md | kept |
| EC10 | No items added to NPC shops; the economy is the core | hard gate | RULES 2 (l.177) | RULES.md as G09 and docs/rules/economy.md | kept as hard gate |
| EC11 | Every guild eventually has an Alchemist who brews potions | behaviour spec | RULES 2 (l.177) | docs/rules/economy.md | kept |
| EC12 | Gear for classes that cannot farm (alts, leader buys) | behaviour spec | RULES 2 (l.180) | docs/rules/economy.md | kept |
| EC13 | Economy knows what every build wants | behaviour spec | RULES 2 (l.183) | docs/rules/economy.md | kept |
| EC14 | Prices follow server wealth and rarity; supply and demand; markdown when unsold | behaviour spec | RULES 2 (l.187) | docs/rules/economy.md | kept |
| EC15 | Abundant and cheap; Prontera market beats NPCs | behaviour spec | RULES 2 (l.196) | docs/rules/economy.md | kept |
| EC16 | Bots buy from NPCs as little as possible; Prontera full of gear | behaviour spec | RULES 2 (l.203) | docs/rules/economy.md | kept |
| EC17 | Farmer accounts: solo item farmers who vend | behaviour spec | RULES 2 (l.207) | docs/rules/economy.md | kept |
| EC18 | Merchant + DS Hunter pair on every account | behaviour spec | RULES 2 (l.213) | docs/rules/economy.md | kept |
| EC19 | Boss items cost a guild's fortune; boss hunts only when fully built | behaviour spec | RULES 2 (l.218) | docs/rules/economy.md | kept |
| EC20 | Bots keep upgrading gear | behaviour spec | RULES 2 (l.225) | docs/rules/economy.md | kept |
| EC21 | Farm what sells; sell useful items, not junk | behaviour spec | RULES 2 (l.227) | docs/rules/economy.md | kept |
| EC22 | Plain gear under 100z is junk | behaviour spec | RULES 2 (l.231) | docs/rules/economy.md | kept |
| EC23 | Interesting loot: use, party, guild, guild stock, then sell | behaviour spec | RULES 2 (l.234) | docs/rules/economy.md | kept |
| EC24 | Never dump important items at an NPC | behaviour spec | RULES 2 (l.241) | docs/rules/economy.md | kept |
| EC25 | 90% of the time is farming | behaviour spec | RULES 2 (l.243) | docs/rules/economy.md | kept |
| EC26 | Town runs use Butterfly Wing; stock basic utilities | behaviour spec | RULES 2 (l.245) | docs/rules/economy.md | kept |
| EC27 | Town runs only for a real need | behaviour spec | RULES 2 (l.249) | docs/rules/economy.md | kept |
| EC28 | 50% weight triggers a town run | behaviour spec | RULES 2 (l.251) | docs/rules/economy.md | kept |
| EC29 | SP users stock SP potions; healers few HP potions | behaviour spec | RULES 2 (l.253) | docs/rules/economy.md | kept |
| EC30 | Bots run away from danger; Fly Wing escape stays on | behaviour spec | RULES 2 (l.256) | docs/rules/economy.md | kept |
| EC31 | HP restore items first in the market | behaviour spec | RULES 2 (l.257) | docs/rules/economy.md | kept |
| EC32 | Bots read the Prontera market to decide what to farm | behaviour spec | RULES 2 (l.262) | docs/rules/economy.md | kept |
| EC33 | Bots share market findings in natural talk | behaviour spec | RULES 2 (l.267) | docs/rules/economy.md | kept |
| EC34 | Gluts redirect farming | behaviour spec | RULES 2 (l.272) | docs/rules/economy.md | kept |
| ST02 | Stock NPC shops stay as shipped | server setting | RULES 2 (l.276) | docs/rules/settings.md | kept |
| ST03 | Butterfly Wing NPC price lowered | server setting | RULES 2 (l.279) | docs/rules/settings.md | kept |
| ST04 | Base/job EXP rate 5x | server setting | RULES 2 (l.281) | docs/rules/settings.md | kept |
| ST05 | Drop rates 5x, all items | server setting | RULES 2 (l.283) | docs/rules/settings.md | kept |
| ST06 | Server capacity as is (about 1024 connections, 960 bot slots) | server setting | RULES 2 (l.286) | docs/rules/settings.md | kept |
| PT01 | Parties come from need, not permanence (Ayla's party too) | behaviour spec | RULES 3 (l.290) | docs/rules/parties.md | kept |
| PT02 | Party roles: tank, ranged, priest; bait-and-carry; EXP shared | behaviour spec | RULES 3 (l.302) | docs/rules/parties.md | kept |
| PT03 | Regroup at the Kafra nearest the destination and save there | behaviour spec | RULES 3 (l.303) | docs/rules/parties.md | kept |
| PT04 | Stay close while grinding | behaviour spec | RULES 3 (l.306) | docs/rules/parties.md | kept |
| PT05 | No walking back and forth; leader is decisive | behaviour spec | RULES 3 (l.308) | docs/rules/parties.md | kept |
| PT06 | Together means within 20 cells | behaviour spec | RULES 3 (l.311) | docs/rules/parties.md | kept |
| PT07 | Higher-level parties: members run to leader; tanks protect support | behaviour spec | RULES 3 (l.313) | docs/rules/parties.md | kept |
| PT08 | Leader never goes back and forth | behaviour spec | RULES 3 (l.322) | docs/rules/parties.md | kept |
| PT09 | The leader leads: protects, guides, talks | behaviour spec | RULES 3 (l.324) | docs/rules/parties.md | kept |
| PT10 | Leaders always communicate the plan | behaviour spec | RULES 3 (l.325) | docs/rules/parties.md | kept |
| PT11 | Ayla's party farms for members' gear in turns | behaviour spec | RULES 3 (l.330) | docs/rules/ayla.md | moved |
| PT12 | Members ask for help, leader collects them | behaviour spec | RULES 3 (l.334) | docs/rules/parties.md | kept |
| PT13 | Tanks active, AoE on crowds (pointer) | behaviour spec | RULES 3 (l.336) | docs/classes/all.md (CL01 to CL03) | merged into CL01, CL02, CL03 |
| PT14 | Priests heal first, sit idle, max INT (pointer) | behaviour spec | RULES 3 (l.338) | docs/classes/acolyte.md (CL09, CL10) | merged into CL09, CL10 |
| PT15 | Sweep the map | behaviour spec | RULES 3 (l.340) | docs/rules/parties.md | kept |
| PT16 | Plan and stick to one map for about 2 hours | behaviour spec | RULES 3 (l.344) | docs/rules/parties.md | kept |
| PT17 | Start farming as soon as the healer is there | behaviour spec | RULES 3 (l.349) | docs/rules/parties.md | kept |
| PT18 | Farm while waiting | behaviour spec | RULES 3 (l.352) | docs/rules/parties.md | kept |
| PT19 | Rest together; no fidgeting | behaviour spec | RULES 3 (l.355) | docs/rules/parties.md | kept |
| PT20 | Announce rests and their end | behaviour spec | RULES 3 (l.360) | docs/rules/parties.md | kept |
| PT21 | No party sitting around; leader breather is not a party stop | behaviour spec | RULES 3 (l.364) | docs/rules/parties.md | kept |
| PT22 | Party town runs by Butterfly Wing; loot free for all | behaviour spec | RULES 3 (l.368) | docs/rules/parties.md | kept |
| PT23 | Member may leave briefly for a build/class step | behaviour spec | RULES 3 (l.372) | docs/rules/parties.md | kept |
| PT24 | Town runs are done together | behaviour spec | RULES 3 (l.378) | docs/rules/parties.md | kept |
| PT25 | The party is one mind for gear | behaviour spec | RULES 3 (l.380) | docs/rules/parties.md | kept |
| PT26 | Members announce what they do | behaviour spec | RULES 3 (l.383) | docs/rules/parties.md | kept |
| PT27 | All bots talk, leaders most | behaviour spec | RULES 3 (l.384) | docs/rules/parties.md | kept |
| PT28 | Friendship system | behaviour spec | RULES 3 (l.386) | docs/rules/parties.md | kept |
| GD01 | Guilds compete; leaders are frenemies; rank and wealth | behaviour spec | RULES 3b (l.394) | docs/rules/guilds.md | kept |
| GD02 | Members follow guild direction about 2 h/day or risk a kick | behaviour spec | RULES 3b (l.399) | docs/rules/guilds.md | kept |
| GD03 | Guild leaders always online, look after the guild | behaviour spec | RULES 3b (l.405) | docs/rules/guilds.md | kept (Haiku-mind driver replaced by puppet-master wording) |
| GD04 | Guild leaders are the server's best players | behaviour spec | RULES 3b (l.408) | docs/rules/guilds.md | kept |
| GD05 | Guild leaders run the supply chain and dedicated farmers | behaviour spec | RULES 3b (l.411) | docs/rules/guilds.md | kept |
| GD06 | Every guild sets an EXP tax | behaviour spec | RULES 3b (l.419) | docs/rules/guilds.md | kept; 2026-11-14 owner: no human exception (G20 human part removed, new gate G25) |
| GD07 | Every guild has a merchant | behaviour spec | RULES 3b (l.422) | docs/rules/guilds.md | kept |
| GD08 | Every bot aims for the Teleport card (Creamy) | behaviour spec | RULES 3b (l.426) | docs/rules/guilds.md | kept |
| GD09 | Guild loyalty and mutual help | behaviour spec | RULES 3b (l.431) | docs/rules/guilds.md | kept |
| GD10 | Natural build resets | behaviour spec | RULES 3b (l.439) | docs/rules/guilds.md | kept |
| CT01 | Giving among party and guild; chat agent helps | behaviour spec | RULES 4 (l.445) | docs/rules/chat.md | kept |
| CT02 | Server feels alive; #global and #trade exist | behaviour spec | RULES 4 (l.449) | docs/rules/chat.md | kept |
| CT03 | #global for important things only | behaviour spec | RULES 4 (l.450) | docs/rules/chat.md | kept |
| CT04 | Conversation is the minds' job; templates fill in | behaviour spec | RULES 4 (l.452) | docs/rules/chat.md | kept (template-only fallback follows PR08) |
| CT05 | Never reveal or hint that players are bots or AI | hard gate | RULES 4 (l.453) | RULES.md as G18 | kept as hard gate |
| AY01 | Ayla is Neo and performs better than the rest | behaviour spec | RULES 5 (l.456) | docs/rules/ayla.md | kept |
| AY02 | Ayla's direct instant line to the system | behaviour spec | RULES 5 (l.458) | docs/rules/ayla.md | kept |
| AY03 | Ayla and party always online | hard gate | RULES 5 (l.462) | RULES.md as G19 | kept as hard gate |
| AY04 | Ayla is a true party leader | behaviour spec | RULES 5 (l.463) | docs/rules/ayla.md | kept |
| AY05 | Ayla's mind makes the grinding decisions | behaviour spec | RULES 5 (l.464) | docs/rules/ayla.md | kept |
| AY06 | Ayla's party gears up first, every member | behaviour spec | RULES 5 (l.465) | docs/rules/ayla.md | kept |
| AY07 | Farm the best map for party level and gear | behaviour spec | RULES 5 (l.470) | docs/rules/ayla.md | kept |
| AY08 | Every mind can look up game data (drops, mobs, maps) | behaviour spec | RULES 5 (l.475) | docs/rules/process.md | moved (applies to all agents) |
| AY09 | Ayla keeps at least 10,000 zeny | hard gate | RULES 5 (l.479) | RULES.md as G12 | kept as hard gate |
| AY10 | Ayla's party gets a Hunter and an Alchemist | behaviour spec | RULES 5 (l.482) | docs/rules/ayla.md | merged into AY11 roster line |
| GD06 | Owner's GM account wander joins guild Starveil (2026-11-15) | behaviour spec | RULES 5 | docs/rules/ayla.md | kept |
| AY11 | Ayla's party of 5: Ayla, Priest, Crusader, Hunter, Wizard (2026-11-15) | behaviour spec | RULES 5 (l.485) | docs/rules/ayla.md | replaced |
| AY12 | Ayla founds guild Starveil, recruits all jobs, no Emperium | behaviour spec | RULES 5 (l.487) | docs/rules/ayla.md | kept |
| AY13 | Ayla is a real guild leader: resources, balanced recruiting | behaviour spec | RULES 5 (l.491) | docs/rules/ayla.md | kept |
| AY14 | Old seed bot AIBot is switched off | server setting | RULES 5 (l.496) | docs/rules/settings.md | moved |
| AY15 | No member rotation in Ayla's party (rotation experiment stopped 2026-11-15) | behaviour spec | RULES 5 (l.498) | docs/rules/ayla.md | replaced |
| AY16 | Always close popups on Ayla's screen | behaviour spec | RULES 5 (l.499) | docs/rules/ayla.md | kept |
| AY17 | Owner watches Ayla in the TigerVNC window | behaviour spec | RULES 5 (l.502) | docs/rules/ayla.md | replaced (see the 2026-11-14 AY17 row) |
| DB01 | Dashboard shows world, minds, guilds, parties with HP/SP/zeny | behaviour spec | RULES 6 (l.505) | docs/rules/process.md | moved |
| EC37 | Junk judged by value per weight across the whole Legion; keep high-value items to sell (2026-11-15) | behaviour spec | RULES 4 | docs/rules/economy.md | kept |
| PT33 | Town-run requests answered; a member at 50% weight goes to town even if the leader is silent (2026-11-15) | behaviour spec | RULES 4 | docs/rules/parties.md | kept |
| AY23 | Ayla collects her group: 10 s wait, then go back; no bouncing; town-run members left (2026-11-15) | behaviour spec | RULES 5 | docs/rules/ayla.md | kept |
| PT32b | Party members catch up with the leader first, then engage monsters (2026-11-15) | behaviour spec | RULES 4 | docs/rules/parties.md | kept |
| DB04 | Retired agents under a collapsed Retired section, nothing removed (2026-11-15) | behaviour spec | RULES 6 | docs/rules/process.md | kept |
| DB02 | Owner can close and reopen mind chats | behaviour spec | RULES 6 (l.506) | docs/rules/process.md | moved |
| CL01 | Every class uses AoE skills on crowds, aimed at the central mob | behaviour spec | CLASSES All (l.22) | docs/classes/all.md | kept |
| CL02 | Tanks are active: pull, take aggro, deal damage | behaviour spec | CLASSES All (l.28) | docs/classes/all.md | kept |
| CL03 | Tanks fight a crowd with AoE instead of escaping | behaviour spec | CLASSES All (l.33) | docs/classes/all.md | kept |
| CL04 | All build varieties, each with matching gear; bots perfect their build | behaviour spec | CLASSES All (l.37) | docs/classes/all.md | kept |
| CL15 | The Legion's builds must be varied (a realistic spread of builds per class) | behaviour spec | CLASSES All (l.114) | docs/classes/all.md | kept |
| CL05 | Knights and Crusaders ride a Peco, top priority, official path | behaviour spec | CLASSES Swordman (l.269) | docs/classes/swordman.md | kept |
| CL06 | Mages/Wizards are AoE builds (3+ mobs); bolt Mages grow into AoE | behaviour spec | CLASSES Mage (l.360) | docs/classes/mage.md | kept |
| CL07 | Wizard AoE at every chance, at the crowd centre | behaviour spec | CLASSES Mage (l.364) | docs/classes/mage.md | kept |
| CL08 | Parties with a Wizard lure and gather for it | behaviour spec | CLASSES Mage (l.368) | docs/classes/mage.md | kept |
| CL09 | Priests heal first, sit when idle; idle sitting does not stop the party | behaviour spec | CLASSES Acolyte (l.521) | docs/classes/acolyte.md | kept |
| CL10 | Acolyte/Priest builds max out INT | behaviour spec | CLASSES Acolyte (l.526) | docs/classes/acolyte.md | kept |
| CL11 | Vendor Merchants learn Vending (plan order) | behaviour spec | CLASSES Merchant (l.606) | docs/classes/merchant.md | kept |
| CL12 | Transcendent path and cap in class form | server setting | CLASSES Trans (l.216) | docs/classes/transcendent.md | merged into ST01 (class data kept) |
| CL13 | Arrows are basic utilities; buying arrows is an errand | behaviour spec | CLASSES Archer (l.439) | docs/classes/archer.md | merged into EC26 and PT23 |
| CL14 | Merchant: guild Alchemist brews potions (pointer) | behaviour spec | CLASSES Merchant (l.605) | docs/classes/merchant.md | merged into EC11 |
| PR37 | All systems on whenever the world runs | process | owner 2026-11-14 | docs/rules/process.md | voided 2026-11-15 (shutdown, restart from the constitution); G21, PR36, DB04 amended to fit the constitution |
| AY18 | Ayla has zero downtime, never idle or stuck | ayla | owner 2026-11-14 | docs/rules/ayla.md | new rule |
| AY19 | Ayla plays like a normal bot without any AI; the mind only adds | ayla | owner 2026-11-14 | docs/rules/ayla.md | new rule |
| EC35 | Shop merchants occasionally announce rare items in #trade | economy | owner 2026-11-14 | docs/rules/economy.md | new rule |
| PR38 | Code decides; AI observes and improves code; goal: only puppet masters, once a day | process | owner 2026-11-14 | docs/rules/process.md | new rule |
| AY05 | Ayla controlled by her Haiku again (map, rally, party commands), code runs underneath; keep OpenKore running | ayla | owner 2026-11-14 | docs/rules/ayla.md | replaced |
| PT29 | Members ask for buffs; priest buffs unless low SP, never passive | parties | owner 2026-11-14 | docs/rules/parties.md | new rule |
| PR39 | Every system agent/script/watchdog files every problem it sees into the bug queue | process | owner 2026-11-14 | docs/rules/process.md | new rule |
| EC36 | Waiting time used for errands (sell, store, restock), esp. with a near-full bag | economy | owner 2026-11-14 | docs/rules/economy.md | new rule |
| AY20 | Owner sends Ayla plain instructions; her Haiku carries them out at once | ayla | owner 2026-11-14 | docs/rules/ayla.md | new rule |
| PT31 | Members stay with the party; no solo random Teleport escapes; retreat to tank/priest, wing to party Kafra only if dying | parties | owner 2026-11-14 | docs/rules/parties.md | new rule |
| AY21 | Ayla fixes deploy immediately, never wait for the 3-hour batch | ayla | owner 2026-11-14 | docs/rules/ayla.md | new rule |
| PT32 | Fight in place near the party; no running around pulling extra attackers; stay back | parties | owner 2026-11-14 | docs/rules/parties.md | new rule |
| DB03 | Owner-requested dashboard changes deploy right away | process | owner 2026-11-14 | docs/rules/process.md | new rule |
| CL17 | Healers heal themselves instead of resting for HP; rest only for SP | classes | owner 2026-11-14 | docs/classes/acolyte.md | new rule |
| PR40 | Try on Ayla's party first, watch and improve, then deploy to the entire Legion | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| PR41 | Check every rule/instruction for conflicts; full conflict review after each batch of rules | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| AY22 | Ayla waits 10 s for a lagging priest, then goes back to collect it | ayla | owner 2026-11-14 | docs/rules/ayla.md | new rule |
| CL16 | A caster with full SP most of the time is a bug; it must spend SP on spells | classes | owner 2026-11-14 | docs/classes/mage.md | new rule |
| AY17 | No watch client for Ayla; owner watches via GM account; OpenKore log in a terminal | ayla | owner 2026-11-14 | docs/rules/ayla.md | replaced |
| PT30 | Duplicate of the existing leader-plan rule (parties.md l.55-57); merged there, not a new line | parties | owner 2026-11-14 | docs/rules/parties.md | merged |
| PR41-review | 2026-11-15 conflict review applied (20 items) | process | main | docs/rules/* | amended |
| DB05 | Dashboard: server status on top, six tabs (Ayla's party, Legion, Economy, Puppet master decisions, Leaderboard, System) (2026-11-15) | behaviour spec | owner 2026-11-15 | docs/rules/process.md | new rule |
| DB06 | Dashboard page always on, also when the stack is down (2026-11-15) | behaviour spec | owner 2026-11-15 | docs/rules/process.md | new rule |
| DB07 | Dashboard tab to inspect any player in the database, read-only (2026-11-15) | behaviour spec | owner 2026-11-15 | docs/rules/process.md | new rule |
| DB08 | Dashboard shows everything on disk when the stack is down (2026-11-15) | behaviour spec | owner 2026-11-15 | docs/rules/process.md | new rule |
| EC39 | Build milestones per level range (stats, gear, skills); map choice gated by meeting the gear milestone (2026-11-15) | economy | owner 2026-11-15 | docs/rules/economy.md | new rule |
| EC38 | Bots take one middle step of their end goal at a time; keep it; show the end goal (2026-11-15) | economy | owner 2026-11-15 | docs/rules/economy.md | new rule |
| DB09 | Dashboard Builds tab: end goal per class/build with items, stats, skills and playstyle (solo, party, MVP, PvP) (2026-11-15) | behaviour spec | owner 2026-11-15 | docs/rules/process.md | new rule |
| PR45 | Haiku implements, Sonnet reviews (2026-11-15) | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| PT35 | Wider level band for forming parties (2026-11-15) | parties | owner 2026-11-15 | docs/rules/parties.md | new rule |
| PR44 | Verify every change at once; commit as soon as verified (2026-11-15) | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| PT34 | Off the roster means leave the in-game party right away (2026-11-15) | parties | owner 2026-11-15 | docs/rules/parties.md | new rule |
| ST07 | rathena chat logging on, for @partyspy (2026-11-15) | settings | owner 2026-11-15 | docs/rules/settings.md | new rule |
| PR43 | Every main-session reply ends with its pending tasks (2026-11-15) | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| PR42 | Nothing is written twice; the constitution is read first and once, never restated (2026-11-15) | process | owner 2026-11-15 | docs/rules/process.md | new rule |
| Constitution | Highest law, three articles (main session, the Matrix world, the self-improving loop); every agent reads it in full; G13, PR23 and AY21 amended to fit (2026-11-15) | constitution | owner 2026-11-15 | CONSTITUTION.md | new, above all rules |

Changes after the trim: 2026-11-14 the owner removed the human part of G20 and added gate G25 (no in-world
discrimination between humans and other players); GD06 has no human exception. Owner's words in `archive.md` (G20, G25, GD06).
