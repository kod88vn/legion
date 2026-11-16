---
description: Read-only start of a Legion session: read the constitution and the status note, then report what runs. Starts nothing.
mode: agent
---

Read `CONSTITUTION.md` in full first. It is the highest law and nothing overrides it. Then read `RULES.md`, `STATUS.md`,
`HANDOVER.md` and `AGENTS.md`. These files hold the text; this prompt holds pointers only (PR42).

Then report, in a few lines: what STATUS.md says runs, what is on hold, and what is waiting for the owner. Check the
repository with `git -C` (status and log) and, if asked, `python tools/system/gatecheck.py --no-suites`.

Do not start containers, loops, puppet masters or agents, and do not deploy or load anything: nothing starts without
the owner's word (G21). If the owner asks for a session to start, the main session assigns the work to an agent under
the roles in `.github/copilot-instructions.md`; the procedure is in `.claude/skills/legion-session/SKILL.md`.
