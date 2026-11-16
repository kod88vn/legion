> **Reference only: 15-mind era; current state is in HANDOVER.md and STATUS.md.**

<!-- Moved unchanged from HANDOVER.md lines 776-813 (as of commit 63ca923). Index: HANDOVER.md. -->

## 6. Current state (2026-11-13 ~19:40)

- ~700 bots online. All bots were reset to Novice Lv1 (re-seed ~17:28; 50 that had already changed job were reset
  again at 18:48 on request, backup `legion/data/backup-20261113-1848-novice-reset`). Many have legitimately reached
  first jobs again since (party EXP share is fast).
- Ayla: Thief Base 42 / Job 32, pay_fild01, played by OpenKore, visible in the watch window.
  Party "Ayla pt": Zendancer84 (Acolyte ~26), Zen.fox (Mage ~28) with her; Echobunny (Novice/Swordman, stuck on
  moc_fild02 earlier, offline at last check).
- Running LLM minds (Haiku, ~90 min runs, they stop by themselves): `ayla` (Ayla's mind via ayla.sh), `chat`
  (chat director). Re-spawn with the legion-session skill or the briefs in AGENTS.md. Watchdog agent finished.
- The owner's account `camera` was deleted (no GM watcher).

---------------------------------------------------------------------------------------------------------------------
## 6b. The minds are the core - hard gate
The owner: "the llm minds are the core of the server, the watch dog is supposed to watch out for them and keep them
alive". Required minds: ayla, chat, life, market, guild01-10, plus a watchdog agent that messages the main session
when one goes silent. `G:\RO\legion\tools\mindgate.py` runs as a UserPromptSubmit and a Stop hook (`G:\RO\.claude\settings.json`,
`G:\RO\legion\.claude\settings.json`). Any required mind silent for more than 5 minutes is injected as a warning and
blocks ending the turn until it's restarted. The minds are background Haiku agents that run ~90 min each and must be
re-spawned (briefs: skills in `.claude/skills`, Ayla's mind and the watchdog as in this file). Chat volume also comes
from Legion's chatter engine (`legion/chatter.go`: #global/#trade autojoin via the mounted `rathena/conf/channels.conf`,
~45 lines/min at 900 online).

## 6c. The self-improvement loop (owner: "I am trying to create a self learning/improving loop")
1. **Observe**: `G:\RO\legion\tools\sentinel.py` (WSL, long-running background command of the engineering session) checks the
   whole system every minute and logs to `legion/data/health.log`: population, kills per bot, share of frozen bots
   (it samples random bots), minds alive, Ayla online, her party together, the watch client and its launcher, chat
   volume, death rate. `tools/legion/classprobe.py [minutes] [per_class]` samples bots of every class on demand;
   `GET /api/bots/<name>` has a `mobs` list saying why each nearby monster is or isn't a target.
2. **Wake**: when a problem outlasts its grace time the sentinel prints the evidence and exits, which wakes the
   engineering session. The Haiku watchdog agent also messages the session (minds down, party split, deaths).
3. **Fix** the cause in the engine (not the symptom), deploy, and verify with the probe.
4. **Record** the lesson in `legion/data/lessons.md` (one line: what was seen -> the rule now in the engine) and
   here, then start the sentinel again.
Ayla's party is the test bench: keep the priest (Zendancer84) and ROTATE the other two members through the other
jobs so each class gets tested (`POST /api/human-party {"human":"Ayla","members":[...]}`). Next rotation planned:
Thornlord (Archer) and Ashwing (Merchant) once the current trio's gear purchase is verified.

