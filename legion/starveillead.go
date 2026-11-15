package main

// starveillead.go - Starveil is led with no AI around (owner rule AY19, 2026-11-14: Ayla functions like a normal bot
// with nothing else running, guild leadership included; G25: Starveil members live under the same rules as every
// guild; G20: Ayla's own party is never kicked; G12: nothing here moves Ayla's zeny). What a bot guild leader does on
// its own, the engine now does for Starveil (main instance only; tuning starveil_auto_lead, default on):
//
//   Duty        guildduty.go dutyActions treats Starveil like any guild (same standings, strictness, caps: guild
//               kicks per hour, 15 min between two, 2 h per guild). The warning and the expel are Ayla's: they are
//               queued here as orders, her plugin guildrecruit.pl fetches GET /api/human-guild/orders (every ~30 s),
//               says the line in guild chat and sends CZ_REQ_BAN_GUILD itself. The member answers the warning once the
//               order is fetched. An expel that is never fetched is retried by dutyActions like a bot leader's.
//               Ayla's party (core instance, pinned, kept, LEGION_CORE) is exempt here, in the ledger and in the plugin.
//   Funding     every 30 min the guild pool (online members' surplus, guildFundLocal: never Ayla, never a member
//               pooling for its own gear turn) pays the alchemist's Potion Creation Guide first, else the weakest
//               member's next shop gear piece - guildPlan's "guide"/"fund" advice, now done. One gift per member per 6 h.
//   Keep gifts  members loyalty.go lists at risk get a keep gift from the pool (bot leaders pay theirs from their own
//               purse; Ayla's purse is never touched): one per member per day, one per guild per 2 h, as keepMember.
//   Parties     every 15 min one party of 3-5 free members within the PT35 level band at the best spot for them all
//               (memberPartyGroups), announced by Ayla in guild chat - guildPlan's "party" advice, now done.
//   Direction   autopilot directions and carry blocks already run for Starveil (guildduty.go dutyAutopilotRound,
//               dutyRunPlans); a leader-set direction from her mind replaces them as for any guild.
//   Recruiting  guildrecruit.pl invites one suggested recruit (GET /api/guild-recruits?n=1&mark=1) every 4-8 min.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	starveilOrderTTL   = 10 * time.Minute // an order not fetched by then is dropped (a kick is retried by dutyActions)
	starveilOrderMax   = 20
	starveilFundGap    = 30 * time.Minute
	starveilFundPerTo  = 6 * time.Hour
	starveilPartyGap   = 15 * time.Minute
	starveilKeepGuild  = 2 * time.Hour
	starveilKeepMember = 24 * time.Hour
	starveilKeepMax    = 5000 // the pool's per-donor cap (guildFundEach) times one
)

// starveilAutoLead: the engine leads Starveil on the main instance; with ayla_engine_on also on the core instance,
// where Ayla's engine bot is the master (the coreInstance gate is relaxed for Starveil only).
func starveilAutoLead() bool { return tune().StarveilAutoLead && (!coreInstance() || aylaEngineOn()) }

// aylaEngineOn: Ayla is played by the engine (package D). The one switch is package C's tuning engine_parties: Ayla is
// on the engine while "Ayla" is listed there, the same check as enginePartyOn("Ayla") in engineparty.go. Off changes nothing.
func aylaEngineOn() bool { return nameSet(tune().EngineParties)["ayla"] }

// starveilOrder: something Ayla's plugin does as Starveil's master (guild chat line, expel)
type starveilOrder struct {
	ID     int64     `json:"id"`
	Kind   string    `json:"kind"` // say | warn | kick
	Char   string    `json:"char,omitempty"`
	Guild  string    `json:"guild"` // in-game guild name: the plugin acts only in its own guild
	GID    uint32    `json:"guild_id,omitempty"`
	AID    uint32    `json:"account_id,omitempty"`
	CID    uint32    `json:"char_id,omitempty"`
	Line   string    `json:"line,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
	fetch  func()    // run once the plugin took the order (the member's answer)
}

var starveilLead = struct {
	sync.Mutex
	q        []*starveilOrder
	next     int64
	fundTo   map[string]time.Time
	keepTo   map[string]time.Time
	keepAt   time.Time
	counters map[string]int
}{fundTo: map[string]time.Time{}, keepTo: map[string]time.Time{}, counters: map[string]int{}}

func starveilCount(k string) {
	starveilLead.Lock()
	starveilLead.counters[k]++
	starveilLead.Unlock()
}

// starveilQueue adds an order (oldest dropped past starveilOrderMax)
func starveilQueue(o *starveilOrder, now time.Time) {
	starveilLead.Lock()
	defer starveilLead.Unlock()
	starveilLead.next++
	o.ID, o.At = starveilLead.next, now
	starveilLead.q = append(starveilLead.q, o)
	if n := len(starveilLead.q); n > starveilOrderMax {
		starveilLead.q = starveilLead.q[n-starveilOrderMax:]
	}
	starveilLead.counters["orders queued: "+o.Kind]++
}

// starveilTake hands the plugin every live order once (expired ones are dropped)
func starveilTake(now time.Time) []*starveilOrder {
	starveilLead.Lock()
	defer starveilLead.Unlock()
	var out []*starveilOrder
	for _, o := range starveilLead.q {
		if now.Sub(o.At) <= starveilOrderTTL {
			out = append(out, o)
		} else {
			starveilLead.counters["orders expired: "+o.Kind]++
		}
	}
	starveilLead.q = nil
	return out
}

// starveilDutyOK: a Starveil member the engine may warn or kick (the duty ledger already exempts these; this is the
// second guard for gate G20 - Ayla's party is never kicked).
func starveilDutyOK(s *dutySnap) bool {
	return starveilAutoLead() && s != nil && !s.pinned && !s.core && !s.keep && (!coreInstance() || aylaEngineOn())
}

// starveilDuty: dutyActions' warning / expel for a Starveil member, done by Ayla's plugin
func starveilDuty(s *dutySnap, m dutyMember, kick bool, guild string) {
	if s == nil || guild == "" || !starveilDutyOK(s) {
		return
	}
	now := time.Now()
	if kick {
		if s.gid == 0 || s.aid == 0 || s.cid == 0 {
			return // ids unknown: dutyActions retries after its 3-minute window
		}
		line := pick(fmt.Sprintf("sorry %s, u havent helped the guild in ages. making room for active ppl", m.Char),
			fmt.Sprintf("removing %s, no guild work for days. no hard feelings", m.Char))
		starveilQueue(&starveilOrder{Kind: "kick", Char: m.Char, Guild: guild, GID: s.gid, AID: s.aid, CID: s.cid,
			Line: line, Reason: "not helping the guild"}, now)
		log.Printf("starveil lead: expel of %s queued for Ayla (%s)", m.Char, m.KickWhy)
		return
	}
	line := pick(fmt.Sprintf("%s, havent seen u do anything for the guild in days. ~2h a day pls or ill have to let u go", m.Char),
		fmt.Sprintf("hey %s, guild needs u too. do a guild shift today ok? otherwise im kicking", m.Char),
		fmt.Sprintf("%s pls help with the guild stuff, its been %d days. last warning", m.Char, m.Short))
	b := s.b
	starveilQueue(&starveilOrder{Kind: "warn", Char: m.Char, Guild: guild, Line: line, fetch: func() {
		go func() {
			time.Sleep(time.Duration(6+crand(10)) * time.Second) // Ayla says it first
			b.mu.Lock()
			if b.Online && b.GuildName != "" {
				b.guildSay(styled(b.P.Typing, pick("sorry, will do", "ok ok, ill help more", "my bad, been busy. will do a shift", "yeah sorry")))
				b.remember("got warned by my guild leader for not helping the guild")
			}
			b.mu.Unlock()
		}()
	}}, now)
}

// starveilFundPick: what the guild pool pays next (pure: tested). The alchemist's Potion Creation Guide first, else
// the weakest-geared online member's next shop piece the pool covers (guildPlan's rules). to == "": nothing.
func starveilFundPick(ms []guildMemberInfo, skip func(name string) bool) (to string, amount int, why string) {
	pool := 0
	for _, m := range ms {
		pool += fundShare(m)
	}
	var alch *guildMemberInfo
	for i := range ms {
		m := &ms[i]
		if !m.Brewer || !m.InGuild && !m.Friend {
			continue
		}
		if alch == nil || m.Guide && !alch.Guide || m.Guide == alch.Guide && m.Level > alch.Level {
			alch = m
		}
	}
	if alch != nil && !alch.Guide && alch.Online && !skip(alch.Name) {
		need := guideCost + 3000 - alch.Zeny
		if p := pool - fundShare(*alch); need > 0 && p >= 300 {
			return alch.Name, min(need, p)/100*100 + 100, "Potion Creation Guide"
		}
	}
	var best *guildMemberInfo
	for i := range ms {
		m := &ms[i]
		if !m.Online || m.Next == nil || m.Next.Shop == "" || m.Next.Cost <= m.Next.Have || skip(m.Name) ||
			(alch != nil && m.Name == alch.Name && !alch.Guide) {
			continue
		}
		if short := m.Next.Cost - m.Next.Have; short > pool-fundShare(*m) {
			continue
		}
		if best == nil || m.Weak < best.Weak {
			best = m
		}
	}
	if best != nil {
		return best.Name, (best.Next.Cost - best.Next.Have + 99) / 100 * 100, best.Next.Item
	}
	return "", 0, ""
}

// starveilKeepAmount: a keep gift's size (loyalty.go keepAmount's clamp, at most what one round of the pool gives)
func starveilKeepAmount(gearShort int) int {
	return min(max(gearShort, 2000), starveilKeepMax) / 100 * 100
}

// starveilLeadManager: Starveil's funding, keep gifts and member parties (main instance; registerGuildDuty hook)
func starveilLeadManager(bots []*Bot) {
	t := time.NewTicker(15 * time.Second) // the engine's orders go out within 15 s (starveilEngineDrain)
	defer t.Stop()
	fundAt := time.Now().Add(5 * time.Minute) // after login settles
	partyAt := time.Now().Add(8 * time.Minute)
	recruitAt := time.Now().Add(6 * time.Minute)
	for range t.C {
		if !starveilAutoLead() {
			continue
		}
		guild := humanGuildInfo().Guild // file read: never under a lock
		if guild == "" {
			continue
		}
		now := time.Now()
		if aylaEngineOn() {
			starveilEngineDrain(now) // the queued warnings, expels, invites and lines go to Ayla's engine bot in core
			if now.After(recruitAt) {
				recruitAt = now.Add(time.Duration(4+crand(5)) * time.Minute) // guildrecruit.pl's cadence (4-8 min)
				starveilRecruitRound(bots, guild, now)
			}
		}
		if now.After(fundAt) {
			fundAt = now.Add(starveilFundGap)
			starveilFundRound(bots, guild, now)
			starveilKeepRound(bots, guild, now)
		}
		if now.After(partyAt) {
			partyAt = now.Add(starveilPartyGap)
			starveilPartyRound(bots, guild, now)
		}
	}
}

func starveilFundRound(bots []*Bot, guild string, now time.Time) {
	ms := localGuildMembers(bots, guild)
	starveilLead.Lock()
	recent := map[string]bool{}
	for n, at := range starveilLead.fundTo {
		if now.Sub(at) < starveilFundPerTo {
			recent[strings.ToLower(n)] = true
		}
	}
	starveilLead.Unlock()
	to, amt, why := starveilFundPick(ms, func(n string) bool { return recent[strings.ToLower(n)] })
	if to == "" {
		return
	}
	pledged, donors := guildFundLocal(bots, to, amt, why, now)
	if pledged <= 0 {
		starveilCount("fund rounds: nobody could give")
		return
	}
	starveilLead.Lock()
	starveilLead.fundTo[to] = now
	starveilLead.counters["fund gifts"]++
	starveilLead.Unlock()
	log.Printf("starveil lead: guild pool funds %s %dz of %dz for %s (%s)", to, pledged, amt, why, strings.Join(donors, ", "))
}

func starveilKeepRound(bots []*Bot, guild string, now time.Time) {
	loyalty.Lock()
	risk := append([]atRisk(nil), loyalty.risk...)
	loyalty.Unlock()
	starveilLead.Lock()
	if now.Sub(starveilLead.keepAt) < starveilKeepGuild {
		starveilLead.Unlock()
		return
	}
	starveilLead.Unlock()
	for _, r := range risk {
		if !strings.EqualFold(r.Guild, guild) {
			continue
		}
		starveilLead.Lock()
		cool := now.Sub(starveilLead.keepTo[r.Char]) < starveilKeepMember
		starveilLead.Unlock()
		if cool {
			continue
		}
		pledged, _ := guildFundLocal(bots, r.Char, starveilKeepAmount(r.GearNeed), "keep: next gear", now)
		if pledged <= 0 {
			continue
		}
		loyaltyCare(r.Char, r.Guild, 12, "kept", 0)
		starveilLead.Lock()
		starveilLead.keepTo[r.Char], starveilLead.keepAt = now, now
		starveilLead.counters["keep gifts"]++
		starveilLead.Unlock()
		starveilQueue(&starveilOrder{Kind: "say", Guild: guild, Line: fmt.Sprintf(pick("%s check ur mail, the guild sent u something for ur next piece",
			"%s, guild money for ur gear is in ur mail. we look after our own"), r.Char)}, now)
		log.Printf("starveil lead: keep gift %dz to %s from the guild pool", pledged, r.Char)
		return // one per round
	}
}

func starveilPartyRound(bots []*Bot, guild string, now time.Time) {
	gs := memberPartyGroups(bots, guild)
	if len(gs) == 0 {
		return
	}
	g := gs[0]
	if formParty(g.members[0], g.members[1:], g.spot) == nil {
		starveilCount("member parties: could not form")
		return
	}
	starveilCount("member parties formed")
	log.Printf("starveil lead: member party on %s: %s", g.spot, strings.Join(g.names, ", "))
	starveilQueue(&starveilOrder{Kind: "say", Guild: guild, Line: fmt.Sprintf(pick("%s - party up and go %s together",
		"%s: grouped u up, head to %s", "made a party for %s, u guys go %s"), strings.Join(g.names, ", "), mapTalk(g.spot))}, now)
}

// registerStarveilOrders: GET /api/human-guild/orders - Ayla's plugin takes the queued orders (each once);
// ?peek=1 shows them without taking, with the counters.
func registerStarveilOrders(mux *http.ServeMux) {
	mux.HandleFunc("/api/human-guild/orders", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if aylaEngineOn() && r.URL.Query().Get("peek") != "1" {
			// the hand-off to Ayla's OpenKore plugin stops: the engine's bot does these orders (package D)
			writeJSON(w, map[string]any{"on": false, "engine": true, "orders": []*starveilOrder{}})
			return
		}
		if r.URL.Query().Get("peek") == "1" {
			starveilLead.Lock()
			q := append([]*starveilOrder(nil), starveilLead.q...)
			c := map[string]int{}
			for k, v := range starveilLead.counters {
				c[k] = v
			}
			starveilLead.Unlock()
			if q == nil {
				q = []*starveilOrder{}
			}
			writeJSON(w, map[string]any{"on": starveilAutoLead(), "orders": q, "counters": c})
			return
		}
		orders := starveilTake(now)
		for _, o := range orders {
			if o.fetch != nil {
				o.fetch()
			}
		}
		if orders == nil {
			orders = []*starveilOrder{}
		}
		writeJSON(w, map[string]any{"on": starveilAutoLead(), "orders": orders})
	})
}

// ---------------------------------------------------------------- Ayla on the engine (package D, ayla_engine_on)
//
// With ayla_engine_on, Ayla's engine bot (her character, the Starveil master already in game) acts as the guild leader
// of Starveil on the core instance. The decisions stay on the main instance, where the members are: the queue above
// (warn, expel, say, invite) is drained every 15 s and posted to the core instance, whose master bot does it. Tax and
// guild skills need nothing new: guildAdminThink runs for any bot that is its guild's master (guildtax.go).

// starveilMasterBot: the engine bot that is Starveil's in-game master on this instance (nil if none). b.mu is taken.
func starveilMasterBot(bots []*Bot, guild string) *Bot {
	for _, b := range bots {
		b.mu.Lock()
		ok := b.Online && b.guildMaster && b.GuildID != 0 && strings.EqualFold(b.GuildName, guild)
		b.mu.Unlock()
		if ok {
			return b
		}
	}
	return nil
}

// starveilExec: one order, done by the master bot l (core instance). The waits are its own; b.mu is not held across them.
func starveilExec(l *Bot, o starveilOrder) {
	switch o.Kind {
	case "say", "warn":
		l.mu.Lock()
		if l.Online && l.GuildName != "" {
			l.guildSay(styled(l.P.Typing, o.Line))
		}
		l.mu.Unlock()
	case "kick":
		l.mu.Lock()
		gid := l.GuildID
		ok := l.Online && l.guildMaster && gid != 0 && (o.GID == 0 || o.GID == gid)
		if ok {
			l.guildSay(styled(l.P.Typing, o.Line))
			l.event("guild", o.Char, "expelling "+o.Char+" (guild duty: "+o.Reason+")")
		}
		l.mu.Unlock()
		if !ok {
			starveilCount("engine kick: master not in the guild")
			return
		}
		time.Sleep(time.Duration(4+crand(6)) * time.Second)
		l.send(dutyExpelPacket(gid, o.AID, o.CID, o.Reason))
	case "invite":
		l.mu.Lock()
		ok := l.Online && l.guildMaster && l.GuildID != 0
		l.mu.Unlock()
		if ok {
			l.send(newPkt(0x0168).u32(o.AID).u32(l.aid).u32(l.cid).b) // CZ_REQ_JOIN_GUILD: the candidate answers itself
		}
	}
}

// registerStarveilEngine: POST /api/human-guild/engine (core instance, ayla_engine_on): main posts the orders it drained.
func registerStarveilEngine(mux *http.ServeMux, bots []*Bot) {
	mux.HandleFunc("/api/human-guild/engine", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			apiErr(w, 405, "POST the orders")
			return
		}
		if !aylaEngineOn() || !coreInstance() {
			apiErr(w, 409, "ayla_engine_on is off on this instance")
			return
		}
		var orders []starveilOrder
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&orders); err != nil {
			apiErr(w, 400, "bad orders: "+err.Error())
			return
		}
		l := starveilMasterBot(bots, humanGuildInfo().Guild)
		if l == nil {
			starveilCount("engine: no master bot in core")
			apiErr(w, 409, "Starveil's master is not online in core")
			return
		}
		for _, o := range orders {
			go starveilExec(l, o)
		}
		writeJSON(w, map[string]any{"master": l.P.Name, "orders": len(orders)})
	})
}

// starveilEngineDrain (main, ayla_engine_on): the queued orders are taken once; the member side of each runs here
// (its fetch), the rest goes to the master bot in core. A failed post is counted, not retried (orders expire anyway).
func starveilEngineDrain(now time.Time) {
	orders := starveilTake(now)
	if len(orders) == 0 {
		return
	}
	for _, o := range orders {
		if o.fetch != nil {
			o.fetch()
		}
	}
	base := coreAPIURL()
	if base == "" {
		starveilCount("engine: no core API, orders dropped")
		return
	}
	raw, _ := json.Marshal(orders)
	resp, err := coreClient.Post(base+"/api/human-guild/engine", "application/json", bytes.NewReader(raw))
	if err != nil {
		starveilCount("engine: core unreachable")
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		starveilCount("engine: core refused the orders")
		return
	}
	starveilCount("engine: orders sent to core")
}

// starveilRecruitRound (main, ayla_engine_on): the next recruit is the one guildrecruit.pl would invite (her party
// friends first, then the biggest role gap; the same inputs as /api/guild-recruits?n=1&mark=1). The invite goes out
// from the master bot in core.
func starveilRecruitRound(bots []*Bot, guild string, now time.Time) {
	info := humanGuildInfo()
	hs, _ := humanStatusOf(info.Human)
	minLv := 40
	if hs.BaseLevel-30 > minLv {
		minLv = hs.BaseLevel - 30
	}
	local := localGuildScan(withAdopted(bots), guild, minLv, now)
	members, cands := local.Members, local.Candidates
	if base := coreAPIURL(); base != "" {
		resp, err := coreClient.Get(base + "/api/guild-recruits?local=1&guild=" + urlQuery(guild) + "&min_lv=" + strconv.Itoa(minLv))
		if err == nil {
			var core guildRecruitsLocal
			if json.NewDecoder(resp.Body).Decode(&core) == nil {
				for _, m := range core.Members {
					m.key, _ = recruitJob(classIDByName(m.Class))
					members = append(members, m)
				}
				for _, c := range core.Candidates {
					c.key, c.second = recruitJob(classIDByName(c.Class))
					cands = append(cands, c)
				}
			}
			resp.Body.Close()
		}
	}
	job := hs.JobID
	if job == 0 {
		job = info.Job
	}
	picks, _ := pickGuildRecruits(members, cands, job, hs.BaseLevel, 1)
	if len(picks) == 0 {
		starveilCount("recruit rounds: no candidate")
		return
	}
	c := picks[0]
	humanGuild.Lock()
	if c.account != "" {
		humanGuild.invited[c.account] = now
	}
	humanGuild.Unlock()
	starveilQueue(&starveilOrder{Kind: "invite", Char: c.Name, Guild: guild, AID: c.AID}, now)
	log.Printf("starveil lead: invite %s (engine)", c.Name)
}
