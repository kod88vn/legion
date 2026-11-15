# life mind - restart state (2026-11-14 ~21:37 UTC)

Run 12 finished its 60 cycles. Run 13 stopped at cycle 19 of 60 because the owner asked to save state.
To resume, start a new life run (60 cycles). Read `memory` before every line and send bug reports to main.

## Tools
- `python tools/mem.py "<name>" ...` (run from `G:\RO\server`): compact memory for each name - class, level, map,
  what it's doing, party, guild, zeny, typing style, people it knows (beyond strangers and partymates), last 6 diary
  lines. Run it before anyone speaks.
- `bash tools/lifecycle.sh [wait_s]`: one cycle. It waits, then prints orders, the inbox, the /api/stats HTTP code
  and notable events (deaths, job changes, cards, refines, big sales, level 60+, Eddga, friendships, guild joins).
- Since about 20:19, /api/stats and /api/bots time out, so `status` and `nearby` don't work. events, memory, say,
  party-say, whisper, parties, think, orders and /api/friends still work. Memory lookups were flaky right after the
  ~21:36 restart.

## Open storylines (callbacks for the next run)
- **Nekoz pt** (Neko_z Monk 72, Mint chan Hunter 67): Mint chan died on pay_fild10 right after levelling, twice
  (Greatest General at 21:11, Savage at 21:26). Running joke: "that map doesn't like you".
- **Dark queen8 pt** (Rogue 76, Neko.fang Alch 73): Eddga killed Neko.fang at 21:22. Neko.fang and ryuBuffs
  (Priest 75) are friends and trade news by whisper. Dark queen8 moved to prt_fild09; Neko.fang walked back to
  pay_fild10.
- **suncat82 pt**: friendly level race. Rin.cat (83, Starveil) passed suncat82 (82, founded Dawnbreakers at 20:45).
- **Silentstorm pt**: Silent.storm (Assassin) and ryu Aco (Priest) changed jobs in the same minute. ryu Aco then died
  and EOF'd at 21:32.
- **Zero monk pt** (Monk 75): priest Thornsong was stranded on pay_fild10 and died to a Savage at 21:27; he said he'd
  come to prt_fild09.
- **Oniheart pt**: Oni_heart moved the party to prt_fild09 after a Beetle King death. Priest Raven_blade (joined
  Crimson Oath) said he'd follow.
- **LilEchodancer pt**: the leader is on pay_fild10 while members die alone in pay_dun01 (silverdragon 21:21,
  TheSilentchan 21:23 and 21:33). The party was told to meet at the Payon Kafra.
- **Thorn sage pt**: echostorm and Echogirl both became Hunters a minute apart ("twins"). echostorm thanked his
  friend LilRyustorm13 for the zeny that kept him going.
- **Zen claw pt**: LilSilenthunter promised Oni.song he'd buy the Battle Axe the party funded. He left the party to
  hunt a Peco card instead and died.
- Smaller threads:
  - Iron.x is still hunting the Horn card. He's friends with Nova.blade (Peco money) and with Mint.blade (now a Monk).
  - Kira.hunter got 1300z of Peco money from LilDarksenpai.
  - Kimchi.smith45 joined Starveil and became a Sage.
  - Lunasmith (Knight 75) has only 273z.
  - TheShadowdragon invited his friend LilSilverrogue to Iron Wolves (friend score 45); no scene run yet.
  - Kaiboy and xXFrostroseXx became acquaintances at the Alberta Kafra.

## Bug reports sent to main (latest first)
- **EOF disconnect right after a job change:** TheHanablade13 became a Crusader at 21:30:31 and logged off (EOF)
  at 21:31. Wolf Bless became a Priest at 21:33:26 and logged off (EOF) at 21:34. ryu Aco died on gef_fild02 at
  21:32 and logged off (EOF) the same minute.
- **LilEchodancer pt stranding:** members left alone in pay_dun01 die while the leader farms pay_fild10.
- **Eddga on pay_fild10 for over an hour** (first seen 20:20): parties keep choosing the map. About 10 bots
  fly-winged away; Neko.fang was killed by it at 21:22. There are other deaths there too.
- **pay_fild03 (passive, lv1-8 monsters) kills bots:** Sun.monk (Acolyte 26) died "fighting Creamy", which doesn't
  spawn there. Several beginners died too.
- **Duplicate death events:** 2-3 "died" lines for one death in the same second.
- **Restocks buy nothing:** Silver_heart (15.9k zeny) and Dark_blade bought 0 HP potions and 0 Butterfly Wings.
- **Party splits:** priests and members stranded on another map. The "waiting in" target flips several times a
  minute.
- **Run 12 reports:**
  - deadly maps for underleveled bots (mjolnir_08/10/12, moc_fild01/02 Peco card hunters);
  - "out of potions" town runs put off until the party run (pixelhunter, kiraknight died);
  - pooled gear money not spent (LilSilenthunter);
  - a mass map switch at 20:01:41;
  - "AIBot" listed in 357 memory files;
  - a party named "echodragon87 pt2 pt2";
  - junk Clubs listed in shops.
- **Not reported yet:** LilSilverrogue "Kafra failed in prontera at stage walk (pos 151,31, kafra 151,29)" at
  21:26, two cells from the Kafra.
