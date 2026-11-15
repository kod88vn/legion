package main

import (
	"strings"
	"testing"
	"time"
)

// townrest_test.go: tick-level check of the town rest (heal.go lowHPGuard + the ai.go rest rule around ai.go:1583-1619).
// The real think() needs the whole world, so each tick runs lowHPGuard and, when it hands the tick back, the ai.go rest
// rule as a model. The server is modelled too:
//   - natural regen: sitting 1% per tick, standing a quarter of that, and NONE at 50% weight or more (rAthena);
//   - eating removes the food from the bag and adds its HP; drinking a potion removes it and heals 55 (Red Potion);
//   - ai.go starts no rest at 50% weight or more (its sit condition and its done line both check overRegenWeight).

type townTick struct {
	ticks      int
	hpReached  int  // first tick at or over the done line (-1: never)
	eaten      int  // snacks eaten in total
	eatenAfter int  // snacks eaten once the done line was reached
	drunk      int  // potions drunk in total
	sat        int  // ticks spent seated
	restEnd    bool // lowHPGuard still owns the bot at the end
}

func useBagItem(b *Bot, ids func(int) bool, heal int) bool {
	for _, it := range b.inv {
		if ids(it.ItemID) && it.Count > 0 && (heal <= 0 || heal <= b.MaxHP-b.HP) {
			it.Count--
			b.HP = min(b.MaxHP, b.HP+max(heal, 0))
			return true
		}
	}
	return false
}

func runTownRest(b *Bot, ticks int, done float64, now time.Time) townTick {
	var r townTick
	r.hpReached = -1
	restAt := (0.3 + 0.35*(1-b.P.Risk)) * neutralTactics.Rest
	for i := 0; i < ticks; i++ {
		now = now.Add(time.Second)
		r.ticks = i + 1
		if b.Sitting && !b.wantSit {
			b.Sitting = false // the stand command is in: the server stands the bot
		}
		owned := b.lowHPGuard(nil, false, now)
		if owned && strings.Contains(b.State, "eating") {
			r.eaten++
			if r.hpReached >= 0 {
				r.eatenAfter++
			}
			for _, it := range b.inv { // the food that fits: one eaten, its HP added
				if h := foodHeal[it.ItemID]; h > 0 && it.Count > 0 && h <= b.MaxHP-b.HP {
					it.Count--
					b.HP = min(b.MaxHP, b.HP+h)
					break
				}
			}
		}
		if owned && strings.Contains(b.State, "drinking a potion") {
			r.drunk++
			useBagItem(b, isHealPotion, 55)
		}
		if !owned {
			// ai.go:1610 sits only when not over the regen weight; ai.go:1588 ends a rest at that weight
			if b.hpFrac() < restAt && !b.Sitting && !b.overRegenWeight() {
				b.wantSit = true
				b.Sitting = true // the server confirms the sit
			} else if b.wantSit && (b.hpFrac() >= 0.95 || b.overRegenWeight()) {
				b.wantSit = false
				b.Sitting = false
			}
		}
		if b.Sitting {
			r.sat++
		}
		if !b.overRegenWeight() { // rAthena: no natural regen at 50% weight or more
			if b.Sitting {
				b.HP += b.MaxHP / 100
			} else {
				b.HP += b.MaxHP / 400
			}
		}
		b.HP = min(b.MaxHP, b.HP)
		if r.hpReached < 0 && b.hpFrac() >= done {
			r.hpReached = i + 1
		}
		if !owned && r.hpReached >= 0 {
			break // the rest is over and the bot is back over the line
		}
		r.restEnd = owned
	}
	return r
}

func townBlacksmith(hp int, withFood bool) *Bot {
	b := restBot(10, hp, 3850, 0, 0) // Blacksmith lv71: no SP
	b.BaseLv = 71
	b.MapName = "aldebaran"
	b.Goal = Goal{}        // no goal
	b.diedAt = time.Time{} // no recent death
	b.inv = map[int]*InvItem{}
	b.MaxWeight = 0
	if withFood {
		b.inv[0] = &InvItem{ItemID: 507, Count: 20} // Red Herb: 23 HP each
	}
	return b
}

// QA 2026-11-15 b1794751009271-548: a Blacksmith at 58% in town, no death, no goal.
func TestTownRestBlacksmithReachesDoneLine(t *testing.T) {
	b := townBlacksmith(2233, true)
	r := runTownRest(b, 600, lowHPDone, time.Now())
	if r.hpReached < 0 {
		t.Fatalf("58%% in town never reached the %.0f%% done line in %d ticks (hp %d/%d)",
			lowHPDone*100, r.ticks, b.HP, b.MaxHP)
	}
	if r.hpReached > 200 {
		t.Fatalf("rest took %d ticks to reach %.0f%%", r.hpReached, lowHPDone*100)
	}
	if r.eatenAfter > 0 {
		t.Fatalf("%d snacks eaten after the done line", r.eatenAfter)
	}
}

// Same bot after a death 3 min ago: the post-death rule wants 90% in town (lowHPGuard, restsplit.go).
func TestTownRestBlacksmithAfterDeath(t *testing.T) {
	b := townBlacksmith(2233, true)
	b.diedAt = time.Now().Add(-3 * time.Minute)
	b.Goal = Goal{Map: "yuno_fild12", Mode: "hunt"} // rejoinAfterDeath needs a goal elsewhere
	r := runTownRest(b, 800, 0.9, time.Now())
	if r.hpReached < 0 {
		t.Fatalf("after a death the rest never reached 90%% in %d ticks (hp %d/%d)", r.ticks, b.HP, b.MaxHP)
	}
	if r.eatenAfter > 0 {
		t.Fatalf("%d snacks eaten after 90%%", r.eatenAfter)
	}
}

// Wizard lv76 in party mode, goal on another map (mjolnir_12), in aldebaran at 48-51% with SP full.
func TestTownRestWizardPartyGoalElsewhere(t *testing.T) {
	b := restBot(9, 1320, 2700, 1320, 1320) // Wizard, SP full (no SP rest needed)
	b.BaseLv = 76
	b.MapName = "aldebaran"
	b.Goal = Goal{Map: "mjolnir_12", Mode: "party"}
	b.diedAt = time.Time{}
	b.inv = map[int]*InvItem{}
	r := runTownRest(b, 800, lowHPDone, time.Now())
	if r.hpReached < 0 {
		t.Fatalf("wizard at 49%% in town never reached 60%% in %d ticks (hp %d/%d, state %q)",
			r.ticks, b.HP, b.MaxHP, b.State)
	}
	if r.hpReached > 300 {
		t.Fatalf("wizard took %d ticks to reach 60%%", r.hpReached)
	}
	if r.eatenAfter > 0 {
		t.Fatalf("%d snacks eaten after the done line", r.eatenAfter)
	}
}

// Overweight (60% weight, no natural regen) with potions in the bag: the bot drinks its way to the line; it does not sit
// and wait for regen that never comes.
func TestTownRestOverweightDrinksPotions(t *testing.T) {
	b := townBlacksmith(2233, false)
	b.MaxWeight, b.Weight = 1000, 600
	b.inv[1] = &InvItem{ItemID: itemRedPotion, Count: 20}
	b.buffAt["lowrest"] = time.Now() // already resting (a rest starts under 45% in town, so 58% alone does not start one)
	r := runTownRest(b, 200, lowHPDone, time.Now())
	if r.hpReached < 0 {
		t.Fatalf("overweight with 20 potions never reached 60%% (hp %d/%d, drunk %d)", b.HP, b.MaxHP, r.drunk)
	}
	if r.sat > 3 {
		t.Fatalf("overweight: sat %d ticks waiting for regen that does not come", r.sat)
	}
}

// Overweight with no potion and no food: no regen, so the bot must not sit for it. lowHPGuard hands the tick back
// (the trip logic sells first) and the rest does not start.
func TestTownRestOverweightNoPotionsDoesNotSit(t *testing.T) {
	b := townBlacksmith(2233, false)
	b.MaxWeight, b.Weight = 1000, 600
	b.buffAt["lowrest"] = time.Now() // already resting
	r := runTownRest(b, 200, lowHPDone, time.Now())
	if r.sat > 0 {
		t.Fatalf("overweight with nothing to drink sat %d ticks for no regen", r.sat)
	}
	if r.restEnd {
		t.Fatalf("overweight with nothing to drink still holds the bot in a rest: %q", b.State)
	}
}
