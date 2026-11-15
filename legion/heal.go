package main

// heal.go: healing comes before everything else (QA 2026-11-14: acolytes with full SP fought, rallied, looted and
// travelled next to party mates at 61%, 5%, 3% HP and never healed - the only heal lived in partyThink's support
// branch, which a bot in a rally, a town trip, travel, hunt() (every party LEADER runs hunt, not partyThink) or a
// fight never reached). Called at the top of think() for every bot:
//   healFirst  - an acolyte-line bot heals the most hurt party member it can see (or itself) in ANY state
//   lowHPGuard - any bot at very low HP sits / drinks / flees before walking on (rally, travel, regroup)

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	healOthersBelow = 0.80 // heal a party member under 80% HP
	healSelfBelow   = 0.60 // heal ourselves under 60% (hunt() used the same line)
	healRange       = 9    // Heal's range in cells
	lowHPLine       = 0.25 // below this a bot stops walking on and recovers first
	lowHPDone       = 0.60 // ...until it is back over this
	lowHPTravel     = 0.45 // before travelling / while in town, recover from under this first
)

// healStats: heals the server confirmed per caster (name -> count, total HP), for the API/evidence
var healStats = struct {
	sync.Mutex
	n, hp map[string]int
	at    map[string]time.Time // last confirmed heal per caster (healreach.go healedLately)
}{n: map[string]int{}, hp: map[string]int{}, at: map[string]time.Time{}}

// healFirst (b.mu held): true when this think was spent healing (or standing up to heal).
func (b *Bot) healFirst(moving bool, now time.Time) bool {
	if !b.isHealer() || b.Dead { // Monks/Champions are melee: they heal only themselves (ai.go healUse)
		return false
	}
	si := b.skills[alHeal]
	if si == nil || si.Lv == 0 || now.Before(b.skillReady) {
		return false
	}
	hp := b.hpFrac()
	attacked := b.underAttack(now)
	if attacked && hp < lowHPLine {
		return false // our own life first: escape()/potions in the normal flow
	}
	// candidates: ourselves, and party members (bots + a human leader) on this map within Heal's range that we see
	type cand struct {
		name  string
		id    uint32
		frac  float64
		x, y  int
		reach bool // healreach.go: the server will take a Heal from where we stand
	}
	gm := maps[b.MapName]
	var worst *cand
	if hp < healSelfBelow {
		worst = &cand{b.charName(), b.aid, hp, b.X, b.Y, true}
	}
	if p := partyOf(b); p != nil {
		_, _, members := b.partyView(p, now)
		for _, s := range members {
			if !s.Online || s.Map != b.MapName || s.Name == b.charName() || s.HPFrac <= 0 || s.HPFrac >= healOthersBelow {
				continue
			}
			if worst != nil && s.HPFrac >= worst.frac {
				continue
			}
			id := b.entityID(s.Name)
			if id == 0 {
				continue // not in sight
			}
			x, y := s.X, s.Y
			if e := b.ents[id]; e != nil {
				x, y = e.X, e.Y
			}
			if dist(b.X, b.Y, x, y) > 14 { // beyond sight
				continue
			}
			if now.Before(b.buffAt["healskip:"+s.Name]) && dist(b.X, b.Y, x, y) >= 2 {
				// 3 heals to it got no reply (healreach.go): walk next to it, no more blind sends
				if b.healApproach(s.Name, x, y, true, now) {
					return true
				}
				continue
			}
			worst = &cand{s.Name, id, s.HPFrac, x, y, healReachable(gm, b.X, b.Y, x, y, healRange)}
		}
	}
	if worst == nil {
		return b.cureParty(now) // poison.go: nobody to heal, a poisoned party mate gets Cure
	}
	if !worst.reach { // a wall on the line or out of range: rAthena drops the cast silently - move first
		if b.healApproach(worst.name, worst.x, worst.y, false, now) || b.healApproach(worst.name, worst.x, worst.y, true, now) {
			return true
		}
		return b.cureParty(now)
	}
	if worst.id != b.aid && healUnacked(b.charName(), worst.id) {
		healResetPend(b.charName())
		b.buffAt["healskip:"+worst.name] = now.Add(5 * time.Second)
		return b.healApproach(worst.name, worst.x, worst.y, true, now)
	}
	cost := max(si.SP, 10+3*si.Lv)
	if b.SP < cost {
		if worst.frac < 0.5 {
			b.drinkSP(now) // SP users carry SP potions for exactly this (owner rule)
		}
		return false
	}
	if b.Sitting { // no casting while seated
		if now.Sub(b.sitSentAt) > 700*time.Millisecond {
			b.sitSentAt, b.wantSit = now, false
			go b.stand()
		}
		b.setState("standing up to heal " + worst.name)
		return true
	}
	b.skillReady = now.Add(900 * time.Millisecond)
	b.setState(fmt.Sprintf("healing %s (%d%%)", worst.name, int(worst.frac*100)))
	healSent(b.charName(), worst.id)
	go b.useSkill(alHeal, si.Lv, worst.id)
	return true
}

// healRecv (b.mu held): count the heals the server confirms we cast (ZC_USE_SKILL 0x011a / ZC_USE_SKILL2 0x09cb).
func (b *Bot) healRecv(op uint16, pk []byte) {
	var skill, amount int
	var dst, src uint32
	switch {
	case op == 0x09cb && len(pk) >= 17:
		skill, amount = int(binary.LittleEndian.Uint16(pk[2:])), int(int32(binary.LittleEndian.Uint32(pk[4:])))
		dst, src = binary.LittleEndian.Uint32(pk[8:]), binary.LittleEndian.Uint32(pk[12:])
	case op == 0x011a && len(pk) >= 15:
		skill, amount = int(binary.LittleEndian.Uint16(pk[2:])), int(int16(binary.LittleEndian.Uint16(pk[4:])))
		dst, src = binary.LittleEndian.Uint32(pk[6:]), binary.LittleEndian.Uint32(pk[10:])
	default:
		return
	}
	if skill != alHeal || src != b.aid || src == 0 {
		return
	}
	who := "self"
	if dst != b.aid {
		who = fmt.Sprint(dst)
		if e := b.ents[dst]; e != nil && e.Name != "" {
			who = e.Name
		}
	}
	if dst != b.aid && who != fmt.Sprint(dst) {
		go friendNote(b.charName(), who, fkHeal) // friends.go
	}
	healAcked(b.charName())
	healStats.Lock()
	healStats.n[b.charName()]++
	healStats.hp[b.charName()] += amount
	n := healStats.n[b.charName()]
	healStats.Unlock()
	if now := time.Now(); now.After(b.buffAt["healev"]) {
		b.buffAt["healev"] = now.Add(20 * time.Second)
		b.event("heal", "", fmt.Sprintf("healed %s for %d (heal #%d)", who, amount, n))
	}
}

// lowHPGuard (b.mu held): a bot at very low HP doesn't walk on (rally, travel, regroup, town trip) - it flees or
// drinks if something hits it, otherwise it drinks / sits until it is back over lowHPDone. True while it owns the bot.
// (QA 2026-11-14: Tacorose walked a rally at 3/109 HP, xXLunamageXx at 11/206.)
func (b *Bot) lowHPGuard(gm *GameMap, moving bool, now time.Time) bool {
	if b.Dead || b.MaxHP <= 0 {
		return false
	}
	hp := b.hpFrac()
	_, resting := b.buffAt["lowrest"]
	// QA 2026-11-14 death regression: bots left town / set out on trips at 26-45% HP and died on the way. Before
	// travelling (a goal on another map, a town trip) or while standing in a town, recover from under 45% first.
	entry := lowHPLine
	if (b.Goal.Map != "" && b.Goal.Map != b.MapName) || b.trip != nil || isTown(b.MapName) {
		entry = lowHPTravel
	}
	done := lowHPDone
	if (b.Goal.Map != "" && b.Goal.Map != b.MapName || b.trip != nil) && !isTown(b.MapName) {
		// QA 2026-11-14 (ThePuddinglord, Sage 63, moc_fild03): "travel -> mjo_dun02" -> Argos/Wolves -> Fly Wing at 29%
		// -> rested to 52-57% -> set off -> hit again, 3 cycles in 8 min. Crossing a map with aggressive monsters near
		// our level, or right after an escape here, rest (or drink) to 90% before walking on.
		escaped := len(b.escapes) > 0 && now.Sub(b.escapes[len(b.escapes)-1]) < 10*time.Minute
		if escaped || mapDanger(b.MapName, b.powerLv()) > 0 {
			done = 0.9
		}
	}
	// QA 2026-11-14 16:20 UTC (core): Zen.fox respawned in Payon, rested only to the 45% town line, walked back into
	// pay_dun01 alone and died 55 cells short of Ayla. The rule above skipped towns. After a death (10 min), before
	// setting out for another map or a party elsewhere: back to 90% HP (and 90% SP for a caster) first, in town too.
	spOK := true
	if b.rejoinAfterDeath(now) {
		entry, done = 0.9, 0.9
		spOK = b.MaxSP <= 0 || b.SP*10 >= b.MaxSP*9 || !b.spUser()
	}
	b.rejoinSay(now)
	if hp >= done && spOK || (!resting && hp >= entry && spOK) {
		if resting {
			delete(b.buffAt, "lowrest")
			rally := false
			if p := partyOf(b); p != nil {
				rally = p.rallyInfo().Town != ""
			}
			switch {
			case b.trip != nil || rally || (b.Goal.Map != "" && b.Goal.Map != b.MapName):
				// on the way somewhere: stand and go on (trip/rally/travel walk from here)
				b.wantSit = false
				if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
					b.sitSentAt = now
					go b.stand()
				}
			case b.Goal.Mode == "hunt":
				// hunting: hunt() keeps resting to its own line (wantSit) and stands up itself
			default:
				b.wantSit = false // partyThink stands up when the leader moves on or a fight starts
			}
		}
		return false
	}
	if b.underAttack(now) {
		// something is hitting us: Fly Wing / Butterfly Wing / run (danger.go), else a potion, and the normal fight flow
		if b.escape(gm, hp, moving, now) {
			return true
		}
		if b.selfHealOK() { // healerstick.go: Heal on ourselves beats a potion
			return b.selfHeal(now)
		}
		if hp < 0.4 && now.After(b.potAt) {
			if idx := b.potionIndex(); idx >= 0 {
				b.potAt = now.Add(time.Second)
				go b.useItem(idx)
			}
		}
		return false
	}
	if b.selfHealOK() { // healerstick.go: a bot with Heal never sits to rest for HP - it heals itself
		delete(b.buffAt, "lowrest")
		return b.selfHeal(now)
	}
	if !resting {
		b.buffAt["lowrest"] = now
	}
	// a potion first when we carry plenty (starter potions), or when sitting gives no regen (>= 50% weight), or after
	// sitting 90 s with potions to spare (a respawn at 1 HP sat for minutes: Thornlord 87 samples "low HP")
	longRest := resting && now.Sub(b.buffAt["lowrest"]) > 90*time.Second && b.potionCount() > 10
	// fruit and herbs in the bag first (weight.go: LilStar99 sat at 14% HP in Prontera with Apples, Potatoes and
	// Red Herbs in the bag); food is cheaper than potions and weighs more per HP
	if now.After(b.potAt) {
		if idx := b.snackIndex(); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
			b.setState(fmt.Sprintf("low HP (%d%%): eating", int(hp*100)))
			return true
		}
	}
	if (b.potionCount() > 30 || b.overRegenWeight() || longRest) && now.After(b.potAt) {
		if idx := b.pickPotion(false); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
			b.setState(fmt.Sprintf("low HP (%d%%): drinking a potion", int(hp*100)))
			return true
		}
	}
	if b.overRegenWeight() && b.potionCount() == 0 {
		delete(b.buffAt, "lowrest")
		return false // no way to recover here: carry on (the trip logic sells)
	}
	if b.poisoned(now) { // poison.go: sitting doesn't heal while poisoned (no regen); poisonGuard cures / drinks
		b.target = 0
		b.setState(fmt.Sprintf("poisoned (%d%%): waiting for it to wear off", int(hp*100)))
		return true
	}
	// QA 2026-11-14 11:13 (core, pay_dun01): Zen.fox (Wizard) Fly-Winged away from Ayla's party, sat to rest alone at
	// 10% HP ~80 cells from it and was killed. With the party (its healer, else its leader) on this map and more than
	// 10 cells away, a member doesn't rest alone: it drinks and walks back to them, and rests there.
	if anchor, ok := b.restAnchor(now); ok {
		b.target, b.wantSit = 0, false
		if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
			b.sitSentAt = now
			go b.stand()
		}
		if hp < 0.5 && now.After(b.potAt) {
			if idx := b.pickPotion(false); idx >= 0 {
				b.potAt = now.Add(time.Second)
				go b.useItem(idx)
			}
		}
		if !moving && gm != nil {
			b.regroupStep(gm, anchor, now)
		}
		b.setState(fmt.Sprintf("low HP (%d%%): going back to %s to rest", int(hp*100), anchor.Name))
		return true
	}
	b.target, b.wantSit = 0, true
	if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 2*time.Second && b.basicLv() >= 3 {
		b.sitSentAt = now
		go b.sit()
		if now.After(b.buffAt["lowsay"]) && partyOf(b) != nil {
			// once per rest (10 min at most): asking for a heal only when a party healer is on this map - Thornlord
			// respawned in Prontera and kept asking his party (on mjolnir_06) for heals
			b.buffAt["lowsay"] = now.Add(10 * time.Minute)
			typing := b.P.Typing
			line := pick("low hp, sitting a sec", "almost dead, resting", "resting a bit, brb")
			if b.partyHealerHere(now) {
				line = pick("need heal pls", "heal pls, low hp")
			}
			go b.partySay(styled(typing, line))
		}
	}
	b.setState(fmt.Sprintf("low HP (%d%%): resting before moving on", int(hp*100)))
	return true
}

// ---------------------------------------------------------------- priest SP discipline
// QA 2026-11-14 (Ayla's party, 5+ min standing still, 0 kills): Zendancer84 reached full SP, re-cast Blessing on all
// three members back to back (renewed every 220-260 s whatever the buff's real length), dropped below the 95% sit
// line, sat "sp low", rested to 95% again and repeated. Rules now:
//   - a buff is renewed only when it is about to run out (its real duration minus ~20 s), at most one per 10 s
//   - a buff is cast only when SP stays at or above priestReadySP afterwards: buffs are never a reason to sit
//   - when the party is ready to go (the leader moving off or fighting) SP rest ends at priestReadySP, not 95%

const (
	priestReadySP = 70 // % SP: a buff must leave this much
	priestStandSP = 80 // % SP: a seated healer rests up to this, then stands (priestSitLine)
	priestMoveSP  = 60 // % SP: ...or up to this when the party moves on / fights
	buffGap       = 10 * time.Second
)

// buffDuration: Blessing / Increase AGI last 40 + 20*lv seconds (lv10 = 240 s) in Renewal.
func buffDuration(lv int) time.Duration {
	return time.Duration(40+20*lv) * time.Second
}

// buffRenewAt: when to renew a buff just cast at lv (~20 s before it runs out, a little jitter).
func (b *Bot) buffRenewAt(lv int, now time.Time) time.Time {
	return now.Add(buffDuration(lv) - 20*time.Second + time.Duration(b.rng.Intn(5))*time.Second)
}

// buffOK: the priest may cast this buff now - one buff per buffGap, and SP stays at/over priestReadySP after it.
func (b *Bot) buffOK(si *SkillInfo, now time.Time) bool {
	if tune().PriestActiveBuffs {
		return b.activeBuffOK(si, now) // priestbuff.go PT29
	}
	if si == nil || si.Lv == 0 || b.MaxSP <= 0 || now.Before(b.buffAt["buffgap"]) {
		return false
	}
	return (b.SP-si.SP)*100/b.MaxSP >= max(priestReadySP, tune().PriestBuffMinSP)
}

// priestSitLine: the SP % under which an acolyte in a party sits / keeps sitting. 95% (tuning) while the party is
// idle around the leader; priestReadySP once the leader moves off or fights (the party is ready to go).
//
// Owner 2026-11-14 ("It should max out intelligence and has plenty of sp now. Why is it still resting all the
// time"): Zendancer84, lv70 Priest with 1026 SP, sat constantly because the line was "SP isn't full" (95%). Now a
// hysteresis: a standing healer sits only under priest_sit_below_sp_pct (40%); once seated it rests up to
// priestStandSP (80%), or priestMoveSP (60%) when the leader moves off or fights - never down for SP above ~60%.
func (b *Bot) priestSitLine(lead pubState) int {
	// healerstick.go (owner 2026-11-15: "the priest can heal itself, why resting?"): while the leader walks off or
	// fights, the healer sits only when its SP is really low, and stands up at once otherwise
	if healerStickOn() && lead.Name != "" && (dist(b.X, b.Y, lead.X, lead.Y) > 3 || lead.Target != 0) {
		return healerMoveSitSP
	}
	if !b.Sitting {
		return tune().PriestSitBelowSP
	}
	if dist(b.X, b.Y, lead.X, lead.Y) > 3 || lead.Target != 0 {
		return max(tune().PriestSitBelowSP, priestMoveSP)
	}
	return max(tune().PriestSitBelowSP, priestStandSP)
}

// partyHealerHere (b.mu held): a party member with Heal (acolyte line) is online on our map.
func (b *Bot) partyHealerHere(now time.Time) bool {
	p := partyOf(b)
	if p == nil {
		return false
	}
	_, _, members := b.partyView(p, now)
	for _, s := range members {
		if s.Online && s.Support && s.Map == b.MapName && s.Name != b.charName() {
			return true
		}
	}
	return false
}

// healerIdleSit (b.mu held): a party healer has nothing to do - nobody hurt, no buff due, the leader within 6 cells
// and not walking off, nothing attacking or targeting it - so it sits (owner 2026-11-14: "If it's not moving or
// blessing, it sits"). Not a party rest: the caller sits with state "sitting by ..." (no wantSit, no announcement).
//
// Hysteresis (QA 2026-11-14 13:15: Kira storm sat/stood 15 times in 67 s at full SP behind a roaming leader): it sits
// only once the leader has stood still for idleLeadStill, and once seated it stays at least idleSitMin and keeps
// sitting with the leader up to 9 cells away (6 to sit down) and a threat up to 5 cells away (7 to sit down); a hurt
// member, a buff due, a fight or a threat within 5 cells stand it up at once.
func (b *Bot) healerIdleSit(lead pubState, members []pubState, mx, my, lx, ly int, now time.Time) bool {
	if b.idleLeadPos == [2]int{} {
		b.idleLeadPos, b.idleLeadStill = [2]int{lx, ly}, now.Add(-time.Minute) // first sight: treat as standing
	} else if dist(b.idleLeadPos[0], b.idleLeadPos[1], lx, ly) > 1 {
		b.idleLeadPos, b.idleLeadStill = [2]int{lx, ly}, now
	}
	seated := b.Sitting && !b.idleSitAt.IsZero() && now.Sub(b.idleSitAt) < 10*time.Minute
	minHold := seated && now.Sub(b.idleSitAt) < idleSitMin
	reach, threat := 6, healerThreatCells
	if seated {
		reach, threat = 9, 5
	}
	if lead.Name == b.charName() || lead.X == 0 && lead.Y == 0 || dist(mx, my, lx, ly) > reach && !(minHold && dist(mx, my, lx, ly) <= 12) {
		return false
	}
	if lead.DX|lead.DY != 0 && dist(lx, ly, lead.DX, lead.DY) > 3 && !minHold && (!seated || dist(mx, my, lead.DX, lead.DY) > reach) {
		return false // the leader is walking somewhere: follow
	}
	if !seated && now.Sub(b.idleLeadStill) < idleLeadStill {
		return false // the leader only paused: no sitting down for a moment
	}
	if b.underAttack(now) || b.fightOn(now) || b.poisoned(now) || b.basicLv() < 3 || b.buffDue(members, now) {
		return false
	}
	if b.threatNear(members, threat, now) { // together.go: never seated with monsters coming (QA 11:30 deaths)
		return false
	}
	for _, s := range members {
		if s.Online && s.Map == b.MapName && s.HPFrac < healOthersBelow {
			return false
		}
	}
	return b.HP <= 0 || b.hpFrac() >= healOthersBelow
}

// idleLeadStill / idleSitMin: healerIdleSit hysteresis (see there).
const (
	idleLeadStill = 4 * time.Second
	idleSitMin    = 8 * time.Second
)

// Kyrie Eleison (PR_KYRIE): a barrier that absorbs hits for 120 s (or until broken). The healer keeps it on the party's
// tank(s) first, then the leader (coordinator 2026-11-14: "Kyrie on the tank first, when SP is above the buff floor").
const (
	prKyrie    = 73
	kyrieRenew = 100 * time.Second
)

// kyrieTargets (b.mu held): who gets Kyrie, in order - the party's tanks, then the leader (bot or human).
func (b *Bot) kyrieTargets(p *Party) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, m := range p.Members {
		if m != b && partyRole(m) == roleTank {
			out = append(out, m.charName())
		}
	}
	lead := p.Human
	if lead == "" && p.Leader != nil && p.Leader != b {
		lead = p.Leader.charName()
	}
	if lead != "" && !slicesContains(out, lead) {
		out = append(out, lead)
	}
	return out
}

func slicesContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// partyAnchor (b.mu held): the party member to stay next to - the party's healer on this map, else the leader on this
// map - with its live position and distance. ok=false: no party, we lead it, or nobody of it on this map.
func (b *Bot) partyAnchor(now time.Time) (anchor pubState, d int, ok bool) {
	p := partyOf(b)
	if p == nil {
		return pubState{}, 0, false
	}
	lead, lok, members := b.partyView(p, now)
	me := b.charName()
	if !lok || lead.Name == me {
		return pubState{}, 0, false
	}
	found := false
	healer := b.isHealer()
	for _, s := range members {
		// the party's healer is the anchor - for the healer itself, the party's tank (QA 2026-11-14 19:43-19:56Z: Ayla's
		// priest Zendancer84 had no anchor when Ayla was on the next floor, so every mob made it Teleport to a random cell:
		// 13 teleports, each landing farther from the party, up to 21 monsters on it)
		if s.Online && s.Map == b.MapName && s.Name != me && s.X|s.Y != 0 && (!healer && s.Support || healer && s.Tank) {
			anchor, found = s, true
			break
		}
	}
	if !found && lead.Online && lead.Map == b.MapName && lead.X|lead.Y != 0 {
		anchor, found = lead, true
	}
	if !found { // anyone of the party on this map beats a random cell
		bd := 1 << 30
		for _, s := range members {
			if s.Online && s.Map == b.MapName && s.Name != me && s.X|s.Y != 0 && s.X < 1000 && s.Y < 1000 {
				if d := dist(b.X, b.Y, s.X, s.Y); d < bd {
					anchor, bd, found = s, d, true
				}
			}
		}
	}
	if !found {
		return pubState{}, 0, false
	}
	if anchor.LX|anchor.LY != 0 {
		anchor.X, anchor.Y = anchor.LX, anchor.LY
	}
	return anchor, dist(b.X, b.Y, anchor.X, anchor.Y), true
}

// restAnchor (b.mu held): where a party member at low HP should rest - its party's healer on this map, else its leader
// on this map - when that is more than 10 cells away. ok=false: rest here (alone, the leader itself, nobody of the party
// on this map, or already next to them).
func (b *Bot) restAnchor(now time.Time) (pubState, bool) {
	if isTown(b.MapName) {
		return pubState{}, false
	}
	a, d, ok := b.partyAnchor(now)
	if !ok || d <= 10 {
		return pubState{}, false
	}
	return a, true
}

// rejoinAfterDeath (b.mu held): died in the last 10 minutes and now on the way back - to a goal on another map, or to
// a party that is on another map.
func (b *Bot) rejoinAfterDeath(now time.Time) bool {
	if b.diedAt.IsZero() || now.Sub(b.diedAt) > 10*time.Minute {
		return false
	}
	if b.Goal.Map != "" && b.Goal.Map != b.MapName {
		return true
	}
	if p := partyOf(b); p != nil {
		if lead, ok, _ := b.partyView(p, now); ok && lead.Name != b.charName() && lead.Map != "" && lead.Map != b.MapName {
			return true
		}
	}
	return false
}

// spUser: a class that lives on SP (mage / acolyte line).
func (b *Bot) spUser() bool {
	f := family(b.Class)
	return f == famMage || f == famAcolyte
}

// rejoining (b.mu held): a party member in a dungeon on its way to its party (more than 20 cells off, or the party on
// another map): it walks on and leaves fights early (danger.go: losingFight transit), it doesn't clear the way.
func (b *Bot) rejoining(now time.Time) bool {
	if !strings.Contains(b.MapName, "_dun") || partyOf(b) == nil {
		return false
	}
	if _, d, ok := b.partyAnchor(now); ok {
		return d > togetherCells()
	}
	p := partyOf(b)
	lead, ok, _ := b.partyView(p, now)
	return ok && lead.Name != b.charName() && lead.Map != b.MapName
}

// rejoinSay (b.mu held): a party member that comes into its party's dungeon far from them says so once (owner rule:
// members ask for help, the leader collects them).
func (b *Bot) rejoinSay(now time.Time) {
	if !strings.Contains(b.MapName, "_dun") || now.Before(b.buffAt["rejoinsay"]) {
		return
	}
	a, d, ok := b.partyAnchor(now)
	if !ok {
		// QA 2026-11-14 17:03 UTC (core): after a relog the priest was on pay_dun00, Ayla on pay_dun01 - partyAnchor
		// only sees this map, so nothing was said. The party on another map counts too.
		p := partyOf(b)
		if p == nil {
			return
		}
		lead, lok, _ := b.partyView(p, now)
		if !lok || lead.Name == b.charName() || lead.Map == "" || lead.Map == b.MapName {
			return
		}
		a, d = lead, 999
	} else if d <= togetherCells() {
		return
	}
	// a lv50+ party (together.go partyStrong): a separated member just walks to the leader - "come get me" only when
	// really lost: the leader on another map or more than 40 cells away (QA 20:03-20:05Z: Ayla's friends said it at
	// 16-24 cells, re-triggering her holds; the party crawled)
	if p := partyOf(b); p != nil && partyStrong(p) {
		if lead, lok, _ := b.partyView(p, now); lok && lead.Map == b.MapName {
			lx, ly := livePosOf(lead)
			if lx|ly == 0 || dist(b.X, b.Y, lx, ly) <= 40 {
				return
			}
		}
	}
	b.buffAt["rejoinsay"] = now.Add(5 * time.Minute)
	typing := b.P.Typing
	line := fmt.Sprintf(pick("omw, at %d,%d - come get me pls", "on my way to u, at %d,%d, can u come meet me?", "omw, im at %d,%d"), b.X, b.Y)
	go b.partySay(styled(typing, line))
	b.event("party", "", fmt.Sprintf("on the way to %s on %s (%d cells), asked to be collected", a.Name, b.MapName, d))
}
