package main

// Algorithmic behaviour (the 99%): routing, hunting, looting, resting, potions, respawn, roaming, AFK breaks.
// Strategy (which map, party, trading...) comes from Goal, set by the mastermind via the API.

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- shared state

var stats struct {
	online, kills, deaths, loots, loginFail, pktsIn int64
}

var loginGate chan struct{}

// claims stop bots from piling onto (or stealing) the same monster.
var claims = &claimTable{m: map[string]map[uint32]claim{}}

type claim struct {
	bot uint32
	at  time.Time
}
type claimTable struct {
	mu sync.Mutex
	m  map[string]map[uint32]claim
}

func (c *claimTable) try(mapName string, mob, bot uint32, steal bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	mm := c.m[mapName]
	if mm == nil {
		mm = map[uint32]claim{}
		c.m[mapName] = mm
	}
	if cl, ok := mm[mob]; ok && cl.bot != bot && time.Since(cl.at) < 20*time.Second && !steal {
		return false
	}
	mm[mob] = claim{bot, time.Now()}
	return true
}

// holder: the bot holding a fresh claim on the monster (0 = none)
func (c *claimTable) holder(mapName string, mob uint32) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.m[mapName][mob]; ok && time.Since(cl.at) < 20*time.Second {
		return cl.bot
	}
	return 0
}

// partyClaim (b.mu held): the monster is claimed by one of our own party members - we may join that fight (QA
// 2026-11-14 10:20: 20% of the leaders' roaming time they saw only "claimed" monsters, many of them their own
// members' fights)
func (b *Bot) partyClaim(mob uint32) bool {
	h := claims.holder(b.MapName, mob)
	if h == 0 || h == b.aid {
		return false
	}
	p := partyOf(b)
	if p == nil {
		return false
	}
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && s.AID == h {
			return true
		}
	}
	return false
}

// free: nobody else holds a fresh claim on the monster.
func (c *claimTable) free(mapName string, mob, bot uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cl, ok := c.m[mapName][mob]
	return !ok || cl.bot == bot || time.Since(cl.at) >= 20*time.Second
}

func (c *claimTable) release(mapName string, mob, bot uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mm := c.m[mapName]; mm != nil {
		if cl, ok := mm[mob]; ok && (cl.bot == bot || bot == 0) {
			delete(mm, mob)
		}
	}
}

// map occupancy so bots spread out over hunting grounds
var occupancy = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

func occ(m string, d int) int {
	occupancy.Lock()
	defer occupancy.Unlock()
	occupancy.m[m] += d
	return occupancy.m[m]
}

// ---------------------------------------------------------------- loop

func (b *Bot) aiLoop(quit <-chan struct{}) {
	keep := time.NewTicker(time.Duration(10000+b.rng.Intn(4000)) * time.Millisecond)
	defer keep.Stop()
	think := time.Duration(250+b.P.ReactionMs/3) * time.Millisecond
	t := time.NewTimer(time.Duration(b.rng.Intn(2000)) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-quit:
			b.leaveMap()
			return
		case <-keep.C:
			b.safely("keepalive", b.keepalive)
		case <-t.C:
			b.safely("think", b.think) // safety.go: a panic is logged and this bot reset, never the whole service
			t.Reset(think + time.Duration(b.rng.Intn(150))*time.Millisecond)
		}
	}
}

var occMap sync.Map // bot -> map it counts toward

func (b *Bot) leaveMap() {
	if v, ok := occMap.LoadAndDelete(b.aid); ok {
		occ(v.(string), -1)
	}
}

func (b *Bot) setState(s string) { b.State = s }

func dist(x0, y0, x1, y1 int) int { return max(abs(x1-x0), abs(y1-y0)) }

func (b *Bot) think() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.meterTick(now) // meter.go: time budget (the last tick's state gets the time since)
	if left := b.MoveUntil.Sub(now); left > 30*time.Second {
		// no walk of ours lasts 30 s (segments are ~11 cells): a bogus move time would keep `moving` true and every
		// walker (tripThink, kafraThink, travel) waiting for it - QA 2026-11-14 bots frozen at prontera 47,226
		if b.tracing() {
			b.tracef("move time %v left (%d,%d -> %d,%d, speed %d): cleared", left, b.X, b.Y, b.DestX, b.DestY, b.Speed)
		}
		b.MoveUntil = now
	}
	if now.After(b.MoveUntil) {
		b.X, b.Y = b.DestX, b.DestY
	}
	b.teleCheck(now) // teleport.go: a skill Teleport that left us on the same cell marks the map no-teleport
	moving := now.Before(b.MoveUntil)
	gm := maps[b.MapName]

	// gearpower.go tradeGuard: a trade window freezes walking and item use; drop it the moment a monster hits us
	// (or HP < 70% with the window open), so survive() below can drink / wing / run in this same tick
	if !b.Dead {
		b.tradeGuard(now)
	}
	if b.tradeThink(moving, now) {
		return
	}

	// warp-loop detector: bouncing between the same two maps (A, B, A, B within a minute) means the warp we took is
	// broken or leads back; mark it bad for everyone for 30 minutes and route around it
	if b.MapName != "" && b.MapName != b.prevMap {
		if t := townOf(b.MapName); t != "" && now.Sub(b.buffAt["respawn"]) < 20*time.Second {
			b.noteSaved(t) // a respawn lands on the save point: now we know it (it was assumed to be the home town)
		} else if t != "" && t == b.MapName && now.Sub(b.escWingAt) < 15*time.Second {
			b.noteSaved(t) // a Butterfly Wing lands on the save point too
		}
		// the warp actually taken (b.lastWarp is only the last route portal stepped on, often from another map
		// minutes ago: the detector used to ban random portals server-wide - prontera's west gate 22,203 for a
		// prt_cas <-> prontera loop - which broke routing for everyone: "no route", detours, more loops)
		took, ok := warpTaken(b.prevMap, b.MapName, b.X, b.Y)
		intended := ok && took == b.lastWarp
		prevIntended := b.prevLegIntended
		b.prevLegIntended = intended
		b.prevMap = b.MapName
		via := ""
		if ok {
			via = warpKey(took.From, took.X, took.Y)
		}
		// a Butterfly Wing, a Kafra teleport or a respawn is not a portal: QA 2026-11-14 16:12 UTC LilMoonsage Bless
		// (lv6) went alberta -BW/Kafra-> pay_fild03 -> alberta and walked out on a shop trip: "warp loop pay_fild03 <->
		// alberta, avoiding 392,63" (the way to the shop). warpLoopLegs ignores such legs.
		if now.Sub(b.escWingAt) < 10*time.Second || now.Sub(b.kafraLast.At) < 5*time.Second || now.Sub(b.buffAt["respawn"]) < 20*time.Second {
			via = "tele"
		}
		b.mapHist = append(b.mapHist, mapVisit{m: b.MapName, at: now, via: via})
		if len(b.mapHist) > 8 {
			b.mapHist = b.mapHist[len(b.mapHist)-8:]
		}
		if n := len(b.mapHist); n >= 4 && ok {
			h := b.mapHist[n-4:]
			dest := b.Goal.Map
			if b.viaTown != "" {
				dest = b.viaTown
			}
			// warploop.go warpLegProgress: a leg that brought us closer to where we are going is travel, not a loop
			// (QA 14:34: guild02 members bound for mjo_dun02 came back to Geffen for the Kafra, were refused, walked
			// out through 119,213 again - "loop", Geffen's north exit banned, and they bounced Geffen <-> gef_fild04)
			if warpLoopLegs(h) && now.Sub(h[0].at) < 2*time.Minute && !warpLegProgress(took, b.X, b.Y, dest) {
				// ban only when BOTH legs were route portals (our own routing ping-pongs). When the way back was an
				// accident (walked into the return portal's area right after landing) the leg we meant to take is the
				// good warp: banning it sent bots on long detours (QA 2026-11-14: alberta 15,234, geffen 119,213)
				// Never ban a town building's door (warpLoopBannable): re-entering a shop room is our own trip logic
				// (wrong room, a second shop), not a broken warp, and a banned door hid the shop from every route.
				// and never the warp the current plan walks through next: then the plan itself ping-pongs - re-plan only
				if intended && prevIntended && warpLoopBannable(took) && !warpOnPlan(took, dest) { // route around it (routeMaps still uses it when it is the only way on)
					badWarps.Store(warpKey(took.From, took.X, took.Y), now.Add(warpLoopBanFor))
					hopCacheReset()
					b.event("info", "", fmt.Sprintf("warp loop %s <-> %s: avoiding the warp at %s %d,%d", h[0].m, h[1].m, took.From, took.X, took.Y))
				} else if at, seen := b.buffAt["warploopnote"]; !seen || now.Sub(at) > 5*time.Minute {
					// we walked onto it by accident (FindPath/stepAlong avoid trigger cells), a town gate, or the plan
					// itself: just re-plan (once-per-5-min note: the old "avoiding the warp" line here banned nothing)
					b.buffAt["warploopnote"] = now
					b.event("info", "", fmt.Sprintf("warp loop %s <-> %s: re-planning (warp at %s %d,%d not banned)", h[0].m, h[1].m, took.From, took.X, took.Y))
				}
				b.route, b.path, b.mapHist = nil, nil, nil
			}
		}
	}

	// ghost-target watchdog (found by tools/legion/classprobe.py: 13 of 14 sampled bots stood frozen "fighting Pupa"
	// or "following" a frozen leader, 0 kills in 3 min): a target the server never lets us act on - gone out of
	// sight, died elsewhere, unreachable - is dropped after 10 s so the bot (and the party waiting on it) moves on
	if b.target != b.tgtID {
		b.tgtID, b.tgtSince, b.hitTargetAt = b.target, now, now
	} else if b.target != 0 && !moving && now.Sub(b.tgtSince) > time.Duration(tune().GhostTargetSec)*time.Second && now.Sub(b.hitTargetAt) > time.Duration(tune().GhostTargetSec)*time.Second {
		delete(b.ents, b.target)
		b.target, b.tgtID = 0, 0
	}

	// dead -> respawn after a human-ish pause
	if b.Dead {
		b.setState("dead")
		if b.restUntil.IsZero() {
			b.restUntil = now.Add(time.Duration(3000+b.rng.Intn(6000)) * time.Millisecond)
		} else if now.After(b.restUntil) {
			b.restUntil = time.Time{}
			b.Dead, b.target, b.path, b.route = false, 0, nil, nil
			go b.respawn()
			b.buffAt["respawn"] = now
			b.deathLoopCheck(now)
		}
		return
	}

	defer b.publish()
	b.stuckWatch(gm, moving, now) // rally.go: trying to walk but not moving for 20 s -> unstick
	b.progress(now)               // spend stat / skill points
	b.acceptInvite()              // answer party invitations
	b.partyLootShare(now)         // together.go: party loot is free for all (owner rule)
	b.guildThink(now)
	if b.pin != nil {
		b.humanPartySync(now)
	}
	// heal.go: healing a party mate (acolyte line) and recovering from very low HP come before everything else
	// danger.go survive: potion below the line / Fly Wing / run when losing - in EVERY state (party defend, "fighting off
	// X on the way", regroup, travel, rally, trip), before anything else (QA 2026-11-14: those paths had no potion/flee)
	b.noteMobSight(now)                                             // seen.go: per-map monsters-in-view average (empty maps drop out of the pickers)
	if b.bossWatch(gm, moving, now) || b.survive(gm, moving, now) { // bossnote.go: leave before a far-above boss reaches us
		return
	}
	if b.poisonGuard(gm, moving, now) || b.healFirst(moving, now) || b.selfKyrie(now) || b.lowHPGuard(gm, moving, now) { // healerstick.go: Kyrie on the priest itself
		return
	}
	// economy.go: meeting another player for a trade (bought or sold on #trade) owns the bot until done or timed out
	if b.marketThink(gm, moving, now) {
		return
	}
	// reset.go: a natural build reset at the Hypnotist in Izlude (rare; stores and fetches the bot's things)
	if b.resetThink(gm, moving, now) {
		return
	}
	// alchemy.go: a guild alchemist brews potions and fetches its supplies
	if b.alchemyThink(gm, moving, now) {
		return
	}
	// vendbuy.go: buying something it asked for from a vending shop at the south gate
	if b.vendBuyThink(gm, moving, now) {
		return
	}
	// refine.go: refining the weapon at Hollgrehenn after a Prontera town trip (economy batch 14)
	if b.refineThink(gm, moving, now) {
		return
	}
	// peco.go: a Knight / Crusader with Riding in Prontera rents its Peco Peco from the breeder (one-time)
	if b.pecoThink(gm, moving, now) {
		return
	}

	// vendors set up shop and stay there (no AFK wandering); Starveil's fighter-trader runs shop shifts between
	// farming sessions (humanguildtrader.go)
	if b.charRole() == "vendor" && b.Goal.Set != "api" && b.canVend() || b.traderShift(b.PartyID != 0, now) {
		b.vendThink(gm, moving, now)
		return
	}

	// AFK breaks (casual players walk away from the keyboard)
	if now.Before(b.afkUntil) {
		b.setState("afk")
		return
	}
	if b.rng.Float64() < b.P.AFKChance/600 && b.trip == nil && partyOf(b) == nil { // occasional; grinding comes first
		b.afkUntil = now.Add(time.Duration(30+b.rng.Intn(240)) * time.Second)
		return
	}

	// session routine: hang out in town after logging in; drift back to town before logging off
	if b.Goal.Set != "api" && b.charRole() == "main" && partyOf(b) == nil {
		if now.Before(b.hangoutUntil) && isTown(b.MapName) {
			b.setState("hanging out in " + b.MapName)
			if b.saleRoom == "" && !b.saleTried && !moving {
				b.saleTried = true
				if b.rng.Float64() < b.tactics().Sell {
					b.openSaleRoom()
				}
			}
			if b.saleRoom == "" && !moving && gm != nil && b.rng.Float64() < 0.04 {
				b.roam(gm)
			}
			return
		}
		if b.saleRoom != "" { // done hanging out: close the shop sign and go grind
			b.closeSaleRoom()
		}
		if !b.sessionEnd.IsZero() && now.After(b.sessionEnd.Add(-4*time.Minute)) && !isTown(b.MapName) && b.trip == nil && !b.alwaysOnline() {
			b.startTrip("calling it a day", now)
		}
	}

	// town trips: sell loot, restock potions
	// party members (pinned to a human too) look after themselves and say so in party chat (owner: "let the party
	// member go take care of themselves whenever they need to. Just make sure they communicate")
	if b.trip == nil && !b.underAttack(now) && b.target == 0 {
		p := partyOf(b)
		if p != nil {
			// the human leader is in town: that's a town run for everyone
			if p.Human != "" {
				// only when the human is actually shopping (inside a town building): just walking through town, or
				// waiting at the Kafra for a rally, is no town run (owner: ~90% of online time must be farming; Ayla
				// passing through Payon dragged everyone into town trips ~10 times an hour)
				if h, ok := humanAt(p.Human); ok && now.Sub(h.At) < 2*time.Minute {
					if t := townOf(h.Map); t != "" && t != h.Map {
						p.callTownRun(t, p.Human, now)
					}
				}
			}
			// join the party's town run once: with business (loot, potions, gear) do it now, otherwise come along
			if at, town, by := p.townRun(); !at.IsZero() && now.Sub(at) < 2*time.Minute && b.townRunSeen != at {
				b.townRunSeen = at
				b.humanAskAt = time.Time{} // the party went to town: the ask is answered
				b.tripAgain = now.Add(3 * time.Minute)
				b.forceTown = town
				if it := b.bestGearIn(town); it != nil && !b.overRegenWeight() { // overweight: sell first (no regen)
					b.gearPick = it.ID
					b.startTrip("gear", now) // buy the best affordable upgrade this town sells on the party run
				} else if b.hasLoot() || b.needsPots() || b.needTrip() != "" || b.noArrows() {
					// a bow user with 0 arrows buys them on EVERY town visit (t.Ammo rides along; "with the party" visits
					// no shop): dark_storm20 joined party runs with a Bow in the bag and never bought arrows
					b.startTrip("town run with the party", now)
				} else if by != b.charName() {
					b.startTrip("with the party", now)
				}
				b.forceTown = ""
			}
		}
		// economy run 9: standing in a town (a rally, waiting at the Kafra, passing through) with the money for a piece
		// this town sells: buy it now - any town visit, not only a party "town run"
		if b.trip == nil && b.errand == nil && b.kafra == nil && now.After(b.buffAt["townbuy"]) {
			// a party member only on the party's town run in this town (owner: members leave only for necessary steps;
			// side gear waits for the next real-need run - QA 14:00 "brb shopping, need eye patch" scattered Ayla's party)
			runHere := p == nil
			if p != nil {
				if at, rt, _ := p.townRun(); !at.IsZero() && now.Sub(at) < 10*time.Minute && rt == townOf(b.MapName) {
					runHere = true
				}
			}
			if town := townOf(b.MapName); runHere && town != "" && town == b.MapName {
				b.buffAt["townbuy"] = now.Add(90 * time.Second)
				if it := b.townBuyNow(town); it != nil {
					b.gearPick, b.forceTown = it.ID, town
					b.startTrip("gear", now)
					b.forceTown = ""
					if b.trip != nil {
						b.event("info", "", fmt.Sprintf("in %s with the money: buying %s now (%dz)", town, it.Name, it.Buy))
						if p != nil {
							typing := b.P.Typing
							go b.partySay(styled(typing, pick("1 sec, buying my "+it.Name, "brb grabbing a "+it.Name+" here", "gonna buy "+it.Name+" quick")))
						}
					} else {
						b.buffAt["townbuy"] = now.Add(10 * time.Minute)
					}
				}
			}
		}
		if b.trip == nil {
			if why := b.needTrip(); why != "" && now.After(b.tripAgain) {
				b.tripAgain = now.Add(3 * time.Minute)
				// in a party, town is a party thing (owner: "party should prioritize town shop together instead of
				// random"): ask for a party town run; only an emergency (bag >= 90%, no potions at all) goes alone
				urgent := (b.MaxWeight > 0 && b.Weight*100/b.MaxWeight >= tune().TripUrgentWeightPct) || b.potionCount() == 0
				// over the regen line while already in a town with a shop: selling here is no trip away from the party
				// (Zendancer84 sat at 85% weight in Payon, unable to regen SP, while the party waited)
				urgent = urgent || (b.overRegenWeight() && potionShop(townOf(b.MapName)) != nil)
				// no usable weapon / no class weapon (a Knife in a Mage's hand): necessary buys. Solo: one trip to the
				// nearest shop. In a party they are a PARTY town run, never a solo trip (owner: town runs together; Zen.fox
				// left Ayla's party in the cave for "buying Rod (necessary: class weapon)" and the party split).
				// "class weapon (sell loot)": the class weapon is paid for by selling junk loot on the same trip (builds.go)
				weaponless, classW := why == "no weapon", why == "class weapon" || why == "class weapon (sell loot)"
				noArrows := why == "no arrows" || why == "no arrows (sell loot)" // ammo.go: a bow without arrows can't attack
				// an Archer / Bard / Dancer without any bow, instrument or whip can't do its job: an emergency like
				// "no weapon" (QA: Ravenrogue, Archer lv36 with 2055z, kept a Knife 14 min one map from a Bow)
				bowless := classW && b.bowless()
				urgent = urgent || p == nil && (weaponless || classW || noArrows)
				// a solo bot already standing in the town with the shop buys its next step there (needTrip: solo only)
				urgent = urgent || why == "gear in town"
				// only a real need calls a party town run (bag full, out of potions, a missing weapon); gear and loot
				// selling wait for the next real run (owner: Ayla "went back to town without any real needs")
				// "bag is heavy" (weight.go): >= 50% weight with loot to sell is a real need for every bot and party (owner
				// 2026-11-14: "a 50% weight should immediately trigger a town run"); one party run serves all members
				realNeed := why == "bag is full" || why == "bag is heavy" || why == "bag is heavy (store)" || why == "out of potions" || weaponless || classW || noArrows
				// at most one non-urgent town trip per trip_min_interval_min (45): farming comes first (owner: "90% of
				// the time should be on farming"); a party run joined in between counts as a trip too
				tooSoon := !b.lastTripAt.IsZero() && now.Sub(b.lastTripAt) < time.Duration(tune().TripMinIntervalMin)*time.Minute
				// owner 2026-11-14: "a 50% weight should immediately trigger a town run" - no trip interval, no waiting for
				// the next party run (QA 13:10: 54 of 970 bots farmed on at 51-64% weight)
				heavyBag := why == "bag is heavy" || why == "bag is full" || why == "bag is heavy (store)"
				// out of potions never waits (QA 20:17: pixelhunter, a party leader, logged "town need waits for the next
				// party run: out of potions" and died twice on gef_fild10; kiraknight likewise on prt_fild10): no trip
				// interval, the short party-run gap, and alone when the party can't go now
				potNeed := why == "out of potions"
				// PT24 for the healer (healerstick.go): no solo town trip while the leader is out farming - only a bag at
				// the urgent weight line goes alone; the rest waits for the party's run
				healerStays := p != nil && healerStickOn() && healerTripBlocked(b.isHealer(), b.leaderOut(p, now), b.MaxWeight > 0 && b.Weight*100/b.MaxWeight >= tune().TripUrgentWeightPct)
				if healerStays {
					urgent, potNeed = false, false
				}
				switch {
				case p != nil && (classW && !bowless || noArrows) && !urgent && b.errandCanLeave(p, now) && errandFieldMap(b.MapName):
					// economy run 8: a short errand buys it right after this (errand.go "weapon"), no waiting line
				case tooSoon && !urgent && !weaponless && !bowless && !noArrows && !heavyBag && !potNeed:
					// keep farming; the need waits for the interval (or turns urgent). Bare hands don't wait.
					if realNeed && p != nil {
						b.waitTownSay(why, now) // a party member says (and logs) why it waits, once per 30 min
					}
				case p == nil || why == "calling it a day" || urgent:
					b.startTrip(why, now)
				case !realNeed:
					// batched into the next party town run
				case p.Human != "" && classW:
					// human-led: ask once per 30 min; the weapon is bought on the leader's next town run (bestGearIn)
					if now.Sub(b.classAskAt) >= 30*time.Minute {
						b.classAskAt = now
						b.noteGearStep(now)
						if it := b.weaponFor(false); it != nil {
							typing := b.P.Typing
							go b.partySay(styled(typing, pick("need a "+it.Name+" next time we're in town",
								"can i grab a "+it.Name+" on our next town run?", "still using a knife, need a "+it.Name+" when we go to town")))
						}
					}
				case p.Human != "" && realNeed && !healerStays && !b.humanAskAt.IsZero() && now.Sub(b.humanAskAt) > 10*time.Minute:
					// asked for 10+ min and the human leader never took the party to town: a real need (no potions, bag
					// full) can't wait forever - a short solo trip, said in party chat (startTrip announces it), back after
					b.humanAskAt = time.Time{}
					b.event("info", "", "party: asked for a town run 10+ min ago, nobody went: going alone ("+why+")")
					b.startTrip(why, now)
				case p.Human != "":
					// a human leads: the human decides when and where the party goes to town (members ran off to
					// Prontera on their own and Ayla's party was split for 8+ min); ask, then go along when they do
					if b.humanAskAt.IsZero() {
						b.humanAskAt = now
					}
					typing, sw := b.P.Typing, spokenWhy(why)
					go b.partySay(styled(typing, pick("need a town run, "+sw, sw+", can we go to town?", "town run? "+sw)))
				case noArrows && p.callTownRun(b.arrowTown(b.partyRunTown(p)), b.charName(), now):
					typing := b.P.Typing
					go b.partySay(styled(typing, pick("out of arrows, need a town run", "no arrows left, can we go to town?", "town run? i'm out of arrows")))
				case classW && !bowless:
					// a class weapon (or a weapon far below the level) can wait: it is bought on the party's next
					// real-need run (bestGearIn ranks it first). It never calls a run itself (QA 2026-11-14: ~7 class
					// weapon calls per party-hour, 6 min apart, every member winging to town). Say so once per 30 min.
					// The errand (errand.go) takes it when the member may leave; otherwise it waits for the run.
					b.waitTownSay(why, now)
				case p.callTownRunGap(b.weaponRunTown(p, weaponless, classW), b.charName(), b.partyRunGap(weaponless || bowless || heavyBag || potNeed), now):
					typing := b.P.Typing
					say := why
					if weaponless || bowless {
						if it := b.weaponFor(weaponless); it != nil {
							say = "need a " + it.Name
						}
					}
					b.event("info", "", "party town run called: "+why+" "+b.tripNumbers())
					go b.partySay(styled(typing, pick("need a town run, "+say, say+", can we go to town?", "town run? "+say)))
				case potNeed:
					// the party can't go right now (a run just happened): restock alone - a short Butterfly Wing trip,
					// announced by startTrip - rather than fight on without potions
					b.event("info", "", "out of potions and no party run possible now: going alone "+b.tripNumbers())
					b.startTrip(why, now)
				default:
					// a party town run happened less than the party interval ago: wait for the next one, say so once
					b.waitTownSay(why, now)
				}
			}
		}
	}
	b.errandThink(now)                                                                                            // errand.go: a member leaves briefly for a necessary step (Peco rental), says brb / back
	b.trapThink(now)                                                                                              // trap.go: a solo trapper Hunter's Trap supply trip
	if (b.trip == nil || b.kafra != nil && b.kafra.Store) && isTown(b.MapName) && b.storeThink(gm, moving, now) { // storage.go: heavy with kept items
		return
	}
	if b.tripThink(gm, moving, now) {
		return
	}
	// party rally: meet at the Kafra nearest the destination and save there (rally.go)
	if b.rallyThink(gm, moving, now) {
		return
	}

	// pick a goal if the mastermind hasn't set one - staggered after login (0-40 s per bot): after a mass login every
	// bot judged the same crowd counts at once and herded onto the same few maps (QA 12:33: gef_fild02 139 bots)
	if (b.Goal.Mode == "" || (b.Goal.Mode == "hunt" && b.Goal.Map == "") ||
		(b.Goal.Mode == "idle" && b.Goal.Set == "auto (nowhere reachable)" && now.After(b.buffAt["huntRetry"]))) &&
		now.Sub(b.ConnectedAt) >= time.Duration(b.P.Index%41)*time.Second {
		b.chooseHuntingGround()
	}

	// progress watchdog: re-plan if we haven't moved in a while, give up on the goal if it keeps failing
	if pos := [3]interface{}{b.MapName, b.X, b.Y}; pos != b.lastPos {
		b.lastPos, b.lastPosAt = pos, now
	} else if b.Goal.Map != "" && b.Goal.Map != b.MapName && b.Goal.Set == "auto" {
		if st := now.Sub(b.lastPosAt); st > 120*time.Second {
			b.Goal, b.route, b.path, b.lastPosAt = Goal{}, nil, nil, now
		} else if st > 45*time.Second && len(b.route) > 0 {
			b.route, b.path = nil, nil
		}
	}

	b.partyGoalSync(now)
	// escort.go: never cross a dangerous approach map alone - wait at the safe point; the leader comes back for us
	if b.escortThink(gm, moving, now) {
		return
	}
	// a pending Kafra save (save gate, the party town, a rally) is done wherever the rest of think would take over
	if b.kafra != nil && b.kafra.Save && b.MapName == b.kafra.Town && !b.underAttack(now) && b.kafraThink(gm, moving, now) {
		return
	}
	// travel to the goal map (but fight back if something jumps us on the way)
	if b.Goal.Map != "" && b.Goal.Map != b.MapName && b.Goal.Mode != "idle" {
		if b.saveGate(now) { // together.go: save at the hunting map's nearest Kafra before going in (owner rule)
			b.route, b.path = nil, nil
		}
		if lx, ly := b.livePos(now); [3]interface{}{b.MapName, lx, ly} != b.travelPos {
			b.travelPos, b.travelMovedAt = [3]interface{}{b.MapName, lx, ly}, now
		}
		// a party member on its way only fights what is hitting it: chasing an old target pulled members back over
		// the map border (QA: 8 bots with 5-18 warp-loop events in 10 min, "fighting Peco Peco" / "regrouping")
		if b.Goal.Mode == "party" && b.target != 0 && !b.underAttack(now) {
			b.target = 0
		}
		// swarm.go: only passing through a map of dense aggressive packs (mjolnir_06, 100 Spores): don't stop to fight,
		// walk on; fight only what holds us in place (3+ on us -> losingFight transit flees)
		transit := b.transitSwarm()
		if transit && !b.transitBlocked(now) {
			b.target = 0
		}
		if (b.underAttack(now) && (!transit || b.transitBlocked(now))) || b.target != 0 {
			// a party member on its way to the leader / party map fights only what is hitting it - no new pulls, no
			// looting (QA 2026-11-14: Zendancer84 fought pay_dun00's Zombies one by one for 6+ min instead of going on
			// to Ayla on pay_dun01: hunt picked the next Zombie after every kill)
			// ... and so does a bot party LEADER that announced a new map (QA 20:01: Ashqueen said "next: mjolnir_07" and
			// kept fighting on gef_fild02 while her members left)
			b.attackersOnly = b.Goal.Mode == "party" || b.Goal.Set == "party-leader"
			// ... and while it is still making headway with HP to spare it doesn't stop even for those: it walks on
			// (together.go regroupWalkOn; QA 10:20: Zen.fox stood on pay_dun00 "engaging Zombie" 2+ min, Ayla a map on)
			if b.attackersOnly && !b.fightBackOnWay(now) && (regroupWalkOn(now.Sub(b.travelMovedAt), b.hpFrac()) || b.isHealer() && healerWalkOn(now.Sub(b.travelMovedAt))) {
				b.target = 0
			} else {
				b.hunt(gm, moving, now)
			}
			b.attackersOnly = false
			if b.target != 0 || b.Sitting {
				return
			}
		}
		if b.partyLeaderSetOut(now) {
			return
		}
		b.travel(gm, moving)
		return
	}
	if v, ok := occMap.Load(b.aid); !ok || v.(string) != b.MapName {
		b.leaveMap()
		occMap.Store(b.aid, b.MapName)
		occ(b.MapName, 1)
	}

	// kafra.go: save at the Kafra nearest this hunting map once (owner rule), before hunting on
	if b.saveDetour(gm, moving, now) {
		return
	}
	switch b.Goal.Mode {
	case "hunt":
		b.hunt(gm, moving, now)
	case "party":
		if b.checkpointWait(moving, now) { // humanparty.go: waiting out a deadly map in the checkpoint town
			break
		}
		if !b.partyThink(gm, moving, now) {
			// a party healer never pulls on its own: it fights only what hits it (together.go healer safety - party
			// priests died "fighting Muka / Peco Peco" after falling through to hunt)
			b.attackersOnly = b.partyHealer()
			b.hunt(gm, moving, now)
			b.attackersOnly = false
		}
	case "vend":
		b.vendThink(gm, moving, now)
	case "follow":
		b.follow(gm, moving)
	default:
		b.loiter(gm, moving)
	}
}

// stayCrowd: how many bots a map may hold before someone already hunting there moves on
// (value: tune().StayCrowd, live-tunable in data/tuning.json)

// goodHere: the current map is a fitting hunting ground for our level and not packed or deadly - level here.
// (owner: "make the bot prioritize levelling up rather than walking around": bots used to log in on a fine
// beginner field, see "more than 28 bots" and walk to another region.)
func (b *Bot) goodHere() bool {
	m, lv := b.MapName, b.powerLv() // effective level: level AND gear (gearpower.go)
	if m == "" || isTown(m) || strings.Contains(m, "_in") {
		return false
	}
	// solo: never a train / aggressive map too hard alone, and not a map that gives our base level little exp while a
	// real-exp map fits (QA 17:05: solo bots averaging lv51 on moc_fild01's lv7-25 monsters, 78 bots there)
	if b.PartyID == 0 && (partyMapTooHard(m, b.soloRateLv(lv)) || mapExpPoor(m, b.BaseLvOr()) && b.realExpSpotFits(lv)) {
		return false
	}
	if m == b.badMap && time.Now().Before(b.badMapUntil) {
		return false // found nothing to fight here a moment ago
	}
	fits := false
	for _, g := range b.guideSpotsFor(lv) {
		if g.Map == m {
			fits = true
			break
		}
	}
	if !fits {
		for _, s := range SpotsFor(lv, b.P.Risk) {
			if s.Map == m {
				fits = true
				break
			}
		}
	}
	if !fits || b.deadlyFor(m) || b.BaseLvOr() < beginnerLv() && !beginnerField(m, lv) || bossRecent(m, lv) { // bossnote.go: a far-above boss seen here lately
		return false
	}
	occupancy.Lock()
	o := occupancy.m[m]
	occupancy.Unlock()
	if !(o <= stayCrowd() && mapHasRoom(m, 0)) { // crowd.go: spawn-based cap, and not a map of roamers
		return false
	}
	// zenyhour.go (economy run 8): a broke bot doesn't stay where a fitting map one change away pays 1.5x more an hour
	if b.shortOfZeny() {
		here := b.MapZenyHour(m)
		for _, g := range b.guideSpotsFor(lv) {
			if g.Map != m && mapHasRoom(g.Map, 1) && b.MapZenyHour(g.Map) > 1.5*here+1000 && mapHops(m, b.X, b.Y, g.Map) <= 1 {
				return false
			}
		}
	}
	return true
}

// beginnerLv: below this base level a bot stays around its home town (owner: "beginners should stick to their
// original town instead of travelling around and waste time. They need to level up first, get some money to buy
// gears and to afford kafra travel. If they cannot afford kafra they still need to stay near town").
// (value: tune().BeginnerLv, live-tunable in data/tuning.json)

// nearHome: map changes from the bot's home town to m (99 if unknown/unreachable).
func (b *Bot) nearHome(m string) int {
	home := b.P.Home
	if home == "" {
		home = nearestTown(b.MapName)
	}
	sp, ok := townSpawn[home]
	if !ok {
		return 99
	}
	return mapHops(home, sp[0], sp[1], m)
}

func (b *Bot) chooseHuntingGround() {
	// a bot party's leader keeps a map for the whole party (its weakest member), never one for its own level
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" {
		if b.leaderPartyPick(p) {
			return
		}
		// nothing fits the party: hold here rather than fall through to a solo pick for the leader's own level - a
		// party member's guideSpotsFor skips the solo safety rules (QA 19:00: silvercat pt's leader picked mjo_dun01,
		// Wolf Bless died crossing mjolnir_02). The fit check moves the party when something fits.
		m := b.MapName
		if isTown(m) || partyMapTooHard(m, b.soloRateLv(b.powerLv())) {
			m = nearestTown(b.MapName)
		}
		mode := "hunt"
		if isTown(m) {
			mode = "idle" // wait in town; the fit check's relocate gives the party a map
		}
		b.Goal, b.route, b.path = Goal{Mode: mode, Map: m, Set: "party-leader-hold"}, nil, nil
		return
	}
	// a member of a bot party restored after a restart whose leader isn't back yet heads for the party's map, not a
	// map of its own: LilGhostgirl (thewolfwing pt, prt_fild06) logged in at 12:06, picked pay_dun00 for herself, took
	// the Kafra to Payon, and two minutes later (party goal restored) took it back to Prontera
	if p := partyOf(b); p != nil && p.Leader != b && p.Human == "" && isRestoring(p) {
		parties.Lock()
		m := p.Map
		parties.Unlock()
		if m == "" || isTown(m) {
			m = b.MapName
		}
		b.Goal, b.route = Goal{Mode: "hunt", Map: m, Set: "party-restore"}, nil
		return
	}
	// lfg.go: a solo bot with an item goal (its build's next gear piece / card) farms on that goal's map while it is
	// safe for it (owner 2026-11-14: "each class need to have a priority ... then it goes to farm where it needs to")
	if b.guildBlockHunt() { // guildduty.go: a solo member on its guild duty block hunts on the block's map
		return
	}
	if g := b.fgoal; g != nil && g.mapped() && goalPartiesOn() && partyOf(b) == nil {
		for _, m := range g.Maps {
			if b.soloMapOK(m) {
				b.Goal, b.route = Goal{Mode: "hunt", Map: m, Set: "auto"}, nil
				if m != b.MapName {
					crowdNote(m, 1)
				}
				return
			}
		}
	}
	if b.goodHere() && (b.BaseLvOr() >= beginnerLv() || b.nearHome(b.MapName) <= 3) {
		b.Goal = Goal{Mode: "hunt", Map: b.MapName, Set: "auto"}
		return
	}
	// beginners: the nearest fitting field around their home town. Never a dungeon or a non-guide map, whatever the
	// fallback (QA 2026-11-14 05:20: an empty-map / "too dangerous" fallback sent lv3-18 Novices into Payon Cave, 7
	// deaths in 12 min); a beginner never reaches the general pickers below.
	if b.BaseLvOr() < beginnerLv() {
		best := b.beginnerPick()
		if best == "" {
			best = b.MapName // nothing fits at all (no guide field for this level): stay
		}
		b.Goal = Goal{Mode: "hunt", Map: best, Set: "auto"}
		b.route = nil
		crowdNote(best, 1)
		return
	}
	// guide spots first (iRO Wiki Renewal leveling guide), most of the time
	// guide spots first (iRO Wiki Renewal leveling guide), most of the time - and the NEAREST ones: players level
	// around where they are instead of crossing two regions for the same kind of field
	if gs := b.guideSpotsFor(b.powerLv()); len(gs) > 0 && b.rng.Float64() < 0.85 {
		limit := 40
		if b.BaseLvOr() < 15 {
			limit = 28 // a thousand fresh Novices: spread out over every beginner field
		}
		type cand struct {
			m    string
			hops int
		}
		var cs []cand
		best := 99
		for _, g := range gs {
			occupancy.Lock()
			o := occupancy.m[g.Map]
			occupancy.Unlock()
			if o > limit || b.deadlyFor(g.Map) || (g.Map == b.badMap && time.Now().Before(b.badMapUntil)) ||
				!mapHasRoom(g.Map, 1) {
				continue
			}
			h := 0
			if g.Map != b.MapName {
				r := RouteMaps(b.MapName, b.X, b.Y, g.Map, b.powerLv())
				if r == nil {
					continue
				}
				h = len(r)
			}
			cs = append(cs, cand{g.Map, h})
		}
		// real exp first: drop maps whose monsters give our BASE level little exp while another candidate does not
		// (QA 17:05: lv51 solo bots on moc_fild01; ai.go mapExpPoor)
		var rich []cand
		for _, c := range cs {
			if !mapExpPoor(c.m, b.BaseLvOr()) {
				rich = append(rich, c)
			}
		}
		if len(rich) > 0 {
			cs = rich
		}
		for _, c := range cs {
			best = min(best, c.hops)
		}
		var near []string
		variety := b.rng.Float64() < b.tactics().Variety // explorer guilds roam further
		for _, c := range cs {
			if c.hops <= best+1 || variety { // the closest spots, plus ones just one map further, for variety
				near = append(near, c.m)
			}
		}
		if len(near) > 0 {
			pickMap := near[b.rng.Intn(len(near))]
			// zenyhour.go (economy run 8): the candidate that pays the most per HOUR for this bot's effective level
			// and the map's crowd - broke bots almost always, others half the time (was: loot per kill, broke only)
			if r := b.rng.Float64(); b.shortOfZeny() && r < 0.85 || r < 0.5 {
				if z := b.bestZenyMap(near); z != "" {
					pickMap = z
				}
			}
			// buildgear.go nextBuildPiece (economy run 7, owner: each bot farms or trades to perfect its build): a
			// drop-only next piece is farmed where it drops, when that map is one of the fitting, safe candidates
			buildPick := false
			if pc := b.nextBuildPiece(); pc != nil && pc.Source == "drop" && b.rng.Float64() < 0.8 {
			farm:
				for _, f := range pc.Farm {
					for _, c := range cs {
						if c.m == f.Map {
							if pickMap != f.Map {
								b.event("info", "", fmt.Sprintf("farming %s on %s for my build (%s, %.2f%%)", pc.Name, f.Map, f.Mob, f.Rate))
							}
							pickMap, buildPick = f.Map, true
							break farm
						}
					}
				}
			}
			// opportunity.go (run 8, owner: farm what the Prontera market lacks): the money goal's map when it fits,
			// unless the build's own drop piece took the pick
			if !buildPick {
				var names []string
				for _, c := range cs {
					names = append(names, c.m)
				}
				if m, g := b.moneyFarmPick(names, time.Now()); m != "" && b.rng.Float64() < 0.7 {
					if pickMap != m {
						b.event("info", "", fmt.Sprintf("farming %s on %s to sell (%s; the market lacks it)", g.Item, m, g.Mob))
					}
					pickMap = m
				}
			}
			b.Goal = Goal{Mode: "hunt", Map: pickMap, Set: "auto"}
			b.route = nil
			crowdNote(pickMap, 1)
			return
		}
	}
	spots := SpotsFor(b.powerLv(), b.P.Risk)
	if len(spots) == 0 {
		b.Goal = Goal{Mode: "idle", Map: b.MapName}
		return
	}
	// prefer emptier maps; bigger maps hold more bots
	best, bs := HuntSpot{}, -1e9
	for i := 0; i < 10; i++ {
		s := spots[b.rng.Intn(len(spots))]
		if s.Map == b.badMap && time.Now().Before(b.badMapUntil) {
			continue
		}
		if b.PartyID == 0 && partyMapTooHard(s.Map, b.soloRateLv(b.powerLv())) {
			continue // train / aggressive maps: not alone (QA mjolnir_02)
		}
		hops := 0
		if s.Map != b.MapName {
			r := RouteMaps(b.MapName, b.X, b.Y, s.Map, b.powerLv())
			if r == nil {
				continue
			}
			hops = len(r)
		}
		o, _ := mapLoad(s.Map) // crowd.go: bots on the map or heading to it
		// explorers don't mind long trips; everyone else prefers grounds close by
		score := float64(s.Mobs)/float64(1+o*4) - float64(s.AggroDanger)*(1-b.P.Risk)*0.01 - recentDeathsFor(s.Map, b.BaseLvOr())*8*(1.2-b.P.Risk) -
			float64(hops*hops)*(1.5-b.P.Patience)*(1-b.explorer())*6
		if !mapHasRoom(s.Map, 1) {
			score -= 1000 // full (crowd.go): only when every spot is full
		}
		if mapExpPoor(s.Map, b.BaseLvOr()) {
			score -= 500 // little exp for our base level: only when nothing else fits
		}
		if score > bs {
			best, bs = s, score
		}
	}
	if best.Map == "" {
		// 10 random picks all failed: try every spot, nearest first, before giving up - and never make a town the
		// hunting map (class QA 2026-11-14 15:10: xXLilybunnyXx roamed prontera 17 min on "auto (nowhere reachable)")
		bh := 1 << 30
		for _, s := range spots {
			if s.Map == b.MapName || isTown(s.Map) || (s.Map == b.badMap && time.Now().Before(b.badMapUntil)) ||
				(b.PartyID == 0 && partyMapTooHard(s.Map, b.soloRateLv(b.powerLv()))) || b.deadlyFor(s.Map) {
				continue
			}
			if h := mapHops(b.MapName, b.X, b.Y, s.Map); h < bh {
				best, bh = s, h
			}
		}
		if best.Map == "" || bh >= 99 {
			if f := b.beginnerPick(); f != "" && !isTown(f) && f != b.MapName {
				best.Map = f // the nearest fitting field around home: little exp, but farming beats roaming a town
			}
		}
	}
	if best.Map == "" {
		if isTown(b.MapName) || townOf(b.MapName) != "" {
			b.event("info", "", "no hunting ground reachable from "+b.MapName+" right now: waiting a moment")
			b.Goal = Goal{Mode: "idle", Map: b.MapName, Set: "auto (nowhere reachable)"}
			b.badMap, b.badMapUntil = "", time.Time{}
			b.buffAt["huntRetry"] = time.Now().Add(time.Minute) // think() picks again in a minute
			return
		}
		b.Goal = Goal{Mode: "hunt", Map: b.MapName, Set: "auto (nowhere reachable)"}
		return
	}
	b.Goal = Goal{Mode: "hunt", Map: best.Map, Set: "auto"}
	b.route = nil
	crowdNote(best.Map, 1)
}

// bestGearIn: the plan's next item if this town sells it, otherwise the best affordable upgrade any shop in this town
// sells for the bot (any weapon/armor it can equip that beats what it wears). Ayla's party had zeny but Payon sells no
// Wand or Katana - a Rod or Blade from Payon beats a Knife, and nobody pays Kafra fees back and forth for it.
func (b *Bot) bestGearIn(town string) *Item {
	if !b.invReady() {
		return nil
	}
	// the necessary step first (class weapon, or the weapon / body-armor step of an under-geared bot: builds.go)
	if it, _ := b.classWeaponNeed(); it != nil && soldIn(it.ID, town) {
		return it
	}
	if b.gearHold() {
		return nil // no class weapon / no arrows yet: nothing else at a gear shop, the money waits (builds.go gearHold)
	}
	// lfg.go turnPiece: a human party's member buys exactly its turn piece (the party's money is for it), nothing else
	if id := b.turnPiece(); id != 0 {
		if it := items[id]; it != nil && soldIn(id, town) && b.Zeny-b.gearReserve() >= it.Buy {
			return it
		}
		return nil
	}
	// then the party gear-turn item the party raised zeny for (gearpool.go): it must actually be bought
	if it, _ := b.gearTurnIn(town, b.Zeny-b.gearReserve()); it != nil {
		return it
	}
	if it, _ := b.nextGear(); it != nil && soldIn(it.ID, town) {
		return it
	}
	budget := b.Zeny - b.gearReserve()
	var best *Item
	bestGain := 0
	for _, m := range append([]string{town}, townInteriors[town]...) {
		for _, sh := range shopsByMap[m] {
			for _, id := range sh.Items {
				it := items[id]
				if it == nil || it.Buy <= 0 || it.Buy > budget || !b.canEquip(it) || b.gearFailed[id] || b.ownsUnequipped(id) {
					continue
				}
				slot := gearSlot(it.Loc)
				if slot == 0 {
					continue
				}
				cur := 0
				if e := b.equipped[slot]; e != nil {
					if e.ItemID == id {
						continue
					}
					cur = b.itemScore(items[e.ItemID])
				}
				g := b.itemScore(it) - cur
				if slot == 0x2 && b.equipped[0x2] == nil {
					g += 1000 // no usable weapon (an acolyte with a Knife): a weapon comes first
				} else if slot == 0x2 && isClassWeapon(family(b.Class), it) && !isClassWeapon(family(b.Class), items[b.equipped[0x2].ItemID]) {
					g += 500 // the class weapon (a Rod for a Mage, a Bow for an Archer) comes first on the party run
				}
				if g > bestGain {
					best, bestGain = it, g
				}
			}
		}
	}
	return best
}

// weaponFor: the weapon a party member's town run is for (no weapon at all, or the class weapon). b.mu held.
func (b *Bot) weaponFor(weaponless bool) *Item {
	it, _ := b.classWeaponFor()
	if weaponless {
		it, _ = b.weaponNeed()
	}
	return it
}

// classWeaponFor: the class weapon to buy now, or the one the junk loot's sale pays for (b.mu held).
func (b *Bot) classWeaponFor() (*Item, *Shop) {
	if it, s := b.classWeaponNeed(); it != nil {
		return it, s
	}
	return b.classWeaponLootNeed()
}

// bowless (b.mu held): an Archer-line character holding a weapon outside its class list (the starter Knife) with no
// bow / instrument / whip in the bag: it can't use its skills at all, so buying one is an emergency like "no weapon".
// Mage line too (QA 2026-11-14: Mages waited 35+ min on a Knife for a 50z Rod - no spell power at all).
func (b *Bot) bowless() bool {
	f := family(b.Class)
	return (f == famArcher || f == famMage) && b.classWeaponMissing()
}

// partyRunGap (b.mu held): the minimum time since the party's last town run before this member may call one: 6 min
// for an emergency (no weapon at all), otherwise the trip interval (45 min) - one party run per interval carries
// everyone's selling, potions, arrows and gear.
func (b *Bot) partyRunGap(weaponless bool) time.Duration {
	if weaponless {
		return 6 * time.Minute
	}
	return time.Duration(max(6, tune().TripMinIntervalMin)) * time.Minute
}

// waitTownSay (b.mu held): a member whose town need waits for the next party run says so in party chat, at most once
// per 30 min (owner: members communicate; no run per member need).
func (b *Bot) waitTownSay(why string, now time.Time) {
	if now.Sub(b.classAskAt) < 30*time.Minute {
		return
	}
	b.classAskAt = now
	b.event("info", "", "town need waits for the next party run: "+why+" "+b.tripNumbers())
	sw := spokenWhy(why)
	if it := b.weaponFor(false); it != nil && (why == "class weapon" || why == "class weapon (sell loot)") {
		sw = "need a " + it.Name
	}
	typing := b.P.Typing
	go b.partySay(styled(typing, pick(sw+", whenever we go to town next", "next town run pls, "+sw, sw+" but it can wait for our next town run")))
}

// weaponRunTown: the party run goes to the usual town, unless the missing weapon isn't sold there: then to the
// town of the shop that sells it (bows: Payon / Prontera's market). b.mu held.
func (b *Bot) weaponRunTown(p *Party, weaponless, classW bool) string {
	town := b.partyRunTown(p)
	if !weaponless && !classW {
		return town
	}
	it := b.weaponFor(weaponless)
	if it == nil || soldIn(it.ID, town) {
		return town
	}
	var sh *Shop
	if weaponless {
		_, sh = b.weaponNeed()
	} else {
		_, sh = b.classWeaponFor()
	}
	if sh != nil {
		if t := townOf(sh.Map); t != "" && potionShop(t) != nil {
			return t
		}
	}
	return town
}

func soldIn(id int, town string) bool {
	for _, s := range gearShops(id) {
		if townOf(s.Map) == town {
			return true
		}
	}
	return false
}

// partyRunTown: the town for a party town run - the nearest one, or Prontera (whose south gate sells every shop
// item) when someone in the party can afford a gear piece the nearest town doesn't sell. Ayla's friends sat on
// 3,600 zeny with a Knife because Payon has no Wand or Katana and runs always went to Payon. (b.mu held; members'
// locks taken one at a time.)
func (b *Bot) partyRunTown(p *Party) string {
	town := nearestTown(b.MapName)
	if p.Human != "" {
		return town // a human-led party never gets dragged to Prontera by a member's shopping list
	}
	// the party gear pool (gearpool.go) planned this run for the member whose gear turn it is
	if t, why := gearRunTown(p.Name, time.Now()); t != "" && t != town {
		b.event("info", "", fmt.Sprintf("party town run goes to %s: gear run for %s", t, why))
		typing := b.P.Typing
		go b.partySay(styled(typing, pick(fmt.Sprintf("lets do the town run in %s, %s is sold there", t, why),
			fmt.Sprintf("town run in %s this time (gear for %s)", t, why))))
		return t
	}
	// peco.go: a Knight/Crusader with Riding and no Peco rents one at Prontera's breeder on this run (owner: Knights
	// ride); once it rides, wantsPeco is false and runs go back to the nearest town
	// party members rent theirs on an errand (errand.go, owner 2026-11-14: necessary steps go alone); only the bot
	// leader's own Peco rides on the party run
	for _, m := range p.Members {
		if m != p.Leader {
			continue
		}
		pt := ""
		if m == b {
			pt = b.pecoTown(time.Now())
		} else {
			m.mu.Lock()
			pt = m.pecoTown(time.Now())
			m.mu.Unlock()
		}
		if pt != "" && pt != town {
			b.event("info", "", "party town run goes to prontera: "+m.charName()+" rents a Peco Peco")
			return pt
		}
	}
	for _, m := range p.Members {
		var it *Item
		var sh *Shop
		classW := true
		// a waiting class weapon ("need a Rod, whenever we go to town next") counts first: the run must go where it
		// is sold, or bestGearIn can't honour the promise
		if m == b {
			if it, sh = b.classWeaponFor(); it == nil {
				it, sh = b.nextGear()
				classW = false
			}
		} else {
			m.mu.Lock()
			if it, sh = m.classWeaponFor(); it == nil {
				it, sh = m.nextGear()
				classW = false
			}
			m.mu.Unlock()
		}
		// economy run 7: a plain upgrade drags the whole party to Prontera and back only when it is worth the trip
		// (fares 120-180z each way per member, plus the farming time lost): 15:15-15:48 saw ~420 Kafra teleports,
		// most of them Prontera <-> Geffen / Payon / Alberta for town runs. Cheaper pieces come from the market
		// (#trade, mail trades, a party run that is in Prontera anyway) or the member's own errand.
		if it != nil && !classW && it.Buy < 800*len(p.Members) {
			it = nil
		}
		if it != nil && sh != nil && townOf(sh.Map) != town {
			sold := false
			for _, s := range gearShops(it.ID) {
				if townOf(s.Map) == town {
					sold = true
					break
				}
			}
			if !sold {
				return "prontera"
			}
		}
	}
	return town
}

// tradeThink: a party or guild mate asked to trade. Stand still, step next to them (the server needs <= 2 cells),
// accept, and stay put until the trade is done or times out. Returns true while it owns the bot.
func (b *Bot) tradeThink(moving bool, now time.Time) bool {
	if b.tradeFrom == "" {
		return false
	}
	if !b.tradeOpen.IsZero() {
		if now.Sub(b.tradeOpen) > 40*time.Second {
			b.tradeFrom, b.tradeOpen = "", time.Time{}
			return false
		}
		b.setState("trading with " + b.tradeFrom)
		return true
	}
	if now.Sub(b.tradeAt) > 12*time.Second {
		go b.send(newPkt(0x00e6).u8(4).b)
		b.tradeFrom = ""
		return false
	}
	b.setState("going to trade with " + b.tradeFrom)
	b.target = 0
	for _, e := range b.ents {
		if e.Type == objPC && strings.EqualFold(e.Name, b.tradeFrom) {
			if dist(b.X, b.Y, e.X, e.Y) <= 1 && !moving {
				b.tradeOpen = now
				b.event("info", "", "accepted a trade from "+b.tradeFrom)
				go b.send(newPkt(0x00e6).u8(3).b)
			} else if !moving {
				go b.walkTo(e.X, e.Y)
			}
			return true
		}
	}
	return true
}

// targetMaxLv: the highest monster level this bot starts a fight with. Everyone takes on monsters up to 4 levels
// above (with potions that's fine), bolder players more.
func (b *Bot) targetMaxLv() int {
	// beginners take on the usual starter monsters (Willow, Fabre, Chonchon ~lv 5-8) from level 1: sampled Novices
	// saw 458 nearby monsters as "too high" against 182 fair ones
	tn := tune()
	// effective level (gearpower.go): a lv40 with the starter Knife picks fights like a lv25
	return max(b.powerLv()+tn.TargetLvOver+int(b.P.Aggression*tn.TargetLvAggression), tn.TargetLvMin)
}

func (b *Bot) explorer() float64 {
	if b.P.Archetype == "explorer" {
		return 0.8
	}
	return 0
}

func (b *Bot) BaseLvOr() int {
	if b.BaseLv > 0 {
		return b.BaseLv
	}
	return b.P.BaseLevel
}

// travel walks to the next warp toward Goal.Map.
func (b *Bot) travel(gm *GameMap, moving bool) {
	b.setState("travel -> " + b.Goal.Map)
	if gm == nil {
		return
	}
	// long trip from a Kafra town: teleport instead of marching (kafra.go)
	now := time.Now()
	// the Kafra is refused for a while (hard limit) and the walk is deadly for us: wait in town (kafra.go)
	if b.kafra == nil && b.viaTown == "" && b.kafraWaitInTown(b.Goal.Map, now) {
		return
	}
	if b.Sitting && now.Sub(b.sitSentAt) > time.Second { // seated (rally wait, SP rest): the server ignores walking
		b.sitSentAt, b.wantSit = now, false
		go b.stand()
	}
	if b.viaTown == "" && b.kafra == nil && b.Goal.Map != b.MapName {
		if t, wing := b.kafraVia(b.Goal.Map, now); t != "" {
			b.viaTown, b.viaFor, b.route, b.path = t, b.Goal.Map, nil, nil
			if wing && now.Sub(b.wingAt) > 30*time.Second {
				b.flySave("kafra trip via "+t+" to "+b.Goal.Map, now) // travelwing.go: Butterfly Wing, else Teleport lv2
			}
			b.event("info", "", fmt.Sprintf("heading to the Kafra in %s for the trip to %s", t, b.Goal.Map))
		}
	}
	arrivedVia := false
	if b.viaTown != "" && (b.MapName == b.viaTown || b.viaFor != b.Goal.Map) {
		arrivedVia = b.MapName == b.viaTown
		b.viaTown, b.route, b.path = "", nil, nil // arrived (the Kafra planner takes over) or the goal changed
	}
	if b.kafra == nil && b.Goal.Map != b.MapName && b.viaTown == "" {
		if b.savedTown == "" {
			b.savedTown = b.P.Home // seeded characters start saved in their home town
		}
		if k := b.kafraSavePlan(b.Goal.Map, now); k != nil {
			b.kafra = k
		} else if k := b.kafraPlan(b.Goal.Map, now); k != nil {
			b.kafra = k
			b.event("info", "", fmt.Sprintf("taking the Kafra from %s to %s (heading to %s)", k.Town, k.Dest.Name, b.Goal.Map))
		}
	}
	if arrivedVia && b.kafra == nil {
		// came to this town for the Kafra but it won't take us (money, cooldown): walk on, and don't decide again to
		// "head to the Kafra" from the next field - that bounced bots field <-> town every 4-5 s
		b.kafraNext = now.Add(5 * time.Minute)
		b.event("info", "", fmt.Sprintf("no Kafra trip from %s after all: walking to %s", b.MapName, b.Goal.Map))
	}
	if b.kafraThink(gm, moving, now) {
		return
	}
	dest := b.Goal.Map
	if b.viaTown != "" {
		dest = b.viaTown
		b.setState("travel -> " + b.Goal.Map + " (via the Kafra in " + b.viaTown + ")")
	}
	if len(b.route) == 0 || b.route[0].From != b.MapName || b.route[len(b.route)-1].To != dest {
		b.route = b.routeFor(dest) // nav.go: skips portals this bot gave up on (travelStalled), when there is another way
		b.path = nil
		if b.route == nil {
			// in a Kafra town the Kafra is the way on (sealed Renewal Morocc has no walking exit to the rest of the
			// world): re-plan with the Kafra before flying home
			if len(kafraSpots[b.MapName]) > 0 && b.kafra == nil {
				if at, ok := b.buffAt["noroutekafra"]; !ok || now.Sub(at) > 30*time.Second {
					b.buffAt["noroutekafra"] = now
					b.kafraNext = time.Time{}
					if k := b.kafraPlan(b.Goal.Map, now); k != nil {
						b.kafra = k
						b.event("info", "", fmt.Sprintf("taking the Kafra from %s to %s (heading to %s, no walking route)", k.Town, k.Dest.Name, b.Goal.Map))
						return
					}
				}
			}
			if b.escapeNoRoute(gm, moving, now) { // nav.go: Butterfly Wing, or out of the nearest door
				return
			}
			b.setState("no route to " + b.Goal.Map)
			b.Goal = Goal{}
			return
		}
	}
	if len(b.route) == 0 {
		// RouteMaps returns an EMPTY (non-nil) route when dest is the map we stand on: nothing to walk. Indexing
		// b.route[0] here panicked (saveDetour -> travel) and took down the whole legion service (2026-11-14).
		b.route, b.path = nil, nil
		if b.viaTown == b.MapName {
			b.viaTown = ""
		}
		return
	}
	w := b.route[0]
	if moving {
		return
	}
	b.lastWarp = w // the portal we mean to take (warp-loop detector compares it with the one actually taken)
	// walk to a walkable cell of the portal's trigger area: many field-edge portals sit on a blocked centre cell,
	// FindPath to it returned nil and bots only nudged randomly in a ~10-cell box for minutes (nav.go portalCell)
	tx, ty := portalCell(gm, b.X, b.Y, w)
	if b.travelStalled(tx, ty, now) { // nav.go: no progress toward the portal for 60 s -> re-route / wing
		return
	}
	if dist(b.X, b.Y, tx, ty) <= 1 {
		// standing on/next to the portal; step onto it
		go b.walkTo(tx, ty)
		return
	}
	b.stepAlong(gm, tx, ty)
}

// stepAlong follows a cached A* path in segments of ~8-11 cells (the server re-paths each short segment).
// The path is only recomputed when the bot is no longer on it, and at most every 2 seconds.
func (b *Bot) stepAlong(gm *GameMap, gx, gy int) {
	b.stepAlongLimit(gm, gx, gy, nil)
}

// stepAlongLimit: stepAlong whose walk segment is shortened while its end fails ok (at least one cell is always
// walked, 3 if the path is that long: patrol.go paces a leader with it - shorter steps, never a stop)
func (b *Bot) stepAlongLimit(gm *GameMap, gx, gy int, ok func(x, y int) bool) {
	if !gm.Walkable(gx, gy) { // a portal on a blocked centre cell (callers pass w.X, w.Y): aim for its trigger area
		for _, w := range warpsFrom[gm.Name] {
			if w.X == gx && w.Y == gy {
				gx, gy = portalCell(gm, b.X, b.Y, w)
				break
			}
		}
	}
	idx := -1
	if len(b.path) > 0 && b.path[len(b.path)-1] == [2]int{gx, gy} {
		for i := 0; i < len(b.path) && i < 48; i++ {
			if dist(b.X, b.Y, b.path[i][0], b.path[i][1]) <= 1 {
				idx = i
			}
			if dist(b.X, b.Y, b.path[i][0], b.path[i][1]) > 14 {
				break
			}
		}
	}
	if idx < 0 {
		if time.Since(b.replanAt) < 2*time.Second {
			return
		}
		b.replanAt = time.Now()
		b.path = gm.FindPath(b.X, b.Y, gx, gy, 80000)
		if b.path == nil {
			// unreachable target. Random nudges only when we stand somewhere odd (off the walkable grid); otherwise
			// say so and stand: travelStalled (nav.go) re-plans after 60 s and then wings home / tries another
			// portal. Random-walking hid the bug for minutes (prt_fild08 "travel -> prontera", 2026-11-14).
			if gm.enclosed(b.X, b.Y) { // nav.go: walled in (seeded on a blocked cell): only a teleport or a relog gets out
				b.unwall()
			} else if !gm.Walkable(b.X, b.Y) {
				x, y := b.X+b.rng.Intn(9)-4, b.Y+b.rng.Intn(9)-4
				if gm.Walkable(x, y) {
					go b.walkTo(x, y)
				}
			} else if time.Since(b.noPathLog) > time.Minute {
				b.noPathLog = time.Now()
				b.event("info", "", fmt.Sprintf("no path on %s from %d,%d to %d,%d (state %q)", b.MapName, b.X, b.Y, gx, gy, b.State))
			}
			return
		}
	}
	b.path = b.path[idx+1:]
	if len(b.path) == 0 {
		return
	}
	n := min(len(b.path), 8+b.rng.Intn(4))
	// the server re-paths each segment itself, and its shortest path can take any cell inside the box spanned by the
	// two ends: shorten the segment until that box holds no warp trigger cell (except the warp we are heading for),
	// or the server walks the bot across a door and warps it away (alberta 102,153 -> alberta_in loop, 2026-11-14)
	// (segHitsWarp: when the server's direct line is open only its cells count, so a step past a door's corner is fine)
	for n > 1 && gm.segHitsWarp(b.X, b.Y, b.path[n-1][0], b.path[n-1][1], gx, gy) {
		n--
	}
	for n > 3 && ok != nil && !ok(b.path[n-1][0], b.path[n-1][1]) { // >= 3 cells: a crawl of 1-cell walks halved the sweep speed
		n--
	}
	p := b.path[n-1]
	go b.walkTo(p[0], p[1])
}

// spCaster: SP is this bot's damage or healing (Mage/Wizard/Sage line, the Acolyte/Priest healers). Everyone else's
// basic attack (any melee weapon, a bow with arrows, an instrument/whip) costs no SP: their skills are extras.
func (b *Bot) spCaster() bool {
	return b.isHealer() || family(b.Class) == famMage && !b.meleeCaster() // run 5: a Hindsight Sage fights like a melee class
}

// skillSPFloor: the SP% a non-caster keeps back from damage skills (Double Strafe, Sonic Blow, Bash...) for
// emergencies; below it the bot just auto-attacks while SP regenerates
const skillSPFloor = 30

// lowSP: casters (whose damage is their skill) rest when they can't cast twice more. Non-casters never rest for SP
// (QA 2026-11-14 07:24: Archers rested 17% of their time, Assassins 18%, Hunters/Bards 10-12% at full HP after
// burning SP on Double Strafe / Sonic Blow; the earlier 15%-sit/40%-stand line made them sit for minutes and, in a
// party, sit/stand flicker as party.go stood them up for the leader's fights). Owner: ~90% of the time is farming.
func (b *Bot) lowSP() bool {
	if b.MaxSP <= 0 || !b.spCaster() {
		return false
	}
	if sk := b.bestAttack(); sk != nil && sk.Elemental && b.SP < sk.SP*2 {
		return true
	}
	if b.isHealer() {
		return b.SP*100/b.MaxSP < 25
	}
	return b.SP*100/b.MaxSP < 15
}

// fightSPLow: a non-caster nearly out of SP in a fight drinks an SP potion if it carries one (it never sits for SP)
func (b *Bot) fightSPLow() bool {
	return b.MaxSP > 0 && !b.spCaster() && b.SP*100/b.MaxSP < 15
}

// healUse: self-heal for the acolyte line, using the level actually learned
func (b *Bot) healUse() *SkillUse {
	si := b.skills[alHeal]
	if si == nil || si.Lv == 0 {
		return nil
	}
	return &SkillUse{ID: alHeal, Lv: si.Lv, SP: max(si.SP, 10+3*si.Lv), Range: 9, CastMs: 1000, Self: true}
}

func (b *Bot) hpFrac() float64 {
	if b.MaxHP <= 0 {
		return 1
	}
	return float64(b.HP) / float64(b.MaxHP)
}

func (b *Bot) underAttack(now time.Time) bool {
	for _, e := range b.ents {
		if now.Sub(e.LastHitMe) < 4*time.Second {
			return true
		}
	}
	return false
}

// potHeal: rough average HP a potion heals (starter-kit potions are weak)
func potHeal(id int) int {
	switch id {
	case 569:
		return 30 // Novice Potion
	case 11500, 11501:
		return 50 // starter-kit Light potions
	case itemRedPotion:
		return 55
	case itemOrangePotion:
		return 125
	case itemYellowPotion:
		return 205
	case itemWhitePotion:
		return 365
	}
	return 0
}

// potionIndex: which HP potion to drink now. Below lv20, or topping up out of danger: the starter kit first (Novice
// Potions). In an emergency (HP < 40%, or < 60% while being hit) the strongest potion that doesn't overheal badly
// (QA 2026-11-14: lv50+ bots with 1,600 HP drank 30 HP Novice Potions at 20% HP and died).
func (b *Bot) potionIndex() int {
	hp := b.hpFrac()
	emergency := b.BaseLvOr() >= 20 && (hp < 0.4 || hp < 0.6 && b.underAttack(time.Now()))
	return b.pickPotion(emergency)
}

func (b *Bot) pickPotion(emergency bool) int {
	if emergency {
		missing := b.MaxHP - b.HP
		best, bestHeal, low, lowHeal := -1, 0, -1, 1<<30
		for _, it := range b.inv {
			h := potHeal(it.ItemID)
			if !isHealPotion(it.ItemID) || it.Count <= 0 || h == 0 {
				continue
			}
			if h <= missing*5/4 && h > bestHeal {
				best, bestHeal = it.Index, h
			}
			if h < lowHeal {
				low, lowHeal = it.Index, h
			}
		}
		if best >= 0 {
			return best
		}
		return low
	}
	best, bestHeal := -1, 1<<30 // topping up: the weakest first (Novice Potions, the starter kit)
	for _, it := range b.inv {
		if h := potHeal(it.ItemID); isHealPotion(it.ItemID) && it.Count > 0 && h > 0 && h < bestHeal {
			best, bestHeal = it.Index, h
		}
	}
	return best
}

func (b *Bot) hunt(gm *GameMap, moving bool, now time.Time) {
	hp := b.hpFrac()
	attacked := b.underAttack(now)

	// danger sense: losing a fight -> get out (Fly Wing, Butterfly Wing near death, or run)
	if attacked && b.escape(gm, hp, moving, now) {
		return
	}

	// potions when it gets dangerous (at most one per second, like spamming the hotkey)
	potLine := 0.25 + 0.2*(1-b.P.Risk)
	if b.potionCount() > 30 { // plenty of (starter) potions: drink earlier and keep going
		potLine = 0.55
	}
	if hp < potLine && attacked && now.After(b.potAt) {
		if idx := b.potionIndex(); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
		}
	}
	// fruit, herbs and surplus starter potions top HP up (they weigh a lot and sell for little): out of combat below
	// 90%, in a fight below 80%, and over the regen line at any HP loss (weight.go snackIndex)
	if (hp < 0.8 || !attacked && hp < 0.9 || b.overRegenWeight()) && now.After(b.potAt) {
		if idx := b.snackIndex(); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
		}
	}
	// rest when low and safe: sitting doubles HP/SP regen (Basic Skill 3+). Keep asking until the server
	// confirms the sit (it ignores a sit sent while the walk is still finishing).
	restAt := (0.3 + 0.35*(1-b.P.Risk)) * b.tactics().Rest
	if partyRole(b) == roleTank && partyOf(b) != nil {
		restAt *= 0.5 // a party's tank keeps going (potions, the priest heals) instead of sitting back
	}
	if b.selfHealInstead(hp, restAt, now) { // healerstick.go: a bot with Heal heals itself instead of sitting for HP
		return
	}
	if b.wantSit {
		done := hp >= 0.95 && !b.lowSP() && (b.MaxSP == 0 || b.SP*100/b.MaxSP >= 80 || b.bestAttack() == nil)
		if !b.spCaster() {
			done = hp >= 0.95 // non-casters rest for HP only: SP regenerates while they farm (lowSP)
		}
		done = done || b.overRegenWeight() // no natural regen at >= 50% weight: sitting would last forever
		if done || attacked {
			b.wantSit = false
			if b.Sitting {
				go b.stand()
			}
		} else {
			if !b.Sitting && !moving && !b.poisoned(now) && now.Sub(b.sitSentAt) > 2*time.Second && b.basicLv() >= 3 { // Novices sit from Basic Skill 3
				b.sitSentAt = now
				go b.sit()
			}
			b.setState("resting")
			return
		}
	} else if hp < restAt && !b.lowSP() && b.potionCount() > 30 && now.After(b.potAt) {
		// lots of light potions early on: drink instead of sitting around
		if idx := b.pickPotion(false); idx >= 0 {
			b.potAt = now.Add(time.Second)
			go b.useItem(idx)
		}
	} else if (b.lowSP() || b.fightSPLow() && b.bestAttack() != nil) && attacked && (b.isHealer() || b.bestAttack() != nil) && b.drinkSP(now) {
		// SP potion in a fight
	} else if (hp < restAt || b.lowSP()) && !attacked && b.target == 0 && !b.fightOn(now) && !b.overRegenWeight() && now.After(b.noSitUntil) &&
		!b.tankHoldsRest() { // tanklead.go: a party tank doesn't sit while a member fights
		b.wantSit = true
		b.setState("resting")
		return
	}
	if b.Sitting && !b.wantSit && now.Sub(b.sitSentAt) > 2*time.Second && now.Sub(b.restSitAt) > 1500*time.Millisecond && !b.waitSitHeld(now) { // stand up if something left us seated (not a party-rest sit: together.go)
		b.sitSentAt = now
		go b.stand()
	}

	// self-heal (acolyte line)
	if hs := b.healUse(); hs != nil && hp < 0.6 && b.SP >= hs.SP && now.After(b.skillReady) {
		b.skillReady = now.Add(time.Duration(hs.CastMs) * time.Millisecond)
		go b.useSkill(hs.ID, hs.Lv, b.aid)
		return
	}

	lx, ly := b.livePos(now) // together.go: mid-walk b.X/b.Y is still the walk's start
	// rest together (together.go): a bot party's leader holds position while a member rests - no collect, roam or
	// map change; it sits or guards the resting members (a member in danger out of reach still breaks the rest)
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" {
		_, _, members := b.partyView(p, now)
		if r, ok := partyRester(members, b.charName(), b.MapName, lx, ly); ok && b.restGuard(members, r, moving, now) {
			return
		}
	}
	// a bot party leader that promised to collect a member finishes that first, fighting only what hits it (QA: the
	// collect was pre-empted by every new target and the member waited "coming to get me" for nothing)
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" && b.collectName != "" && !attacked &&
		now.Sub(b.collectAt) < time.Duration(tune().LeaderCollectSec)*time.Second {
		for _, m := range p.Members {
			if s, ok := peek(m.P.Name); ok && m != b && s.Name == b.collectName && (!partyStrong(p) || memberInDanger(s)) &&
				needsCollect(b.MapName, lx, ly, s, true) && b.sameWalkRegion(gm, s.X, s.Y) { // together.go; a strong party's members come to us
				b.target = 0
				b.setState("collecting " + s.Name)
				if !moving && gm != nil {
					b.stepAlong(gm, s.X, s.Y)
				}
				return
			}
		}
		b.collectName = "" // together / busy fighting / gone: drop the promise so the member stops waiting for us
	}
	// party tank leader (tanklead.go): take whatever targets a member off it - before going on with our own target
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" && partyRole(b) == roleTank {
		_, _, ms := b.partyView(p, now)
		if b.tankFallBack(gm, ms, moving, now) || b.tankTakeAggro(ms, moving, now) {
			return
		}
	}
	// a melee leader protects the back line too (together.go meleePeel)
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" && partyRole(b) == roleMelee {
		_, _, ms := b.partyView(p, now)
		if b.meleePeel(ms, moving, now) {
			return
		}
	}
	// current target still around?
	if b.target != 0 {
		if e, ok := b.ents[b.target]; ok && b.attackersOnly && now.Sub(e.LastHitMe) > 6*time.Second {
			b.target = 0 // on the way: a target that stopped hitting us is left behind
		}
	}
	if b.target != 0 {
		if e, ok := b.ents[b.target]; ok {
			b.setState("fighting " + mobName(e.Class))
			b.fight(e, moving, now)
			return
		}
		b.target = 0
	}

	// loot nearby drops
	heavy := b.MaxWeight > 0 && b.Weight*100/b.MaxWeight >= 80 || b.attackersOnly // on the way: no looting
	// every drop is picked up (economy, 2026-11-14): a 30-100% roll per tick by Greed let low-Greed bots engage the
	// next monster and leave the loot (zeny comes only from loot at 1x drops)
	if !heavy {
		var best *GroundItem
		bd := 7
		sRefs, sOK := b.stickRefs(now) // partystick.go: no drop beyond together_cells of the party
		sx0, sy0 := b.livePos(now)
		for _, it := range b.items {
			if sOK && !stickAllows(sx0, sy0, it.X, it.Y, sRefs, togetherCells()) {
				continue
			}
			if b.overRegenWeight() && heavyJunk(it.ItemID) {
				continue // over the regen line: low-value NPC junk stays on the ground (lootvalue.go heavyJunk)
			}
			if b.spareGearFull(it.ItemID) {
				continue // lootvalue.go (batch 16): a spare of this weapon/armor is already in the bag
			}
			if d := dist(b.X, b.Y, it.X, it.Y); d < bd && now.Sub(it.Seen) < 60*time.Second && (gm == nil || !gm.OnWarp(it.X, it.Y)) {
				best, bd = it, d
			}
		}
		if best != nil {
			b.setState("looting")
			if bd <= 1 {
				id := best.ID
				delete(b.items, id) // optimistic; the server confirms with 0x0b41
				go b.pickup(id)
			} else if !moving {
				go b.walkTo(best.X, best.Y)
			}
			return
		}
	}

	// a bot party's leader collects stragglers one by one (owner: "make up her mind and find 1 friend and bring that
	// to the other ... Go and collect one by one"): commit to the nearest member who fell behind, walk to it, then the
	// next. Members also walk back to the leader (party.go), so they meet halfway.
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" && !attacked {
		strong := partyStrong(p) // owner 2026-11-14: a higher-level party's members run straight to the leader
		var tgt pubState
		found := false
		// one straggler, kept until done: picked beyond together_cells, finished within collect_stop_cells (hysteresis:
		// ending at 20 too made leaders flip "collecting"/"roaming" and switch stragglers every few seconds)
		if b.collectName != "" && now.Sub(b.collectAt) < time.Duration(tune().LeaderCollectSec)*time.Second {
			for _, m := range p.Members {
				if s, ok := peek(m.P.Name); ok && s.Name == b.collectName && (!strong || memberInDanger(s)) && needsCollect(b.MapName, lx, ly, s, true) && b.sameWalkRegion(gm, s.X, s.Y) { // together.go
					// the commitment ends as soon as the member is within together_cells (QA: leaders kept "collecting" members
					// 5-9 cells away until 3 cells)
					tgt, found = s, true
				}
			}
		}
		if !found {
			b.collectName = ""
			bd := 99
			for _, m := range p.Members {
				if m == b {
					continue
				}
				if s, ok := peek(m.P.Name); ok && (!strong || memberInDanger(s)) && needsCollect(b.MapName, lx, ly, s, false) && b.sameWalkRegion(gm, s.X, s.Y) && // together.go: beyond together_cells, not busy fighting; a strong party's members walk to us themselves
					!bossSpotNear(b.MapName, s.X, s.Y, 12, b.powerLv()) { // nav.go: not across a river (the member goes round); bossnote.go: not into a boss spot
					sx, sy := livePosOf(s)
					if d := dist(lx, ly, sx, sy); d < bd {
						tgt, bd, found = s, d, true
					}
				}
			}
			if found {
				b.collectName, b.collectAt = tgt.Name, now
				if now.After(b.buffAt["leadsay"]) { // at most one call-out every 2 minutes
					b.buffAt["leadsay"] = now.Add(2 * time.Minute)
					typing := b.P.Typing
					go b.partySay(styled(typing, pick("going to get "+tgt.Name+", follow me", tgt.Name+" wait there, coming", "omw to "+tgt.Name)))
				}
			}
		}
		if found {
			b.setState("collecting " + tgt.Name)
			if !moving && gm != nil {
				b.stepAlong(gm, tgt.X, tgt.Y)
			}
			return
		}
	}

	// choose a target: whoever is hitting us first, then the nearest suitable monster
	maxLv := b.targetMaxLv()
	plv := b.powerLv()
	clv := b.champLv() // bossnote.go: champions measured with the party's strength
	var pick *Entity
	pd := 99
	bt := b.bossHuntTarget()       // bosshunt.go: the party hunts this boss here - everyone goes for it on sight
	tRefs, tOK := b.stickRefs(now) // partystick.go: no pull beyond together_cells of the party (what hits us still counts)
	tx0, ty0 := b.livePos(now)
	fip := b.fightInPlace(now, tRefs) // pt32.go: PT32 in-place filter for party members (nil = off / solo)
	for _, e := range b.ents {
		if e.Type != objMob {
			continue
		}
		if b.attackersOnly && now.Sub(e.LastHitMe) >= 4*time.Second {
			continue // on the way to the leader: no new pulls
		}
		ex, ey := e.pos(now)
		d := dist(b.X, b.Y, ex, ey)
		if bt > 0 && e.Class == bt {
			pick, pd = e, -1
			break
		}
		if now.Sub(e.LastHitMe) < 4*time.Second {
			pick, pd = e, -1
			break
		}
		if tOK && !stickAllows(tx0, ty0, ex, ey, tRefs, togetherCells()) {
			continue
		}
		if fip != nil && !fip.allows(e.ID, ex, ey) {
			continue
		}
		mb := mobs[e.Class]
		if mb != nil && (mb.Boss || mb.Level > maxLv || mb.worthless()) {
			continue
		}
		if bt == 0 && bossSpotNearC(b.MapName, ex, ey, 12, plv, clv) { // bossnote.go: a boss was seen / killed someone there in the last 10 min
			continue
		}
		if mb != nil && mb.Champion && !mb.Boss && d <= 14 && !champThreat(mb, clv) && b.goalMob(e.Class) &&
			(claims.free(b.MapName, e.ID, b.aid) || b.partyClaim(e.ID)) {
			pick, pd = e, 0 // run 6: a champion we outclass that drops our goal item (Swift Creamy -> Creamy Card) first
			break
		}
		if gm != nil && gm.OnWarp(ex, ey) { // standing in a portal's area: walking to it warps us away (nav.go safeWalkDest)
			continue
		}
		if b.packAround(e) >= 3 && partyOf(b) == nil { // a pack of aggressive monsters: a solo player walks around it
			continue
		}
		// the nearest one nobody else is on (classprobe: Novices roamed crowded fields with 0 kills because only
		// the single nearest monster was considered, and on a busy map someone else always had it)
		if d < pd && d <= 14 && (claims.free(b.MapName, e.ID, b.aid) || b.partyClaim(e.ID)) {
			pick, pd = e, d
		}
	}
	// a non-tank bot leader lets its tank open the fight (tanklead.go): assist the tank's target, hold a fresh pull
	// for a moment (never when the monster is already hitting us or it's the hunted boss)
	if p := partyOf(b); p != nil && pd >= 0 && b.leaderDefer(p, pick, lx, ly, moving, now) {
		return
	}
	if pick != nil && (b.partyClaim(pick.ID) || claims.try(b.MapName, pick.ID, b.aid, pd < 0 || b.P.Aggression > 0.92)) {
		b.target, b.targetAt = pick.ID, time.Time{}
		b.setState("engaging " + mobName(pick.Class))
		b.fight(pick, moving, now)
		return
	}
	if b.attackersOnly {
		return // on the way: nothing hitting us - the caller travels on (no roaming / exploring)
	}

	// a bot party's member with nothing to fight stays by its leader: the leader sweeps the map and picks the next
	// map, the member does not explore on its own (QA 2026-11-14 06:35: 37 of 75 "roaming" bots were party members
	// next to their leader; explore() walked them 20-80 cells off, past together_cells, and the leader turned back to
	// collect them - back-and-forth, and their time was counted as roaming)
	if p := partyOf(b); p != nil && p.Human == "" && p.Leader != nil && p.Leader != b && b.Goal.Mode == "party" {
		if s, ok := peek(p.Leader.P.Name); ok && s.Online && s.Map == b.MapName && !(s.X == 0 && s.Y == 0) {
			b.roamSince = time.Time{}
			b.setState("with " + s.Name)
			if !moving && gm != nil && dist(b.X, b.Y, s.X, s.Y) > 6 {
				b.stepAlong(gm, s.X, s.Y)
			}
			return
		}
	}
	// nothing to do: roam - but not forever. Sampled 2026-11-13: 424 of ~660 active bots were "roaming" with 0 kills;
	// 23 of 40 saw no monster at all (field hunted out), the rest only monsters above their level. After 45 s
	// without a target a solo bot gives this map up for 10 minutes and picks another hunting ground.
	b.setState("roaming")
	if b.roamSince.IsZero() || now.Sub(b.roamAt) > 5*time.Second {
		b.roamSince = now
	}
	// teleport.go (owner 2026-11-14: Teleport "so it can travel fast"): a solo hunter with Teleport (Creamy Card or
	// the Acolyte skill) teleports to a random cell after a few seconds with nothing to fight instead of walking
	if b.teleHunt(now) {
		b.roamAt = now
		return
	}
	// roam share over a 3-minute window (QA 2026-11-14 06:40: 489 of 659 roaming streaks lasted one 12 s sample - a
	// kill now and then reset roamSince, so a bot or party on a thin map (0.7-1.7 monsters in view vs 5-8) never
	// roamed 75 s in a row and never moved on, while roaming 30-60% of its time)
	if gap := now.Sub(b.roamAt); !b.roamAt.IsZero() && gap < 3*time.Second {
		b.roamAcc += gap
	}
	if b.roamWin.IsZero() || now.Sub(b.roamWin) > 2*roamWindow {
		b.roamWin, b.roamAcc = now, 0
	}
	roamShare := 0.0
	if w := now.Sub(b.roamWin); w >= roamWindow {
		roamShare = float64(b.roamAcc) / float64(w)
		b.roamWin, b.roamAcc = now, 0
	}
	b.roamAt = now
	// crowd.go: on a full map (over its spawn-based cap, 30%+ of its bots roaming, or thin: seen.go) give up after
	// roam_crowd_sec in a row, or after roaming roamSharePct of the last roamWindow
	giveUp := time.Duration(tune().RoamGiveUpSec) * time.Second
	crowded := (now.Sub(b.roamSince) > time.Duration(tune().RoamCrowdSec)*time.Second || roamShare*100 >= roamSharePct) &&
		mapCrowded(b.MapName)
	if crowded {
		giveUp = time.Duration(tune().RoamCrowdSec) * time.Second
	}
	if crowded && partyOf(b) == nil && b.Goal.Set == "auto" {
		b.roamSince = now.Add(-giveUp - time.Second) // the solo give-up below fires on this tick
	}
	// seen.go: no monster at all in view for 60 s (not even too-high ones) - the map reads empty, move on sooner
	if (now.Sub(b.roamSince) > giveUp || b.seesNothing(now)) && partyOf(b) == nil && b.Goal.Set == "auto" {
		if b.seesNothing(now) {
			b.event("info", "", fmt.Sprintf("no monsters in sight on %s for a minute: moving on", b.MapName))
		}
		b.badMap, b.badMapUntil = b.MapName, now.Add(10*time.Minute)
		b.roamSince = time.Time{}
		b.Goal, b.route, b.path = Goal{}, nil, nil
		return
	}
	// a bot party leader gives a dead map up too (QA 2026-11-14: low-level parties roamed 5+ min with 0 kills on
	// maps whose monsters were all above their filter; the give-up above was solo-only): the nearest map where the
	// weakest member's filter accepts most monsters, the whole party moves (rally at its Kafra when apart)
	if p := partyOf(b); p != nil && p.Leader == b && p.Human == "" && b.Goal.Set == "party-leader" &&
		(now.Sub(b.roamSince) > 2*time.Duration(tune().RoamGiveUpSec)*time.Second || crowded) {
		b.roamSince = time.Time{}
		b.badMap, b.badMapUntil = b.MapName, now.Add(20*time.Minute)
		if !p.resting() { // together.go: no map change while a member rests
			go p.relocate(b.MapName, b.X, b.Y, "")
		}
	}
	if !moving && gm != nil {
		b.explore(gm, now)
	}
}

// roamWindow / roamSharePct: a bot (or bot party leader) that roamed this share of the last window on a full or thin
// map (crowd.go mapCrowded) moves on, even when single kills keep breaking its roaming streak
const (
	roamWindow   = 3 * time.Minute
	roamSharePct = 35
)

// deathLoopCheck (on respawn): 2 deaths on one map within 10 minutes -> that map is too hard. QA 2026-11-14:
// LilDarkboy (Thief lv7) died 8 times and a lv1 Novice 7 times in 6 min on gef_fild04 with their party; a lv21 Mage
// died on moc_fild01 every ~3 min and paid the Kafra back each time. Solo: drop the map for 30 min. Bot party: the
// leader moves the party to an easier map; a member far below the party's level leaves it (it levels at home).
func (b *Bot) deathLoopCheck(now time.Time) {
	if len(b.deathLog) == 0 {
		return
	}
	last := b.deathLog[len(b.deathLog)-1]
	n := 0
	for _, d := range b.deathLog {
		if d.m == last.m && now.Sub(d.at) < 10*time.Minute {
			n++
		}
	}
	if len(b.deathLog) > 10 {
		b.deathLog = b.deathLog[len(b.deathLog)-10:]
	}
	if n < 2 || isTown(last.m) {
		return
	}
	b.deathLog = nil
	b.badMap, b.badMapUntil = last.m, now.Add(30*time.Minute)
	p := partyOf(b)
	if p == nil || p.Human != "" {
		if b.Goal.Set == "auto" || b.Goal.Set == "" {
			b.Goal, b.route, b.path = Goal{}, nil, nil
		}
		b.event("info", "", fmt.Sprintf("died twice on %s in 10 min: looking for an easier map", last.m))
		return
	}
	top := 0
	for _, m := range p.Members {
		if m != b {
			top = max(top, peekLv(m))
		}
	}
	if p.Leader != b && top-b.BaseLvOr() >= 10 {
		b.event("party", "", fmt.Sprintf("died twice on %s, too weak for this party (lv %d vs %d): leaving", last.m, b.BaseLvOr(), top))
		typing := b.P.Typing
		go b.partySay(styled(typing, pick("too strong here for me, gonna lvl a bit on my own. ty!", "i keep dying lol, ill go lvl elsewhere, cya")))
		go p.drop(b)
		return
	}
	b.event("party", "", fmt.Sprintf("died twice on %s in 10 min: the party moves to an easier map", last.m))
	go p.relocate(last.m, -1, -1, "") // deaths are danger: not held back by a party rest (together.go)
}

// partyMapFor: the party's hunting map must suit its WEAKEST member (QA 2026-11-14: a lv1 Novice and a lv7 Thief
// died 7-8 times in 6 min on gef_fild04 with their party). If most of m's monsters are above the weakest member's
// target filter, or the map's guide range starts far above it, use the closest spot that fits. Takes no bot locks.
func partyMapFor(ms []*Bot, m string) string {
	if m == "" || isTown(m) {
		return m
	}
	if partyMapOK(ms, m) {
		// keep it - unless it is exp-poor for the weakest base level or packed while a better map fits (QA 17:05:
		// lfg pairs of lv52-70 formed "lvling @ moc_fild01" on a 78-bot field of lv7-25 monsters)
		t := nearestTown(m)
		sp := townSpawn[t]
		poor := mapExpPoor(m, partyMinBase(ms)) && partyMapNearTier(ms, t, sp[0], sp[1], m, false) != ""
		packed := !poor && mapCrowded(m) && partyMapSearch(ms, t, sp[0], sp[1], m, partyAnySafe, true) != ""
		if !poor && !packed {
			return m
		}
	}
	// re-fit from where the LEADER stands (ms[0]), not from the requested map's town: QA 2026-11-14 10:32, guild06's
	// mind asked for gef_fild03 (too hard), the re-fit measured from Geffen's side and the leader on gef_fild01 walked
	// 15 min across the world to pay_fild09
	from, x, y := nearestTown(m), 0, 0
	if sp, ok := townSpawn[from]; ok {
		x, y = sp[0], sp[1]
	}
	if s, ok := peek(ms[0].P.Name); ok && s.Online && s.Map != "" {
		from, x, y = s.Map, s.X, s.Y
	}
	if best := partyMapNear(ms, from, x, y, m); best != "" {
		return best
	}
	return m
}

// partyFitLv: the weakest member's effective level and the lowest target filter in the party (no bot locks), over
// the ONLINE members whose level the server has told us (known); unknown = online members still without a level.
// Offline / logging-in members read as the persona's lv1 default (persona base_level 1): counting them sent 66
// parties of lv45+ to Novice fields after the 07:06 deploy (QA 2026-11-14 07:20).
func partyFitLv(ms []*Bot) (minLv, fitLv, known, unknown int) {
	minLv, fitLv, known, unknown = partyFitLvRaw(ms)
	if known > 0 {
		// lfg.go partyStrength: a healer / tank / full group fights a few levels above its weakest member (capped at
		// the weakest real base level)
		if b := partyStrength(ms); b > 0 {
			minLv = min(minLv+b, max(minLv, partyMinBase(ms)))
			fitLv += b
		}
		minLv, fitLv = partyPerfAdjust(ms, minLv, fitLv, false) // gearscore.go: easy farming steps the party up
	}
	return
}

// partyFitLvRaw: partyFitLv without the party-strength bonus - the weakest member on its own (item-goal maps,
// lfg.go goalMapSafeItem: lv36-46 card parties wiped on moc_fild02 with the bonus, 2026-11-14 19:28-19:48).
func partyFitLvRaw(ms []*Bot) (minLv, fitLv, known, unknown int) {
	tn := tune()
	minLv, fitLv = 999, 999
	for _, b := range ms {
		s, ok := peek(b.P.Name)
		if !ok || !s.Online {
			continue
		}
		if !s.Known {
			unknown++
			continue
		}
		known++
		lv := s.Power // effective level: level AND gear (gearpower.go)
		if lv <= 0 {
			lv = s.Lv
		}
		if s.Fragile {
			lv = max(1, lv-partyFragileLv)
		}
		minLv = min(minLv, lv)
		fitLv = min(fitLv, max(lv+tn.TargetLvOver+int(b.P.Aggression*tn.TargetLvAggression), tn.TargetLvMin))
	}
	if known > 0 {
		minLv, fitLv = partyPerfAdjust(ms, minLv, fitLv, true) // gearscore.go: deaths / poor kills step the party down
	}
	return
}

// partyMapOverLv: a party map's monsters (spawn-weighted mean level) may be at most this far over the weakest
// member's EFFECTIVE level (gear-rated powerLv), less 1 level per 5% of aggressive monsters (partyMapTooHard).
// QA 2026-11-14: Kimchi07 (lv39) in a party on a lv59 map used 30 Fly Wings in 3 minutes fleeing (first fix, +12).
// After parties really moved up (07:44 deploy) deaths rose 3 -> 5.5/min; measured 12:26-13:13 (dep12_ev.jsonl):
// deaths per bot standing on the map were 0.06 on maps with <20% aggressive monsters, 0.4 at 20-40%, 0.66 at
// 40-60%, 0.88 at 60-80% and 6.0 above (gef_fild03 Orc Warriors 92%: 32 deaths, 3 bots; gef_fild10 96%: 28/7;
// mjolnir_06 48%: 20/24; prt_fild10 65%: 17/31) - the dying bots fought at "lv 45 vs 44": gear-poor lv60-70s
// rated ~45. Aggression, not mean level, is what kills a party.
var partyMapOverLv = 4

// partyFragileLv: a mage-family member counts this many levels weaker when a party map is picked (QA 12:46-13:16:
// 9 of mjolnir_06's 18 deaths were Mages, lv33-51, among Poison Spores the rest of their parties survived).
const partyFragileLv = 6

// soloRateLv (b.mu held): the effective level the map-safety rules (partyMapTooHard) rate this bot at alone.
func (b *Bot) soloRateLv(lv int) int {
	if fragileClass(b.Class) {
		return max(1, lv-partyFragileLv)
	}
	return lv
}

// realExpSpotFits (b.mu held): a guide spot fits this effective level (guideSpotsFor), is not exp-poor for our base
// level, and has room - so leaving an exp-poor map leads somewhere better.
func (b *Bot) realExpSpotFits(lv int) bool {
	for _, g := range b.guideSpotsFor(lv) {
		if g.Map != b.MapName && !mapExpPoor(g.Map, b.BaseLvOr()) && mapHasRoom(g.Map, 1) && !b.deadlyFor(g.Map) {
			return true
		}
	}
	return false
}

// fragileClass: low-HP or weak-fighting classes rated partyFragileLv weaker for party maps - the mage family, and the
// Alchemist line (QA 2026-11-14: guild03 leader Blaze.mage, Alchemist lv34-38, swarmed on mjolnir_02 by lv39 mobs
// every time it came back; its own party map).
func fragileClass(c int) bool { return family(c) == famMage || isAlchemistClass(c) }

// partyMapDeadly: map m is killing people right now - 5+ deaths in 30 min and at least one per 3 bots on it (QA:
// gef_fild03 23 deaths / 30 min, gef_fild10 22, prt_fild10 16, while parties kept being sent there). Party pickers
// skip it until the deaths age out.
func partyMapDeadly(m string) bool {
	d := deathsWithin(m, 30*time.Minute) // danger.go
	if d < 5 {
		return false
	}
	load, _ := mapLoad(m)
	return d*3 >= load
}

// partyMapTooHard: map m's monsters are too strong for a party whose weakest member's effective level is minLv.
// gef_fild03 (mean 41, 92% aggressive) needs a weakest power of 55; mjolnir_06 (25, 48%) 31; moc_fild03 (42, 20%) 42.
//
// Also: the common aggressive monsters (5%+ of the spawns) must not out-level the weakest member by more than
// partyAggroTopOverLv - they come for it. QA 09:36-10:05 deploy: moc_fild03 (mean 42, 20% aggressive, Argos lv47 and
// Wolves aggro) became the top death map, 8 -> 12 deaths / 15 min, with lv56-74 parties (effective ~40-53) on it.
//
// Train maps: 100+ aggressive spawns (x2 on this server's mob_count_rate) gather 10-30 monsters on whoever walks
// there. QA 15:20-15:58: 46 deaths on mjolnir_02 (177 aggressive spawns, top lv38) - the dead fought "lv 34 vs 39..51",
// i.e. effective levels OVER the monsters, with 10-31 monsters on them. Such a map needs the weakest member
// partyTrainOverLv over its strongest common aggressive monster (mjolnir_02 52, prt_fild10 48, gef_fild03 59).
//
// It also covers the WAY IN: a map reached only through a too-hard map is too hard (QA 18:30-18:50: 22 of 41 deaths on
// mjolnir_02 - parties hunting mjo_dun01/02, whose entrance is on mjolnir_02, walked their lv40-50 members through its
// trains). Every picker (party, solo, lfg goal maps via goalMapSafe / soloMapOK, guild leaders) calls this one function.
func partyMapTooHard(m string, minLv int) bool {
	return partyMapTooHardHere(m, minLv) || mapAccessTooHard(m, minLv)
}

// mapAccessTooHard: the walk from m's nearest town to m crosses a map partyMapTooHardHere rates too hard for minLv
// (a Kafra teleport can't skip a dungeon's entrance field).
func mapAccessTooHard(m string, minLv int) bool {
	if isTown(m) {
		return false
	}
	t := nearestTown(m)
	sp, ok := townSpawn[t]
	if !ok {
		return false
	}
	return routeCrossesTooHard(t, sp[0], sp[1], m, minLv)
}

// partyMapTooHardHere: map m's own monsters are too strong for minLv (no route check).
func partyMapTooHardHere(m string, minLv int) bool {
	// an MVP's map is no farming ground unless the party is far above it (QA 20:49: the engine's best_map sent Ayla's
	// lv72-80 party to pay_fild10; Eddga's Fire Ball hit 8-11k and wiped it). Boss hunts use bosshunt.go, not this.
	if mv := mapMVP(m); mv != nil && minLv < mv.Level+partyMVPOverLv {
		return true
	}
	top := mapAggroTopLv(m)
	if partyMapTrain(m, minLv) {
		return true
	}
	return mapMeanLv(m) > minLv+partyMapOverLv-mapAggroPct(m)/5 || top > minLv+partyAggroTopOverLv
}

// partyMapTrain: m is a train map (100+ aggressive / assist-pack spawns) too strong for minLv - always deadly.
func partyMapTrain(m string, minLv int) bool {
	return mapAggroPct(m)*mapSpawnCount(m)/100 >= partyTrainAggroN && mapAggroTopLv(m) > minLv-partyTrainOverLv
}

// partyMVPOverLv: a map with a timed MVP spawn counts as too hard below the MVP's level + this (effective level).
const partyMVPOverLv = 20

var mvpMaps struct {
	once sync.Once
	m    map[string]*Mob
}

// mapMVP: the strongest MVP with a timed spawn on map m (bosshunt.go bossSpawns + the lookup's MVP flag), nil if none.
func mapMVP(m string) *Mob {
	mvpMaps.once.Do(func() {
		lookupOnce.Do(loadLookup)
		mvpMaps.m = map[string]*Mob{}
		for id, ms := range bossSpawns {
			ex, mb := mobExtras[id], mobs[id]
			if ex == nil || !ex.Mvp || mb == nil {
				continue
			}
			for mm := range ms {
				if old := mvpMaps.m[mm]; old == nil || mb.Level > old.Level {
					mvpMaps.m[mm] = mb
				}
			}
		}
	})
	return mvpMaps.m[m]
}

// isMVP: monster id is an MVP (the lookup's MVP flag).
func isMVP(id int) bool {
	lookupOnce.Do(loadLookup)
	ex := mobExtras[id]
	return ex != nil && ex.Mvp
}

// partyTrainAggroN / partyTrainOverLv: see partyMapTooHard.
const (
	partyTrainAggroN = 100
	partyTrainOverLv = 14
)

// partyAggroTopOverLv: how far the strongest common aggressive monster may be over the weakest member's effective level.
const partyAggroTopOverLv = 2

// partyAggroHops: map-change penalty for a map with 40%+ aggressive monsters when pickers compare distances: an
// equally near passive map is preferred (deaths per bot 0.06 on <20% aggressive maps vs 0.66+ on 40%+).
const partyAggroHops = 2

// partyMapOK: map m suits the party's weakest member (no bot locks): its filter accepts most monsters, the map is
// not a swarm / empty / boss map for it, a guide spot's range doesn't start over it, and the monsters' mean level
// isn't far over it. bossnote.go: never send / keep a party on a map with a recent far-above boss note (QA:
// Crimson07's party moved into pay_fild04 with Angeling/Ghostring notes and died there).
func partyMapOK(ms []*Bot, m string) bool {
	minLv, fitLv, known, _ := partyFitLv(ms)
	if known == 0 {
		return true // nobody's level known yet: can't judge, keep the map
	}
	if bad, _ := leaderAvoids(ms[0].P.Name, m, time.Now()); bad {
		return false // party.go leaderMem: the leader's parties died / kept fleeing here lately (ms[0] is the leader)
	}
	if partyMapDeadly(m) || mapPacked(m) {
		return false // crowd.go mapPacked: 25% over what its monsters feed / its bots roam - spread out
	}
	if partyMapTooEasy(ms, m) {
		return false // outgrown (QA 07:20: lv50-68 parties on prt_fild08/pay_fild03/gef_fild07): move up
	}
	if swarmVeto(m, minLv) || mapReadsEmpty(m) || bossRecent(m, minLv) { // swarm.go, seen.go
		return false
	}
	strict := fitShare(m, fitLv) >= minFitShare && !partyMapTooHard(m, minLv)
	for _, g := range guideSpots {
		if g.Map == m && g.Min > minLv+2 {
			strict = false
		}
	}
	if strict {
		return true
	}
	// a mostly passive real-exp map a little over the party (partyRelaxedPassive) is kept too: no ping-pong with the
	// pickers that chose it
	if mapExpPoor(m, partyMinBase(ms)) {
		return false
	}
	for _, g := range guideSpots {
		if g.Map == m && partyPassiveRelaxOK(g, minLv, fitLv) {
			return true
		}
	}
	return false
}

// partyMapNear: the guide spot closest to (from, x, y) that suits the party's weakest member, never `skip` (no bot
// locks); "" when none fits.
//
// Order (QA 2026-11-14 15:30-15:55: pay_fild07 48 bots with 24 roaming and 0 monsters seen, gef_fild02 82, pay_fild09
// 71, moc_fild01 67 - the pickers fell back to the nearest fitting map whether full or not): a map with ROOM and real
// exp, a map with room, then (all full) the least crowded fitting map with real exp, then the least crowded at all.
func partyMapNear(ms []*Bot, from string, x, y int, skip string) string {
	type stage struct {
		mode int
		room bool
	}
	for _, st := range []stage{{partyStrict, true}, {partyRelaxedPassive, true}, {partyAnySafe, true},
		{partyStrict, false}, {partyRelaxedPassive, false}, {partyAnySafe, false}} {
		if m := partyMapSearch(ms, from, x, y, skip, st.mode, st.room); m != "" {
			crowdNote(m, len(ms)) // crowd.go: reserve the seats so the next picker sees them
			return m
		}
	}
	return ""
}

// partyExpPoorMarginLv: a map whose guide band tops out this far under the weakest member's BASE level gives it
// little exp (Renewal: monsters 16+ levels under -> heavy exp penalty). QA 2026-11-14 14:38-14:58: 18 parties with
// every member lv45+ farmed prt_fild07 / pay_fild02 / pay_fild06 / pay_fild08 - safe for their gear (effective
// level 24-33: Thieves/Merchants with starter weapons), but almost no exp, and crowding the beginners.
const partyExpPoorMarginLv = 15

// partyMinBase: the weakest known BASE level among the online members (999 = none known). No bot locks.
func partyMinBase(ms []*Bot) int {
	lv := 999
	for _, b := range ms {
		if s, ok := peek(b.P.Name); ok && s.Online && s.Known && s.Lv > 0 {
			lv = min(lv, s.Lv)
		}
	}
	return lv
}

// mapExpPoor: map m's monsters give little exp to a party whose weakest base level is minBase: every guide band
// listed for it tops out partyExpPoorMarginLv+ under it (off the guide list: monsters averaging 20+ under it).
func mapExpPoor(m string, minBase int) bool {
	if minBase >= 999 {
		return false
	}
	listed := false
	for _, g := range guideSpots {
		if g.Map == m {
			listed = true
			if g.Max+partyExpPoorMarginLv >= minBase {
				return false
			}
		}
	}
	if listed {
		return true
	}
	ml := mapMeanLv(m)
	return ml > 0 && ml+20 < minBase
}

// partyMapNearTier: partyMapNear's search; allowPoor=false skips maps that give the weakest member little exp
// (mapExpPoor) - the first choice whenever such a map is safe for the party's effective levels.
func partyMapNearTier(ms []*Bot, from string, x, y int, skip string, allowPoor bool) string {
	modes := []int{partyStrict, partyRelaxedPassive}
	if allowPoor {
		modes = []int{partyAnySafe}
	}
	for _, room := range []bool{true, false} {
		for _, md := range modes {
			if m := partyMapSearch(ms, from, x, y, skip, md, room); m != "" {
				return m
			}
		}
	}
	return ""
}

// partyPassiveRelaxOK: guide spot g is a mostly passive map a little over the party (partyRelaxedPassive).
func partyPassiveRelaxOK(g GuideSpot, minLv, fitLv int) bool {
	return mapAggroPct(g.Map) < 20 && g.Min <= minLv+6 && mapMeanLv(g.Map) <= minLv+partyMapOverLv+4 &&
		fitShare(g.Map, fitLv) >= 0.35 && !partyMapTooHard(g.Map, minLv+4)
}

// partyMapSearch modes. partyStrict: real exp for the weakest base level, every safety rule. partyRelaxedPassive:
// real exp, and a mostly passive map (< 20% aggressive: 0.06 deaths per bot measured) may sit a little over the
// weakest member's effective level (band start +6, mean +8, 35% under its filter) - QA 14:38-15:55: gear-poor lv45-50
// parties (effective 24-33) found no strict real-exp map and stayed on Novice fields. partyAnySafe: every safety rule,
// exp-poor maps allowed (the last resort).
const (
	partyStrict = iota
	partyRelaxedPassive
	partyAnySafe
)

// partyMapSearch: one stage of partyMapNear (reserves nothing). roomOnly: maps with room for the party (crowd.go), nearest first;
// otherwise the least crowded fitting map (crowdPct), distance breaking ties (+1 map change per 50% over the cap).
func partyMapSearch(ms []*Bot, from string, x, y int, skip string, mode int, roomOnly bool) string {
	minLv, fitLv, known, _ := partyFitLv(ms)
	if known == 0 {
		return ""
	}
	minBase := partyMinBase(ms)
	best, bh, bmin := "", 1e9, 0
	var cands []pickCand
	for pass := 0; pass < 1; pass++ {
		for _, g := range guideSpots {
			if mode != partyAnySafe && mapExpPoor(g.Map, minBase) {
				continue
			}
			if bad, _ := leaderAvoids(ms[0].P.Name, g.Map, time.Now()); bad {
				continue // party.go leaderMem (ms[0] is the leader)
			}
			if g.Map == skip || maps[g.Map] == nil || minLv > g.Max+3 || swarmVeto(g.Map, minLv) || mapReadsEmpty(g.Map) ||
				bossRecent(g.Map, minLv) || partyMapDeadly(g.Map) || roomOnly && !mapHasRoom(g.Map, len(ms)) {
				continue
			}
			if mode == partyRelaxedPassive {
				if !partyPassiveRelaxOK(g, minLv, fitLv) {
					continue
				}
			} else if g.Min > minLv+2 || fitShare(g.Map, fitLv) < minFitShare || partyMapTooHard(g.Map, minLv) {
				continue
			}
			h := mapHops(from, x, y, g.Map)
			if h > 8 {
				continue // across the world: never a party pick (walks of 10-15 min)
			}
			if routeCrossesTooHard(from, x, y, g.Map, minLv) {
				continue // the walk there crosses a train map (QA 17:48-17:53: parties for mjo_dun01 died crossing mjolnir_02)
			}
			if mapAggroPct(g.Map) >= 40 {
				h += partyAggroHops
			}
			if !roomOnly {
				h += crowdPct(g.Map) / 50
			}
			// zenyhour.go (economy run 8): each zphPerHop of expected zeny/hour is worth one map change
			c := float64(h) - min(3, partyZenyHour(g.Map, fitLv)/zphPerHop)
			cands = append(cands, pickCand{g.Map, c, g.Min})
		}
	}
	best = spreadPick(cands, 1)
	_, _ = bh, bmin
	return best // no seat reservation here: partyMapNear reserves the pick (checks must not fill maps)
}

// pickCand / spreadPick: candidates within `slack` of the best score (lower is better) are equally good; one of them
// is chosen at random, the higher band first among exact ties. QA 2026-11-14 12:33 deploy: after a mass login every
// picker scored the same maps against the same counts at once - gef_fild02 139 bots, pay_fild09 76. Together with the
// seat reservations (crowd.go crowdNote) a wave now spreads over the near-equal maps.
type pickCand struct {
	m   string
	s   float64
	min int
}

func spreadPick(cs []pickCand, slack float64) string {
	if len(cs) == 0 {
		return ""
	}
	bs := cs[0].s
	for _, c := range cs {
		bs = math.Min(bs, c.s)
	}
	var near []pickCand
	for _, c := range cs {
		if c.s <= bs+slack {
			near = append(near, c)
		}
	}
	return near[crand(len(near))].m
}

// routeCacheTH: "from|dest|lv" -> the walk crosses a map too hard for that level (static data + level: cached)
var routeCacheTH sync.Map

// routeCrossesTooHard: the walking route from (from, x, y) to dest crosses a map (dest excluded) that partyMapTooHard
// rates too hard for minLv - a train map is deadly to walk through as well (mjolnir_02 on the way to mjo_dun01).
func routeCrossesTooHard(from string, x, y int, dest string, minLv int) bool {
	k := fmt.Sprintf("%s|%s|%d", from, dest, minLv)
	if v, ok := routeCacheTH.Load(k); ok {
		return v.(bool)
	}
	bad := false
	for _, w := range RouteMaps(from, x, y, dest, minLv) {
		if w.To != dest && !isTown(w.To) && partyMapTooHardHere(w.To, minLv) {
			bad = true
			break
		}
	}
	routeCacheTH.Store(k, bad)
	return bad
}

// leaderPartyPick (b.mu held, b leads a bot party): the leader's goal was dropped (no route, a death, a job change,
// a panic reset...). It used to re-pick a map for its OWN level (Set "auto") and the members followed it there -
// how parties ended up ~20 levels over their weakest member. Now the party map stays the party's: the current one
// while it suits the weakest member and is reachable, else the nearest one that does. False: nothing fits.
func (b *Bot) leaderPartyPick(p *Party) bool {
	if h := huntOf(p); h != nil { // bosshunt.go: the party is on a boss hunt - its map is the boss map
		if b.Goal.Map != h.Boss.Map {
			b.Goal, b.route, b.path = Goal{Mode: "hunt", Map: h.Boss.Map, Set: "party-leader"}, nil, nil
		}
		return true
	}
	parties.Lock()
	ms, cur, anchored, aitem := append([]*Bot(nil), p.Members...), p.Map, p.partyAnchored(), p.AnchorItem
	staying := !p.ArrivedAt.IsZero() && !p.ArrivedAt.Before(p.MovedAt) && time.Since(p.ArrivedAt) < partyStayTime
	parties.Unlock()
	bad := func(m string) bool {
		return m == "" || isTown(m) || m == b.badMap && time.Now().Before(b.badMapUntil) || mapHops(b.MapName, b.X, b.Y, m) >= 99 ||
			partyBeginnerFar(ms, m) // lfg.go: never a map far from a beginner member's home town
	}
	m := ""
	if !bad(cur) && (partyMapOK(ms, cur) || anchored && goalMapSafeItem(ms, cur, aitem) || staying && mapStayOK(ms, cur)) { // lfg.go: an item goal's map, while safe
		m = cur
	} else if n := partyMapNear(ms, b.MapName, b.X, b.Y, cur); !bad(n) {
		m = n
		parties.Lock()
		back := p.partyLeftRecently(n, time.Now())
		parties.Unlock()
		if back && !bad(cur) {
			m = cur // never straight back to the map the party left < 30 min ago (A -> B -> A): stay
		}
	}
	if m == "" {
		return false
	}
	b.Goal, b.route, b.path = Goal{Mode: "hunt", Map: m, Set: "party-leader"}, nil, nil
	if m != cur {
		b.event("party", "", fmt.Sprintf("re-planning the party map: %s -> %s (fits the weakest member)", cur, m))
		go p.moveTo(m)
		sayPartyDest(b, b.P.Typing, p, m, "better spot for all of us") // partydest.go PT30: say where to be
	}
	return true
}

// peekLv: another bot's base level without taking its lock (the published snapshot)
func peekLv(m *Bot) int {
	if s, ok := peek(m.P.Name); ok && s.Lv > 0 {
		return s.Lv
	}
	return m.P.BaseLevel
}

// drop: b leaves the bot party (own goroutine). A party left with one member disbands.
func (p *Party) drop(b *Bot) {
	parties.Lock()
	var rest []*Bot
	for _, m := range p.Members {
		if m != b {
			rest = append(rest, m)
		}
	}
	p.Members = rest
	if parties.ofBot[b] == p {
		delete(parties.ofBot, b)
	}
	alone := len(rest) < 2 || p.Leader == b
	parties.Unlock()
	lfgNoteLeft(b)
	b.send(newPkt(0x0100).b) // leave party
	b.mu.Lock()
	b.PartyName, b.PartyID = "", 0
	b.Goal, b.route, b.path = Goal{}, nil, nil
	b.mu.Unlock()
	if alone {
		disband(p)
	}
}

// partySpotNear: the closest guide spot (from the leader's map) the whole party can handle, where the weakest
// member's target filter accepts most monsters; never the map being left. Call with no bot lock held.
func partySpotNear(p *Party, from string, x, y int) string {
	minLv, maxLv, fitLv, allRanged, known, minBase := 999, 0, 999, true, 0, 999
	for _, m := range p.Members {
		m.mu.Lock()
		if !m.Online || m.BaseLv <= 0 { // offline / still logging in: its level reads as the persona's lv1 default
			m.mu.Unlock()
			continue
		}
		known++
		lv, tl, r := m.powerLv(), m.targetMaxLv(), partyRole(m) // effective level (gearpower.go)
		if fragileClass(m.Class) {
			lv = max(1, lv-partyFragileLv) // low HP / weak fighter: dies first in a crowd
		}
		minBase = min(minBase, m.BaseLv)
		m.mu.Unlock()
		minLv, maxLv, fitLv = min(minLv, lv), max(maxLv, lv), min(fitLv, tl)
		allRanged = allRanged && (r == roleRanged || r == roleSupport)
	}
	if known == 0 {
		return ""
	}
	if b := partyStrength(p.Members); b > 0 { // lfg.go: a composed party fights above its weakest member
		minLv, fitLv = min(minLv+b, max(minLv, minBase)), fitLv+b
	}
	best, bh, bmin := "", 99, 0
	// maps the party left in the last 30 min are skipped while anything else fits (no A -> B -> A relocations)
	now := time.Now()
	parties.Lock()
	left := map[string]bool{}
	for m := range p.Left {
		if p.partyLeftRecently(m, now) {
			left[m] = true
		}
	}
	lead := ""
	if p.Leader != nil {
		lead = p.Leader.P.Name
	}
	parties.Unlock()
	banned := func(m string) bool { // party.go leaderMem: deaths / fleeing there lately - never, whatever else fits
		bad, gone := leaderAvoids(lead, m, now)
		if gone {
			left[m] = true
		}
		return bad
	}
	// crowd.go: only maps with room for the whole party; after a death (x < 0) any easier map if none has room (the
	// least crowded one). Tier 0: maps that also give the weakest member real exp (mapExpPoor); tier 1: any safe map.
	// Room comes before exp (QA 15:30-15:55: parties piled onto pay_fild07 / gef_fild02 / pay_fild09 / moc_fild01).
	bs := 1 << 30
	// tiers: 0 strict + real exp, 1 relaxed passive + real exp (partyRelaxedPassive), 2 strict, exp-poor allowed
	for _, st := range [][2]int{{0, 0}, {1, 0}, {2, 0}, {0, 1}, {1, 1}, {2, 1}, {0, 2}, {1, 2}, {2, 2}} {
		tier, pass := st[0], st[1]
		if best != "" {
			break
		}
		if pass >= 1 && x >= 0 {
			break
		}
		bh, bmin, bs = 99, 0, 1<<30
		var scs []pickCand
		for _, g := range guideSpots {
			if tier < 2 && mapExpPoor(g.Map, minBase) {
				continue
			}
			if g.Map == from || banned(g.Map) || pass < 2 && left[g.Map] || maps[g.Map] == nil || maxLv > g.Max+3 || (g.Ranged && !allRanged) ||
				swarmVeto(g.Map, minLv) || mapReadsEmpty(g.Map) || bossRecent(g.Map, minLv) || // bossnote.go
				partyMapDeadly(g.Map) || pass == 0 && !mapHasRoom(g.Map, len(p.Members)) {
				continue
			}
			if tier == 1 {
				if !partyPassiveRelaxOK(g, minLv, fitLv) {
					continue
				}
			} else if g.Min > minLv+2 || fitShare(g.Map, fitLv) < minFitShare || partyMapTooHard(g.Map, minLv) {
				continue
			}
			var h int
			if x < 0 { // after a death: measure from the map's own nearest town
				t := nearestTown(from)
				sp := townSpawn[t]
				h = mapHops(t, sp[0], sp[1], g.Map)
			} else {
				h = mapHops(from, x, y, g.Map)
			}
			if h > 8 {
				continue // nothing across the world
			}
			if x >= 0 && routeCrossesTooHard(from, x, y, g.Map, minLv) {
				continue // the walk crosses a train map (mjolnir_02 on the way to mjo_dun01)
			}
			sc := h
			if mapAggroPct(g.Map) >= 40 {
				sc += partyAggroHops // prefer an equally near passive map
			}
			if pass >= 1 {
				sc += crowdPct(g.Map) / 50 // all full: the least crowded
			}
			// zenyhour.go (economy run 8): in tenths of a map change, zphPerHop zeny/hour = one map (max 3)
			sc = sc*10 - int(min(30, partyZenyHour(g.Map, fitLv)*10/zphPerHop))
			if sc < bs || (sc == bs && g.Min > bmin) {
				bh, bmin, bs = h, g.Min, sc
			}
			scs = append(scs, pickCand{g.Map, float64(sc), h})
		}
		if len(scs) > 0 { // spread a wave over the near-equal maps (spreadPick); keep its hops for the > 8 check
			best = spreadPick(scs, 10)
			for _, c := range scs {
				if c.m == best {
					bh = c.min
				}
			}
		}
	}
	if best == "" || bh > 8 {
		return "" // nothing sensible nearby: stay (and keep sweeping) rather than march across the world
	}
	crowdNote(best, len(p.Members))
	return best
}

// relocate: pick the party's next map and move everyone there (own goroutine: no bot lock held by the caller).
// why: "" = nothing to hunt / a death loop; else the reason the map is wrong for the party ("too strong for X").
func (p *Party) relocate(from string, x, y int, why string) {
	if p.bossActive() { // bosshunt.go decides when a boss hunt leaves (kill, retreat, timeout)
		return
	}
	m := partySpotNear(p, from, x, y)
	l := p.Leader
	if m == "" || l == nil {
		return
	}
	parties.Lock()
	ms := append([]*Bot(nil), p.Members...)
	parties.Unlock()
	if partyBeginnerFar(ms, m) {
		return // lfg.go: beginners stay near their home town - no relocation across the world for them
	}
	l.mu.Lock()
	// moved on meanwhile? The party's map counts too (a leader still in town / on the way), and a leader whose goal
	// fell back to its own pick ("auto", "party-leader-hold") is a party leader all the same (QA 19:00: silvercat pt's
	// leader had picked mjo_dun01 for itself and the fit check's relocate was refused)
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	if l.MapName != from && l.Goal.Map != from && pm != from || l.Goal.Set == "api" || strings.HasPrefix(l.Goal.Set, "boss") {
		l.mu.Unlock()
		return
	}
	l.Goal, l.route, l.path = Goal{Mode: "hunt", Map: m, Set: "party-leader"}, nil, nil
	if l.badMap != from || time.Now().After(l.badMapUntil) {
		l.badMap, l.badMapUntil = from, time.Now().Add(20*time.Minute) // leaderPartyPick doesn't pick it straight back
	}
	typing := l.P.Typing
	l.mu.Unlock()
	if why != "" {
		l.event("party", "", fmt.Sprintf("%s on %s: moving the party to %s", why, from, m))
		reason := "too rough here for some of us"
		switch {
		case strings.Contains(why, "packed"):
			reason = "too crowded here, nothing to kill"
		case strings.Contains(why, "too easy"):
			reason = "we outgrew this place"
		case strings.Contains(why, "too dangerous for "):
			i := strings.Index(why, "too dangerous for ")
			reason = "too dangerous for " + strings.Fields(why[i+len("too dangerous for "):] + " x")[0]
		case strings.HasSuffix(why, " keeps fleeing"):
			reason = strings.TrimSuffix(why, " keeps fleeing") + " keeps getting chased here"
		}
		l.partySay(styled(typing, planLine(p, m, reason))) // together.go: where, why, where to meet
	} else {
		l.event("party", "", fmt.Sprintf("nothing to hunt on %s for the party: moving to %s", from, m))
		l.partySay(styled(typing, planLine(p, m, "nothing left to hunt here")))
	}
	p.moveTo(m)
}

// moveTo: the party hunts on map m from now on (members' goals follow; a rally gathers them when they are apart).
// Runs in its own goroutine: it locks each member in turn.
func (p *Party) moveTo(m string) {
	parties.Lock()
	if p.Map != "" && p.Map != m {
		if p.Left == nil {
			p.Left = map[string]time.Time{}
		}
		p.Left[p.Map] = time.Now() // partySpotNear / partyMapNear: not straight back here (A -> B -> A)
		if p.Leader != nil {
			leaderNoteLeft(p.Leader.P.Name, p.Map) // survives the party being disbanded and re-formed (party.go leaderMem)
		}
		p.MovedAt = time.Now()
	}
	p.Map, p.Checkpoint = m, nearestTown(m)
	parties.Unlock()
	for _, mb := range p.Members {
		if mb == p.Leader {
			continue
		}
		mb.mu.Lock()
		if mb.Goal.Mode == "party" && mb.Goal.Set == "party" {
			mb.Goal.Map = m
			mb.route, mb.path = nil, nil
		}
		mb.mu.Unlock()
	}
	p.rallyOnForm()
}

// explore: sweep the hunting map for monsters instead of shuffling 5-12 cells around one spot. Bots see ~14 cells;
// jittering in place found nothing, so after 45 s they gave the map up and marched to another one (owner: "90% of
// the time should be on farming instead of roaming around"). Walk to a far cell of this region, then the next.
func (b *Bot) explore(gm *GameMap, now time.Time) {
	// patrol.go: party leaders and solo bots sweep the map on a planned loop (owner rule "Sweep the map"); the random
	// far cell below is only the fallback for a pocket without a loop
	if b.sweep(gm, now) {
		return
	}
	d := b.roamDest
	if d == [2]int{} || dist(b.X, b.Y, d[0], d[1]) <= 3 || now.Sub(b.roamDestAt) > 40*time.Second {
		reg, prev := gm.Region(b.X, b.Y), d
		d = [2]int{}
		// seen.go: head for the nearest monster another bot saw lately and isn't fighting (beyond our own view,
		// same walk region, not in a portal or a boss spot) before sweeping blind
		maxLv, plv := b.targetMaxLv(), b.powerLv()
		// (min 15 cells: our own view; not near the spot we just reached and found empty - no bouncing between stale ones)
		if x, y, ok := sightNear(b.MapName, b.X, b.Y, 15, 80, maxLv, now, func(x, y int) bool {
			return (prev == [2]int{} || dist(x, y, prev[0], prev[1]) > 10) && gm.Walkable(x, y) && gm.Region(x, y) == reg && !gm.OnWarp(x, y) && !bossSpotNear(b.MapName, x, y, 12, plv)
		}); ok {
			d = [2]int{x, y}
		}
		for i := 0; i < 12 && d == [2]int{}; i++ {
			x, y := gm.RandomWalkable(b.rng)
			if gm.Region(x, y) == reg && dist(b.X, b.Y, x, y) >= 20 && dist(b.X, b.Y, x, y) <= 80 {
				d = [2]int{x, y}
				break
			}
		}
		if d == [2]int{} {
			b.roam(gm)
			return
		}
		b.roamDest, b.roamDestAt, b.path = d, now, nil
	}
	b.stepAlong(gm, d[0], d[1])
}

// fight: use the class skill when it's ready and affordable, otherwise auto-attack. Casters keep their
// distance and only melee when out of SP.
func (b *Bot) fight(e *Entity, moving bool, now time.Time) {
	if b.shouldBait() {
		if b.baited == nil || len(b.baited) > 200 {
			b.baited = map[uint32]time.Time{}
		}
		at, tagged := b.baited[e.ID]
		if !tagged {
			if b.closeIn(e, moving, now) {
				return // walk into reach first: a hit sent from afar is refused (0x0139) and pulls nothing
			}
			b.baited[e.ID] = now
			b.setState("baiting " + mobName(e.Class) + " for the party")
			go b.send(newPkt(0x0437).u32(e.ID).u8(0).b) // one single hit pulls it; the party finishes it
			return
		}
		if now.Sub(at) < 6*time.Second {
			return
		}
		// still alive 6 s after the tag: the weaker members aren't killing it - fight it for real (QA 2026-11-14:
		// baiters stood "fighting" one monster forever with full SP, never attacking or using a skill)
	}
	if b.aidPotion(now) { // transfight.go: Alchemists throw a potion at a party member under 45% HP
		return
	}
	if b.transBuffs(e, now) { // transfight.go: Aura Blade, Frenzy, Double Casting, Mystical Amplification (trans classes)
		return
	}
	if b.kiteStep(e, moving, now) { // kite.go: a bow user steps back out of melee reach (hit and run), Ankle Snare
		return
	}
	if b.casterKite(e, moving, now) { // casterkite.go: a Mage-line caster walls / backs off a monster next to it
		return
	}
	if b.trapCast(e, now) { // trap.go: Claymore Trap / Land Mine where the monster will step (trapper Hunter)
		return
	}
	if b.aoeCast(e, now) { // aoe.go: Mage/Wizard area spells on 3+ grouped monsters, Fire Wall against a walker
		return
	}
	if b.casterActive(e, moving, now) { // casteractive.go (CL16): no group -> a spell on a party target in range, no melee chase
		return
	}
	if b.weaponAoE(e, now) { // weaponskill.go: Archer-line Arrow Shower on 3+ grouped monsters
		return
	}
	if b.healAttack(e, now) { // weaponskill.go: acolytes Heal undead monsters (holy damage)
		return
	}
	if b.fightSkills(e, now) { // skillbuff.go: melee self-buffs (Adrenaline Rush, Blessing, Call Spirits...) and Hammer Fall
		return
	}
	if b.asuraBurst(e, now) { // weaponskill.go: the Asura Monk's Fury -> Asura Strike on a tough monster
		return
	}
	if b.stripTarget(e, now) { // variantskill.go: Divest Weapon / Shield on a strong monster (strip Rogue, Stalker)
		return
	}
	if b.occultFight(e, now) { // variantskill.go: Occult Monk - Mental Strength when mobbed, Occult Impaction on armour
		return
	}
	// tanks deal damage too (owner: "the tank ... needs to deal damage too. Apply that to all tanks"): with two or
	// more monsters around, a Swordman-line tank uses Magnum Break (area damage around itself, keeps the aggro)
	if family(b.Class) == famSword && now.After(b.skillReady) {
		if si := b.skills[smMagnum]; si != nil && si.Lv > 0 && b.SP >= si.SP+10 && b.SP-si.SP >= b.MaxSP/4+b.keyReserve(now) { // SP for emergencies (and a due key buff)
			near := 0
			for _, o := range b.ents {
				if ox, oy := o.pos(now); o.Type == objMob && dist(b.X, b.Y, ox, oy) <= 2 {
					near++
				}
			}
			if near >= 2 {
				b.skillReady = now.Add(time.Duration(900+b.P.ReactionMs/2) * time.Millisecond)
				b.targetAt, b.attackSentFor = now, 0 // resume auto-attack after the skill
				go b.useSkill(smMagnum, si.Lv, b.aid)
				return
			}
		}
	}
	sk := b.bestAttack()
	ex, ey := e.pos(now)
	// run 7 (skillaudit: Bash / Back Stab / Mammonite 24-42% of requests silently dropped): rAthena drops a skill whose
	// target is out of range at the SERVER's positions without any packet (unit_skilluse_id2 "Arrow-path check
	// failed"). b.X is our walk's START while we move, and a walking monster is somewhere along its path - so a melee
	// skill goes out only from where we really are (myPos) to a target that stands still, and never while we walk.
	mx, my := b.myPos(now)
	d := dist(mx, my, ex, ey)
	if sk != nil && sk.Range <= 3 && (moving || ex != e.DX || ey != e.DY) {
		d = 99 // auto-attack (the server keeps the swing in reach); the skill waits until both stand
	}
	reserve := int(float64(b.MaxSP) * 0.15 * (1 - b.P.Aggression))
	if !b.spCaster() {
		// QA 2026-11-14: Echobunny sat at 11/200 SP; Archers emptied SP on Double Strafe then sat. Non-casters keep
		// skillSPFloor% back for emergencies and auto-attack below it while SP regenerates (they never sit for SP)
		reserve = max(reserve, b.MaxSP*skillSPFloor/100)
	}
	if sk != nil && sk.Range <= 3 && b.tankHeld(now) < 2 {
		// tanklead.go: a party tank fighting one monster keeps the SP for two Bowling Bash / Brandish Spear casts
		reserve = max(reserve, b.tankAoEReserve())
	}
	if !b.spCaster() {
		reserve += b.keyReserve(now) // skillbuff.go: a due Two-Hand Quicken / Adrenaline Rush / Auto Guard comes first
	}
	if sk != nil && b.SP-sk.SP >= reserve && now.After(b.skillReady) &&
		(sk.ID != mcMammo || b.rng.Float64() < 0.35) {
		if d <= sk.Range {
			id, lv := sk.ID, sk.Lv
			if sk.Elemental {
				id, lv = b.boltFor(mobs[e.Class], sk.Lv) // skills.go: the counter element this character has learned
			}
			b.skillReady = now.Add(time.Duration(sk.CastMs+b.P.ReactionMs/2) * time.Millisecond)
			go b.useSkill(id, lv, e.ID)
			b.targetAt, b.attackSentFor = now, 0 // resume auto-attack after the skill
			return
		}
		if sk.Range > 3 && !moving { // ranged: close in to casting range, not melee
			go b.walkTo(ex+sign(b.X-ex)*min(sk.Range-2, abs(b.X-ex)), ey+sign(b.Y-ey)*min(sk.Range-2, abs(b.Y-ey)))
			return
		}
	}
	if (family(b.Class) == famMage || (sk != nil && sk.Elemental && b.SP >= sk.SP)) && now.Before(b.skillReady) {
		return // casting (a bolt, Jupitel Thunder or an area spell from aoe.go); don't walk in for a melee swing
	}
	// auto-attack: send the continuous attack ONCE per target and let the server keep swinging (owner: "turn on auto
	// attack and send attack once only. That's avoid reset"). Re-send only after a skill (skills end the auto-attack)
	// or when the server has shown no hit on the target for 4 s (the swing really stopped).
	// The server only swings while the target stays in reach: rAthena never walks a player to its target, so first
	// close in (closeIn), and after a "too far" (0x0139) the attack is re-sent once we're in reach again.
	if b.closeIn(e, moving, now) {
		return
	}
	if b.attackSentFor != e.ID || (now.Sub(b.hitTargetAt) > 4*time.Second && now.Sub(b.targetAt) > 4*time.Second) {
		b.attackSentFor, b.targetAt = e.ID, now
		go b.attack(e.ID)
	}
}

// myPos: our position now, interpolated along the walk the server accepted (b.X/b.Y stay at the walk's start cell
// until it ends).
func (b *Bot) myPos(now time.Time) (int, int) {
	steps := max(abs(b.DestX-b.X), abs(b.DestY-b.Y))
	if steps == 0 || !now.Before(b.MoveUntil) {
		return b.DestX, b.DestY
	}
	left := int(b.MoveUntil.Sub(now) / (time.Duration(max(b.Speed, 20)) * time.Millisecond))
	done := max(steps-left, 0)
	return b.X + (b.DestX-b.X)*done/steps, b.Y + (b.DestY-b.Y)*done/steps
}

// closeIn walks toward the target while it is out of our attack reach and reports whether we're still closing in.
// Root cause of "melee standing still next to / near its target, not attacking" (lessons.md 2026-11-14): rAthena's
// unit_attack_timer_sub answers a player's attack on an out-of-reach target with 0x0139 and drops the attack - it
// never chases (only monsters chase). The bots sent the attack once and waited for the server to walk them in, so
// any target 2+ cells away (or one that stepped away) was never hit. Reach uses the server's client distance
// (rounded-down euclid - 0.1), i.e. Chebyshev 1 for range 1, plus 1 when the target is walking.
func (b *Bot) closeIn(e *Entity, moving bool, now time.Time) bool {
	reach := max(b.atkRange, 1)
	ex, ey := e.pos(now)
	mx, my := b.myPos(now)
	d := clientDist(mx, my, ex, ey)
	// in reach - unless the server just told us otherwise for this target (then walk anyway, our positions are off)
	if d <= reach && !(b.tooFarID == e.ID && now.Sub(b.tooFarAt) < time.Second) {
		return false
	}
	b.attackSentFor = 0 // whatever was sent is void out of reach; send it again once we're there
	// walk to where the target is heading; re-aim when it has wandered off from our walk's end, at most every 400 ms
	tx, ty := e.DX, e.DY
	if !moving || (clientDist(b.DestX, b.DestY, tx, ty) > reach && now.Sub(b.chaseAt) > 400*time.Millisecond) {
		if now.Sub(b.chaseAt) > 250*time.Millisecond {
			b.chaseAt = now
			b.path = nil
			go b.walkTo(tx, ty)
		}
	}
	return true
}

// clientDist: rAthena's distance_client (what check_distance_client_bl uses for a player's attack reach).
func clientDist(x0, y0, x1, y1 int) int {
	dx, dy := float64(x1-x0), float64(y1-y0)
	v := math.Sqrt(dx*dx+dy*dy) - 0.1
	if v < 0 {
		return 0
	}
	return int(v)
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func (b *Bot) roam(gm *GameMap) {
	r := 5 + int((1-b.P.Patience)*7)
	for i := 0; i < 8; i++ {
		x, y := b.X+b.rng.Intn(2*r+1)-r, b.Y+b.rng.Intn(2*r+1)-r
		if gm.Walkable(x, y) && gm.LineWalkable(b.X, b.Y, x, y) {
			b.path = nil
			go b.walkTo(x, y)
			return
		}
	}
	// stuck in a corner: long jump toward a random walkable cell
	x, y := gm.RandomWalkable(b.rng)
	b.path = nil
	b.stepAlong(gm, x, y)
}

func (b *Bot) loiter(gm *GameMap, moving bool) {
	b.setState("idle in " + b.MapName)
	if !moving && gm != nil && b.rng.Float64() < 0.05 {
		b.roam(gm)
	}
}

func (b *Bot) follow(gm *GameMap, moving bool) {
	b.setState("following " + b.Goal.Who)
	for _, e := range b.ents {
		if e.Type == objPC && e.Name == b.Goal.Who {
			if dist(b.X, b.Y, e.X, e.Y) > 3 && !moving {
				go b.walkTo(e.DX+b.rng.Intn(3)-1, e.DY+b.rng.Intn(3)-1)
			}
			return
		}
	}
	b.loiter(gm, moving)
}

func mobName(id int) string {
	if m := mobs[id]; m != nil {
		return m.Name
	}
	return "?"
}

// ---------------------------------------------------------------- online schedule

type sched struct {
	enabled bool
	speed   float64 // simulated hours per real hour (for testing day cycles)
	start   time.Time
}

var scheduler = &sched{}

func (s *sched) simHour() float64 {
	el := time.Since(s.start).Hours() * s.speed
	h := float64(s.start.Hour()) + float64(s.start.Minute())/60 + el
	for h >= 24 {
		h -= 24
	}
	return h
}

// simDay: day index of the simulated clock (for day-to-day variation and weekends)
func (s *sched) simDay() int {
	el := time.Since(s.start).Hours() * s.speed
	h := float64(s.start.Hour()) + float64(s.start.Minute())/60 + el
	return s.start.YearDay() + int(h/24)
}

// window: today's play session for a persona, varying day to day like a real person's evenings
// (start ±1.5h, length ±40%, longer and earlier on weekends, the occasional skipped day).
func (s *sched) window(p *Persona, day int) (start, end float64, skip bool) {
	r := rand.New(rand.NewSource(int64(p.AccountID)*1000 + int64(day)))
	weekend := (day+int(s.start.Weekday())-s.start.YearDay())%7 == 0 || (day+int(s.start.Weekday())-s.start.YearDay())%7 == 6
	start = float64(p.SessionStart) + (r.Float64()*3 - 1.5)
	dur := float64(p.SessionHours) * (0.6 + r.Float64()*0.8)
	if weekend {
		dur *= 1 + p.WeekendBoost
		start -= 2 * r.Float64() * p.WeekendBoost
	}
	skip = r.Float64() < 0.12*(1-p.Patience)
	return start, start + dur, skip
}

func (s *sched) ShouldBeOnline(b *Bot) bool {
	if !s.enabled {
		return true
	}
	if b.guildBrewerChar() != "" {
		return true // alchemy.go (economy run 9): the guild's brewer is its dedicated crafter, on for long hours
	}
	h, d := s.simHour(), s.simDay()
	for _, dd := range []int{0, -1} { // today's session, or yesterday's running past midnight
		st, en, skip := s.window(b.P, d+dd)
		hh := h - float64(dd*24)
		if !skip && hh >= st && hh < en {
			return true
		}
	}
	return false
}

// sessionEnd: when the current session should end (zero without a schedule)
func (s *sched) sessionEnd(b *Bot) time.Time {
	if !s.enabled {
		return time.Time{}
	}
	h, d := s.simHour(), s.simDay()
	for _, dd := range []int{0, -1} {
		st, en, skip := s.window(b.P, d+dd)
		hh := h - float64(dd*24)
		if !skip && hh >= st && hh < en {
			return time.Now().Add(time.Duration((en - hh) / s.speed * float64(time.Hour)))
		}
	}
	return time.Time{}
}

var onlineSlots chan struct{} // caps concurrent sessions (profiles > online players)

func (s *sched) logoutSignal(b *Bot) <-chan struct{} {
	if !s.enabled {
		return nil
	}
	ch := make(chan struct{})
	go func() {
		var holdSince time.Time
		for {
			if s.ShouldBeOnline(b) || b.pinned() != nil || b.alwaysOnline() { // guild leaders never end a session
				holdSince = time.Time{}
			} else if b.dealOpen() && (holdSince.IsZero() || time.Since(holdSince) < 15*time.Minute) {
				// economy run 10 (life mind, 19:10-19:20: Shadow_fire agreed to bring Emveretarcon to thenovaknight87,
				// then logged off at the end of its session): a player finishes the trade it agreed to before it
				// logs off - at most 15 min past the session end (deal deadlines are 4-10 min)
				if holdSince.IsZero() {
					holdSince = time.Now()
				}
			} else {
				break
			}
			time.Sleep(time.Duration(20+b.rng.Intn(20)) * time.Second)
		}
		close(ch)
	}()
	return ch
}

// ---------------------------------------------------------------- events ring

type eventRing struct {
	mu   sync.Mutex
	buf  []Event
	next int64
	base int64
}

var events = &eventRing{}

func (r *eventRing) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Kind == "party" { // the same party line heard by another member: one event, hearers listed
		for i := len(r.buf) - 1; i >= 0 && i >= len(r.buf)-200; i-- {
			p := &r.buf[i]
			if e.T.Sub(p.T) > 3*time.Second {
				break
			}
			if p.Kind == "party" && p.Text == e.Text && p.Bot != e.Bot {
				if len(p.HeardBy) < 12 {
					p.HeardBy = append(p.HeardBy, e.Bot)
				}
				return
			}
		}
	}
	r.buf = append(r.buf, e)
	r.next++
	if len(r.buf) > 5000 {
		r.buf = r.buf[1000:]
		r.base += 1000
	}
}

func (r *eventRing) since(id int64) ([]Event, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id < r.base {
		id = r.base
	}
	i := int(id - r.base)
	if i > len(r.buf) {
		i = len(r.buf)
	}
	return append([]Event{}, r.buf[i:]...), r.next
}

var _ = atomic.AddInt64

// partyMapTooEasy: the weakest member (effective level) has outgrown map m - above every guide band listed for it,
// or, off the guide list, the monsters average 20+ levels under it. No bot locks.
func partyMapTooEasy(ms []*Bot, m string) bool {
	minLv, _, known, _ := partyFitLv(ms)
	if known == 0 {
		return false
	}
	listed := false
	for _, g := range guideSpots {
		if g.Map == m {
			listed = true
			if minLv <= g.Max+5 { // clearly outgrown only (pickers stop choosing it at Max+3): no leaving a farmable map
				return false
			}
		}
	}
	if listed {
		return true
	}
	ml := mapMeanLv(m)
	return ml > 0 && ml < minLv-20
}
