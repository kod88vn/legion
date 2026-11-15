package main

// partyintent.go: what Ayla is trying to do, and her party's chat, on the dashboard Parties card (owner 2026-11-14:
// "since OpenKore crashed I have no way to know what Ayla is trying to do ... also log the party chat to that panel").
// Her intent and recent decisions come from statusout.pl (status.json extras, partylead.pl status_intent); the party
// chat comes from the engine itself (her friends receive every party line), so it keeps working while OpenKore is down.

import (
	"strings"
	"sync"
	"time"
)

// humanEvent: one console line from her OpenKore (statusout.pl logline).
type humanEvent struct {
	Text   string `json:"text"`
	TS     int64  `json:"ts"`
	Result string `json:"result,omitempty"` // orders: the first reply line ("blocked", a [partylead] line...)
}

// humanExtras: the dashboard extras statusout.pl adds to status.json (zero when an older statusout runs).
type humanExtras struct {
	Intent        string      `json:"intent"`
	IntentSince   int64       `json:"intent_since"`
	LastDecision  *humanEvent `json:"last_decision"`
	LastWatchdog  *humanEvent `json:"last_watchdog"`
	LastOrder     *humanEvent `json:"last_order"`
	BaseExpPct    float64     `json:"base_exp_pct"`
	JobExpPct     float64     `json:"job_exp_pct"`
	ExpH          int64       `json:"exp_h"`
	JobExpH       int64       `json:"job_exp_h"`
	KPM           float64     `json:"kpm"`
	MeterMin      float64     `json:"meter_min"`
	DeathsToday   int         `json:"deaths_today"`
	OKStart       int64       `json:"ok_start"`
	RestartsToday int         `json:"restarts_today"`
	PrevStart     int64       `json:"prev_start"`
	ExtrasError   string      `json:"extras_error,omitempty"`
}

// humanDetail: Ayla's row extras in /api/parties/detail.
type humanDetail struct {
	humanExtras
	Map      string `json:"map"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	LockMap  string `json:"lock_map"`
	Dest     string `json:"dest"`
	TownRun  bool   `json:"town_run"`
	StatusTS int64  `json:"status_ts"` // status.json write time (stale = OpenKore down)
	Fresh    bool   `json:"fresh"`     // status.json younger than 30 s
}

// humanDetailOf: her detail from the last status.json read, fresh or not (a stale one still says what she did last).
func humanDetailOf(name string) *humanDetail {
	humanStatusOf(name) // refresh the cached read
	humanStats.Lock()
	s, ok := humanStats.got, humanStats.ok
	humanStats.Unlock()
	if !ok || !strings.EqualFold(s.Name, name) {
		return nil
	}
	return &humanDetail{humanExtras: s.humanExtras, Map: s.Map, X: s.X, Y: s.Y, LockMap: s.LockMap, Dest: s.Dest,
		TownRun: s.TownRun, StatusTS: s.TS, Fresh: time.Since(time.Unix(s.TS, 0)) <= 30*time.Second}
}

// ---------------------------------------------------------------- party chat

type partyChatLine struct {
	TS   int64  `json:"ts"`
	From string `json:"from"`
	Text string `json:"text"`
}

const partyChatKeep = 30

var partyChat struct {
	sync.Mutex
	byHuman map[string][]partyChatLine // lower(human) -> oldest first
}

// partyChatHeard (packet handler, b.mu held): a party line one of the human's friends received. Leaf lock only.
// Every friend receives the same line: a line equal to one of the last few within 5 s is the same line.
func partyChatHeard(human, raw string, now time.Time) {
	if human == "" || raw == "" {
		return
	}
	from, text := "", raw
	if i := strings.Index(raw, " : "); i >= 0 {
		from, text = strings.TrimSpace(raw[:i]), raw[i+3:]
	}
	k := strings.ToLower(human)
	partyChat.Lock()
	defer partyChat.Unlock()
	if partyChat.byHuman == nil {
		partyChat.byHuman = map[string][]partyChatLine{}
	}
	l := partyChat.byHuman[k]
	for i := len(l) - 1; i >= 0 && i >= len(l)-8; i-- {
		if l[i].From == from && l[i].Text == text && now.Unix()-l[i].TS <= 5 {
			return
		}
	}
	l = append(l, partyChatLine{TS: now.Unix(), From: from, Text: text})
	if len(l) > partyChatKeep {
		l = append([]partyChatLine(nil), l[len(l)-partyChatKeep:]...)
	}
	partyChat.byHuman[k] = l
}

// partyChatOf: the human's party chat, newest first.
func partyChatOf(human string) []partyChatLine {
	partyChat.Lock()
	defer partyChat.Unlock()
	l := partyChat.byHuman[strings.ToLower(human)]
	out := make([]partyChatLine, len(l))
	for i := range l {
		out[len(l)-1-i] = l[i]
	}
	return out
}
