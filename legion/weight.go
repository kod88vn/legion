package main

// Staying under the regen line (rAthena: no natural HP/SP regen at >= 50% weight). QA 2026-11-14 06:20: 68 of 1,010
// online bots sat at 50-69% weight with no town run (the bag trip waits for trip_bag_pct 70, and heavyJunk stops
// cheap pickups at 50%, so they hovered just under 70 forever). Their bags held, per heavy bot on average: ~36 Fly
// Wings (5.0 each), ~60 Green Herbs, 140-170 Novice Potions, 50-200 Apples/Carrots, plus Jellopy/Fluff/Clover.
// food.go ate fruit only out of combat below 90% HP and only without overhealing, so almost nothing was eaten.
// Now:
//   - snackIndex: food is eaten whenever the bot is hurt (in a fight too: it is HP the bot already carries), and
//     over the regen line any HP loss is a reason to eat (overheal allowed: every Apple is 2.0 weight gone), plus
//     surplus Novice Potions from lv20 (they can't be sold: Buy 0).
//   - regenWeightNeed: over the line with anything to sell (junk, surplus potions/herbs/wings/fruit) is a real need
//     for a town run ("bag is heavy"), for party members too; the trip interval and party batching in ai.go apply.
//     Cards, gear and valuable materials never count as sellable (npcJunk / surplusPots).

const itemNovicePotion = 569

// snackIndex (b.mu held): what to eat right now, -1 for nothing. Hurt: the food that doesn't overheal (foodIndex).
// Over the regen line: any food, heaviest first, and surplus Novice Potions from lv20.
func (b *Bot) snackIndex() int {
	if b.MaxHP <= 0 || b.HP >= b.MaxHP {
		return -1
	}
	if idx := b.foodIndex(); idx >= 0 {
		return idx
	}
	if !b.overRegenWeight() {
		return -1
	}
	best, bw := -1, -1
	for _, it := range b.inv {
		if it.Count <= 0 || it.Equipped {
			continue
		}
		if foodHeal[it.ItemID] == 0 && !(it.ItemID == itemNovicePotion && b.BaseLvOr() >= 20 && it.Count > keepNovicePots) {
			continue
		}
		w := 1
		if x := items[it.ItemID]; x != nil && x.Weight > 0 {
			w = x.Weight
		}
		if w > bw {
			best, bw = it.Index, w
		}
	}
	return best
}

// regenWeightNeed (b.mu held): "bag is heavy" as soon as the bot is over the regen line (owner 2026-11-14: "a 50%
// weight should immediately trigger a town run", every bot and party) - with anything to sell at all (a bag heavy only
// from gear, which is never NPC-sold, gains nothing from a Tool Dealer). It is a realNeed in ai.go: a party member may
// call the party run; one run serves everyone, and the trip interval still keeps it to one per trip_min_interval_min.
func (b *Bot) regenWeightNeed() string {
	if b.MaxWeight <= 0 || !b.overRegenWeight() || !b.hasLoot() {
		return ""
	}
	return "bag is heavy"
}
