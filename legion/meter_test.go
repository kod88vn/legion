package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeterBucketOf(t *testing.T) {
	hunt := meterCtx{onHunt: true}
	town := meterCtx{town: true}
	field := meterCtx{} // a field that is not the goal map
	cases := []struct {
		state string
		ctx   meterCtx
		want  int
	}{
		{"fighting Poring", hunt, mbFight},
		{"engaging Lunatic", hunt, mbFight},
		{"looting", hunt, mbFight},
		{"healing Bob (40%)", hunt, mbFight},
		{"standing up to heal Bob", hunt, mbFight},
		{"standing up", hunt, mbRest},
		{"resting", hunt, mbRest},
		{"sitting by Ayla", hunt, mbRest},
		{"roaming", hunt, mbSearch},
		{"teleporting to find monsters", hunt, mbSearch},
		{"with Ayla", hunt, mbSearch},
		{"with Ayla", town, mbTown},
		{"following Ayla", field, mbTravel},
		{"travel -> pay_fild09", field, mbTravel},
		{"going to the Kafra (Payon -> 120 z)", town, mbTravel},
		{"town trip: buy", town, mbTown},
		{"vending: S> pots", town, mbTown},
		{"waiting for Kim to trade", town, mbTown},
		{"going to prontera to buy from Kim", town, mbTown},
		{"going to Kim's shop", town, mbTown},
		{"rally: waiting at the Kafra in payon", town, mbWait},
		{"regrouping with Ayla", field, mbWait},
		{"regrouping with Ayla: waiting (can't get there)", field, mbStuck},
		{"collecting Bob", hunt, mbWait},
		{"going to payon to cross pay_fild04 with the party", field, mbWait},
		{"no route to gef_fild10", field, mbStuck},
		{"walled in: getting out", field, mbStuck},
		{"afk", town, mbIdle},
		{"hanging out in prontera", town, mbIdle},
		{"idle in prontera", town, mbIdle},
		{"dead", hunt, mbDead},
		{"poisoned (40%)", hunt, mbRest},
		{"poisoned with no cure: back to town", field, mbTravel},
		{"curing myself (poisoned)", hunt, mbRest},
		{"curing Bob (poisoned)", hunt, mbFight},
		{"Double Strafe", meterCtx{onHunt: true, hasTarget: true}, mbFight}, // unknown state, target in hand
		{"something new", hunt, mbSearch},
		{"something new", town, mbTown},
		{"something new", field, mbTravel},
		{"fighting Poring", meterCtx{dead: true}, mbDead},
		{"travel -> x", meterCtx{stuck: true}, mbStuck},
	}
	for _, c := range cases {
		if got := meterBucketOf(c.state, c.ctx); got != c.want {
			t.Errorf("%q %+v: got %s, want %s", c.state, c.ctx, meterBucketNames[got], meterBucketNames[c.want])
		}
	}
}

func TestMeterFair(t *testing.T) {
	if !meterIsFair(45, 60, 15) || meterIsFair(44, 60, 15) || !meterIsFair(80, 60, 15) || !meterIsFair(1, 10, 15) {
		t.Fatal("fair kill threshold wrong")
	}
}

func TestMeterWindowRollover(t *testing.T) {
	var c meterCounters
	t0 := time.Unix(1_800_000_000, 0) // a minute boundary
	for m := 0; m < 70; m++ {         // one kill per minute for 70 minutes
		now := t0.Add(time.Duration(m) * time.Minute)
		c.record(now, func(s *meterSlot) { s.Kills++; s.Sec[mbFight] += 60 })
	}
	nowMin := t0.Add(69*time.Minute).Unix() / 60
	if w := c.sumWin(nowMin, 10); w.Kills != 10 || w.Sec[mbFight] != 600 {
		t.Fatalf("10m window: %d kills, %v s", w.Kills, w.Sec[mbFight])
	}
	if w := c.sumWin(nowMin, 60); w.Kills != 60 {
		t.Fatalf("1h window: %d kills", w.Kills)
	}
	if c.sess.Kills != 70 {
		t.Fatalf("session: %d kills", c.sess.Kills)
	}
	// 30 idle minutes later the 10-minute window is empty and the hour holds the last 30 active minutes
	later := nowMin + 30
	if w := c.sumWin(later, 10); w.Kills != 0 {
		t.Fatalf("stale 10m window: %d", w.Kills)
	}
	if w := c.sumWin(later, 60); w.Kills != 30 {
		t.Fatalf("1h window after a gap: %d", w.Kills)
	}
	// a slot reused 60 minutes later starts from zero
	c.record(t0.Add(130*time.Minute), func(s *meterSlot) { s.Kills++ })
	if w := c.sumWin(t0.Add(130*time.Minute).Unix()/60, 1); w.Kills != 1 {
		t.Fatalf("reused slot: %d", w.Kills)
	}
}

func TestMeterPct(t *testing.T) {
	s := []float64{1, 2, 3, 4}
	if v := meterPct(s, 0.5); v != 2.5 {
		t.Fatalf("median %v", v)
	}
	if v := meterPct(s, 0.75); math.Abs(v-3.25) > 1e-9 {
		t.Fatalf("p75 %v", v)
	}
	if meterPct(nil, 0.75) != 0 || meterPct([]float64{7}, 0.75) != 7 {
		t.Fatal("edge cases")
	}
	d := meterDistOf([]float64{4, 1, 3, 2}, func(f float64) float64 { return f })
	if d.Par != 3.25 || d.Mean != 2.5 || d.Median != 2.5 || d.Gap != 0.75 || d.Prize != 3 {
		t.Fatalf("dist %+v", d)
	}
}

func TestMeterCohortKey(t *testing.T) {
	cases := []struct {
		class, lv int
		role      string
		party     uint32
		want      string
	}{
		{7, 55, "main", 0, "swordman|41-60|solo"},     // Knight
		{4008, 99, "main", 3, "swordman|81-99|party"}, // Lord Knight
		{4, 20, "support", 2, "acolyte|1-20|party"},
		{0, 5, "", 0, "novice|1-20|solo"},
		{10, 61, "vendor", 9, "merchant|61-80|vendor"},
		{12, 81, "main", 1, "thief|81-99|party"},
		{11, 40, "main", 0, "archer|21-40|solo"},
	}
	for _, c := range cases {
		if got := meterCohortKey(meterLineName(c.class), c.lv, c.role, c.party); got != c.want {
			t.Errorf("class %d lv %d: %s, want %s", c.class, c.lv, got, c.want)
		}
	}
}

func row(name string, party uint32, min float64, fair int, loot, cons, travel int64) *meterRow {
	r := &meterRow{Name: name, Line: "swordman", Lv: 50, Role: "main", PartyID: party}
	for w := 0; w < mwN; w++ {
		r.Win[w] = meterWin{Min: min, Fair: fair, Kills: fair + 1, Loot: loot, Cons: cons, Travel: travel,
			Sec: []float64{min * 60 * 0.8, min * 60 * 0.2, 0, 0, 0, 0, 0, 0, 0}}
	}
	return r
}

func TestMeterIncomeAndPartyCredit(t *testing.T) {
	w := meterWin{Loot: 10000, Cons: 1500, Travel: 600}
	if w.income() != 7900 {
		t.Fatalf("income %d", w.income())
	}
	solo := row("solo", 0, 30, 60, 6000, 500, 500) // 2 kpm, (5000 / 30 min) x 60 = 10,000 z/h
	a := row("a", 7, 30, 90, 9000, 0, 0)           // the party: 90 + 0 kills, 9000 + 0 z over 60 member-minutes
	b := row("b", 7, 30, 0, 0, 0, 0)               // the healer: no own kills
	rows := []*meterRow{solo, a, b}
	meterDerive(rows, [mwN]float64{5, 5, 5})
	st := solo.stat[mw60]
	if st.KPM != 2 || st.ZPH != 10000 || !st.Counted {
		t.Fatalf("solo %+v", st)
	}
	for _, r := range []*meterRow{a, b} {
		if s := r.stat[mw60]; s.KPM != 1.5 || s.ZPH != 9000 {
			t.Fatalf("%s credited %+v", r.Name, s)
		}
	}
	if b.stat[mw60].KPMOwn != 0 || a.stat[mw60].KPMOwn != 3 {
		t.Fatal("own kills")
	}
	if solo.cohort != "swordman|41-60|solo" || a.cohort != "swordman|41-60|party" {
		t.Fatalf("cohorts %s %s", solo.cohort, a.cohort)
	}
	if sh := st.Shares; sh["fighting"] != 0.8 || sh["searching"] != 0.2 {
		t.Fatalf("shares %v", sh)
	}
	short := row("short", 0, 2, 10, 0, 0, 0)
	meterDerive([]*meterRow{short}, [mwN]float64{5, 5, 5})
	if short.stat[mw60].Counted {
		t.Fatal("2 minutes online must not count with a 5-minute threshold")
	}
}

func TestMeterCohorts(t *testing.T) {
	var rows []*meterRow
	for i, f := range []int{10, 20, 30, 40} { // kpm 1, 2, 3, 4 over 10 minutes
		rows = append(rows, row(string(rune('a'+i)), 0, 10, f, int64(f)*100, 0, 0))
	}
	meterDerive(rows, [mwN]float64{1, 1, 1})
	cs, tot := meterCohorts(rows, mw10)
	if len(cs) != 1 {
		t.Fatalf("cohorts %d", len(cs))
	}
	c := cs[0]
	if c.N != 4 || c.KPM.Par != 3.25 || c.KPM.Mean != 2.5 || c.KPM.Prize != 3 {
		t.Fatalf("cohort %+v", c)
	}
	if tot.Bots != 4 || tot.Fair != 100 || tot.KPM != 2.5 {
		t.Fatalf("totals %+v", tot)
	}
}

func TestMeterTickBuckets(t *testing.T) {
	b := NewBot(&Persona{AccountID: 1, Chars: []CharSpec{{Name: "Tester"}}})
	b.MapName, b.Goal = "pay_fild01", Goal{Mode: "hunt", Map: "pay_fild01"}
	t0 := time.Unix(1_800_000_000, 0)
	b.meterLogin(t0)
	b.State = "fighting Poring"
	b.meterTick(t0)
	b.meterTick(t0.Add(2 * time.Second)) // 2 s fighting
	b.State = "roaming"
	b.meterTick(t0.Add(3 * time.Second))  // 1 s searching: think set "roaming" at 2 s
	b.meterTick(t0.Add(13 * time.Second)) // gap > meterMaxDt: not counted
	b.meterTick(t0.Add(14 * time.Second)) // 1 s searching
	s := b.meter.sess
	if s.Sec[mbFight] != 2 || s.Sec[mbSearch] != 2 {
		t.Fatalf("buckets %v", s.Sec)
	}
	// purse: baseline at login, then changes
	b.Zeny = 1000
	b.meterLogin(t0)
	b.meterZeny(1500)
	b.meterZeny(1200)
	if b.meter.sess.ZIn != 500 || b.meter.sess.ZOut != 300 {
		t.Fatalf("zeny in/out %d %d", b.meter.sess.ZIn, b.meter.sess.ZOut)
	}
	b.meterTravel(120)
	if b.meter.sess.Travel != 120 {
		t.Fatal("travel")
	}
	// another character on the same account starts fresh counters
	b.cur = &CharSpec{Name: "Other"}
	b.meterLogin(t0)
	if b.meter.sess.Travel != 0 || b.meter.char != "Other" {
		t.Fatal("character switch must reset")
	}
}

func TestMeterAPI(t *testing.T) {
	a := row("Alpha", 7, 30, 90, 9000, 0, 0)
	a.Instance, a.Guild, a.Online, a.Class = "core", "Starveil", true, "Knight"
	b := row("Beta", 7, 30, 0, 0, 0, 0)
	b.Instance, b.Class = "main", "Priest"
	c := row("Gamma", 0, 30, 60, 6000, 500, 500)
	c.Instance, c.Guild, c.Class = "main", "Starveil", "Knight"
	v := meterBuild([]*meterRow{a, b, c}, time.Unix(1_800_000_000, 0))
	meterCur.Store(v)
	defer meterCur.Store(nil)
	mux := http.NewServeMux()
	meterHandlers(mux)
	get := func(path string, into interface{}) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		if dir := os.Getenv("METER_DUMP"); dir != "" {
			name := strings.NewReplacer("/", "_", "?", "_", "=", "_", "&", "_", "|", "-").Replace(path)
			os.WriteFile(filepath.Join(dir, name+".json"), rec.Body.Bytes(), 0644)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	var m struct {
		Server  meterTotals   `json:"server"`
		Cohorts []meterCohort `json:"cohorts"`
	}
	get("/api/meter?window=1h", &m)
	if m.Server.Bots != 3 || len(m.Cohorts) != 2 {
		t.Fatalf("meter: %+v", m)
	}
	var bl struct {
		Total int           `json:"total"`
		Bots  []meterBotOut `json:"bots"`
	}
	get("/api/meter/bots?cohort=swordman|41-60|party&sort=kpm", &bl)
	if bl.Total != 2 || bl.Bots[0].KPM != 1.5 {
		t.Fatalf("bots: %+v", bl)
	}
	var one map[string]interface{}
	get("/api/meter/bot/gamma", &one)
	if one["cohort"] != "swordman|41-60|solo" {
		t.Fatalf("bot: %v", one)
	}
	var neo neoOut
	get("/api/meter/neo", &neo)
	if len(neo.Members) != 2 || len(neo.Guild) != 2 {
		t.Fatalf("neo: party %d, starveil %d", len(neo.Members), len(neo.Guild))
	}
	var rows []meterRow
	get("/api/meter/rows", &rows)
	if len(rows) != 3 || len(rows[0].Win[mw60].Sec) != mbN {
		t.Fatalf("rows: %d", len(rows))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/meter/bot/nobody", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown bot: %d", rec.Code)
	}
}

func TestMeterAyla(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "status.json")
	t.Setenv("LEGION_HUMAN_STATUS", f)
	old := meterAylaS
	defer func() { meterAylaS = old }()
	meterAylaS.samples, meterAylaS.sess, meterAylaS.last = nil, aylaWin{}, nil
	t0 := time.Now()
	write := func(at time.Time, zeny int, m string, sitting bool) {
		b, _ := json.Marshal(humanStatus{Name: "Ayla", Online: true, HP: 10, MaxHP: 10, Zeny: zeny, Map: m,
			Sitting: sitting, BaseLevel: 81, JobID: 12, Guild: "Starveil", TS: at.Unix()})
		os.WriteFile(f, b, 0644)
		meterAylaSample(at)
	}
	write(t0, 10000, "pay_dun00", false)
	write(t0.Add(15*time.Second), 10100, "pay_dun00", false) // 15 s hunting, +100
	write(t0.Add(30*time.Second), 10100, "pay_dun00", true)  // 15 s hunting
	write(t0.Add(45*time.Second), 10050, "payon", false)     // 15 s resting, -50
	w := aylaWindow(t0.Add(45*time.Second), time.Hour)
	if w.ZDelta != 50 || math.Abs(w.Min-0.75) > 1e-9 || w.Sec["hunting"] != 30 || w.Sec["resting"] != 15 {
		t.Fatalf("ayla window %+v", w)
	}
	if meterAylaS.sess.ZDelta != 50 {
		t.Fatalf("ayla session %+v", meterAylaS.sess)
	}
	v := meterBuild(nil, t0.Add(45*time.Second))
	if v.Ayla == nil || v.Ayla.Cohort != "thief|81-99|party" || v.Ayla.Windows["1h"].ZPHPurse != 4000 {
		t.Fatalf("ayla view %+v", v.Ayla)
	}
}
