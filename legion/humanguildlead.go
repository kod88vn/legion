package main

// Ayla as Starveil's guild leader (owner 2026-11-14: "Make sure she also functions as a guild leader, do resource
// planning and growing members, etc. Recruit the right and balance jobs"). The engine does the arithmetic, her mind
// decides and acts:
//   GET  /api/human-guild            (main; merges the core instance) the guild at a glance - members with role, level,
//        map, party, zeny, next gear piece; role gaps; the week's goal and its progress; the guild alchemist; and
//        "actions": concrete next steps, each with the exact command (invite, fund a member's gear, fund the potion
//        guide, form a member party on a map, carry a member who fell behind).
//   POST /api/human-guild/fund {"to","amount","why"}  guild mates (bots of both instances) mail the zeny to a member
//        (RODEX, 2% fee; each gives at most 5,000z and half its spare, once an hour) - the guild pool for gear and the
//        Potion Creation Guide. Returns what was pledged; the mails go out within seconds.
// Guild storage: rAthena opens it only with the guild skill Guild Storage Expansion, which a new (level 1) guild
// doesn't have - members keep using their own Kafra storage until the guild levels (shown as a note).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// humanGuildID: guildOfAccount's id for Starveil members and Ayla's party friends (alchemy.go guild alchemist and
// guild fund, economy guild-mate matching, parcels) - they leave their old persona guild (guild01...) for it.
const humanGuildID = "human-guild"

const (
	guildWeekLen      = 7 * 24 * time.Hour
	guildWeekLvGain   = 3 // the week's goal: every member +3 base levels
	guildFundEach     = 5000
	guildFundCooldown = time.Hour
	guildFundBuffer   = 2000 // a donor keeps its own next gear piece's cost + this
)

// humanGuildMember (b.pin read without b.mu like guildOfAccount reads GuildName: a pointer/string snapshot is fine)
func humanGuildMember(b *Bot) bool {
	if b.pin != nil && b.pin.Human != "" {
		return true
	}
	n := b.GuildName
	return n != "" && isHumanGuild(n)
}

type guildMemberInfo struct {
	Name     string    `json:"name"`
	Class    string    `json:"class"`
	Role     string    `json:"role"`
	Level    int       `json:"level"`
	Online   bool      `json:"online"`
	Map      string    `json:"map,omitempty"`
	Party    string    `json:"party,omitempty"`
	Friend   bool      `json:"party_friend,omitempty"`
	InGuild  bool      `json:"in_guild"`
	Zeny     int       `json:"zeny"`
	Spare    int       `json:"spare"` // zeny it can put into gear (potion reserve kept)
	Next     *gearStep `json:"gear_next,omitempty"`
	Weak     float64   `json:"gear_per_level"` // gear score per level (lower = weaker)
	Brewer   bool      `json:"brewer,omitempty"`
	Trader   bool      `json:"vendor,omitempty"` // Merchant-line vendor with Vending: the guild's trader candidate
	Guide    bool      `json:"potion_guide,omitempty"`
	Instance string    `json:"instance,omitempty"`
}

var guildFundNextAt = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// localGuildMembers: this instance's members of the human's guild (her party friends count before they joined).
func localGuildMembers(bots []*Bot, guild string) []guildMemberInfo {
	var out []guildMemberInfo
	for _, b := range bots {
		gname := b.GuildName
		if gname == "" {
			if v, ok := guildRoster.Load(b.P.Account); ok {
				gname = v.(string)
			}
		}
		friend := b.pin != nil && b.pin.Human != ""
		if !(guild != "" && strings.EqualFold(gname, guild)) && !friend {
			continue
		}
		pname := ""
		if p := partyOf(b); p != nil {
			pname = p.Name
		}
		b.mu.Lock()
		m := guildMemberInfo{Name: b.charName(), Class: className(b.Class), Role: guildRole(b.Class), Level: b.BaseLvOr(),
			Online: b.Online && b.MapName != "", Map: b.MapName, Party: pname, Friend: friend, Zeny: b.Zeny,
			InGuild: guild != "" && strings.EqualFold(gname, guild)}
		if m.Online {
			m.Spare = b.giftableZeny()
			m.Next = b.gearStepView()
			m.Weak = b.gearWeakness()
			m.Brewer = b.canBrew()
			m.Trader = b.isVendorTrader() || b.isHumanGuildTrader(time.Now()) // a fighter-trader on shop shifts counts too
			m.Guide = b.countItem(itemPotionGuide) > 0
		}
		b.mu.Unlock()
		if coreInstance() {
			m.Instance = "core"
		}
		out = append(out, m)
	}
	return out
}

type guildAction struct {
	Kind string `json:"kind"` // found / invite / fund / guide / party / carry / potions / week
	What string `json:"what"`
	Cmd  string `json:"cmd,omitempty"`
}

type guildWeek struct {
	Start   time.Time      `json:"start"`
	StartLv map[string]int `json:"start_lv"`
}

var guildWeekState = struct {
	sync.Mutex
	w *guildWeek
}{}

func guildWeekFile() string { return *dataDir + "/human-guild-week.json" }

// guildWeekNow: the current week (a new one every 7 days, starting levels noted as members are first seen).
func guildWeekNow(ms []guildMemberInfo, now time.Time) *guildWeek {
	guildWeekState.Lock()
	defer guildWeekState.Unlock()
	w := guildWeekState.w
	if w == nil {
		w = &guildWeek{}
		if raw, err := os.ReadFile(guildWeekFile()); err == nil {
			json.Unmarshal(raw, w)
		}
	}
	changed := false
	if w.Start.IsZero() || now.Sub(w.Start) > guildWeekLen {
		w = &guildWeek{Start: now, StartLv: map[string]int{}}
		changed = true
	}
	if w.StartLv == nil {
		w.StartLv = map[string]int{}
	}
	for _, m := range ms {
		if _, ok := w.StartLv[m.Name]; !ok && m.Level > 0 {
			w.StartLv[m.Name] = m.Level
			changed = true
		}
	}
	guildWeekState.w = w
	if changed {
		if raw, err := json.MarshalIndent(w, "", " "); err == nil {
			writeFileAtomic(guildWeekFile(), raw)
		}
	}
	c := *w
	return &c
}

// guildPlan: the leader's next steps from the members, recruits and role gaps (pure: tested).
func guildPlan(guild, human string, ms []guildMemberInfo, recruits []guildRecruit, gaps map[string]int, week *guildWeek, now time.Time) ([]guildAction, map[string]any) {
	var acts []guildAction
	if guild == "" {
		acts = append(acts, guildAction{"found", "found the guild Starveil (no Emperium needed)", "ayla.sh guild-create Starveil"})
	}
	if len(recruits) > 0 {
		var ns []string
		for _, r := range recruits {
			ns = append(ns, fmt.Sprintf("%s (%s lv%d, %s)", r.Name, r.Class, r.Level, r.Why))
		}
		acts = append(acts, guildAction{"invite", "invite " + strings.Join(ns, "; "), fmt.Sprintf("ayla.sh recruit %d", len(recruits))})
	}
	// the guild pool: what the online bot members can give (half their spare, at most 5,000z each)
	pool := 0
	for _, m := range ms {
		pool += fundShare(m)
	}
	// the guild alchemist: the brewer with the guide, else the highest-level member who can brew
	var alch *guildMemberInfo
	for i := range ms {
		m := &ms[i]
		if !m.Brewer {
			continue
		}
		if alch == nil || m.Guide && !alch.Guide || m.Guide == alch.Guide && m.Level > alch.Level {
			alch = m
		}
	}
	alchemist := map[string]any{}
	if alch != nil {
		alchemist = map[string]any{"name": alch.Name, "level": alch.Level, "has_guide": alch.Guide, "zeny": alch.Zeny}
		if !alch.Guide {
			need := guideCost + 3000 - alch.Zeny
			if need > 0 {
				acts = append(acts, guildAction{"guide", fmt.Sprintf("%s needs %s more for the Potion Creation Guide (guild potions, SP potions too); guild pool now ~%s",
					alch.Name, fmtZ(need), fmtZ(pool)), fmt.Sprintf("ayla.sh guild-fund \"%s\" %d \"Potion Creation Guide\"", alch.Name, min(need, max(pool, 300))/100*100+100)})
			} else {
				acts = append(acts, guildAction{"guide", alch.Name + " has the money for the Potion Creation Guide: it buys it in Aldebaran (alde_alche) on its own", ""})
			}
		} else {
			acts = append(acts, guildAction{"potions", alch.Name + " brews the guild's potions: members feed it Blue/White/Yellow Herbs and buy its potions (SP potions first for casters/priests)", ""})
		}
	}
	// the guild's merchant trader (guildstock.go picks it from the guild's vendors with an open Prontera shop)
	hasTrader := false
	for _, m := range ms {
		if m.InGuild && m.Trader {
			hasTrader = true
		}
	}
	if guild != "" && !hasTrader {
		acts = append(acts, guildAction{"trader", "no merchant trader yet: recruit a vendor Merchant/Blacksmith/Alchemist with an open south-gate shop (recruits marked vendor come first) - it sells the guild's surplus and buys what the guild lacks", "ayla.sh recruit 1"})
	}
	// growth: rAthena's cap is 16 + 6 x Guild Extension level (GD_EXTENSION, up to 76); guild skill points come from
	// guild levels, which come from the EXP tax members pay. A full guild plans the tax, then Guild Extension.
	inG := 1
	for _, m := range ms {
		if m.InGuild {
			inG++
		}
	}
	if gmax := humanGuildMax(); guild != "" && inG >= gmax {
		acts = append(acts, guildAction{"growth", fmt.Sprintf("Starveil is full (%d/%d): the EXP tax (ayla.sh guild-tax) levels the guild; each level gives a guild skill point and gskill auto spends it on Guild Extension (+6 members a level). Check with ayla.sh guild-info; recruit again when max_members grows", inG, gmax), "ayla.sh guild-info"})
	}
	// gear: the weakest online member whose next piece is shop-bought and short, if the pool covers the gap
	var cand []guildMemberInfo
	for _, m := range ms {
		if m.Online && m.Next != nil && m.Next.Shop != "" && m.Next.Cost > m.Next.Have && (alch == nil || m.Name != alch.Name || alch.Guide) {
			cand = append(cand, m)
		}
	}
	sort.Slice(cand, func(i, j int) bool { return cand[i].Weak < cand[j].Weak })
	for _, m := range cand {
		short := m.Next.Cost - m.Next.Have
		if short <= pool-fundShare(m) {
			acts = append(acts, guildAction{"fund", fmt.Sprintf("%s (%s lv%d, the weakest geared) is %s short of its %s (%s)",
				m.Name, m.Class, m.Level, fmtZ(short), m.Next.Item, fmtZ(m.Next.Cost)), fmt.Sprintf("ayla.sh guild-fund \"%s\" %d \"%s\"", m.Name, (short+99)/100*100, m.Next.Item)})
			break
		}
	}
	// carry: members far below the guild's median level (they need a party at their level, or a carry)
	var lvs []int
	for _, m := range ms {
		lvs = append(lvs, m.Level)
	}
	sort.Ints(lvs)
	if len(lvs) > 0 {
		med := lvs[len(lvs)/2]
		for _, m := range ms {
			if m.Online && m.Level < med-15 && !m.Friend {
				acts = append(acts, guildAction{"carry", fmt.Sprintf("%s (lv%d) is far below the guild (median lv%d): put it in a member party at its level or carry it", m.Name, m.Level, med), ""})
			}
		}
	}
	// the week's goal
	gained, done := 0, 0
	for _, m := range ms {
		if s, ok := week.StartLv[m.Name]; ok {
			d := m.Level - s
			gained += max(0, d)
			if d >= guildWeekLvGain {
				done++
			}
		}
	}
	weekView := map[string]any{"goal": fmt.Sprintf("every member +%d base levels this week, in a party on a map that fits its level; the alchemist owns the Potion Creation Guide; every member's next gear piece bought", guildWeekLvGain),
		"since": week.Start.UTC().Format("2006-01-02 15:04"), "ends_in_h": int(week.Start.Add(guildWeekLen).Sub(now).Hours()),
		"members_done": done, "members": len(ms), "levels_gained": gained,
		"later": "boss prep: once every member is fully geared for its build (RULES.md 2), plan a guild boss hunt (legionctl boss-check)"}
	acts = append(acts, guildAction{"week", fmt.Sprintf("week goal: %d of %d members +%d levels (%d levels gained so far)", done, len(ms), guildWeekLvGain, gained), ""})
	if len(gaps) > 0 {
		var g []string
		for _, t := range guildRoleTarget {
			if n := gaps[t.Role]; n > 0 {
				g = append(g, fmt.Sprintf("%d %s", n, t.Role))
			}
		}
		acts = append(acts, guildAction{"recruit-line", "still missing: " + strings.Join(g, ", ") + " - one #global recruiting line every 20-30 min at most", ""})
	}
	return acts, map[string]any{"alchemist": alchemist, "week": weekView, "pool": pool}
}

// memberParties: online main-instance members without a party, grouped by level (PT35 band: partyLvOK) into parties of 3-5
// with a map that fits them all (guildlead.go bestPartySpot). The leader is the highest level.
func memberParties(bots []*Bot, guild string) []guildAction {
	var acts []guildAction
	for _, g := range memberPartyGroups(bots, guild) {
		var names []string
		for _, n := range g.names {
			names = append(names, fmt.Sprintf("%q", n))
		}
		acts = append(acts, guildAction{"party", fmt.Sprintf("form a Starveil party on %s: %s", g.spot, strings.Join(names, ", ")),
			fmt.Sprintf("python tools/legionctl.py party %s --map %s", strings.Join(names, " "), g.spot)})
	}
	return acts
}

// memberPartyGroup: free Starveil members who fit one party (members[0], the highest level, leads) and its map
type memberPartyGroup struct {
	members []*Bot
	names   []string
	spot    string
}

// memberPartyGroups: memberParties' grouping (also formed by starveillead.go). Free = online, in the guild, no
// party, not on a guild block, a town errand, a vendor shop or dead; never Ayla's party friends.
func memberPartyGroups(bots []*Bot, guild string) []memberPartyGroup {
	type fb struct {
		b    *Bot
		lv   int
		name string
	}
	var free []fb
	for _, b := range bots {
		if guild == "" || (b.pin != nil && b.pin.Human != "") || partyOf(b) != nil {
			continue
		}
		b.mu.Lock()
		ok := b.Online && b.MapName != "" && strings.EqualFold(b.GuildName, guild) && !b.Dead && b.trip == nil &&
			b.mkt == nil && b.kafra == nil && !(b.vend != nil && b.vend.Opened) && !(b.fgoal != nil && b.fgoal.Kind == "guild")
		lv, name := b.BaseLvOr(), b.charName()
		b.mu.Unlock()
		if ok && !b.alwaysOnline() {
			free = append(free, fb{b, lv, name})
		}
	}
	// free is sorted strongest first: the lead is the group's strongest, so checking each candidate against it bounds the
	// spread and the exp rule for every member. A change to this sort order breaks that pairwise check.
	sort.Slice(free, func(i, j int) bool { return free[i].lv > free[j].lv })
	var out []memberPartyGroup
	for len(free) >= 3 {
		g := []fb{free[0]}
		rest := []fb{}
		for _, x := range free[1:] {
			if len(g) < 5 && partyLvOK(x.lv, free[0].lv, 0) { // PT35 band (was 10 under the leader)
				g = append(g, x)
			} else {
				rest = append(rest, x)
			}
		}
		free = rest
		if len(g) < 3 {
			continue
		}
		var mg memberPartyGroup
		for _, x := range g {
			mg.members = append(mg.members, x.b)
			mg.names = append(mg.names, x.name)
		}
		if mg.spot = bestPartySpot(mg.members); mg.spot == "" {
			continue
		}
		out = append(out, mg)
	}
	return out
}

// guildFundLocal: this instance's members mail zeny to a member (RODEX). Returns the amount pledged and the donors.
func guildFundLocal(bots []*Bot, to string, amount int, why string, now time.Time) (int, []string) {
	// Donors give from SURPLUS only (bug 14:46-14:58: Zen.fox, saving 9.5k for its gear-turn Staff, gave 800z and
	// 1,200z; Silent rogue, short of its potion guide, gave 900z): never a member of a party with an active gear
	// turn (the turn member and everyone pooling for it), never an alchemist still short of the guide, and only zeny
	// above its own next gear piece + guildFundBuffer. Traders / vendors (shop income) first, then the biggest surplus.
	type donor struct {
		b       *Bot
		name    string
		surplus int
		trader  bool
	}
	var cands []donor
	for _, b := range bots {
		if !humanGuildMember(b) {
			continue
		}
		if p := partyOf(b); p != nil {
			if turn, _ := gearPoolView(p.Name); turn != "" {
				continue // its party is pooling for a member's gear turn
			}
		}
		b.mu.Lock()
		name := b.charName()
		ok := b.Online && !strings.EqualFold(name, to) && b.mkt == nil && b.trip == nil && b.kafra == nil && !b.Dead &&
			!b.guideSaving() && !b.mailBusy() && !(b.canBrew() && b.countItem(itemPotionGuide) == 0)
		surplus := 0
		if ok {
			surplus = b.giftableZeny() // never its raised gear-turn zeny
			if st := b.gearStepView(); st != nil && st.Cost > 0 {
				surplus -= st.Cost + guildFundBuffer // keeps what its own next gear piece needs
			}
		}
		trader := b.charRole() == "vendor" || b.isHumanGuildTrader(now)
		b.mu.Unlock()
		guildFundNextAt.Lock()
		cool := now.Before(guildFundNextAt.m[name])
		guildFundNextAt.Unlock()
		if !ok || cool || surplus < 600 {
			continue
		}
		cands = append(cands, donor{b, name, surplus, trader})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].trader != cands[j].trader {
			return cands[i].trader
		}
		return cands[i].surplus > cands[j].surplus
	})
	pledged := 0
	var donors []string
	for _, d := range cands {
		if pledged >= amount {
			break
		}
		b := d.b
		give := min(guildFundEach, d.surplus/2, amount-pledged) / 100 * 100
		if give < 300 {
			continue
		}
		b.mu.Lock()
		if !b.Online || b.Zeny < mailCost(give)+1500 {
			b.mu.Unlock()
			continue
		}
		typing, rName, g, w := b.P.Typing, to, give, why
		started := b.mailZeny(rName, g, "guild: "+w, "from the Starveil pool", func(ok bool, note string) {
			if !ok {
				return
			}
			econCount("human guild fund: mailed")
			b.event("market", rName, fmt.Sprintf("mailed %dz to %s (Starveil pool: %s)", g, rName, w))
			go func() {
				time.Sleep(time.Duration(2+crand(3)) * time.Second)
				b.whisper(rName, styled(typing, pick(fmt.Sprintf("mailed u %dz for ur %s", g, w), fmt.Sprintf("guild money sent, %dz, check ur mail", g))))
			}()
		})
		b.mu.Unlock()
		if !started {
			continue
		}
		guildFundNextAt.Lock()
		guildFundNextAt.m[d.name] = now.Add(guildFundCooldown)
		guildFundNextAt.Unlock()
		pledged += give
		donors = append(donors, fmt.Sprintf("%s %dz", d.name, give))
	}
	return pledged, donors
}

// registerHumanGuildLead: /api/human-guild and /api/human-guild/fund (both instances; main merges the core).
func registerHumanGuildLead(mux *http.ServeMux, bots []*Bot) {
	registerStarveilOrders(mux) // starveillead.go: /api/human-guild/orders for Ayla's plugin
	mux.HandleFunc("/api/human-guild/fund", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST {\"to\",\"amount\",\"why\"}", 405)
			return
		}
		var body struct {
			To     string `json:"to"`
			Amount int    `json:"amount"`
			Why    string `json:"why"`
			Local  bool   `json:"local"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.To == "" || body.Amount < 300 {
			http.Error(w, "need to and amount >= 300", 400)
			return
		}
		if body.Why == "" {
			body.Why = "gear"
		}
		body.Amount = min(body.Amount, 150000)
		pledged, donors := guildFundLocal(bots, body.To, body.Amount, body.Why, time.Now())
		if !body.Local && pledged < body.Amount {
			if base := coreAPIURL(); base != "" {
				raw, _ := json.Marshal(map[string]any{"to": body.To, "amount": body.Amount - pledged, "why": body.Why, "local": true})
				if resp, err := coreClient.Post(base+"/api/human-guild/fund", "application/json", strings.NewReader(string(raw))); err == nil {
					var got struct {
						Pledged int      `json:"pledged"`
						Donors  []string `json:"donors"`
					}
					if json.NewDecoder(resp.Body).Decode(&got) == nil {
						pledged += got.Pledged
						donors = append(donors, got.Donors...)
					}
					resp.Body.Close()
				}
			}
		}
		if donors == nil {
			donors = []string{}
		}
		note := "mails go out within seconds (2% fee); each donor gives once an hour"
		if pledged < body.Amount {
			note = fmt.Sprintf("only %s of %s pledged (members online with spare zeny: see /api/human-guild); try again in an hour or fund the rest yourself", fmtZ(pledged), fmtZ(body.Amount))
		}
		writeJSON(w, map[string]any{"to": body.To, "asked": body.Amount, "pledged": pledged, "donors": donors, "note": note})
	})
	mux.HandleFunc("/api/human-guild", func(w http.ResponseWriter, r *http.Request) {
		info := humanGuildInfo()
		guild := info.Guild
		ms := localGuildMembers(bots, guild)
		if r.URL.Query().Get("local") == "1" {
			if ms == nil {
				ms = []guildMemberInfo{}
			}
			writeJSON(w, ms)
			return
		}
		if base := coreAPIURL(); base != "" {
			if resp, err := coreClient.Get(base + "/api/human-guild?local=1"); err == nil {
				var core []guildMemberInfo
				if json.NewDecoder(resp.Body).Decode(&core) == nil {
					ms = append(ms, core...)
				}
				resp.Body.Close()
			}
		}
		now := time.Now()
		hs, _ := humanStatusOf(info.Human)
		job := hs.JobID
		if job == 0 {
			job = info.Job
		}
		// recruits (same pick as /api/guild-recruits, without marking)
		var inGuild []guildMemberRow // friends not in the guild yet are recruits, not members
		for _, m := range ms {
			if m.InGuild {
				k, _ := recruitJob(classIDByName(m.Class))
				inGuild = append(inGuild, guildMemberRow{Name: m.Name, Class: m.Class, Level: m.Level, Trader: m.Trader, key: k})
			}
		}
		minLv := max(40, hs.BaseLevel-25)
		local := localGuildScan(bots, guild, minLv, now)
		cands := local.Candidates
		if base := coreAPIURL(); base != "" {
			if resp, err := coreClient.Get(base + "/api/guild-recruits?local=1&guild=" + urlQuery(guild) + "&min_lv=" + fmt.Sprint(minLv)); err == nil {
				var core guildRecruitsLocal
				if json.NewDecoder(resp.Body).Decode(&core) == nil {
					for _, c := range core.Candidates {
						c.key, c.second = recruitJob(classIDByName(c.Class))
						cands = append(cands, c)
					}
				}
				resp.Body.Close()
			}
		}
		var recruits []guildRecruit
		gaps := map[string]int{}
		if guild != "" {
			recruits, gaps = pickGuildRecruits(inGuild, cands, job, hs.BaseLevel, 3)
		}
		week := guildWeekNow(ms, now)
		acts, extra := guildPlan(guild, info.Human, ms, recruits, gaps, week, now)
		acts = append(acts, memberParties(bots, guild)...)
		acts = append(loyaltyActions(guild), acts...) // loyalty.go: keep at-risk members, answer help requests first
		roles := map[string]int{}
		if r := guildRole(job); r != "" {
			roles[r]++
		}
		size := 1
		for _, m := range ms {
			if m.InGuild {
				if m.Trader && roles["trader"] == 0 {
					roles["trader"]++
				} else {
					roles[m.Role]++
				}
				size++
			}
		}
		writeJSON(w, map[string]any{
			"guild": guild, "leader": info.Human, "founded": guild != "", "members": ms, "size": size, "max": humanGuildMax(),
			"roles": roles, "role_targets": guildRoleTarget, "role_gaps": gaps, "recruits": recruits,
			"alchemist": extra["alchemist"], "week": extra["week"], "pool": extra["pool"], "actions": acts,
			"storage": "guild storage needs the guild skill Guild Storage Expansion (not at guild level 1): members keep their own Kafra storage for now",
		})
	})
}

// loyaltyActions: the friendship agent's view of Starveil (loyalty.go) as leader actions - members at risk of leaving
// (with its suggested keep gift) and open help requests from Starveil members Ayla can answer. Starveil members may
// leave like anyone; only her party friends are fixed.
func loyaltyActions(guild string) []guildAction {
	if guild == "" {
		return nil
	}
	loyalty.Lock()
	risk := append([]atRisk(nil), loyalty.risk...)
	var open []helpReq
	for _, h := range loyalty.help {
		if h.State == "open" && strings.EqualFold(h.Guild, guild) {
			open = append(open, *h)
		}
	}
	loyalty.Unlock()
	var acts []guildAction
	for _, r := range risk {
		if !strings.EqualFold(r.Guild, guild) {
			continue
		}
		what := fmt.Sprintf("%s may leave Starveil (loyalty %.0f", r.Char, r.Loyalty)
		if r.PullTo != "" {
			what += fmt.Sprintf(", friends pull it to %s", r.PullTo)
		}
		what += "): show it care - a gift, a party, a word in guild chat"
		acts = append(acts, guildAction{"keep", what, r.Action})
	}
	for _, h := range open {
		what := fmt.Sprintf("help %s (lv%d, %s): %s", h.Asker, h.Lv, h.Map, h.Text)
		cmd := ""
		switch h.Kind {
		case "gear", "pots":
			amt := h.Amount
			if amt <= 0 {
				amt = 2000
			}
			why := h.Kind
			if h.Item != "" {
				why = h.Item
			}
			cmd = fmt.Sprintf("ayla.sh guild-fund \"%s\" %d \"%s\"", h.Asker, min(amt, 10000), why)
		case "party":
			cmd = fmt.Sprintf("ayla.sh guildchat \"anyone near %s? %s wants a party\"", h.Map, h.Asker)
		case "rescue":
			cmd = fmt.Sprintf("ayla.sh guildchat \"%s needs help on %s, whoever is close please go\"", h.Asker, h.Map)
		}
		acts = append(acts, guildAction{"help", what, cmd})
	}
	return acts
}

// fundShare: what a member can give to the guild pool (guildFundLocal's rule, estimated from the API row): half its
// spare above its own next gear piece + guildFundBuffer, at most guildFundEach; her party friends pool for their own
// party's gear turns, so they don't count.
func fundShare(m guildMemberInfo) int {
	if !m.Online || m.Friend {
		return 0
	}
	s := m.Spare
	if m.Next != nil && m.Next.Cost > 0 {
		if m.Next.Turn {
			return 0
		}
		s -= m.Next.Cost + guildFundBuffer
	}
	if s < 600 {
		return 0
	}
	return min(guildFundEach, s/2)
}
