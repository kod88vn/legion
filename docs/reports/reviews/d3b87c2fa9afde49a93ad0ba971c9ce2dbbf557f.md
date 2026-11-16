# Retroactive review: merge 0ff7ef2 (fix/chat-guild-api, tip d3b87c2)

Reviewer: Sonnet 5.5, 2026-11-15. Retroactive (Constitution 4.2: the merge predates the rule). Read-only review of
`git diff 0ff7ef2^1...0ff7ef2^2` (13 files) against current main. Go tests of the touched areas pass on a scratch copy of main.

What it brought: chat jobs wait 45-90 s (was 20-40) with a 45 s grace after the agent lists them, capped 3 min after
queueing (`chatjobs_wait.go`); kinds `thanks,accept` are said by code at once; a late agent answer is a 200 "late" reply
instead of 409; `POST /api/bots/{name}/guild-chat` plus `legionctl guild-say` / `say <guildNN>`; Starveil's purse (from the
human status file) counts in the guild wealth rank (`guildrank_human.go`).

## Checked, no defect
- G18: the guild-chat route runs the text through `chatLineOK` (api.go:300-306), as the other say routes do.
- G25: `humanPurseOf` applies the same rule to any guild (name match, fresh under 24 h, zeny >= 0); pure and tested.
- Locking: `chatjobs_wait.go` takes only `chatJobs.mu` (leaf lock) and reads `tune()`; `chatJobsListed` and
  `chatJobLateLocked` are called outside other locks. Kick lines keep their deadline (guildchat.go:35 uses max+2 s).
- Tests prove the claims (grace, cap at 3 min, grace 0 = off, code kinds, late answer, unknown id still an error, kick not
  extended, longer defaults). No secrets, no live reach-out in the tests (`humanGuildPurse` reads a file in production only).

## Findings
- minor, historic, already fixed forward: `tools/legionctl_guild.py` guild-say first read `["leader"]["name"]` from
  `GET /api/guild-leaders/{id}`, but that payload carries `leader` as a plain string (api.go:360 area), so the command
  would have raised. Main now posts to `/api/guild-leaders/{id}/say` (legionctl_guild.py:152, guildlead.go:701), which also
  answers 409 for an offline leader. Nothing to do.
- minor, still on main: the old route `case "guild-chat"` (api.go:300) is now unused by the tools. It does not check that the
  bot is online and does not return a JSON ack. Harmless (no caller); remove or give it the same online check when touched.
- minor, judgement (Constitution 2.1, believable pace): a question to a character can now wait 45-90 s, up to 3 min after a
  listing, before the answer or the template line is said. That is slow for local chat but inside the range of a distracted
  player, and the feature stays default off (`chat_jobs_on` false). Watch the "agent answer late" counter in
  `/api/chat-jobs` before lowering `chat_job_listed_grace_sec`.

verdict: merge
