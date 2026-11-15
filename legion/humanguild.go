package main

// Ayla's guild (owner 2026-11-14: "Ayla should also start a guild and recruit all different jobs"; no Emperium
// needed - rAthena runs guild_emperium_check: no, like the bot guilds).
//
// She founds it from her OpenKore (`guild create <name>`); her plugin statusout.pl puts the guild name and her job in
// /ayla/status.json. The engine then:
//   - answers her invitations: her party friends (pinned to her human party) always accept her guild and refuse
//     every other guild; any other unguilded, non-leader bot accepts with a high chance (she is a well-known
//     party leader), the rest of the time it declines and is not suggested again for humanGuildDeclineFor.
//   - suggests recruits: GET /api/guild-recruits[?n=3&min_lv=40&mark=1] (main instance; merges the core instance's
//     friends) lists online, unguilded bots of every job the guild doesn't have yet (one per job, her friends
//     first, 2nd/transcendent jobs before 1st jobs, nearest her level), with their account id. ayla.sh recruit
//     invites them through her OpenKore (plugin guildrecruit.pl `ginvite <aid> <name>`, CZ_REQ_JOIN_GUILD 0x0168 -
//     rAthena needs no distance for that). mark=1 records the invitation so the next call suggests someone else.
//   Guild leaders (guilds.go leaderVision) and members of the bot guilds are never suggested (no poaching).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	humanGuildDeclineFor = 6 * time.Hour    // a bot that declined her invitation isn't suggested again for this long
	humanGuildInviteGap  = 20 * time.Minute // a suggested (mark=1) bot isn't suggested again for this long
	humanGuildBaseMax    = 16               // rAthena's guild size without Guild Extension
)

type savedHumanGuild struct {
	Human string `json:"human"`
	Guild string `json:"guild"`
	Job   int    `json:"job,omitempty"`
}

var humanGuild = struct {
	sync.Mutex
	at       time.Time
	cur      savedHumanGuild
	declined map[string]time.Time // persona account -> declined her invitation at
	invited  map[string]time.Time // persona account -> suggested with mark=1 at
	accepted map[string]time.Time // persona account -> accepted her invitation at (the hello is said only then)
}{declined: map[string]time.Time{}, invited: map[string]time.Time{}, accepted: map[string]time.Time{}}

func humanGuildFile() string { return filepath.Join(*dataDir, "human-guild.json") }

// humanGuildInfo: the human leader's guild, from her status file (any age: the guild name doesn't go stale), kept in
// data/human-guild.json so it is known while she is offline. Re-read at most every 10 s.
func humanGuildInfo() savedHumanGuild {
	humanGuild.Lock()
	defer humanGuild.Unlock()
	now := time.Now()
	if now.Sub(humanGuild.at) < 10*time.Second {
		return humanGuild.cur
	}
	humanGuild.at = now
	cur := humanGuild.cur
	if cur.Guild == "" {
		if raw, err := os.ReadFile(humanGuildFile()); err == nil {
			json.Unmarshal(raw, &cur)
		}
	}
	if raw, err := os.ReadFile(humanStatusPath()); err == nil {
		var s humanStatus
		if json.Unmarshal(raw, &s) == nil && s.Name != "" && s.Guild != "" {
			if s.Guild != cur.Guild || s.Name != cur.Human || (s.JobID != 0 && s.JobID != cur.Job) {
				cur = savedHumanGuild{Human: s.Name, Guild: s.Guild, Job: s.JobID}
				if b, err := json.MarshalIndent(cur, "", "  "); err == nil {
					writeFileAtomic(humanGuildFile(), b)
				}
			}
		}
	}
	humanGuild.cur = cur
	return cur
}

// isHumanGuild: the guild name is the human leader's guild.
func isHumanGuild(name string) bool {
	g := humanGuildInfo().Guild
	return g != "" && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(g))
}

// humanPartyFriend (b.mu held): the bot is pinned to a human's party (Ayla's friends).
func (b *Bot) humanPartyFriend() bool { return b.pin != nil && b.pin.Human != "" }

// humanGuildAnswer (b.mu held): the answer to an invitation from the human's guild or, for her friends, from any
// guild. ok=false: not a case this file decides (guilds.go guildFit applies).
func (b *Bot) humanGuildAnswer(guild string) (accept, ok bool) {
	human := isHumanGuild(guild)
	if b.humanPartyFriend() {
		// her friends join her guild and no other one (they'd leave her party's guild plan otherwise)
		if human && b.GuildID == 0 {
			noteHumanGuildAccept(b)
			return true, true
		}
		return false, true
	}
	if !human {
		return false, false
	}
	if b.GuildID != 0 || b.leaderVision() != nil {
		return false, true
	}
	f := guildFitBase(b)
	if f < 0.8 {
		f = 0.8
	}
	if b.rng.Float64() < f {
		noteHumanGuildAccept(b)
		return true, true
	}
	humanGuild.Lock()
	humanGuild.declined[b.P.Account] = time.Now()
	humanGuild.Unlock()
	return false, true
}

func noteHumanGuildAccept(b *Bot) {
	humanGuild.Lock()
	humanGuild.accepted[b.P.Account] = time.Now()
	humanGuild.Unlock()
}

// guildFitBase: personality part of guildFit (guilds.go)
func guildFitBase(b *Bot) float64 { return 0.25 + 0.35*b.P.Sociability + 0.25*b.P.Loyalty }

// humanGuildJoined (b.mu held): a short hello in guild chat after joining the human's guild.
func (b *Bot) humanGuildJoined(guild string) {
	humanGuild.Lock()
	at, ok := humanGuild.accepted[b.P.Account]
	delete(humanGuild.accepted, b.P.Account)
	humanGuild.Unlock()
	if !ok || time.Since(at) > 5*time.Minute || !isHumanGuild(guild) { // not on every login after a restart
		return
	}
	line := pick("ty for the invite! :)", "hi guild o/", "thx for having me", "hey all, glad to be here", "joined! hi everyone")
	go func() {
		time.Sleep(time.Duration(3+crand(6)) * time.Second)
		b.guildSay(line)
	}()
}

// recruitJob: the job a recruit stands for ("every job" = each 2nd job once). Transcendent and mounted jobs count as
// their 2nd job; 1st jobs count as themselves (second priority). 0 = not recruited for (Novice).
func recruitJob(class int) (key int, second bool) {
	c := class
	if c >= 4008 && c <= 4022 { // Lord Knight .. Paladin (Peco) -> Knight .. Crusader (Peco)
		c -= 4001
	} else if c >= 4002 && c <= 4007 { // High Swordman .. High Thief
		c -= 4001
	}
	switch c {
	case 13:
		c = 7 // Knight (Peco)
	case 21:
		c = 14 // Crusader (Peco)
	}
	switch {
	case c >= 7 && c <= 20 && c != 13:
		return c, true
	case c >= 1 && c <= 6:
		return c, false
	case c == 23, c == 24, c == 25, c == 4046, c == 4047, c == 4049: // Super Novice, Gunslinger, Ninja, TK line
		return c, c != 4046
	}
	return 0, false
}

type guildRecruit struct {
	Name    string `json:"name"`
	AID     uint32 `json:"account_id"`
	Class   string `json:"class"`
	Level   int    `json:"level"`
	Map     string `json:"map"`
	Friend  bool   `json:"party_friend,omitempty"`
	Trader  bool   `json:"vendor,omitempty"` // a Merchant-line vendor with Vending and an open shop: the guild's trader
	Why     string `json:"why"`
	key     int
	second  bool
	account string
}

type guildMemberRow struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Level  int    `json:"level"`
	Trader bool   `json:"vendor,omitempty"`
	key    int
}

// skMerchantVending: MC_VENDING
const skMerchantVending = 41

// isVendorTrader (b.mu held): a Merchant-line character with Vending that sells (vendor role or an open shop) - what
// guildstock.go picks as a guild's trader (owner 2026-11-14: every guild has a merchant trader at the south gate).
func (b *Bot) isVendorTrader() bool {
	return family(b.Class) == famMerchant && b.skillLv(skMerchantVending) > 0 &&
		(b.charRole() == "vendor" || b.vend != nil && b.vend.Opened)
}

type guildRecruitsLocal struct {
	Guild      string           `json:"guild"`
	Members    []guildMemberRow `json:"members"`
	Candidates []guildRecruit   `json:"candidates"`
}

// localGuildScan: this instance's bots in the human's guild and its unguilded candidates (not filtered by job yet).
func localGuildScan(bots []*Bot, guild string, minLv int, now time.Time) guildRecruitsLocal {
	out := guildRecruitsLocal{Guild: guild}
	humanGuild.Lock()
	declined, invited := map[string]bool{}, map[string]bool{}
	for a, t := range humanGuild.declined {
		if now.Sub(t) < humanGuildDeclineFor {
			declined[a] = true
		}
	}
	for a, t := range humanGuild.invited {
		if now.Sub(t) < humanGuildInviteGap {
			invited[a] = true
		}
	}
	humanGuild.Unlock()
	for _, b := range bots {
		b.mu.Lock()
		name, class, lv, on := b.charName(), b.Class, b.BaseLvOr(), b.Online && b.MapName != ""
		gname, gid := b.GuildName, b.GuildID
		friend, leader, m := b.humanPartyFriend(), b.leaderVision() != nil, b.MapName
		aid := b.aid
		trader := b.isVendorTrader()
		b.mu.Unlock()
		if aid == 0 {
			aid = uint32(b.P.AccountID)
		}
		if gname == "" {
			if v, ok := guildRoster.Load(b.P.Account); ok {
				gname = v.(string)
			}
		}
		if guild != "" && strings.EqualFold(gname, guild) {
			k, _ := recruitJob(class)
			out.Members = append(out.Members, guildMemberRow{Name: name, Class: className(class), Level: lv, Trader: trader, key: k})
			continue
		}
		if !on || gid != 0 || gname != "" || leader || aid == 0 || declined[b.P.Account] || invited[b.P.Account] {
			continue
		}
		k, second := recruitJob(class)
		if k == 0 || (!friend && !trader && lv < minLv) { // a trader sells, its level matters little
			continue
		}
		out.Candidates = append(out.Candidates, guildRecruit{Name: name, AID: aid, Class: className(class), Level: lv, Map: m,
			Friend: friend, Trader: trader, key: k, second: second, account: b.P.Account})
	}
	return out
}

// Balanced roster (owner 2026-11-14: "Recruit the right and balance jobs"): role targets for a 16-member guild,
// the leader included. The biggest gap is filled first; inside a role a job the guild doesn't have yet comes first
// (variety), then 2nd/transcendent jobs, then the level nearest the leader's.
var guildRoleTarget = []struct {
	Role string `json:"role"`
	N    int    `json:"n"`
}{{"tank", 3}, {"healer", 3}, {"aoe", 3}, {"ranged", 3}, {"craft", 1}, {"trader", 1}, {"dps", 2}}

// guildRole: the role a job fills in the guild ("" = not recruited for)
func guildRole(class int) string {
	k, _ := recruitJob(class)
	switch k {
	case 7, 14, 1:
		return "tank"
	case 8, 15, 4:
		return "healer" // Priest, support Monk (Heal 3 first in the monk plan), Acolyte
	case 9, 16, 2:
		return "aoe"
	case 11, 19, 20, 3, 24:
		return "ranged"
	case 10, 18, 5:
		return "craft"
	case 12, 17, 6, 25, 4046, 4047, 4049, 23:
		return "dps"
	}
	return ""
}

func roleIdx(r string) int {
	for i, t := range guildRoleTarget {
		if t.Role == r {
			return i
		}
	}
	return len(guildRoleTarget)
}

func roleTarget(r string) int {
	for _, t := range guildRoleTarget {
		if t.Role == r {
			return t.N
		}
	}
	return 0
}

// pickGuildRecruits: her party friends first, then the biggest role gap first; at most n, never past the guild's size.
// Returns the picks and the gap per role after them.
func pickGuildRecruits(members []guildMemberRow, cands []guildRecruit, leaderJob, leaderLv, n int) ([]guildRecruit, map[string]int) {
	have := map[string]int{}
	jobs := map[int]bool{}
	if r := guildRole(leaderJob); r != "" {
		have[r]++
		k, _ := recruitJob(leaderJob)
		jobs[k] = true
	}
	for _, m := range members {
		if m.Trader && have["trader"] == 0 {
			have["trader"]++ // the guild's trader (one) counts as the trader, not as its class role
		} else if r := guildRole(classIDByName(m.Class)); r != "" {
			have[r]++
		}
		if m.key != 0 {
			jobs[m.key] = true
		}
	}
	room := humanGuildMax() - 1 - len(members)
	if n > room {
		n = room
	}
	pool := append([]guildRecruit(nil), cands...)
	var out []guildRecruit
	roleOf := func(c guildRecruit) string {
		if c.Trader && have["trader"] < roleTarget("trader") {
			return "trader"
		}
		return guildRole(classIDByName(c.Class))
	}
	take := func(i int, why string) {
		c := pool[i]
		c.Why = why
		have[roleOf(c)]++
		jobs[c.key] = true
		out = append(out, c)
		pool = append(pool[:i], pool[i+1:]...)
	}
	for i := 0; i < len(pool) && len(out) < n; {
		if pool[i].Friend {
			take(i, "party friend")
			continue
		}
		i++
	}
	// the guild's merchant trader comes right after the friends (owner 2026-11-14: every guild has a trader who vends
	// its surplus at the south gate and buys what it lacks): a vendor near the leader's level
	if have["trader"] < roleTarget("trader") && len(out) < n {
		t := -1
		for i, c := range pool {
			if c.Trader && (t < 0 || abs(c.Level-leaderLv) < abs(pool[t].Level-leaderLv)) {
				t = i
			}
		}
		if t >= 0 {
			take(t, "the guild's merchant trader (vends at the Prontera south gate, buys what the guild needs)")
		}
	}
	for len(out) < n {
		best, bestGap := -1, 0
		for i, c := range pool {
			r := roleOf(c)
			gap := roleTarget(r) - have[r]
			if gap <= 0 {
				continue
			}
			if best < 0 || gap > bestGap {
				best, bestGap = i, gap
				continue
			}
			if gap < bestGap {
				continue
			}
			o := pool[best]
			if or := roleOf(o); or != r {
				if roleIdx(r) < roleIdx(or) { // same gap: the role first in target order (tanks, healers, ...)
					best = i
				}
				continue
			}
			switch {
			case jobs[c.key] != jobs[o.key]:
				if !jobs[c.key] {
					best = i
				}
			case c.second != o.second:
				if c.second {
					best = i
				}
			case abs(c.Level-leaderLv) < abs(o.Level-leaderLv):
				best = i
			}
		}
		if best < 0 {
			break
		}
		c := pool[best]
		r := roleOf(c)
		take(best, fmt.Sprintf("%s: %d of %d in the guild", r, have[r], roleTarget(r)))
	}
	gaps := map[string]int{}
	for _, t := range guildRoleTarget {
		if g := t.N - have[t.Role]; g > 0 {
			gaps[t.Role] = g
		}
	}
	return out, gaps
}

// registerHumanGuild: GET /api/guild-recruits (both instances; the main one merges the core one's friends).
func registerHumanGuild(mux *http.ServeMux, bots []*Bot) {
	registerHumanGuildLead(mux, bots) // humanguildlead.go: /api/human-guild, /api/human-guild/fund
	registerGuildLevels(mux)          // guildtax.go: /api/guild-levels
	mux.HandleFunc("/api/guild-recruits", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		info := humanGuildInfo()
		guild := q.Get("guild")
		if guild == "" {
			guild = info.Guild
		}
		now := time.Now()
		hs, _ := humanStatusOf(info.Human)
		lv := hs.BaseLevel
		minLv, _ := strconv.Atoi(q.Get("min_lv"))
		if minLv <= 0 {
			minLv = 40
			if lv-30 > minLv {
				minLv = lv - 30
			}
		}
		local := localGuildScan(withAdopted(bots), guild, minLv, now) // handoff.go: adopted party friends too
		if q.Get("local") == "1" {
			writeJSON(w, local)
			return
		}
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
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 3
		}
		job := hs.JobID
		if job == 0 {
			job = info.Job
		}
		pickN, gaps := pickGuildRecruits(members, cands, job, lv, n)
		if q.Get("mark") == "1" {
			humanGuild.Lock()
			for _, c := range pickN {
				if c.account != "" {
					humanGuild.invited[c.account] = now
				}
			}
			humanGuild.Unlock()
		}
		if pickN == nil {
			pickN = []guildRecruit{}
		}
		writeJSON(w, map[string]any{
			"guild": guild, "human": info.Human, "founded": guild != "",
			"members": len(members) + 1, "max": humanGuildMax(), "member_list": members,
			"recruits": pickN, "role_gaps": gaps, "role_targets": guildRoleTarget,
			"note": "invite each with: ayla.sh recruit (or ayla.sh cmd ginvite <account_id> <name>)",
		})
	})
}

func urlQuery(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "%", "%25"), " ", "%20")
}

// classIDByName: inverse of className for the classes recruits can have (0 = unknown; 3rd jobs skipped)
func classIDByName(n string) int {
	best := 0
	for id, s := range classNames {
		if s == n && !(id >= 4054 && id <= 4079) && (best == 0 || id < best) {
			best = id
		}
	}
	return best
}

// humanGuildMax: Starveil's real cap, 16 + 6 x Guild Extension, as a member bot last saw it (guildtax.go).
func humanGuildMax() int {
	if g := humanGuildInfo().Guild; g != "" {
		return guildMaxMembers(g)
	}
	return humanGuildBaseMax
}
