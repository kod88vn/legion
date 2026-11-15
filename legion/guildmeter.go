package main

// guildmeter.go: wires the guild-direction package (guildduty*.go) into the meter (meter.go). Time a bot spends on a
// guild block - its farm goal is a guild block, duty is on, it is alive and on one of the block's maps - shows as
// "on guild duty" in the meter's time budget (duty_min / duty_pct), the same rule the duty ledger counts minutes by.
//
// Lock note (meter.md): meterDutyHook runs inside meterTick with b.mu held. This hook reads only the bot's own fields
// (b.fgoal, b.Dead, b.MapName) and the lock-free tuning; it never takes gduty or any other lock.

func init() { meterDutyHook = guildDutyMetered }

// guildDutyMetered (b.mu held): true while the bot works a guild block on one of the block's maps.
func guildDutyMetered(b *Bot) bool {
	g := b.fgoal
	return g != nil && g.Kind == "guild" && !b.Dead && dutyOn() && g.onMap(b.MapName)
}
