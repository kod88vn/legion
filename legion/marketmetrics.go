package main

// marketmetrics.go (market WP8; design docs/design/market-metrics.md, section 3.5 of market-engine.md).
//
// Flag-independent metrics for the market engine: they count whether the server is getting what it needs, whatever
// tuning.json says. Nothing here changes a bot or a price; it only reads the engine and keeps counters.
//
//   coverage         units supplied / need units, per band and per item. Need units = sum over clock hours of the peak
//                    number of bots (in the band) whose next build piece is the item ("bot-hours of demand, peaked").
//                    Supplied = units farmers delivered (noteMMSupplied, called by the farmer code).
//   price stability  stddev of (price / standard price) per item over the samples taken each engine round. Target < 1.5.
//   farmers          offers, accepts, declines by reason, deaths, farmer-hours (placeholders: the farmer-orders branch
//                    calls the note functions; until then every number is null or 0 and status is "no_data").
//   kite             kite steps and deaths (kite.go counters, read here).
//   zeny_per_hour    the meter's income per bot-hour (60-min window, counted rows) by band (meterCur).
//
// Persistence: <data>/market_metrics.json (core instance: market_metrics_core.json), written every minute with the
// same atomic rename as econ_counters.json, read at start-up and added to the running counts. Counts of the hour in
// progress are not saved (at most one hour of need is lost at a restart).
//
// JSON: marketMetricsBlock(now) is served by GET /api/meter/market and as the "metrics" key of GET /api/market/engine
// (on or off). Shape: docs/design/market-metrics.md. Every ratio is null when its denominator is zero (no invented
// numbers).

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	mmPriceTarget = 1.5 // price stability target: stddev of price / standard price, per item
	mmPriceMinN   = 3   // samples an item needs before its stddev counts
	mmTopN        = 40  // rows per list in the block
)

// mmKey: one band and one item (need and supply are kept per band and item).
type mmKey struct {
	Band string
	Item int
}

// mmBucket: the open clock hour of one band and item; Peak is the most bots short of it seen in that hour.
type mmBucket struct {
	Hour int64
	Peak int
}

// mmSaved is the file <data>/market_metrics.json.
type mmSaved struct {
	Since       time.Time                  `json:"since"`
	Saved       time.Time                  `json:"saved"`
	Rounds      int64                      `json:"rounds"`
	Need        map[string]map[int]float64 `json:"need"`     // band -> item -> need units (closed hours)
	Supplied    map[string]map[int]int64   `json:"supplied"` // band -> item -> units delivered
	PriceN      map[int]int64              `json:"price_n"`
	PriceSum    map[int]float64            `json:"price_sum"`
	PriceSq     map[int]float64            `json:"price_sq"`
	Offers      int64                      `json:"offers"`
	Accepts     int64                      `json:"accepts"`
	Declines    map[string]int64           `json:"declines"`
	Deaths      int64                      `json:"deaths"`
	FarmerHours float64                    `json:"farmer_hours"`
}

// mm: the counters. Lock it for every access.
var mm struct {
	sync.Mutex
	since    time.Time
	rounds   int64
	need     map[string]map[int]float64
	supplied map[string]map[int]int64
	open     map[mmKey]*mmBucket
	pN       map[int]int64
	pSum     map[int]float64
	pSq      map[int]float64
	offers   int64
	accepts  int64
	declines map[string]int64
	deaths   int64
	farmerH  float64
	ensured  bool
}

// mmEnsureLocked makes the maps usable (mm locked).
func mmEnsureLocked() {
	if mm.ensured {
		return
	}
	mm.need = map[string]map[int]float64{}
	mm.supplied = map[string]map[int]int64{}
	mm.open = map[mmKey]*mmBucket{}
	mm.pN = map[int]int64{}
	mm.pSum = map[int]float64{}
	mm.pSq = map[int]float64{}
	mm.declines = map[string]int64{}
	mm.ensured = true
}

// mmStddev: the population stddev of the samples given as count, sum and sum of squares. ok=false under mmPriceMinN.
func mmStddev(n int64, sum, sq float64) (float64, bool) {
	if n < mmPriceMinN {
		return 0, false
	}
	mean := sum / float64(n)
	v := sq/float64(n) - mean*mean
	if v < 0 {
		v = 0 // rounding
	}
	return math.Sqrt(v), true
}

// mmCoverage: supplied / need. ok=false when there is no need (never a made-up 0 or 1).
func mmCoverage(supplied, need float64) (float64, bool) {
	if need <= 0 {
		return 0, false
	}
	return supplied / need, true
}

// noteMMSupplied counts n units of an item that farmers delivered in a band (the farmer code calls this).
func noteMMSupplied(band string, item int, n int64) {
	if n <= 0 || band == "" {
		return
	}
	mm.Lock()
	defer mm.Unlock()
	mmEnsureLocked()
	if mm.supplied[band] == nil {
		mm.supplied[band] = map[int]int64{}
	}
	mm.supplied[band][item] += n
}

// noteMMFarmerOffer counts an order offered to a farmer.
func noteMMFarmerOffer() {
	mm.Lock()
	mm.offers++
	mm.Unlock()
}

// noteMMFarmerAccept counts an accepted order.
func noteMMFarmerAccept() {
	mm.Lock()
	mm.accepts++
	mm.Unlock()
}

// noteMMFarmerDecline counts a declined order by reason (for example "unsafe map", "gear gate").
func noteMMFarmerDecline(reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	mm.Lock()
	defer mm.Unlock()
	mmEnsureLocked()
	mm.declines[reason]++
}

// noteMMFarmerDeath counts a farmer's death.
func noteMMFarmerDeath() {
	mm.Lock()
	mm.deaths++
	mm.Unlock()
}

// noteMMFarmerHours adds farmer online hours (the denominator of deaths per farmer-hour).
func noteMMFarmerHours(h float64) {
	if h <= 0 {
		return
	}
	mm.Lock()
	mm.farmerH += h
	mm.Unlock()
}

// mmApplyRound: one engine round. counts: bots short per band and item now; ratios: price / standard per item now.
// The need of a (band, item) is the peak count seen in each clock hour, added when the hour closes.
func mmApplyRound(now time.Time, counts map[mmKey]int, ratios map[int]float64) {
	mm.Lock()
	defer mm.Unlock()
	mmEnsureLocked()
	hour := now.Unix() / 3600
	for k, b := range mm.open {
		if b.Hour != hour {
			mmAddNeedLocked(k.Band, k.Item, float64(b.Peak))
			delete(mm.open, k)
		}
	}
	for k, c := range counts {
		if c <= 0 {
			continue
		}
		b := mm.open[k]
		if b == nil {
			b = &mmBucket{Hour: hour}
			mm.open[k] = b
		}
		if c > b.Peak {
			b.Peak = c
		}
	}
	for id, r := range ratios {
		mm.pN[id]++
		mm.pSum[id] += r
		mm.pSq[id] += r * r
	}
	mm.rounds++
}

// mmAddNeedLocked adds need units to a band and item (mm locked).
func mmAddNeedLocked(band string, item int, v float64) {
	if v <= 0 {
		return
	}
	if mm.need[band] == nil {
		mm.need[band] = map[int]float64{}
	}
	mm.need[band][item] += v
}

// marketMetricsRound is called by econRound every round, whatever the market_engine_on flag says.
func marketMetricsRound(snaps []*econSnap, now time.Time) {
	counts := map[mmKey]int{}
	ids := map[int]bool{}
	for _, s := range snaps {
		if s.nextPiece > 0 && !s.keeper {
			counts[mmKey{Band: meBand(s.lv), Item: s.nextPiece}]++
			ids[s.nextPiece] = true
		}
	}
	for id := itemRedPotion; id <= itemWhitePotion; id++ {
		ids[id] = true
	}
	ids[itemWhiteHerb] = true
	ratios := map[int]float64{}
	for id := range ids {
		std := standardPrice(id)
		if std <= 0 {
			continue
		}
		ratios[id] = float64(priceOf(id)) / float64(std)
	}
	mmApplyRound(now, counts, ratios)
}

// loadMarketMetrics (start-up, before the first round): add the saved counters to this run's.
func loadMarketMetrics() {
	mm.Lock()
	mmEnsureLocked()
	mm.since = time.Now()
	mm.Unlock()
	raw, err := os.ReadFile(mmFileName())
	if err != nil {
		return
	}
	var s mmSaved
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	mm.Lock()
	defer mm.Unlock()
	if !s.Since.IsZero() {
		mm.since = s.Since
	}
	mm.rounds += s.Rounds
	for band, m := range s.Need {
		for id, v := range m {
			mmAddNeedLocked(band, id, v)
		}
	}
	for band, m := range s.Supplied {
		if mm.supplied[band] == nil {
			mm.supplied[band] = map[int]int64{}
		}
		for id, v := range m {
			mm.supplied[band][id] += v
		}
	}
	for id, v := range s.PriceN {
		mm.pN[id] += v
	}
	for id, v := range s.PriceSum {
		mm.pSum[id] += v
	}
	for id, v := range s.PriceSq {
		mm.pSq[id] += v
	}
	mm.offers += s.Offers
	mm.accepts += s.Accepts
	for k, v := range s.Declines {
		mm.declines[k] += v
	}
	mm.deaths += s.Deaths
	mm.farmerH += s.FarmerHours
}

func mmFileName() string {
	if coreInstance() {
		return filepath.Join(*dataDir, "market_metrics_core.json")
	}
	return filepath.Join(*dataDir, "market_metrics.json")
}

// saveMarketMetrics writes the counters (closed hours only) atomically: temp file, then rename.
func saveMarketMetrics(now time.Time) {
	mm.Lock()
	mmEnsureLocked()
	s := mmSaved{Saved: now, Since: mm.since, Rounds: mm.rounds, Need: map[string]map[int]float64{},
		Supplied: map[string]map[int]int64{}, PriceN: map[int]int64{}, PriceSum: map[int]float64{}, PriceSq: map[int]float64{},
		Offers: mm.offers, Accepts: mm.accepts, Declines: map[string]int64{}, Deaths: mm.deaths, FarmerHours: mm.farmerH}
	for band, m := range mm.need {
		s.Need[band] = map[int]float64{}
		for id, v := range m {
			s.Need[band][id] = v
		}
	}
	for band, m := range mm.supplied {
		s.Supplied[band] = map[int]int64{}
		for id, v := range m {
			s.Supplied[band][id] = v
		}
	}
	for id, v := range mm.pN {
		s.PriceN[id] = v
		s.PriceSum[id] = mm.pSum[id]
		s.PriceSq[id] = mm.pSq[id]
	}
	for k, v := range mm.declines {
		s.Declines[k] = v
	}
	mm.Unlock()
	raw, err := json.Marshal(s)
	if err != nil {
		return
	}
	name := mmFileName()
	tmp := name + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		os.Rename(tmp, name)
	}
}

// mmZeny: the meter's zeny per hour by band (60-min window, counted rows only). Empty when the meter has no view.
type mmZeny struct {
	Band string
	N    int
	Sum  float64
}

func mmZenyBands() []mmZeny {
	v := meterCur.Load()
	if v == nil {
		return nil
	}
	acc := map[string]*mmZeny{}
	for _, r := range v.Rows {
		st := r.stat[mw60]
		if !st.Counted {
			continue
		}
		b := meBand(r.Lv)
		if acc[b] == nil {
			acc[b] = &mmZeny{Band: b}
		}
		acc[b].N++
		acc[b].Sum += st.ZPH
	}
	out := make([]mmZeny, 0, len(acc))
	for _, z := range acc {
		out = append(out, *z)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Band < out[j].Band })
	return out
}

// meZenyFeed: the market feed's zeny_per_hour (only bands with counted bots; nil when none).
func meZenyFeed() []MEFeedZeny {
	var out []MEFeedZeny
	for _, z := range mmZenyBands() {
		if z.N > 0 {
			out = append(out, MEFeedZeny{Band: z.Band, N: z.N, ZPHMean: mmRound(z.Sum / float64(z.N))})
		}
	}
	return out
}

// meFeedMissingNow: the feed's missing list, without the zeny line once the meter feeds it.
func meFeedMissingNow(all []string, haveZeny bool) []string {
	var out []string
	for _, m := range all {
		if haveZeny && len(m) >= 14 && m[:14] == "zeny_per_hour:" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func mmRound(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// mmNum: a rounded number, or nil (JSON null) when there is no denominator.
func mmNum(v float64, ok bool) interface{} {
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return mmRound(v)
}

func mmItemName(id int) string {
	if x := items[id]; x != nil {
		return x.Name
	}
	return "#" + strconv.Itoa(id)
}

// marketMetricsBlock: the metrics block (design docs/design/market-metrics.md). now: the time it is served.
func marketMetricsBlock(now time.Time) map[string]interface{} {
	zeny := mmZenyBands()
	kite := kiteCounters()

	mm.Lock()
	mmEnsureLocked()
	since, rounds := mm.since, mm.rounds
	// need: closed hours plus the hour in progress (its peaks so far), per band and item
	need := map[mmKey]float64{}
	for band, m := range mm.need {
		for id, v := range m {
			need[mmKey{band, id}] += v
		}
	}
	for k, b := range mm.open {
		need[k] += float64(b.Peak)
	}
	supplied := map[mmKey]float64{}
	for band, m := range mm.supplied {
		for id, v := range m {
			supplied[mmKey{band, id}] += float64(v)
		}
	}
	type sdRow struct {
		id int
		n  int64
		sd float64
	}
	var sds []sdRow
	for id, n := range mm.pN {
		if sd, ok := mmStddev(n, mm.pSum[id], mm.pSq[id]); ok {
			sds = append(sds, sdRow{id, n, sd})
		}
	}
	offers, accepts, deaths, farmerH := mm.offers, mm.accepts, mm.deaths, mm.farmerH
	declines := map[string]int64{}
	for k, v := range mm.declines {
		declines[k] = v
	}
	mm.Unlock()

	// coverage: band and item sums
	type cov struct{ need, supplied float64 }
	byBand := map[string]*cov{}
	byItem := map[int]*cov{}
	var cells []mmKey
	for k := range need {
		cells = append(cells, k)
	}
	for k := range supplied {
		if _, ok := need[k]; !ok {
			cells = append(cells, k)
		}
	}
	var totN, totS float64
	for _, k := range cells {
		n, s := need[k], supplied[k]
		totN += n
		totS += s
		if byBand[k.Band] == nil {
			byBand[k.Band] = &cov{}
		}
		byBand[k.Band].need += n
		byBand[k.Band].supplied += s
		if byItem[k.Item] == nil {
			byItem[k.Item] = &cov{}
		}
		byItem[k.Item].need += n
		byItem[k.Item].supplied += s
	}
	bandRows := []map[string]interface{}{}
	bandNames := make([]string, 0, len(byBand))
	for b := range byBand {
		bandNames = append(bandNames, b)
	}
	sort.Strings(bandNames)
	for _, b := range bandNames {
		c := byBand[b]
		cv, ok := mmCoverage(c.supplied, c.need)
		bandRows = append(bandRows, map[string]interface{}{"band": b, "need": mmRound(c.need),
			"supplied": mmRound(c.supplied), "coverage": mmNum(cv, ok)})
	}
	itemRows := []map[string]interface{}{}
	itemIDs := make([]int, 0, len(byItem))
	for id := range byItem {
		itemIDs = append(itemIDs, id)
	}
	sort.Slice(itemIDs, func(i, j int) bool {
		a, b := byItem[itemIDs[i]].need, byItem[itemIDs[j]].need
		if a != b {
			return a > b
		}
		return itemIDs[i] < itemIDs[j]
	})
	for i, id := range itemIDs {
		if i >= mmTopN {
			break
		}
		c := byItem[id]
		cv, ok := mmCoverage(c.supplied, c.need)
		itemRows = append(itemRows, map[string]interface{}{"item_id": id, "item": mmItemName(id), "need": mmRound(c.need),
			"supplied": mmRound(c.supplied), "coverage": mmNum(cv, ok)})
	}
	totCov, totOK := mmCoverage(totS, totN)
	covStatus := "no_data"
	if totN > 0 || totS > 0 {
		covStatus = "ok"
	}

	// price stability
	sort.Slice(sds, func(i, j int) bool {
		if sds[i].sd != sds[j].sd {
			return sds[i].sd > sds[j].sd
		}
		return sds[i].id < sds[j].id
	})
	var sdSum float64
	over := 0
	priceRows := []map[string]interface{}{}
	for i, r := range sds {
		sdSum += r.sd
		if r.sd >= mmPriceTarget {
			over++
		}
		if i < mmTopN {
			priceRows = append(priceRows, map[string]interface{}{"item_id": r.id, "item": mmItemName(r.id),
				"n": r.n, "stddev": mmRound(r.sd)})
		}
	}
	var sdMean float64
	if len(sds) > 0 {
		sdMean = sdSum / float64(len(sds))
	}
	priceStatus := "no_data"
	var within interface{}
	if len(sds) > 0 {
		priceStatus = "ok"
		within = mmRound(sdMean) < mmPriceTarget
	}

	// farmers: placeholders until the farmer-orders branch calls the note functions
	farmerStatus := "no_data"
	if offers > 0 || accepts > 0 || deaths > 0 || farmerH > 0 || len(declines) > 0 {
		farmerStatus = "ok"
	}
	decl := map[string]int64{}
	for k, v := range declines {
		decl[k] = v
	}

	// kite
	steps, _ := kite["kite_steps"].(int64)
	kdeaths, _ := kite["kite_deaths"].(int64)
	kiteStatus := "no_data"
	if steps > 0 || kdeaths > 0 {
		kiteStatus = "ok"
	}

	// zeny per hour
	zRows := []map[string]interface{}{}
	for _, z := range zeny {
		var mean interface{}
		if z.N > 0 {
			mean = mmRound(z.Sum / float64(z.N))
		}
		zRows = append(zRows, map[string]interface{}{"band": z.Band, "n": z.N, "zph_mean": mean})
	}
	zStatus := "no_data"
	if len(zeny) > 0 {
		zStatus = "ok"
	}

	return map[string]interface{}{
		"t":              now,
		"counters_since": since,
		"rounds":         rounds,
		"coverage": map[string]interface{}{
			"status":  covStatus,
			"overall": mmNum(totCov, totOK),
			"unit":    "units supplied / need units (need = sum over hours of the peak bots short)",
			"by_band": bandRows,
			"by_item": itemRows,
		},
		"price_stability": map[string]interface{}{
			"status":        priceStatus,
			"target":        mmPriceTarget,
			"overall":       mmNum(sdMean, len(sds) > 0),
			"within_target": within,
			"items_counted": len(sds),
			"items_over":    over,
			"items":         priceRows,
		},
		"farmers": map[string]interface{}{
			"status":          farmerStatus,
			"offers":          offers,
			"accepted":        accepts,
			"accept_rate":     mmNum(float64(accepts)/float64(offers), offers > 0),
			"declined":        decl,
			"deaths":          deaths,
			"farmer_hours":    mmRound(farmerH),
			"deaths_per_hour": mmNum(float64(deaths)/farmerH, farmerH > 0),
			"items_supplied":  int64(totS),
		},
		"kite": map[string]interface{}{
			"status":          kiteStatus,
			"steps":           steps,
			"deaths":          kdeaths,
			"deaths_per_step": mmNum(float64(kdeaths)/float64(steps), steps > 0),
		},
		"zeny_per_hour": map[string]interface{}{
			"status":  zStatus,
			"by_band": zRows,
		},
	}
}

// marketMetricsHandler: GET /api/meter/market (registered in meterHandlers).
func marketMetricsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, marketMetricsBlock(time.Now()))
}
