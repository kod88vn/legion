# Chat director (chat mind) - restart notes

Written at the owner's request: "save all agent work in a way we can restart".

## How to resume
1. `python tools/legionctl.py orders chat` (owner first), then `inbox --role chat` every cycle.
2. Answer each NEW HUMAN line once, from a character in heard_by, after reading its memory (`memory <name> --diary 4`).
3. Every ~20 cycles: `humans` (it has been timing out; see below). Heartbeat thought every few cycles:
   `think chat "..." --kind thought --status "..."`.
4. Market-gap lines in #trade: at most one every 10-20 minutes, from a Prontera vendor after reading its memory.
   Post with `whisper "<vendor>" "#trade" "<text>"` (same packet as the engine's channel post), then verify with
   `curl -s http://127.0.0.1:8090/api/events?since=0 | grep -c '<text prefix>'`.
5. Do NOT answer B> (buy) offers; the engine handles offers. Do not invent trade.
6. Redeploy = connection reset / refused: `wait 60` and continue.

## Open conversations and promises (as of run 5)
- LilRyuheart (Acolyte lv46, healer) <-> Ayla: Ayla invited her into the Starveil party; LilRyuheart accepted and
  promised to heal. Memory updated (goal "join Starveil party with Ayla"). Follow-up: nothing pending on our side.
- vexclaw (Crusader lv36) -> Ayla: offered to tank if she ever needs a shield. No reply yet from Ayla. Memory updated.
- lunaqueen (Crusader lv55) -> Ayla: sold a Creamy Card by mail for 8700z; Ayla paid by mail. Deal closed.
- LilAshboy Trade (Indonesia, hoards rare drops) -> Ayla: told her "ok no problem, when u have money pm me, i keep
  it for u" (item held for Ayla, 90k price). OPEN: waiting for Ayla to pm and pay.
- Ayla's repeated #global recruiting line (Starveil: need bard, monk, merchant): already answered by LilRyuheart and
  vexclaw. Do not answer repeats.
- Ayla's #trade wants (Clip, Creamy Card): none of our characters had the items; nothing offered. Engine handles B>.

## Market-gap lines already posted (verified in the event feed)
- Crimsonfang Goods: no sandals at the south gate.
- LilMintfire48Vend: Ragamuffin Manteau nowhere.
- Shadow Goods: clubs everywhere, nobody buys.
- TheNekohunter3 Mart: Hood nowhere (run 4).
Keep ~1 per 10-20 min, varied wording, in character.

## Known issues
- `legionctl humans` times out (20 s) repeatedly since the 20:43Z restart; orders and inbox still answer.
  Reported to main by SendMessage (run 5). Fallback: `curl -s http://127.0.0.1:8090/api/overview` (not verified).
- Verification via the events ring only shows about the last 4000 events; older lines roll out.
- AIBot (old seed test bot) is switched off. Do not treat any name as a bot hint.

## Run counts
- Run 1: ~300 cycles. Run 2: ~300 cycles. Run 3: ~300 cycles. Run 4: ~300 cycles (approximate).
- Run 5: started at thought #10173; roughly 150 cycles done when saved; last heartbeat ~#10662.
