<!-- Moved unchanged from HANDOVER.md lines 1409-1485 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6h. Ayla's party of 7 and Ayla's guild Starveil (2026-11-14, `legion/humanguild.go`, `humanguildlead.go`, `guildrecruit.pl`)
- Owner: "add more members to ayla party, a hunter and an alchemy", "add a crusader to ayla party", "Ayla should also
  start a guild and recruit all different jobs", guild name "Starveil", "Make sure she also functions as a guild
  leader, do resource planning and growing members, etc. Recruit the right and balance jobs".
- Friends added (bot accounts, single-character, unguilded, wrongbuilds audit clean): **xXRinfangXx** (lg2337, Hunter
  lv72, plan `hunter`; live since 12:29), **Silent rogue** (lg0917, Alchemist lv77, plan `alchemist`, Aid Potion 5,
  Pharmacy 9, NO Potion Creation Guide yet, 77k zeny; live since 12:29), **vex_x18** (lg1222, aid 2101222, Crusader
  lv76/33, plan `crusader` = VIT shield tank: Auto Guard 10, Holy Cross 10, Faith 10, Guard + Blade - the shield
  tank next to Echobunny's two-hand Knight; needs the next main + core deploy). LEGION_CORE in `.env`, the compose
  defaults, `data/human-parties.json`, wrongbuilds.py PARTY, sentinel.py CORE, progress_report.py all list the six.
  lfg_human.go turns are generic over members. Zen.fox (in-game party leader) invites new friends.
- Roster change AY11 (2026-11-14, `legion/humanpartyroster.go`): the Monk **LilLilyblade** (lg2064, Monk lv81/38, plan
  `monk` AGI/STR combo, Heal 3, Increase AGI 10, Blessing 6, Triple Attack/Chain Combo/Combo Finish; Starveil) replaced
  Silent rogue. `data/human-parties.json` holds `members` plus `alumni` (characters that left on the owner's order):
  an alumnus still in the human's in-game party says goodbye in party chat and leaves it (both instances), and never
  leaves the human's guild (friends.go). A new member still in an old bot party says bye there and leaves it (core),
  then Zen.fox invites it. Silent rogue now runs in the main instance (`legion`) like any bot, still in Starveil.
- Automatic rotation AY15 (2026-11-15, `legion/aylarotate.go` + `legion/handoff.go`): every `ayla_rotation_hours`
  (default 3; `ayla_rotation_off`) while the party is calm (no rally, town run or fight, Ayla online) one member (never
  the healer or the in-game party leader; longest tenure first) is swapped for an online bot of the same role
  (tank/melee/ranged/caster/support), level within 5 of the party average, a class/build not in her party for 7 days,
  never back within 24 h; Starveil members first, else guildless (Ayla's recruiting invites party friends first). The
  roster is edited in place (no disband), the old member becomes an alumnus, says bye and leaves, then its account
  moves to `legion`; the new one's account moves from `legion` to `legion-core` (handoff files, see operate.md) and
  says hi on joining. First swap 3 h after the first legion-core start with this code.
- No Emperium (`guild_emperium_check: no`). Ayla founds it: `ayla.sh guild-create Starveil`. statusout.pl writes
  `guild` + `job_id` to status.json; Legion keeps the name in `data/human-guild.json`.
- Invitations by account id: plugin `openkore-ayla/plugins/cmdfile/guildrecruit.pl`, console `ginvite <aid> [name]`
  (0x0168; rAthena checks no distance; OpenKore's own `guild request` needs the player on screen). **Loading:**
  OpenKore's `plugin load X` matches X as a regex against plugin NAMES in the plugin folders (not a path), so it is
  `plugin load guildrecruit`; `loadPlugins 2` in control/sys.txt means only listed plugins load at start -
  guildrecruit is now in `loadPlugins_list`.
- Balanced recruiting (`GET /api/guild-recruits[?n=3&min_lv=&mark=1]`, `ayla.sh recruit [N]`): her party friends
  first, then the biggest role gap of the 16-member target roster (leader included): tank 3 (Knight/Crusader),
  healer 3 (Priest, Monk), aoe 3 (Wizard/Sage), ranged 3 (Hunter/Bard/Dancer), craft 1 (Blacksmith/Alchemist),
  **trader 1** (owner rule: every guild has a merchant trader - a Merchant-line vendor with Vending and an open
  south-gate shop, `isVendorTrader`; recruited right after the friends, level not required; guildstock.go then picks
  it as the "human-guild" trader), dps 2 (Assassin/Rogue); equal gaps go in that order; inside a role a job the guild lacks first, 2nd/trans before 1st
  jobs, then nearest her level (min level max(40, hers - 30)). Online, unguilded, non-leader bots only (no poaching).
- Answers: her friends accept only her guild and refuse all others; other bots accept with max(0.8, fit); a decline
  isn't suggested again for 6 h, a suggestion (mark=1) for 20 min; a new member says hello in guild chat.
- **Guild leader** (`GET /api/human-guild`, `ayla.sh guild`): members (both instances; role, level, map, party, zeny,
  spare, gear_next, gear per level, brewer/guide, in_guild), roles vs targets, role_gaps, recruits, the guild
  alchemist, the week's goal (every member +3 levels in 7 days, in a party on a fitting map, the alchemist owns the
  guide, next gear pieces bought; boss prep later; state in data/human-guild-week.json), the pool (half the online
  members' spare, 5k max each) and **actions** with exact commands: found, invite, guide (fund the alchemist's
  Potion Creation Guide), fund (the weakest-geared member's shop piece when the pool covers it), party (main-instance
  members without a party grouped within the PT35 level band on bestPartySpot -> `legionctl party ...`), carry (members 15+
  levels under the median), recruit-line (#global text of the gaps), week.
- `POST /api/human-guild/fund {"to","amount","why"}` (`ayla.sh guild-fund NAME AMOUNT WHY`): Starveil members of both
  instances mail zeny by RODEX (2% fee), each at most 5,000z and half its spare, once an hour; a guide-saving
  alchemist never donates. guildOfAccount (alchemy.go) returns "human-guild" for Starveil members and her friends, so
  the guild alchemist pick, guild fund, guild-mate trade matching and herb parcels work for Starveil too (per
  instance: main-instance members' herb parcels need a main-instance Starveil alchemist - open item).
- Guild storage needs the guild skill Guild Storage Expansion (not at guild level 1); members keep Kafra storage.
- **Starveil's trader** (`humanguildtrader.go`, owner rule "every guild has a merchant"): Starveil filled 16/16 with
  fighters, no vendor. Its trader is a member (never a party friend) with Vending + Pushcart - a vendor-role member
  first, else the highest level: today **Crimsonfang28** (Blacksmith lv70: Vending 3, Pushcart 3, Discount 3,
  Overcharge 10). It runs **shop shifts**: after 3 h of farming, when not in a party and with goods (canVend), it goes
  to Prontera, opens its shop (vend.go) for 40 min, says so in guild chat, then closes and farms again (~80% farming).
  economy.go counts it as a guild keeper, so guildstock.go picks it as the "human-guild" trader (stock, buys for the
  gaps). The pick runs in a background refresh (callers hold their b.mu). The members' account alts ("Kimchi Bazaar",
  "Mintrogue Goods", "Wolfrose") are lv1 Novices - no Vending yet; a vendor alt that joins later takes over.
- Guild fund donors give from surplus only (bug 14:46-14:58: Zen.fox, saving for its gear-turn Staff, and Silent
  rogue, short of the potion guide, were donors): guildFundLocal skips members of a party with an active gear turn,
  alchemists without the guide and guide savers, and gives at most half of (giftable zeny - own next gear piece -
  2,000z), max 5k; traders/vendors first, then the biggest surplus. /api/human-guild's "pool" uses the same rule
  (fundShare).
- Loyalty (friendship agent's loyalty.go): /api/human-guild puts its Starveil items FIRST in actions - "keep"
  (members at risk of leaving, with loyalty.go's suggested `ayla.sh guild-fund` keep gift) and "help" (open help
  requests from Starveil members: gear/pots -> guild-fund, party/rescue -> a guild-chat call). Starveil members may
  leave like anyone; only her 6 party friends are fixed.
- **Guild EXP tax and guild skills** (owner: "every guild should tax to level up", "definitely tax the guild to open
  more slots"; `legion/guildtax.go`, plugin `guildrecruit.pl`). Guild levels come only from members' taxed EXP
  (db/re/exp_guild.yml: 100k guild EXP for level 2, 400k for 3, 900k for 4 ...); each level = 1 guild skill point;
  Guild Extension = +6 members a level (16 + 6 x lv, rAthena int_guild.cpp). Bot guild masters: CZ_REQ_GUILD_MENU
  0x014f (2 positions, 3 skills) every 10 min, CZ_REG_CHANGE_GUILD_POSITIONINFO 0x0161 sets 10% on every position
  (names/permissions kept), CZ_UPGRADE_SKILLLEVEL 0x0112 spends points: Extension 10, then the Guild Storage chain
  (Approval, Guard Research, Battle Orders, Regeneration, Emergency Call, Glory of Wounds, Cold Heart, Leadership,
  Sharp Gaze, Guild Storage 5). Any member bot requests the basic info (0x0b7b) every 10 min per guild:
  `GET /api/guild-levels` (level, exp, next, members online/max, tax_set, rates, skill points/levels, first_seen for
  growth). Starveil's recruiting cap = its real max (`humanGuildMax()` from that, else 16).
  Starveil (Ayla is human): plugin commands `gtax <member%> [master%]` (saved in control/guildtax.txt, re-applied every
  10 min and after restarts), `gskill [auto|off|GD_NAME]` (auto by default: spends points every 10 min, Extension
  first), `ginfo`; ayla.sh `guild-tax`, `guild-skill`, `guild-info`.
- Founded 2026-11-14 12:42 (guild_id 11, Ayla master; Silent rogue, Zendancer84, Echobunny joined). New members learn
  their guild from the 0x0a30 name update (bot.go; rAthena sends no 0x016c on join) - needs the next deploy; until
  then members who joined before it show as unguilded in the engine until they relog.
- ayla.sh: `guild`, `recruits`, `recruit` format through tools/rogui/ayla_guild.py; `global TEXT` / `trade TEXT` post
  on the rAthena channels (OpenKore `pm "#global" ...` = channel whisper, as the bots' channelSay does); `guildchat TEXT`.

