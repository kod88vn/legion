package main

// guildduty.go (redesign package G, 2026-11-14). Owner (RULES.md 3b): "Bots should generally prioritize guild's
// direction because it's in a guild as long as it's safe and can do. Maybe not dedicate 100% of the time for guild's
// direction but maybe 2h a day, or risk getting kicked." No bounties, no payment.
//
//   Directions  up to 3 active per guild (guildDirMax): leader-set (POST /api/guild-direction/{guild}, 24 h by default)
//               first, autopilot fills the free slots (no AI: guildstock.go gaps -> supply / stock, the lowest member
//               -> carry, the guild's sale income -> wealth). Recomputed every 10 min.
//   Duty        a member owes guild_duty_min_per_day minutes (120) per played day, never more than half its time
//               online. At a plan check (every 10 min per member with duty outstanding, solo and free) the member
//               takes ONE guild block - a farm goal of kind "guild" (lfg.go farmGoal) on the first direction that is
//               safe for it (the existing map checks: fitFarms / soloMapOK) and that it can do - lasting what it
//               still owes today (30..120 min). The solo map picker (ai.go chooseHuntingGround -> guildBlockHunt)
//               sends it there; town runs at 50% weight, save points, fleeing and rests are untouched. Minutes on the
//               block's maps count. Nothing fits -> that check is excused.
//   Loot        items the direction asked for go to the guild stock: RODEX to the guild trader (as lootflow.go's
//               stock step and parcel.go do); the bot keeps everything else.
//   Ledger      minutes per member per day for the last 7 played days (data/guild_duty.json, written off-lock).
//   Standing    good / behind / warned / kicked (guildduty_logic.go nextStanding; per-guild strictness). The leader
//               warns in guild chat; a kick is an in-game expel (CZ_REQ_BAN_GUILD 0x015b) by the leader bot, paced
//               server-wide (guild_kick_per_hour, one per guild per 2 h, 10-60 min after the decision).
//   After       a kicked bot is independent: 24 h cool-down (refuses every invitation), then it seeks a guild with
//               room, preferring rank, wealth and friends (guildrank.go), and that guild's leader invites it.
//   Never       guild leaders, guild alchemists, traders / vendors, Ayla's party (core instance, -exclude /
//               LEGION_CORE names, pinned friends, kept accounts) are never warned or kicked. No human exemption
//               (gate G25); the ledger only measures engine-run characters so far (STATUS.md, equal-treatment gaps).
//
// Behaviour (blocks, warnings, kicks, invitations) needs tuning guild_duty_on (default off). The ledger, rank,
// wealth and relations are computed regardless. Only the main instance runs this; the core instance (Ayla's party)
// serves the endpoints with empty data.

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	guildDirMax       = 3
	guildDirLeaderTTL = 24 * time.Hour
	dutyTick          = time.Minute
	dutyPlanGap       = 10 * time.Minute // between two plan checks of one member
	dutyPlanPerTick   = 30               // plan checks per tick (each takes one bot's lock briefly)
	dutyBlockMinMin   = 30
	dutyBlockSlack    = 45 * time.Minute // a block may run this much longer than its minutes (town runs, deaths)
	dutyDaysKept      = 8                // today + the last 7 days
	dutyPlayedMin     = 30               // online minutes that make a played day
	dutyDeliverGap    = 10 * time.Minute
	dutyRecentBlock   = time.Hour // requested items of a block that ended this recently still go to the stock
	dutyWarnGap       = 5 * time.Minute
	dutyGuildKickGap  = 2 * time.Hour
	dutyKickMinGap    = 15 * time.Minute
	dutySayGap        = 3 * time.Minute // guild chat lines about blocks, per guild
	dutyOldGuildBan   = 7 * 24 * time.Hour
	dutyInviteGap     = 90 * time.Second
)

// GuildDirection: one standing direction of a guild
type GuildDirection struct {
	ID        string    `json:"id"`
	Guild     string    `json:"guild"`
	Kind      string    `json:"kind"` // stock|gear|carry|level|supply|wealth
	ItemID    int       `json:"item_id,omitempty"`
	Item      string    `json:"item,omitempty"`
	Map       string    `json:"map,omitempty"`
	MinLv     int       `json:"min_lv,omitempty"`
	MaxLv     int       `json:"max_lv,omitempty"`
	Classes   []string  `json:"classes,omitempty"`
	Note      string    `json:"note,omitempty"`
	SetBy     string    `json:"set_by"` // leader|autopilot
	Set       time.Time `json:"set"`
	Expires   time.Time `json:"expires"`
	Exclusive bool      `json:"exclusive,omitempty"` // leader-set: no autopilot directions beside it
}

// dutyBlock: a member's current (or last) guild block
type dutyBlock struct {
	DirID  string    `json:"dir_id"`
	Kind   string    `json:"kind"`
	ItemID int       `json:"item_id,omitempty"`
	Item   string    `json:"item,omitempty"`
	Items  []int     `json:"items,omitempty"` // requested items: they go to the guild stock
	Maps   []string  `json:"maps"`
	Target int       `json:"target_min"`
	Done   int       `json:"done_min"`
	Set    time.Time `json:"set"`
	Until  time.Time `json:"until"`
	Ended  time.Time `json:"ended,omitempty"`
	EndWhy string    `json:"end_why,omitempty"`
	prev   *farmGoal // the goal it had before (restored when the block ends; not persisted)
}

func (k *dutyBlock) active() bool { return k != nil && k.Ended.IsZero() }

// dutyMember: one character's duty to its guild
type dutyMember struct {
	Char    string     `json:"char"`
	Account string     `json:"account"`
	Guild   string     `json:"guild"`
	Days    []*dutyDay `json:"days"`
	dutyStanding
	Exempt      string     `json:"exempt,omitempty"`
	WarnPending bool       `json:"warn_pending,omitempty"`
	WarnedAt    time.Time  `json:"warned_at,omitempty"`
	KickPending bool       `json:"kick_pending,omitempty"`
	KickAt      time.Time  `json:"kick_at,omitempty"`
	KickTries   int        `json:"kick_tries,omitempty"`
	KickSentAt  time.Time  `json:"kick_sent_at,omitempty"`
	KickWhy     string     `json:"kick_why,omitempty"`
	Pardons     int        `json:"pardons,omitempty"`
	Block       *dutyBlock `json:"block,omitempty"`
	LastPlan    time.Time  `json:"last_plan,omitempty"`
	LastFit     string     `json:"last_fit,omitempty"` // what the last plan check found
	LastDeliver time.Time  `json:"last_deliver,omitempty"`
	Seen        time.Time  `json:"seen"`
}

func (m *dutyMember) today(date string) *dutyDay {
	if n := len(m.Days); n > 0 && m.Days[n-1].Date == date {
		return m.Days[n-1]
	}
	return nil
}

// dutyKicked: a bot expelled for not doing its duty, until it has a new guild
type dutyKicked struct {
	Char      string    `json:"char"`
	Account   string    `json:"account"`
	From      string    `json:"from"`      // guild id
	FromName  string    `json:"from_name"` // in-game guild name
	At        time.Time `json:"at"`
	CoolUntil time.Time `json:"cool_until"`
	Stage     string    `json:"stage"` // cooldown | seeking | invited | joined
	Target    string    `json:"target,omitempty"`
	Invites   int       `json:"invites,omitempty"`
	InviteAt  time.Time `json:"invite_at,omitempty"`
	NextTry   time.Time `json:"next_try,omitempty"`
	Joined    string    `json:"joined,omitempty"`
	JoinedAt  time.Time `json:"joined_at,omitempty"`
}

var gduty = struct {
	sync.Mutex
	dirs      map[string][]*GuildDirection // guild -> leader-set directions (persisted)
	auto      map[string][]*GuildDirection // guild -> autopilot directions
	members   map[string]*dutyMember       // guild|char
	byChar    map[string]*dutyMember       // char -> its current record
	kicked    map[string]*dutyKicked       // char
	kicks     []time.Time                  // server-wide expel times (rate limit)
	guildKick map[string]time.Time
	guildWarn map[string]time.Time
	guildSay  map[string]time.Time
	charGuild map[string]string   // last snapshot: char -> guild id (relations, trades)
	left      map[string]dutyLeft // char -> the guild it left on its own (poaching)
	counters  map[string]int
	dirty     bool
	loaded    bool
}{dirs: map[string][]*GuildDirection{}, auto: map[string][]*GuildDirection{}, members: map[string]*dutyMember{},
	byChar: map[string]*dutyMember{}, kicked: map[string]*dutyKicked{}, guildKick: map[string]time.Time{},
	guildWarn: map[string]time.Time{}, guildSay: map[string]time.Time{}, charGuild: map[string]string{}, left: map[string]dutyLeft{}, counters: map[string]int{}}

type dutyLeft struct {
	guild string
	at    time.Time
}

func dutyOn() bool { return tune().GuildDutyOn && !coreInstance() }

func dutyCount(k string) { // gduty held
	gduty.counters[k]++
}

func dutyDate(t time.Time) string { return t.Format("2006-01-02") }

// dutyGuildID: our id of an in-game guild name ("" when it is no guild we know). starveil = Ayla's guild name.
func dutyGuildID(name, starveil string) string {
	if name == "" {
		return ""
	}
	if g := guildByName[strings.ToLower(name)]; g != nil {
		return g.ID
	}
	if starveil != "" && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(starveil)) {
		return humanGuildID
	}
	return ""
}

// dutyGuildName: the in-game name of a guild id
func dutyGuildName(id, starveil string) string {
	if id == humanGuildID {
		return starveil
	}
	if g := guildByID(id); g != nil {
		return g.Name
	}
	return id
}

// ---------------------------------------------------------------- snapshot

type dutySnap struct {
	b                  *Bot
	name, account      string
	guildName, guild   string
	gid                uint32
	aid, cid           uint32
	online, dead       bool
	lv                 int
	class, mapN        string
	master, vision     bool
	vendor, healer     bool
	beginner           bool
	pinned, keep, core bool
	partied, busy      bool
	blockSet           time.Time // fgoal is a guild block set at this time (zero: no guild block)
	zeny               int
}

// dutySnapshot: one short lock per bot, nothing else under it.
func dutySnapshot(bots []*Bot, starveil string) []*dutySnap {
	core := nameSet(*excludeNames)
	for k := range nameSet(os.Getenv("LEGION_CORE")) {
		core[k] = true
	}
	out := make([]*dutySnap, 0, len(bots))
	for _, b := range bots {
		b.mu.Lock()
		if !b.Online || b.BaseLv <= 0 {
			b.mu.Unlock()
			continue
		}
		s := &dutySnap{b: b, name: b.charName(), account: b.P.Account, guildName: b.GuildName, gid: b.GuildID, aid: b.aid,
			cid: b.cid, online: true, dead: b.Dead, lv: b.BaseLvOr(), class: className(b.Class), mapN: b.MapName,
			master: b.guildMaster, vendor: b.charRole() == "vendor", healer: b.isHealer(),
			beginner: b.BaseLvOr() < beginnerLv(), pinned: b.pin != nil, keep: seedKeep[b.P.AccountID],
			busy: b.trip != nil || b.kafra != nil || b.mkt != nil || (b.vend != nil && b.vend.Opened), zeny: b.Zeny}
		if g := b.fgoal; g != nil && g.Kind == "guild" {
			s.blockSet = g.Set
		}
		b.mu.Unlock()
		s.core = core[strings.ToLower(s.name)]
		s.vision = b.leaderVision() != nil
		s.partied = partyOf(b) != nil
		s.guild = dutyGuildID(s.guildName, starveil)
		out = append(out, s)
	}
	return out
}

// exemptOf: why this member is never warned or kicked (no bot lock held)
func (s *dutySnap) exemptOf() string {
	alch := guildAlchemistChar2(s.guild)
	return dutyExempt(dutyExemptIn{
		Leader:    s.vision || s.master,
		Alchemist: alch != "" && strings.EqualFold(alch, s.name),
		Trader:    s.vendor || strings.EqualFold(guildTrader(s.guild), s.name),
		AylaParty: coreInstance() || s.core || s.pinned || s.keep,
	})
}

// ---------------------------------------------------------------- directions

// activeDirsLocked (gduty held): the guild's directions in force: leader-set first, autopilot fills free slots
// (none beside an exclusive leader direction); duplicates of kind+item+map are skipped.
func activeDirsLocked(guild string, now time.Time) []*GuildDirection {
	var out []*GuildDirection
	seen := map[string]bool{}
	excl := false
	keep := gduty.dirs[guild][:0]
	for _, d := range gduty.dirs[guild] {
		if now.After(d.Expires) {
			gduty.dirty = true
			dutyCount("leader directions expired")
			continue
		}
		keep = append(keep, d)
		if len(out) < guildDirMax {
			out = append(out, d)
			seen[d.Kind+"|"+fmt.Sprint(d.ItemID)+"|"+d.Map] = true
			excl = excl || d.Exclusive
		}
	}
	gduty.dirs[guild] = keep
	if excl {
		return out
	}
	for _, d := range gduty.auto[guild] {
		k := d.Kind + "|" + fmt.Sprint(d.ItemID) + "|" + d.Map
		if len(out) >= guildDirMax || seen[k] || now.After(d.Expires) {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}

// autopilotDirs: what a guild with no AI asks of its members (pure: built from the stock round and the roster).
//
//	supply  the stock lacks healing items (members short of potions)
//	stock   the most valuable item gap of the stock
//	carry   the lowest member 15+ levels under the guild average, on its hunting map (carriers lv+10)
//	wealth  fill: farm the best sellable item for the guild shop
type dutyLow struct {
	name, mapN string
	lv         int
}

func autopilotDirs(guild string, gaps []int, healGap bool, lows []dutyLow, now time.Time) []*GuildDirection {
	exp := now.Add(15 * time.Minute)
	mk := func(kind string) *GuildDirection {
		return &GuildDirection{Guild: guild, Kind: kind, SetBy: "autopilot", Set: now, Expires: exp}
	}
	var out []*GuildDirection
	if healGap {
		d := mk(dirSupply)
		d.ID, d.Note = guild+"-auto-supply", "healing items for the guild stock"
		out = append(out, d)
	}
	for _, id := range gaps {
		if id == itemWhitePotion || id <= 0 {
			continue
		}
		d := mk(dirStock)
		d.ID, d.ItemID, d.Item = fmt.Sprintf("%s-auto-stock-%d", guild, id), id, itemName(id)
		d.Note = "the guild stock is short of " + d.Item
		out = append(out, d)
		break
	}
	if len(lows) >= 2 {
		sum := 0
		for _, l := range lows {
			sum += l.lv
		}
		avg := sum / len(lows)
		sort.Slice(lows, func(i, j int) bool { return lows[i].lv < lows[j].lv })
		for _, l := range lows {
			if l.lv > avg-15 {
				break
			}
			if l.mapN == "" || isTown(l.mapN) || strings.Contains(l.mapN, "_in") {
				continue
			}
			d := mk(dirCarry)
			d.ID, d.Map, d.MinLv = guild+"-auto-carry-"+l.mapN, l.mapN, l.lv+10
			d.Note = fmt.Sprintf("carry %s (lv%d) on %s", l.name, l.lv, l.mapN)
			out = append(out, d)
			break
		}
	}
	if len(out) < guildDirMax {
		d := mk(dirWealth)
		d.ID, d.Note = guild+"-auto-wealth", "farm what sells for the guild shop"
		out = append(out, d)
	}
	if len(out) > guildDirMax {
		out = out[:guildDirMax]
	}
	return out
}

// dutyAutopilotRound: recompute every guild's autopilot directions (no bot lock held)
func dutyAutopilotRound(snaps []*dutySnap, now time.Time) {
	byGuild := map[string][]dutyLow{}
	for _, s := range snaps {
		if s.guild != "" && !s.vendor {
			byGuild[s.guild] = append(byGuild[s.guild], dutyLow{s.name, s.mapN, s.lv})
		}
	}
	ids := map[string]bool{humanGuildID: true}
	for _, g := range guildDefs {
		ids[g.ID] = true
	}
	res := map[string][]*GuildDirection{}
	for id := range ids {
		gstock.Lock()
		gaps := append([]int(nil), gstock.gapIDs[id]...)
		heal := false
		if r := gstock.rows[id]; r != nil && r.Needs["healing items"] > 0 {
			heal = true
		}
		gstock.Unlock()
		res[id] = autopilotDirs(id, gaps, heal, byGuild[id], now)
	}
	gduty.Lock()
	for id, ds := range res {
		gduty.auto[id] = ds
	}
	gduty.Unlock()
}

// ---------------------------------------------------------------- the bot side (b.mu held)

// dutyMaps (b.mu held): the maps where this member can work the direction safely, the monster, and the items it asks
// for. Uses the existing checks: fitFarms (drop maps safe for a long stay), soloMapOK / soloMapOKFor.
func (b *Bot) dutyMaps(d *GuildDirection, now time.Time) (maps []string, mob string, items []int, item string) {
	lv := b.BaseLvOr()
	farm := func(ids []int, loose bool) bool {
		for _, it := range ids {
			if glutted(it) && d.Kind != dirStock && d.Kind != dirGear {
				continue
			}
			ms, mb := b.fitFarms(pieceFarms(it, lv, 4), it)
			if len(ms) == 0 && loose {
				for _, f := range pieceFarms(it, lv, 8) {
					if b.soloMapOK(f.Map) {
						ms, mb = []string{f.Map}, f.Mob
						break
					}
				}
			}
			if len(ms) > 0 {
				maps, mob, items, item = ms, mb, []int{it}, itemName(it)
				return true
			}
		}
		return false
	}
	switch d.Kind {
	case dirStock, dirGear:
		if d.ItemID > 0 && !farm([]int{d.ItemID}, false) && d.Map != "" && b.soloMapOK(d.Map) {
			maps, items, item = []string{d.Map}, []int{d.ItemID}, itemName(d.ItemID)
		}
	case dirSupply:
		ids := healFarmItems
		if d.ItemID > 0 {
			ids = []int{d.ItemID}
		}
		farm(ids, true) // healing drops come from low monsters: any safe map (guildstock.go does the same)
		if len(items) > 0 && d.ItemID == 0 {
			items = append([]int(nil), healFarmItems...)
		}
	case dirWealth:
		if d.ItemID > 0 {
			farm([]int{d.ItemID}, false)
		} else if g := b.MoneyFarmGoal(now); g != nil && len(g.Maps) > 0 {
			// opportunity.go: the member's market opportunity (registered as a chaser, so bots spread over items)
			maps, mob, items, item = g.Maps, g.Mob, []int{g.ItemID}, g.Item
		}
	case dirCarry:
		if d.Map != "" && b.soloMapOKFor(d.Map, true) {
			maps = []string{d.Map}
		}
	case dirLevel:
		if d.Map != "" {
			if b.soloMapOK(d.Map) {
				maps = []string{d.Map}
			}
		} else {
			for _, g := range b.guideSpotsFor(b.powerLv()) {
				if b.soloMapOK(g.Map) && !containsStr(maps, g.Map) {
					maps = append(maps, g.Map)
				}
				if len(maps) == 3 {
					break
				}
			}
		}
	}
	return
}

// dutyPlanResult: what a plan check found
type dutyPlanResult struct {
	state string // fit | unfit | busy | skip
	why   string
	block *dutyBlock
	say   string
}

// dutyPlan (no lock held): one plan check of one member: the first direction that is safe and doable becomes its
// guild block (a farm goal of kind "guild").
func dutyPlan(s *dutySnap, dirs []*GuildDirection, owedMin int, now time.Time) dutyPlanResult {
	if s.partied {
		return dutyPlanResult{state: "busy", why: "in a party"}
	}
	b := s.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.Online || b.Dead || !b.invReady() || b.charName() != s.name {
		return dutyPlanResult{state: "skip"}
	}
	if b.trip != nil || b.kafra != nil || b.mkt != nil || b.tradeFrom != "" {
		return dutyPlanResult{state: "skip"}
	}
	in := dutyFitIn{Lv: b.BaseLvOr(), Class: className(b.Class), Healer: b.isHealer(), Vendor: b.charRole() == "vendor",
		Beginner: b.BaseLvOr() < beginnerLv()}
	why := "no direction"
	for _, d := range dirs {
		if ok, r := dirBasicFit(d, in); !ok {
			why = d.Kind + ": " + r
			continue
		}
		maps, mob, items, item := b.dutyMaps(d, now)
		if len(maps) == 0 {
			why = d.Kind + ": no safe map"
			continue
		}
		mins := max(dutyBlockMinMin, owedMin)
		k := &dutyBlock{DirID: d.ID, Kind: d.Kind, ItemID: d.ItemID, Item: item, Items: items, Maps: maps, Target: mins,
			Set: now, Until: now.Add(time.Duration(mins)*time.Minute + dutyBlockSlack)}
		if k.ItemID == 0 && len(items) == 1 {
			k.ItemID = items[0]
		}
		prev := b.fgoal
		if prev != nil && prev.Kind == "guild" {
			prev = nil
		}
		k.prev = prev
		g := &farmGoal{Kind: "guild", ItemID: k.ItemID, Item: item, Mob: mob, Map: maps[0], Maps: maps, Target: mins,
			Set: now, Until: k.Until}
		b.fgoal = g
		if b.Goal.Mode == "hunt" && strings.HasPrefix(b.Goal.Set, "auto") && !g.onMap(b.Goal.Map) && b.trip == nil {
			b.Goal, b.route, b.path = Goal{}, nil, nil // re-pick: chooseHuntingGround -> guildBlockHunt
		}
		what := d.Kind + " on " + maps[0]
		if item != "" {
			what = d.Kind + ": " + item + " on " + maps[0]
		}
		b.event("guild", "", fmt.Sprintf("guild duty block (%d min): %s", mins, what))
		say := dutyBlockLine(d, item, maps[0])
		return dutyPlanResult{state: "fit", block: k, say: styled(b.P.Typing, say)}
	}
	return dutyPlanResult{state: "unfit", why: why}
}

func dutyBlockLine(d *GuildDirection, item, m string) string {
	mt := mapTalk(m)
	switch d.Kind {
	case dirCarry:
		return pick("heading to "+mt+" to help the lowbies for a bit", "gonna help out at "+mt+" for the guild",
			"going to "+mt+", anyone need a hand there?")
	case dirLevel:
		return pick("doing the guild lvl run at "+mt, "ok heading to "+mt+" like we planned")
	case dirSupply:
		return pick("gonna farm some "+item+" for the guild pots", "getting "+item+" for the stock, "+mt,
			"herb run for the guild at "+mt)
	}
	if item == "" {
		return pick("doing my guild shift at "+mt, "ok doing guild stuff at "+mt+" for a while")
	}
	return pick("ill farm "+item+" for the guild on "+mt, "on it, "+item+" for the stock at "+mt,
		"doing my guild shift: "+item+" at "+mt)
}

// guildBlockStatus (b.mu held; lfg.go goalStatus hook): a guild block goal is ok while its block runs, met when its
// minutes are done, expired after its deadline, changed when the block is gone or duty was switched off.
func (b *Bot) guildBlockStatus(g *farmGoal, now time.Time) string {
	if !dutyOn() {
		return "changed"
	}
	gduty.Lock()
	m := gduty.byChar[b.charName()]
	var k *dutyBlock
	if m != nil {
		k = m.Block
	}
	st := "changed"
	switch {
	case k == nil || !k.Set.Equal(g.Set):
	case k.Done >= k.Target:
		st = "met"
	case !k.Ended.IsZero() && k.EndWhy == "done":
		st = "met"
	case !k.Ended.IsZero():
		st = "changed"
	case now.After(k.Until):
		st = "expired"
	default:
		st = "ok"
	}
	gduty.Unlock()
	return st
}

// guildBlockHunt (b.mu held; ai.go chooseHuntingGround hook): a solo member on a guild block hunts on the block's
// first map that is still safe for it. false = not on a block (the normal pickers run).
func (b *Bot) guildBlockHunt() bool {
	g := b.fgoal
	if g == nil || g.Kind != "guild" || !dutyOn() || partyOf(b) != nil {
		return false
	}
	for _, m := range g.Maps {
		if b.soloMapOKFor(m, true) {
			b.Goal, b.route = Goal{Mode: "hunt", Map: m, Set: "auto"}, nil
			if m != b.MapName {
				crowdNote(m, 1)
			}
			return true
		}
	}
	return false
}

// dutyGuildAnswer (b.mu held; guilds.go guildThink hook): a kicked bot refuses invitations during its cool-down and
// from its old guild for a week, and accepts the guild it chose. ok=false: not decided here.
func (b *Bot) dutyGuildAnswer(guild string) (accept, ok bool) {
	if !dutyOn() {
		return false, false
	}
	now := time.Now()
	gduty.Lock()
	defer gduty.Unlock()
	k := gduty.kicked[b.charName()]
	if k == nil || k.Stage == "joined" {
		return false, false
	}
	switch {
	case now.Before(k.CoolUntil):
		return false, true
	case strings.EqualFold(guild, k.FromName) && now.Sub(k.At) < dutyOldGuildBan:
		return false, true
	case k.Stage == "invited" && strings.EqualFold(guild, k.Target):
		return true, true
	}
	return false, false
}

// ---------------------------------------------------------------- manager

var dutyRng = rand.New(rand.NewSource(time.Now().UnixNano()))

func guildDutyManager(bots []*Bot) {
	dutyLoad()
	guildRankLoad()
	t := time.NewTicker(dutyTick)
	defer t.Stop()
	var autoAt, rankAt, saveAt, tradeAt time.Time
	for range t.C {
		now := time.Now()
		starveil := humanGuildInfo().Guild // file read: never under a lock
		snaps := dutySnapshot(bots, starveil)
		if now.Sub(autoAt) >= 10*time.Minute {
			autoAt = now
			dutyAutopilotRound(snaps, now)
		}
		exempt := map[*dutySnap]string{}
		for _, s := range snaps {
			if s.guild != "" {
				exempt[s] = s.exemptOf()
			}
		}
		endings, plans := dutyLedgerMinute(snaps, exempt, now)
		dutyApplyEndings(endings)
		dutyRunPlans(plans, now)
		if dutyOn() {
			dutyActions(snaps, now, starveil)
			dutyKickedTick(snaps, now, starveil)
			dutyDeliver(snaps, now)
		}
		if now.Sub(tradeAt) >= 5*time.Minute {
			tradeAt = now
			relationsFromTrades(now)
		}
		if now.Sub(rankAt) >= 10*time.Minute {
			rankAt = now
			guildRankRound(snaps, now, starveil)
		}
		if now.Sub(saveAt) >= 5*time.Minute {
			saveAt = now
			dutySave()
			guildRankSave()
		}
	}
}

type dutyEnding struct {
	b    *Bot
	set  time.Time
	prev *farmGoal
	why  string
	say  bool
}

type dutyPlanJob struct {
	s     *dutySnap
	dirs  []*GuildDirection
	owed  int
	guild string
}

// dutyLedgerMinute: one minute of the ledger for every online guild member (gduty held inside; no bot lock):
// online / duty minutes, day roll-over (standing), block progress, leavers and kicks confirmed. Returns the blocks
// that ended (their bots get their old goal back) and the members due for a plan check.
func dutyLedgerMinute(snaps []*dutySnap, exempt map[*dutySnap]string, now time.Time) ([]dutyEnding, []dutyPlanJob) {
	on := dutyOn()
	tn := tune()
	date := dutyDate(now)
	var endings []dutyEnding
	var plans []dutyPlanJob
	gduty.Lock()
	defer gduty.Unlock()
	cg := map[string]string{}
	for _, s := range snaps {
		cg[s.name] = s.guild
		old := gduty.byChar[s.name]
		if old != nil && old.Guild != s.guild {
			// out of the guild: a confirmed kick, or it left / moved on its own
			if old.KickPending && !old.KickSentAt.IsZero() {
				dutyKickConfirmedLocked(old, s, now)
			} else if s.guild != "" && now.Sub(old.Seen) < 3*time.Hour {
				relNote(old.Guild, s.guild, relPoach, s.name+" moved from "+old.Guild+" to "+s.guild)
			} else if s.guild == "" {
				gduty.left[s.name] = dutyLeft{old.Guild, now} // left on its own: a join elsewhere soon is a poach
			}
			if old.Block.active() {
				old.Block.Ended, old.Block.EndWhy = now, "left the guild"
				endings = append(endings, dutyEnding{b: s.b, set: old.Block.Set, prev: old.Block.prev, why: "left"})
			}
			delete(gduty.members, old.Guild+"|"+old.Char)
			delete(gduty.byChar, s.name)
			gduty.dirty = true
		}
		if k := gduty.kicked[s.name]; k != nil && s.guild != "" && k.Stage != "joined" {
			k.Stage, k.Joined, k.JoinedAt = "joined", s.guild, now
			dutyCount("kicked bots joined a new guild")
			gduty.dirty = true
		}
		if s.guild == "" {
			continue
		}
		if l, ok := gduty.left[s.name]; ok {
			if l.guild != s.guild && now.Sub(l.at) < 3*time.Hour {
				relNote(l.guild, s.guild, relPoach, s.name+" left "+l.guild+" and joined "+s.guild)
			}
			delete(gduty.left, s.name)
		}
		key := s.guild + "|" + s.name
		m := gduty.members[key]
		if m == nil {
			m = &dutyMember{Char: s.name, Account: s.account, Guild: s.guild, dutyStanding: dutyStanding{Standing: standGood}}
			gduty.members[key] = m
		}
		gduty.byChar[s.name] = m
		m.Seen = now
		m.Exempt = exempt[s]
		if m.Exempt != "" {
			m.Standing, m.Short, m.SinceWarn = standExempt, 0, 0
			m.WarnPending, m.KickPending = false, false
		} else if m.Standing == standExempt {
			m.Standing = standGood
		}
		// day roll-over: close every earlier day, then today's record
		d := m.today(date)
		if d == nil {
			dutyCloseDaysLocked(m, date, tn.GuildDutyMinPerDay, now)
			d = &dutyDay{Date: date}
			m.Days = append(m.Days, d)
			if len(m.Days) > dutyDaysKept {
				m.Days = m.Days[len(m.Days)-dutyDaysKept:]
			}
		}
		d.OnlineMin++
		if on {
			d.OnMin++
		}
		gduty.dirty = true
		// block progress
		if k := m.Block; k.active() {
			switch {
			case !s.blockSet.Equal(k.Set) && now.Sub(k.Set) > 2*time.Minute:
				k.Ended, k.EndWhy = now, "replaced"
				dutyCount("blocks replaced by another goal")
			case !on:
				k.Ended, k.EndWhy = now, "duty off"
				endings = append(endings, dutyEnding{b: s.b, set: k.Set, prev: k.prev, why: "off"})
			default:
				if !s.dead && containsStr(k.Maps, s.mapN) {
					k.Done++
					d.DutyMin++
				}
				if k.Done >= k.Target {
					k.Ended, k.EndWhy = now, "done"
					dutyCount("blocks done")
					endings = append(endings, dutyEnding{b: s.b, set: k.Set, prev: k.prev, why: "done", say: dutySayOKLocked(s.guild, now)})
				} else if now.After(k.Until) {
					k.Ended, k.EndWhy = now, "expired"
					dutyCount("blocks expired")
					endings = append(endings, dutyEnding{b: s.b, set: k.Set, prev: k.prev, why: "expired"})
				}
			}
		} else if !s.dead {
			// incidental duty: a member already hunting on a direction's own map (carry / level) counts too
			for _, dir := range activeDirsLocked(s.guild, now) {
				if dir.Map != "" && dir.Map == s.mapN && (dir.Kind == dirCarry || dir.Kind == dirLevel) {
					if ok, _ := dirBasicFit(dir, dutyFitIn{Lv: s.lv, Class: s.class, Healer: s.healer && dir.Kind != dirCarry}); ok {
						d.DutyMin++
						break
					}
				}
			}
		}
		// plan check due?
		if on && m.Exempt == "" && !m.Block.active() && !s.dead && !s.busy && d.DutyMin < tn.GuildDutyMinPerDay &&
			now.Sub(m.LastPlan) >= dutyPlanGap && len(plans) < dutyPlanPerTick {
			m.LastPlan = now
			dirs := activeDirsLocked(s.guild, now)
			cp := make([]*GuildDirection, len(dirs))
			for i, x := range dirs {
				c := *x
				cp[i] = &c
			}
			plans = append(plans, dutyPlanJob{s: s, dirs: cp, owed: tn.GuildDutyMinPerDay - d.DutyMin, guild: s.guild})
		}
	}
	gduty.charGuild = cg
	for c, l := range gduty.left {
		if now.Sub(l.at) > 3*time.Hour {
			delete(gduty.left, c)
		}
	}
	// forget members not seen for a week
	for k, m := range gduty.members {
		if now.Sub(m.Seen) > 8*24*time.Hour {
			delete(gduty.members, k)
			if gduty.byChar[m.Char] == m {
				delete(gduty.byChar, m.Char)
			}
		}
	}
	return endings, plans
}

// dutyCloseDaysLocked: closes every open day before `date` and moves the standing (gduty held)
func dutyCloseDaysLocked(m *dutyMember, date string, dutyMin int, now time.Time) {
	tn := tune()
	warnAt, kickAfter := dutyThresholds(tn.GuildDutyWarnDays, tn.GuildDutyKickDays, guildStrictness(m.Guild))
	for _, d := range m.Days {
		if d.Result != "" || d.Date >= date {
			continue
		}
		res := closeDay(d, dutyMin, dutyPlayedMin)
		dutyCount("days " + res)
		if m.Exempt != "" {
			continue
		}
		st, act := nextStanding(m.dutyStanding, res, warnAt, kickAfter)
		m.dutyStanding = st
		switch act {
		case actWarn:
			m.WarnPending = true
			dutyCount("warnings decided")
		case actKick:
			m.KickPending = true
			m.KickWhy = fmt.Sprintf("%d short days, %d after the warning", st.Short, st.SinceWarn)
			m.KickAt = now.Add(time.Duration(10+dutyRng.Intn(50)) * time.Minute)
			dutyCount("kicks decided")
		}
	}
}

// dutySayOKLocked: one block line per guild per dutySayGap (gduty held)
func dutySayOKLocked(guild string, now time.Time) bool {
	if now.Sub(gduty.guildSay[guild]) < dutySayGap {
		return false
	}
	gduty.guildSay[guild] = now
	return true
}

// dutyApplyEndings: a bot whose block ended gets its earlier goal back (its own lock only)
func dutyApplyEndings(es []dutyEnding) {
	for _, e := range es {
		b := e.b
		b.mu.Lock()
		if g := b.fgoal; g != nil && g.Kind == "guild" && g.Set.Equal(e.set) {
			b.fgoal = nil
			if e.prev != nil && time.Now().Before(e.prev.Until) {
				b.fgoal = e.prev
			}
			b.event("guild", "", "guild duty block ended ("+e.why+")")
			if e.say && b.GuildName != "" {
				b.guildSay(styled(b.P.Typing, pick("done with my guild shift o/", "ok guild stuff done for today",
					"finished my bit for the guild, back to my own stuff")))
			}
		}
		b.mu.Unlock()
	}
}

// dutyRunPlans: plan checks, one bot lock at a time; the results go into the ledger afterwards
func dutyRunPlans(jobs []dutyPlanJob, now time.Time) {
	for _, j := range jobs {
		r := dutyPlan(j.s, j.dirs, j.owed, now)
		if r.state == "skip" {
			continue
		}
		say := false
		gduty.Lock()
		m := gduty.byChar[j.s.name]
		if m != nil && m.Guild == j.guild {
			d := m.today(dutyDate(now))
			switch r.state {
			case "fit":
				m.Block = r.block
				m.LastFit = "block: " + r.block.Kind + " " + r.block.Item + " on " + strings.Join(r.block.Maps, ",")
				if d != nil {
					d.Fit++
				}
				dutyCount("blocks started")
				say = dutySayOKLocked(j.guild, now)
			case "unfit":
				m.LastFit = "nothing fits: " + r.why
				if d != nil {
					d.Unfit++
				}
				dutyCount("plan checks: nothing fits")
			case "busy":
				m.LastFit = "busy: " + r.why
				if d != nil {
					d.Busy++
				}
			}
			gduty.dirty = true
		}
		gduty.Unlock()
		if say && r.say != "" {
			b := j.s.b
			b.mu.Lock()
			if b.GuildName != "" {
				b.guildSay(r.say)
			}
			b.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------- warnings and kicks

// dutyActions: pending warnings (the leader in guild chat) and kicks (the leader expels), naturally paced
func dutyActions(snaps []*dutySnap, now time.Time, starveil string) {
	byName := map[string]*dutySnap{}
	for _, s := range snaps {
		byName[s.name] = s
	}
	type act struct {
		m      dutyMember
		kick   bool
		leader *Bot
	}
	var acts []act
	gduty.Lock()
	// server-wide kick window
	keep := gduty.kicks[:0]
	for _, t := range gduty.kicks {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	gduty.kicks = keep
	perHour := tune().GuildKicksPerHour
	for _, m := range gduty.members {
		if m.Exempt != "" || (!m.WarnPending && !m.KickPending) {
			continue
		}
		s := byName[m.Char]
		if s == nil || s.guild != m.Guild {
			continue // warnings and kicks happen while the member is online
		}
		l := guildLeader(m.Guild)
		if l == nil || m.Guild == humanGuildID {
			continue // Starveil: Ayla (OpenKore) is its master - shown in the API, never done by the engine
		}
		if m.WarnPending && now.Sub(gduty.guildWarn[m.Guild]) >= dutyWarnGap {
			gduty.guildWarn[m.Guild] = now
			m.WarnPending, m.WarnedAt = false, now
			gduty.dirty = true
			acts = append(acts, act{m: *m, leader: l})
			continue
		}
		if m.KickPending && now.After(m.KickAt) && m.KickSentAt.IsZero() && len(gduty.kicks) < perHour &&
			(len(gduty.kicks) == 0 || now.Sub(gduty.kicks[len(gduty.kicks)-1]) >= dutyKickMinGap) &&
			now.Sub(gduty.guildKick[m.Guild]) >= dutyGuildKickGap {
			gduty.kicks = append(gduty.kicks, now)
			gduty.guildKick[m.Guild] = now
			m.KickSentAt = now
			m.KickTries++
			gduty.dirty = true
			acts = append(acts, act{m: *m, kick: true, leader: l})
		}
	}
	// a kick that wasn't confirmed in 3 minutes is tried once more later, then given up for 6 hours
	for _, m := range gduty.members {
		if m.KickPending && !m.KickSentAt.IsZero() && now.Sub(m.KickSentAt) > 3*time.Minute {
			m.KickSentAt = time.Time{}
			if m.KickTries >= 2 {
				m.KickAt, m.KickTries = now.Add(6*time.Hour), 0
				dutyCount("kicks not confirmed (6 h pause)")
			} else {
				m.KickAt = now.Add(10 * time.Minute)
			}
			gduty.dirty = true
		}
	}
	gduty.Unlock()
	for _, a := range acts {
		s := byName[a.m.Char]
		if a.kick {
			go dutyExpel(a.leader, s, a.m)
		} else {
			go dutyWarn(a.leader, s, a.m)
		}
	}
}

// dutyWarn: the leader warns the member in guild chat; the member answers
func dutyWarn(l *Bot, s *dutySnap, m dutyMember) {
	reply, scripted := "", false
	l.mu.Lock()
	if l.Online && l.GuildName != "" && l.guildMaster {
		line := pick(fmt.Sprintf("%s, havent seen u do anything for the guild in days. ~2h a day pls or ill have to let u go", m.Char),
			fmt.Sprintf("hey %s, guild needs u too. do a guild shift today ok? otherwise im kicking", m.Char),
			fmt.Sprintf("%s pls help with the guild stuff, its been %d days. last warning", m.Char, m.Short))
		reply = styled(s.b.P.Typing, pick("sorry, will do", "ok ok, ill help more", "my bad, been busy. will do a shift", "yeah sorry"))
		if scripted = dutyChatWarn(l, m.Char, styled(l.P.Typing, line), reply, m.Short); !scripted { // guildchat.go
			l.guildSay(styled(l.P.Typing, line))
		}
		l.event("guild", m.Char, "duty warning to "+m.Char)
	}
	l.mu.Unlock()
	if !scripted {
		time.Sleep(time.Duration(4+crand(8)) * time.Second)
	}
	b := s.b
	b.mu.Lock()
	if b.Online && b.GuildName != "" {
		if !scripted {
			b.guildSay(reply)
		}
		b.remember("got warned by my guild leader for not helping the guild")
	}
	b.mu.Unlock()
}

// dutyExpel: the leader says why and expels the member in game (CZ_REQ_BAN_GUILD 0x015b:
// guild id.L, account id.L, char id.L, reason[40]; rAthena clif_parse_GuildExpulsion, master only)
func dutyExpel(l *Bot, s *dutySnap, m dutyMember) {
	l.mu.Lock()
	ok := l.Online && l.GuildName != "" && l.guildMaster && l.GuildID != 0 && l.GuildID == s.gid
	gid := l.GuildID
	wait := 0
	if ok {
		line := styled(l.P.Typing, pick(fmt.Sprintf("sorry %s, u havent helped the guild in ages. making room for active ppl", m.Char),
			fmt.Sprintf("removing %s, no guild work for days. no hard feelings", m.Char)))
		var used bool
		if wait, used = dutyChatKick(l, m.Char, line, m.KickWhy); !used { // guildchat.go
			l.guildSay(line)
		}
		l.event("guild", m.Char, "expelling "+m.Char+" (guild duty: "+m.KickWhy+")")
	}
	l.mu.Unlock()
	if !ok {
		return // retried after the 3-minute confirmation window
	}
	time.Sleep(time.Duration(wait+4+crand(6)) * time.Second)
	l.send(dutyExpelPacket(gid, s.aid, s.cid, "not helping the guild"))
	log.Printf("guild duty: %s expels %s from %s (%s)", l.charName(), m.Char, m.Guild, m.KickWhy)
}

// dutyExpelPacket: CZ_REQ_BAN_GUILD (0x015b, 54 bytes in recvpackets.txt)
func dutyExpelPacket(gid, aid, cid uint32, reason string) []byte {
	return newPkt(0x015b).u32(gid).u32(aid).u32(cid).str(reason, 40).b
}

// dutyKickConfirmedLocked: the member is out of the guild after our expel (gduty held)
func dutyKickConfirmedLocked(m *dutyMember, s *dutySnap, now time.Time) {
	cool := time.Duration(tune().GuildKickCooldownH) * time.Hour
	gduty.kicked[m.Char] = &dutyKicked{Char: m.Char, Account: m.Account, From: m.Guild, FromName: dutyGuildName(m.Guild, ""),
		At: now, CoolUntil: now.Add(cool), Stage: "cooldown"}
	dutyCount("kicks done")
	if len(gduty.kicked) > 200 {
		var old string
		for c, k := range gduty.kicked {
			if old == "" || k.At.Before(gduty.kicked[old].At) {
				old = c
			}
		}
		delete(gduty.kicked, old)
	}
	go func(b *Bot, from string) {
		b.mu.Lock()
		b.event("guild", "", "kicked from "+from+" for not doing guild duty; going solo for a while")
		b.mu.Unlock()
	}(s.b, m.Guild)
}

// ---------------------------------------------------------------- after the kick

// dutyKickedTick: cool-down, then the bot looks for a guild (rank, wealth, friends, room) and its leader invites it
func dutyKickedTick(snaps []*dutySnap, now time.Time, starveil string) {
	byName := map[string]*dutySnap{}
	for _, s := range snaps {
		byName[s.name] = s
	}
	type inv struct {
		k      dutyKicked
		s      *dutySnap
		target string
	}
	var todo []inv
	gduty.Lock()
	for _, k := range gduty.kicked {
		s := byName[k.Char]
		if s == nil || s.guild != "" || k.Stage == "joined" {
			continue
		}
		switch {
		case now.Before(k.CoolUntil):
		case k.Stage == "cooldown" || (k.Stage == "seeking" && now.After(k.NextTry)):
			k.Stage = "seeking"
			todo = append(todo, inv{k: *k, s: s})
		case k.Stage == "invited" && k.Invites >= 3 && now.Sub(k.InviteAt) > 3*time.Minute:
			k.Stage, k.Target, k.Invites, k.NextTry = "seeking", "", 0, now.Add(time.Hour)
			dutyCount("kicked bots: invitation not accepted")
		case k.Stage == "invited" && k.Invites < 3 && now.Sub(k.InviteAt) >= dutyInviteGap:
			todo = append(todo, inv{k: *k, s: s, target: k.Target})
		}
	}
	gduty.Unlock()
	for _, t := range todo {
		target := t.target
		if target == "" {
			target = dutyPickGuild(t.s, t.k.From, now)
			if target == "" {
				gduty.Lock()
				if k := gduty.kicked[t.k.Char]; k != nil {
					k.NextTry = now.Add(time.Hour)
				}
				gduty.Unlock()
				continue
			}
		}
		g := guildByID(target)
		l := guildLeader(target)
		if g == nil || l == nil {
			continue
		}
		l.mu.Lock()
		ok := l.Online && l.guildMaster && l.GuildID != 0
		l.mu.Unlock()
		if !ok {
			continue
		}
		gduty.Lock()
		if k := gduty.kicked[t.k.Char]; k != nil {
			k.Stage, k.Target, k.InviteAt = "invited", g.Name, now
			k.Invites++
			gduty.dirty = true
		}
		dutyCount("kicked bots: invitations")
		gduty.Unlock()
		go func(lead, target *Bot) {
			time.Sleep(time.Duration(2+crand(6)) * time.Second)
			lead.send(newPkt(0x0168).u32(target.aid).u32(lead.aid).u32(lead.cid).b) // CZ_REQ_JOIN_GUILD
		}(l, t.s.b)
	}
}

// dutyPickGuild: the guild a guild-less bot asks to join: room left, not its old guild, best joinPreference
// (rank, wealth, friends); a weighted pick among the top three. No bot lock held.
func dutyPickGuild(s *dutySnap, from string, now time.Time) string {
	places, wealth, n := guildRankView()
	if n == 0 {
		return ""
	}
	richest := 1
	for _, w := range wealth {
		richest = max(richest, w)
	}
	members := map[string]int{}
	guildRoster.Range(func(_, v interface{}) bool {
		if g := guildByName[strings.ToLower(v.(string))]; g != nil {
			members[g.ID]++
		}
		return true
	})
	friendsIn := map[string]int{}
	for _, f := range friendsOf(s.name, friendTierFriend) {
		if o := botByCharName(f.Name); o != nil {
			if g := guildByName[strings.ToLower(o.GuildName)]; g != nil { // string snapshot without o.mu, as guildOfAccount
				friendsIn[g.ID]++
			}
		}
	}
	type cand struct {
		id string
		v  float64
	}
	var cs []cand
	for _, g := range guildDefs {
		if g.ID == from {
			continue
		}
		room := members[g.ID] < guildMaxMembers(g.Name)
		place := places[g.ID]
		if place <= 0 {
			place = n // not ranked yet: last
		}
		v := joinPreference(place, n, float64(wealth[g.ID])/float64(richest), friendsIn[g.ID], room)
		if v > 0 {
			cs = append(cs, cand{g.ID, v})
		}
	}
	if len(cs) == 0 {
		return ""
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].v > cs[j].v })
	if len(cs) > 3 {
		cs = cs[:3]
	}
	sum := 0.0
	for _, c := range cs {
		sum += c.v
	}
	r := dutyRng.Float64() * sum
	for _, c := range cs {
		if r -= c.v; r <= 0 {
			return c.id
		}
	}
	return cs[0].id
}

// ---------------------------------------------------------------- loot of a block -> guild stock

// dutyDeliver: requested items from a guild block go to the guild trader by RODEX (one mail of up to 4 stacks per
// member per 10 min, only stacks worth the 100z-per-item stamp four times over). The bot keeps everything else.
func dutyDeliver(snaps []*dutySnap, now time.Time) {
	type job struct {
		s     *dutySnap
		items []int
	}
	var jobs []job
	gduty.Lock()
	for _, s := range snaps {
		m := gduty.byChar[s.name]
		if m == nil || m.Block == nil || len(m.Block.Items) == 0 || m.Guild != s.guild || now.Sub(m.LastDeliver) < dutyDeliverGap ||
			(!m.Block.active() && now.Sub(m.Block.Ended) > dutyRecentBlock) {
			continue
		}
		m.LastDeliver = now
		jobs = append(jobs, job{s, append([]int(nil), m.Block.Items...)})
		if len(jobs) >= 10 {
			break
		}
	}
	gduty.Unlock()
	for _, j := range jobs {
		to := guildTrader(j.s.guild)
		if to == "" || strings.EqualFold(to, j.s.name) {
			continue
		}
		want := map[int]bool{}
		for _, id := range j.items {
			want[id] = true
		}
		prices := map[int]int{}
		for id := range want {
			prices[id] = priceOf(id)
		}
		b := j.s.b
		b.mu.Lock()
		if b.Dead || b.mkt != nil || b.trip != nil || b.kafra != nil || b.tradeFrom != "" || b.target != 0 ||
			b.underAttack(now) || b.mailBusy() || !b.invReady() || b.charName() != j.s.name {
			b.mu.Unlock()
			continue
		}
		var idx []int
		names := []string{}
		for i, it := range b.inv {
			if it == nil || it.Equipped || it.Count <= 0 || !want[it.ItemID] || it.Count*prices[it.ItemID] < 4*mailItemFee {
				continue
			}
			idx = append(idx, i)
		}
		sort.Ints(idx)
		if len(idx) > 4 {
			idx = idx[:4]
		}
		if len(idx) == 0 || b.Zeny < mailItemFee*len(idx)+200 {
			b.mu.Unlock()
			continue
		}
		for _, i := range idx {
			names = append(names, fmt.Sprintf("%dx %s", b.inv[i].Count, itemName(b.inv[i].ItemID)))
		}
		what := strings.Join(names, ", ")
		mo := &mailOut{To: to, Title: "for the guild stock", Body: "from my guild shift", Items: idx, stage: 1, at: now,
			onDone: func(ok bool, note string) {
				if !ok {
					return
				}
				b.event("guild", to, "guild duty: mailed "+what+" to the guild stock ("+to+")")
				gduty.Lock()
				dutyCount("deliveries to the guild stock")
				gduty.Unlock()
			}}
		mailOuts.Store(b, mo)
		go b.send(newPkt(0x0a08).str(to, 24).b)
		b.mu.Unlock()
	}
}

// ---------------------------------------------------------------- persistence

type dutyFile struct {
	Saved    time.Time                    `json:"saved"`
	Dirs     map[string][]*GuildDirection `json:"directions"`
	Members  []*dutyMember                `json:"members"`
	Kicked   []*dutyKicked                `json:"kicked"`
	Counters map[string]int               `json:"counters"`
}

func dutyFilePath() string { return filepath.Join(*dataDir, "guild_duty.json") }

func dutyLoad() {
	raw, err := os.ReadFile(dutyFilePath())
	gduty.Lock()
	defer gduty.Unlock()
	gduty.loaded = true
	if err != nil {
		return
	}
	var f dutyFile
	if json.Unmarshal(raw, &f) != nil {
		log.Printf("guild duty: %s does not parse, starting empty", dutyFilePath())
		return
	}
	for g, ds := range f.Dirs {
		gduty.dirs[g] = ds
	}
	for _, m := range f.Members {
		if m.Block.active() {
			m.Block.Ended, m.Block.EndWhy = time.Now(), "restart" // the goal (fgoal) did not survive the restart
		}
		gduty.members[m.Guild+"|"+m.Char] = m
		gduty.byChar[m.Char] = m
	}
	for _, k := range f.Kicked {
		gduty.kicked[k.Char] = k
	}
	for k, v := range f.Counters {
		gduty.counters[k] = v
	}
}

// dutySave: marshal under the lock, write after releasing it (never file I/O under a lock)
func dutySave() {
	gduty.Lock()
	if !gduty.dirty || !gduty.loaded {
		gduty.Unlock()
		return
	}
	f := dutyFile{Saved: time.Now(), Dirs: gduty.dirs, Counters: gduty.counters}
	for _, m := range gduty.members {
		f.Members = append(f.Members, m)
	}
	for _, k := range gduty.kicked {
		f.Kicked = append(f.Kicked, k)
	}
	raw, err := json.Marshal(f)
	gduty.dirty = false
	gduty.Unlock()
	if err == nil {
		writeFileAtomic(dutyFilePath(), raw)
	}
}
