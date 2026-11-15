package main

// guildduty_window.go (audit 2026-11-14, rule GD02: guild direction for about 2 hours a day, not all the time).
//
//   - Duty window: a guild block overrides the solo map picker (ai.go chooseHuntingGround -> guildBlockHunt) only while
//     the block itself runs: not ended, minutes not done, deadline not passed, and today's duty minutes not yet owed in
//     full. Outside it the bot gets its own goal back at once (the gear goal saved when the block started, lfg.go) instead
//     of waiting for the duty tick to notice, so its own gear-goal map is never overridden outside the duty block.
//   - Kick cool-down: the refusals in dutyGuildAnswer apply only to the character that was kicked (same account, not in
//     a guild now). A record left from an earlier character of the same name (re-seed) or a character already in a guild
//     never blocks recruiting (Starveil's or any other guild's).

import (
	"strings"
	"time"
)

// dutyWindowOK: the guild block k (set at blockSet) still owns the bot's map choice.
func dutyWindowOK(k *dutyBlock, blockSet time.Time, dayMin, perDay int, now time.Time) bool {
	if k == nil || !k.Set.Equal(blockSet) || !k.Ended.IsZero() || k.Done >= k.Target || now.After(k.Until) {
		return false
	}
	return perDay <= 0 || dayMin < perDay
}

// dutyWindowOpen (b.mu held; gduty is taken briefly - the same order as guildBlockStatus): is the guild goal g inside
// its duty window? When it is not, the bot's earlier goal comes back now.
func (b *Bot) dutyWindowOpen(g *farmGoal, now time.Time) bool {
	gduty.Lock()
	m := gduty.byChar[b.charName()]
	var k *dutyBlock
	dayMin := 0
	if m != nil {
		k = m.Block
		if d := m.today(dutyDate(now)); d != nil {
			dayMin = d.DutyMin
		}
	}
	open := dutyWindowOK(k, g.Set, dayMin, tune().GuildDutyMinPerDay, now)
	var prev *farmGoal
	if !open && k != nil && k.Set.Equal(g.Set) {
		prev = k.prev
	}
	gduty.Unlock()
	if !open && b.fgoal == g {
		b.fgoal = nil
		if prev != nil && now.Before(prev.Until) {
			b.fgoal = prev
		}
	}
	return open
}

// dutyKickApplies: the kick record k is this character's own and still in force (not joined a guild since, the same
// account, not in a guild now).
func dutyKickApplies(k *dutyKicked, account string, guildID uint32) bool {
	if k == nil || k.Stage == "joined" || guildID != 0 {
		return false
	}
	return k.Account == "" || strings.EqualFold(k.Account, account)
}
