package main

// healerstick.go - the party's healer stays with the leader (PT04 rule number one, PT06, PT24, PT31; owner 2026-11-14:
// "why is the priest always somewhere else?").

// partyNameOf: the engine party the bot belongs to ("" = none), for the API (BotSummary.Party).
func partyNameOf(b *Bot) string {
	if p := partyOf(b); p != nil {
		return p.Name
	}
	return ""
}
