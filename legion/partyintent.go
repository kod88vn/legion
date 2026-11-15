package main

// partyintent.go: what Ayla is trying to do, and her party's chat, on the dashboard Parties card (owner 2026-11-14:
// "since OpenKore crashed I have no way to know what Ayla is trying to do ... also log the party chat to that panel").
// Her intent and recent decisions come from statusout.pl (status.json extras, partylead.pl status_intent); the party
// chat comes from the engine itself (her friends receive every party line), so it keeps working while OpenKore is down.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

// ---------------------------------------------------------------- per-member metrics (partydash.go)

// PartyMemberExtras: a bot member's distance to the human leader and its 10-minute meter.
type PartyMemberExtras struct {
	DistLead int     `json:"dist_lead"`          // cells to the human leader on the same map; -1 = another map / unknown
	LeadMap  string  `json:"lead_map,omitempty"` // the leader's map (to read dist_lead -1)
	KPM      float64 `json:"kpm"`                // own kills per minute, last 10 min (meter, any level)
	ExpH     int64   `json:"exp_h"`              // base exp per hour over the last <= 10 min (expSample)
	ExpMin   float64 `json:"exp_min"`            // minutes the exp/h covers
}

type expPoint struct {
	t    time.Time
	gain int64 // cumulative base exp gained since the first sample
}

var expMeter struct {
	sync.Mutex
	by map[string]*expTrack
}

type expTrack struct {
	lv   int
	exp  int64
	gain int64
	pts  []expPoint
}

// expSample records a member's base exp (b.mu may be held: leaf lock only) and returns its exp/h over the last
// <= 10 min and the minutes covered (0 below one minute of samples). A level-up counts the new level's exp only.
func expSample(name string, lv int, exp int64, now time.Time) (int64, float64) {
	k := strings.ToLower(name)
	expMeter.Lock()
	defer expMeter.Unlock()
	if expMeter.by == nil {
		expMeter.by = map[string]*expTrack{}
	}
	t := expMeter.by[k]
	if t == nil {
		t = &expTrack{lv: lv, exp: exp}
		expMeter.by[k] = t
	}
	switch {
	case lv == t.lv && exp >= t.exp:
		t.gain += exp - t.exp
	case lv > t.lv:
		t.gain += exp
	}
	t.lv, t.exp = lv, exp
	t.pts = append(t.pts, expPoint{now, t.gain})
	i := 0
	for i < len(t.pts)-1 && now.Sub(t.pts[i].t) > 10*time.Minute {
		i++
	}
	t.pts = t.pts[i:]
	if len(t.pts) > 400 { // the dashboard polls every 10 s: keep it bounded anyway
		t.pts = append([]expPoint(nil), t.pts[len(t.pts)-400:]...)
	}
	min := now.Sub(t.pts[0].t).Minutes()
	if min < 1 {
		return 0, 0
	}
	return int64(float64(t.gain-t.pts[0].gain) / min * 60), float64(int(min*10)) / 10
}

// meterKPM: a bot's own kills per minute over 10 min from the published meter view (no bot lock).
func meterKPM(name string) float64 {
	v := meterCur.Load()
	if v == nil {
		return 0
	}
	if r := v.byName[strings.ToLower(name)]; r != nil {
		return r.stat[mw10].KPMAll
	}
	return 0
}

// ---------------------------------------------------------------- the Haiku helper's last run (puppet dispatcher)

// helperRun: the last ayla row of <data>/puppet_calls.jsonl (tools/puppet/dispatch.py append_log).
type helperRun struct {
	T       float64 `json:"t"`
	Trigger string  `json:"trigger"`
	Model   string  `json:"model"`
	Exit    int     `json:"exit"`
	IsError bool    `json:"is_error"`
	Result  string  `json:"result"`
	Seconds float64 `json:"duration_s"`
}

var helperCache struct {
	sync.Mutex
	at  time.Time
	run *helperRun
}

// helperLastRun: read the file's last 256 KB at most every 15 s.
func helperLastRun(role string) *helperRun {
	helperCache.Lock()
	defer helperCache.Unlock()
	if time.Since(helperCache.at) < 15*time.Second {
		return helperCache.run
	}
	helperCache.at = time.Now()
	f, err := os.Open(filepath.Join(*dataDir, "puppet_calls.jsonl"))
	if err != nil {
		return helperCache.run
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return helperCache.run
	}
	off := st.Size() - 256*1024
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return helperCache.run
	}
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], `"role": "`+role+`"`) && !strings.Contains(lines[i], `"role":"`+role+`"`) {
			continue
		}
		var r helperRun
		if json.Unmarshal([]byte(lines[i]), &r) == nil {
			helperCache.run = &r
			break
		}
	}
	return helperCache.run
}
