package main

import (
	"fmt"
	"sync"
	"time"
)

// Shared boss / mini-boss notes (QA 2026-11-14 08:41-08:45 UTC, pay_fild04: Angeling lv77 and Ghostring lv90 killed
// themintfox, LilEchochan, Zen hunter and lilvexsmith within 22 s, then three more; each logged "killed by a level 77
// monster: avoiding it for 30 min", but that memory was per bot, and Kai_99 walked back in to collect Vex 9913 and both
// died). Now every sighting of a boss-class monster (mob_db Class Boss / MVP) far above the viewer's effective level is
// a map-wide note: it raises the map's decaying danger (recentDeaths, which every map picker, Kafra landing, party-map
// and route cost reads), bots in sight of it leave, and nobody targets monsters or collects members near the spot for
// 10 minutes.

type bossSpot struct {
	mob, x, y int
	at        time.Time
	kill      bool
	champ     bool // a champion (Ringleader / Swift / Solid...), not a Boss-class monster: a local note only
}

// Mini-bosses (QA 2026-11-14 12:25: pay_fild01 killed 6 of 27 dead bots, all lv6-19; 14 deaths to its 2 Willow Ringleaders,
// which summon Willows and use Telekinesis Attack): bots under bossSafeLv treat a Boss-class monster as a threat whatever
// its level (Vocal lv18 on prt_fild07 was "only 8 above" a lv10 bot) - map-wide, like a far-above boss. Champions
// (mob_db AegisName C1_..C5_) are a threat below max(bossSafeLv, their level+15), but only LOCALLY: rAthena spawns 1-5 of
// them on almost every field (prt_fild08 alone has 5), so a map-wide veto would close every beginner field. Bots keep away
// from the spot (no targets within 12 cells, walk off when one comes within champFleeCells) and never target it.
const (
	bossSafeLv     = 25
	champFleeCells = 9
)

// bossThreatMob: monster mb is a threat to a bot of (effective) level lv.
func bossThreatMob(mb *Mob, lv int) bool {
	switch {
	case mb == nil:
		return true
	case mb.Boss:
		// an MVP is a threat until we are far above it (QA 20:49: Eddga, lv65, wiped Ayla's lv72-80 party): leave
		return mb.Level >= lv+10 || lv < bossSafeLv || isMVP(mb.ID) && lv < mb.Level+partyMVPOverLv
	case mb.Champion:
		return lv < max(bossSafeLv, mb.Level+15)
	}
	return false
}

var bossSpots = struct {
	sync.Mutex
	m    map[string][]bossSpot
	last map[string]time.Time // last time a sighting raised the map's danger
}{m: map[string][]bossSpot{}, last: map[string]time.Time{}}

const bossSpotTTL = 10 * time.Minute

// noteBoss records a boss seen (or a kill by one) at x,y on map m.
func noteBoss(m string, mob, x, y int, kill bool) {
	now := time.Now()
	bossSpots.Lock()
	ss := bossSpots.m[m][:0:0]
	for _, s := range bossSpots.m[m] {
		if now.Sub(s.at) < bossSpotTTL && !(!kill && !s.kill && s.mob == mob && dist(s.x, s.y, x, y) <= 10) {
			ss = append(ss, s) // keep (a non-kill sighting of the same boss close by is replaced by the new one)
		}
	}
	champ := mobs[mob] != nil && !mobs[mob].Boss && mobs[mob].Champion
	bossSpots.m[m] = append(ss, bossSpot{mob: mob, x: x, y: y, at: now, kill: kill, champ: champ})
	raise := !champ && (kill || now.Sub(bossSpots.last[m]) > 2*time.Minute) // champions: a local note only (bossSafeLv)
	if raise {
		bossSpots.last[m] = now
	}
	bossSpots.Unlock()
	if raise && !kill { // a kill already counts as a death (reportDeath)
		deathLog.Lock()
		deathLog.m[m] = recentDeathsLocked(m) + 2
		deathLog.t[m] = now
		deathLog.Unlock()
	}
}

// threat: the noted boss / champion is a threat to a bot of level lv (bossThreatMob).
func (s bossSpot) threat(lv int) bool {
	return bossThreatMob(mobs[s.mob], lv)
}

// bossSpotNear: a boss 10+ levels above lv was seen (or killed someone) within r cells of x,y on map m in the last
// 10 minutes.
func bossSpotNear(m string, x, y, r, lv int) bool { return bossSpotNearC(m, x, y, r, lv, lv) }

// bossSpotNearC: as bossSpotNear, champions judged by champLv (bossnote.go champThreat; run 6).
func bossSpotNearC(m string, x, y, r, lv, champLv int) bool {
	bossSpots.Lock()
	defer bossSpots.Unlock()
	for _, s := range bossSpots.m[m] {
		threat := s.threat(lv)
		if s.champ {
			if mb := mobs[s.mob]; mb != nil {
				threat = champThreat(mb, champLv)
			}
		}
		if time.Since(s.at) < s.ttl() && dist(s.x, s.y, x, y) <= r && threat {
			return true
		}
	}
	return false
}

// ttl: how long a note closes its spot to targets. A champion walks around and bossWatch re-notes it every 5 s while
// it is in view, so its sighting holds 30 s (a kill by it 2 min). QA 2026-11-14 10:20: with the 10-minute TTL a
// wandering champion left a trail of 12-cell no-target circles - the leading cause of party leaders "roaming" with
// targets in view (Solid Coco on gef_fild02, Bigfoot Ringleader on pay_fild07, Solid Horn on pay_fild09; 336
// keep-away events in the last ~5,000).
func (s bossSpot) ttl() time.Duration {
	switch {
	case s.champ && s.kill:
		return 2 * time.Minute
	case s.champ:
		return 30 * time.Second
	}
	return bossSpotTTL
}

// bossRecent: a note on map m in the last 10 minutes of a boss 10+ levels above lv.
func bossRecent(m string, lv int) bool {
	bossSpots.Lock()
	defer bossSpots.Unlock()
	for _, s := range bossSpots.m[m] {
		if time.Since(s.at) < bossSpotTTL && !s.champ && s.threat(lv) { // champions never close a whole map
			return true
		}
	}
	return false
}

// bossFar: a boss-class monster 10+ levels above our effective level (any boss below bossSafeLv), or a champion that
// is a threat to us (bossThreatMob).
func (b *Bot) bossFar(e *Entity) bool {
	if e.Type != objMob {
		return false
	}
	mb := mobs[e.Class]
	if mb != nil && mb.Champion && !mb.Boss {
		return champThreat(mb, b.champLv()) && e.Class != b.bossHuntTarget() // run 6: level gap + party strength
	}
	return mb != nil && mb.Boss && bossThreatMob(mb, b.powerLv()) && e.Class != b.bossHuntTarget()
}

// noteBossDeath (b.mu held, on our death): a far-above boss in sight killed us here.
func (b *Bot) noteBossDeath(now time.Time) {
	for _, e := range b.ents {
		if b.bossFar(e) {
			noteBoss(b.MapName, e.Class, b.X, b.Y, true)
			return
		}
	}
}

// bossWatch (b.mu held, every tick before survive): note far-above bosses in sight; when one is within 14 cells, leave
// before it reaches us (Fly Wing, Butterfly Wing after a recent Fly Wing, else run) and drop an auto hunting map.
func (b *Bot) bossWatch(gm *GameMap, moving bool, now time.Time) bool {
	if b.Dead || isTown(b.MapName) || len(b.ents) == 0 {
		return false
	}
	var near *Entity
	nd, nx, ny := 99, 0, 0
	for _, e := range b.ents {
		if !b.bossFar(e) {
			continue
		}
		ex, ey := e.pos(now)
		if d := dist(b.X, b.Y, ex, ey); d < nd {
			near, nd, nx, ny = e, d, ex, ey
		}
	}
	if near == nil {
		return false
	}
	if now.Sub(b.buffAt["bossnote"]) > 5*time.Second {
		b.buffAt["bossnote"] = now
		noteBoss(b.MapName, near.Class, nx, ny, false)
	}
	b.escCheck(now) // escapeitem.go: did the last wing work?
	if mb := mobs[near.Class]; mb != nil && mb.Champion && !mb.Boss {
		// a champion (bossSafeLv): walk off on foot and keep hunting elsewhere on the map - no wing, no map ban (every
		// field has some); hunt's target picker skips monsters within 12 cells of the noted spot
		if nd > champFleeCells || now.Sub(b.buffAt["bossflee"]) < 2*time.Second {
			return false
		}
		b.buffAt["bossflee"] = now
		b.target, b.path = 0, nil
		why := fmt.Sprintf("a %s nearby", mobName(near.Class))
		if now.Sub(b.buffAt["bosssay"]) > time.Minute {
			b.buffAt["bosssay"] = now
			b.event("info", "", fmt.Sprintf("a level %d %s %d cells away on %s: keeping away", mb.Level, mobName(near.Class), nd, b.MapName))
		}
		if !moving {
			b.runFrom(gm, nx, ny)
		}
		b.setState("keeping away (" + why + ")")
		return true
	}
	if nd > 14 || now.Sub(b.buffAt["bossflee"]) < 3*time.Second {
		return false // far enough, or already leaving (survive still drinks potions meanwhile)
	}
	b.buffAt["bossflee"] = now
	lvl := 0
	if mb := mobs[near.Class]; mb != nil {
		lvl = mb.Level
	}
	b.target, b.path = 0, nil
	if b.Goal.Mode == "hunt" && b.Goal.Map == b.MapName && (b.Goal.Set == "auto" || b.Goal.Set == "") {
		b.badMap, b.badMapUntil = b.MapName, now.Add(30*time.Minute)
		b.Goal, b.route = Goal{}, nil
	}
	if now.Sub(b.buffAt["bosssay"]) > 30*time.Second {
		b.buffAt["bosssay"] = now
		b.event("info", "", fmt.Sprintf("a level %d %s %d cells away on %s: leaving", lvl, mobName(near.Class), nd, b.MapName))
	}
	why := fmt.Sprintf("a level %d %s nearby", lvl, mobName(near.Class))
	switch {
	case now.Sub(b.buffAt["flywing"]) < 20*time.Second && b.escItemOK(itemButterflyWing, now):
		b.useEscapeItem(itemButterflyWing, now, "boss: "+why)
		b.setState("fled to town (" + why + ")")
	case b.escItemOK(itemFlyWing, now):
		b.buffAt["flywing"] = now
		b.useEscapeItem(itemFlyWing, now, "boss: "+why)
		b.setState("teleported away (" + why + ")")
	default:
		if !moving {
			b.runFrom(gm, nx, ny)
		}
		b.setState("running away (" + why + ")")
	}
	return true
}

// champLv (b.mu held): the level this bot measures a champion (Ringleader / Swift / Solid ...) against. Run 6 used
// the gear level unless a tank AND a healer were near; run 7 (class QA after the 14:55 deploy: the gef_fild05 Creamy
// party - Nova.fire, Archer 46 - still kept away from the lv23 Swift Creamy, and partied tank xXDarkheartXx lv37 from
// a lv29 Bigfoot Ringleader on pay_fild07; 55 Ringleader keep-aways in 7 min): the gear level is a harsh estimate
// (an Archer 46 with a plain bow reads ~30), so solo it counts at least 3/4 of the base level; with a party member on
// the map the base level; with a tank and a healer on the map base + 5.
func (b *Bot) champLv() int {
	base := b.BaseLvOr()
	lv := max(b.powerLv(), base*3/4)
	p := partyOf(b)
	if p == nil {
		return lv
	}
	tank, heal, mates := partyRole(b) == roleTank, b.isHealer(), 0
	for _, m := range p.Members {
		if m == b {
			continue
		}
		if s, ok := peek(m.P.Name); ok && s.Online && s.Map == b.MapName {
			mates++
			tank = tank || s.Tank
			heal = heal || s.Support
		}
	}
	switch {
	case mates > 0 && tank && heal:
		lv = max(lv, base+5)
	case mates > 0:
		lv = max(lv, base)
	}
	return lv
}

// champThreat: a champion is a threat when the champion level (champLv) is under its level + 5, and always below
// bossSafeLv. (Run 6: level + 10; before that + 15 on the gear level for everyone.)
func champThreat(mb *Mob, champLv int) bool {
	return champLv < max(bossSafeLv, mb.Level+5)
}

// goalMob (b.mu held): this monster drops the bot's current farm goal item (lfg.go farmGoal; a goal party shares it).
func (b *Bot) goalMob(class int) bool {
	goals := []*farmGoal{b.fgoal}
	lookupOnce.Do(loadLookup)
	ex := mobExtras[class]
	if ex == nil {
		return false
	}
	for _, g := range goals {
		if g == nil || g.ItemID == 0 {
			continue
		}
		for _, d := range ex.Drops {
			if d.Item == g.ItemID && !d.Mvp {
				return true
			}
		}
	}
	return false
}
