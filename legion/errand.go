package main

// errand.go: a party member leaves briefly for a necessary build/class step (owner rule 2026-11-14, RULES.md 3: "any
// necessary step for the build or the class, bots can leave party and take care of its own business before coming
// back. It's OK to not be with the group temporarily. It's just common sense.").
//
// The member announces it in party chat ("brb, renting a peco"), goes alone (a solo town trip: Butterfly Wing /
// walk / Kafra), does the step, returns to the party map, and says "back" once it is with the leader again. While
// on an errand its published state carries Errand: the leader doesn't collect it (needsCollect), the rally doesn't
// wait for it (rallyMembers), nobody holds a party rest for it (partyRester / Party.resting), a human-led member
// doesn't call "where r u" - and the party keeps farming. Ayla's plugin ignores a friend between its "brb ..." and
// "back" lines.
//
// Limits: one errand at a time, errandCooldown between errand starts (a failed step can't loop: peco.go also rests
// 2 h after a failed rental), only when the member isn't needed right now (not fighting, nobody in the party losing a
// fight, no rally) and not the bot party's leader (the party follows its leader: its own step rides on a party run).
// Errands so far: the Peco Peco rental (peco.go). The same frame takes the other steps (job change, falcon, class
// weapon, quest skill, refine) as they move over.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type errand struct {
	Kind    string // "peco"
	Why     string // what it said: "renting a peco"
	Started time.Time
	Done    bool      // the step is over (done or given up): now it only goes back to the party
	DoneAt  time.Time //
}

const (
	errandCooldown = 2 * time.Hour    // between errand starts of one member
	errandMax      = 30 * time.Minute // an errand never lasts longer than this
	errandStepMax  = 15 * time.Minute // the step itself (reach the town, do it) - then it's over, done or not
)

// errandCanLeave (b.mu held): a party member that isn't needed right now - no fight on, nobody in the party losing
// a fight, no rally, no town business going on, HP fine.
func (b *Bot) errandCanLeave(p *Party, now time.Time) bool {
	if p == nil || b.errand != nil || b.trip != nil || b.kafra != nil || b.mkt != nil || b.vend != nil || b.Dead {
		return false
	}
	if p.Human == "" && p.Leader == b {
		return false // a bot leader leads: its step rides on a party run (ai.go partyRunTown)
	}
	if now.Before(b.buffAt["errand:cd"]) || b.target != 0 || b.underAttack(now) || b.fightOn(now) || b.hpFrac() < 0.6 {
		return false
	}
	if p.rallyOn() {
		return false
	}
	// economy run 9 (QA 14:00: several of Ayla's friends off on separate errands, the party over 5 maps): one member
	// away on an errand at a time
	if v, ok := partyErrand.Load(p); ok {
		if pe := v.(partyErrandRec); pe.name != b.charName() && now.Sub(pe.at) < errandMax {
			return false
		}
	}
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && memberInDanger(s) {
			return false
		}
	}
	return true
}

// errandNeed (b.mu held): the necessary step this member should leave for now ("" = none).
func (b *Bot) errandNeed(now time.Time) (kind, say string) {
	if _, _, ok := b.wantsPeco(now); ok && b.MapName != "prontera" {
		return "peco", pick("renting a peco", "getting my peco", "gotta rent a peco in prontera")
	}
	// song.go: a partied Bard / Dancer with a song and no instrument / whip buys one (Prontera's weapon shop)
	if b.songSupport() && !b.songWeaponOwned() && !b.gearFailed[b.songBuy()] {
		if x := items[b.songBuy()]; x != nil && x.Buy > 0 && b.Zeny >= x.Buy+songReserve {
			return "song", "buying a " + x.Name + " for my songs"
		}
	}
	// economy run 8 (QA: Hunters lv58-68 in parties wielded a Knife with a Bow in the bag and no arrows, waiting for a
	// party run): arrows are a necessary class step (owner rule 3: "buying its class weapon or arrows") - an errand
	if errandFieldMap(b.MapName) && now.After(b.arrowNeedAt) {
		if b.noArrows() && b.arrowsWanted(arrowPrice(), false) >= minArrowTrip {
			return "arrows", "buying arrows"
		}
		if b.arrowLootOK() {
			return "arrows (sell loot)", "selling loot for arrows"
		}
	}
	// trap.go: a partied trapper Hunter low on Traps fetches them alone (owner rule 3: a necessary class step), the
	// party keeps farming; trapTripDue keeps it to one supply run per 30 min and only when a real restock is affordable
	if errandFieldMap(b.MapName) && b.trapTripDue(now) {
		return "traps", pick("getting traps", "out of traps, restocking", "brb buying traps")
	}
	// economy run 10: a guild brewer's bottles / Scell (alchemy.go brewSupplyNeed) - from a town or a field map
	if errandFieldMap(b.MapName) && b.brewSupplyNeed(now) {
		return "brew supplies", pick("getting potion bottles for brewing", "need bottles n scell for potions", "getting brewing stuff in prontera")
	}
	// economy run 8: the class weapon (a Rod for a Mage on a Knife, a Bow for an Archer) or a weapon far below the
	// level is a necessary build step: a short errand instead of "waits for the next party run" (~100 members said
	// so every 30 min; party runs come only from a real need, so the weapon waited for hours). Not from a dungeon:
	// the walk back alone is where members die (Zen.fox, pay_dun01). gearStepReady keeps it to one per hour.
	if errandFieldMap(b.MapName) && b.gearStepReady(now) {
		if it, s := b.classWeaponNeed(); it != nil && s != nil && gearSlot(it.Loc) == 0x2 {
			return "weapon", "buying a " + it.Name
		}
		if it, s := b.classWeaponLootNeed(); it != nil && s != nil {
			return "weapon (sell loot)", "selling loot for a " + it.Name
		}
	}
	return "", ""
}

// errandFieldMap: an errand may start here - not a dungeon / sewer / pyramid, where the walk back alone is deadly.
func errandFieldMap(m string) bool {
	for _, x := range []string{"dun", "sew", "pryd", "anthell", "_in", "gld", "tower"} {
		if strings.Contains(m, x) {
			return false
		}
	}
	return m != ""
}

// errandThink (b.mu held, from think before tripThink): start an errand when one is due, follow it, end it.
func (b *Bot) errandThink(now time.Time) {
	p := partyOf(b)
	e := b.errand
	if e == nil {
		if !b.errandCanLeave(p, now) {
			return
		}
		kind, say := b.errandNeed(now)
		if kind == "" {
			return
		}
		cd := errandCooldown
		if strings.HasPrefix(kind, "weapon") || strings.HasPrefix(kind, "arrows") || kind == "brew supplies" || kind == "traps" {
			cd = 20 * time.Minute // run 8: short shop steps (gearStepReady / arrowNeedAt keep them from looping)
		}
		b.buffAt["errand:cd"] = now.Add(cd)
		if kind == "peco" {
			b.buffAt["errand:cd"] = now.Add(pecoRetry) // the Peco is the rider's top priority: retry sooner
		}
		b.errand = &errand{Kind: kind, Why: say, Started: now}
		partyErrand.Store(p, partyErrandRec{b.charName(), now})
		b.event("party", "", "errand: "+say+" (back soon) "+b.tripNumbers())
		typing := b.P.Typing
		go b.partySay(styled(typing, "brb, "+say))
		switch kind {
		case "peco":
			b.forceTown = ""
			b.startTrip("Peco Peco ride", now) // town.go: the errand's solo trip goes to Prontera; peco.go rents there
		case "song":
			b.gearPick, b.forceTown = b.songBuy(), "prontera" // town.go startTrip: the gear trip buys this in Prontera
			b.startTrip("gear", now)
			b.forceTown = ""
			if b.trip == nil {
				b.errand.Done, b.errand.DoneAt = true, now // no shop found: nothing to do
			}
		case "arrows", "arrows (sell loot)":
			b.forceTown = ""
			why := "no arrows"
			if kind == "arrows (sell loot)" {
				why = "no arrows (sell loot)"
			}
			b.startTrip(why, now) // town.go: an arrow town's shop (arrowTown)
			if b.trip == nil {
				b.errand.Done, b.errand.DoneAt = true, now
			}
		case "traps":
			b.forceTown = ""
			if !b.startTrapTrip(now) { // trap.go: the nearest Trap seller (Izlude / Prontera south gate ...)
				b.errand.Done, b.errand.DoneAt = true, now
			}
		case "brew supplies":
			b.forceTown = ""
			if !b.alchSupplyTrip(b.alchSt(), now) {
				b.errand.Done, b.errand.DoneAt = true, now
			}
		case "weapon", "weapon (sell loot)":
			why := "class weapon"
			if kind == "weapon (sell loot)" {
				why = "class weapon (sell loot)"
			}
			b.forceTown = ""
			b.startTrip(why, now) // town.go: the solo trip buys it at the shop classWeaponNeed picked (noteGearStep)
			if b.trip == nil {
				b.errand.Done, b.errand.DoneAt = true, now
			}
		}
		return
	}
	if p == nil {
		b.errand = nil // left the party: nothing to come back to
		return
	}
	if !e.Done {
		switch {
		case e.Kind == "peco" && b.option&optRiding != 0:
			e.Done = true
		case e.Kind == "peco" && now.Before(b.buffAt["peco:wait"]): // peco.go gave up (2 h rest) or rented
			e.Done = true
		case e.Kind == "song" && (b.songWeaponOwned() || b.trip == nil && now.Sub(e.Started) > time.Minute):
			e.Done = true // bought (or the trip ended without it: town.go gearFailed keeps it from looping)
		case (strings.HasPrefix(e.Kind, "weapon") || strings.HasPrefix(e.Kind, "arrows") || e.Kind == "brew supplies" || e.Kind == "traps") && b.trip == nil && now.Sub(e.Started) > time.Minute:
			e.Done = true // the gear trip ended (bought, or town.go gearFailed keeps it from looping)
		case e.Kind == "alchemy": // alchemy.go: the Alchemist Union trip is over when the dealer stage ends
			if v, ok := alchStates.Load(b); !ok || v.(*alchState).Stage != "dealer" {
				e.Done = true
			}
		case now.Sub(e.Started) > errandStepMax && b.pecoBusy() == false:
			e.Done = true
		}
		if e.Done {
			e.DoneAt = now
			b.event("party", "", fmt.Sprintf("errand over (%s): going back to the party", e.Why))
		}
	}
	back := false
	if e.Done {
		lead, ok, _ := b.partyView(p, now)
		if ok && lead.Online && lead.Map == b.MapName && !(lead.X == 0 && lead.Y == 0) {
			mx, my := b.livePos(now)
			lx, ly := livePosOf(lead)
			back = dist(mx, my, lx, ly) <= togetherCells() || lead.Name == b.charName()
		}
		if p.Human == "" && p.Leader == b {
			back = true // became the leader meanwhile
		}
	}
	maxDur := errandMax
	if e.Kind == "alchemy" {
		maxDur = 50 * time.Minute // the Al De Baran trip may take 45 min (alchemy.go dealerThink)
	}
	if back || now.Sub(e.Started) > maxDur {
		b.errand = nil
		if v, ok := partyErrand.Load(p); ok && v.(partyErrandRec).name == b.charName() {
			partyErrand.Delete(p)
		}
		b.event("party", "", fmt.Sprintf("errand finished (%s) after %s", e.Why, now.Sub(e.Started).Round(time.Second)))
		typing := b.P.Typing
		go b.partySay(styled(typing, "back"))
	}
}

// pecoBusy (b.mu held): talking to / walking to the breeder right now.
func (b *Bot) pecoBusy() bool {
	_, ok := pecoTasks.Load(b)
	return ok
}

// onErrand (b.mu held).
func (b *Bot) onErrand() bool { return b.errand != nil }

// partyErrand: the party member currently away on an errand (*Party -> partyErrandRec); one at a time (run 9)
var partyErrand sync.Map

type partyErrandRec struct {
	name string
	at   time.Time
}
