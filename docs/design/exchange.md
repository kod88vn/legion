# Exchange and chat jobs (work package X)

Branch `feat/exchange`, 2026-11-14. Owner: "items can be freely given to party member or guild member or at a heavily
discounted price, therefore, a chat agent must be there to help bots communicate".

Everything is behind two tuning flags, both **off by default**. With both off the world behaves exactly as on `main`
(every hook returns at once; the #trade lines go out through `channelSay` as before).

## Files
- `legion/exchange.go`: price tiers, ask order, matcher hooks, tier deal talk, counters, `GET /api/exchange`,
  `lootTalkJob` (loot-flow offer as a chat job).
- `legion/chatjobs.go`: the chat-job queue, `EnqueueChatJob` / `ChatJobLine`, `GET /api/chat-jobs`,
  `POST /api/chat-jobs/{id}/lines`.
- `legion/chatintent.go`: `POST /api/chat-intent` (and `GET` for the list), human needs / offers in the economy round.
- `legion/exchange_test.go`: unit tests (19).
- Shared files, one-line hook-ins only:
  - `economy.go` econRound: `exchangePrepare` after `lootFlowRound`; `exchangeAsk` in the want-posting loop;
    `exchangeEligible` in the seller loop; `exchangeTier` next to `guildEach` (one extra term in the price check and
    `each = min(each, tierEach)`); `exchangePrefer` before the existing best-candidate rule; `exchangeChosen` after a
    match; `exchangeTradePost` replaces `channelSay("#trade")` for the posts; `exchangeRound` before `humanWantRound`.
    startDeal's conversation goroutine starts with `exchangeDealTalk`; marketDone calls `exchangeDealDone`.
  - `lootflow.go` lootGive's conversation goroutine starts with `lootTalkJob`.
  - `api.go`: `registerExchange(mux, bots)`.
  - `tuning.go`: 7 fields, their defaults, doc lines.
  - `tools/legionctl.py`: `exchange`, `chat-jobs`, `chat-intent`.

## Tuning keys (`legion/data/tuning.json`, re-read every 30 s)
| key | default | meaning |
|---|---|---|
| `exchange_tiers_on` | false | relationship prices and the party -> guild -> #trade ask order |
| `kin_discount_pct` | 25 | percent of the market price a guild mate / friend pays for a non-spare (0-100) |
| `exchange_gifts_per_hour` | 3 | free or discounted hand-overs ("favours") per giver per hour; then the giver sells at market |
| `exchange_ask_stage_sec` | 180 | how long a want stays at each ask stage |
| `chat_jobs_on` | false | social lines wait for chat-agent words; intents are acted on |
| `chat_job_wait_min_sec` | 20 | a waiting job's deadline is random in [min, max] seconds |
| `chat_job_wait_max_sec` | 40 | |

## Exchange rules (exact)
- Relation between giver and taker, closest first: **party** (same Legion party; for a human, a bot whose party the
  human leads) > **guild** (same guild id from `guildOfAccount`; Starveil = `human-guild`, for a human from its
  status.json guild) > **friend** (friendship score >= 35, friends.go "friend" tier) > stranger.
- Price per unit (`exTier`): party 0; guild/friend 0 when the item is a **spare** for the giver, else
  `roundPrice(market * kin_discount_pct / 100)` (at least 1z); stranger: the matcher's existing price.
  Market = `priceOf(id)` this round (falls back to the seller's ask).
- **Spare** (`exSpare`): the giver owns 2+ (bag + worn), or the item is not on its build's gear table (slots or cards)
  for its level band or any later band (`exOnTable`, table read before any bot lock).
- The giver side keeps the loot rule: only `econSpare` reaches the matcher (never what the bot wears, compounds or
  refines with; `keepsGear`/`keepsCard`/`refineKeep`), filtered by lootflow.go (use -> party -> guild -> guild stock
  -> sell). A guild vendor's stock (`keeper`) is never "spare" (kin price, not free). Vendor couriers and stock buyers
  keep market rules.
- Favours: at most `exchange_gifts_per_hour` per giver in a rolling hour; over the limit the giver is a market seller.
- Ask order: stage = want age / `exchange_ask_stage_sec` over [party (if in a party), guild (if in a guild), #trade].
  At the party stage only party members may fill the want; at the guild stage party, guild mates and friends; at
  #trade anyone. One party / guild ask line per want per stage and at most one per bot every 5 minutes (as a chat
  job, kind `ask`); the #trade line keeps the existing per-bot (10 min) and server (6 s gap) limits.
- Seller preference: party > kin with a spare > kin > stranger, then the existing rules (in person before mail,
  guild mates, nearest).
- Tier deals are talked through in party chat (party), guild chat (guild free), or whisper (friend, any paid kin
  price), as one chat job (kind `offer`: the giver's line and the taker's reply). A free hand-over ends with a
  thanks line in party / guild chat (kind `thanks`). Paid kin deals keep the usual whispered thanks.
- Distance rules are unchanged: a free item far away is not mailed (mail trades need 1,000z+); party gifts happen in
  person within 10 cells as every party trade does.

## Humans and Ayla
- A human gets the same tiers as any player (gate G25, owner 2026-11-14). The engine cannot act for a human, so the
  human's side is always the human's own move: a kin bot mails a free gift (the giver pays the 100z fee), or quotes the
  kin price through the existing human-sale flow (it mails the item and asks; the human pays by its own mail).
  A human's offer is answered with a whisper; the hand-over is the human's move.
- Ayla keeps 10,000z (`aylaZenyFloor`): a paid quote reaches her only if `purse - price >= 10,000` with the purse from
  status.json; purse unknown -> gifts only. A need intent from her goes to the market with max price
  `(purse - 10,000) / count`; at or under 10k it waits as "gifts only". Nothing in this package moves her zeny.

## Chat jobs
- Enqueue: `EnqueueChatJob(ChatJobSpec) string` (never blocks, safe with any lock held; returns the job id, or ""
  when the template was said at once). Single line: `ChatJobLine(kind, speaker *Bot, channel, text, facts) string`.

```go
EnqueueChatJob(ChatJobSpec{
    Kind:    ChatKindDutyWarning,            // ask offer accept thanks direction duty_warning kick leader_banter
    Channel: "guild",                        // party | guild | #trade | #global | local | whisper
    To:      "",                             // whisper target
    Speaker: leaderBot,                      // *Bot (or Template[0].Speaker)
    Hearers: []string{"Member1"},            // who it's addressed to / may answer
    Facts:   map[string]any{"member": "Member1", "minutes_owed": 240, "days_behind": 3},
    Template: []ChatLine{{Speaker: "LeaderName", Channel: "guild", Text: "Member1 u havent helped the guild in days"}},
})
```
  Multi-line templates are scripted exchanges (other speakers allowed, `DelaySec` = pause before the line).
- Path: `chat_jobs_on` and an agent polled `GET /api/chat-jobs` within 2 minutes -> the job waits until its deadline
  (20-40 s); lines posted in time are said, else the template at the deadline (resolver loop, 1 s). Otherwise the
  template is said at once. If the agent stops polling, waiting jobs fall back at once. Max 200 waiting.
- Agent lines are validated: speaker = the job's speaker, a hearer or a template speaker, and a character this
  instance runs; channel = one of the job's channels; one line, <= 160 chars, nothing like bot / AI / legion /
  script / engine / template / agent; at most template lines + 1; never more public (#..., local) lines than the
  template had (so public chat stays inside the engine's rate limits). Refused -> HTTP 409, the job keeps waiting.
- Core instance: job ids are `m<n>` (main) / `c<n>` (core). The main API merges core's waiting jobs into the list and
  forwards `POST /api/chat-jobs/c<n>/lines` to the core instance; its GET is forwarded too, so polling main marks the
  agent present on both.

## Endpoints (examples for the chat agent)
`GET /api/chat-jobs` (records the poll; `?peek=1` doesn't; `?recent=1` adds resolved jobs)
```json
{"on": true, "agent_present": true, "agent_seen": "18:02:11", "wait_sec": [20, 40],
 "jobs": [{"id": "m41", "t": "...", "kind": "offer", "channel": "guild", "speaker": "Kira", "hearers": ["Tobi"],
   "facts": {"item": "Mantis Card", "tier": "kin_free", "relation": "guild", "price": 0, "market_each": 12000},
   "template_line": "i have a spare Mantis Card, its yours Tobi",
   "template_lines": [{"speaker": "Kira", "channel": "guild", "text": "i have a spare Mantis Card, its yours Tobi"},
                      {"speaker": "Tobi", "channel": "guild", "text": "omg ty, come trade", "delay_sec": 5}],
   "deadline": "...", "state": "waiting"}],
 "counts": {"queued": 12, "agent lines": 9, "template: timeout": 3}}
```
`POST /api/chat-jobs/m41/lines`
```json
[{"speaker": "Kira", "channel": "guild", "text": "Tobi got an extra Mantis, want it? free"},
 {"speaker": "Tobi", "channel": "guild", "text": "omg yes pls! im in pay dun"}]
```
-> `{"ok": true, "id": "m41", "state": "agent", "lines": 2}`; `[]` = say the template now; error ->
409 `{"ok": false, "error": "line 1: ..."}`.

`POST /api/chat-intent`
```json
{"from": "Ayla", "kind": "need", "item": "Clip", "count": 1, "max_price": 5000, "note": "said in party she needs one"}
{"from": "Kitty", "kind": "offer", "item": "Poring Card", "price": 800}
```
-> `{"ok": true, "id": 3, "status": "open", "human": true, "zeny_cap": 2400}`. From a Legion character: a need
becomes its want (`"want registered at 4500z each"`), an offer is noted. `GET /api/chat-intent` lists intents.

`GET /api/exchange` -> `{"on", "kin_discount_pct", "gifts_per_hour", "ask_stage_sec", "rules", "counts":
{"asked: party", "matched: party|kin_free|kin|market", "done: ...", "intent: ..."}, "recent": [{t, giver, taker,
item_id, item, count, tier, relation, price, market_each, status, human}], "intents": [...], "core": {...}}`.

legionctl: `exchange`, `chat-jobs [list|answer|pass]`, `chat-intent <from> <need|offer> <item> ...`, `chat-intent list`.

## Switch on and verify live (after the baseline run)
1. `exchange_tiers_on: true` in tuning.json. Within a minute `legionctl exchange` shows `asked: party` / `asked:
   guild` counters; party / guild chat shows "anyone got a spare X?" lines (`/api/events` kind said); after a few
   rounds `matched: party|kin_free|kin`, then `done: ...`. Check `recent`: no favour giver above 3 per hour; prices of
   `kin` rows = 25% of `market_each`. `/api/economy` misses should not rise noticeably (wants wait up to 6 min at the
   party/guild stages). Meter: zeny/hour of the cohorts should not fall.
2. `chat_jobs_on: true`, no agent: `legionctl chat-jobs --peek` -> counts `template: no agent` grow, no job waits; chat
   looks the same as before.
3. Start the chat agent polling `legionctl chat-jobs`: jobs wait (`queued`), its answers show as `agent lines`; stop
   it -> within 2 min `template: no agent` again.
4. `legionctl chat-intent Ayla need "Clip" --max 3000` -> intent listed; with Ayla under 10k it stays "gifts only".

## Open risks
- Free gifts between far-apart kin are not mailed (mail trades need 1,000z+): a kin want with no holder nearby waits
  for the #trade stage and may then buy at market from a stranger.
- The party/guild ask stages delay strangers' sales by up to 2 x 180 s per want; watch the economy miss counters.
- A human's offer has no automatic settlement for strangers (bots don't accept a stranger's trade window and item
  mail from humans isn't paid automatically); kin humans hand over via a trade window (bots accept party/guild mates).
- A human need claimed by the core instance (Ayla's party) is not offered to main afterwards if the core giver turns
  busy; it expires after 45 min.
- `coreHasPartySpare` and intent offers call `ownUse` / `lootCanUse` under a bot lock as lootflow.go already does;
  those may stat buildgear.json under the lock (pre-existing pattern, not new I/O paths in the hot loop).
- Agent lines are filtered by a word list; tone and fit are the chat agent's job.
- `tuning.go` and `economy.go` are touched by other branches too: expect small merge conflicts in the struct/defaults
  and in econRound around the hook lines.
