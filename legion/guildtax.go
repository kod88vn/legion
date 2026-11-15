package main

// Guild EXP tax and guild skills (owner 2026-11-14, RULES.md 3b: "every guild should tax to level up").
// Guilds level only from the EXP their members pay as tax (a pay rate per guild position); guild levels give guild
// skill points; Guild Extension (+6 members a level, 16 + 6 x lv, up to 76) is the first thing to buy, then the chain
// to Guild Storage (Approval, Guard Research, Battle Orders, Regeneration, Emergency Call, Glory of Wounds, Cold Heart,
// Leadership, Sharp Gaze - its prerequisites in db/re/guild_skill_tree.yml).
//
// Packets (PACKETVER 20210107, rAthena clif.cpp):
//   CZ_REQ_GUILD_MENU 0x014f <type>.L: 0 basic info (ZC_GUILD_INFO 0x0b7b), 2 positions (0x0166 names + 0x0160 info),
//     3 skills (ZC_GUILD_SKILLINFO 0x0162)
//   ZC_GUILD_INFO 0x0b7b (118): GDID@2 level@6 userNum@10 maxUserNum@14 avgLv@18 exp@22 maxExp@26 ... name@46[24]
//   ZC_POSITION_ID_NAME_INFO 0x0166 <len>.W { <id>.L <name>.24B }*
//   ZC_POSITION_INFO 0x0160 <len>.W { <id>.L <mode>.L <ranking>.L <payRate>.L }*
//   CZ_REG_CHANGE_GUILD_POSITIONINFO 0x0161 <len>.W { <id>.L <mode>.L <ranking>.L <payRate>.L <name>.24B }* (master only;
//     pay rate capped by guild_exp_limit 50)
//   ZC_GUILD_SKILLINFO 0x0162 <len>.W <points>.W { <id>.W <inf>.L <lv>.W <sp>.W <range>.W <name>.24B <up>.B }* (37 each)
//   CZ_UPGRADE_SKILLLEVEL 0x0112 <skill id>.W (guild skills too: pc_skillup -> guild_skillup for ids >= 10000)
// Bot guild masters set the tax once (re-checked every 10 min, so it survives restarts) and spend skill points; any
// member bot refreshes its guild's basic info (level, EXP, max members) for /api/guild-levels and Starveil's cap.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	guildPositions   = 20
	guildAdminEvery  = 10 * time.Minute
	guildInfoEvery   = 10 * time.Minute
	taxMasterPct     = 10 // the master's own position
	taxOfficerPct    = 10 // positions 1-3
	taxMemberPct     = 10 // every other position (new members join at the last one); owner: "definitely tax the guild to open more slots"
	guildSkillBaseID = 10000
)

// guildSkillPlan: what masters buy, in order (name, target level). Extension first: more members.
var guildSkillPlan = []struct {
	Name string
	Lv   int
}{
	{"GD_EXTENSION", 10}, {"GD_APPROVAL", 1}, {"GD_GUARDRESEARCH", 1}, {"GD_BATTLEORDER", 1}, {"GD_REGENERATION", 1},
	{"GD_EMERGENCYCALL", 1}, {"GD_GLORYWOUNDS", 1}, {"GD_SOULCOLD", 1}, {"GD_LEADERSHIP", 1}, {"GD_HAWKEYES", 1},
	{"GD_GUILD_STORAGE", 5}, {"GD_LEADERSHIP", 5}, {"GD_GLORYWOUNDS", 5}, {"GD_SOULCOLD", 5}, {"GD_HAWKEYES", 5},
	{"GD_KAFRACONTRACT", 1}, {"GD_DEVELOPMENT", 1}, {"GD_REGENERATION", 3}, {"GD_RESTORE", 1},
}

func taxTarget(pos int) uint32 {
	switch {
	case pos == 0:
		return taxMasterPct
	case pos <= 3:
		return taxOfficerPct
	}
	return taxMemberPct
}

type guildStat struct {
	Guild       string         `json:"guild"`
	Level       int            `json:"level"`
	Exp         int            `json:"exp"`
	NextExp     int            `json:"next_exp"`
	Members     int            `json:"members_online"`
	Max         int            `json:"max_members"`
	AvgLevel    int            `json:"avg_level"`
	TaxSet      bool           `json:"tax_set"`
	TaxRates    []uint32       `json:"tax_rates,omitempty"` // pay rate per position (0..19) as last seen by the master
	SkillPoints int            `json:"skill_points"`
	Skills      map[string]int `json:"skills,omitempty"`
	At          time.Time      `json:"updated"`
	First       *guildSnap     `json:"first_seen,omitempty"` // level/exp when first seen this process: growth measure
	SkillUps    int            `json:"skill_ups_session"`
	TaxSends    int            `json:"tax_changes_session"`
}

type guildSnap struct {
	Level int       `json:"level"`
	Exp   int       `json:"exp"`
	At    time.Time `json:"at"`
}

var guildStats = struct {
	sync.Mutex
	m       map[string]*guildStat // lower-case guild name
	infoReq map[string]time.Time  // guild -> last basic-info request by any member
}{m: map[string]*guildStat{}, infoReq: map[string]time.Time{}}

func guildStatOf(name string) *guildStat { // guildStats held
	k := strings.ToLower(name)
	s := guildStats.m[k]
	if s == nil {
		s = &guildStat{Guild: name}
		guildStats.m[k] = s
	}
	return s
}

// guildMaxMembers: the guild's real member cap (16 + 6 x Guild Extension) when a member bot has seen it, else 16.
func guildMaxMembers(name string) int {
	guildStats.Lock()
	defer guildStats.Unlock()
	if s := guildStats.m[strings.ToLower(name)]; s != nil && s.Max > 0 {
		return s.Max
	}
	return 16
}

// guildAdmin: a master's view of its positions (per bot, b.mu)
type guildAdmin struct {
	names   [guildPositions]string
	modes   [guildPositions]uint32
	rates   [guildPositions]uint32
	gotName bool
	nextReq time.Time
	taxAt   time.Time // last 0x0161 sent
	skillAt time.Time // last skill-up sent
}

// guildAdminThink (b.mu held; from guildThink): basic info for any member, positions + skills for a master.
func (b *Bot) guildAdminThink(now time.Time) {
	if b.GuildID == 0 || b.GuildName == "" || !b.Online || b.MapName == "" {
		return
	}
	g := b.GuildName
	guildStats.Lock()
	due := now.Sub(guildStats.infoReq[strings.ToLower(g)]) >= guildInfoEvery
	if due {
		guildStats.infoReq[strings.ToLower(g)] = now
	}
	guildStats.Unlock()
	if due {
		go b.send(newPkt(0x014f).u32(0).b)
	}
	if !b.guildMaster {
		return
	}
	if b.gadm == nil {
		b.gadm = &guildAdmin{nextReq: now.Add(time.Duration(30+crand(60)) * time.Second)}
	}
	if now.Before(b.gadm.nextReq) {
		return
	}
	b.gadm.nextReq = now.Add(guildAdminEvery)
	go func() {
		b.send(newPkt(0x014f).u32(2).b)
		time.Sleep(time.Second)
		b.send(newPkt(0x014f).u32(3).b)
	}()
}

// onGuildAdmin (b.mu held): the guild info / position / skill replies.
func (b *Bot) onGuildAdmin(op uint16, pk []byte) {
	now := time.Now()
	switch op {
	case 0x0b7b: // ZC_GUILD_INFO
		if len(pk) < 70 {
			return
		}
		name := cstr(pk[46:70])
		if name == "" {
			return
		}
		guildStats.Lock()
		s := guildStatOf(name)
		s.Level, s.Members, s.Max = int(le.Uint32(pk[6:])), int(le.Uint32(pk[10:])), int(le.Uint32(pk[14:]))
		s.AvgLevel, s.Exp, s.NextExp, s.At = int(le.Uint32(pk[18:])), int(le.Uint32(pk[22:])), int(le.Uint32(pk[26:])), now
		if s.First == nil {
			s.First = &guildSnap{s.Level, s.Exp, now}
		}
		guildStats.Unlock()
	case 0x0166: // position names
		if !b.guildMaster || b.gadm == nil {
			return
		}
		for i := 4; i+28 <= len(pk); i += 28 {
			if id := int(le.Uint32(pk[i:])); id >= 0 && id < guildPositions {
				b.gadm.names[id] = cstr(pk[i+4 : i+28])
			}
		}
		b.gadm.gotName = true
	case 0x0160: // position info: set the tax where it differs
		if !b.guildMaster || b.gadm == nil {
			return
		}
		for i := 4; i+16 <= len(pk); i += 16 {
			if id := int(le.Uint32(pk[i:])); id >= 0 && id < guildPositions {
				b.gadm.modes[id], b.gadm.rates[id] = le.Uint32(pk[i+4:]), le.Uint32(pk[i+12:])
			}
		}
		var change []int
		for id := 0; id < guildPositions; id++ {
			if b.gadm.rates[id] != taxTarget(id) {
				change = append(change, id)
			}
		}
		guildStats.Lock()
		s := guildStatOf(b.GuildName)
		s.TaxRates = append([]uint32(nil), b.gadm.rates[:]...)
		s.TaxSet = len(change) == 0
		guildStats.Unlock()
		if len(change) == 0 || !b.gadm.gotName || now.Sub(b.gadm.taxAt) < guildAdminEvery-time.Minute {
			return
		}
		b.gadm.taxAt = now
		p := newPkt(0x0161).u16(0)
		for _, id := range change {
			name := b.gadm.names[id]
			if name == "" {
				name = "Position " + strconv.Itoa(id+1)
			}
			p = p.u32(uint32(id)).u32(b.gadm.modes[id]).u32(uint32(id)).u32(taxTarget(id)).str(name, 24)
		}
		go b.send(p.varlen())
		guildStats.Lock()
		guildStatOf(b.GuildName).TaxSends++
		guildStats.Unlock()
		econCount("guild tax: positions set")
		b.event("guild", "", "set the guild EXP tax (10% on every position) so "+b.GuildName+" levels up")
		b.gadm.nextReq = now.Add(30 * time.Second) // read it back
	case 0x0162: // guild skills: spend a point on the plan's next skill
		if len(pk) < 6 {
			return
		}
		points := int(le.Uint16(pk[4:]))
		lv, up, ids := map[string]int{}, map[string]bool{}, map[string]uint16{}
		for i := 6; i+37 <= len(pk); i += 37 {
			n := cstr(pk[i+12 : i+36])
			lv[n], up[n], ids[n] = int(le.Uint16(pk[i+6:])), pk[i+36] != 0, le.Uint16(pk[i:])
		}
		guildStats.Lock()
		s := guildStatOf(b.GuildName)
		s.SkillPoints, s.Skills = points, lv
		guildStats.Unlock()
		if !b.guildMaster || points <= 0 || b.gadm == nil || now.Sub(b.gadm.skillAt) < 3*time.Second {
			return
		}
		for _, st := range guildSkillPlan {
			if lv[st.Name] >= st.Lv || !up[st.Name] || ids[st.Name] < guildSkillBaseID {
				continue
			}
			b.gadm.skillAt = now
			go b.send(newPkt(0x0112).u16(ids[st.Name]).b)
			guildStats.Lock()
			guildStatOf(b.GuildName).SkillUps++
			guildStats.Unlock()
			econCount("guild skill: point spent")
			b.event("guild", "", "learned the guild skill "+st.Name+" for "+b.GuildName)
			b.gadm.nextReq = now.Add(5 * time.Second) // read the skills again for the next point
			break
		}
	}
}

// registerGuildLevels: GET /api/guild-levels - every guild a member bot has seen: level, EXP, cap, tax, skills.
func registerGuildLevels(mux *http.ServeMux) {
	mux.HandleFunc("/api/guild-levels", func(w http.ResponseWriter, r *http.Request) {
		guildStats.Lock()
		out := []guildStat{}
		taxed := 0
		for _, s := range guildStats.m {
			c := *s
			out = append(out, c)
			if s.TaxSet {
				taxed++
			}
		}
		guildStats.Unlock()
		sort.Slice(out, func(i, j int) bool {
			if out[i].Level != out[j].Level {
				return out[i].Level > out[j].Level
			}
			return out[i].Exp > out[j].Exp
		})
		writeJSON(w, map[string]any{"guilds": out, "tax_set": taxed, "count": len(out),
			"tax": map[string]int{"master": taxMasterPct, "officers_1_3": taxOfficerPct, "members": taxMemberPct}})
	})
}
