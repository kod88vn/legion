package main

import (
	"fmt"
	"time"
)

// QA 2026-11-15 (core, aldebaran): LilLilyblade (Monk) "low HP (100%): resting before moving on", Echobunny "low HP
// (89%): resting before moving on". Both within 10 min of a death, on the way back to Ayla (rejoinAfterDeath):
//   - the post-death rule also wanted 90% SP for every "SP user", and a Monk counts as one (acolyte family). At full HP
//     with SP short, lowHPGuard took the whole low-HP path: labelled "low HP (100%)", could self-Heal at full HP (which
//     spends the very SP it waits for) and asked the party for heals. Now the SP wait is its own short branch, labelled
//     "resting for SP", and only real casters (mage / acolyte line except Monk and Champion) wait for SP.
//   - the post-death entry line was the done line (90%): a bot at 89% sat down for 1%. Now a rest after a death starts
//     under postDeathEntry and, once started, still runs to 90% (hysteresis like the other lines).

const postDeathEntry = 0.8 // after a death: start resting under this (done at 0.9)

// postDeathSPOK (b.mu held): SP is no reason to wait after a death, or it is back over 90%.
func (b *Bot) postDeathSPOK() bool {
	if b.MaxSP <= 0 || !b.spUser() || isMonkClass(b.Class) {
		return true
	}
	return b.SP*10 >= b.MaxSP*9
}

// spRestBeforeMoving (b.mu held, lowHPGuard: HP is fine, SP is short after a death): sit for SP, nothing else. A fight
// is the normal flow's (false).
func (b *Bot) spRestBeforeMoving(moving bool, now time.Time) bool {
	if b.underAttack(now) || b.poisoned(now) {
		return false
	}
	if _, resting := b.buffAt["lowrest"]; !resting {
		b.buffAt["lowrest"] = now // the stand-up path in lowHPGuard ends it once SP is back
	}
	b.target, b.wantSit = 0, true
	if !b.Sitting && !moving && now.Sub(b.sitSentAt) > 2*time.Second && b.basicLv() >= 3 {
		b.sitSentAt = now
		go b.sit()
	}
	sp := 100
	if b.MaxSP > 0 {
		sp = b.SP * 100 / b.MaxSP
	}
	b.setState(fmt.Sprintf("resting for SP (%d%%) before moving on", sp))
	return true
}
