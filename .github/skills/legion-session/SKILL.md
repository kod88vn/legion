---
name: legion-session
description: Start (or resume) a live session of the AI-Lab Ragnarok world - checks the server and Legion bot engine, then spawns sub-agents that act as the minds of the players (chat director, ambient life, guild leaders, market). Use when asked to "start the bot legion", "bring the server to life", or "run the masterminds".
---

# Start a Legion session

You are the orchestrator. Your job is to make sure the world is running and then hand the players' minds to
sub-agents. Read `AGENTS.md` first if you haven't.

## 1. Check the world
```
python tools/legionctl.py status
```
- If the API isn't reachable, start the stack and wait ~2 minutes:
  `wsl -d Ubuntu-24.04 -- bash -c "cd /mnt/g/RO/server && docker compose up -d"`
  then re-run `status`. Use the `legion-ops` skill if it still fails.
- Note how many are online, the busiest maps, and whether any **humans** are online.

## 2. Spawn the minds (sub-agents, in parallel)
Spawn these as sub-agents (Claude Code: the agents in `.claude/agents/`; Copilot: the custom agents in
`.github/agents/`). If your tool can't select a named agent, give a general sub-agent the brief below and tell
it which skill to follow. Use a fast/cheap model (e.g. Haiku) for them if you can choose.

| agent | skill to follow | brief |
|---|---|---|
| legion-chat | legion-chat | "Run the chat-director loop for 60 cycles; answer every HUMAN line." |
| legion-life | legion-life | "Run ambient life for 60 cycles; focus on maps with humans, then towns, then busy fields." |
| legion-guild (xN) | legion-guild | "You lead guild01, guild02, guild03 (and so on). Run 20 cycles." Split guild01..guild10 across 3-5 agents. |
| legion-market | legion-market | "Run the Prontera market for 30 cycles." |

Each sub-agent must use only `python tools/legionctl.py ...` (plus reading files in this folder), sleep between
cycles with `legionctl wait`, and finish with a 5-line summary of what happened in the world.

**Minds bus (tell every sub-agent this in its brief):** each mind has a name: `chat`, `life` (`life2` for a second
one), `market`, one per guild (`guild01`..`guild10`), and `ayla` for Ayla's mind. Every cycle it must first run
`python tools/legionctl.py orders <mind>` and follow the owner's orders (they override the plan; acknowledge with a
thought), and post 1-2 short plain-English thoughts with `python tools/legionctl.py think <mind> "..."` (what it saw,
what it decided). The owner reads these and replies on the dashboard (`http://127.0.0.1:8090/dashboard`, Minds).
You can check the minds yourself with `python tools/legionctl.py minds`; an `idle` mind (not seen for 3+ min) has
probably stopped and should be re-spawned.

## 3. Keep it going
When a sub-agent finishes, read its summary, check `status` and `minds`, and spawn a fresh one for that role (memory lives
in Legion, so nothing is lost). If a human is online, make sure the chat director and an ambient-life agent are
always running.

## Notes
- Never touch the database or Docker unless `status` shows the world is down.
- Don't speak as players yourself while sub-agents run (avoid double replies).
