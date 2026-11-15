package main

import "time"

// ---------------------------------------------------------------- potion budget (Constitution 3.2: zeny per hour)
// A restock's potion spend beyond the survival minimum is capped at a share of what the bot really earns: the meter's
// zeny in over the last hour, scaled to zeny per hour (tuning pot_budget_pct, default 15). A bot with no measured hour
// yet gets the floor (pot_budget_floor, default 1,000z). Survival potions (potMin) are not capped.

// potBudgetFor: the potion budget (zeny) for a bot earning zph zeny per hour
func potBudgetFor(zph float64) int {
	pct, floor := tune().PotBudgetPct, tune().PotBudgetFloor
	if pct <= 0 {
		pct = 15
	}
	if floor <= 0 {
		floor = 1000
	}
	return max(floor, int(zph*float64(pct)/100))
}

// potBudget (b.mu held): this bot's potion budget now
func (b *Bot) potBudget() int { return potBudgetFor(b.recentZenyPerHour(time.Now())) }

// recentZenyPerHour (b.mu held): zeny in over the last hour (or since the session started, from 10 minutes on), per
// hour. 0 before 10 minutes of the session (meter.go keeps 60 one-minute slots per bot).
func (b *Bot) recentZenyPerHour(now time.Time) float64 {
	c := &b.meter
	if c.sessFrom.IsZero() {
		return 0
	}
	mins := int(now.Sub(c.sessFrom).Minutes())
	if mins < 10 {
		return 0
	}
	if mins > meterRingMin {
		mins = meterRingMin
	}
	s := c.sumWin(now.Unix()/60, mins)
	return float64(s.ZIn) * 60 / float64(mins)
}
