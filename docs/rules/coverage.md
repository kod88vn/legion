# Rule coverage table (inventory, step 1)

One row per rule bullet in the old `RULES.md` and per owner rule in the old `CLASSES.md` (before the trim).
Kind: hard gate, behaviour spec, process, server setting, replaced. New location and status are filled in at the
end of the trim (step 7). Old line numbers refer to the files as of commit 63ca923.

| ID | Name | Kind | Old location | New location | Status |
|---|---|---|---|---|---|
| PR01 | Short rules, no repeated prompts | process | RULES 0 hdr (l.6) | (later) | (later) |
| PR02 | Recording rule for new rules | process | RULES 0 hdr (l.10) | (later) | (later) |
| PR03 | Goal: Matrix-like simulation, judge by normal human behaviour | behaviour spec | RULES 0 (l.17) | (later) | (later) |
| PR04 | Society model: routines, roles from leaders and market | behaviour spec | RULES 0 (l.21) | (later) | (later) |
| PR05 | Token balance; most agents Haiku | process | RULES 0 (l.27) | (later) | (later) |
| PR06 | Agents improve over time, measured by metrics | process | RULES 0 (l.33) | (later) | (later) |
| PR07 | Two key metrics per bot: kills/minute, zeny/hour | process | RULES 0 (l.37) | (later) | (later) |
| PR08 | The system runs without AI agents | behaviour spec | RULES 0 (l.40) | (later) | (later) |
| PR09 | Every bot has one goal and works toward it without AI | behaviour spec | RULES 0 (l.43) | (later) | (later) |
| PR10 | Two classes of agents: puppet masters, system agents | process | RULES 0 (l.47) | (later) | (later) |
| PR11 | Main session listens and assigns; no coding; all work by subagents | hard gate | RULES 0 (l.51, l.111) | (later) | (later) |
| PR12 | Puppet masters run as own agents outside main session | process | RULES 0 (l.54) | (later) | (later) |
| PR13 | Harness lives in git folder legion | process | RULES 0 (l.56) | (later) | (later) |
| PR14 | Standing setup checklist (loops, 15 minds, standing agents) | replaced | RULES 0 (l.60) | (later) | (later) |
| PR15 | Agent system maintained at all times (heartbeats, watchdog) | replaced | RULES 0 (l.70) | (later) | (later) |
| PR16 | Lessons from Ayla's party apply to the whole Legion | behaviour spec | RULES 0 (l.76) | (later) | (later) |
| PR17 | Observe, find problems, fix them; classprobe after changes | process | RULES 0 (l.80) | (later) | (later) |
| PR18 | Party-watcher agent always on | replaced | RULES 0 (l.83) | (later) | (later) |
| PR19 | Class-QA agent always on | replaced | RULES 0 (l.87) | (later) | (later) |
| PR20 | Build-and-strategy agent always on | replaced | RULES 0 (l.90) | (later) | (later) |
| PR21 | CLASSES.md is the build reference | process | RULES 0 (l.94) | (later) | (later) |
| PR22 | Keep highlighting owner decisions | process | RULES 0 (l.97) | (later) | (later) |
| PR23 | Main thread never controls Ayla | hard gate | RULES 0 (l.101) | (later) | (later) |
| PR24 | Minds report bugs to main | process | RULES 0 (l.106) | (later) | (later) |
| PR25 | Self-improving loop | process | RULES 0 (l.110) | (later) | (later) |
| PR26 | Main priority: Ayla's party running well | process | RULES 0 (l.111) | (later) | (later) |
| PR27 | If something keeps failing, investigate | process | RULES 0 (l.115) | (later) | (later) |
| PR28 | Measure before claiming | process | RULES 0 (l.116) | (later) | (later) |
| PR29 | Deploys never recreate rathena by accident; Ayla follows rathena | hard gate | RULES 0 (l.117) | (later) | (later) |
| PR30 | Rathena restart at most once an hour | hard gate | RULES 0 (l.121) | (later) | (later) |
| PR31 | Deploy agent may stop stray build/test containers | process | RULES 0 (l.123) | (later) | (later) |
| PR32 | Deploys every 3 hours (critical excepted); core deploys too | hard gate | RULES 0 (l.126) | (later) | (later) |
| PR33 | Do not waste money on travel | behaviour spec | RULES 0 (l.131) | (later) | (later) |
| PR34 | LLM minds are the core, always alive (mindgate) | replaced | RULES 0 (l.132) | (later) | (later) |
| PR35 | Write everything down in HANDOVER.md | process | RULES 0 (l.135) | (later) | (later) |
| PR36 | System agents: scripts watch, agents woken by violations | process | REDESIGN 1 and 3S (new) | (later) | (later) |
| SA01 | Never drive a client with the real mouse or keyboard | hard gate | RULES 1 (l.138) | (later) | (later) |
| SA02 | No fullscreen for the owner's client | hard gate | RULES 1 (l.140) | (later) | (later) |
| SA03 | No GM account for watching | hard gate | RULES 1 (l.141) | (later) | (later) |
| SA04 | GM test account allowed; stays out of economy, parties, public chat | hard gate | RULES 1 (l.142) | (later) | (later) |
| SA05 | Never turn Ayla's AI off | hard gate | RULES 1 (l.146) | (later) | (later) |
| SA06 | Re-seeds never touch human characters | hard gate | RULES 1 (l.149) | (later) | (later) |
| SA07 | No anti-cheat bypass, pirated clients, unpacking protected executables | hard gate | RULES 1 (l.150) | (later) | (later) |
| SA08 | Never remove dashboard information | hard gate | RULES 1 (l.151) | (later) | (later) |
| ST01 | Server caps at transcendent classes (server config, no 3rd jobs) | hard gate | RULES 2 (l.154) | (later) | (later) |
| EC01 | Real progression from level 1 Novice | behaviour spec | RULES 2 (l.159) | (later) | (later) |
| EC02 | Wrong build = delete and restart (bot accounts only) | hard gate | RULES 2 (l.161) | (later) | (later) |
| EC03 | Mages/Wizards are AoE builds (pointer) | behaviour spec | RULES 2 (l.163) | (later) | (later) |
| EC04 | Level, do not march; Kafra teleport for long trips | behaviour spec | RULES 2 (l.164) | (later) | (later) |
| EC05 | Save respawn point at the Kafra nearest the grinding ground | behaviour spec | RULES 2 (l.166) | (later) | (later) |
| EC06 | Beginners stay near home town | behaviour spec | RULES 2 (l.171) | (later) | (later) |
| EC07 | All shops at Prontera south gate (market place) | server setting | RULES 2 (l.173) | (later) | (later) |
| EC08 | Gear: farm or buy, ask in #trade; real economy | behaviour spec | RULES 2 (l.174) | (later) | (later) |
| EC09 | Bots prefer farming; open shops to sell surplus | behaviour spec | RULES 2 (l.176) | (later) | (later) |
| EC10 | No items added to NPC shops; the economy is the core | hard gate | RULES 2 (l.177) | (later) | (later) |
| EC11 | Every guild eventually has an Alchemist who brews potions | behaviour spec | RULES 2 (l.177) | (later) | (later) |
| EC12 | Gear for classes that cannot farm (alts, leader buys) | behaviour spec | RULES 2 (l.180) | (later) | (later) |
| EC13 | Economy knows what every build wants | behaviour spec | RULES 2 (l.183) | (later) | (later) |
| EC14 | Prices follow server wealth and rarity; supply and demand; markdown when unsold | behaviour spec | RULES 2 (l.187) | (later) | (later) |
| EC15 | Abundant and cheap; Prontera market beats NPCs | behaviour spec | RULES 2 (l.196) | (later) | (later) |
| EC16 | Bots buy from NPCs as little as possible; Prontera full of gear | behaviour spec | RULES 2 (l.203) | (later) | (later) |
| EC17 | Farmer accounts: solo item farmers who vend | behaviour spec | RULES 2 (l.207) | (later) | (later) |
| EC18 | Merchant + DS Hunter pair on every account | behaviour spec | RULES 2 (l.213) | (later) | (later) |
| EC19 | Boss items cost a guild's fortune; boss hunts only when fully built | behaviour spec | RULES 2 (l.218) | (later) | (later) |
| EC20 | Bots keep upgrading gear | behaviour spec | RULES 2 (l.225) | (later) | (later) |
| EC21 | Farm what sells; sell useful items, not junk | behaviour spec | RULES 2 (l.227) | (later) | (later) |
| EC22 | Plain gear under 100z is junk | behaviour spec | RULES 2 (l.231) | (later) | (later) |
| EC23 | Interesting loot: use, party, guild, guild stock, then sell | behaviour spec | RULES 2 (l.234) | (later) | (later) |
| EC24 | Never dump important items at an NPC | behaviour spec | RULES 2 (l.241) | (later) | (later) |
| EC25 | 90% of the time is farming | behaviour spec | RULES 2 (l.243) | (later) | (later) |
| EC26 | Town runs use Butterfly Wing; stock basic utilities | behaviour spec | RULES 2 (l.245) | (later) | (later) |
| EC27 | Town runs only for a real need | behaviour spec | RULES 2 (l.249) | (later) | (later) |
| EC28 | 50% weight triggers a town run | behaviour spec | RULES 2 (l.251) | (later) | (later) |
| EC29 | SP users stock SP potions; healers few HP potions | behaviour spec | RULES 2 (l.253) | (later) | (later) |
| EC30 | Bots run away from danger; Fly Wing escape stays on | behaviour spec | RULES 2 (l.256) | (later) | (later) |
| EC31 | HP restore items first in the market | behaviour spec | RULES 2 (l.257) | (later) | (later) |
| EC32 | Bots read the Prontera market to decide what to farm | behaviour spec | RULES 2 (l.262) | (later) | (later) |
| EC33 | Bots share market findings in natural talk | behaviour spec | RULES 2 (l.267) | (later) | (later) |
| EC34 | Gluts redirect farming | behaviour spec | RULES 2 (l.272) | (later) | (later) |
| ST02 | Stock NPC shops stay as shipped | server setting | RULES 2 (l.276) | (later) | (later) |
| ST03 | Butterfly Wing NPC price lowered | server setting | RULES 2 (l.279) | (later) | (later) |
| ST04 | Base/job EXP rate 5x | server setting | RULES 2 (l.281) | (later) | (later) |
| ST05 | Drop rates 5x, all items | server setting | RULES 2 (l.283) | (later) | (later) |
| ST06 | Server capacity as is (about 1024 connections, 960 bot slots) | server setting | RULES 2 (l.286) | (later) | (later) |
| PT01 | Parties come from need, not permanence (Ayla's party too) | behaviour spec | RULES 3 (l.290) | (later) | (later) |
| PT02 | Party roles: tank, ranged, priest; bait-and-carry; EXP shared | behaviour spec | RULES 3 (l.302) | (later) | (later) |
| PT03 | Regroup at the Kafra nearest the destination and save there | behaviour spec | RULES 3 (l.303) | (later) | (later) |
| PT04 | Stay close while grinding | behaviour spec | RULES 3 (l.306) | (later) | (later) |
| PT05 | No walking back and forth; leader is decisive | behaviour spec | RULES 3 (l.308) | (later) | (later) |
| PT06 | Together means within 20 cells | behaviour spec | RULES 3 (l.311) | (later) | (later) |
| PT07 | Higher-level parties: members run to leader; tanks protect support | behaviour spec | RULES 3 (l.313) | (later) | (later) |
| PT08 | Leader never goes back and forth | behaviour spec | RULES 3 (l.322) | (later) | (later) |
| PT09 | The leader leads: protects, guides, talks | behaviour spec | RULES 3 (l.324) | (later) | (later) |
| PT10 | Leaders always communicate the plan | behaviour spec | RULES 3 (l.325) | (later) | (later) |
| PT11 | Ayla's party farms for members' gear in turns | behaviour spec | RULES 3 (l.330) | (later) | (later) |
| PT12 | Members ask for help, leader collects them | behaviour spec | RULES 3 (l.334) | (later) | (later) |
| PT13 | Tanks active, AoE on crowds (pointer) | behaviour spec | RULES 3 (l.336) | (later) | (later) |
| PT14 | Priests heal first, sit idle, max INT (pointer) | behaviour spec | RULES 3 (l.338) | (later) | (later) |
| PT15 | Sweep the map | behaviour spec | RULES 3 (l.340) | (later) | (later) |
| PT16 | Plan and stick to one map for about 2 hours | behaviour spec | RULES 3 (l.344) | (later) | (later) |
| PT17 | Start farming as soon as the healer is there | behaviour spec | RULES 3 (l.349) | (later) | (later) |
| PT18 | Farm while waiting | behaviour spec | RULES 3 (l.352) | (later) | (later) |
| PT19 | Rest together; no fidgeting | behaviour spec | RULES 3 (l.355) | (later) | (later) |
| PT20 | Announce rests and their end | behaviour spec | RULES 3 (l.360) | (later) | (later) |
| PT21 | No party sitting around; leader breather is not a party stop | behaviour spec | RULES 3 (l.364) | (later) | (later) |
| PT22 | Party town runs by Butterfly Wing; loot free for all | behaviour spec | RULES 3 (l.368) | (later) | (later) |
| PT23 | Member may leave briefly for a build/class step | behaviour spec | RULES 3 (l.372) | (later) | (later) |
| PT24 | Town runs are done together | behaviour spec | RULES 3 (l.378) | (later) | (later) |
| PT25 | The party is one mind for gear | behaviour spec | RULES 3 (l.380) | (later) | (later) |
| PT26 | Members announce what they do | behaviour spec | RULES 3 (l.383) | (later) | (later) |
| PT27 | All bots talk, leaders most | behaviour spec | RULES 3 (l.384) | (later) | (later) |
| PT28 | Friendship system | behaviour spec | RULES 3 (l.386) | (later) | (later) |
| GD01 | Guilds compete; leaders are frenemies; rank and wealth | behaviour spec | RULES 3b (l.394) | (later) | (later) |
| GD02 | Members follow guild direction about 2 h/day or risk a kick | behaviour spec | RULES 3b (l.399) | (later) | (later) |
| GD03 | Guild leaders always online, look after the guild | behaviour spec | RULES 3b (l.405) | (later) | (later) |
| GD04 | Guild leaders are the server's best players | behaviour spec | RULES 3b (l.408) | (later) | (later) |
| GD05 | Guild leaders run the supply chain and dedicated farmers | behaviour spec | RULES 3b (l.411) | (later) | (later) |
| GD06 | Every guild sets an EXP tax | behaviour spec | RULES 3b (l.419) | (later) | (later) |
| GD07 | Every guild has a merchant | behaviour spec | RULES 3b (l.422) | (later) | (later) |
| GD08 | Every bot aims for the Teleport card (Creamy) | behaviour spec | RULES 3b (l.426) | (later) | (later) |
| GD09 | Guild loyalty and mutual help | behaviour spec | RULES 3b (l.431) | (later) | (later) |
| GD10 | Natural build resets | behaviour spec | RULES 3b (l.439) | (later) | (later) |
| CT01 | Giving among party and guild; chat agent helps | behaviour spec | RULES 4 (l.445) | (later) | (later) |
| CT02 | Server feels alive; #global and #trade exist | behaviour spec | RULES 4 (l.449) | (later) | (later) |
| CT03 | #global for important things only | behaviour spec | RULES 4 (l.450) | (later) | (later) |
| CT04 | Conversation is the minds' job; templates fill in | behaviour spec | RULES 4 (l.452) | (later) | (later) |
| CT05 | Never reveal or hint that players are bots or AI | hard gate | RULES 4 (l.453) | (later) | (later) |
| AY01 | Ayla is Neo and performs better than the rest | behaviour spec | RULES 5 (l.456) | (later) | (later) |
| AY02 | Ayla's direct instant line to the system | behaviour spec | RULES 5 (l.458) | (later) | (later) |
| AY03 | Ayla and party always online | hard gate | RULES 5 (l.462) | (later) | (later) |
| AY04 | Ayla is a true party leader | behaviour spec | RULES 5 (l.463) | (later) | (later) |
| AY05 | Ayla's mind makes the grinding decisions | behaviour spec | RULES 5 (l.464) | (later) | (later) |
| AY06 | Ayla's party gears up first, every member | behaviour spec | RULES 5 (l.465) | (later) | (later) |
| AY07 | Farm the best map for party level and gear | behaviour spec | RULES 5 (l.470) | (later) | (later) |
| AY08 | Every mind can look up game data (drops, mobs, maps) | behaviour spec | RULES 5 (l.475) | (later) | (later) |
| AY09 | Ayla keeps at least 10,000 zeny | hard gate | RULES 5 (l.479) | (later) | (later) |
| AY10 | Ayla's party gets a Hunter and an Alchemist | behaviour spec | RULES 5 (l.482) | (later) | (later) |
| AY11 | Ayla's party gets a Crusader (party of 7) | behaviour spec | RULES 5 (l.485) | (later) | (later) |
| AY12 | Ayla founds guild Starveil, recruits all jobs, no Emperium | behaviour spec | RULES 5 (l.487) | (later) | (later) |
| AY13 | Ayla is a real guild leader: resources, balanced recruiting | behaviour spec | RULES 5 (l.491) | (later) | (later) |
| AY14 | Old seed bot AIBot is switched off | server setting | RULES 5 (l.496) | (later) | (later) |
| AY15 | Rotate party members through other jobs; keep the priest | behaviour spec | RULES 5 (l.498) | (later) | (later) |
| AY16 | Always close popups on Ayla's screen | behaviour spec | RULES 5 (l.499) | (later) | (later) |
| AY17 | Owner watches Ayla in the TigerVNC window | behaviour spec | RULES 5 (l.502) | (later) | (later) |
| DB01 | Dashboard shows world, minds, guilds, parties with HP/SP/zeny | behaviour spec | RULES 6 (l.505) | (later) | (later) |
| DB02 | Owner can close and reopen mind chats | behaviour spec | RULES 6 (l.506) | (later) | (later) |
| CL01 | Every class uses AoE skills on crowds, aimed at the central mob | behaviour spec | CLASSES All (l.22) | (later) | (later) |
| CL02 | Tanks are active: pull, take aggro, deal damage | behaviour spec | CLASSES All (l.28) | (later) | (later) |
| CL03 | Tanks fight a crowd with AoE instead of escaping | behaviour spec | CLASSES All (l.33) | (later) | (later) |
| CL04 | All build varieties, each with matching gear; bots perfect their build | behaviour spec | CLASSES All (l.37) | (later) | (later) |
| CL05 | Knights and Crusaders ride a Peco, top priority, official path | behaviour spec | CLASSES Swordman (l.269) | (later) | (later) |
| CL06 | Mages/Wizards are AoE builds (3+ mobs); bolt Mages grow into AoE | behaviour spec | CLASSES Mage (l.360) | (later) | (later) |
| CL07 | Wizard AoE at every chance, at the crowd centre | behaviour spec | CLASSES Mage (l.364) | (later) | (later) |
| CL08 | Parties with a Wizard lure and gather for it | behaviour spec | CLASSES Mage (l.368) | (later) | (later) |
| CL09 | Priests heal first, sit when idle; idle sitting does not stop the party | behaviour spec | CLASSES Acolyte (l.521) | (later) | (later) |
| CL10 | Acolyte/Priest builds max out INT | behaviour spec | CLASSES Acolyte (l.526) | (later) | (later) |
| CL11 | Vendor Merchants learn Vending (plan order) | behaviour spec | CLASSES Merchant (l.606) | (later) | (later) |
| CL12 | Transcendent path and cap in class form | server setting | CLASSES Trans (l.216) | (later) | (later) |
| CL13 | Arrows are basic utilities; buying arrows is an errand | behaviour spec | CLASSES Archer (l.439) | (later) | (later) |
| CL14 | Merchant: guild Alchemist brews potions (pointer) | behaviour spec | CLASSES Merchant (l.605) | (later) | (later) |
