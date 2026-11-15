package main

// idleerrand.go (EC36, owner 2026-11-14: "if they have nothing to do at least go sell their stuff. It's almost full.
// Stop doing nothing useful."): a bot that stands waiting in a town with a Tool Dealer - for its party, a rally at the
// Kafra, a late member, its leader - for idle_errand_wait_sec (45 s) uses the time: with the bag at
// idle_errand_weight_pct (35%) or idle_errand_slots (85 entries) and loot to sell, or short of potions with the zeny
// for them, it walks to that town's Tool Dealer (a normal town trip: sell junk, restock), then the rally / party logic
// takes it back to the meeting point. Over the 50% regen line needTrip already sends it ("selling in town"); this
// covers the band below and the slot-full bag. It says "brb" in party chat (Ayla's plugin then doesn't wait for it,
// and the trip's "back" line ends that). One errand per idle_errand_cooldown_min (10) per bot.
// Switch: tuning.json idle_errand_off (default false = on).

import (
	"strings"
	"time"
)

// idleWaitState: the states in which the bot is only waiting for others (party.go, humanparty.go, rally.go,
// together.go). Trade meetings ("waiting for <buyer>") never reach the trip block: marketThink owns the bot first.
func idleWaitState(s string) bool {
	return strings.HasPrefix(s, "waiting for ") || strings.HasPrefix(s, "waiting in ") ||
		strings.HasPrefix(s, "waiting outside for ") || strings.HasPrefix(s, "rally: waiting")
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

// idleErrand (b.mu held; called from the town-trip block of think with b.trip == nil): start the errand when due.
func (b *Bot) idleErrand(p *Party, now time.Time) {
	t := tune()
	if t.IdleErrandOff || b.trip != nil || b.errand != nil || b.kafra != nil {
		return
	}
	// how long it has been waiting: the state is the last tick's; a gap in the calls (fighting, moving) restarts it
	if !idleWaitState(b.State) || now.Sub(b.buffAt["idlewait:seen"]) > 5*time.Second {
		b.buffAt["idlewait:since"] = now
	}
	b.buffAt["idlewait:seen"] = now
	if !idleWaitState(b.State) || now.Before(b.buffAt["idlewait:next"]) {
		return
	}
	town := townOf(b.MapName)
	shopTown := town != "" && potionShop(town) != nil
	why := idleErrandWhy(t, now.Sub(b.buffAt["idlewait:since"]), shopTown, b.weightPct(), b.invSlots(), b.hasLoot(), b.needsPots(), b.Zeny)
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
