package main

import (
	"fmt"
	"time"
)

// Tanks lead the fight (owner rule 2026-11-14, RULES.md 3: "make the tanks in party be more active and attacking the
// mobs more aggressively instead of sitting behind"). Measured before (61 party tanks, 4 min, 14:05Z): member tanks
// were fighting 38% of the time and "with/following/waiting/regrouping" 27%; in parties led by a non-tank (Sage,
// Alchemist, Hunter...) the leader engaged first ("engaging Coco") while both tanks trailed "following" 7-12 cells
// behind, and a tank-led party's leader kept hitting its own target while a monster chewed on the priest (its peel
// ran only after "current target still around").
//
// The shared rules, for bot-led and human-led parties alike:
//   - tankTakeAggro: any monster targeting another member is taken first - Provoke if known (25% SP kept), else hit
//     it. Runs before the tank's own current target, for member tanks (party.go) and tank leaders (ai.go hunt).
//   - tankFront: the tank pulls up to tank_pull_radius from the leader AND up to that far around a point a few cells
//     ahead of the leader on its walk (the group's front), never beyond together_cells-4 from the leader; with
//     nothing to pull it walks at that front instead of trailing behind.
//   - leaderDefer (ai.go hunt): a non-tank bot leader lets the party tank open the fight: it assists the tank's
//     target, and holds a fresh pull of its own for tankDeferWait so the tank engages first.
//   - tankHoldsRest (ai.go hunt): a party tank doesn't sit down for its own rest while a member is fighting (it
//     still drinks potions; danger.go escape still flees at low HP). Party rests are restGuard's (together.go).

// tankDeferWait: how long a non-tank leader holds a fresh pull for the tank before it engages itself.
const tankDeferWait = 3 * time.Second

// tankFrontAhead: the front point is this many cells ahead of the leader along its walk.
const tankFrontAhead = 5

// tankPeelTarget: the nearest monster (within 12 cells of us) that targets another party member right now.
func (b *Bot) tankPeelTarget(members []pubState, now time.Time) (*Entity, string, int) {
	others := map[uint32]string{}
	for _, m := range members {
		if m.AID != 0 && m.AID != b.aid && m.Map == b.MapName && m.Online {
			others[m.AID] = m.Name
		}
	}
	var peel *Entity
	pd := 99
	for _, e := range b.ents {
		if e.Type != objMob || now.Sub(e.TargetAt) > 4*time.Second || others[e.Target] == "" {
			continue
		}
		ex, ey := e.pos(now)
		if d := dist(b.X, b.Y, ex, ey); d < pd && d <= 12 {
			peel, pd = e, d
		}
	}
	if peel == nil {
		return nil, "", 0
	}
	return peel, others[peel.Target], pd
}

// tankTakeAggro: take whatever targets a party member off it (caller holds b.mu). True when it acted.
func (b *Bot) tankTakeAggro(members []pubState, moving bool, now time.Time) bool {
	peel, who, pd := b.tankPeelTarget(members, now)
	if peel == nil {
		return false
	}
	if si := b.skills[smProvoke]; si != nil && si.Lv > 0 && b.SP-si.SP >= b.MaxSP/4 && pd <= 9 && now.After(b.skillReady) && // keep 25% SP
		now.After(b.buffAt[fmt.Sprintf("prov:%d", peel.ID)]) {
		b.buffAt[fmt.Sprintf("prov:%d", peel.ID)] = now.Add(8 * time.Second)
		b.skillReady = now.Add(700 * time.Millisecond)
		b.setState("provoking " + mobName(peel.Class) + " off " + who)
		go b.useSkill(smProvoke, si.Lv, peel.ID)
		b.target, b.targetAt, b.attackSentFor = peel.ID, now, 0 // fight() re-sends the auto-attack after the taunt
		return true
	}
	if b.target != peel.ID {
		b.target, b.targetAt = peel.ID, time.Time{}
	}
	b.setState("taking " + mobName(peel.Class) + " off " + who)
	b.fight(peel, moving, now)
	return true
}

// tankFront: the group's front - a point tankFrontAhead cells ahead of the leader along the walk it is on (bot
// leaders publish their walk destination; a human leader's position is already where she heads, so no front).
func tankFront(lead pubState) (int, int, bool) {
	lx, ly := livePosOf(lead)
	if lead.DX == 0 && lead.DY == 0 || lx == 0 && ly == 0 {
		return 0, 0, false
	}
	dx, dy := lead.DX-lx, lead.DY-ly
	n := max(abs(dx), abs(dy))
	if n < 3 {
		return 0, 0, false // standing (or nearly there): the leader itself is the front
	}
	k := min(n, tankFrontAhead)
	return lx + dx*k/n, ly + dy*k/n, true
}

// tankPullOK: may the tank pull a monster at (ex, ey) with pull radius r? Within r of the leader, or of the front
// point while still well inside together_cells of the leader (the pull comes back to the group).
func tankPullOK(lead pubState, ex, ey, r int) bool {
	lx, ly := livePosOf(lead)
	d := dist(lx, ly, ex, ey)
	if d <= r {
		return true
	}
	if fx, fy, ok := tankFront(lead); ok && dist(fx, fy, ex, ey) <= r && d <= togetherCells()-4 {
		return true
	}
	return false
}

// A crowd on the tank (party watcher 2026-11-14, pay_dun01): Echobunny pulled 8-12 monsters, then "Fly Wing: losing:
// 12 monsters on me (hp 46%)" teleported it ~130 cells off and left the priest and the wizard alone with a Furious
// Soldier Skeleton. Owner (RULES.md 3): "knight can pull but must use AOE instead of escaping". So:
//   - no pull limit: the tank may pull big groups;
//   - with a crowd on it, its area skills fire back to back from fight() every tick: Bowling Bash / Brandish Spear
//     (weaponskill.go crowd table, 3+ around the centre monster), Magnum Break (ai.go fight, 2+ around), Grand Cross
//     (skillbuff.go). tankAoEReserve keeps the SP for two crowd casts out of single-target skills (Bash), and
//     tankCrowdSP drinks an SP potion when a crowd is on it and the next crowd cast doesn't fit; HP potions come from
//     survive() (potion line 50%+);
//   - tankFallBack: a crowd (tankCrowd+) on it and the priest more than tankPriestCells away -> it walks to the
//     priest, the crowd follows, the heals land and the casters' AoE hits the clump;
//   - tankStands (danger.go escape): a party tank with potions or a priest near is exempt from the "N monsters on me" /
//     "losing" Fly Wing triggers. It escapes only as a last resort: under 20% HP with no potions or no priest near,
//     under 10% HP whatever, against a monster far above it, or a boss. After a wing the regroup (party.go, beyond
//     together_cells) walks it straight back.
const (
	tankCrowd       = 3
	tankPriestCells = 5
)

// tankHeld: monsters on us right now (targeting us in the last 6 s or hit us in the last 4 s).
func (b *Bot) tankHeld(now time.Time) int {
	n := 0
	for _, e := range b.ents {
		if e.Type == objMob && (e.Target == b.aid && now.Sub(e.TargetAt) < 6*time.Second || now.Sub(e.LastHitMe) < 4*time.Second) {
			n++
		}
	}
	return n
}

// tankCrowdCost: SP of the crowd skill the tank would cast (Bowling Bash / Brandish Spear), 0 if it knows none.
func (b *Bot) tankCrowdCost() int {
	cost := 0
	for _, c := range crowdSkills {
		if c.fam != famSword {
			continue
		}
		if si := b.skills[c.id]; si != nil && si.Lv > 0 {
			cost = max(cost, max(si.SP, c.minSP))
		}
	}
	return cost
}

// tankAoEReserve: SP a party tank keeps out of single-target skills: the crowd-skill floor plus two casts.
func (b *Bot) tankAoEReserve() int {
	if partyOf(b) == nil || partyRole(b) != roleTank {
		return 0
	}
	if cost := b.tankCrowdCost(); cost > 0 {
		return b.MaxSP*skillSPFloor/100 + 2*cost
	}
	return 0
}

// tankCrowdSP: a crowd on the party tank and the next crowd cast doesn't fit -> SP potion (caller holds b.mu).
func (b *Bot) tankCrowdSP(held int, now time.Time) {
	if held < tankCrowd {
		return
	}
	if cost := b.tankCrowdCost(); cost > 0 && b.SP-cost < b.MaxSP*skillSPFloor/100 {
		b.drinkSP(now)
	}
}

// tankFallBack: a crowd on us and the priest away -> walk to the priest (caller holds b.mu). True while it owns the tick.
func (b *Bot) tankFallBack(gm *GameMap, members []pubState, moving bool, now time.Time) bool {
	held := b.tankHeld(now)
	b.tankCrowdSP(held, now)
	if held < tankCrowd {
		return false
	}
	mx, my := b.livePos(now)
	if done, owns := b.tankWizardLure(gm, members, mx, my, held, moving, now); done {
		return owns
	}
	var anchor pubState
	found, bd := false, 1<<30
	for _, s := range members {
		if s.Name == b.charName() || !s.Online || s.Map != b.MapName || !s.Support {
			continue
		}
		sx, sy := livePosOf(s)
		if sx == 0 && sy == 0 {
			continue
		}
		if d := dist(mx, my, sx, sy); d < bd {
			anchor, bd, found = s, d, true
		}
	}
	if !found || bd <= tankPriestCells || bd > togetherCells()+5 {
		return false // next to the priest: fight there with AoE; no priest / too far: regroup decides
	}
	sx, sy := livePosOf(anchor)
	b.setState(fmt.Sprintf("bringing %d monsters to %s", held, anchor.Name))
	if moving && now.Sub(b.lastMoveTx) < 1500*time.Millisecond {
		return true
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
		return true
	}
	if bd > 10 && gm != nil {
		b.stepAlong(gm, sx, sy)
		return true
	}
	x, y := sx+2*sign(mx-sx), sy+2*sign(my-sy) // 2 cells from the priest on our side: the crowd follows us in
	if gm != nil && !gm.Walkable(x, y) {
		x, y = sx, sy
	}
	go b.walkTo(x, y)
	return true
}

// tankStands: a party tank keeps fighting a fight escape() calls lost (caller holds b.mu); see the note above.
func (b *Bot) tankStands(v fightView) bool {
	p := partyOf(b)
	if p == nil || partyRole(b) != roleTank {
		return false
	}
	if bossRecent(b.MapName, v.lv) {
		return false
	}
	_, _, members := b.partyView(p, time.Now())
	priest := false
	for _, s := range members {
		if s.Name != b.charName() && s.Online && s.Map == b.MapName && s.Support {
			if sx, sy := livePosOf(s); dist(b.X, b.Y, sx, sy) <= 14 {
				priest = true
			}
		}
	}
	return tankHolds(v, priest)
}

// tankHolds: the escape exemption itself (pure, for the test).
func tankHolds(v fightView, priest bool) bool {
	if v.maxLv > v.lv+8 || v.hp < 0.1 {
		return false // far stronger monster, or about to die: wing out
	}
	if v.pots == 0 && !priest {
		return false // nothing keeps us alive
	}
	if v.hp < 0.2 && (v.pots == 0 || !priest) {
		return false // last resort
	}
	return true
}

// partyTankFor: the bot party's tank that should open fights for this (non-tank) leader: online, on our map, with the
// group, not resting and not hurt. Caller holds b.mu.
func (b *Bot) partyTankFor(p *Party, lx, ly int) (pubState, bool) {
	if p == nil || p.Human != "" || p.Leader != b || partyRole(b) == roleTank {
		return pubState{}, false
	}
	for _, m := range p.Members {
		if m == b || partyRole(m) != roleTank {
			continue
		}
		s, ok := peek(m.P.Name)
		if !ok || !s.Online || s.Map != b.MapName || s.Resting || s.HPFrac < 0.4 {
			continue
		}
		if sx, sy := livePosOf(s); dist(lx, ly, sx, sy) <= tune().TankPullRadius {
			return s, true
		}
	}
	return pubState{}, false
}

// leaderDefer: a non-tank bot leader about to start a fresh fight with pick lets the tank go first (caller holds
// b.mu). Returns true when the leader acted (assisting the tank) or holds this tick.
func (b *Bot) leaderDefer(p *Party, pick *Entity, lx, ly int, moving bool, now time.Time) bool {
	tank, ok := b.partyTankFor(p, lx, ly)
	if !ok {
		return false
	}
	if tank.Target != 0 {
		if e := b.ents[tank.Target]; e != nil && e.Type == objMob {
			b.target, b.targetAt = e.ID, time.Time{}
			b.setState("assisting " + tank.Name)
			b.fight(e, moving, now)
			return true
		}
	}
	if pick == nil {
		return false
	}
	// one hold window per 8 s (a per-monster window restarted whenever the nearest monster changed, and the leader
	// could hold forever on a monster its tank doesn't see or won't take)
	if b.deferAt.IsZero() || now.Sub(b.deferAt) > tankDeferWait+5*time.Second {
		b.deferAt = now
	}
	if now.Sub(b.deferAt) < tankDeferWait {
		b.setState("letting " + tank.Name + " pull " + mobName(pick.Class))
		return true
	}
	return false // the tank didn't take it: engage ourselves
}

// membersFighting: another online member on our map within together_cells is fighting right now.
func (b *Bot) membersFighting(members []pubState) bool {
	for _, s := range members {
		if s.Name == b.charName() || !s.Online || s.Map != b.MapName || s.Target == 0 {
			continue
		}
		if sx, sy := livePosOf(s); dist(b.X, b.Y, sx, sy) <= togetherCells() {
			return true
		}
	}
	return false
}

// tankHoldsRest: a party tank keeps fighting instead of sitting down while a member fights near it (caller holds
// b.mu). Very low HP still rests (and escape flees first).
func (b *Bot) tankHoldsRest() bool {
	p := partyOf(b)
	if p == nil || partyRole(b) != roleTank || b.hpFrac() < 0.3 {
		return false
	}
	_, _, members := b.partyView(p, time.Now())
	return b.membersFighting(members)
}

// ---- Wizard lure (owner 2026-11-14: "if party has a wiz, they should follow that strategy, lure for wiz to cast
// spell and pick off the rest") ----
// A party tank with a crowd on it and an area caster (pubState.AoE) in the party brings the pack to a spot
// wizLureCells in front of the caster - inside its casting range, not on top of it - and holds still there while
// the caster's spell is in the air (pubState.CastUntil), so the pack isn't split before the AoE lands; then the
// party picks off the survivors as usual. Without a caster, tankFallBack brings the crowd to the priest.
const (
	wizLureCells = 4 // the pack's distance in front of the caster
	wizZoneMin   = 2 // the tank (and the pack on it) counts as "in front" from here ...
	wizZoneMax   = 7 // ... to here (cast range 9 leaves room for the pack around the tank)
)

// wizLureSpot: where the tank takes the pack (walk=false: it is already in the zone, fight there).
func wizLureSpot(mx, my, wx, wy int, walkable func(x, y int) bool) (x, y int, walk bool) {
	d := dist(mx, my, wx, wy)
	if d >= wizZoneMin && d <= wizZoneMax {
		return mx, my, false
	}
	dx, dy := sign(mx-wx), sign(my-wy)
	if dx == 0 && dy == 0 {
		dx = 1
	}
	for _, r := range []int{wizLureCells, wizLureCells - 1, wizLureCells + 1, wizZoneMin + 1} {
		x, y = wx+dx*r, wy+dy*r
		if walkable == nil || walkable(x, y) {
			return x, y, true
		}
	}
	return wx + dx*wizZoneMin, wy + dy*wizZoneMin, true
}

// tankWizardLure (b.mu held): done=true when the wizard rule decided this tick (owns: the tank used the tick to walk).
func (b *Bot) tankWizardLure(gm *GameMap, members []pubState, mx, my, held int, moving bool, now time.Time) (done, owns bool) {
	var wiz pubState
	found, bd := false, 1<<30
	for _, s := range members {
		if s.Name == b.charName() || !s.Online || s.Map != b.MapName || !s.AoE || s.Errand || s.Trip {
			continue
		}
		sx, sy := livePosOf(s)
		if sx == 0 && sy == 0 {
			continue
		}
		if d := dist(mx, my, sx, sy); d < bd {
			wiz, bd, found = s, d, true
		}
	}
	if !found || bd > togetherCells()+5 {
		return false, false // no caster near: the priest rule
	}
	wx, wy := livePosOf(wiz)
	if now.Before(wiz.CastUntil) && bd <= aoeCastRange {
		b.setState(fmt.Sprintf("holding %d monsters for %s's spell", held, wiz.Name))
		return true, false // hold still: fight what's on us, don't drag the pack out of the spell
	}
	var walkable func(x, y int) bool
	if gm != nil {
		walkable = gm.Walkable
	}
	x, y, walk := wizLureSpot(mx, my, wx, wy, walkable)
	if !walk {
		b.setState(fmt.Sprintf("holding %d monsters in front of %s", held, wiz.Name))
		return true, false // in the kill zone: fight here, the caster's AoE hits the clump
	}
	b.setState(fmt.Sprintf("luring %d monsters to %s", held, wiz.Name))
	if moving && now.Sub(b.lastMoveTx) < 1500*time.Millisecond {
		return true, true
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
		return true, true
	}
	if dist(mx, my, x, y) > 10 && gm != nil {
		b.stepAlong(gm, x, y)
		return true, true
	}
	go b.walkTo(x, y)
	return true, true
}
