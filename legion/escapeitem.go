package main

import (
	"fmt"
	"sync"
	"time"
)

// Escape items that do nothing (QA 2026-11-14 08:41 UTC: Kai_wing, lv57 Assassin on a fetch deal, logged "fled to town
// (Butterfly Wing)" every 4 s for minutes on mjolnir_07 and never left the map). rAthena refuses item use while an NPC
// dialog, a trade window, a chat room or a shop window is open, and escape() retried the same item forever.
// Now: clear those first, check 2 s later that the item worked (count dropped or the map changed), and when it did
// not, skip that item for 30 s so escape() falls through to the next option (Fly Wing, then running).

// QA 2026-11-14 14:00 UTC (life mind: Lily.dancer, Wolf.rose, Moon_knight, Kiraheart, Silversage13 died on the orc
// fields after "Fly Wing did nothing ... something else for 30 s"): rAthena drops a wing SILENTLY when the character
// is sitting (pc_useitem: delay-consume items, and the teleport cast: clif_parse_skill_toid pc_issit), casting
// (ud.skilltimer: a Sage's bolt, a priest's Heal), stunned/frozen/asleep/stoned (opt1), or when ANY other skill is sent
// between the item use and our teleport cast (that clears sd->skillitem, the cast then has level 0). And after one
// failure the wing was skipped for 30 s while the bot was beaten. Now: stand up first, wait for our own cast bar to
// end, send no other skill until the teleport is cast (escHold, useSkill), take a refused-use ack (0x01c8 result 0)
// as a failure at once, retry after 3 s on a first failure (30 s only after two in a row), and let escape() decide
// again immediately (Butterfly Wing, potion, running).

type escPending struct {
	id, count int
	m         string
	at        time.Time
	wait      time.Duration // stand-up / cast-bar wait before the item use
}

// escHold: *Bot -> time.Time. Until then useSkill sends no skill but the wing's teleport (AL_TELEPORT 26).
var escHold sync.Map

// escHoldIdx: *Bot -> inventory index of the pending wing (the only item use let through during escHold).
var escHoldIdx sync.Map

// escBlocks (any goroutine): while a wing is pending, drop packets that would void it. QA 2026-11-14:
//   - 15:24:50 UTC Onimage (Wizard) "Fly Wing did nothing (use refused; sitting false, casting false, opt1 0)": a
//     potion from survive() went out within item_use_interval (100 ms) before the wing (canuseitem_tick) - refused;
//   - 16:16 UTC Onimage "no teleport": ground skills (0x0366: Storm Gust, Fire Wall, Hammer Fall - aoe.go,
//     weaponskill.go, skillbuff.go) bypass useSkill; one between the wing and its teleport clears sd->skillitem and
//     starts a cast bar, so the teleport cast is dropped.
func (b *Bot) escBlocks(p []byte) bool {
	if len(p) < 4 || !b.escHolding(0) {
		return false
	}
	switch le.Uint16(p) {
	case 0x0366: // UseSkillToPos
		return true
	case 0x0439: // use item: only the wing itself
		if v, ok := escHoldIdx.Load(b); ok {
			return int(le.Uint16(p[2:])) != v.(int)
		}
	}
	return false
}

// escHolding: a wing is being used; other skills wait (skills.go useSkill).
func (b *Bot) escHolding(skill int) bool {
	if skill == 26 {
		return false
	}
	v, ok := escHold.Load(b)
	return ok && time.Now().Before(v.(time.Time))
}

var escPend sync.Map // *Bot -> escPending

func escItemName(id int) string {
	switch id {
	case itemButterflyWing:
		return "Butterfly Wing"
	case itemFlyWing:
		return "Fly Wing"
	}
	return fmt.Sprintf("item %d", id)
}

func escFailKey(id int) string { return fmt.Sprintf("escfail:%d", id) }

// escItemOK (b.mu held): we carry the escape item, it isn't being skipped after a failure, and nothing stops item
// use (stone/freeze/stun/sleep).
func (b *Bot) escItemOK(id int, now time.Time) bool {
	return b.invIndex(id) >= 0 && now.After(b.buffAt[escFailKey(id)]) && b.opt1 == 0
}

// escFailed (b.mu held): the pending wing didn't work. Skip it 3 s (the next escape tick retries it, now standing and
// out of the cast), 30 s after a second failure within a minute; escape() decides again at once.
func (b *Bot) escFailed(p escPending, how string, now time.Time) {
	escPend.Delete(b)
	escHold.Delete(b)
	skip := 3 * time.Second
	if now.Sub(b.buffAt["escfailat:"+escItemName(p.id)]) < time.Minute {
		skip = 30 * time.Second
	}
	b.buffAt["escfailat:"+escItemName(p.id)] = now
	b.buffAt[escFailKey(p.id)] = now.Add(skip)
	b.escapeAt = time.Time{}
	b.event("info", "", fmt.Sprintf("%s did nothing on %s %d,%d (%s; sitting %v, casting %v, opt1 %d, npc %d, trade %q): something else for %ds",
		escItemName(p.id), b.MapName, b.X, b.Y, how, b.Sitting, now.Before(b.castUntil), b.opt1, b.dialogNPC, b.tradeFrom, int(skip.Seconds())))
}

// escRefused (b.mu held, 0x01c8 result 0): the server refused the item use outright.
func (b *Bot) escRefused(id int, now time.Time) {
	v, ok := escPend.Load(b)
	if !ok || v.(escPending).id != id || now.Sub(v.(escPending).at) > 5*time.Second {
		return
	}
	b.escFailed(v.(escPending), "use refused", now)
}

// escCheck (b.mu held): did the last escape item work? If not, mark it failed for 30 s.
func (b *Bot) escCheck(now time.Time) {
	v, ok := escPend.Load(b)
	if !ok {
		return
	}
	p := v.(escPending)
	if b.MapName != p.m || b.countItem(p.id) < p.count {
		escPend.Delete(b) // it worked
		escHold.Delete(b)
		return
	}
	if now.Sub(p.at) < 2*time.Second+p.wait {
		return
	}
	if now.Sub(p.at) > 30*time.Second {
		escPend.Delete(b) // too old to tell (wings bought since)
		return
	}
	b.escFailed(p, "no teleport", now)
}

// wingEvent (b.mu held): one "wing" event per Fly / Butterfly Wing use, with the trigger, so /api/events can count
// wing use by reason (QA 2026-11-14 10:17 UTC: the first measurement had to guess the trigger from 1.5 s state polls).
// Format: "<item>: <trigger> (hp N%, M on me, lv A vs B, solo|party)".
func (b *Bot) wingEvent(id int, why string, now time.Time) {
	if id == itemButterflyWing {
		// kafra.go kafraNeed: a Butterfly Wing escape (landed in the save town) is a real reason for the next Kafra
		// teleport. Not a Fly Wing: it stays on the map (QA 2026-11-14 14:05-14:26 UTC: every Fly Wing re-opened the
		// A->B->A Kafra block and beginners ping-ponged pay_fild01 <-> pay_fild03).
		b.escWingAt = now
	}
	n, maxLv, _, _ := b.attackers(now)
	who := "solo"
	if partyOf(b) != nil {
		who = "party"
	}
	b.event("wing", "", fmt.Sprintf("%s: %s (hp %d%%, %d on me, lv %d vs %d, %s)", escItemName(id), why,
		int(b.hpFrac()*100), n, maxLv, b.powerLv(), who))
}

// useEscapeItem (b.mu held): close whatever blocks item use (NPC dialog, trade window, chat room, shop window), then use
// the item, and remember it for escCheck. why = the trigger, for the "wing" event.
func (b *Bot) useEscapeItem(id int, now time.Time, why string) {
	idx := b.invIndex(id)
	if idx < 0 {
		return
	}
	b.wingEvent(id, why, now)
	var wait time.Duration
	sitting := b.Sitting || b.wantSit
	if sitting {
		b.wantSit = false
		wait += 400 * time.Millisecond
	}
	if left := b.castUntil.Sub(now); left > 0 && left < 3*time.Second {
		wait += left + 100*time.Millisecond // a cast in progress refuses the wing: wait for it to end
	}
	escPend.Store(b, escPending{id: id, count: b.countItem(id), m: b.MapName, at: now, wait: wait})
	escHold.Store(b, now.Add(wait+2*time.Second))
	escHoldIdx.Store(b, idx)
	b.potAt = now.Add(wait + 1500*time.Millisecond) // no potion in between (escBlocks drops one anyway)
	npc := b.dialogNPC
	b.dialogNPC = 0
	trade := !b.tradeOpen.IsZero() || (b.mkt != nil && b.mkt.open)
	castLeft := wait
	if sitting {
		castLeft -= 400 * time.Millisecond
	}
	go func() {
		if sitting {
			b.stand() // rAthena: no delay-consume item and no teleport cast while sitting
			time.Sleep(400 * time.Millisecond)
		}
		if castLeft > 0 {
			time.Sleep(castLeft)
		}
		if npc != 0 { // whatever state the dialog is in: next, cancel the menu, close
			b.send(newPkt(0x00b9).u32(npc).b)
			b.send(newPkt(0x00b8).u32(npc).u8(255).b)
			b.send(newPkt(0x0146).u32(npc).b)
		}
		if trade {
			b.send(newPkt(0x00ed).b) // cancel the trade window
		}
		b.send(newPkt(0x00e3).b) // leave a chat room
		b.send(newPkt(0x09d4).b) // close an NPC shop window
		time.Sleep(300 * time.Millisecond)
		b.useItem(idx)
	}()
}

// ROOT CAUSE of "Fly Wing / Butterfly Wing did nothing" (QA 2026-11-14 09:37-09:50: 280 in 13 min, 43 of 52 deaths):
// both wings are DelayConsume items whose script is `itemskill "AL_TELEPORT",1` (Fly) / `,3` (Butterfly). rAthena
// answers the use with an OK ack that does not lower the count, runs itemskill, and itemskill only sends
// ZC_AUTORUN_SKILL (0x0147) to the client: the CLIENT must then cast the skill (CZ_USE_SKILL 0x0438 on itself), and
// only that cast teleports and consumes the wing. The Legion ignored 0x0147, so no wing ever worked (picklog: 0
// Butterfly Wings consumed in 14 h, the few Fly Wings were OpenKore's). Now we cast every self-target autorun skill.
// 0x0147: skill id u16 @2, inf u32 @4, lv u16 @8, sp u16 @10, range u16 @12, name[24] @14, up flag @38.
func (b *Bot) onAutorunSkill(pk []byte) {
	if len(pk) < 39 {
		return
	}
	id, inf, lv := int(le.Uint16(pk[2:])), le.Uint32(pk[4:]), int(le.Uint16(pk[8:]))
	if b.tracing() {
		b.tracef("RECV 0147 autorun skill %d lv%d inf=%d", id, lv, inf)
	}
	if inf&4 == 0 && id != 26 { // INF_SELF_SKILL; AL_TELEPORT is self-cast either way
		return
	}
	aid := b.aid
	sitting := b.Sitting
	go func() {
		if sitting {
			b.stand() // the cast is dropped while sitting
			time.Sleep(300 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond) // after the ack, like a client
		b.useSkill(id, lv, aid)
	}()
}
