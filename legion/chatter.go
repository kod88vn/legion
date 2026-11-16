package main

// Chatter: the algorithmic share of the world's talk, so the server sounds alive at all times (owner: "make the
// server alive with chats ... Shouldn't be server wide channel for everyone?").
//
//   - #global (rAthena channel, autojoin for everyone - rathena/conf/channels.conf is mounted with autojoin on):
//     party requests (LFP/LFM), trade (S>/B>) from real inventories and gear plans, guild recruiting by the guild
//     leaders, "where to level at N?" questions answered by other players from the levelling guide, build
//     questions, grats on job changes and milestones, greetings and small talk.
//   - local chat (normal 0x00f3, seen on screen): small talk between bots on the same map, death complaints.
//
// Every line is shaped by the speaker's typing style (persona.Typing) and chattiness. Rates scale with the online
// population (roughly 6-22 #global lines and 3-15 local lines a minute). The LLM minds (chat director etc.) still
// handle real conversations, especially with humans; human lines on #global reach their inbox via onChannel.

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode"
)

type chatSnap struct {
	b      *Bot
	name   string
	class  string
	lv     int
	mapN   string
	guild  string
	role   string
	loot   []int // Etc item ids carried (5+)
	want   string
	typing string
	chat   float64
	help   float64
}

var chatRng = rand.New(rand.NewSource(time.Now().UnixNano()))
var chatMu sync.Mutex // guards chatRng

func crand(n int) int {
	chatMu.Lock()
	defer chatMu.Unlock()
	return chatRng.Intn(n)
}
func cfloat() float64 {
	chatMu.Lock()
	defer chatMu.Unlock()
	return chatRng.Float64()
}
func pick(xs ...string) string { return xs[crand(len(xs))] }

func (b *Bot) snap() (chatSnap, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.Online || b.MapName == "" {
		return chatSnap{}, false
	}
	s := chatSnap{b: b, name: b.charName(), class: className(b.Class), lv: b.BaseLvOr(), mapN: b.MapName,
		guild: b.GuildName, role: b.charRole(), typing: b.P.Typing, chat: b.P.Chattiness, help: b.P.Helpfulness}
	for _, it := range b.inv {
		if x := items[it.ItemID]; x != nil && x.Type == "Etc" && it.Count >= 5 {
			s.loot = append(s.loot, it.ItemID)
		}
	}
	if it, _ := b.nextGearBudget(1 << 30); it != nil {
		s.want = it.Name
	}
	return s, true
}

// channelSay posts on a server channel ("#global", "#trade"): a whisper addressed to the channel.
func (b *Bot) channelSay(ch, text string) {
	b.send(newPkt(0x0096).u16(0).str(ch, 24).str(text, len(text)+1).varlen())
	b.event("said", b.charName(), ch+" "+text)
}

// ---------------------------------------------------------------- typing style

// tradeLine (run 10): a B>/S>/WTB/WTS line - its item names and prices are never scrambled or cut
func tradeLine(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	return strings.HasPrefix(u, "B>") || strings.HasPrefix(u, "S>") || strings.HasPrefix(u, "WTB") || strings.HasPrefix(u, "WTS") ||
		strings.Contains(u, " B>") || strings.Contains(u, " S>")
}

// replaceWord replaces whole words only ("are" -> "r" never turns "Rare" into "Rr")
func replaceWord(s, from, to string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], from)
		if j < 0 {
			out.WriteString(s[i:])
			break
		}
		j += i
		k := j + len(from)
		before := j == 0 || !isWordByte(s[j-1])
		after := k >= len(s) || !isWordByte(s[k])
		out.WriteString(s[i:j])
		if before && after {
			out.WriteString(to)
		} else {
			out.WriteString(from)
		}
		i = k
	}
	return out.String()
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == 39 // apostrophe
}

// typo (run 10): swaps two letters inside one plain lowercase word (filler), never in an item name (capitalised),
// a number or a price. Party watcher 19:27: "B> aHt 930z" - the old typo swapped any two bytes of the line.
func typo(s string) string {
	words := strings.Split(s, " ")
	var cand []int
	for i, w := range words {
		ok := len(w) >= 4
		for j := 0; ok && j < len(w); j++ {
			ok = w[j] >= 'a' && w[j] <= 'z'
		}
		if ok {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		return s
	}
	k := cand[crand(len(cand))]
	w := words[k]
	i := 1 + crand(len(w)-2) // never the first letter; i+1 <= len(w)-1
	words[k] = w[:i] + w[i+1:i+2] + w[i:i+1] + w[i+2:]
	return strings.Join(words, " ")
}

// typoRoll: the dice for the odd typo (a seam for the unit tests: a typo can garble a place or name they match on).
var typoRoll = cfloat

func styled(typing, s string) string {
	t := strings.ToLower(typing)
	trade := tradeLine(s)
	switch {
	case strings.Contains(t, "lowercase"):
		s = strings.ToLower(s)
		s = strings.TrimRight(s, ".!?")
		if !trade && cfloat() < 0.3 {
			s += " lol"
		}
	case strings.Contains(t, "emoticons"):
		s += pick(" :D", " xD", " ^^", " :)", "")
	case strings.Contains(t, "all caps"):
		if cfloat() < 0.35 {
			s = strings.ToUpper(s)
		}
	case strings.Contains(t, "abbreviations") || strings.Contains(t, "slang"):
		for _, r := range [][2]string{{"you", "u"}, {"are", "r"}, {"please", "pls"}, {"thanks", "ty"}, {"because", "cuz"}, {"right now", "rn"}} {
			s = replaceWord(s, r[0], r[1])
		}
	case strings.Contains(t, "terse"):
		if w := strings.Fields(s); !trade && len(w) > 6 && cfloat() < 0.5 {
			s = strings.Join(w[:6], " ")
		}
	case strings.Contains(t, "broken english"):
		s = strings.ReplaceAll(s, " the ", " ")
		s = strings.ReplaceAll(s, " is ", " ")
	case strings.Contains(t, "polite"):
		if !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
			s += pick(", thanks!", " :)", ".")
		}
	case strings.Contains(t, "proper"):
		if r := []rune(s); len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
			s = string(r)
		}
	}
	if !trade && typoRoll() < 0.06 && len(s) > 8 { // the odd typo, on a filler word
		s = typo(s)
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// ---------------------------------------------------------------- content

var friendlyMap = map[string]string{
	"prt_fild08": "south prontera field", "prt_fild06": "west prontera field", "prt_fild01": "prt fild 01",
	"gef_fild07": "geffen field 7", "gef_fild00": "chonchon field n of geffen", "gef_fild10": "orc field",
	"pay_fild01": "payon forest", "pay_fild02": "payon field 2", "pay_fild03": "pay fild 03", "pay_fild04": "pay fild 04",
	"pay_dun00": "payon cave", "pay_dun01": "payon cave 2", "moc_fild02": "morroc desert", "moc_fild18": "moc fild 18",
	"mjolnir_09": "mjolnir 9", "mjo_dun01": "mine dungeon", "orcsdun01": "orc dungeon", "prt_sewb1": "culverts",
	"iz_dun00": "byalan 1", "iz_dun01": "byalan 2", "gef_dun00": "geffen dungeon", "ant_hell01": "ant hell",
}

func mapTalk(m string) string {
	if f, ok := friendlyMap[m]; ok {
		return f
	}
	return strings.ReplaceAll(m, "_", " ")
}

func region(m string) string {
	switch {
	case strings.HasPrefix(m, "prt") || m == "prontera" || strings.HasPrefix(m, "iz"):
		return "prt"
	case strings.HasPrefix(m, "pay"):
		return "payon"
	case strings.HasPrefix(m, "gef"):
		return "geffen"
	case strings.HasPrefix(m, "moc"):
		return "morroc"
	case strings.HasPrefix(m, "mjo"):
		return "mjolnir"
	}
	return "here"
}

func (s chatSnap) lfp() string {
	switch s.class {
	case "Acolyte", "Priest":
		return pick(fmt.Sprintf("%s %d LFP, can heal/bless", s.class, s.lv), fmt.Sprintf("lf party, %s lv%d, heal+inc agi", strings.ToLower(s.class), s.lv))
	case "Swordman", "Knight":
		return pick(fmt.Sprintf("%s %d lf party, i tank", s.class, s.lv), fmt.Sprintf("tank lv%d LFP %s", s.lv, region(s.mapN)))
	}
	return pick(fmt.Sprintf("LFP %s lv%d", s.class, s.lv), fmt.Sprintf("%s %d looking for party, %s area", s.class, s.lv, region(s.mapN)),
		fmt.Sprintf("any party for lv%d %s?", s.lv, strings.ToLower(s.class)))
}

func (s chatSnap) trade() string {
	if len(s.loot) > 0 && cfloat() < 0.6 {
		id := s.loot[crand(len(s.loot))]
		base := sellPrice(id) // EC48: the chat price follows the model's reference when dynamic_price_on (pricedyn.go)
		if dynamicPriceOn() {
			base = max(priceOf(id), base)
		}
		pr := base * (12 + crand(10)) / 10
		return pick(fmt.Sprintf("S> %s %dz ea", itemName(id), max(pr, 2)), fmt.Sprintf("selling %s, pm offers", itemName(id)),
			fmt.Sprintf("S> %s cheap, pm", itemName(id)))
	}
	if s.want != "" {
		return pick("B> "+s.want+" pm me", "buying "+s.want+", pm", "anyone selling "+s.want+"?")
	}
	return pick("anyone selling red pots cheap?", "B> fly wings", "B> butterfly wing x5", "price check jellopy?")
}

var smallTalk = []string{
	"server is busy today", "anyone else lagging?", "this grind is so slow", "brb food", "ugh no drops all day",
	"so many porings lol", "who wants to duo?", "back", "finally got some zeny", "need more pots...",
	"anyone doing culverts later?", "ks much", "lol nice", "gonna afk soon", "almost job 10", "this map is packed",
	"payon cave or orcs?", "these chonchons hit hard", "first card drop ever!!", "wish i had a priest friend",
}

func (s chatSnap) question() (string, func(a chatSnap) string) {
	switch crand(3) {
	case 0:
		lv := s.lv
		return pick(fmt.Sprintf("where to lvl at %d?", lv), fmt.Sprintf("good spot for lv%d %s?", lv, strings.ToLower(s.class)),
				fmt.Sprintf("best place to level %d-%d?", lv, lv+10)), func(a chatSnap) string {
				gs := a.b.guideSpotsFor(lv)
				if len(gs) == 0 {
					return pick("idk, try the fields", "just grind porings lol")
				}
				g := gs[crand(len(gs))]
				return pick(mapTalk(g.Map)+" is decent at that lv", "try "+mapTalk(g.Map), mapTalk(g.Map)+" worked for me",
					"i went "+mapTalk(g.Map))
			}
	case 1:
		return pick("what job should i go?", "is acolyte good solo?", "thief or archer for first char?", "swordie or mage?"),
			func(a chatSnap) string {
				return pick("go "+strings.ToLower(a.class)+" its fun", "depends what u like", "acolyte if u want parties",
					"thief, easy to level", "mage kills fast but squishy")
			}
	}
	return pick("where do i buy armor?", "where are the shops?", "where to sell loot?"), func(a chatSnap) string {
		return pick("south gate of prontera", "prontera south gate, all shops there", "go to pron south gate")
	}
}

// ---------------------------------------------------------------- scheduler

func chatterLoop(bots []*Bot, stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		var on []*Bot
		byMap := map[string][]*Bot{}
		for _, b := range bots {
			b.mu.Lock()
			if b.Online && b.MapName != "" {
				on = append(on, b)
				byMap[b.MapName] = append(byMap[b.MapName], b)
			}
			b.mu.Unlock()
		}
		if len(on) < 2 {
			continue
		}
		// owner: "Reduce global chat rate and increase party, guild, local chat" (lines per minute)
		tn := tune() // rates and divisors: data/tuning.json (tuning.go)
		global := chatRate(len(on), tn.ChatGlobalDiv, tn.ChatGlobalMin, tn.ChatGlobalMax)
		local := chatRate(len(on), tn.ChatLocalDiv, tn.ChatLocalMin, tn.ChatLocalMax)
		party := chatRate(len(on), tn.ChatPartyDiv, tn.ChatPartyMin, tn.ChatPartyMax)
		guild := chatRate(len(on), tn.ChatGuildDiv, tn.ChatGuildMin, tn.ChatGuildMax)
		if cfloat() < global/30 {
			chatterGlobal(on)
		}
		for n := local / 30; n > 0; n-- { // may be >1 per tick
			if cfloat() < n {
				chatterLocal(byMap)
			}
		}
		for n := party / 30; n > 0; n-- {
			if cfloat() < n {
				chatterParty()
			}
		}
		if cfloat() < guild/30 {
			chatterGuild(on)
		}
		ambientTalk(on, byMap) // convo.go: short conversations between players near each other
	}
}

func clampF(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

// talker picks an online bot, weighted by chattiness.
func talker(bs []*Bot, skip *Bot) (chatSnap, bool) {
	for i := 0; i < 8; i++ {
		b := bs[crand(len(bs))]
		if b == skip {
			continue
		}
		if s, ok := b.snap(); ok && cfloat() < 0.25+s.chat {
			return s, true
		}
	}
	return chatSnap{}, false
}

func chatterGlobal(on []*Bot) {
	// #global carries only what matters to everyone (owner: "reduce the global chat, let them talk about important
	// stuff only"; real conversation is the LLM minds' job): looking for party/members, wanted/for-sale items
	// (#trade), guild recruiting. No small talk, no quiz questions.
	s, ok := talker(on, nil)
	if !ok {
		return
	}
	var line string
	ch := "#global"
	r := cfloat()
	inParty := partyOf(s.b) != nil
	switch {
	case s.role == "vendor" || r < tune().ChatGlobalTrade:
		line, ch = s.trade(), "#trade"
	case r < tune().ChatGlobalLFP && !inParty && !goalPartiesOn():
		// with need-driven parties on, LFG/LFM lines come from lfg.go only (real posts that lead to parties)
		line = s.lfp()
	case s.guild != "" && guildLeaderOf(s.b):
		line = pick(fmt.Sprintf("[%s] recruiting, all lvls welcome, pm me", s.guild), fmt.Sprintf("%s is looking for active ppl! pm %s", s.guild, s.name))
	default:
		return
	}
	s.b.channelSay(ch, styled(s.typing, line))
}

func guildLeaderOf(b *Bot) bool {
	if g := b.guild(); g != nil {
		return guildLeader(g.ID) == b
	}
	return false
}

func chatterLocal(byMap map[string][]*Bot) {
	var maps []string
	for m, bs := range byMap {
		if len(bs) >= 3 {
			maps = append(maps, m)
		}
	}
	if len(maps) == 0 {
		return
	}
	bs := byMap[maps[crand(len(maps))]]
	s, ok := talker(bs, nil)
	if !ok {
		return
	}
	var line string
	var reply []string
	inParty := partyOf(s.b) != nil
	if isTown(s.mapN) {
		line = pick("anyone wanna party?", "hi all", "where is the kafra", "selling stuff, pm", "zzz", "lf "+pick("priest", "tank", "duo"))
		reply = []string{"hey", "sup", "yo", "me", "maybe later", "o/"}
	} else {
		line = pick("hi", "u lvling here too?", "nice spot", "ks?", "lots of mobs here", "lol that was close", "gonna stay here a while",
			"want to party up?", "gl hf", "any drops?")
		reply = []string{"yeah", "hi", "lol", "sure", "ya", "np", "nope", "gl", "hehe", "sorry thought it was free"}
	}
	// already in a party: no recruiting lines (Zen.fox asked Ayla "want to party up?" while in her party)
	if inParty && (strings.Contains(line, "party") || strings.HasPrefix(line, "lf ")) {
		line = pick("gl hf", "lots of mobs here", "any drops?", "nice spot")
	}
	s.b.mu.Lock()
	s.b.say(styled(s.typing, line))
	s.b.mu.Unlock()
	if cfloat() < tune().ChatLocalReply {
		go func() {
			time.Sleep(time.Duration(3+crand(8)) * time.Second)
			if a, ok := talker(bs, s.b); ok {
				a.b.mu.Lock()
				a.b.say(styled(a.typing, pick(reply...)))
				a.b.mu.Unlock()
			}
		}()
	}
}

// chatterReact: some players announce milestones and get grats from others (called from Bot.event, b.mu held).
var reactBots []*Bot

func (b *Bot) chatterReact(kind, text string) {
	if len(reactBots) == 0 || cfloat() > tune().ChatReactBase+b.P.Chattiness*tune().ChatReactChattiness {
		return
	}
	var lv int
	fmt.Sscanf(text, "reached base level %d", &lv)
	var line string
	switch kind {
	case "jobchange":
		if cfloat() > tune().ChatReactJobChange { // a few, not every one
			return
		}
		line = pick("finally "+strings.TrimPrefix(text, "became a ")+"!!", "just "+text+" :D", "yay job change")
	case "levelup":
		if lv == 0 || lv%10 != 0 || lv < 30 {
			return
		}
		line = pick(fmt.Sprintf("%d!!", lv), fmt.Sprintf("finally lv %d", lv), fmt.Sprintf("base %d woo", lv))
	case "death":
		if cfloat() > tune().ChatReactDeath {
			return
		}
		line = pick("ugh died", "rip", "died again...", "who put that mob there", "lag killed me")
	default:
		return
	}
	typing := b.P.Typing
	go func() {
		time.Sleep(time.Duration(1+crand(4)) * time.Second)
		if kind == "death" {
			b.mu.Lock()
			b.say(styled(typing, line))
			b.mu.Unlock()
			return
		}
		b.channelSay("#global", styled(typing, line))
		for i := 0; i < crand(2); i++ {
			time.Sleep(time.Duration(2+crand(6)) * time.Second)
			if a, ok := talker(reactBots, b); ok {
				a.b.channelSay("#global", styled(a.typing, pick("grats", "gz!", "congrats", "gratz", "grats!! :)", "nice gz")))
			}
		}
	}()
}

// onChannel: channel lines (ZC_NPC_CHAT "[Global] Name : text"). Lines from humans go to the minds' inbox once.
var channelSeen sync.Map // text -> time

func (b *Bot) onChannel(pk []byte) {
	if len(pk) <= 12 {
		return
	}
	line := cstr(pk[12:])
	// 0x02c1 is also npctalk / colored NPC speech (id = the NPC): "Resting Adventurer : You know what? If you are
	// sitting..." on prt_fild08 reached the minds' inbox as a HUMAN. Channel lines (#global) carry id 0.
	if id := le.Uint32(pk[4:]); id != 0 {
		if o := b.ents[id]; id >= 100000000 || (o != nil && o.Type != objPC) {
			b.hear("npc", line)
			return
		}
	}
	rest := line
	if i := strings.Index(rest, "] "); i > 0 && strings.HasPrefix(rest, "[") {
		rest = rest[i+2:]
	}
	j := strings.Index(rest, " : ")
	if j <= 0 || allChars[rest[:j]] {
		return
	}
	if v, ok := channelSeen.Load(line); ok && time.Since(v.(time.Time)) < 30*time.Second {
		return
	}
	channelSeen.Store(line, time.Now())
	b.hear("chat", line)
	noteHumanTradeLine(rest[:j], rest[j+3:]) // humanwant.go: a human's B> gets an answer
}

// ---------------------------------------------------------------- party and guild chat

// partySay: party chat (CZ_REQUEST_CHAT_PARTY "Name : text").
func (b *Bot) partySay(text string) {
	msg := b.charName() + " : " + text
	b.send(newPkt(0x0108).u16(0).str(msg, len(msg)+1).varlen())
	b.event("said", b.charName(), "(party) "+text)
	noteSaid(b, text) // leadertalk.go: a decision already spoken is not spoken twice
}

// guildSay: guild chat (CZ_GUILD_CHAT "Name : text").
func (b *Bot) guildSay(text string) {
	msg := b.charName() + " : " + text
	b.send(newPkt(0x017e).u16(0).str(msg, len(msg)+1).varlen())
	b.event("said", b.charName(), "(guild) "+text)
}

// spokenWhy: a trip's log reason without its log label ("buying Jacket (on the party town run)" -> "buying Jacket")
func spokenWhy(why string) string {
	if i := strings.Index(why, " ("); i > 0 {
		return why[:i]
	}
	return why
}

// tripAnnounce: a party member heading to town tells the party why (called with b.mu held).
func (b *Bot) tripAnnounce(why, town string) {
	// the trip's log reason ends in a label for the event log ("(on the party town run)", "(necessary: class
	// weapon)"): players never say those (2026-11-14, party chat read like a log)
	why = spokenWhy(why)
	if b.leaderTalkSkipTrip(why) { // leadertalk.go: the leader says the run's plan instead
		return
	}
	item := strings.ToLower(strings.TrimPrefix(why, "buying "))
	var line string
	switch {
	case strings.HasPrefix(why, "town run"):
		line = pick("ok town run, i got stuff to sell too", "coming, need to sell + restock", "town run, omw")
		// "bag is heavy" only when it is (QA 2026-11-14: Zen.fox said it at 37% weight - a canned line, not a need)
		if b.MaxWeight > 0 && b.Weight*100/b.MaxWeight >= 50 && b.rng.Float64() < 0.5 {
			line = "yes pls, bag is heavy"
		}
	case why == "with the party":
		line = pick("coming with u guys", "ok following to town", "omw to town too")
	case strings.HasPrefix(why, "buying "):
		line = pick("brb, buying a "+item+" in "+town, "getting a "+item+" in "+town+", back soon",
			"gonna grab a "+item+" in "+town+", brb", "brb shopping, need a "+item)
		if strings.HasSuffix(item, "s") || strings.HasPrefix(item, "arrow") || strings.Contains(item, " ") {
			line = pick("brb, buying "+item+" in "+town, "getting "+item+" in "+town+", back soon", "brb shopping, need "+item)
		}
	case why == "bag is full" || why == "bag is heavy":
		line = pick("overweight, brb selling", "bag full lol, quick town trip", "brb need to sell stuff")
	case why == "out of potions":
		line = pick("out of pots, brb", "need pots, back in a few", "brb buying potions")
	case why == topUpWhy: // together.go potTopUpNeed: a top-up before the dungeon, not an emergency
		line = pick("grabbing pots, 1 min", "quick pot grab, 1 min", "grabbing a few pots, 1 min")
	case strings.HasPrefix(why, "selling loot"):
		line = pick("brb selling loot", "gonna sell my stuff real quick", "town trip, brb")
	default:
		line = "brb " + why
	}
	typing := b.P.Typing
	go b.partySay(styled(typing, line))
}

func chatterParty() {
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
	m := p.Members[crand(len(p.Members))]
	s, ok := m.snap()
	if !ok {
		return
	}
	var line string
	switch partyRole(m) {
	case roleTank:
		line = pick("pulling", "got it", "stay behind me", "on me", "next one", "careful, 2 coming", "hp low, heal pls")
	case roleSupport:
		line = supportLine(s.b) // healreach.go: "healing" only right after a heal the server confirmed
	case roleRanged:
		line = pick("on it", "nice pull", "kiting this one", "sp low", "that one's mine lol", "good exp here")
	default:
		line = pick("nice", "lol", "this exp is good", "brb 1 min", "back", "ty", "we stay here?", "gg", "almost lv up")
	}
	if p.Human == "" && m == p.Leader {
		line = pick("this way", "stay close", "next pull coming", "everyone ok?", "sp check", "we move after this one",
			"call out if u need pots", "nice, keep it up", "dont pull extra pls", "tank first, then hit")
	}
	if p.Human != "" && cfloat() < tune().ChatPartyHuman {
		line = pick("where next "+p.Human+"?", "following u "+p.Human, "nice one "+p.Human, "wait up "+p.Human)
	}
	go s.b.partySay(styled(s.typing, line))
	if cfloat() < tune().ChatPartyReply && len(p.Members) > 1 {
		go func() {
			time.Sleep(time.Duration(2+crand(6)) * time.Second)
			o := p.Members[crand(len(p.Members))]
			if o == m {
				return
			}
			if os, ok := o.snap(); ok {
				o.partySay(styled(os.typing, pick("ok", "k", "lol", "ty", "np", "yep", "sure", "omw")))
			}
		}()
	}
}

func chatterGuild(on []*Bot) {
	s, ok := talker(on, nil)
	if !ok || s.guild == "" {
		return
	}
	line := pick("evening guild", "anyone need help?", "who's on?", "lf party, anyone?", "gz on the job change!",
		"where is everyone leveling?", "brb food", "lol", "nice work today guys", "anyone selling pots?", "o/")
	if guildLeaderOf(s.b) && cfloat() < tune().ChatGuildLeader {
		line = pick("guild party at "+mapTalk(s.mapN)+", come join", "everyone level safe, no solo in dungeons",
			"recruit your friends!", "good job today", "who needs a priest?")
	}
	go s.b.guildSay(styled(s.typing, line))
}
