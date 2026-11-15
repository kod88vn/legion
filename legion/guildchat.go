package main

// guildchat.go: the guild-direction package's social lines (duty warning, kick, direction announced) go through
// the exchange package's chat jobs (chatjobs.go), so the chat agent can voice them. With chat_jobs_on off every
// helper returns false and the caller says its template line exactly as before (guildSay); with it on and no chat
// agent polling, EnqueueChatJob says the template at once.
//
// Lock note: EnqueueChatJob never blocks and is safe with any lock held; the line itself is said later by a
// chat-job goroutine that takes the speaker's lock on its own.

// dutyChatWarn (l.mu held): the warning as a scripted two-line job (leader, then the member's reply).
// false = chat jobs off: the caller says both lines itself.
func dutyChatWarn(l *Bot, member, line, reply string, shortDays int) bool {
	if !chatJobsOn() || l == nil {
		return false
	}
	EnqueueChatJob(ChatJobSpec{Kind: ChatKindDutyWarning, Channel: "guild", Speaker: l, Hearers: []string{member},
		Facts: map[string]any{"member": member, "short_days": shortDays, "guild": l.GuildName,
			"why": "the member did not work the guild's directions for days"},
		Template: []ChatLine{{Speaker: l.charName(), Channel: "guild", Text: line},
			{Speaker: member, Channel: "guild", Text: reply, DelaySec: 4 + crand(8)}}})
	return true
}

// dutyChatKick (l.mu held): the leader's line before an expel. It returns how long the caller should wait before
// sending the expel so the line is said first (0 = chat jobs off: say the line yourself, as before).
func dutyChatKick(l *Bot, member, line, why string) (wait int, used bool) {
	if !chatJobsOn() || l == nil {
		return 0, false
	}
	id := EnqueueChatJob(ChatJobSpec{Kind: ChatKindKick, Channel: "guild", Speaker: l, Hearers: []string{member},
		Facts:    map[string]any{"member": member, "guild": l.GuildName, "why": why},
		Template: []ChatLine{{Speaker: l.charName(), Channel: "guild", Text: line}}})
	if id != "" {
		wait = tune().ChatJobWaitMaxSec + 2 // the job resolves by its deadline at the latest
	}
	return wait, true
}

// dutyChatDirection (l.mu held): a new guild direction announced by the leader.
func dutyChatDirection(l *Bot, line string, d *GuildDirection) bool {
	if !chatJobsOn() || l == nil || d == nil {
		return false
	}
	ChatJobLine(ChatKindDirection, l, "guild", line, map[string]any{"guild": l.GuildName, "kind": d.Kind,
		"item": d.Item, "map": d.Map, "min_lv": d.MinLv, "max_lv": d.MaxLv, "note": d.Note, "set_by": d.SetBy})
	return true
}
