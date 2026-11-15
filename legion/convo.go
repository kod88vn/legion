package main

// convo.go: ambient life talk (CT02 "the server feels alive", PR38: done in code, not by an AI loop). chatter.go
// already says single lines (local small talk with a one-word answer, party call-outs, guild greetings); real
// players also hold short conversations. This adds 2-4 line exchanges between two players standing near each other:
//
//   - in a town: public chat, both within convoNearCells of each other and not fighting;
//   - in a party: party chat, both members on the same map.
//
// The lines are grounded in what the two are doing (their class, level, the map they hunt on, the gear they save
// for, loot in the bag). Rate-limited: about online/ambient_talk_div conversations a minute, never more than
// ambient_talk_max_per_min started in any minute, a player at most one per convoBotCooldown and a place (town map
// or party) at most one per convoPlaceCooldown. Lines never hint at bots, AI or scripts (G18). Behind tuning
// ambient_talk_on (default on: system layer, PR37).

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	convoNearCells     = 8
	convoBotCooldown   = 15 * time.Minute
	convoPlaceCooldown = 3 * time.Minute
)

// convoSide: what one speaker is, read once (under its own lock) before the conversation starts.
type convoSide struct {
	b      *Bot
	name   string
	class  string
	lv     int
	mapN   string
	hunt   string // the map it hunts on (goal map), "" if none
	want   string // next gear it saves for
	loot   string // an etc item it carries 5+ of
	guild  string
	typing string
	x, y   int
	busy   bool // fighting / attacking: no chatting now
}

type convoLine struct {
	who  int // 0 = a, 1 = b
	text string
}

var convoState = struct {
	sync.Mutex
	lastBot   map[*Bot]time.Time
	lastPlace map[string]time.Time
	started   []time.Time
}{lastBot: map[*Bot]time.Time{}, lastPlace: map[string]time.Time{}}

func convoSideOf(b *Bot) (convoSide, bool) {
	s, ok := b.snap() // locks b once
	if !ok {
		return convoSide{}, false
	}
	c := convoSide{b: b, name: s.name, class: s.class, lv: s.lv, mapN: s.mapN, want: s.want, guild: s.guild, typing: s.typing}
	if len(s.loot) > 0 {
		c.loot = itemName(s.loot[crand(len(s.loot))])
	}
	b.mu.Lock()
	c.x, c.y = b.X, b.Y
	if b.Goal.Map != "" && !isTown(b.Goal.Map) {
		c.hunt = b.Goal.Map
	}
	st := strings.ToLower(b.State)
	c.busy = strings.Contains(st, "fight") || strings.Contains(st, "attack") || strings.Contains(st, "kill") ||
		strings.Contains(st, "flee") || strings.Contains(st, "dead")
	b.mu.Unlock()
	return c, true
}

// convoAllowed: the rate limits (pure apart from the shared state; now passed in for tests).
func convoAllowed(place string, a, b *Bot, now time.Time, maxPerMin float64) bool {
	convoState.Lock()
	defer convoState.Unlock()
	keep := convoState.started[:0]
	for _, t := range convoState.started {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	convoState.started = keep
	if float64(len(keep)) >= maxPerMin {
		return false
	}
	if now.Sub(convoState.lastPlace[place]) < convoPlaceCooldown ||
		now.Sub(convoState.lastBot[a]) < convoBotCooldown || now.Sub(convoState.lastBot[b]) < convoBotCooldown {
		return false
	}
	convoState.started = append(convoState.started, now)
	convoState.lastPlace[place] = now
	convoState.lastBot[a], convoState.lastBot[b] = now, now
	if len(convoState.lastBot) > 5000 { // forget old entries now and then
		for k, t := range convoState.lastBot {
			if now.Sub(t) > convoBotCooldown {
				delete(convoState.lastBot, k)
			}
		}
	}
	return true
}

// convoPair: two sides near each other (both free, within cells); -1,-1 if none. Starts at a random index.
func convoPair(sides []convoSide, cells int) (int, int) {
	n := len(sides)
	if n < 2 {
		return -1, -1
	}
	off := crand(n)
	for k := 0; k < n; k++ {
		i := (k + off) % n
		if sides[i].busy {
			continue
		}
		for j := 0; j < n; j++ {
			if j == i || sides[j].busy || sides[j].mapN != sides[i].mapN {
				continue
			}
			if max(abs(sides[i].x-sides[j].x), abs(sides[i].y-sides[j].y)) <= cells {
				return i, j
			}
		}
	}
	return -1, -1
}

func (c convoSide) spot() string {
	if c.hunt != "" {
		return mapTalk(c.hunt)
	}
	return "the fields"
}

func classTalk(class string) string {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "priest") || c == "acolyte":
		return pick("everyone wants me in their party lol", "heal spam all day", "sp is always empty")
	case strings.Contains(c, "knight") || c == "swordman" || strings.Contains(c, "crusader") || strings.Contains(c, "paladin"):
		return pick("tanky, pots are pricey tho", "solid, i just walk in and hit", "pecos are the best")
	case strings.Contains(c, "wizard") || c == "mage" || strings.Contains(c, "sage") || c == "professor":
		return pick("aoe is so fun", "squishy but kills fast", "sp pots eat my zeny")
	case strings.Contains(c, "hunter") || c == "archer" || c == "sniper" || strings.Contains(c, "bard") || strings.Contains(c, "dancer"):
		return pick("arrows cost a fortune", "kiting is relaxing tbh", "good, dex is everything")
	case strings.Contains(c, "assassin") || c == "thief" || strings.Contains(c, "rogue") || c == "stalker":
		return pick("crits go brrr", "dodge everything lol", "fast leveling so far")
	case strings.Contains(c, "smith") || c == "merchant" || strings.Contains(c, "alchemist") || c == "creator":
		return pick("zeny is easy at least", "slow lvling but rich lol", "cart is heavy as always")
	case strings.Contains(c, "monk") || c == "champion":
		return pick("combos are fun", "spirit spheres everywhere", "good, hits hard")
	}
	return pick("its ok", "fun so far", "slow but fine")
}

// convoTownLines: a short public exchange between a and b in a town.
func convoTownLines(a, b convoSide) []convoLine {
	switch crand(5) {
	case 0: // levels and spots
		l := []convoLine{{0, pick("what lv r u now?", "how far r u now?", "what level are you at?")},
			{1, pick(fmt.Sprintf("%d, been grinding %s", b.lv, b.spot()), fmt.Sprintf("%d now, mostly %s", b.lv, b.spot()))}}
		if a.lv > b.lv+3 {
			l = append(l, convoLine{0, pick("nice, "+b.spot()+" was good for me around there", "keep going, it gets better")})
		} else {
			l = append(l, convoLine{0, pick(fmt.Sprintf("%d here, still at %s", a.lv, a.spot()), "nice, same-ish lol")})
		}
		return l
	case 1: // gear
		if a.want != "" {
			return []convoLine{{0, pick("still saving for a "+a.want+"...", "need a "+a.want+" so bad")},
				{1, pick("how much do they go for?", "gl, prices r crazy rn", "check the vendors, saw some earlier")},
				{0, pick("too much lol", "few more runs and i got it", "yeah ill look")}}
		}
	case 2: // class
		return []convoLine{{0, pick("how's "+strings.ToLower(b.class)+" treating u?", "is "+strings.ToLower(b.class)+" fun?")},
			{1, classTalk(b.class)},
			{0, pick("nice", "might make one later", "lol fair")}}
	case 3: // plans
		l := []convoLine{{0, pick("where u heading after town?", "going back out?")},
			{1, pick("back to "+b.spot(), "prob "+b.spot()+", u?")}}
		if a.hunt != "" && a.hunt == b.hunt {
			return append(l, convoLine{0, pick("same lol", "oh me too, see u there")})
		}
		return append(l, convoLine{0, pick(a.spot(), "gonna try "+a.spot())})
	}
	if a.guild != "" && a.guild == b.guild { // guild mates catching up
		return []convoLine{{0, pick("oh hey "+b.name, "yo "+b.name)}, {1, pick("hey! long time", "o/ whats up")},
			{0, pick("just restocking", "selling loot, then back out")}}
	}
	return []convoLine{{0, pick("hi", "hey", "yo")}, {1, pick("hey", "hi there", "o/")},
		{0, pick("busy town today", "u selling anything?", "nice gear")}, {1, pick("ya", "not really lol", "ty")}}
}

// convoPartyLines: a short party-chat exchange between two members.
func convoPartyLines(a, b convoSide) []convoLine {
	switch crand(4) {
	case 0:
		return []convoLine{{0, pick("how much exp to next lv?", "close to lv up?")},
			{1, pick(fmt.Sprintf("like %d%% left", 5+crand(60)), "almost there", "ages lol")},
			{0, pick("nice", "gl", "we'll get u there")}}
	case 1:
		if b.loot != "" {
			return []convoLine{{0, pick("drops any good so far?", "get anything nice?")},
				{1, pick("got a bunch of "+b.loot, "mostly "+b.loot+" lol")}, {0, pick("same", "sell it later", "nice")}}
		}
		return []convoLine{{0, pick("drops any good so far?", "get anything nice?")}, {1, pick("just junk lol", "nothing yet")},
			{0, pick("same", "rng hates us")}}
	case 2:
		return []convoLine{{0, pick("we stay here or move?", "this spot ok for everyone?")},
			{1, pick("stay a bit, exp is good", "fine by me", "a bit longer then move")}, {0, pick("ok", "k", "sounds good")}}
	}
	return []convoLine{{0, pick("ur doing great dmg "+b.name, "nice work "+b.name)}, {1, pick("ty lol", "haha thx", "u too")}}
}

// ambientTalk: called every chatter tick (chatter.go chatterLoop, 2 s). Starts at most one conversation.
func ambientTalk(on []*Bot, byMap map[string][]*Bot) {
	tn := tune()
	if !tn.AmbientTalkOn || len(on) < 2 {
		return
	}
	rate := chatRate(len(on), tn.AmbientTalkDiv, 0.3, tn.AmbientTalkMaxPerMin) // conversations per minute
	if cfloat() >= rate/30 {
		return
	}
	if cfloat() < 0.5 {
		ambientTown(byMap, tn.AmbientTalkMaxPerMin)
	} else {
		ambientParty(tn.AmbientTalkMaxPerMin)
	}
}

func ambientTown(byMap map[string][]*Bot, maxPerMin float64) {
	var towns []string
	for m, bs := range byMap {
		if isTown(m) && len(bs) >= 2 {
			towns = append(towns, m)
		}
	}
	if len(towns) == 0 {
		return
	}
	m := towns[crand(len(towns))]
	bs := byMap[m]
	if len(bs) > 30 { // a busy town: look at a random slice, not every player
		k := crand(len(bs) - 29)
		bs = bs[k : k+30]
	}
	var sides []convoSide
	for _, b := range bs {
		if c, ok := convoSideOf(b); ok && c.mapN == m {
			sides = append(sides, c)
		}
	}
	i, j := convoPair(sides, convoNearCells)
	if i < 0 || !convoAllowed("map:"+m, sides[i].b, sides[j].b, time.Now(), maxPerMin) {
		return
	}
	go playConvo(sides[i], sides[j], convoTownLines(sides[i], sides[j]), false)
}

func ambientParty(maxPerMin float64) {
	parties.Lock()
	var ps []*Party
	for _, p := range parties.byName {
		if len(p.Members) >= 2 {
			ps = append(ps, p)
		}
	}
	parties.Unlock()
	if len(ps) == 0 {
		return
	}
	p := ps[crand(len(ps))]
	parties.Lock()
	members := append([]*Bot(nil), p.Members...)
	parties.Unlock()
	var sides []convoSide
	for _, m := range members {
		if c, ok := convoSideOf(m); ok {
			c.busy = false // party chat goes on while fighting
			sides = append(sides, c)
		}
	}
	i, j := convoPair(sides, 1<<20) // same map is enough in a party
	if i < 0 || !convoAllowed(fmt.Sprintf("party:%p", p), sides[i].b, sides[j].b, time.Now(), maxPerMin) {
		return
	}
	go playConvo(sides[i], sides[j], convoPartyLines(sides[i], sides[j]), true)
}

// playConvo says the lines with human pauses; it stops when a speaker logged off or left the map.
func playConvo(a, b convoSide, lines []convoLine, party bool) {
	sides := [2]convoSide{a, b}
	for n, l := range lines {
		if n == 0 {
			time.Sleep(time.Duration(crand(3)) * time.Second)
		} else {
			time.Sleep(time.Duration(3+crand(6)) * time.Second) // reading + typing
		}
		s := sides[l.who]
		text := styled(s.typing, l.text)
		if party {
			if partyOf(s.b) == nil {
				return
			}
			s.b.partySay(text)
			continue
		}
		s.b.mu.Lock()
		if !s.b.Online || s.b.MapName != s.mapN {
			s.b.mu.Unlock()
			return
		}
		s.b.say(text)
		s.b.mu.Unlock()
	}
}
