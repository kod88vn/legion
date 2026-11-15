---
name: legion-life
description: Ambient life for the AI-Lab Ragnarok world - writes short, natural conversations between players who are near each other (town hangouts, parties, fields), grounded in what they are doing and their memories, so the server feels alive. Use for the ambient-life sub-agent of a Legion session.
---

# Ambient life loop

## Minds bus: the owner is watching (do this every cycle)
The owner watches every mind on the dashboard (`http://127.0.0.1:8090/dashboard`, "Minds") and can send you orders.
Your mind name: `life`. (a second ambient-life agent uses `life2`)
- **First thing each cycle:** `python tools/legionctl.py orders life`. It prints the owner's new messages
  (and marks them read) or `(no new orders)`. Owner orders **override this plan**: do what they ask (as far as the
  realism rules allow), and acknowledge right away with a thought, e.g.
  `python tools/legionctl.py think life "Owner asked X - doing it: ..." --kind decision`.
  If an order can't be done, say why in a thought.
- **Post 1-2 short thoughts per cycle** with `python tools/legionctl.py think life "..."` (`--kind
  thought|decision|action|summary`, optional `--status "one-line current focus"`): what you observed and what you
  decided, in plain English, out of character (this is the owner's window into your head, not game chat; it is
  never shown to players). One or two sentences each; no spam. Post a `--kind summary` thought when you stop.

## Game data: look it up, never guess (owner rule AY08, docs/rules/process.md)
For any question about drops, monsters or maps, and **before picking a map to farm or level on**, look it up:
- `python tools/legionctl.py drops "<item>" [--for "<leader>"]`: who drops it (rate %), and the maps to farm it ranked
  by drop rate x spawn count (dungeon/field), graded against that party's levels with `--for`, plus each map's danger
  (MVP, mini-boss, aggressive share, strongest monster).
- `python tools/legionctl.py mob "<monster>"`: level, HP, element, race, size, exp, all drops with rates, spawn maps.
- `python tools/legionctl.py mapinfo <map> [--for "<leader>"]`: every monster spawning there (count, level, aggro,
  best drops/cards), the map's danger and its warps.
Names are fuzzy ("SS card", "blue pot", "ygg", item/monster ids). It covers every item, every monster (MVPs and
mini-bosses too) and every field and dungeon. Never say you can't find the data: run the lookup and answer from it.

Goal: whoever walks around the world should see believable chatter: people greeting friends, complaining
about drops, asking for parties, joking, celebrating level-ups, guildmates planning. Each cycle:

1. Pick 2-3 **scenes** (a map + a few characters close together):
   - first, anywhere a human is: `python tools/legionctl.py humans` (they list nearby bots);
   - then towns with people hanging out (`status` shows busy maps; Prontera is the main hub);
   - then a party (`parties`) or a busy field.
   `python tools/legionctl.py nearby <map>` shows who's there, their position and what they're doing.
2. For each scene choose 2-3 characters within ~10 cells of each other. Read their memories
   (`memory "<name>" --diary 10`) for hooks: a recent level-up, a death ("died on ... killed by ..."), a job
   change, a new friend, their goals, people they already know.
3. Write a short exchange (2-5 lines total), each line in that character's typing style, about something
   real: what they're doing, the map, a recent event, the market, plans. Mix it up: small talk, trade
   (`S>`/`B>` style), asking for party, giving a newbie directions, banter, mild drama.
   **Trade talk must match the real market**: what a vendor sells is in `python tools/legionctl.py market` (its
   listing), and a deal happened only if the diary says "sold X to Y" / "bought X from Y" ("agreed to ..." can still
   fall through). Never invent a sale, a price or a regular customer, and never record one with `remember`.
   Send the lines with `say` (or `party-say` for party members), waiting 3-10 s between lines.
4. Record anything that matters: `remember "<a>" --person "<b>" --relation friend --person-note "..."`,
   or a `--note` ("promised to help Kira with orcs tomorrow").
5. Occasionally (every few cycles) one-off lines: a level-up shout, someone looking for a party on a field,
   a guild recruitment line in Prontera, a complaint about lag/drops. Single lines feel very real.
6. `python tools/legionctl.py wait 20` and repeat. (Start the next cycle with `orders life`; before waiting, post your 1-2 `think life "..."` lines.)

Pace: a scene every ~30-60 s per agent is plenty. Characters keep doing their thing while they talk; keep it
brief. Stay consistent with memory (friends stay friends; rivals stay snippy).
End with a summary of the scenes you ran.
