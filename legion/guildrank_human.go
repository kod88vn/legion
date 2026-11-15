package main

// guildrank_human.go (2026-11-14, bug queue: Starveil's wealth read 0). Guild funds are the guild master's and the
// guild vendors' purses (guildrank.go), but the rank round sees only the engine's own bots on the main instance.
// Starveil's master plays through OpenKore, so the guild had no funds at all while every other guild counted its
// master's purse. Same rule for every guild (G25): her purse comes from her status file (humanstats.go), which
// carries her zeny and guild; an offline reading counts like an offline leader's (kept for a day by the round).

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// humanGuildPurse: the human guild master's name, purse and reading time, when the status file is about the
// master of guild `starveil` (no lock held; a file read).
func humanGuildPurse(starveil string) (name string, zeny int, ts int64, ok bool) {
	if starveil == "" {
		return "", 0, 0, false
	}
	raw, err := os.ReadFile(humanStatusPath())
	if err != nil {
		return "", 0, 0, false
	}
	var s humanStatus
	if json.Unmarshal(raw, &s) != nil {
		return "", 0, 0, false
	}
	return humanPurseOf(s, starveil, time.Now())
}

// humanPurseOf (pure): the status counts when it names a character of that guild and is under a day old.
func humanPurseOf(s humanStatus, starveil string, now time.Time) (string, int, int64, bool) {
	if s.Name == "" || s.TS <= 0 || !strings.EqualFold(strings.TrimSpace(s.Guild), strings.TrimSpace(starveil)) {
		return "", 0, 0, false
	}
	if now.Unix()-s.TS > 24*3600 || s.Zeny < 0 {
		return "", 0, 0, false
	}
	return s.Name, s.Zeny, s.TS, true
}
