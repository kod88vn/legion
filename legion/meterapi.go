package main

// meterapi.go: the meter's background aggregator, history file, Ayla tracker, core-instance merge and HTTP API.
//
//	GET /api/meter[?window=10m|1h|session]                       server totals + cohorts (sorted by prize)
//	GET /api/meter/bots?cohort=&sort=&limit=&window=&order=     per-bot rows (sort: zph|kpm|gap_zph|gap_kpm|deaths|name|min)
//	GET /api/meter/bot/{name}                                    one bot: every window, raw counters, its cohort's par
//	GET /api/meter/neo[?window=]                                 Ayla, her party and Starveil against their cohorts' par
//	GET /api/meter/rows?local=1                                  raw per-bot rows of this instance (the main instance merges the core's)
//
// One goroutine (meterLoop) does all the work every meter_period_sec (15 s): it copies each bot's three window sums
// under that bot's lock (one bot at a time, microseconds each), fetches the core instance's rows over HTTP, computes
// everything with no lock held, publishes an immutable view with one atomic store, and appends the hourly history to
// <data>/meter.jsonl. Handlers only read the published view: they never lock a bot.

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- tuning (own keys in tuning.json)

// meterTuning: the meter's keys, read from the shared tuning.json by the meter goroutine (missing keys keep the
// defaults; tuning.go is not touched, so the other packages' Tuning edits merge cleanly).
type meterTuning struct {
	FairLvGap    int  `json:"meter_fair_lv_gap"`    // a kill is fair when monster level >= bot base level - this (15)
	MinOnlinePct int  `json:"meter_min_online_pct"` // a bot counts in a window's cohort with >= this % of the window online (25)
	PeriodSec    int  `json:"meter_period_sec"`     // aggregation period (15)
	HistoryOff   bool `json:"meter_history_off"`    // true: no meter.jsonl rows
}

var meterTuningDefault = meterTuning{FairLvGap: 15, MinOnlinePct: 25, PeriodSec: 15}

var meterTuneCur atomic.Pointer[meterTuning]

// meterTune: lock-free (bot hooks call it with b.mu held).
func meterTune() *meterTuning {
	if t := meterTuneCur.Load(); t != nil {
		return t
	}
	return &meterTuningDefault
}

func meterLoadTuning() {
	t := meterTuningDefault
	if raw, err := os.ReadFile(tuningFile()); err == nil {
		if err := json.Unmarshal(raw, &t); err != nil {
			t = *meterTune()
		}
	}
	if t.FairLvGap < 0 {
		t.FairLvGap = meterTuningDefault.FairLvGap
	}
	if t.MinOnlinePct < 0 || t.MinOnlinePct > 100 {
		t.MinOnlinePct = meterTuningDefault.MinOnlinePct
	}
	if t.PeriodSec < 5 {
		t.PeriodSec = meterTuningDefault.PeriodSec
	}
	meterTuneCur.Store(&t)
}

// meterMinMinutes: online minutes a bot needs to count, per window (session: of at most one hour).
func meterMinMinutes(sessAge float64) [mwN]float64 {
	p := float64(meterTune().MinOnlinePct) / 100
	return [mwN]float64{10 * p, 60 * p, math.Min(60, math.Max(sessAge, 1)) * p}
}

// ---------------------------------------------------------------- prices

var meterPricedAt time.Time

// meterPriceRound (meter goroutine, no lock held): prices the items the hooks met; every 5 min re-prices them all.
func meterPriceRound(now time.Time) {
	old := map[int]int{}
	if t := meterPriceTab.Load(); t != nil {
		old = *t
	}
	var fresh []int
	meterSeen.Range(func(k, _ interface{}) bool {
		id := k.(int)
		if _, ok := old[id]; !ok {
			fresh = append(fresh, id)
		}
		meterSeen.Delete(k)
		return true
	})
	full := now.Sub(meterPricedAt) >= 5*time.Minute
	if len(fresh) == 0 && !full {
		return
	}
	nt := make(map[int]int, len(old)+len(fresh))
	for id, v := range old {
		if full {
			v = meterPriceOf(id)
		}
		nt[id] = v
	}
	for _, id := range fresh {
		nt[id] = meterPriceOf(id)
	}
	if full {
		meterPricedAt = now
	}
	meterPriceTab.Store(&nt)
}

// meterPriceOf: the engine's market price (economy.go priceOf: standard price blended with real trades and the
// supply/demand factor); never below what an NPC pays.
func meterPriceOf(id int) int {
	return max(priceOf(id), sellPrice(id), 0)
}

// ---------------------------------------------------------------- snapshot

// meterSnapshot copies one bot's identity and window sums under its own lock. ok=false: never metered.
func meterSnapshot(b *Bot, now time.Time) (meterRow, bool) {
	nowMin := now.Unix() / 60
	var r meterRow
	var w10, w60, ws meterSlot
	ok := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock() // never leave a bot locked, even if something below panics
		c := &b.meter
		if c.sessFrom.IsZero() {
			return false
		}
		w10, w60, ws = c.sumWin(nowMin, 10), c.sumWin(nowMin, 60), c.sess
		r = meterRow{Name: b.charName(), Class: className(b.Class), Line: meterLineName(b.Class), Lv: b.BaseLvOr(),
			Role: b.charRole(), PartyID: b.PartyID, Party: b.PartyName, Guild: b.GuildName, Map: b.MapName,
			State: b.State, Bucket: meterBucketNames[c.bucket], Online: b.Online, SessMin: now.Sub(c.sessFrom).Minutes()}
		if c.char != "" {
			r.Name = c.char
		}
		return true
	}()
	if !ok {
		return meterRow{}, false
	}
	if !r.Online {
		r.Bucket, r.Map = "", ""
	}
	r.Win[mw10], r.Win[mw60], r.Win[mwSess] = meterWinOf(&w10), meterWinOf(&w60), meterWinOf(&ws)
	r.Instance = "main"
	if coreInstance() {
		r.Instance = "core"
	}
	r.SessMin = math.Round(r.SessMin)
	return r, true
}

// ---------------------------------------------------------------- core instance rows

var meterCore struct {
	rows []meterRow
	ok   time.Time
}

// meterFetchCore (meter goroutine): the core instance's raw rows; the last good answer is kept for 2 minutes.
func meterFetchCore(now time.Time) []meterRow {
	base := coreAPIURL()
	if base == "" || coreInstance() {
		return nil
	}
	var got []meterRow
	resp, err := coreClient.Get(base + "/api/meter/rows?local=1")
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			err = json.NewDecoder(resp.Body).Decode(&got)
		} else {
			err = errStatus(resp.StatusCode)
		}
		resp.Body.Close()
	}
	if err == nil {
		for i := range got {
			got[i].Instance = "core"
		}
		meterCore.rows, meterCore.ok = got, now
	} else if now.Sub(meterCore.ok) > 2*time.Minute {
		meterCore.rows = nil
	}
	return meterCore.rows
}

type errStatus int

func (e errStatus) Error() string { return "status " + strconv.Itoa(int(e)) }

// ---------------------------------------------------------------- Ayla (OpenKore, status.json)

// meterAyla: Ayla is played by OpenKore, not by the engine; status.json gives her purse, level, map, HP, whether she
// sits and (statusout.pl) a running kill count from her exp gains (kills, kills_since), every ~3 s. The meter samples
// it each round. Loot value is not in the file.
type aylaSample struct {
	t      time.Time
	zeny   int
	bucket string // hunting, town, resting, dead
	lv     int
	online bool
	kills  int64 // statusout.pl running count since ksince (0 = an older plugin without it)
	ksince int64
}

// aylaKillDelta: kills between two samples; none when the count restarted (a new OpenKore process) or went back.
func aylaKillDelta(p, c aylaSample) int64 {
	if c.ksince != p.ksince || c.ksince == 0 || c.kills < p.kills {
		return 0
	}
	return c.kills - p.kills
}

var meterAylaS struct {
	samples []aylaSample // last 60 min
	sess    aylaWin
	last    *humanStatus
}

type aylaWin struct {
	Min    float64            `json:"min"`
	ZDelta int64              `json:"zeny_delta"` // purse change over the online time
	Kills  int64              `json:"kills"`      // kills from her exp gains over the online time
	Sec    map[string]float64 `json:"sec"`
}

func meterAylaSample(now time.Time) {
	raw, err := os.ReadFile(humanStatusPath())
	if err != nil {
		return
	}
	var s humanStatus
	if json.Unmarshal(raw, &s) != nil || s.Name == "" {
		return
	}
	online := s.Online && now.Sub(time.Unix(s.TS, 0)) <= 30*time.Second
	bk := "hunting"
	switch {
	case s.HP <= 0 && s.MaxHP > 0:
		bk = "dead"
	case isTown(s.Map):
		bk = "town"
	case s.Sitting:
		bk = "resting"
	}
	a := &meterAylaS
	sm := aylaSample{t: now, zeny: s.Zeny, bucket: bk, lv: s.BaseLevel, online: online, kills: s.Kills, ksince: s.KillsSince}
	if n := len(a.samples); n > 0 {
		p := a.samples[n-1]
		if dt := now.Sub(p.t); online && p.online && dt <= 60*time.Second {
			if a.sess.Sec == nil {
				a.sess.Sec = map[string]float64{}
			}
			a.sess.Min += dt.Minutes()
			a.sess.ZDelta += int64(s.Zeny - p.zeny)
			a.sess.Kills += aylaKillDelta(p, sm)
			a.sess.Sec[p.bucket] += dt.Seconds()
		}
	}
	a.samples = append(a.samples, sm)
	cut := 0
	for cut < len(a.samples) && now.Sub(a.samples[cut].t) > time.Hour {
		cut++
	}
	a.samples = a.samples[cut:]
	a.last = &s
}

// aylaWindow: the purse change and time over the last d (from the samples).
func aylaWindow(now time.Time, d time.Duration) aylaWin {
	return aylaWindowOf(meterAylaS.samples, now, d)
}

func aylaWindowOf(ss []aylaSample, now time.Time, d time.Duration) aylaWin {
	w := aylaWin{Sec: map[string]float64{}}
	for i := 1; i < len(ss); i++ {
		p, c := ss[i-1], ss[i]
		if now.Sub(c.t) > d {
			continue
		}
		if dt := c.t.Sub(p.t); c.online && p.online && dt <= 60*time.Second {
			w.Min += dt.Minutes()
			w.ZDelta += int64(c.zeny - p.zeny)
			w.Kills += aylaKillDelta(p, c)
			w.Sec[p.bucket] += dt.Seconds()
		}
	}
	return w
}

// ---------------------------------------------------------------- the published view

type meterView struct {
	T       time.Time
	Rows    []*meterRow
	byName  map[string]*meterRow
	Cohorts [mwN][]meterCohort
	Totals  [mwN]meterTotals
	par     [mwN]map[string]*meterCohort
	Ayla    *aylaView
	Core    int // rows from the core instance
}

type aylaView struct {
	Name    string                `json:"name"`
	Online  bool                  `json:"online"`
	Lv      int                   `json:"lv"`
	Class   string                `json:"class"`
	Map     string                `json:"map"`
	Zeny    int                   `json:"zeny"`
	Guild   string                `json:"guild"`
	Cohort  string                `json:"cohort"`
	ExpH    int64                 `json:"exp_h"`      // statusout.pl: base exp per hour over its last 10 min
	JobExpH int64                 `json:"job_exp_h"`  // statusout.pl: job exp per hour over its last 10 min
	KPM10   float64               `json:"kpm_plugin"` // statusout.pl's own 10-min kills/min (exp gains)
	Kills   int64                 `json:"kills"`      // statusout.pl running count since its OpenKore start
	Windows map[string]aylaWinOut `json:"windows"`
	Note    string                `json:"note"`
}

type aylaWinOut struct {
	Min      float64            `json:"min"`
	Counted  bool               `json:"counted"` // enough online time in the window (the bots' rule)
	Kills    int64              `json:"kills"`   // kills from her exp gains (her own and the party's she shares exp in)
	KPM      float64            `json:"kpm"`
	ZPHPurse float64            `json:"zph_purse"` // purse change per hour (not loot value: see note)
	ParZPH   float64            `json:"par_zph"`
	ParKPM   float64            `json:"par_kpm"`
	Shares   map[string]float64 `json:"shares"`
}

var meterCur atomic.Pointer[meterView]

func meterBuild(rows []*meterRow, now time.Time) *meterView {
	v := &meterView{T: now, Rows: rows, byName: map[string]*meterRow{}}
	for _, r := range rows {
		v.byName[strings.ToLower(r.Name)] = r
		if r.Instance == "core" {
			v.Core++
		}
	}
	minMin := meterMinMinutes(60)
	meterDerive(rows, minMin)
	for _, r := range rows { // session window: a young session needs less (25% of its age, up to 15 min)
		mm := meterMinMinutes(r.SessMin)
		x := &r.Win[mwSess]
		r.stat[mwSess].Counted = x.Min > 0 && x.Min >= mm[mwSess]
	}
	for w := 0; w < mwN; w++ {
		v.Cohorts[w], v.Totals[w] = meterCohorts(rows, w)
		v.par[w] = map[string]*meterCohort{}
		for i := range v.Cohorts[w] {
			v.par[w][v.Cohorts[w][i].Key] = &v.Cohorts[w][i]
		}
	}
	if s := meterAylaS.last; s != nil {
		a := &aylaView{Name: s.Name, Online: s.Online && now.Sub(time.Unix(s.TS, 0)) <= 30*time.Second, Lv: s.BaseLevel,
			Class: className(s.JobID), Map: s.Map, Zeny: s.Zeny, Guild: s.Guild, Windows: map[string]aylaWinOut{},
			ExpH: s.ExpH, JobExpH: s.JobExpH, KPM10: s.KPM, Kills: s.Kills,
			Note: "OpenKore character: kills counted from her exp gains (statusout.pl: her kills and the party kills she shares exp in), purse change per hour; loot value is not observable from status.json"}
		if s.KillsSince == 0 {
			a.Note = "OpenKore character: purse change and status only; statusout.pl without the kill count (reload it) - kills not observable"
		}
		a.Cohort = meterCohortKey(meterLineName(s.JobID), s.BaseLevel, "", 1)
		ws := [mwN]aylaWin{aylaWindow(now, 10*time.Minute), aylaWindow(now, time.Hour), meterAylaS.sess}
		for w := 0; w < mwN; w++ {
			o := aylaWinOut{Min: math.Round(ws[w].Min*10) / 10, Kills: ws[w].Kills, Shares: map[string]float64{}}
			mm := meterMinMinutes(ws[mwSess].Min)
			o.Counted = ws[w].Min > 0 && ws[w].Min >= mm[w] && s.KillsSince != 0
			if ws[w].Min > 0 {
				o.ZPHPurse = math.Round(float64(ws[w].ZDelta) / ws[w].Min * 60)
				o.KPM = round3(float64(ws[w].Kills) / ws[w].Min)
				for k, sec := range ws[w].Sec {
					o.Shares[k] = round3(sec / 60 / ws[w].Min)
				}
			}
			if c := v.par[w][a.Cohort]; c != nil {
				o.ParZPH, o.ParKPM = c.ZPH.Par, c.KPM.Par
			}
			a.Windows[meterWinNames[w]] = o
		}
		v.Ayla = a
	}
	return v
}

// ---------------------------------------------------------------- loop and history

func meterLoop(bots []*Bot) {
	meterLoadTuning()
	lastTune := time.Now()
	lastHour := time.Now().Unix() / 3600
	lastSave := time.Now()
	lastBots := time.Now()
	for {
		now := time.Now()
		if now.Sub(lastTune) >= time.Minute {
			meterLoadTuning()
			lastTune = now
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("meter: round panicked: %v", r) // the meter must never take the engine down
				}
			}()
			meterPriceRound(now)
			meterAylaSample(now)
			rows := make([]*meterRow, 0, len(bots))
			for _, b := range bots {
				if r, ok := meterSnapshot(b, now); ok {
					rows = append(rows, &r)
				}
			}
			have := map[string]bool{}
			for _, r := range rows {
				have[strings.ToLower(r.Name)] = true
			}
			for _, cr := range meterFetchCore(now) {
				if !have[strings.ToLower(cr.Name)] {
					c := cr
					rows = append(rows, &c)
				}
			}
			v := meterBuild(rows, now)
			meterCur.Store(v)
			if now.Sub(lastSave) >= meterPersistEvery {
				lastSave = now
				meterPersist(bots, now) // meterpersist.go: counters survive a restart
			}
			if now.Sub(lastBots) >= meterBotsEvery && !coreInstance() {
				lastBots = now
				meterWriteBots(v, now) // the per-bot numbers on disk (below)
			}
			if h := now.Unix() / 3600; h != lastHour {
				lastHour = h
				if !coreInstance() && !meterTune().HistoryOff {
					meterWriteHistory(v, now)
				}
			}
		}()
		time.Sleep(time.Duration(meterTune().PeriodSec) * time.Second)
	}
}

// meterBotsEvery: how often the per-bot file is rewritten (about 7,800 rows, a few MB).
const meterBotsEvery = 5 * time.Minute

// meterBotsFile: <data>/meter_bots.json, the per-bot meter (kills/min, zeny/h, deaths/h, time shares) of every bot that
// has been online in the last hour, plus Ayla's own row. The Players tab and the leaderboard read it while the engine
// is down (meter.jsonl keeps only cohort, server and neo rows). Written by the main instance only; the rows are the
// published view, so they include the core instance's bots.
type meterBotsFile struct {
	T      int64         `json:"t"`
	Window string        `json:"window"`
	Total  int           `json:"total"`
	Bots   []meterBotOut `json:"bots"`
	Ayla   *meterBotOut  `json:"ayla,omitempty"`
}

func meterWriteBots(v *meterView, now time.Time) {
	w := mw60
	f := meterBotsFile{T: now.Unix(), Window: meterWinNames[w], Bots: []meterBotOut{}}
	for _, r := range v.Rows {
		if r.Win[w].Min > 0 {
			f.Bots = append(f.Bots, v.botOut(r, w, true))
		}
	}
	f.Total = len(f.Bots)
	f.Ayla = aylaBotOut(v.Ayla, meterWinNames[w])
	raw, err := json.Marshal(f)
	if err != nil {
		return
	}
	if err := writeFileAtomic(filepath.Join(*dataDir, "meter_bots.json"), raw); err != nil {
		log.Printf("meter: bots file: %v", err)
	}
}

// meterHistRow: one line of meter.jsonl.
type meterHistRow struct {
	T      string             `json:"t"`    // the hour just closed (RFC3339, start of that hour)
	Kind   string             `json:"kind"` // cohort | server | neo
	Window string             `json:"window"`
	Key    string             `json:"key,omitempty"`
	Line   string             `json:"line,omitempty"`
	Band   string             `json:"band,omitempty"`
	Mode   string             `json:"mode,omitempty"`
	N      int                `json:"n"`
	KPM    *meterDist         `json:"kpm,omitempty"`
	ZPH    *meterDist         `json:"zph,omitempty"`
	DPH    float64            `json:"deaths_h"`
	Shares map[string]float64 `json:"shares,omitempty"`
	Totals *meterTotals       `json:"totals,omitempty"`
	Neo    *neoSummary        `json:"neo,omitempty"`
}

// meterWriteHistory appends one row per cohort (the last hour), one server row and one Neo row. Meter goroutine only,
// no lock held.
func meterWriteHistory(v *meterView, now time.Time) {
	hour := time.Unix((now.Unix()/3600-1)*3600, 0).UTC().Format(time.RFC3339)
	var lines []byte
	add := func(x meterHistRow) {
		if b, err := json.Marshal(x); err == nil {
			lines = append(append(lines, b...), '\n')
		}
	}
	t := v.Totals[mw60]
	add(meterHistRow{T: hour, Kind: "server", Window: "1h", N: t.Bots, DPH: t.DPH, Shares: t.Shares, Totals: &t})
	for i := range v.Cohorts[mw60] {
		c := v.Cohorts[mw60][i]
		if c.N == 0 {
			continue
		}
		add(meterHistRow{T: hour, Kind: "cohort", Window: "1h", Key: c.Key, Line: c.Line, Band: c.Band, Mode: c.Mode,
			N: c.N, KPM: &c.KPM, ZPH: &c.ZPH, DPH: c.DPH, Shares: c.Shares})
	}
	n := meterNeo(v, mw60)
	add(meterHistRow{T: hour, Kind: "neo", Window: "1h", N: n.Party.N + n.Starveil.N, Neo: &neoSummary{Party: n.Party, Starveil: n.Starveil}})
	f, err := os.OpenFile(filepath.Join(*dataDir, "meter.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("meter: history: %v", err)
		return
	}
	if _, err := f.Write(lines); err != nil {
		log.Printf("meter: history: %v", err)
	}
	f.Close()
}

// ---------------------------------------------------------------- views for the API

type meterBotOut struct {
	Name     string             `json:"name"`
	Class    string             `json:"class"`
	Lv       int                `json:"lv"`
	Cohort   string             `json:"cohort"`
	Instance string             `json:"instance"`
	Online   bool               `json:"online"`
	Map      string             `json:"map,omitempty"`
	Bucket   string             `json:"bucket,omitempty"`
	Party    string             `json:"party,omitempty"`
	Guild    string             `json:"guild,omitempty"`
	Min      float64            `json:"min"`
	Counted  bool               `json:"counted"`
	KPM      float64            `json:"kpm"`
	KPMOwn   float64            `json:"kpm_own"`
	KPMAll   float64            `json:"kpm_all"` // own kills of any level per minute (kpm counts fair kills only)
	ZPH      float64            `json:"zph"`
	ZPHOwn   float64            `json:"zph_own"`
	DPH      float64            `json:"deaths_h"`
	ParKPM   float64            `json:"par_kpm"`
	ParZPH   float64            `json:"par_zph"`
	GapKPM   float64            `json:"gap_kpm"` // par - its value (negative: above par)
	GapZPH   float64            `json:"gap_zph"`
	Shares   map[string]float64 `json:"shares,omitempty"`
}

func (v *meterView) botOut(r *meterRow, w int, shares bool) meterBotOut {
	st := r.stat[w]
	o := meterBotOut{Name: r.Name, Class: r.Class, Lv: r.Lv, Cohort: r.cohort, Instance: r.Instance, Online: r.Online,
		Map: r.Map, Bucket: r.Bucket, Party: r.Party, Guild: r.Guild, Min: math.Round(r.Win[w].Min*10) / 10,
		Counted: st.Counted, KPM: st.KPM, KPMOwn: st.KPMOwn, KPMAll: st.KPMAll, ZPH: st.ZPH, ZPHOwn: st.ZPHOwn, DPH: st.DPH}
	if c := v.par[w][r.cohort]; c != nil {
		o.ParKPM, o.ParZPH = c.KPM.Par, c.ZPH.Par
		o.GapKPM, o.GapZPH = round3(c.KPM.Par-st.KPM), math.Round(c.ZPH.Par-st.ZPH)
	}
	if shares {
		o.Shares = st.Shares
	}
	return o
}

type neoGroup struct {
	N        int     `json:"n"` // members counted in the window
	AboveKPM int     `json:"above_par_kpm"`
	AboveZPH int     `json:"above_par_zph"`
	RatioKPM float64 `json:"ratio_kpm"` // mean of value / par (1.0 = at par)
	RatioZPH float64 `json:"ratio_zph"`
}

type neoSummary struct {
	Party    neoGroup `json:"party"`
	Starveil neoGroup `json:"starveil"`
}

type neoOut struct {
	T        time.Time     `json:"t"`
	Window   string        `json:"window"`
	Ayla     *aylaView     `json:"ayla"`
	Party    neoGroup      `json:"party_summary"`
	Members  []meterBotOut `json:"party"`
	Starveil neoGroup      `json:"starveil_summary"`
	Guild    []meterBotOut `json:"starveil"`
}

func neoSum(list []meterBotOut) neoGroup {
	var g neoGroup
	rk, rz := 0.0, 0.0
	nk, nz := 0, 0
	for _, m := range list {
		if !m.Counted {
			continue
		}
		g.N++
		if m.KPM >= m.ParKPM {
			g.AboveKPM++
		}
		if m.ZPH >= m.ParZPH {
			g.AboveZPH++
		}
		if m.ParKPM > 0 {
			rk += m.KPM / m.ParKPM
			nk++
		}
		if m.ParZPH > 0 {
			rz += m.ZPH / m.ParZPH
			nz++
		}
	}
	if nk > 0 {
		g.RatioKPM = round2(rk / float64(nk))
	}
	if nz > 0 {
		g.RatioZPH = round2(rz / float64(nz))
	}
	return g
}

// meterNeo: Ayla's party = the core instance's bots (instances.go: legion-core runs exactly her friends) and anyone
// in a party with one of them; Starveil = every bot whose guild is Ayla's (status.json guild, default Starveil).
func meterNeo(v *meterView, w int) neoOut {
	out := neoOut{T: v.T, Window: meterWinNames[w], Ayla: v.Ayla, Members: []meterBotOut{}, Guild: []meterBotOut{}}
	parties := map[string]bool{} // by meterPartyKey: b.PartyID is a placeholder 1 for every party (meterparty.go)
	for _, r := range v.Rows {
		if k := r.partyKey(); r.Instance == "core" && k != "" {
			parties[k] = true
		}
	}
	guild := "Starveil"
	if v.Ayla != nil && v.Ayla.Guild != "" {
		guild = v.Ayla.Guild
	}
	for _, r := range v.Rows {
		if k := r.partyKey(); r.Instance == "core" || k != "" && parties[k] {
			out.Members = append(out.Members, v.botOut(r, w, true))
		}
		if strings.EqualFold(r.Guild, guild) {
			out.Guild = append(out.Guild, v.botOut(r, w, false))
		}
	}
	if r := aylaBotOut(v.Ayla, meterWinNames[w]); r != nil { // Ayla herself: her row counts in the party summary
		out.Members = append(out.Members, *r)
	}
	sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].Name < out.Members[j].Name })
	sort.Slice(out.Guild, func(i, j int) bool { return out.Guild[i].ZPH > out.Guild[j].ZPH })
	out.Party, out.Starveil = neoSum(out.Members), neoSum(out.Guild)
	return out
}

// aylaBotOut: Ayla's window as a party row (instance "openkore"): kpm from her exp-gain kill count, z/h = purse change
// per hour (OpenKore has no loot value), par from her cohort. nil without status.json.
func aylaBotOut(a *aylaView, win string) *meterBotOut {
	if a == nil {
		return nil
	}
	aw, ok := a.Windows[win]
	if !ok {
		return nil
	}
	o := &meterBotOut{Name: a.Name, Class: a.Class, Lv: a.Lv, Cohort: a.Cohort, Instance: "openkore", Online: a.Online,
		Map: a.Map, Guild: a.Guild, Min: aw.Min, Counted: aw.Counted, KPM: aw.KPM, KPMAll: aw.KPM, ZPH: aw.ZPHPurse,
		ZPHOwn: aw.ZPHPurse, ParKPM: aw.ParKPM, ParZPH: aw.ParZPH, Shares: aw.Shares}
	o.GapKPM, o.GapZPH = round3(aw.ParKPM-aw.KPM), math.Round(aw.ParZPH-aw.ZPHPurse)
	return o
}

// ---------------------------------------------------------------- HTTP

// registerMeter: called once from serveAPI; starts the aggregator.
func registerMeter(mux *http.ServeMux, bots []*Bot) {
	go meterLoop(bots)
	meterHandlers(mux)
}

// meterHandlers: the endpoints over the published view (no bot lock anywhere).
func meterHandlers(mux *http.ServeMux) {
	view := func(w http.ResponseWriter) *meterView {
		v := meterCur.Load()
		if v == nil {
			http.Error(w, "meter: first round not done yet (wait 15 s)", http.StatusServiceUnavailable)
		}
		return v
	}
	mux.HandleFunc("/api/meter", func(w http.ResponseWriter, r *http.Request) {
		v := view(w)
		if v == nil {
			return
		}
		wi := meterWinIndex(r.URL.Query().Get("window"))
		cs := v.Cohorts[wi]
		if r.URL.Query().Get("sort") == "kpm" {
			cs = append([]meterCohort(nil), cs...)
			sort.SliceStable(cs, func(i, j int) bool { return cs[i].KPM.Prize > cs[j].KPM.Prize })
		}
		writeJSON(w, map[string]interface{}{"t": v.T, "window": meterWinNames[wi], "rows": len(v.Rows),
			"core_rows": v.Core, "fair_lv_gap": meterTune().FairLvGap, "buckets": meterBucketNames,
			"server": v.Totals[wi], "cohorts": cs})
	})
	mux.HandleFunc("/api/meter/rows", func(w http.ResponseWriter, r *http.Request) {
		v := view(w)
		if v == nil {
			return
		}
		out := []meterRow{}
		for _, x := range v.Rows {
			if r.URL.Query().Get("local") == "" || x.Instance == "main" && !coreInstance() || x.Instance == "core" && coreInstance() {
				out = append(out, *x)
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/meter/bots", func(w http.ResponseWriter, r *http.Request) {
		v := view(w)
		if v == nil {
			return
		}
		q := r.URL.Query()
		wi := meterWinIndex(q.Get("window"))
		coh := strings.ToLower(q.Get("cohort"))
		var list []meterBotOut
		for _, x := range v.Rows {
			if coh != "" && !strings.HasPrefix(x.cohort, coh) {
				continue
			}
			if x.Win[wi].Min <= 0 && q.Get("all") == "" {
				continue
			}
			list = append(list, v.botOut(x, wi, q.Get("shares") != ""))
		}
		meterSortBots(list, q.Get("sort"), q.Get("order") == "asc")
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		total := len(list)
		if len(list) > limit {
			list = list[:limit]
		}
		if list == nil {
			list = []meterBotOut{}
		}
		writeJSON(w, map[string]interface{}{"t": v.T, "window": meterWinNames[wi], "total": total, "bots": list})
	})
	mux.HandleFunc("/api/meter/bot/", func(w http.ResponseWriter, r *http.Request) {
		v := view(w)
		if v == nil {
			return
		}
		name, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/meter/bot/"))
		x := v.byName[strings.ToLower(strings.TrimSpace(name))]
		if x == nil {
			http.Error(w, "meter: no such bot (or never online since the engine started): "+name, http.StatusNotFound)
			return
		}
		wins := map[string]interface{}{}
		for wi := 0; wi < mwN; wi++ {
			wins[meterWinNames[wi]] = map[string]interface{}{"stats": v.botOut(x, wi, true), "raw": x.Win[wi]}
		}
		writeJSON(w, map[string]interface{}{"t": v.T, "name": x.Name, "class": x.Class, "lv": x.Lv, "cohort": x.cohort,
			"instance": x.Instance, "online": x.Online, "map": x.Map, "state": x.State, "bucket": x.Bucket,
			"party": x.Party, "guild": x.Guild, "session_age_min": x.SessMin, "windows": wins})
	})
	mux.HandleFunc("/api/meter/neo", func(w http.ResponseWriter, r *http.Request) {
		v := view(w)
		if v == nil {
			return
		}
		writeJSON(w, meterNeo(v, meterWinIndex(r.URL.Query().Get("window"))))
	})
}

func meterSortBots(list []meterBotOut, by string, asc bool) {
	key := func(m meterBotOut) float64 {
		switch by {
		case "kpm":
			return m.KPM
		case "gap_kpm":
			return m.GapKPM
		case "gap_zph", "gap":
			return m.GapZPH
		case "deaths":
			return m.DPH
		case "min":
			return m.Min
		}
		return m.ZPH
	}
	sort.SliceStable(list, func(i, j int) bool {
		if by == "name" { // always A-Z
			return list[i].Name < list[j].Name
		}
		if asc {
			return key(list[i]) < key(list[j])
		}
		return key(list[i]) > key(list[j])
	})
}
