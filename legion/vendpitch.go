package main

// vendpitch.go (bug b1794719728496-252, Prontera market stall: shops_open fell from 57 to 19, vendors "setting up").
//
// Three leaks emptied the south gate market over a long run:
//  1. every ZC_ACK_TOUSESKILL 0x0110 for MC_VENDING burned the vendor's pitch for the session (vendPitchRefused), but
//     rAthena also sends it when the SHOP is refused (vending_openvending: an item that can't be vended, item count)
//     and for SP / other causes - nothing to do with the cell. 888 "vend pitch refused: moved" against 84 pitches:
//     the free list ran dry and the remaining vendors loitered without a pitch. Now only a cast-time refusal for the
//     cell itself (cause 83 "NPC around", or cause 0 before any shop was sent: a no-vending cell) burns the pitch.
//  2. a vendor that stopped vending (sold out, went farming, canVend false) kept its pitch in market.taken until it
//     logged out (TheWolfsmithStore fighting Muka in moc_fild01 with a pitch held). Pitches whose holder has not run
//     vendThink in Prontera for pitchStale go back on the free list; a returning vendor takes a new one.
//  3. a vendor whose cart moves never happen (the server still in "preparing a shop" or an open shop the engine
//     thinks closed: pc_putitemtocart refuses silently during prevend/vending) sat on "loading the cart" forever
//     (Shadow_rogue Shop / sunlordStore at 163,56-57 for 1+ min, bag unchanged). After a few tries the shop setup is
//     cancelled (CZ_REQ_OPENSTORE2 flag 0 clears prevend) and any open shop closed; if that does not help, the vendor
//     stops vending for a while (farms) instead of standing in the market as a dead shop.
// All of it is on by default; tuning vend_pitch_fix_off true restores the old behaviour.

import (
	"fmt"
	"time"
)

const (
	useskillFailLevel     = 0  // USESKILL_FAIL_LEVEL: rAthena's generic cause (no-vending cell, refused shop)
	useskillFailNPCAround = 83 // USESKILL_FAIL_THERE_ARE_NPC_AROUND (min_npc_vendchat_distance)

	pitchStale     = 3 * time.Minute  // a pitch whose holder has not been vending in Prontera this long goes back
	shopAskWindow  = 10 * time.Second // a 0x0110 this soon after our shop list is the shop's refusal, not the cell's
	cartLoadReset  = 4                // empty-cart load rounds (6 s apart) before the shop setup is cancelled
	cartLoadGiveUp = 8                // ... before the vendor stops vending for vendPauseFor
	vendPauseFor   = 20 * time.Minute
)

var pitchSeen = map[*Bot]time.Time{} // under market's lock: last vendThink in Prontera of each pitch holder

func vendPitchFixOn() bool {
	t := tune()
	return t == nil || !t.VendPitchFixOff
}

// vendSkillCause: the cause byte of a ZC_ACK_TOUSESKILL (2021 layout: skill 2, btype 4, itemId 8, flag 12, cause 13)
func vendSkillCause(pk []byte) int {
	if len(pk) >= 14 {
		return int(pk[13])
	}
	return -1
}

// pitchRefusal: does this Vending refusal mean the cell itself is unusable? shopAskAt: when we last sent a shop list.
func pitchRefusal(cause int, shopAskAt, now time.Time) bool {
	if !shopAskAt.IsZero() && now.Sub(shopAskAt) < shopAskWindow {
		return false // vending_openvending refused the shop (onOpenStoreAck handles it); the pitch is fine
	}
	return cause == useskillFailNPCAround || cause == useskillFailLevel
}

// vendRefused: 0x0110 for MC_VENDING (bot.go; b.mu held)
func (b *Bot) vendRefused(pk []byte, now time.Time) {
	if !vendPitchFixOn() {
		b.vendPitchRefused()
		return
	}
	if b.vend == nil || b.vend.Opened {
		return
	}
	cause := vendSkillCause(pk)
	if pitchRefusal(cause, b.vend.shopAskAt, now) {
		b.vendPitchRefused() // vend.go: the pitch is dropped for the session, another one taken
		return
	}
	econCount(fmt.Sprintf("vend refused (not the pitch): cause %d", cause))
	b.vend.TriedAt = now // try again at the same pitch after the usual 12 s
}

// notePitch: the vendor is in Prontera running its shop logic (vendThink; b.mu held, takes market's lock). Keeps its
// pitch from being reclaimed; a pitch reclaimed while it was away is retaken if still free, else dropped (a closed
// shop then takes a new pitch; an open shop keeps standing where it is).
func (b *Bot) notePitch(now time.Time) {
	if !vendPitchFixOn() || b.vend == nil {
		return
	}
	v := b.vend
	market.Lock()
	defer market.Unlock()
	p, held := market.taken[b]
	if held {
		pitchSeen[b] = now
		if v.Spot != ([2]int{}) && p != v.Spot { // can't happen today; keep the two in step
			v.Spot = p
		}
		return
	}
	if v.Spot == ([2]int{}) {
		return
	}
	for i, f := range market.free {
		if f == v.Spot {
			market.free = append(market.free[:i], market.free[i+1:]...)
			market.taken[b] = f
			pitchSeen[b] = now
			return
		}
	}
	if !v.Opened {
		v.Spot = [2]int{} // someone else stands there now: take another pitch
	}
}

// reclaimStalePitchesLocked: pitches of vendors that left the market go back on the free list (market's lock held).
// Returns how many were reclaimed (counted by the caller outside the lock).
func reclaimStalePitchesLocked(now time.Time) int {
	n := 0
	for vb, p := range market.taken {
		if t, ok := pitchSeen[vb]; ok && now.Sub(t) <= pitchStale {
			continue
		}
		delete(market.taken, vb)
		delete(pitchSeen, vb)
		market.free = append(market.free, p)
		n++
	}
	return n
}

// pitchTakenLocked: a pitch was just handed to b (takePitch, market's lock held)
func pitchTakenLocked(b *Bot, now time.Time) {
	pitchSeen[b] = now
}

// forgetPitchLocked: b's pitch is gone (released or burned; market's lock held)
func forgetPitchLocked(b *Bot) {
	delete(pitchSeen, b)
}

// cartLoadStuck: called after a load round sent n > 0 cart moves while the cart is still empty (b.mu held). True when
// it acted (the caller returns).
func (b *Bot) cartLoadStuck(now time.Time) bool {
	if !vendPitchFixOn() || b.vend == nil {
		return false
	}
	v := b.vend
	v.loadTries++
	switch {
	case v.loadTries >= cartLoadGiveUp:
		v.loadTries = 0
		v.pauseUntil = now.Add(vendPauseFor)
		v.Spot = [2]int{}
		go b.send(newPkt(0x012e).b)
		releasePitch(b)
		econCount("vendor cart load stuck: vending paused")
		b.event("info", "", "my cart won't take anything, I'll farm a while and set up later")
		return true
	case v.loadTries == cartLoadReset:
		// prevend (a Vending cast whose shop list never went out) blocks every cart move: cancel it, and close a shop
		// the server may still hold open for us; both are no-ops when there is nothing to cancel or close
		title := v.Title
		if title == "" {
			title = "Cheap stuff"
		}
		go b.send(newPkt(0x01b2).u16(0).str(title, 80).u8(0).varlen())
		go b.send(newPkt(0x012e).b)
		econCount("vendor cart load stuck: shop setup reset")
		b.setState("loading the cart (resetting the shop)")
		return true
	}
	return false
}

// vendPaused: the vendor gave up on its cart for a while (canVend; b.mu held)
func (b *Bot) vendPaused(now time.Time) bool {
	return vendPitchFixOn() && b.vend != nil && now.Before(b.vend.pauseUntil)
}

// ---------------------------------------------------------------- shop stock already promised to a buyer

// shopReserved: how many of item the buyers already walking to vendor's shop will take (vendBuys in flight). Several
// buyers were sent to the same one-off listing (the cheapest shop wins every match) and all but the first found it
// gone: "shop buy: not in the shop at that price any more" was 140 of 486 shop trips. Reads only the fields set when
// a vendBuy is created plus its atomic end flag (no buyer lock).
func shopReserved(vendor *Bot, item int, now time.Time) int {
	if !vendPitchFixOn() || vendor == nil {
		return 0
	}
	n := 0
	vendBuys.Range(func(_, x any) bool {
		vb := x.(*vendBuy)
		if vb.Vendor == vendor && vb.ItemID == item && !vb.ended.Load() && now.Before(vb.Deadline) {
			n += vb.Count
		}
		return true
	})
	return n
}

type shopPromise struct {
	vendor *Bot
	item   int
}

// shopAvailable: a copy of a shop's listing less what walking buyers will take (browseShops)
func shopAvailable(vendor *Bot, ls []vendListing, now time.Time) []vendListing {
	out := make([]vendListing, 0, len(ls))
	for _, l := range ls {
		l.Count -= shopReserved(vendor, l.ItemID, now)
		if l.Count > 0 {
			out = append(out, l)
		}
	}
	return out
}
