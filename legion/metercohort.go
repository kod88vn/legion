package main

// metercohort.go: from per-bot window counters to rates, party credit, cohorts, par and gap. Pure functions, no
// locks: the aggregator (meterapi.go) calls them on copies. Definitions: docs/design/meter.md.

import (
	"math"
	"sort"
	"strings"
)

// window indexes
const (
	mw10 = iota
	mw60
	mwSess
	mwN
)

var meterWinNames = [mwN]string{"10m", "1h", "session"}

// meterWinIndex: "10m" / "1h" / "session" (also "sess", "s", "60m"); default 1h.
func meterWinIndex(s string) int {
	switch strings.ToLower(s) {
	case "10m", "10", "10min":
		return mw10
	case "session", "sess", "s", "all":
		return mwSess
	}
	return mw60
}

// meterWin: one bot's raw counters over one window (sent as is between instances).
type meterWin struct {
	Min    float64   `json:"min"` // measured online minutes (sum of the time buckets)
	Kills  int       `json:"kills"`
	Fair   int       `json:"fair"`
	Deaths int       `json:"deaths"`
	Loot   int64     `json:"loot"`
	Cons   int64     `json:"cons"`
	Travel int64     `json:"travel"`
	ZIn    int64     `json:"zeny_in"`
	ZOut   int64     `json:"zeny_out"`
	Sec    []float64 `json:"sec"` // seconds per bucket, in meterBucketNames order
	Duty   float64   `json:"duty_min"`
}

func meterWinOf(s *meterSlot) meterWin {
	w := meterWin{Kills: int(s.Kills), Fair: int(s.Fair), Deaths: int(s.Deaths), Loot: s.Loot, Cons: s.Cons,
		Travel: s.Travel, ZIn: s.ZIn, ZOut: s.ZOut, Sec: make([]float64, mbN), Duty: float64(s.Duty) / 60}
	tot := 0.0
	for i, v := range s.Sec {
		w.Sec[i] = float64(v)
		tot += float64(v)
	}
	w.Min = tot / 60
	return w
}

// income: loot value minus consumables and travel (zeny).
func (w *meterWin) income() int64 { return w.Loot - w.Cons - w.Travel }

// meterRow: one bot (one character) with its three windows.
type meterRow struct {
	Name     string         `json:"name"`
	Class    string         `json:"class"`
	Line     string         `json:"line"`
	Lv       int            `json:"lv"`
	Role     string         `json:"role,omitempty"`
	PartyID  uint32         `json:"party_id,omitempty"`
	Party    string         `json:"party,omitempty"`
	Guild    string         `json:"guild,omitempty"`
	Map      string         `json:"map,omitempty"`
	State    string         `json:"state,omitempty"`
	Bucket   string         `json:"bucket"`
	Online   bool           `json:"online"`
	Instance string         `json:"instance"`
	SessMin  float64        `json:"session_age_min"` // minutes since its session counters started
	Win      [mwN]meterWin  `json:"win"`
	stat     [mwN]meterStat // derived (meterDerive)
	cohort   string         // cohort key
}

// meterStat: the derived numbers of one bot in one window.
type meterStat struct {
	Counted bool               `json:"counted"` // enough online minutes in the window to count in its cohort
	KPM     float64            `json:"kpm"`     // fair kills per minute, party-credited (the cohort metric)
	KPMOwn  float64            `json:"kpm_own"` // its own fair kills per minute
	KPMAll  float64            `json:"kpm_all"` // its own kills of any level per minute
	ZPH     float64            `json:"zph"`     // income per hour, party-credited (the cohort metric)
	ZPHOwn  float64            `json:"zph_own"`
	DPH     float64            `json:"deaths_h"`
	Shares  map[string]float64 `json:"shares"`
}

// meterLineName: the cohort's class line (first-job family: a Knight and a Lord Knight are both "swordman").
func meterLineName(class int) string {
	switch family(class) {
	case famSword:
		return "swordman"
	case famMage:
		return "mage"
	case famArcher:
		return "archer"
	case famAcolyte:
		return "acolyte"
	case famMerchant:
		return "merchant"
	case famThief:
		return "thief"
	}
	return "novice"
}

// meterBand: level band 1-20, 21-40, 41-60, 61-80, 81-99.
func meterBand(lv int) string {
	switch {
	case lv <= 20:
		return "1-20"
	case lv <= 40:
		return "21-40"
	case lv <= 60:
		return "41-60"
	case lv <= 80:
		return "61-80"
	}
	return "81-99"
}

// meterMode: vendor (vending is their job; kept out of the fighters' cohorts), party or solo.
func meterMode(role string, partyID uint32) string {
	switch {
	case role == "vendor":
		return "vendor"
	case partyID != 0:
		return "party"
	}
	return "solo"
}

// meterCohortKey: "line|band|mode", e.g. "swordman|41-60|party".
func meterCohortKey(line string, lv int, role string, partyID uint32) string {
	return line + "|" + meterBand(lv) + "|" + meterMode(role, partyID)
}

func meterShares(sec []float64) map[string]float64 {
	tot := 0.0
	for _, v := range sec {
		tot += v
	}
	out := map[string]float64{}
	if tot <= 0 {
		return out
	}
	for i, v := range sec {
		if i < mbN && v > 0 {
			out[meterBucketNames[i]] = round3(v / tot)
		}
	}
	return out
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// meterDerive fills every row's stat and cohort key. minMin[w] = online minutes a bot needs in window w to count.
// Party credit: a party member's KPM / ZPH is its party's total (fair kills, income) over the members' total online
// minutes in the window - the party's kills and loot are shared, whoever landed the blow or picked the item up.
func meterDerive(rows []*meterRow, minMin [mwN]float64) {
	for w := 0; w < mwN; w++ {
		type agg struct {
			fair, min float64
			inc       int64
		}
		parties := map[string]*agg{} // by meterPartyKey (meterparty.go): b.PartyID is a placeholder 1 for every party
		for _, r := range rows {
			k := r.partyKey()
			if k == "" || meterMode(r.Role, r.PartyID) != "party" {
				continue
			}
			a := parties[k]
			if a == nil {
				a = &agg{}
				parties[k] = a
			}
			x := &r.Win[w]
			a.fair += float64(x.Fair)
			a.min += x.Min
			a.inc += x.income()
		}
		for _, r := range rows {
			x := &r.Win[w]
			st := meterStat{Shares: meterShares(x.Sec)}
			if x.Min > 0 {
				st.KPMOwn = float64(x.Fair) / x.Min
				st.KPMAll = float64(x.Kills) / x.Min
				st.ZPHOwn = float64(x.income()) / x.Min * 60
				st.DPH = float64(x.Deaths) / x.Min * 60
				st.KPM, st.ZPH = st.KPMOwn, st.ZPHOwn
				if a := parties[r.partyKey()]; a != nil && meterMode(r.Role, r.PartyID) == "party" && a.min > 0 {
					st.KPM = a.fair / a.min
					st.ZPH = float64(a.inc) / a.min * 60
				}
			}
			st.Counted = x.Min >= minMin[w] && x.Min > 0
			st.KPM, st.KPMOwn, st.KPMAll = round3(st.KPM), round3(st.KPMOwn), round3(st.KPMAll)
			st.ZPH, st.ZPHOwn, st.DPH = math.Round(st.ZPH), math.Round(st.ZPHOwn), round2(st.DPH)
			r.stat[w] = st
		}
	}
	for _, r := range rows {
		r.cohort = meterCohortKey(r.Line, r.Lv, r.Role, r.PartyID)
	}
}

// meterPct: the p-quantile (0..1) of sorted values, linear interpolation between closest ranks (R type 7).
func meterPct(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	h := p * float64(n-1)
	lo := int(math.Floor(h))
	if lo >= n-1 {
		return sorted[n-1]
	}
	return sorted[lo] + (h-float64(lo))*(sorted[lo+1]-sorted[lo])
}

// meterDist: one metric over a cohort. Par = p75. Gap = par - mean (>= 0); Prize = Gap x N (what the cohort would
// gain if its average reached par).
type meterDist struct {
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P75    float64 `json:"p75"`
	Par    float64 `json:"par"`
	Gap    float64 `json:"gap"`
	Prize  float64 `json:"prize"`
}

func meterDistOf(vals []float64, round func(float64) float64) meterDist {
	if len(vals) == 0 {
		return meterDist{}
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	d := meterDist{Mean: sum / float64(len(s)), Median: meterPct(s, 0.5), P75: meterPct(s, 0.75)}
	d.Par = d.P75
	d.Gap = math.Max(0, d.Par-d.Mean)
	d.Prize = d.Gap * float64(len(s))
	d.Mean, d.Median, d.P75, d.Par, d.Gap, d.Prize = round(d.Mean), round(d.Median), round(d.P75), round(d.Par), round(d.Gap), round(d.Prize)
	return d
}

// meterCohort: one cohort in one window.
type meterCohort struct {
	Key     string             `json:"key"`
	Line    string             `json:"line"`
	Band    string             `json:"band"`
	Mode    string             `json:"mode"`
	N       int                `json:"n"`    // bots counted
	Seen    int                `json:"seen"` // bots with any time in the window (counted or not)
	KPM     meterDist          `json:"kpm"`  // fair kills per minute (party-credited)
	ZPH     meterDist          `json:"zph"`  // income per hour (party-credited)
	DPH     float64            `json:"deaths_h"`
	Shares  map[string]float64 `json:"shares"` // time shares over the counted bots
	DutyPct float64            `json:"duty_pct,omitempty"`
}

// meterTotals: the whole population of one window (counted bots only), rates per bot.
type meterTotals struct {
	Bots    int                `json:"bots"`    // counted
	Seen    int                `json:"seen"`    // with any time in the window
	Online  int                `json:"online"`  // online now
	BotMin  float64            `json:"bot_min"` // online minutes summed
	Kills   int                `json:"kills"`   // own kills, any level
	Fair    int                `json:"fair"`    // fair kills
	Deaths  int                `json:"deaths"`
	Loot    int64              `json:"loot"`
	Cons    int64              `json:"cons"`
	Travel  int64              `json:"travel"`
	Income  int64              `json:"income"`
	KPM     float64            `json:"kpm"`      // fair kills per bot-minute
	ZPH     float64            `json:"zph"`      // income per bot-hour
	DPH     float64            `json:"deaths_h"` // deaths per bot-hour
	KPMMed  float64            `json:"kpm_median"`
	ZPHMed  float64            `json:"zph_median"`
	Shares  map[string]float64 `json:"shares"`
	DutyPct float64            `json:"duty_pct,omitempty"`
}

// meterCohorts: cohorts and totals of window w over rows already derived. Cohorts are sorted by zeny prize, then
// kill prize, largest first.
func meterCohorts(rows []*meterRow, w int) ([]meterCohort, meterTotals) {
	type acc struct {
		c         meterCohort
		kpm, zph  []float64
		sec       []float64
		min, duty float64
		deaths    int
	}
	by := map[string]*acc{}
	var tot meterTotals
	tsec := make([]float64, mbN)
	var tk, tz []float64
	tduty := 0.0
	for _, r := range rows {
		x := &r.Win[w]
		if r.Online {
			tot.Online++
		}
		if x.Min <= 0 {
			continue
		}
		a := by[r.cohort]
		if a == nil {
			parts := strings.SplitN(r.cohort, "|", 3)
			for len(parts) < 3 {
				parts = append(parts, "")
			}
			a = &acc{c: meterCohort{Key: r.cohort, Line: parts[0], Band: parts[1], Mode: parts[2]}, sec: make([]float64, mbN)}
			by[r.cohort] = a
		}
		a.c.Seen++
		tot.Seen++
		st := &r.stat[w]
		if !st.Counted {
			continue
		}
		a.c.N++
		a.kpm = append(a.kpm, st.KPM)
		a.zph = append(a.zph, st.ZPH)
		a.min += x.Min
		a.deaths += x.Deaths
		a.duty += x.Duty
		for i, v := range x.Sec {
			if i < mbN {
				a.sec[i] += v
				tsec[i] += v
			}
		}
		tot.Bots++
		tot.BotMin += x.Min
		tot.Kills += x.Kills
		tot.Fair += x.Fair
		tot.Deaths += x.Deaths
		tot.Loot += x.Loot
		tot.Cons += x.Cons
		tot.Travel += x.Travel
		tduty += x.Duty
		tk = append(tk, st.KPMOwn)
		tz = append(tz, st.ZPHOwn)
	}
	var out []meterCohort
	for _, a := range by {
		c := a.c
		c.KPM = meterDistOf(a.kpm, round3)
		c.ZPH = meterDistOf(a.zph, math.Round)
		if a.min > 0 {
			c.DPH = round2(float64(a.deaths) / a.min * 60)
			c.DutyPct = round3(a.duty / a.min)
		}
		c.Shares = meterShares(a.sec)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ZPH.Prize != out[j].ZPH.Prize {
			return out[i].ZPH.Prize > out[j].ZPH.Prize
		}
		if out[i].KPM.Prize != out[j].KPM.Prize {
			return out[i].KPM.Prize > out[j].KPM.Prize
		}
		return out[i].Key < out[j].Key
	})
	tot.Income = tot.Loot - tot.Cons - tot.Travel
	if tot.BotMin > 0 {
		tot.KPM = round3(float64(tot.Fair) / tot.BotMin)
		tot.ZPH = math.Round(float64(tot.Income) / tot.BotMin * 60)
		tot.DPH = round2(float64(tot.Deaths) / tot.BotMin * 60)
		tot.DutyPct = round3(tduty / tot.BotMin)
		tot.BotMin = math.Round(tot.BotMin)
	}
	sort.Float64s(tk)
	sort.Float64s(tz)
	tot.KPMMed, tot.ZPHMed = round3(meterPct(tk, 0.5)), math.Round(meterPct(tz, 0.5))
	tot.Shares = meterShares(tsec)
	return out, tot
}
