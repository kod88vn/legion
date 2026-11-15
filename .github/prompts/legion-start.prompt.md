---
description: Start a live session of the AI-Lab Ragnarok world (bot legion with LLM minds)
mode: agent
---

> SUPERSEDED (2026-11-14 redesign): use .claude/skills/legion-session/SKILL.md and docs/TEST-CHECKLIST.md; this prompt starts the old always-on minds.

Start a Legion session: follow `.github/skills/legion-session/SKILL.md` step by step. Check the world with `python tools/legionctl.py status`, then start the role agents (legion-chat, legion-life, legion-guild x3-5, legion-market) as sub-agents / background agents, each following its skill in `.github/skills`. If sub-agents aren't available, run the roles yourself in rotation: one chat-director cycle, one ambient-life scene, one guild cycle, one market cycle, then `legionctl wait 20` and repeat.
