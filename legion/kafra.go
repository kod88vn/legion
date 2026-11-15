package main

// Kafra teleport service for long trips (owner: "they have kafra for fast travel. Stop the march").
// A bot in a town with a Kafra Employee whose walk to its goal is long checks the Kafra's destinations (fixed per
// town, from rAthena's F_KafSet in npc/kafras/functions_kafras.txt; prices are 1/10 of official, see
// rathena/npc-custom/legion_kafra.txt) and teleports when a destination is clearly closer to the goal.
// Dialog: talk -> menu "Use Teleport Service" -> next -> menu "<Destination> -> <price> z" -> warped.
//
// Save point rule (owner: "save the spawn at the closest kafra near grinding ground"): a bot heading out to grind
// that passes through the town nearest its grinding map saves there first (Kafra menu "Save"), so a death or a
// Butterfly Wing brings it back next to its hunting ground instead of across the world.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type kafraSpot struct{ X, Y int }

type kafraDest struct {
	Name  string // as shown in the menu ("Payon -> 120 z")
	Map   string
	X, Y  int
	Price int
}

// Kafra Employees in the main towns (npc/kafras/kafras.txt)
var kafraSpots = map[string][]kafraSpot{
	"prontera":  {{152, 326}, {151, 29}, {29, 207}, {282, 200}, {146, 89}},
	"payon":     {{181, 104}, {175, 226}},
	"geffen":    {{120, 62}, {203, 123}},
	"morocc":    {{156, 97}, {160, 258}}, // + the north Kafra (npc/kafras/kafras.txt kaf_morocc2)
	"izlude":     {{128, 148}},            // npc/re/kafras/kafras.txt (travel-QA 2026-11-14: was missing, Izlude bots walked)
	"cmd_fild07": {{136, 134}},            // Comodo Pharos Beacon
	"alberta":   {{28, 229}, {113, 60}},
	"aldebaran": {{143, 119}},
	"yuno":      {{152, 187}, {327, 108}, {277, 221}},
	"comodo":    {{195, 150}},
	"umbala":    {{87, 160}},
}

var (
	kdIzlude   = kafraDest{"Izlude", "izlude", 128, 98, 60}
	kdGeffen   = kafraDest{"Geffen", "geffen", 120, 39, 120}
	kdPayon    = kafraDest{"Payon", "payon", 161, 58, 120}
	kdMorocc   = kafraDest{"Morocc", "morocc", 156, 46, 120}
	kdOrc      = kafraDest{"Orc Dungeon", "gef_fild10", 52, 326, 170}
	kdAlberta  = kafraDest{"Alberta", "alberta", 117, 56, 180}
	kdProntera = kafraDest{"Prontera", "prontera", 116, 72, 120}
	kdAldeb    = kafraDest{"Al De Baran", "aldebaran", 168, 112, 120}
	kdJuno     = kafraDest{"Juno", "yuno", 158, 125, 120}
	kdMjolnir  = kafraDest{"Mjolnir Dead Pit", "mjolnir_02", 99, 351, 170}
	kdComodo   = kafraDest{"Comodo", "comodo", 209, 143, 180}
	kdPharos   = kafraDest{"Comodo Pharos Beacon", "cmd_fild07", 127, 134, 120}
	kdUmbala   = kafraDest{"Umbala", "umbala", 100, 154, 180}
)

func withPrice(d kafraDest, p int) kafraDest { d.Price = p; return d }

var kafraDests = map[string][]kafraDest{
	"prontera":  {kdIzlude, kdGeffen, kdPayon, kdMorocc, kdOrc, kdAlberta},
	"alberta":   {withPrice(kdPayon, 120), withPrice(kdMorocc, 180), withPrice(kdProntera, 180)},
	"aldebaran": {kdGeffen, kdJuno, withPrice(kdIzlude, 180), kdMjolnir},
	"comodo":    {withPrice(kdMorocc, 180), kdPharos, kdUmbala},
	"geffen":    {kdProntera, kdAldeb, kdOrc, kdMjolnir},
	"morocc":    {kdProntera, kdPayon, kdAlberta, kdComodo, kdPharos},
	"umbala":    {kdComodo},
	"payon":     {kdProntera, withPrice(kdAlberta, 120), kdMorocc},
	"yuno":      {kdAldeb},
	// F_KafSet (rathena/npc-custom/functions_kafras.txt): izlude -> Geffen, Payon, Morocc 120, Al De Baran 180;
	// cmd_fild07 -> Comodo, Morocc 120
	"izlude":     {kdGeffen, kdPayon, kdMorocc, withPrice(kdAldeb, 180)},
	"cmd_fild07": {withPrice(kdComodo, 120), kdMorocc},
}

type kafraTrip struct {
	Save    bool // just set the save point here (no teleport)
	Cart    bool // rent a pushcart (vendors, vend.go)
	Store   bool // put kept items in Kafra storage (storage.go)
	Stored  bool // the storage window opened and the items went in
	Dest    kafraDest
	Spot    kafraSpot
	Town    string
	Stage   string // walk, talk, dialog
	AX, AY  int    // walkable cell next to the Kafra to stand on
	Started time.Time
	At      time.Time
	NPC     uint32
}

// kafraMinSaving(): teleport only if it saves at least this many map changes (tune().KafraMinSaving, tuning.go)

// kafraPlan: a Kafra teleport that gets the bot much closer to goal, if it stands in a Kafra town and can pay.
func (b *Bot) kafraPlan(goal string, now time.Time) *kafraTrip {
	town := b.MapName
	spots, dests := kafraSpots[town], kafraDests[town]
	if len(spots) == 0 || len(dests) == 0 || goal == "" || goal == town || now.Before(b.kafraNext) || b.kafraHold("", goal, now) {
		return nil
	}
	here := mapHops(town, b.X, b.Y, goal) + swarmPenalty(town, b.X, b.Y, goal, b.powerLv()) // swarm.go: teleport around swarm maps
	saving := b.kafraSaving(now)
	sp := spots[0]
	for _, s := range spots[1:] {
		if dist(b.X, b.Y, s.X, s.Y) < dist(b.X, b.Y, sp.X, sp.Y) {
			sp = s
		}
	}
	// walking time too (walkCells): a route over a few huge maps (the Morroc desert, Geffen -> mjolnir_02 ~2,100
	// cells) is "few map changes" but 10+ minutes of walking - take the Kafra when it saves kafra_min_save_sec
	walkHere := walkCells(town, b.X, b.Y, goal) + swarmPenalty(town, b.X, b.Y, goal, b.powerLv())*swarmCells
	toKafra := dist(b.X, b.Y, sp.X, sp.Y) + kafraOverheadCells
	var best *kafraDest
	bh, bc := here-saving, walkHere-kafraMinSaveCells()
	// owner rule: never walk through maps far above our level. QA 2026-11-14: lilzensenpaiVend (Alchemist lv61, power
	// lv31) walked prontera -> mjolnir_08/04/05/12 -> Al De Baran, died to a lv59 Flora, respawned and walked the same
	// way again: no single Kafra hop beat the walk (Geffen -> Al De Baran on foot is even longer), the chained
	// prontera -> Geffen -> Al De Baran was never considered. When the walk is unsafe, a teleport whose onward trip is
	// safe wins even if it is up to 2 map changes longer.
	unsafe := !b.kafraWalkSafe(goal, now)
	safeH := here + 2
	following := b.followingParty() // together.go: a party move - it must be able to take the same Kafra hop
	short := 0                      // the cheapest useful fare it couldn't pay
	for i := range dests {
		d := dests[i]
		keep := tune().KafraKeep // keep a little for potions
		if b.BaseLvOr() < beginnerLv() {
			keep = tune().KafraKeepBeginner // beginners teleport only with money to spare (owner rule)
		}
		if following {
			keep = 0 // following the party's move: the fare comes before the potion money
		}
		if !b.kafraLandingOK(d, goal) || b.kafraHold(d.Map, goal, now) {
			continue
		}
		if b.Zeny < d.Price+keep {
			if following && (short == 0 || d.Price < short) {
				if oh, _, _ := b.kafraOnward(d.Map, d.X, d.Y, goal, d.Price, now, town); oh+1 < here {
					short = d.Price
				}
			}
			continue
		}
		// onward from the landing: walk, or one more Kafra teleport there (prontera -> Geffen -> Al De Baran)
		oh, oc, osafe := b.kafraOnward(d.Map, d.X, d.Y, goal, d.Price, now, town) // (swarm penalties included)
		if oh >= 99 {
			continue // no way on from that landing at all, walking or by its Kafra
		}
		h, c := oh, toKafra+oc
		if here >= saving+1 && h+1 <= bh || c <= bc || unsafe && osafe && h+1 <= safeH {
			best, bh, bc = &dests[i], min(bh, h+1), min(bc, c)
			if unsafe && osafe {
				safeH = min(safeH, h+1)
			}
		}
	}
	if best == nil {
		if short > 0 && !b.kafraVirtual {
			b.askFare(short-b.Zeny, town, goal, now) // together.go: the party pays a broke member's fare
		}
		return nil
	}
	return &kafraTrip{Dest: *best, Spot: sp, Town: town, Stage: "walk", Started: now, At: now}
}

// kafraSaving (b.mu held): the map changes a Kafra teleport must save. On the way back from a town trip (started in
// the last 20 min) any saving will do: the fare home was kept for it (town.go fareHome), and walking 3 big maps home
// costs more farming than the fare (rally QA 2026-11-14: parties took the Kafra out to Prontera, then walked
// prontera > mjolnir_09 > geffen > gef_fild07 back, 6-8 map changes per run).
func (b *Bot) kafraSaving(now time.Time) int {
	if !b.lastTripAt.IsZero() && now.Sub(b.lastTripAt) < 20*time.Minute && b.trip == nil {
		return 1
	}
	return kafraMinSaving()
}

// kafraSavePlan: save here if this town is the nearest one to the grinding map and we haven't saved here yet.
func (b *Bot) kafraSavePlan(goal string, now time.Time) *kafraTrip {
	town := b.MapName
	spots := kafraSpots[town]
	if saved, known := b.knownSave(); known && saved == town {
		return nil // already saved here (a guessed save point doesn't count: together.go knownSave)
	}
	// owner rule (enforced 2026-11-14: "always save location to a kafra closest to hunting ground"): the save point is
	// THE nearest Kafra town of the hunting map - no "close enough" exception (saveStillGood) and no waiting for a
	// human leader to be in town first (that gate kept Ayla's friends from saving at the Morocc rally before the Sphinx)
	if len(spots) == 0 || goal == "" || isTown(goal) || now.Before(b.kafraNext) {
		return nil
	}
	// a human leader's friends save where SHE saved (her save point / rally town), never in a pass-through town (QA 14:00:
	// Ayla saved in Payon, her friends took another road and saved in Alberta, the engine's "nearest Kafra" of
	// pay_fild10); bot parties and solo bots: the hunting map's nearest Kafra town
	if p := partyOf(b); p != nil && p.Human != "" {
		if humanSaveTown(p) != town {
			return nil
		}
	} else if nearestTown(goal) != town {
		return nil
	}
	sp := spots[0]
	for _, s := range spots[1:] {
		if dist(b.X, b.Y, s.X, s.Y) < dist(b.X, b.Y, sp.X, sp.Y) {
			sp = s
		}
	}
	return &kafraTrip{Save: true, Spot: sp, Town: town, Stage: "walk", Started: now, At: now}
}

// kafraThink drives an active Kafra trip; true while it owns the bot's movement (b.mu held).
func (b *Bot) kafraThink(gm *GameMap, moving bool, now time.Time) bool {
	k := b.kafra
	if k == nil {
		return false
	}
	if b.MapName != k.Town { // warped (or wandered off): done
		b.kafra, b.route, b.path = nil, nil, nil // (the teleport was logged by noteKafraHop on the map change)
		// no wait at all (was 2 min, then 15 s): the next hop of a chain must go at once - QA 2026-11-14 18:30Z, 16 "no
		// route from morocc 156,46: Butterfly Wing home" in 15 min: landed in sealed Morocc for the Morocc -> Payon/
		// Alberta hop, kafraPlan said "not yet", travel found no walking route and the bot flew home. kafraHold guards loops.
		b.kafraNext = time.Time{}
		// a teleport out to a town run, or a human leader's party member: the way back must be able to take the Kafra
		// again as soon as the shopping is done (QA 2026-11-14 12:17: Echobunny and Zen.fox finished shopping in
		// Prontera within the 2 min, kafraPlan said no, and they walked prontera > prt_fild08 > moc_fild01 > moc_fild02
		// > pay_gld > payon, split from the party for 6+ min). kafraHold still guards against teleport loops.
		// QA 2026-11-14 16:43-16:45Z: the 2 min also blocked the SECOND hop of a planned chain and the way back after
		// an errand: lilzensenpaiVend landed in Al De Baran (from Geffen), bought at the Alchemist Union in 45 s,
		// kafraPlan said "not yet", and it walked into mjolnir_12 and died; TheShadowsenpai landed in Geffen for the
		// Geffen -> Al De Baran hop and walked out to gef_fild00 instead. 15 s for everyone now.
		return false
	}
	if k.Cart && b.hasCart {
		b.kafra = nil
		b.event("info", "", "rented a pushcart at the Kafra in "+k.Town)
		return false
	}
	if k.Save && b.savedTown == k.Town && k.Stage == "dialog" && now.Sub(k.At) > 2*time.Second {
		b.kafra = nil // saved; carry on to the hunting ground
		b.event("info", "", "saved respawn point at the Kafra in "+k.Town)
		if partyOf(b) != nil && b.P != nil { // owner: members report the save so the leader can confirm
			typing, town := b.P.Typing, k.Town
			go b.partySay(styled(typing, pick("saved at "+town, "saved in "+town+" kafra", "ok saved at "+town)))
		}
		return false
	}
	if now.Sub(k.Started) > 90*time.Second || (k.Stage == "dialog" && now.Sub(k.At) > 15*time.Second) {
		b.event("info", "", fmt.Sprintf("Kafra failed in %s at stage %s (pos %d,%d, kafra %d,%d, zeny %d)",
			k.Town, k.Stage, b.X, b.Y, k.Spot.X, k.Spot.Y, b.Zeny))
		b.kafra = nil // couldn't reach / talk to the Kafra: walk instead, try again later
		b.kafraNext = now.Add(5 * time.Minute)
		if npc := k.NPC; npc != 0 && k.Stage == "dialog" {
			// a Kafra dialog left open server-side keeps sd->npc_id set: rAthena then refuses every walk and item use
			// (pc_cant_act), and answerStrayDialog skips dialogs while the trip is in its dialog stage
			b.dialogNPC = 0
			go func() {
				b.send(newPkt(0x00b9).u32(npc).b)
				b.send(newPkt(0x00b8).u32(npc).u8(255).b)
				b.send(newPkt(0x0146).u32(npc).b)
			}()
		}
		return false
	}
	if k.Cart {
		b.setState("renting a pushcart at the Kafra")
	} else if k.Store {
		b.setState("storing items at the Kafra in " + k.Town)
	} else if k.Save {
		b.setState("saving at the Kafra in " + k.Town)
	} else {
		b.setState("going to the Kafra (" + k.Dest.Name + ")")
	}
	switch k.Stage {
	case "walk":
		if k.AX == 0 && gm != nil { // a walkable cell right next to the Kafra (the NPC's own cell is blocked)
			k.AX, k.AY = k.Spot.X, k.Spot.Y
			best := 99
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					x, y := k.Spot.X+dx, k.Spot.Y+dy
					if (dx != 0 || dy != 0) && gm.Walkable(x, y) && dist(b.X, b.Y, x, y) < best {
						k.AX, k.AY, best = x, y, dist(b.X, b.Y, x, y)
					}
				}
			}
		}
		if dist(b.X, b.Y, k.Spot.X, k.Spot.Y) > 3 {
			if !moving && gm != nil {
				b.stepAlong(gm, k.AX, k.AY)
			}
			return true
		}
		k.Stage, k.At = "talk", now
		fallthrough
	case "talk":
		npc := b.npcAt(k.Spot.X, k.Spot.Y)
		if npc == 0 {
			if now.Sub(k.At) > 10*time.Second {
				b.event("info", "", fmt.Sprintf("Kafra failed in %s: no NPC seen at %d,%d", k.Town, k.Spot.X, k.Spot.Y))
				b.kafra, b.kafraNext = nil, now.Add(5*time.Minute)
				return false
			}
			return true
		}
		k.NPC, k.Stage, k.At = npc, "dialog", now
		if b.Sitting {
			go b.stand()
		}
		go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	}
	return true
}

// onKafraDialog answers NPC dialog packets while a Kafra trip is talking (b.mu held). Other dialogs are left alone.
func (b *Bot) onKafraDialog(op uint16, pk []byte) {
	k := b.kafra
	if k == nil || k.Stage != "dialog" || len(pk) < 6 {
		return
	}
	switch op {
	case 0x00b5: // next
		id := le.Uint32(pk[2:])
		k.At = time.Now()
		go b.send(newPkt(0x00b9).u32(id).b)
	case 0x00b6: // close
		id := le.Uint32(pk[2:])
		go b.send(newPkt(0x0146).u32(id).b)
	case 0x00b7: // menu: len.W id.L "opt1:opt2:..."
		if len(pk) < 9 {
			return
		}
		id := le.Uint32(pk[4:])
		opts := strings.Split(cstr(pk[8:]), ":")
		choice := 255 // cancel
		for i, o := range opts {
			o = strings.TrimSpace(o)
			if k.Store {
				if strings.Contains(o, "Storage") {
					choice = i + 1
					break
				}
				continue
			}
			if k.Cart {
				if strings.HasPrefix(o, "Rent a Pushcart") {
					choice = i + 1
					break
				}
				continue
			}
			if k.Save {
				if o == "Save" {
					choice = i + 1
					b.noteSaved(k.Town) // together.go: remembered across restarts (memory.go)
					break
				}
				continue
			}
			if strings.HasPrefix(o, k.Dest.Name+" ->") || o == "Use Teleport Service" {
				choice = i + 1
				break
			}
		}
		k.At = time.Now()
		go b.send(newPkt(0x00b8).u32(id).u8(byte(choice)).b)
		if choice == 255 {
			b.event("info", "", "Kafra failed: menu without our option: "+strings.Join(opts, "|"))
			b.kafra, b.kafraNext = nil, time.Now().Add(10*time.Minute)
		}
	}
}

// kafraTeleCost: map changes from town t to goal, using its Kafra if that helps (+1 for the teleport itself).
func (b *Bot) kafraTeleCost(t, goal string) int {
	sp, ok := townSpawn[t]
	if !ok {
		return 99
	}
	best := mapHops(t, sp[0], sp[1], goal)
	keep := tune().KafraKeep         // the same money rule as kafraPlan: a via-Kafra trip it would refuse on arrival made
	if b.BaseLvOr() < beginnerLv() { // bots walk field -> town -> field forever (ryuknight20 pay_fild01 <-> payon)
		keep = tune().KafraKeepBeginner
	}
	now := time.Now()
	for _, d := range kafraDests[t] {
		if b.Zeny >= d.Price+keep && b.kafraLandingOK(d, goal) && !b.kafraHoldQ(d.Map, goal, now, true) {
			oh, _, _ := b.kafraOnward(d.Map, d.X, d.Y, goal, d.Price, now, t)
			best = min(best, oh+1)
		}
	}
	return best
}

// kafraLandingOK (b.mu held): the Kafra destination doesn't drop us among monsters too strong for our EFFECTIVE level
// (gearpower.go). QA 2026-11-14: "Kafra teleport prontera -> Orc Dungeon" was the last event before 15 deaths: the
// "Orc Dungeon" teleport lands on gef_fild10, the Orc Village (lv40-56, aggressive, assisting), and lv20-30 bots with
// starter gear took it as a shortcut to fields beyond and walked through the Orcs.
func (b *Bot) kafraLandingOK(d kafraDest, goal string) bool {
	if isTown(d.Map) {
		return true
	}
	if d.Map == goal { // the hunting map itself: the map choice already rated it
		return true
	}
	// a bot party's member never drops onto a field (the "Orc Dungeon" Kafra lands on gef_fild10, the Orc Village)
	// ahead of its party: only when that field is the party's map or the leader is there (QA 2026-11-14 18:40Z:
	// Storm_99 took it alone and died on gef_fild10)
	if p := partyOf(b); p != nil && p.Leader != b {
		parties.Lock()
		pm, lead := p.Map, p.Leader
		parties.Unlock()
		if pm != d.Map {
			if s, ok := peek(lead.P.Name); !ok || s.Map != d.Map {
				return false
			}
		}
	}
	return !mapTooStrong(d.Map, b.targetMaxLv()) && recentDeaths(d.Map) < 4 && !swarmVeto(d.Map, b.powerLv())
}

// kafraVia: out in the field with a long way to go - head to a Kafra town first (walk to the nearest one, or a
// Butterfly Wing back to the save town) when that beats walking the whole way. Returns the town ("" = just walk)
// and whether to use a Butterfly Wing.
func (b *Bot) kafraVia(goal string, now time.Time) (string, bool) {
	if isTown(b.MapName) || now.Before(b.kafraNext) || goal == "" || goal == b.MapName || b.kafraHold("", goal, now) {
		return "", false
	}
	here := mapHops(b.MapName, b.X, b.Y, goal) + swarmPenalty(b.MapName, b.X, b.Y, goal, b.powerLv()) // swarm.go
	// walking time as well as map changes (walkCells): 2-3 desert maps are few changes but many minutes
	walkHere := walkCells(b.MapName, b.X, b.Y, goal) + swarmPenalty(b.MapName, b.X, b.Y, goal, b.powerLv())*swarmCells
	if here < 3 && walkHere < 2*kafraMinSaveCells() { // was 5: friends stranded in prt_fild08 walked 4+ maps to Payon with 3,000z in the bag
		return "", false
	}
	bestT, bestC, wing := "", here-1, false // must save at least 2 map changes ...
	bestW := walkHere - kafraMinSaveCells() // ... or kafra_min_save_sec of walking
	unsafe := !b.kafraWalkSafe(goal, now)   // never walk through maps far above our level: a safe Kafra way wins
	if t := nearestTown(b.MapName); len(kafraDests[t]) > 0 {
		c := mapHops(b.MapName, b.X, b.Y, t) + b.kafraTeleCost(t, goal)
		w := walkCells(b.MapName, b.X, b.Y, t) + b.kafraTeleCells(t, goal)
		safeVia := false
		if unsafe && b.routeSafeFrom(b.MapName, b.X, b.Y, t) {
			if sp, ok := townSpawn[t]; ok {
				_, _, safeVia = b.kafraOnward(t, sp[0], sp[1], goal, 0, now)
			}
		}
		if (c < bestC || w < bestW || safeVia && c <= here+2) && b.kafraWouldTake(t, goal, now) {
			bestT, bestC, bestW = t, min(c, bestC), min(w, bestW)
		}
	}
	// only to a KNOWN save point (together.go knownSave): a wing on a guessed one (the home town after a restart)
	// landed guild05's leader in the wrong town (QA 11:46: "via prontera", landed near Payon)
	if st, known := b.knownSave(); known && st != "" && len(kafraDests[st]) > 0 && b.countItem(itemButterflyWing) >= 2 { // keep the last for escapes
		c := 1 + b.kafraTeleCost(st, goal)
		w := kafraOverheadCells + b.kafraTeleCells(st, goal)
		// the wing must clearly beat walking to the nearest Kafra town (kafra_min_save_sec), and never fly back to
		// the town the last Kafra teleport left (TheShadowsenpai 16:44Z: prontera -Kafra-> Geffen, then on gef_fild00
		// "heading to the Kafra in prontera" by wing for 2 % fewer estimated cells - prontera -> Geffen -> Prontera)
		back := st == b.kafraLast.From && now.Sub(b.kafraLast.At) < time.Duration(tune().KafraCooldownMin)*time.Minute && !b.kafraNeed(now)
		margin := 0
		if bestT != "" {
			margin = kafraMinSaveCells()
		}
		if !back && (c < bestC || w+margin < bestW) && b.kafraWouldTake(st, goal, now) {
			bestT, bestC, bestW, wing = st, min(c, bestC), min(w, bestW), true
		}
	}
	return bestT, wing
}

// saveDetour (b.mu held): a solo bot hunting on a field whose nearest Kafra town is not its save point walks there
// once, saves, and comes back (owner rule: "Save the respawn point at the Kafra nearest the grinding ground").
// kafraSavePlan only saved when the route happened to pass through that town: LilSkyz (lv26) died on moc_fild02 and
// respawned in Alberta. At most one try per hunting map per session, only when the town is 1-2 map changes away and
// nothing is hitting us. It reuses travel()'s via-town walk: on arrival kafraSavePlan saves, then travel walks back.
func (b *Bot) saveDetour(gm *GameMap, moving bool, now time.Time) bool {
	if b.viaTown != "" && b.viaFor == b.Goal.Map && b.Goal.Map == b.MapName {
		if b.underAttack(now) || b.hpFrac() < 0.5 {
			return false // fight back first / rest up (hunt, lowHPGuard), then walk on
		}
		b.target = 0
		b.travel(gm, moving) // walking out to the Kafra town
		return true
	}
	// solo hunters; and members of a strong bot party (together.go partyStrong: they come back on their own) when the
	// Kafra town is next door (checked below: 1 map). Weaker parties save on their next town run / via the save gate.
	p := partyOf(b)
	strongMember := p != nil && p.Human == "" && p.Leader != b && b.Goal.Mode == "party" && partyStrong(p)
	if !(b.Goal.Mode == "hunt" && p == nil || strongMember) || b.Goal.Map != b.MapName || isTown(b.MapName) || b.trip != nil ||
		b.kafra != nil || b.target != 0 || b.underAttack(now) || b.charRole() == "vendor" {
		return false
	}
	// QA 2026-11-14: "saved respawn point at the Kafra in payon" was the last event before 21 deaths. The detour starts
	// only with HP to spare (>= 70%) and potions (an acolyte with Heal needs none); the route must not cross a map
	// too strong for our effective level (checked below).
	if b.hpFrac() < 0.7 || (b.potionCount() < 3 && b.healUse() == nil) {
		return false
	}
	nt := nearestTown(b.MapName)
	// owner rule (enforced 2026-11-14): the save point is THE nearest Kafra town - a guessed or "close enough" save
	// doesn't count (together.go knownSave). QA 14:00: 130 of 865 hunting bots kept a wrong / unknown save because
	// only travel into a map checked it.
	saved, known := b.knownSave()
	if nt == "" || known && nt == saved || len(kafraSpots[nt]) == 0 {
		return false
	}
	// staggered: not every mismatched bot walks to town in the same minute after a deploy (0-10 min spread)
	if at, ok := b.buffAt["savefixAt"]; !ok {
		b.buffAt["savefixAt"] = now.Add(time.Duration(b.rng.Intn(600)) * time.Second)
		return false
	} else if now.Before(at) {
		return false
	}
	key := "savetry:" + b.MapName
	if t, tried := b.buffAt[key]; tried && now.Sub(t) < 30*time.Minute {
		return false
	}
	b.buffAt[key] = now
	if h := mapHops(b.MapName, b.X, b.Y, nt); h < 1 || h > 2 || strongMember && h > 1 {
		return false // too far: levelling beats marching (it saves when a route passes through the town)
	}
	tl := b.targetMaxLv()
	for _, w := range RouteMaps(b.MapName, b.X, b.Y, nt, b.powerLv()) {
		if w.To != nt && !isTown(w.To) && (mapTooStrong(w.To, tl) || recentDeaths(w.To) >= 3) {
			return false // the way to the Kafra crosses a map we can't handle: stay and level
		}
	}
	b.viaTown, b.viaFor, b.route, b.path = nt, b.Goal.Map, nil, nil
	b.kafraNext = time.Time{}
	b.event("info", "", fmt.Sprintf("saving at the Kafra in %s, the town nearest %s (saved in %q)", nt, b.MapName, saved))
	b.travel(gm, moving)
	return true
}

// ---------------------------------------------------------------- no back-and-forth Kafra trips
// Owner rule: "Don't waste money on travel: no back-and-forth Kafra trips; decide once where to go and stay."
// QA 2026-11-14 (guild07 mind): parties and solo bots took the Kafra out and straight back when their goal flipped -
// LilGhostgirl prontera -> Geffen (11:35) and geffen -> Prontera (11:37), again prontera -> Payon -> Prontera after the
// 12:06 restart; Raven99 prontera -> Geffen 12:02, geffen -> Prontera 12:08 (new goal after the restart); 13 bots took
// 3+ teleports in one hour, 88 in three. Now one Kafra teleport is followed by kafra_cooldown_min (25) minutes without
// another one, and never straight back to the town it left within twice that, unless there is a real need
// (kafraNeed). Revised after the 07:44 deploy (QA: second hops of one trip became 10-20 min walks through lv50 fields):
// only A -> B -> A within kafra_cooldown_min (50) and more than kafra_max_per_hour (3) are refused, and never when the
// walk is unsafe. Blocked bots walk (no fare) - and the goal flips themselves are fixed where they start.

// kafraHop: one Kafra teleport taken.
type kafraHop struct {
	At       time.Time
	From, To string // the Kafra town left, the map landed on
	ForTrip  bool   // taken on a town run (the way home from it is a real need)
}

// noteKafraHop (b.mu held, 0x0091 before MapName changes): the map change that ends a Kafra teleport trip.
func (b *Bot) noteKafraHop(newMap string, now time.Time) {
	k := b.kafra
	if k == nil || k.Save || k.Cart || newMap == "" || newMap == k.Town || b.MapName != k.Town {
		return
	}
	b.kafraLast = kafraHop{At: now, From: k.Town, To: newMap, ForTrip: b.trip != nil}
	hops := b.kafraHops[:0]
	for _, t := range b.kafraHops {
		if now.Sub(t) < time.Hour {
			hops = append(hops, t)
		}
	}
	b.kafraHops = append(hops, now)
	b.event("info", "", fmt.Sprintf("Kafra teleport %s -> %s", k.Town, k.Dest.Name))
}

// kafraNeed (b.mu held): a real reason for a Kafra teleport the limits would otherwise refuse.
func (b *Bot) kafraNeed(now time.Time) bool {
	l := b.kafraLast
	switch {
	case b.trip != nil: // a real town run (bag full, out of potions/arrows, a party run)
		return true
	case l.ForTrip: // the way back out from that town run (home, or a newly chosen hunting map)
		return true
	case b.Goal.Mode == "vend":
		return true
	}
	if p := partyOf(b); p != nil && p.Human != "" {
		return true // a real player leads: we follow wherever they go
	}
	return false
}

// kafraCapExempt (b.mu held): travel the hourly cap must not stop - only the A -> B -> A block applies to it.
// QA 2026-11-14: guild02's priest Holy Aco ("3 teleports in the last hour already, walking" toward mjo_dun02) walked
// alone across aggressive maps while its party moved on by Kafra.
func (b *Bot) kafraCapExempt(now time.Time) bool {
	l := b.kafraLast
	if b.diedAt.After(l.At) { // respawned since: back out to the hunting ground
		return true
	}
	if b.escWingAt.After(l.At) && b.BaseLvOr() >= beginnerLv() { // a wing escape since (beginners walk: home fields are next to town)
		return true
	}
	p := partyOf(b)
	if p == nil || p.Human != "" {
		return false
	}
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	// following the party: a member going to the party map (a party move, a regroup at the leader's map) or a
	// leader taking its party to the map it picked (guild leader party relocations included)
	return pm != "" && b.Goal.Map == pm && (b.Goal.Mode == "party" || b.Goal.Set == "party-leader")
}

// kafraHold (b.mu held): true = no Kafra teleport now (to dest when given; any teleport when dest is ""). Only
// repeat / flip-flop teleports are refused (QA 2026-11-14 07:44 deploy: a plain 25-min cooldown turned the second hop
// of one trip into 10-20 min cross-world walks through lv50 fields): straight back to the town the last teleport
// left within kafra_cooldown_min (50), or more than kafra_max_per_hour (3) in an hour. Never when there is a real
// need (kafraNeed) or when the walk to goal crosses a map far too strong for us.
func (b *Bot) kafraHold(dest, goal string, now time.Time) bool {
	return b.kafraHoldQ(dest, goal, now, false)
}

// kafraHoldQ: kafraHold; quiet = no "no Kafra teleport" event (cost estimates). The unsafe-walk override lifts only the
// hourly cap, never the A -> B -> A block (QA 2026-11-14 17:13-17:27Z: Ryustorm lapped payon <-> Morocc every 30-60 s
// for pay_dun03 - the walk through pay_dun01/02 read "unsafe", which used to lift the back-teleport block too).
func (b *Bot) kafraHoldQ(dest, goal string, now time.Time, quiet bool) bool {
	l := b.kafraLast
	cd := time.Duration(tune().KafraCooldownMin) * time.Minute
	if l.At.IsZero() || cd <= 0 {
		return false
	}
	// hard limits no exemption lifts (QA 2026-11-14 19:00Z: a trip to Yuno - a "real need" - lapped prontera <->
	// Morocc 12 times in 2.5 min until broke): at most 3 teleports in 10 minutes, and never X -> Y -> X within 10
	// minutes unless it is a party move (the member follows its party map, which a route reaches)
	n10 := 0
	for _, t := range b.kafraHops {
		if now.Sub(t) < 10*time.Minute {
			n10++
		}
	}
	if n10 >= 3 || dest != "" && now.Sub(l.At) < 10*time.Minute && (dest == l.From || nearestTown(dest) == l.From) && !b.kafraPartyMove() &&
		!(l.ForTrip && b.trip == nil) { // (the one way home after a town run stays free)
		// when it clears (travel waits in town for it rather than walking a deadly way, see kafraWaitInTown)
		until := l.At.Add(10 * time.Minute)
		if n10 >= 3 {
			oldest := now
			k := 0
			for _, t := range b.kafraHops {
				if now.Sub(t) < 10*time.Minute {
					k++
					if t.Before(oldest) {
						oldest = t
					}
				}
			}
			until = oldest.Add(10 * time.Minute)
		}
		if until.After(b.kafraHardUntil) {
			b.kafraHardUntil = until
		}
		if goal != "" && !quiet && b.buffAt != nil {
			if at, ok := b.buffAt["kafrahard"]; !ok || now.Sub(at) > 5*time.Minute {
				b.buffAt["kafrahard"] = now
				b.event("info", "", fmt.Sprintf("no Kafra teleport toward %s: %d teleports in 10 min / straight back to %s", goal, n10, l.From))
			}
		}
		return true
	}
	if b.kafraNeed(now) {
		return false
	}
	if goal != "" && mapHops(b.MapName, b.X, b.Y, goal) >= 99 {
		return false // no walking way at all (Renewal Morocc): the Kafra is the only way, never hold it
	}
	n := 0
	for _, t := range b.kafraHops {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	why := ""
	if dest != "" && now.Sub(l.At) < cd && (dest == l.From || nearestTown(dest) == l.From) {
		why = fmt.Sprintf("not straight back to %s (left it %d min ago)", l.From, int(now.Sub(l.At).Minutes()))
	} else if mx := tune().KafraMaxPerHour; mx > 0 && n >= mx && !b.kafraCapExempt(now) {
		if goal != "" && !b.kafraWalkSafe(goal, now) {
			return false // walking there is deadly: the fare is cheaper than the deaths
		}
		why = fmt.Sprintf("%d teleports in the last hour already", n)
	} else {
		return false
	}
	if goal != "" && !quiet && b.buffAt != nil {
		if at, ok := b.buffAt["kafrahold"]; !ok || now.Sub(at) > 10*time.Minute {
			b.buffAt["kafrahold"] = now
			b.event("info", "", fmt.Sprintf("no Kafra teleport toward %s: %s, walking", goal, why))
		}
	}
	return true
}

// kafraWalkSafe (b.mu held): the walking route to goal crosses no map too strong for our effective level and no
// map with recent deaths (cached 60 s per goal).
func (b *Bot) kafraWalkSafe(goal string, now time.Time) bool {
	return b.routeSafeFrom(b.MapName, b.X, b.Y, goal) // position-keyed (kafraWouldTake moves the bot virtually)
}

// kafraLastFromDiary (b.mu held, at login): the last Kafra teleport from this character's diary, so an hourly
// restart doesn't let it teleport straight back (Raven99, LilGhostgirl 12:06-12:09).
func (b *Bot) kafraLastFromDiary(now time.Time) {
	cd := time.Duration(tune().KafraCooldownMin) * time.Minute
	if !b.kafraLast.At.IsZero() || cd <= 0 {
		return
	}
	pre := "[" + b.charName() + "] Kafra teleport "
	m := memoryOf(b.P)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Diary) - 1; i >= 0; i-- {
		e := m.Diary[i]
		if now.Sub(e.T) > max(cd, time.Hour) {
			return
		}
		if !strings.HasPrefix(e.Text, pre) {
			continue
		}
		from, name, ok := strings.Cut(strings.TrimPrefix(e.Text, pre), " -> ")
		if !ok {
			continue
		}
		to := ""
		for _, d := range kafraDests[from] {
			if d.Name == name {
				to = d.Map
			}
		}
		if to == "" {
			continue
		}
		if b.kafraLast.At.IsZero() {
			b.kafraLast = kafraHop{At: e.T, From: from, To: to}
		}
		b.kafraHops = append([]time.Time{e.T}, b.kafraHops...)
	}
}

// ---------------------------------------------------------------- walking time estimate
// QA 2026-11-14 13:41Z: kafraPlan / kafraVia compared routes by map changes only, so moc_fild03 -> moc_fild02 ->
// moc_fild01 -> prt_fild08 (~4.5 min a map) or geffen -> mjolnir_02 (~2,140 cells, ~10 min) looked cheap and leaders
// walked instead of paying a 120z fare. Now both also compare estimated walking cells.

const (
	walkDetour         = 1.3  // on top of the A* path: monsters hitting us on the way, stops, re-paths
	kafraOverheadCells = 80   // the Kafra dialog + the map load, in walking cells (~15 s)
	swarmCells         = 300  // a swarm map on the route (swarmPenalty, in map changes) costs this many cells
	walkCellSec        = 0.22 // measured: geffen -> mjolnir_02, 2,775 estimated cells, ~10 min for guild03's leader
)

// kafraMinSaveCells: the walking a Kafra teleport must save (tune().KafraMinSaveSec, default 120 s), in cells.
func kafraMinSaveCells() int {
	sec := tune().KafraMinSaveSec
	if sec <= 0 {
		sec = 120
	}
	return int(float64(sec) / walkCellSec) // walkCells already includes walkDetour
}

var walkCache = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

// walkCells: estimated cells walked from (ax, ay) on map a until arriving on map goal (99999 = no route): the A* path
// to each warp of the route, from where the previous warp landed, times walkDetour. Cached per 10x10 cell block.
func walkCells(a string, ax, ay int, goal string) int {
	if a == goal {
		return 0
	}
	k := fmt.Sprintf("%s|%d|%d|%s", a, ax/10, ay/10, goal)
	walkCache.Lock()
	v, ok := walkCache.m[k]
	walkCache.Unlock()
	if ok {
		return v
	}
	r := RouteMaps(a, ax, ay, goal)
	n := 99999
	if r != nil {
		c, x, y, from := 0, ax, ay, a
		for _, w := range r {
			c += legCells(from, x, y, w.X, w.Y)
			x, y, from = w.ToX, w.ToY, w.To
		}
		n = int(float64(c) * walkDetour)
	}
	walkCache.Lock()
	if len(walkCache.m) > 50000 {
		walkCache.m = map[string]int{}
	}
	walkCache.m[k] = n
	walkCache.Unlock()
	return n
}

// legCells: the A* path length on map m from (x, y) to the warp at (wx, wy) (a walkable cell next to it when the
// warp cell itself is blocked); 1.6x the straight line when no path is found within the node budget.
func legCells(m string, x, y, wx, wy int) int {
	gm := maps[m]
	if gm == nil {
		return dist(x, y, wx, wy) * 16 / 10
	}
	for r := 0; r <= 2; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r || !gm.Walkable(wx+dx, wy+dy) {
					continue
				}
				if p := gm.FindPath(x, y, wx+dx, wy+dy, 60000); p != nil {
					return len(p)
				}
				r = 3 // one try per warp: a failed search is expensive
				dy, dx = r, r
			}
		}
	}
	return dist(x, y, wx, wy) * 16 / 10
}

// kafraTeleCells (b.mu held): walking cells from town t's spawn to goal, using its Kafra when that is shorter (the
// same money / landing / loop-limit rules as kafraTeleCost).
func (b *Bot) kafraTeleCells(t, goal string) int {
	sp, ok := townSpawn[t]
	if !ok {
		return 99999
	}
	best := walkCells(t, sp[0], sp[1], goal)
	keep := tune().KafraKeep
	if b.BaseLvOr() < beginnerLv() {
		keep = tune().KafraKeepBeginner
	}
	toK := 99999
	for _, s := range kafraSpots[t] {
		toK = min(toK, dist(sp[0], sp[1], s.X, s.Y))
	}
	now := time.Now()
	for _, d := range kafraDests[t] {
		if b.Zeny >= d.Price+keep && b.kafraLandingOK(d, goal) && !b.kafraHoldQ(d.Map, goal, now, true) {
			_, oc, _ := b.kafraOnward(d.Map, d.X, d.Y, goal, d.Price, now, t)
			best = min(best, toK+kafraOverheadCells+oc)
		}
	}
	return best
}

// kafraWouldTake (b.mu held): kafraPlan, run as if we already stood where the walk into town t lands, accepts a
// teleport toward goal. kafraVia used its own estimate (from the town's spawn point) and kafraPlan then refused on
// arrival - "no Kafra trip from geffen after all: walking to mjo_dun02" (QA 2026-11-14 14:34, guild02): a detour back
// into town for nothing, and the way out again looked like a warp loop. Now the via-town walk starts only when the
// Kafra will really take us.
func (b *Bot) kafraWouldTake(t, goal string, now time.Time) bool {
	x, y := 0, 0
	if r := RouteMaps(b.MapName, b.X, b.Y, t, b.powerLv()); len(r) > 0 {
		x, y = r[len(r)-1].ToX, r[len(r)-1].ToY
	} else if sp, ok := townSpawn[t]; ok {
		x, y = sp[0], sp[1]
	} else {
		return false
	}
	om, ox, oy := b.MapName, b.X, b.Y
	b.MapName, b.X, b.Y, b.kafraVirtual = t, x, y, true
	k := b.kafraPlan(goal, now)
	b.MapName, b.X, b.Y, b.kafraVirtual = om, ox, oy, false
	return k != nil
}

// kafraOnward (b.mu held): from (x, y) on map m to goal - map changes, walking cells, and whether the way is safe -
// walking, or one more Kafra teleport from m when m is a Kafra town (spent = fares already planned). Not recursive.
func (b *Bot) kafraOnward(m string, x, y int, goal string, spent int, now time.Time, from ...string) (int, int, bool) {
	// swarm.go penalties belong to the path actually walked (QA 2026-11-14 19:00Z: prontera -> yuno picked Morocc, a
	// town with NO walking route anywhere, because its penalty came out 0 while Geffen's way via mjolnir carried one;
	// Dark rogue and Holyboy lapped prontera <-> Morocc 12 times in 2.5 min)
	h, c := mapHops(m, x, y, goal), walkCells(m, x, y, goal)
	if h < 99 {
		sw := swarmPenalty(m, x, y, goal, b.powerLv())
		h, c = h+sw, c+sw*swarmCells
	}
	safe := h < 99 && b.routeSafeFrom(m, x, y, goal)
	spots := kafraSpots[m]
	if len(spots) == 0 || m == goal {
		return h, c, safe
	}
	toK := 99999
	for _, s := range spots {
		toK = min(toK, dist(x, y, s.X, s.Y))
	}
	keep := tune().KafraKeep
	if b.BaseLvOr() < beginnerLv() {
		keep = tune().KafraKeepBeginner
	}
	for _, d := range kafraDests[m] {
		if b.Zeny < spent+d.Price+keep || !b.kafraLandingOK(d, goal) {
			continue
		}
		// never a chain back into the town the plan starts from (A -> B -> A): it only "saves" the walk inside
		// town A (Ryustorm 17:27Z: payon -> Morocc -> Payon lands at payon 161,58, nearer pay_arche than its Kafra)
		if len(from) > 0 && (d.Map == from[0] || nearestTown(d.Map) == from[0]) {
			continue
		}
		// nor a second hop the loop guard will refuse on arrival (a chain around the A -> B -> A block: payon ->
		// Morocc -> Alberta right after alberta -> Payon), which strands the bot in the middle town
		if b.kafraHoldQ(d.Map, goal, now, true) {
			continue
		}
		dh := mapHops(d.Map, d.X, d.Y, goal)
		if dh >= 99 {
			continue
		}
		dsw := swarmPenalty(d.Map, d.X, d.Y, goal, b.powerLv())
		dh += dsw
		dsafe := b.routeSafeFrom(d.Map, d.X, d.Y, goal)
		if dh+1 < h || dsafe && !safe && dh+1 <= h+2 {
			h, safe = min(h, dh+1), safe || dsafe
		}
		c = min(c, toK+kafraOverheadCells+walkCells(d.Map, d.X, d.Y, goal)+dsw*swarmCells)
	}
	return h, c, safe
}

var routeSafeCache = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

// routeSafeFrom (b.mu held): the walking route from (x, y) on m to goal crosses no map too strong for our effective
// level (and none with 3+ recent deaths; that part is cached with the rest for a minute at most via the key's time).
func (b *Bot) routeSafeFrom(m string, x, y int, goal string) bool {
	tl := b.targetMaxLv()
	k := fmt.Sprintf("%s|%d|%d|%s|%d|%d", m, x/10, y/10, goal, tl, time.Now().Unix()/60)
	routeSafeCache.Lock()
	v, hit := routeSafeCache.m[k]
	routeSafeCache.Unlock()
	if hit {
		return v
	}
	r := RouteMaps(m, x, y, goal, b.powerLv())
	ok := r != nil || m == goal
	for _, w := range r {
		if w.To != goal && !isTown(w.To) && (mapTooStrong(w.To, tl) || recentDeaths(w.To) >= 3) {
			ok = false
			break
		}
	}
	routeSafeCache.Lock()
	if len(routeSafeCache.m) > 50000 {
		routeSafeCache.m = map[string]bool{}
	}
	routeSafeCache.m[k] = ok
	routeSafeCache.Unlock()
	return ok
}

// kafraPartyMove (b.mu held): b is travelling to its bot party's map (member following a party move / regroup, or
// the leader taking the party there).
func (b *Bot) kafraPartyMove() bool {
	p := partyOf(b)
	if p == nil || p.Human != "" {
		return false
	}
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	return pm != "" && b.Goal.Map == pm && (b.Goal.Mode == "party" || b.Goal.Set == "party-leader")
}

// kafraWaitInTown (b.mu held, travel): the Kafra is refused by the hard limits, and the walking way to goal crosses a
// map too strong for us: wait in town (sitting) until the limit clears instead of walking it. QA 2026-11-14 19:20Z:
// guild05's TheShadowsenpai (Alchemist 69, gear-rated ~51) walked aldebaran -> mjolnir_12 (lv63-65) four times in
// 12 min and died there. Waits at most 10 min (the limit's own length).
func (b *Bot) kafraWaitInTown(goal string, now time.Time) bool {
	if goal == "" || goal == b.MapName || !isTown(b.MapName) || len(kafraSpots[b.MapName]) == 0 || !now.Before(b.kafraHardUntil) {
		return false
	}
	if b.routeSafeFrom(b.MapName, b.X, b.Y, goal) {
		return false // the walk is fine: walk
	}
	b.setState(fmt.Sprintf("waiting in %s for the Kafra (%ds; the walk to %s is too dangerous)", b.MapName, int(b.kafraHardUntil.Sub(now).Seconds()), goal))
	if at, ok := b.buffAt["kafrawait"]; b.buffAt != nil && (!ok || now.Sub(at) > 10*time.Minute) {
		b.buffAt["kafrawait"] = now
		b.event("info", "", fmt.Sprintf("waiting in %s for the Kafra toward %s: the walk crosses maps too strong for me", b.MapName, goal))
	}
	b.route, b.path = nil, nil
	if !b.Sitting && now.Sub(b.sitSentAt) > 3*time.Second {
		b.sitSentAt = now
		go b.sit()
	}
	return true
}
