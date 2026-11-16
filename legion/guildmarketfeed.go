package main

// guildmarketfeed.go (WP5, EC43; design docs/design/market-engine.md 4.2). Guild leaders read the market feed as
// INFORMATION. The feed never sets a direction: this file scores the feed's short items for one guild, and the leader's
// own autopilot (dutyAutopilotRound) takes the pick as the wealth direction's item. The market gives signals only (EC43).
//
// score = need^Need * gap^Gap * fit^Fit / (1 + chasers). The personality weights are exponents, so they change the
// ranking between items (with plain multipliers every guild would rank the same way):
//
//	need   0.25 + 0.75 x (share of the guild's members whose next build piece is the item; feed guild block)
//	gap    (short units / demand) x (price / the feed's highest price among the short items)
//	fit    min(1, drop maps / 3): the places the item can be farmed (member safety is checked per member in dutyMaps)
//	chasers  the feed's farmers on it + the other guilds' picks of it (the herd penalty; the ten leaders do not all pick one item)
//
// A pick is held for gmfHold (24 h). Inside the hold it is kept while it stays short. After the hold a new item replaces
// it only when its score is gmfSwitch times the held item's current score, so a guild does not flip-flop.
// Flag guild_market_feed_on (tuning.go) gates everything here: off means nothing is read and nothing changes.

import (
	"math"
	"time"
)

const (
	gmfHold   = 24 * time.Hour
	gmfSwitch = 1.25 // a replacement pick must score this many times the held item's current score
)

// gmfWeights is one guild's personality knob (report docs/reports/ai-vs-algorithm-20261115.md, section 2.2).
type gmfWeights struct{ Need, Gap, Fit float64 }

// gmfPersonality: the styles the report names. Styles not named there keep 1.0 on every factor.
var gmfPersonality = map[string]gmfWeights{
	"guild01": {Need: 1.0, Gap: 1.0, Fit: 1.3}, // 01 rank: whoever can do it wins, fit first
	"guild02": {Need: 1.3, Gap: 0.8, Fit: 1.0}, // 02 mentors: the build pieces their members wait for
	"guild03": {Need: 0.8, Gap: 1.5, Fit: 1.0}, // 03 wealth and loot: the biggest market gap
	"guild04": {Need: 1.4, Gap: 1.0, Fit: 0.9}, // 04 duty and order: what the members need next
	"guild05": {Need: 0.9, Gap: 0.9, Fit: 1.2}, // 05 explorers: items on less-farmed maps weigh more (fit)
	"guild07": {Need: 1.0, Gap: 1.2, Fit: 1.1}, // 07 exp and zeny: a gap that pays
}

func gmfWeightsOf(guild string) gmfWeights {
	if w, ok := gmfPersonality[guild]; ok {
		return w
	}
	return gmfWeights{Need: 1, Gap: 1, Fit: 1}
}

// gmfPick: the item a guild's wealth direction farms, with when it was chosen (persisted in guild_duty.json)
type gmfPick struct {
	ItemID int       `json:"item_id"`
	Item   string    `json:"item"`
	Since  time.Time `json:"since"`
	Score  float64   `json:"score"`
}

// gmfFeed: the feed in force, nil when the flag is off or no feed was written (read under the engine's lock, copied out).
func gmfFeed() *MEFeed {
	if !tune().GuildMarketFeedOn {
		return nil
	}
	meState.Lock()
	defer meState.Unlock()
	return meState.feed
}

// gmfScoreItems: every feed short item with a score for this guild (pure). others: the other guilds' picks by item id.
func gmfScoreItems(guild string, f *MEFeed, others map[int]int) map[int]float64 {
	out := map[int]float64{}
	if f == nil {
		return out
	}
	w := gmfWeightsOf(guild)
	var maxPrice float64
	for _, it := range f.Short {
		if float64(it.Price) > maxPrice {
			maxPrice = float64(it.Price)
		}
	}
	need := map[string]int{}
	total := 0
	if g, ok := f.Guilds[guild]; ok {
		for _, n := range g.Needs {
			need[n.Item] += n.N
			total += n.N
		}
	}
	for _, it := range f.Short {
		gap := it.Demand - it.Listed
		if it.ItemID <= 0 || gap <= 0 || it.Demand <= 0 || len(it.DropMaps) == 0 || maxPrice <= 0 {
			continue
		}
		share := 0.0
		if total > 0 {
			share = float64(need[it.Item]) / float64(total)
		}
		needF := math.Pow(0.25+0.75*share, w.Need)
		gapF := math.Pow(float64(gap)/float64(it.Demand)*(float64(it.Price)/maxPrice), w.Gap)
		fitF := math.Pow(math.Min(1, float64(len(it.DropMaps))/3), w.Fit)
		chasers := it.FarmersOn + others[it.ItemID]
		out[it.ItemID] = needF * gapF * fitF / float64(1+chasers)
	}
	return out
}

// gmfChoose (no bot lock held; takes gduty): the item this guild farms for the market, 0 for none. Updates the pick.
func gmfChoose(guild string, f *MEFeed, now time.Time) int {
	gduty.Lock()
	defer gduty.Unlock()
	others := map[int]int{}
	for g, p := range gduty.gmf {
		if g != guild && p.ItemID > 0 {
			others[p.ItemID]++
		}
	}
	scores := gmfScoreItems(guild, f, others)
	held, has := gduty.gmf[guild]
	if has && now.Sub(held.Since) < gmfHold && scores[held.ItemID] > 0 {
		// inside the hold: kept while it is still short (its score is the one shown, so the record stays true)
		held.Score = scores[held.ItemID]
		gduty.gmf[guild] = held
		gduty.dirty = true
		return held.ItemID
	}
	best, bestScore := 0, 0.0
	for id, s := range scores {
		if s > bestScore || (s == bestScore && id < best) {
			best, bestScore = id, s
		}
	}
	if best == 0 {
		if has {
			delete(gduty.gmf, guild)
			gduty.dirty = true
		}
		return 0
	}
	if has && held.ItemID != best && scores[held.ItemID] > 0 && bestScore < gmfSwitch*scores[held.ItemID] {
		held.Score = scores[held.ItemID]
		gduty.gmf[guild] = held
		gduty.dirty = true
		return held.ItemID
	}
	if has && held.ItemID == best {
		held.Score = bestScore
		held.Since = now // the same item again: a fresh hold, the record reads the current score
		gduty.gmf[guild] = held
		gduty.dirty = true
		return best
	}
	name := ""
	for _, it := range f.Short {
		if it.ItemID == best {
			name = it.Item
		}
	}
	gduty.gmf[guild] = gmfPick{ItemID: best, Item: name, Since: now, Score: bestScore}
	gduty.dirty = true
	dutyCount("market feed picks")
	return best
}

// gmfApply (no lock held): the guild's autopilot directions with the feed pick as the wealth direction's item.
// Flag off, no feed or no pick: the directions come back unchanged (same slice).
func gmfApply(guild string, dirs []*GuildDirection, f *MEFeed, now time.Time) []*GuildDirection {
	if !tune().GuildMarketFeedOn || f == nil {
		return dirs
	}
	id := gmfChoose(guild, f, now)
	if id == 0 {
		return dirs
	}
	idx := -1
	for i, d := range dirs {
		if d.Kind == dirWealth {
			idx = i
		}
	}
	if idx < 0 {
		if len(dirs) >= guildDirMax {
			dirs = dirs[:guildDirMax-1]
		}
		d := &GuildDirection{Guild: guild, Kind: dirWealth, SetBy: "autopilot", Set: now, Expires: now.Add(15 * time.Minute)}
		d.ID = guild + "-auto-wealth"
		dirs = append(dirs, d)
		idx = len(dirs) - 1
	}
	d := *dirs[idx]
	d.ItemID, d.Item = id, itemName(id)
	d.Note = "farm " + d.Item + ": the market is short of it (feed)"
	dirs[idx] = &d
	return dirs
}
