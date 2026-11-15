package main

// Party members don't walk out of a dungeon to reach the party (owner 2026-11-14: "Ayla's party is walking instead of
// using the Kafra"; live 02:31-02:55Z: the Priest Zendancer84 walked pay_dun01 -> Payon Kafra for 20+ min - 400-500
// cells, Fly Wing escapes, "travel stalled" - with 0 Butterfly Wings and Teleport lv2 learned, while Ayla waited in
// Geffen). Two gaps:
//   - kafraVia only flew with 2+ Butterfly Wings ("keep the last for escapes"): a party member that must reach its
//     party far away (in a dungeon, or 4+ maps from the Kafra) may spend its last wing - it restocks in town;
//   - Teleport lv2 (Acolyte line) goes to the save point like a Butterfly Wing, for SP only - but the warp list was
//     always answered "Random". It now counts as a wing for that trip (warpReply picks the save point entry).
//   - Ayla's friends keep 2 wings when they can afford them (bwWant), so the next far move has one.
// Off with tuning party_dest_off (with the rest of PT30).

import "time"

// saveTeleOK (b.mu held): Teleport lv2 can take us to the save point now.
func (b *Bot) saveTeleOK(now time.Time) bool {
	si := b.skills[alTeleport]
	return partyDestOn() && si != nil && si.Lv >= 2 && b.canSkillTele(now)
}

// travelWingOK (b.mu held): kafraVia may use a Butterfly Wing (or Teleport lv2) to the save point on the way to goal.
// here = map changes the walk to goal would take.
func (b *Bot) travelWingOK(here int, now time.Time) bool {
	n := b.countItem(itemButterflyWing)
	if n >= 2 {
		return true // the old rule: the last one stays for escapes
	}
	if !partyDestOn() || !b.followingParty() || !(dungeonMap(b.MapName) || here >= 4) {
		return false
	}
	return n >= 1 || b.saveTeleOK(now)
}

// flySave (b.mu held): to the save point by Butterfly Wing, else by Teleport lv2. false = neither was possible.
func (b *Bot) flySave(why string, now time.Time) bool {
	if idx := b.invIndex(itemButterflyWing); idx >= 0 {
		b.wingAt = now
		b.wingEvent(itemButterflyWing, why, now)
		go b.useItem(idx)
		return true
	}
	if b.saveTeleOK(now) {
		b.wingAt, b.teleSaveAt = now, now
		b.buffAt["tele:cd"] = now.Add(4 * time.Second)
		b.skillReady = now.Add(800 * time.Millisecond)
		b.target, b.path = 0, nil
		b.event("wing", "", "Teleport to the save point: "+why)
		aid, sitting := b.aid, b.Sitting
		go func() {
			if sitting {
				b.stand()
				time.Sleep(300 * time.Millisecond)
			}
			b.useSkill(alTeleport, 2, aid) // lv2: the warp list has the save map (warpReply picks it)
		}()
		return true
	}
	return false
}

// warpReply (b.mu held): the answer to our Teleport warp list: the save point right after a flySave, else "Random".
func (b *Bot) warpReply(pk []byte) []byte {
	return warpListReplySave(pk, !b.teleSaveAt.IsZero() && time.Since(b.teleSaveAt) < 5*time.Second)
}

// warpListReplySave: CZ_SELECT_WARPPOINT for ZC_WARPLIST (u16 op, u16 len, u16 skill, 16-byte map names). save =
// the entry after "Random" (Teleport lv2 lists the save map there; rAthena sends any non-"Random" pick to the save
// point); without one, "Random".
func warpListReplySave(pk []byte, save bool) []byte {
	r := warpListReply(pk)
	if r == nil || !save || len(pk) < 6+32 {
		return r
	}
	name := make([]byte, 16)
	copy(name, pk[6+16:6+32])
	if name[0] == 0 {
		return r
	}
	return append(newPkt(0x011b).u16(alTeleport).b, name...)
}

// bwWantParty (b.mu held): Butterfly Wings a human leader's friend keeps: at least 2 when it can pay for them and keep
// 1,000z (its party moves far and often - the next far move needs one).
func (b *Bot) bwWantParty(base int) int {
	// queue b17947117209mo (PR16): any party member, not only a human leader's friend (Zendancer84 had 0 wings at 7k
	// zeny and walked out of pay_dun01 for 20+ min); party town runs (PT22) start with a wing
	if base >= 2 || b.pin == nil && partyOf(b) == nil || b.pin != nil && !partyDestOn() {
		return base
	}
	if b.Zeny >= 2*wingPrice()+1000 {
		return 2
	}
	return base
}
