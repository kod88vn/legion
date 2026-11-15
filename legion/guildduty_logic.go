package main

// guildduty_logic.go: the pure rules of guild direction and duty (no locks, no bots, no I/O), so they can be unit
// tested: which direction fits a member, how a played day is judged, how a standing moves with the guild's
// strictness, who is never kicked, how guild rank and wealth are scored and how relations between guilds drift.
// The engine side (snapshots, blocks, warnings, kicks, persistence, API) is in guildduty.go / guildrank.go.

import (
	"math"
	"sort"
	"strings"
)

// direction kinds
const (
	dirStock  = "stock"  // farm an item the guild stock lacks (guildstock.go gaps)
	dirGear   = "gear"   // farm a gear piece a member needs
	dirCarry  = "carry"  // hunt on the lowest members' map (the party logic / mentoring does the carrying)
	dirLevel  = "level"  // level on a map (or anywhere fitting) - a leader's call, autopilot never sets it
	dirSupply = "supply" // healing drops (herbs, food) for the guild's potion stock
	dirWealth = "wealth" // farm a sellable item for the guild shop (item given, or the member's best market opportunity)
)

var dirKinds = []string{dirStock, dirGear, dirCarry, dirLevel, dirSupply, dirWealth}

func validDirKind(k string) bool {
	for _, x := range dirKinds {
		if x == k {
			return true
		}
	}
	return false
}

// dirFarms: the kind sends the member to farm an item (its drop maps decide where)
func dirFarms(k string) bool {
	return k == dirStock || k == dirGear || k == dirSupply || k == dirWealth
}

// dutyFitIn: what the basic fit check needs to know about a member
type dutyFitIn struct {
	Lv       int
	Class    string // className, e.g. "Hunter"
	Healer   bool   // support healer (Acolyte line, support build): it doesn't solo-farm
	Vendor   bool   // vendor character
	Beginner bool   // under beginner_lv: stays near its home town (owner rule)
}

// dirBasicFit: the level / class / role part of "safe for it and it can do it". The map part (the existing map-fit
// checks: fitFarms, soloMapOK) runs on the bot afterwards. Returns ok and the reason when not.
func dirBasicFit(d *GuildDirection, m dutyFitIn) (bool, string) {
	if d == nil {
		return false, "no direction"
	}
	if m.Vendor {
		return false, "vendor"
	}
	if m.Beginner {
		return false, "beginner (stays near home)"
	}
	if d.MinLv > 0 && m.Lv < d.MinLv {
		return false, "level under the minimum"
	}
	if d.MaxLv > 0 && m.Lv > d.MaxLv {
		return false, "level over the maximum"
	}
	if len(d.Classes) > 0 {
		ok := false
		for _, c := range d.Classes {
			if classMatches(c, m.Class) {
				ok = true
				break
			}
		}
		if !ok {
			return false, "class not asked for"
		}
	}
	if m.Healer && dirFarms(d.Kind) {
		return false, "a healer doesn't solo-farm"
	}
	if d.Kind == dirCarry && d.Map == "" {
		return false, "carry without a map"
	}
	return true, ""
}

// classMatches: a direction's class entry ("hunter", "Archer line", "knight") against a class name
func classMatches(want, class string) bool {
	w := strings.ToLower(strings.TrimSpace(want))
	c := strings.ToLower(class)
	if w == "" {
		return false
	}
	if w == c {
		return true
	}
	lines := map[string][]string{
		"swordman": {"swordman", "knight", "crusader", "lord knight", "paladin", "high swordman"},
		"mage":     {"mage", "wizard", "sage", "high wizard", "professor", "high mage"},
		"archer":   {"archer", "hunter", "bard", "dancer", "sniper", "clown", "gypsy", "high archer"},
		"acolyte":  {"acolyte", "priest", "monk", "high priest", "champion", "high acolyte"},
		"merchant": {"merchant", "blacksmith", "alchemist", "whitesmith", "creator", "high merchant"},
		"thief":    {"thief", "assassin", "rogue", "assassin cross", "stalker", "high thief"},
	}
	w = strings.TrimSuffix(strings.TrimSuffix(w, " line"), "s")
	for _, x := range lines[w] {
		if x == c {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- days

// dutyDay: one member's guild duty on one calendar day (server local time)
type dutyDay struct {
	Date      string `json:"date"`             // 2006-01-02
	OnlineMin int    `json:"online_min"`       // minutes online in the guild
	DutyMin   int    `json:"duty_min"`         // minutes credited on a guild block / direction map
	Fit       int    `json:"fit"`              // plan checks where a direction fitted (a block was taken)
	Unfit     int    `json:"unfit"`            // plan checks where nothing fitted (not safe / can't do)
	Busy      int    `json:"busy"`             // plan checks while partied / otherwise busy
	OnMin     int    `json:"on_min"`           // minutes online while guild_duty_on was set
	Result    string `json:"result,omitempty"` // met / short / excused / unplayed / off (set when the day closes)
	Owed      int    `json:"owed,omitempty"`
}

// dutyOwed: what a played day asks for - the daily duty, but never more than half the time played (owner: "maybe not
// dedicate 100% of the time for guild's direction but maybe 2h a day")
func dutyOwed(onlineMin, dutyMin int) int {
	return max(0, min(dutyMin, onlineMin/2))
}

// dutyTolerance: minutes short that still count as met (a town run, a death walk)
const dutyTolerance = 10

// closeDay: judges a finished day. playedMin = the online minutes that make it a played day.
//
//	unplayed - online under playedMin (not counted at all)
//	off      - guild_duty_on was off for most of the day (ledger only, no standing change)
//	met      - credited >= owed - tolerance
//	excused  - nothing ever fitted it (no plan check found a safe, doable direction)
//	short    - everything else
func closeDay(d *dutyDay, dutyMin, playedMin int) string {
	d.Owed = dutyOwed(d.OnlineMin, dutyMin)
	switch {
	case d.OnlineMin < playedMin:
		d.Result = "unplayed"
	case d.OnMin*2 < d.OnlineMin:
		d.Result = "off"
	case d.DutyMin >= d.Owed-dutyTolerance:
		d.Result = "met"
	case d.Fit == 0:
		d.Result = "excused"
	default:
		d.Result = "short"
	}
	return d.Result
}

// ---------------------------------------------------------------- standing

const (
	standGood    = "good"
	standBehind  = "behind"
	standWarned  = "warned"
	standKicked  = "kicked"
	standExempt  = "exempt"
	actNone      = ""
	actWarn      = "warn"
	actKick      = "kick"
	strictNormal = 1.0
)

// dutyStanding: where a member stands with its guild
type dutyStanding struct {
	Standing  string `json:"standing"`
	Short     int    `json:"short_days"`       // short days not yet made up (a met day takes one off)
	SinceWarn int    `json:"short_since_warn"` // short days since the warning
}

// guildStrictness: the guild's style. Strict min-maxers kick sooner, the social guilds later.
func guildStrictness(guild string) float64 {
	switch guild {
	case "guild04", "guild07": // disciplined, cold min-maxers
		return 1.5
	case "guild01": // competitive
		return 1.25
	case "guild06": // laid-back, social
		return 0.5
	case "guild02", "guild10": // patient mentors, goofy
		return 0.6
	case "guild08": // earnest underdogs
		return 0.7
	case humanGuildID:
		return 0.8
	}
	return strictNormal
}

// dutyThresholds: short days before the warning, and short days after it before the kick, for a strictness
func dutyThresholds(warnDays, kickDays int, strict float64) (warnAt, kickAfter int) {
	if strict <= 0 {
		strict = strictNormal
	}
	warnAt = max(1, int(math.Round(float64(warnDays)/strict)))
	kickAfter = max(1, int(math.Round(float64(kickDays)/strict)))
	return
}

// nextStanding: the standing after one closed day. Only met and short days move it.
func nextStanding(st dutyStanding, result string, warnAt, kickAfter int) (dutyStanding, string) {
	if st.Standing == "" {
		st.Standing = standGood
	}
	if st.Standing == standExempt || st.Standing == standKicked {
		return st, actNone
	}
	switch result {
	case "met":
		st.Short = max(0, st.Short-1)
		if st.Short == 0 {
			st.Standing, st.SinceWarn = standGood, 0
		} else if st.Standing != standWarned {
			st.Standing = standBehind
		}
	case "short":
		st.Short++
		if st.Standing == standWarned {
			st.SinceWarn++
			if st.SinceWarn >= kickAfter {
				st.Standing = standKicked
				return st, actKick
			}
			return st, actNone
		}
		if st.Short >= warnAt {
			st.Standing, st.SinceWarn = standWarned, 0
			return st, actWarn
		}
		st.Standing = standBehind
	}
	return st, actNone
}

// dutyExemptIn: why a member is never warned or kicked
type dutyExemptIn struct {
	Leader    bool // guild leader / in-game master
	Alchemist bool // the guild's alchemist
	Trader    bool // the guild's trader, or any vendor character (its shop is the guild's stock)
	AylaParty bool // Ayla's party: core instance, LEGION_CORE / -exclude names, pinned to a human party, kept accounts
}

// No human exemption (owner 2026-11-14, gate G25: no in-world difference between humans and other players). The
// ledger measures only engine-run characters so far; a human member's duty is open work (STATUS.md "Equal
// treatment of humans: gaps").

// dutyExempt: the reason a member is exempt ("" = not exempt)
func dutyExempt(e dutyExemptIn) string {
	switch {
	case e.AylaParty:
		return "Ayla's party"
	case e.Leader:
		return "guild leader"
	case e.Alchemist:
		return "guild alchemist"
	case e.Trader:
		return "guild trader"
	}
	return ""
}

// ---------------------------------------------------------------- rank and wealth

// rankIn: what a guild's rank is computed from
type rankIn struct {
	GuildLevel int
	MemberLvs  []int // every known member's base level
	TopPlaces  []int // leaderboard places (1 = best) of members in the server top 100
}

// guildRankScore = guild level x 10 + the average of its 10 best members' levels + leaderboard points (a member at
// place p of the top 100 brings (101 - p) / 10).
func guildRankScore(in rankIn) float64 {
	lv := append([]int(nil), in.MemberLvs...)
	sort.Sort(sort.Reverse(sort.IntSlice(lv)))
	if len(lv) > 10 {
		lv = lv[:10]
	}
	avg := 0.0
	for _, x := range lv {
		avg += float64(x)
	}
	if len(lv) > 0 {
		avg /= float64(len(lv))
	}
	pts := 0.0
	for _, p := range in.TopPlaces {
		if p >= 1 && p <= 100 {
			pts += float64(101-p) / 10
		}
	}
	return math.Round((float64(in.GuildLevel)*10+avg+pts)*10) / 10
}

// guildWealth = market value of the guild's stock (its vendors' listings) + guild funds (the leader's and the guild
// vendors' zeny)
func guildWealth(stockValue, funds int) int { return max(0, stockValue) + max(0, funds) }

// rankOrder: guild ids ordered by score (ties by wealth, then id); returns id -> place (1 = top)
func rankOrder(score map[string]float64, wealth map[string]int) map[string]int {
	ids := make([]string, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if score[a] != score[b] {
			return score[a] > score[b]
		}
		if wealth[a] != wealth[b] {
			return wealth[a] > wealth[b]
		}
		return a < b
	})
	out := map[string]int{}
	for i, id := range ids {
		out[id] = i + 1
	}
	return out
}

// joinPreference: how much a guild-less bot wants to join a guild: higher rank and wealth, friends inside, room
// left. place = rank place (1 = top) of n guilds; wealthShare = this guild's wealth / the richest guild's (0..1).
func joinPreference(place, n int, wealthShare float64, friends int, room bool) float64 {
	if !room || n <= 0 {
		return 0
	}
	rank := float64(n-place+1) / float64(n) // 1 for the top guild
	return 40*rank + 30*math.Max(0, math.Min(1, wealthShare)) + 20*float64(min(friends, 3)) + 1
}

// ---------------------------------------------------------------- relations

// relation events (deltas on the -1 (rival) .. +1 (ally) scale)
const (
	relPoach     = -0.08 // a member left one guild and joined the other within hours (friend move, poaching)
	relFairTrade = 0.01  // a fair-priced trade between members of the two guilds
	relGift      = 0.02  // a free hand-over between them
	relDecayDay  = 0.05  // share of the distance to neutral that fades per day without events
)

// relApply: a relation after an event, clamped to -1..1
func relApply(v, delta float64) float64 { return math.Max(-1, math.Min(1, v+delta)) }

// relDrift: the relation after `days` without events: it fades toward neutral (0)
func relDrift(v, days float64) float64 {
	if days <= 0 {
		return v
	}
	return v * math.Pow(1-relDecayDay, days)
}

// relLabel: words for briefings and chat tone
func relLabel(v float64) string {
	switch {
	case v <= -0.5:
		return "rival"
	case v <= -0.15:
		return "cool"
	case v < 0.15:
		return "neutral"
	case v < 0.5:
		return "friendly"
	}
	return "ally"
}

// relKey: the table key of two guilds (order-free)
func relKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}
