package main

// idleerrand.go (EC36 as rewritten 2026-11-14: "A solo bot, or a party gathering at a Kafra before setting out, uses idle
// time for errands (sell, store, restock). A party member does errands only on the party town run (PT22/PT24) or at 50%
// weight (EC28); otherwise it farms near the party (PT18)."):
//   - a SOLO bot standing in any waiting state for idle_errand_wait_sec (45 s) in a town with a Tool Dealer;
//   - a PARTY member only while its party gathers at a Kafra before setting out (the rally wait at the Kafra,
//     rally.go "rally: waiting at the Kafra in <town>") - never from a field, never away from the party;
// with the bag at idle_errand_weight_pct (35%) or idle_errand_slots (85 entries) and loot to sell, or short of potions
// with the zeny for them, walks to that town's Tool Dealer (a normal town trip: sell junk, restock); then the rally /
// party logic takes it back to the Kafra. The party town run (ai.go townRun join) and the 50% line (needTrip, EC28)
// stay as they are. A party member says "brb" in party chat (Ayla's plugin then doesn't wait for it, and the trip's
// "back" line ends that). One errand per idle_errand_cooldown_min (10) per bot.
// Switch: tuning.json idle_errand_off (default false = on).

import (
	"strings"
	"time"
)

// idleWaitState: the states in which the bot may use the wait for an errand. A party member: only the party's
// gathering at a Kafra (rally.go). A solo bot: any wait (party.go, humanparty.go, together.go leftovers, the rally).
// Trade meetings ("waiting for <buyer>") never reach the trip block: marketThink owns the bot first.
func idleWaitState(s string, inParty bool) bool {
	if strings.HasPrefix(s, "rally: waiting at the Kafra") {
		return true
	}
	if inParty {
		return false
	}
	return strings.HasPrefix(s, "waiting for ") || strings.HasPrefix(s, "waiting in ") ||
		strings.HasPrefix(s, "waiting outside for ")
}

// idleErrandWhy: the errand worth doing in the wait, or "" (pure: unit tested)
func idleErrandWhy(t *Tuning, waited time.Duration, shopTown bool, weightPct, slots int, hasLoot, needsPots bool, zeny int) string {
	if t.IdleErrandOff || !shopTown || waited < time.Duration(t.IdleErrandWaitSec)*time.Second {
		return ""
	}
	if hasLoot && (weightPct >= t.IdleErrandWeightPct || slots >= t.IdleErrandSlots) {
		return "selling loot while waiting"
	}
	if needsPots && zeny >= 500 {
		return "restocking while waiting"
	}
	return ""
}

// idleRestockNeed: the restock errand is for a real potion need only - fewer than 3 real potions, the same line as the
// trip trigger (town.go outOfPotions). SP potions and a bag of 14 potions are no reason (QA 2026-11-15).
func idleRestockNeed(b *Bot) bool {
	return b.realPotCount() < 3
}

// idleErrand (b.mu held; called from the town-trip block of think with b.trip == nil): start the errand when due.
func (b *Bot) idleErrand(p *Party, now time.Time) {
	t := tune()
	if t.IdleErrandOff || b.trip != nil || b.errand != nil || b.kafra != nil {
		return
	}
	// how long it has been waiting: the state is the last tick's; a gap in the calls (fighting, moving) restarts it
	waiting := idleWaitState(b.State, p != nil)
	if !waiting || now.Sub(b.buffAt["idlewait:seen"]) > 5*time.Second {
		b.buffAt["idlewait:since"] = now
	}
	b.buffAt["idlewait:seen"] = now
	if !waiting || now.Before(b.buffAt["idlewait:next"]) {
		return
	}
	town := townOf(b.MapName)
	shopTown := town != "" && potionShop(town) != nil
	// the restock errand only for real potion need (fewer than 3 real potions, the same line as the trip trigger in
	// town.go outOfPotions): a bot with 14 potions and low SP made "restocking while waiting" trips (QA 2026-11-15)
	why := idleErrandWhy(t, now.Sub(b.buffAt["idlewait:since"]), shopTown, b.weightPct(), b.invSlots(), b.hasLoot(), idleRestockNeed(b), b.Zeny)
	if why == "" {
		return
	}
	b.buffAt["idlewait:next"] = now.Add(time.Duration(t.IdleErrandCooldownMin) * time.Minute)
	b.forceTown = town
	b.startTrip(why, now)
	b.forceTown = ""
	if b.trip == nil {
		return
	}
	b.event("info", "", "EC36 errand while the party waits: "+why+" in "+town+" "+b.tripNumbers())
	if p != nil {
		typing := b.P.Typing
		line := pick("brb selling my stuff while we wait", "brb, quick sell run while we wait", "bag's heavy, brb selling at the tool dealer")
		if strings.HasPrefix(why, "restocking") {
			line = pick("brb getting pots while we wait", "brb, grabbing potions while we wait")
		}
		go b.partySay(styled(typing, line))
	}
}
