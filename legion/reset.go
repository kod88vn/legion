package main

// reset.go: natural build resets (main thread 2026-11-14, under the owner's "simulate normal human behaviour" rule and
// "the builds of the legions still lack variety"): no mass reset. Now and then a bot decides to respec into another
// build variant the way a player does - the server's own way: the Hypnotist in Izlude (rAthena npc/re/other/
// resetskill.txt, official) resets stats and skills for free for a 1st-class character (Swordman, Mage, Archer,
// Acolyte, Thief, Merchant) whose carried weight is 0 and who has no Pushcart. So the bot announces it ("finally
// resetting to a spear build, wish me luck"), travels to Izlude, takes off its gear, puts everything in the Kafra
// storage next to the Hypnotist (izlude 128,148), has its stats and then its skills reset (127,175), takes its things
// back, wears them again and rebuilds by the variant's plan (stat and skill points are held while it is underway).
// 2nd-class characters have no reset service on this server (official resets above 1st class are cash items), so
// they keep their builds.
//
// Who: 1st class, base level resetMinLv+, its line's main plan with a variant (the least represented one is chosen),
// main role (never support healers or vendors), not a Merchant (Pushcart; guild traders / alchemists), not in a party,
// not a guild leader (always online), never in the core instance (Ayla's party), not busy (trip, errand, market...).
// How often: one start at most every 30-60 min server-wide, resetPerDay a day; each eligible bot rolls every 15 min.
// Safety: anything that fails after the storage step goes straight to taking the items back; a reset in progress is
// saved to resets_pending.json, so a bot that logs out with its things in storage fetches them at its next login.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	resetMinLv    = 30
	resetPerDay   = 24
	resetMaxTime  = 40 * time.Minute
	resetCheckGap = 15 * time.Minute
)

var (
	resetKafra = kafraSpot{128, 148} // Kafra Employee#iz
	resetHypno = [2]int{127, 175}    // Hypnotist#novice
)

type resetItem struct {
	ItemID, Count int
}

type resetTask struct {
	Char     string      `json:"char"`
	From     string      `json:"from"`
	To       string      `json:"to"`
	Stage    string      `json:"stage"` // travel, unequip, store, hypno, fetch, done
	Stored   []resetItem `json:"stored"`
	Switched bool        `json:"switched"` // the plan is the new one (stats and skills were reset)
	Started  time.Time   `json:"started"`
	At       time.Time   `json:"-"`
	Tries    int         `json:"-"`
	Want     string      `json:"-"` // stats / skills: what the Hypnotist dialog asks for now
	Goal     Goal        `json:"goal"`
	storage  map[int]*InvItem
}

var resetGate = struct {
	sync.Mutex
	next  time.Time
	day   string
	n     int
	saved map[string]*resetTask // pending tasks by character (resets_pending.json)
}{}

func resetFile() string { return filepath.Join(*dataDir, "resets_pending.json") }

func loadResetPending() {
	if resetGate.saved != nil {
		return
	}
	resetGate.saved = map[string]*resetTask{}
	if raw, err := os.ReadFile(resetFile()); err == nil {
		json.Unmarshal(raw, &resetGate.saved)
	}
}

// saveResetPending (resetGate held): marshal under the lock, write the file in the background - never file I/O under
// a lock that bots take while holding their own b.mu (run 7: the API hung on b.mu while files were written on the
// slow G: share).
func saveResetPending() {
	raw, _ := json.MarshalIndent(resetGate.saved, "", " ")
	go func() {
		resetFileMu.Lock()
		defer resetFileMu.Unlock()
		os.WriteFile(resetFile(), raw, 0o644)
	}()
}

var resetFileMu sync.Mutex // file writes only (resets_pending.json, personas.json)

// resetSave (b.mu held): remember (or forget) this bot's reset across logouts.
func (b *Bot) resetSave() {
	resetGate.Lock()
	defer resetGate.Unlock()
	loadResetPending()
	if t := b.reset; t != nil && (t.Stage == "store" || t.Stage == "hypno" || t.Stage == "fetch") && len(t.Stored) > 0 {
		c := *t
		resetGate.saved[t.Char] = &c
	} else {
		delete(resetGate.saved, b.charName())
	}
	saveResetPending()
}

// resetResume (b.mu held): a reset left unfinished at logout (items in storage): fetch them now.
func (b *Bot) resetResume(now time.Time) {
	if b.reset != nil || !b.invReady() || now.Before(b.buffAt["reset:resume"]) {
		return
	}
	b.buffAt["reset:resume"] = now.Add(10 * time.Minute) // a cheap look, not every tick
	resetGate.Lock()
	loadResetPending()
	t := resetGate.saved[b.charName()]
	resetGate.Unlock()
	if t == nil {
		return
	}
	c := *t
	c.Stage, c.At, c.Started, c.Tries = "travel-fetch", now, now, 0
	b.reset = &c
	b.Goal = Goal{Mode: "idle", Map: "izlude", Set: "reset"}
	b.event("info", "", fmt.Sprintf("reset: my things are still in the Izlude storage (%d kinds) - fetching them", len(c.Stored)))
}

// resetVariant: the least represented variant of the bot's main plan (by all personas' characters), "" = none.
func resetVariant(main *Build) string {
	var vs []string
	for _, id := range buildOrder() {
		if v := builds[id]; v.Base == main.ID && v.First == main.First {
			vs = append(vs, id)
		}
	}
	if len(vs) == 0 {
		return ""
	}
	count := map[string]int{}
	bots, _ := allBots.Load().([]*Bot)
	for _, o := range bots {
		if o == nil || o.P == nil {
			continue
		}
		for _, c := range o.P.Chars {
			count[c.Plan]++
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return count[vs[i]] < count[vs[j]] })
	return vs[0]
}

// resetCandidate (b.mu held): this bot may start a natural reset now (rare: rolls every 15 min, server-wide gate).
func (b *Bot) resetCandidate(now time.Time) string {
	if *onlyNames != "" || b.reset != nil || now.Before(b.buffAt["reset:check"]) || tune().ResetsOff {
		return ""
	}
	b.buffAt["reset:check"] = now.Add(resetCheckGap)
	if b.Class < 1 || b.Class > 6 || family(b.Class) == famMerchant || b.BaseLvOr() < resetMinLv || b.charRole() != "main" ||
		b.hasCart || b.Zeny < 500 || humanOf(b) != "" || b.alwaysOnline() || b.trip != nil ||
		b.errand != nil || b.kafra != nil || b.mkt != nil || b.vend != nil || b.Dead || b.Goal.Set == "api" {
		return ""
	}
	// run 7: almost every eligible character is in a goal party (13:50: 4 solo of 153), so a member may go too - like
	// a player leaving its party to respec ("brb respec"); never a leader, a human-led party, or a boss hunt
	if pt := partyOf(b); pt != nil && (pt.Human != "" || pt.Leader == b || pt.bossActive() || isRestoring(pt)) {
		return ""
	}
	p := b.plan()
	if p == nil || p.Base != "" {
		return "" // already on a variant
	}
	if b.rng.Intn(20) != 0 {
		return ""
	}
	to := resetVariant(p)
	if to == "" {
		return ""
	}
	resetGate.Lock()
	defer resetGate.Unlock()
	day := now.Format("2006-01-02")
	if resetGate.day != day {
		resetGate.day, resetGate.n = day, 0
	}
	if now.Before(resetGate.next) || resetGate.n >= resetPerDay {
		return ""
	}
	resetGate.n++
	resetGate.next = now.Add(30*time.Minute + time.Duration(b.rng.Intn(30))*time.Minute)
	return to
}

// resetHoldPoints (b.mu held): progress() waits while a reset is underway until the new plan is set - no points
// spent under the old plan, no gear put back on while it is going into storage.
func (b *Bot) resetHoldPoints() bool { return b.reset != nil && !b.reset.Switched }

// resetThink (b.mu held, from think): start / drive a natural reset; true = it owns the bot this tick.
func (b *Bot) resetThink(gm *GameMap, moving bool, now time.Time) bool {
	b.resetResume(now)
	t := b.reset
	if t == nil {
		to := b.resetCandidate(now)
		if to == "" {
			return false
		}
		p := b.plan()
		t = &resetTask{Char: b.charName(), From: p.ID, To: to, Stage: "travel", Started: now, At: now, Goal: b.Goal}
		b.reset = t
		b.Goal = Goal{Mode: "idle", Map: "izlude", Set: "reset"}
		name := strings.ToLower(builds[to].Name)
		go b.say(styled(b.P.Typing, pick("finally resetting to a "+name+" build, wish me luck",
			"off to the hypnotist in izlude, going "+name, "respec time! "+name+" here i come")))
		if pt := partyOf(b); pt != nil { // leaves its goal party first, like a player off to respec
			typing := b.P.Typing
			go func() {
				b.partySay(styled(typing, pick("gonna go respec at the hypnotist, ty for the party", "brb... actually respec time, cya guys", "leaving to reset my build, gl all")))
				time.Sleep(3 * time.Second)
				b.event("party", "", "left "+pt.Name+": build reset")
				pt.drop(b)
			}()
		}
		b.event("info", "", fmt.Sprintf("reset: %s -> %s (Hypnotist, Izlude)", builds[t.From].Name, builds[to].Name))
		return false
	}
	if now.Sub(t.Started) > resetMaxTime && t.Stage != "fetch" && t.Stage != "travel-fetch" && t.Stage != "done" {
		return b.resetAbort("took too long", now)
	}
	if b.MapName != "izlude" {
		if t.Stage == "travel" || t.Stage == "travel-fetch" {
			b.Goal = Goal{Mode: "idle", Map: "izlude", Set: "reset"}
			return false // the goal walks / Kafra-hops us there
		}
		if now.Sub(t.At) > 20*time.Minute {
			b.resetEnd("left Izlude", now)
		}
		return false
	}
	if b.target != 0 || b.underAttack(now) {
		return false // a monster in town: fight first
	}
	switch t.Stage {
	case "travel":
		t.Stage, t.At = "unequip", now
		fallthrough
	case "unequip":
		for _, it := range b.inv {
			if it.Equipped && it.Count > 0 {
				if now.After(b.buffAt["reset:uneq"]) {
					b.buffAt["reset:uneq"] = now.Add(600 * time.Millisecond)
					go b.send(newPkt(0x00ab).u16(uint16(it.Index)).b)
				}
				if now.Sub(t.At) > time.Minute {
					return b.resetAbort("could not take off my gear", now)
				}
				return true
			}
		}
		t.Stage, t.At = "store", now
		b.kafra = &kafraTrip{Store: true, Spot: resetKafra, Town: "izlude", Stage: "walk", Started: now, At: now}
		return b.kafraThink(gm, moving, now)
	case "store":
		if b.kafra != nil {
			return b.kafraThink(gm, moving, now)
		}
		if len(t.Stored) == 0 {
			if now.Sub(t.At) > 2*time.Minute {
				return b.resetAbort("the Kafra storage did not open", now)
			}
			b.kafra = &kafraTrip{Store: true, Spot: resetKafra, Town: "izlude", Stage: "walk", Started: now, At: now}
			return true
		}
		if b.Weight > 0 {
			if now.Sub(t.At) > 20*time.Second {
				return b.resetAbort(fmt.Sprintf("still carrying weight %d", b.Weight), now)
			}
			return true
		}
		t.Stage, t.At, t.Want, t.Tries = "hypno", now, "stats", 0
		return true
	case "hypno":
		return b.resetHypno(moving, now)
	case "travel-fetch":
		t.Stage, t.At = "fetch", now
		b.kafra = nil
		fallthrough
	case "fetch":
		if b.kafra != nil {
			return b.kafraThink(gm, moving, now)
		}
		if t.Tries == 0 || now.Sub(t.At) > 90*time.Second && t.Tries < 3 {
			t.Tries++
			t.At = now
			b.kafra = &kafraTrip{Store: true, Spot: resetKafra, Town: "izlude", Stage: "walk", Started: now, At: now}
			return b.kafraThink(gm, moving, now)
		}
		if t.Tries >= 3 && now.Sub(t.At) > 90*time.Second {
			b.resetEnd("could not take everything back (some items stay in storage)", now)
		}
		return true
	case "done":
		b.resetEnd("", now)
	}
	return false
}

// resetHypno (b.mu held): talk to the Hypnotist for the stat reset, then the skill reset.
func (b *Bot) resetHypno(moving bool, now time.Time) bool {
	t := b.reset
	statsDone := b.stats[0] <= 1 && b.stats[1] <= 1 && b.stats[2] <= 1 && b.stats[3] <= 1 && b.stats[4] <= 1 && b.stats[5] <= 1
	skillsDone := true
	for id, s := range b.skills {
		if s.Lv > 0 && id != nvBasic && (id < 142 || id > 160) { // Basic Skill and the 1st-class quest skills stay
			skillsDone = false
			break
		}
	}
	if statsDone && t.Want == "stats" {
		t.Want, t.Tries = "skills", 0
	}
	if statsDone && skillsDone {
		b.resetSwitch(now)
		t.Stage, t.At, t.Tries = "fetch", now, 0
		return true
	}
	if now.Before(b.buffAt["reset:talk"]) {
		return true
	}
	if t.Tries >= 3 {
		return b.resetAbort("the Hypnotist did not reset my "+t.Want, now)
	}
	if d := dist(b.X, b.Y, resetHypno[0], resetHypno[1]); d > 2 {
		if !moving {
			go b.walkTo(resetHypno[0], resetHypno[1]+1)
		}
		return true
	}
	npc := b.npcAt(resetHypno[0], resetHypno[1])
	if npc == 0 {
		if now.Sub(t.At) > time.Minute {
			return b.resetAbort("no Hypnotist here", now)
		}
		return true
	}
	if b.Sitting {
		go b.stand()
	}
	t.Tries++
	b.buffAt["reset:talk"] = now.Add(15 * time.Second)
	b.setState("talking to the Hypnotist (" + t.Want + " reset)")
	go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	return true
}

// onResetDialog (b.mu held, bot.go NPC dialog packets): the Hypnotist's dialog - next, close, and the menus "Reset
// stats" / "Reset skills", then "Yes". nav.go answerStrayDialog leaves this dialog to us.
func (b *Bot) onResetDialog(op uint16, pk []byte) {
	t := b.reset
	if t == nil || t.Stage != "hypno" || len(pk) < 6 {
		return
	}
	switch op {
	case 0x00b5:
		go b.send(newPkt(0x00b9).u32(le.Uint32(pk[2:])).b)
	case 0x00b6:
		b.dialogNPC = 0
		go b.send(newPkt(0x0146).u32(le.Uint32(pk[2:])).b)
		b.buffAt["reset:talk"] = time.Now().Add(1500 * time.Millisecond) // the stat / skill lists arrive, then talk again
	case 0x00b7:
		if len(pk) < 9 {
			return
		}
		ans := byte(255)
		for i, o := range strings.Split(cstr(pk[8:]), ":") {
			o = strings.ToLower(strings.TrimSpace(stripColor(o)))
			if t.Want == "stats" && o == "reset stats" || t.Want == "skills" && o == "reset skills" || o == "yes" {
				ans = byte(i + 1)
				break
			}
		}
		go b.send(newPkt(0x00b8).u32(le.Uint32(pk[4:])).u8(ans).b)
	}
}

// resetDialogOwned (b.mu held): the Hypnotist dialog is ours (nav.go answerStrayDialog stays out).
func (b *Bot) resetDialogOwned() bool { return b.reset != nil && b.reset.Stage == "hypno" }

// stripColor: drop ^RRGGBB colour codes from an NPC menu option.
func stripColor(s string) string {
	for {
		i := strings.IndexByte(s, '^')
		if i < 0 || i+7 > len(s) {
			return s
		}
		s = s[:i] + s[i+7:]
	}
}

// resetSwitch (b.mu held): stats and skills are reset - the new plan takes over (memory and personas.json).
func (b *Bot) resetSwitch(now time.Time) {
	t := b.reset
	if t.Switched {
		return
	}
	t.Switched = true
	if cs := b.charSpec(); cs != nil {
		cs.Plan = t.To
	}
	go setPersonaPlan(t.Char, t.To)
	b.event("info", "", fmt.Sprintf("reset done: now %s, rebuilding from scratch", builds[t.To].Name))
	b.remember("reset my build to %s at the Hypnotist in Izlude", builds[t.To].Name)
	b.resetSave()
}

// setPersonaPlan: write the new plan of one character into personas.json (read-modify-write, so other pending
// edits to the file stay).
func setPersonaPlan(char, plan string) {
	resetFileMu.Lock() // not resetGate: loading / saving the 6 MB personas.json takes seconds
	defer resetFileMu.Unlock()
	path := filepath.Join(*dataDir, "personas.json")
	ps, err := loadPersonas(path)
	if err != nil {
		return
	}
	for _, p := range ps {
		for i := range p.Chars {
			if p.Chars[i].Name == char {
				p.Chars[i].Plan = plan
				savePersonas(path, ps)
				return
			}
		}
	}
}

// resetAbort (b.mu held): give up; if things are in storage, go get them first.
func (b *Bot) resetAbort(why string, now time.Time) bool {
	t := b.reset
	b.event("info", "", "reset abandoned: "+why)
	b.kafra = nil
	if len(t.Stored) > 0 && t.Stage != "fetch" {
		t.Stage, t.At, t.Tries = "fetch", now, 0
		b.resetSave()
		return true
	}
	b.resetEnd(why, now)
	return false
}

// resetEnd (b.mu held): back to the old goal; the gear comes back on through equipFromInventory.
func (b *Bot) resetEnd(why string, now time.Time) {
	t := b.reset
	if t == nil {
		return
	}
	b.reset = nil
	b.resetSave()
	if b.Goal.Set == "reset" {
		b.Goal = t.Goal
		if b.Goal.Map == "" || b.Goal.Set == "reset" {
			b.Goal = Goal{Mode: "hunt", Map: b.MapName, Set: "auto"}
		}
	}
	if t.Switched && why == "" {
		go b.say(styled(b.P.Typing, pick("ok respec done, back to grinding", "new build lets go", "done! feels weird starting over")))
	}
	if why != "" {
		b.event("info", "", "reset ended: "+why)
	}
}

// resetOnStorage (b.mu held, storage.go onStorageOpen): deposit everything for the Hypnotist, or take it back.
// true = handled.
func (b *Bot) resetOnStorage() bool {
	t := b.reset
	if t == nil || (t.Stage != "store" && t.Stage != "fetch") {
		return false
	}
	// run 7 (live 20:43-20:55Z: Yuki_soul's pending entry grew to 967 "stored" items and the API hung): rAthena sends
	// 0x00f2 (storage count) again after EVERY item moved, so this ran once per item - re-sending every deposit and
	// rewriting resets_pending.json each time while holding the bot's lock. Act once per storage opening (our Kafra
	// trip); later 0x00f2s of the same opening are swallowed (not "a storage we didn't ask for": that would close it).
	if b.kafra == nil || !b.kafra.Store || b.kafra.Stored {
		return true
	}
	b.kafra.Stored = true
	b.kafra = nil
	if t.Stage == "store" {
		n := 0
		for idx, it := range b.inv {
			if it == nil || it.Count <= 0 || it.Equipped {
				continue
			}
			t.Stored = append(t.Stored, resetItem{ItemID: it.ItemID, Count: it.Count})
			go b.send(newPkt(0x0364).u16(uint16(idx)).u32(uint32(it.Count)).b)
			n++
		}
		t.At = time.Now()
		b.resetSave()
		b.event("info", "", fmt.Sprintf("reset: %d kinds of items into the Izlude storage", n))
	} else {
		want := map[int]int{}
		for _, s := range t.Stored {
			want[s.ItemID] += s.Count
		}
		n := 0
		for idx, it := range t.storage {
			if it == nil || it.Count <= 0 || want[it.ItemID] <= 0 {
				continue
			}
			c := min(it.Count, want[it.ItemID])
			want[it.ItemID] -= c
			go b.send(newPkt(0x0365).u16(uint16(idx)).u32(uint32(c)).b)
			n++
		}
		left := 0
		for _, c := range want {
			left += c
		}
		b.event("info", "", fmt.Sprintf("reset: took %d kinds of items back (%d left)", n, left))
		if left == 0 || n == 0 && t.Tries >= 3 {
			t.Stage = "done"
		}
		b.resetSave()
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		b.send(newPkt(0x0193).b)
	}()
	return true
}

// resetStorageItem (b.mu held, bot.go storage item lists): remember the storage's items while fetching.
func (b *Bot) resetStorageItem(idx int, it *InvItem) {
	t := b.reset
	if t == nil {
		return
	}
	if t.storage == nil {
		t.storage = map[int]*InvItem{}
	}
	t.storage[idx] = it
}
