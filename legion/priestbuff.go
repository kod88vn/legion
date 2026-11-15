package main

// PT29 (owner 2026-11-14, "the priest in Ayla's party is very passive"): party members ask for buffs when they lack
// them, and the priest buffs them unless its SP is low; never passive. Applies to every party. Behind tuning
// priest_active_buffs (default on).
//
// Root cause (ro-openkore-ayla log 01:43-02:29 + engine trace, Zendancer84 lv79 Priest, 1265 max SP): the priest only
// buffed when SP stayed >= 70% after the cast (priestReadySP, max'd with priest_buff_min_sp_pct 60) and cast one buff
// per 10 s. In town it kept everyone buffed; in pay_dun01-02 a few heals put it under ~76% SP, buffDue went false, so
// healerIdleSit sat it down "sitting by Ayla" with buffs expired: Blessing / Increase AGI uptime on the 7 members in
// the dungeon was 17-77% (mean 55% / 43%), single gaps of 600-960 s (Echobunny Blessing 957 s). Nobody could ask:
// there was no request path at all.
//
// What this file does (the old code keeps running behind it; it shares b.buffAt keys "<skill>:<name>"):
//   - activeBuffOK replaces the 70% floor: a standing priest buffs down to priest_buff_hold_sp_pct (30%), keeping that
//     for Heal; a priest seated for an SP rest gets up for buffs only above 60%, or above the hold line when asked.
//   - activeBuffs: one buff per activeBuffGap (2.5 s, a real priest chains buffs), renewed before expiry, ordered
//     requested > tank > melee > ranged/support > self; Blessing before Increase AGI; Kyrie on request; Magnificat on
//     request or when the priest's own SP is under 50%; Impositio Manus on the tank / melee while the party fights.
//   - buffRecv: every bot notes the buffs that land on itself (ZC_USE_SKILL) and when they end (status icon off), and
//     a priest notes buffs another caster put on a party member (no double casts).
//   - buffAsk: a member lacking Blessing / Increase AGI (Kyrie for a tank that got it before, Magnificat for a caster
//     low on SP) with the priest next to it asks in party chat ("bless pls", "agi?") - at most one ask per member per
//     90-150 s and one per party per 25 s, and only after the priest had 20 s to do it unasked.
//   - buffAskHeard: the priest reads those lines (from anyone in the party, Ayla and humans included - G25) and
//     moves the asker to the front; under the hold line it says so ("sp low, sec") instead of going silent.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	prImpositio   = 66
	prMagnificat  = 74
	activeBuffGap = 2500 * time.Millisecond
	buffAskWait   = 20 * time.Second // a member lacks a buff this long before it asks
	buffAskParty  = 25 * time.Second // at most one ask per party in this window
	buffReqHold   = 60 * time.Second // an ask stays at the front of the priest's queue this long
	seatedBuffSP  = 60               // % SP: a priest resting for SP gets up for an unasked buff only above this
)

// EFST status icon ids (rAthena efst list) for the end of a buff on ourselves.
var buffEfst = map[int]int{10: alBlessing, 12: alIncAgi, 19: prKyrie, 20: prMagnificat}

// activeBuffDur: how long a buff cast at lv lasts.
func activeBuffDur(skill, lv int) time.Duration {
	switch skill {
	case alBlessing, alIncAgi:
		return buffDuration(lv)
	case prKyrie:
		return 120 * time.Second
	case prMagnificat:
		return time.Duration(15+15*max(lv, 1)) * time.Second
	case prImpositio:
		return 60 * time.Second
	case hpAssumptio:
		return time.Duration(20*max(lv, 1)) * time.Second
	}
	return 0
}

// buffAsks: per-party ask throttle (leaf lock, taken with b.mu held).
var buffAsks = struct {
	sync.Mutex
	at     map[*Party]time.Time
	deaths map[*Bot]int // a member's death count when its buff book was last cleared
}{at: map[*Party]time.Time{}, deaths: map[*Bot]int{}}

// activeBuffHold: the SP % a priest keeps for Heal (no buffs under it).
func activeBuffHold() int {
	if h := tune().PriestBuffHoldSP; h > 0 {
		return h
	}
	return 30
}

// activeBuffFloor: the SP % that must remain after a buff. Seated (resting for SP) the priest stands up for an
// unasked buff only above seatedBuffSP; asked, or standing, the floor is the hold line.
func activeBuffFloor(sitting, asked bool) int {
	if sitting && !asked {
		return max(seatedBuffSP, activeBuffHold())
	}
	return activeBuffHold()
}

// activeBuffOK (b.mu held): buffOK when priest_active_buffs is on - the gap is activeBuffGap (set by the caster) and
// the SP floor is activeBuffFloor.
func (b *Bot) activeBuffOK(si *SkillInfo, now time.Time) bool {
	if si == nil || si.Lv == 0 || b.MaxSP <= 0 || now.Before(b.buffAt["buffgap"]) {
		return false
	}
	asked := now.Before(b.buffAt["buffreq"])
	return (b.SP-si.SP)*100/b.MaxSP >= activeBuffFloor(b.Sitting, asked)
}

type buffCand struct {
	skill int
	name  string
	score int
}

// buffCands (b.mu held): the buffs due on party members within reach, best first. Pure apart from b's state, so it
// is unit-tested directly. fighting: the party fights right now (Impositio).
func (b *Bot) buffCands(p *Party, members []pubState, fighting bool, now time.Time) []buffCand {
	me := b.charName()
	var out []buffCand
	learned := func(id int) bool { si := b.skills[id]; return si != nil && si.Lv > 0 }
	for _, s := range members {
		if !s.Online || s.Map != b.MapName || dist(b.X, b.Y, s.X, s.Y) > 9 {
			continue
		}
		// healerstick.go: rAthena range + wall line (healreach.go) - a cast that can't land isn't a candidate
		if healerStickOn() && s.Name != me {
			if lx, ly := livePosOf(s); !healReachable(maps[b.MapName], b.X, b.Y, lx, ly, healRange) {
				continue // where she stands now, not the end of her walk
			}
		}
		if s.HPFrac <= 0 && s.Name != me { // dead: its buffs are gone - rebuff once it is up
			for _, id := range []int{alBlessing, alIncAgi, prKyrie, prImpositio, hpAssumptio} {
				delete(b.buffAt, fmt.Sprintf("%d:%s", id, s.Name))
			}
			continue
		}
		base := 10
		switch {
		case s.Name == me:
			base = 5
		case s.Tank:
			base = 30
		case !s.Back && !s.Support:
			base = 20
		}
		leader := healerStickOn() && s.Name != me && isPartyLeaderName(p, s.Name)
		if leader {
			base = 40 // healerstick.go: the leader (Ayla too) is buffed first, after explicit requests
		}
		asked := now.Before(b.buffAt["buffreq:"+s.Name])
		due := func(id int) bool { return learned(id) && !now.Before(b.buffAt[fmt.Sprintf("%d:%s", id, s.Name)]) }
		add := func(id, bonus int) {
			sc := base + bonus
			if asked && now.Before(b.buffAt[fmt.Sprintf("req:%d:%s", id, s.Name)]) {
				sc += 100
			}
			out = append(out, buffCand{id, s.Name, sc})
		}
		if due(alBlessing) {
			add(alBlessing, 2)
		}
		if due(alIncAgi) {
			add(alIncAgi, 1)
		}
		// Kyrie: the tank (old loop does it too) or anyone who asked for it
		if due(prKyrie) && (s.Tank || leader && (fighting || s.Target != 0) || now.Before(b.buffAt[fmt.Sprintf("req:%d:%s", prKyrie, s.Name)])) && s.Name != me {
			add(prKyrie, 3)
		}
		if fighting && due(prImpositio) && s.Name != me && (s.Tank || !s.Back && !s.Support) && b.MaxSP > 0 && b.SP*100/b.MaxSP >= 60 {
			add(prImpositio, 0)
		}
	}
	// Magnificat (party-wide SP regen): asked for, or the priest itself under 50% SP
	if learned(prMagnificat) && !now.Before(b.buffAt[fmt.Sprintf("%d:party", prMagnificat)]) {
		asked := now.Before(b.buffAt[fmt.Sprintf("req:%d:any", prMagnificat)])
		if asked || b.MaxSP > 0 && b.SP*100/b.MaxSP < 50 {
			sc := 15
			if asked {
				sc = 110
			}
			out = append(out, buffCand{prMagnificat, me, sc})
		}
	}
	// stable best-first (insertion sort: a handful of entries)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].score > out[j-1].score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// activeBuffs (b.mu held, party healer, from party.go before the old buff loop): cast the best due buff. true = a
// skill was sent this tick. No-op with priest_active_buffs off.
func (b *Bot) activeBuffs(p *Party, members []pubState, lead pubState, now time.Time) bool {
	if !tune().PriestActiveBuffs || b.Sitting || now.Before(b.skillReady) || b.MaxSP <= 0 {
		return false
	}
	fighting := lead.Target != 0 || b.fightOn(now)
	for _, c := range b.buffCands(p, members, fighting, now) {
		si := b.skills[c.skill]
		if !b.activeBuffOK(si, now) {
			if c.score >= 100 && now.After(b.buffAt["buffsplow"]) && (b.SP-si.SP)*100/b.MaxSP < activeBuffHold() {
				b.buffAt["buffsplow"] = now.Add(90 * time.Second) // asked but SP is for heals: say so, once
				typing := b.P.Typing
				go b.partySay(styled(typing, pick("sp low, sec", "low sp, gimme a sec", "sec, need sp first", "oom, 1 sec")))
			}
			continue
		}
		id := b.aid
		if c.name != b.charName() {
			if id = b.entityID(c.name); id == 0 {
				continue
			}
		}
		key := fmt.Sprintf("%d:%s", c.skill, c.name)
		switch c.skill {
		case alBlessing, alIncAgi, prKyrie:
			if c.name != b.charName() && b.buffBookPending(c.skill, c.name, now) {
				break // healerstick.go: booked for real when the cast is seen landing (buffRecv)
			}
			if c.skill == prKyrie {
				b.buffAt[key] = now.Add(kyrieRenew)
			} else {
				b.buffAt[key] = b.buffRenewAt(si.Lv, now)
			}
		case prMagnificat:
			key = fmt.Sprintf("%d:party", prMagnificat)
			b.buffAt[key] = now.Add(activeBuffDur(prMagnificat, si.Lv) - 5*time.Second)
			id = b.aid
		default:
			b.buffAt[key] = now.Add(activeBuffDur(c.skill, si.Lv) - 5*time.Second)
		}
		delete(b.buffAt, fmt.Sprintf("req:%d:%s", c.skill, c.name))
		if c.skill == prMagnificat {
			delete(b.buffAt, fmt.Sprintf("req:%d:any", prMagnificat))
		}
		b.buffAt["buffgap"] = now.Add(activeBuffGap)
		b.skillReady = now.Add(1000 * time.Millisecond)
		b.setState(fmt.Sprintf("%s on %s", skillTalk(c.skill), c.name))
		go b.useSkill(c.skill, si.Lv, id)
		return true
	}
	return false
}

func skillTalk(id int) string {
	return map[int]string{alBlessing: "Blessing", alIncAgi: "Increase AGI", prKyrie: "Kyrie Eleison",
		prMagnificat: "Magnificat", prImpositio: "Impositio Manus", hpAssumptio: "Assumptio"}[id]
}

// buffRecv (b.mu held, every packet): buffs landing on us (our own book, for asking) and, for a party healer, buffs
// any caster put on a party member (so it doesn't cast them again). Status icons end our own buffs early.
func (b *Bot) buffRecv(op uint16, pk []byte, now time.Time) {
	if !tune().PriestActiveBuffs {
		return
	}
	var skill, lv int
	var dst, src uint32
	switch {
	case op == 0x09cb && len(pk) >= 17:
		skill, lv = int(binary.LittleEndian.Uint16(pk[2:])), int(int32(binary.LittleEndian.Uint32(pk[4:])))
		dst, src = binary.LittleEndian.Uint32(pk[8:]), binary.LittleEndian.Uint32(pk[12:])
	case op == 0x011a && len(pk) >= 15:
		skill, lv = int(binary.LittleEndian.Uint16(pk[2:])), int(int16(binary.LittleEndian.Uint16(pk[4:])))
		dst, src = binary.LittleEndian.Uint32(pk[6:]), binary.LittleEndian.Uint32(pk[10:])
	case (op == 0x0196 && len(pk) >= 9) || (op == 0x043f && len(pk) >= 9) || (op == 0x0983 && len(pk) >= 9):
		efst, id, on := int(binary.LittleEndian.Uint16(pk[2:])), binary.LittleEndian.Uint32(pk[4:]), pk[8] != 0
		if sk, ok := buffEfst[efst]; ok && id == b.aid && b.aid != 0 && !on {
			delete(b.buffAt, fmt.Sprintf("got:%d", sk))
		}
		return
	default:
		return
	}
	d := activeBuffDur(skill, lv)
	if d == 0 || dst == 0 {
		return
	}
	if dst == b.aid {
		b.buffAt[fmt.Sprintf("got:%d", skill)] = now.Add(d)
		b.buffAt[fmt.Sprintf("gotever:%d", skill)] = now
		return
	}
	if src == b.aid && b.aid != 0 {
		b.buffConfirm(skill, lv, dst, now) // healerstick.go: our own cast landed on a party member
		return
	}
	if !b.isHealer() || skill == prMagnificat {
		return
	}
	if e := b.ents[dst]; e != nil && e.Name != "" { // someone else buffed a player near us: no double cast
		key := fmt.Sprintf("%d:%s", skill, e.Name)
		if renew := now.Add(d - 20*time.Second); renew.After(b.buffAt[key]) {
			b.buffAt[key] = renew
		}
	}
}

// buffWant (b.mu held): what this member lacks and would ask for, as a party line ("" = nothing).
func (b *Bot) buffWant(now time.Time) string {
	if b.Deaths != buffDeathsOf(b) { // died: every buff is gone
		for _, id := range []int{alBlessing, alIncAgi, prKyrie, prMagnificat, prImpositio} {
			delete(b.buffAt, fmt.Sprintf("got:%d", id))
		}
	}
	lack := func(id int) bool { return !now.Before(b.buffAt[fmt.Sprintf("got:%d", id)]) }
	bless, agi := lack(alBlessing), lack(alIncAgi)
	kyrie := partyRole(b) == roleTank && lack(prKyrie) && !b.buffAt[fmt.Sprintf("gotever:%d", prKyrie)].IsZero()
	magni := fragileClass(b.Class) && b.MaxSP > 0 && b.SP*100/b.MaxSP < 30 && lack(prMagnificat) &&
		!b.buffAt[fmt.Sprintf("gotever:%d", prMagnificat)].IsZero()
	switch {
	case magni:
		return pick("magni pls", "sp low, magnificat?", "magnificat pls")
	case kyrie && (bless || agi):
		return pick("kyrie + buffs pls", "kyrie and bless pls", "buffs pls, kyrie too")
	case kyrie:
		return pick("kyrie pls", "kyrie?", "kyrie pls ty")
	case bless && agi:
		return pick("buff pls", "buffs?", "bless agi pls", "can i get buffs?", "bless + agi pls ty")
	case bless:
		return pick("bless pls", "bless?", "blessing pls")
	case agi:
		return pick("agi pls", "agi?", "inc agi pls", "agi pls ty")
	}
	return ""
}

func buffDeathsOf(b *Bot) int {
	buffAsks.Lock()
	defer buffAsks.Unlock()
	d, ok := buffAsks.deaths[b]
	if !ok || d != b.Deaths {
		buffAsks.deaths[b] = b.Deaths
		if !ok {
			return b.Deaths
		}
	}
	return d
}

// buffAsk (b.mu held, party member, from party.go): ask the party's priest for missing buffs in party chat.
func (b *Bot) buffAsk(p *Party, members []pubState, now time.Time) {
	if !tune().PriestActiveBuffs || p == nil || b.isHealer() || b.Dead || b.HP <= 0 || townOf(b.MapName) != "" {
		return
	}
	priest := false
	for _, s := range members {
		if s.Support && s.Name != b.charName() && s.Online && s.Map == b.MapName && !s.Resting && s.HPFrac > 0 &&
			dist(b.X, b.Y, s.X, s.Y) <= 9 {
			priest = true
		}
	}
	line := b.buffWant(now)
	if !priest || line == "" {
		delete(b.buffAt, "asklack")
		return
	}
	if b.buffAt["asklack"].IsZero() {
		b.buffAt["asklack"] = now
		return
	}
	if now.Sub(b.buffAt["asklack"]) < buffAskWait || now.Before(b.buffAt["buffask"]) {
		return
	}
	buffAsks.Lock()
	ok := now.After(buffAsks.at[p])
	if ok {
		buffAsks.at[p] = now.Add(buffAskParty)
	}
	buffAsks.Unlock()
	if !ok {
		return
	}
	b.buffAt["buffask"] = now.Add(time.Duration(90+crand(61)) * time.Second)
	typing := b.P.Typing
	go b.partySay(styled(typing, line))
}

// buffAskParse: the buffs a party line asks for (nil = not a request). "Name : text" -> speaker, skills.
func buffAskParse(msg string) (string, []int) {
	i := strings.Index(msg, " : ")
	if i <= 0 {
		return "", nil
	}
	who, txt := msg[:i], strings.ToLower(strings.TrimSpace(msg[i+3:]))
	if len(txt) > 40 || strings.Contains(txt, "priest") || strings.Contains(txt, "no ") || strings.Contains(txt, "dont") {
		return "", nil // "need a priest for ...", "no agi needed"
	}
	words := strings.FieldsFunc(txt, func(r rune) bool { return !(r >= 'a' && r <= 'z') })
	var out []int
	add := func(id int) {
		for _, x := range out {
			if x == id {
				return
			}
		}
		out = append(out, id)
	}
	for _, w := range words {
		switch {
		case strings.HasPrefix(w, "bless"):
			add(alBlessing)
		case w == "agi" || strings.HasPrefix(w, "agil"):
			add(alIncAgi)
		case strings.HasPrefix(w, "buff"):
			add(alBlessing)
			add(alIncAgi)
		case strings.HasPrefix(w, "kyrie"):
			add(prKyrie)
		case strings.HasPrefix(w, "magni"):
			add(prMagnificat)
		case strings.HasPrefix(w, "impo"):
			add(prImpositio)
		}
	}
	return who, out
}

// buffAskHeard (b.mu held, party chat line): a party healer moves the asker's buffs to the front of its queue.
func (b *Bot) buffAskHeard(msg string, now time.Time) {
	if !tune().PriestActiveBuffs || !b.isHealer() {
		return
	}
	who, skills := buffAskParse(msg)
	if who == "" || who == b.charName() || len(skills) == 0 {
		return
	}
	for _, id := range skills {
		target := who
		if id == prMagnificat {
			target = "any"
			delete(b.buffAt, fmt.Sprintf("%d:party", prMagnificat))
		} else {
			delete(b.buffAt, fmt.Sprintf("%d:%s", id, who)) // they say it's gone: believe them
		}
		b.buffAt[fmt.Sprintf("req:%d:%s", id, target)] = now.Add(buffReqHold)
	}
	b.buffAt["buffreq:"+who] = now.Add(buffReqHold)
	b.buffAt["buffreq"] = now.Add(buffReqHold)
}
