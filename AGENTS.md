# AI-Lab Ragnarok server: agent guide

> **HOLD - REDESIGN IN PROGRESS (2026-11-14). Read `STATUS.md` first.**
> Do not start containers, background loops, minds or agents automatically. The old "bring everything back" restart
> order and the 15 always-on minds are superseded (RULES.md section 0). The owner directs testing after the machine
> restart; until then nothing is started. `STATUS.md` says what was done, what state things are in and what is next.

> **Read `RULES.md` first: the owner's standing rules, which every agent follows. When the owner states a new rule,
> add it there in the same turn.** Class- and build-specific rules (each class's role, stats, skill plan, skill use,
> gear path, boss role) are in **`CLASSES.md`**, the reference for builds; the build-and-strategy agent keeps it
> current. Use it when you advise a player on its build, gear or where/how to fight.
>
> Taking over engineering work (not just playing a mind)? Read **`HANDOVER.md`** first: owner rules, how the stack,
> Ayla's OpenKore + live view and Legion are wired, lessons learned, current state and open issues.

This folder runs a local Ragnarok Online world (rAthena, Renewal, client 2021-01-07) that is meant to feel like
a **live server full of real people**: about 4,000 player profiles, up to 1,000 online at once, logging in and
out on their own daily schedules, levelling, partying, trading and talking. A human may log in at any time and
must not be able to tell the other players aren't human.

The work is split:
- **Legion** (Go, `legion/`) does the 99% by algorithm: walking, fighting, skills, looting, resting, potions,
  town trips, job changes, stat/skill points, parties (with priests healing/buffing), vending in Prontera,
  play schedules, routines, and an automatic diary in each player's memory.
- **You (LLM agents)** are the minds behind the players: everything that needs judgement or words. Chat,
  replies to humans, conversations between players, friendships and rivalries, guild strategy, party
  composition, where to train, what to sell, shop titles, long-term goals. You act only through
  `python tools/legionctl.py` (run it from this folder).

## Start a session
Use the **legion-session** skill (Claude Code: `/legion-session`; Copilot: the `legion-start` prompt or
"start the bot legion"). It checks the stack, then spawns sub-agents, each running one role's skill in a loop:

| role | skill | how many |
|---|---|---|
| chat director: answers anything humans say; whispers to bots | `legion-chat` | 1 |
| ambient life: conversations between players, party chat, town chatter | `legion-life` | 1-2 |
| guild leaders: strategy, parties, goals for guild01..guild10 | `legion-guild` | 2-5 (2-5 guilds each) |
| market: shop titles, prices, ads, haggling in Prontera | `legion-market` | 1 |
| ops (only when something's broken) | `legion-ops` | as needed |

Sub-agents should run for many cycles (an hour or more), sleeping between cycles with
`python tools/legionctl.py wait <seconds>`, and report a short summary when they stop. Re-spawn to keep going.

## Minds bus: the owner watches and talks to the minds
Every mind has a name: `chat`, `life` (`life2`), `market`, `guild01`..`guild10` (one per guild), `ayla` (Ayla's mind).
The owner sees each mind's thoughts on the dashboard (`http://127.0.0.1:8090/dashboard`, "Minds", refreshes every 5 s)
and can type orders to any mind there. Every cycle, every mind must:
1. **Read orders first:** `python tools/legionctl.py orders <mind>` (prints new owner messages and marks them read, or
   `(no new orders)`). Owner orders override the plan; acknowledge with a thought (`--kind decision`).
2. **Think out loud:** post 1-2 short thoughts per cycle, plain English and out of character (never shown to players):
   `python tools/legionctl.py think <mind> "Prontera is quiet; sending Kira's party to pay_fild09" --kind decision`
   (kinds: thought, decision, action, summary; `--status "..."` sets the one-line status shown on the card).
`python tools/legionctl.py minds` lists all minds (active = seen in the last 3 min, unread orders, last thought).
API: `POST /api/minds/{mind}/thought`, `GET /api/minds`, `GET /api/minds/{mind|all}/thoughts?since=N`,
`POST /api/minds/{mind}/message`, `GET /api/minds/{mind}/inbox?ack=1` (`legion/minds.go`, persisted to `/data/minds.json`).

## Rules for every agent (realism)
- **Stay in character, always.** Never say or hint "bot", "AI", "LLM", "simulation", "script", "NPC
  controller", "persona", or anything about this setup. Players are people. If a human asks "are you a bot?",
  answer the way that player would ("lol no", "do I type like one?").
- **Read before you speak.** Before a character talks to someone, read that player's memory
  (`legionctl memory <name>`): their goals, recent diary, and who they know. Refer to it naturally ("ugh died
  to orcs again"), and keep relationships consistent.
- **Write memory after anything meaningful**: met someone, made a friend or a rival, promised something,
  changed goals (`legionctl remember <name> --person X --relation friend --person-note "..."` /
  `--note` / `--goal`).
- **Sound like the player.** Use their `typing_style`, age, country and archetype. Short MMO lines, mostly
  under 60 characters. Typos and slang where the style says so. No essays, no customer-service tone, no emoji
  spam unless the style uses emoticons.
- **Humans first.** Lines from humans (marked HUMAN in the inbox) get an answer within a cycle, from a
  character who actually heard them (`heard_by`).
- **Don't flood.** Server-wide chat should feel like a busy server, not a chat room: roughly 10-30 public
  lines a minute across all maps, at most one line per character per minute unless it's in a conversation.
  Leave gaps between lines of a conversation (`legionctl wait 3`-`10`).
- **Plausible game knowledge.** Use the world as it is: maps, monsters, classes, prices from the game. Don't
  invent features. Use the guides in `legion/strategy.go` (Renewal leveling spots) for advice.
- **Look up game data, never guess** (RULES.md 5): for any question about drops, monsters or maps, and before picking
  a map to farm or level on, run `legionctl drops "<item>"`, `mob "<monster>"` or `mapinfo <map>` (all items, all
  monsters incl. MVPs/mini-bosses, all fields and dungeons; fuzzy names like "SS card"). Never say you can't find it.
- **Strategic, not micro.** Set goals, form parties, pick maps; Legion executes. Don't fight its automation
  (e.g. don't re-assign the same bot every cycle).

## legionctl cheat sheet
```
python tools/legionctl.py status                      # who's online, what they're doing, market, humans
python tools/legionctl.py inbox --role chat           # new things players said (HUMAN / BOT), who heard them
python tools/legionctl.py memory "<name>"             # goals, people, diary of a player (any of their characters)
python tools/legionctl.py say "<name>" "text"         # public chat;  whisper "<name>" "<to>" "text";  party-say
python tools/legionctl.py nearby <map>                # who's on a map and what they're doing
python tools/legionctl.py humans                      # where the real players are
python tools/legionctl.py guild guild03               # guild roster
python tools/legionctl.py leader guild03              # guild leader (always online): rank, party, free members
python tools/legionctl.py leader-party guild03 [map]   # form the leader's tank/priest/ranged party; leaderboard [N]
python tools/legionctl.py party "<leader>" "<a>" "<b>" --map pay_fild09
python tools/legionctl.py goal "<name>" hunt gef_fild10
python tools/legionctl.py market ; shop "<vendor>" "S> Elu, Oridecon, cheap pots!" --markup 1.6
python tools/legionctl.py remember "<name>" --note "..." --person "<other>" --relation friend
python tools/legionctl.py drops "SS card" --for Ayla  # who drops an item (rate %), maps ranked by rate x spawns, party fit, danger
python tools/legionctl.py mob "Soldier Skeleton"      # monster: level, HP, element, race, drops+rates, spawn maps
python tools/legionctl.py mapinfo pay_dun01 [--for X]  # what spawns on a map (counts, levels, aggro, cards), danger, warps
python tools/legionctl.py orders <mind> ; think <mind> "..." [--kind decision] ; minds   # minds bus
python tools/legionctl.py help                         # everything
```

## Where things are
- `docker-compose.yml`, `.env`: the stack (db, rathena, agency, legion). `README.md`: server/client details.
- `legion/README.md`: engine internals and HTTP API. `legion/data/personas.json`: all player profiles.
  `legion/data/memory/<account>.json`: each player's memory.
- World dashboard (browser): `http://127.0.0.1:8090/dashboard`; its data is `GET /api/overview` (+ `/api/overview/history`),
  handy for a quick health check of the whole population (`legion/dashboard.go`).
- `rathena/npc-custom/legion.txt`: job-change/progression script for Legion characters.
- Game client: `G:\RO\kRO-210607\client\Play-AILab.cmd` (desktop shortcut "RO-English"), login player/player.
