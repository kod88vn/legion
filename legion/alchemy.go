package main

// Alchemy: every guild grows its own Alchemist who brews potions and sells them (owner 2026-11-14: "Eventually each
// guild needs to have an alchemist who makes pots and they can sell pots for money ... the economy is the core to the
// game"). No NPC sells Blue Potions (SP) here on purpose: Blue Syrup at 7,000z is the only NPC SP potion, so a guild
// alchemist's Blue Potions are what keeps priests and mages going.
//
// Real progression, no shortcuts: the guild alchemist is a character whose build plan is "alchemist" (Merchant ->
// Alchemist at Merchant job 40, rathena/npc-custom/legion.txt legion_branch 1). alchemyManager picks the most advanced
// such character of every guild (the guild's alchemist) and that player logs in with it more often (persona.go
// pickChar). Once it is an Alchemist with Pharmacy (AM_PHARMACY 228, after Potion Research 227 lv5) it:
//   - buys the Potion Creation Guide (7144, 100,000z, kept, never consumed) and Medicine Bowls (7134, 8z, one per
//     attempt) from Gever Al Sharp, the Guild Dealer of the Alchemist Union (alde_alche 24,188): NPC dialog
//     "Purchase a production manual." -> "Potion Creation Guide" -> "Purchase." / "Purchase materials." ->
//     "Medicine Bowl - 8 Zeny" -> amount (ZC_OPEN_EDITDLG 0x0142 -> CZ_INPUT_EDITDLG 0x0143);
//   - buys Empty Potion Bottles (1093, 10z) and Scell (911) from the Goods Merchant at Prontera's south gate
//     (town.go trip "alchemy");
//   - gets herbs from its own hunting and from other players: B> wants on #trade, guild mates sell cheaper
//     (economy.go); herbs are never sold to NPCs (they are not Etc loot);
//   - brews (rAthena produce_db: Blue Potion = Blue Herb + Scell + Empty Potion Bottle; White Potion = White Herb +
//     bottle; Yellow Potion = Yellow Herb + bottle). Packets for this client (PACKETVER 20210107):
//       CZ_USE_SKILL 0x0438 (lv, 228, own id)  -> server consumes a Medicine Bowl, sends
//       ZC_MAKABLEITEMLIST 0x018d (len, per item: itemId u32, material[3] u32) = what can be made right now
//       CZ_REQMAKINGITEM   0x018e (itemId u32, material[3] u32)  (14 bytes)
//       ZC_ACK_REQMAKINGITEM 0x018f (result i16, itemId u32): 0/2 = made, 1/3 = failed (materials are lost)
//   - keeps a few for itself, gives Blue/White Potions to its guild leader's party (SP potions for priests and mages
//     first), sells to guild mates at a guild price and to everyone else at the market price (economy.go).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	skPotionResearch      = 227 // AM_LEARNINGPOTION
	skPharmacy            = 228 // AM_PHARMACY
	skDiscount            = 37  // MC_DISCOUNT: a merchant sees discounted NPC prices (town.go wingPriceSeen)
	itemMedicineBowl      = 7134
	itemPotionGuide       = 7144
	itemEmptyPotionBottle = 1093
	itemScell             = 911
	itemRedHerb           = 507
	itemYellowHerb        = 508
	itemWhiteHerb         = 509
	itemBlueHerb          = 510
	itemBluePotion        = 505
	guideCost             = 100000
	alcheDealerMap        = "alde_alche"
)

var alcheDealer = [2]int{24, 188} // Gever Al Sharp, Guild Dealer (npc/merchants/alchemist.txt)

type alchRecipe struct {
	Out  int
	Mats [][2]int // item, amount (the guide is checked separately: it is not consumed)
}

// pharmacyRecipes: what a guild alchemist brews, most wanted first. Red Potions cost more to brew (bottle + bowl)
// than the Tool Dealer charges, so they are left out; Yellow Potions (~63z of materials) sell at ~160z.
var pharmacyRecipes = []alchRecipe{
	{itemBluePotion, [][2]int{{itemBlueHerb, 1}, {itemScell, 1}, {itemEmptyPotionBottle, 1}}},
	{itemWhitePotion, [][2]int{{itemWhiteHerb, 1}, {itemEmptyPotionBottle, 1}}},
	{itemYellowPotion, [][2]int{{itemYellowHerb, 1}, {itemEmptyPotionBottle, 1}}},
}

// brewedKeep: how many of each brewed potion the alchemist keeps for itself; the rest goes to the guild and the market
var brewedKeep = map[int]int{itemBluePotion: 5, itemWhitePotion: 5, itemYellowPotion: 10}

func isAlchemistClass(c int) bool { return c == 18 || c == 4019 || c == 4071 || c == 4078 }

func isHerb(id int) bool { return id >= itemRedHerb && id <= itemBlueHerb }

// brewMaterial: what the Pharmacy consumes (herbs, Scell, bottles, Medicine Bowls) - a brewer keeps it
func brewMaterial(id int) bool {
	return isHerb(id) || id == itemScell || id == itemEmptyPotionBottle || id == itemMedicineBowl
}

// ---------------------------------------------------------------- what every character's account knows

// charLevels: class / levels of every character seen in a char list (bot.go reads all slots at login), so the guild
// alchemist can be picked among characters that are not logged in right now.
type charLv struct{ Class, Base, Job int }

var charLevels sync.Map // char id -> charLv

func noteCharLevels(cid, class, base, job int) {
	if cid > 0 {
		if v, ok := charLevels.Load(cid); !ok || v.(charLv) != (charLv{class, base, job}) {
			atomic.StoreInt32(&charLevelsDirty, 1)
		}
		charLevels.Store(cid, charLv{class, base, job})
	}
}

// economy run 11: charLevels lived only in memory, so after every restart each offline character counted as its
// persona seed (Novice 1) and guilds 01/02/04/08/09/10 showed "Novice 1, offline" as their alchemist although each has
// lv58-70 Alchemists (DB 15:50). The levels are now saved to <data>/char_levels.json (char id -> class/base/job) and
// loaded before the first pick; tools can seed the file from the char table.
var charLevelsDirty int32

func charLevelsFile() string { return filepath.Join(*dataDir, "char_levels.json") }

func loadCharLevels() {
	raw, err := os.ReadFile(charLevelsFile())
	if err != nil {
		return
	}
	var m map[string][3]int
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	for k, v := range m {
		if cid, err := strconv.Atoi(k); err == nil && cid > 0 {
			if _, ok := charLevels.Load(cid); !ok { // what this run saw itself wins
				charLevels.Store(cid, charLv{v[0], v[1], v[2]})
			}
		}
	}
}

func saveCharLevels() {
	if coreInstance() || !atomic.CompareAndSwapInt32(&charLevelsDirty, 1, 0) {
		return // the main instance writes the file (the core instance sees only a handful of accounts)
	}
	m := map[string][3]int{}
	if raw, err := os.ReadFile(charLevelsFile()); err == nil {
		json.Unmarshal(raw, &m) // keep characters this run hasn't seen
	}
	charLevels.Range(func(k, v any) bool {
		l := v.(charLv)
		m[strconv.Itoa(k.(int))] = [3]int{l.Class, l.Base, l.Job}
		return true
	})
	if raw, err := json.Marshal(m); err == nil {
		tmp := charLevelsFile() + ".tmp"
		if os.WriteFile(tmp, raw, 0o644) == nil {
			os.Rename(tmp, charLevelsFile())
		}
	}
}

func knownLevels(c *CharSpec) charLv {
	if v, ok := charLevels.Load(c.CharID); ok {
		return v.(charLv)
	}
	return charLv{c.Class, c.BaseLevel, c.JobLevel}
}

// guildOfAccount: the guild a player belongs to - the one it joined in game, else its assigned guild
func guildOfAccount(b *Bot) string {
	if humanGuildMember(b) { // humanguildlead.go: Starveil members and Ayla's party friends
		return humanGuildID
	}
	if g := b.leaderVision(); g != nil {
		return g.ID
	}
	if n := b.GuildName; n != "" { // read without b.mu: a string snapshot is fine here
		if g := guildByName[strings.ToLower(n)]; g != nil {
			return g.ID
		}
	}
	return b.P.Guild
}

// ---------------------------------------------------------------- the guild's alchemist

type guildAlch struct {
	Guild      string
	Bot        *Bot
	Char       string
	Role       string
	Lv         charLv
	Candidates int
	Since      time.Time
}

var alchemists = struct {
	sync.Mutex
	byGuild map[string]*guildAlch
	byChar  map[string]string // char name -> guild id
}{byGuild: map[string]*guildAlch{}, byChar: map[string]string{}}

// alchTier: how far along the Merchant -> Alchemist path a character is
func alchTier(l charLv) int {
	switch {
	case isAlchemistClass(l.Class):
		return 3
	case l.Class == 5 || l.Class == 4006:
		return 2
	case l.Class == 0 || l.Class == 4001:
		return 1
	}
	return 0 // went another way (e.g. Blacksmith): not an alchemist candidate
}

func alchScore(l charLv, role string) int {
	s := alchTier(l)*100000 + l.Job*100 + l.Base
	if role == "main" {
		s += 50 // a main character is played most
	}
	return s
}

// pickGuildAlchemists: for every guild, the most advanced character on the alchemist plan. Sticky: the current one
// keeps the job unless another candidate is a whole tier further, or 10 job levels ahead.
var alchStart = time.Now() // process start: picks saved before a restart get 20 min to log in

func pickGuildAlchemists(bots []*Bot, now time.Time) {
	type cand struct {
		b     *Bot
		c     *CharSpec
		lv    charLv
		score int
	}
	best := map[string]cand{}
	count := map[string]int{}
	cands := map[string]cand{} // guild|char -> candidate (the saved pick, data/alchemists.json)
	for _, b := range bots {
		g := guildOfAccount(b)
		if g == "" {
			continue
		}
		for i := range b.P.Chars {
			c := &b.P.Chars[i]
			if builds[c.Plan].base() != "alchemist" || c.Role == "support" {
				continue
			}
			lv := knownLevels(c)
			if alchTier(lv) == 0 {
				continue
			}
			count[g]++
			s := alchScore(lv, c.Role)
			if c.Plan == "alchemist_potion" {
				s += 500 // the brewer build (INT/DEX/LUK, Pharmacy first): ~5 job levels ahead of a hybrid/battle one
			}
			cands[g+"|"+c.Name] = cand{b, c, lv, s}
			if o, ok := best[g]; !ok || s > o.score {
				best[g] = cand{b, c, lv, s}
			}
		}
	}
	saved := loadAlchPicks()
	alchemists.Lock()
	defer alchemists.Unlock()
	changed := false
	for g, c := range best {
		cur := alchemists.byGuild[g]
		if cur == nil { // after a restart: the character picked before (it may hold the guild's fund)
			if sc, ok := cands[g+"|"+saved[g]]; ok {
				cur = &guildAlch{Guild: g, Since: now, Bot: sc.b, Char: sc.c.Name, Role: sc.c.Role, Lv: sc.lv}
				alchemists.byGuild[g] = cur
			} else if saved[g] != "" && now.Sub(alchStart) < 20*time.Minute {
				// batch 14: the saved pick isn't a candidate YET - a character that joined the guild in game (persona
				// guild "") counts for it only once logged in. 05:06 deploy: Blaze.mage (Alchemist, 9.2k of guild03's
				// fund) was replaced by an offline Novice and could never win the job back. Wait for it.
				continue
			}
		}
		if cur != nil && cur.Char != c.c.Name {
			// keep the current one unless clearly overtaken
			var curLv charLv
			seen := false
			if cur.Bot != nil {
				for i := range cur.Bot.P.Chars {
					if cur.Bot.P.Chars[i].Name == cur.Char {
						curLv = knownLevels(&cur.Bot.P.Chars[i])
						_, seen = charLevels.Load(cur.Bot.P.Chars[i].CharID)
					}
				}
			}
			if !seen { // its account hasn't logged in since the restart: the seed levels say Novice, don't judge it yet.
				// (batch 15: no time limit - with one, at 06:25 four offline Alchemists holding 6-14k of fund each
				// (Lily_dancer, Star song...) were replaced by low Merchants; the restore wait above fixed Blaze.mage)
				cur.Candidates = count[g]
				continue
			}
			// only a whole tier further takes over: the guild's fund (guildFund) is in the current character's bag
			if alchTier(curLv) >= alchTier(c.lv) {
				cur.Lv, cur.Candidates = curLv, count[g]
				continue
			}
		}
		if cur == nil || cur.Char != c.c.Name {
			cur = &guildAlch{Guild: g, Since: now}
			alchemists.byGuild[g] = cur
			changed = true
		}
		cur.Bot, cur.Char, cur.Role, cur.Lv, cur.Candidates = c.b, c.c.Name, c.c.Role, c.lv, count[g]
	}
	alchemists.byChar = map[string]string{}
	picks := map[string]string{}
	for g, a := range alchemists.byGuild {
		alchemists.byChar[a.Char] = g
		picks[g] = a.Char
	}
	if (changed || len(saved) == 0) && !coreInstance() && len(picks) > 0 {
		if raw, err := json.MarshalIndent(picks, "", " "); err == nil {
			os.WriteFile(filepath.Join(*dataDir, "alchemists.json"), raw, 0644)
		}
	}
}

// loadAlchPicks: the guild alchemists picked before the last restart (guild -> character)
func loadAlchPicks() map[string]string {
	m := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join(*dataDir, "alchemists.json")); err == nil {
		json.Unmarshal(raw, &m)
	}
	return m
}

// guildAlchemistChar: the guild id when this character is its guild's alchemist
func guildAlchemistChar(name string) string {
	alchemists.Lock()
	defer alchemists.Unlock()
	return alchemists.byChar[name]
}

func guildAlchemistOf(guild string) *guildAlch {
	alchemists.Lock()
	defer alchemists.Unlock()
	if a := alchemists.byGuild[guild]; a != nil {
		c := *a
		return &c
	}
	return nil
}

// brewersSeen: characters seen owning the Potion Creation Guide (data/brewers.json). Economy run 9: both guilds'
// brewers (Neko.fang, lilzensenpaiVend) were offline at 13:20 while their accounts played other characters - a
// guild alchemist with the guide is played most sessions (persona.go pickChar) so the guild gets its potions.
var brewersSeen = struct {
	sync.Mutex
	m      map[string]bool
	loaded bool
}{m: map[string]bool{}}

func loadBrewers() {
	if brewersSeen.loaded {
		return
	}
	brewersSeen.loaded = true
	if raw, err := os.ReadFile(filepath.Join(*dataDir, "brewers.json")); err == nil {
		json.Unmarshal(raw, &brewersSeen.m)
	}
}

func noteBrewer(name string) {
	brewersSeen.Lock()
	defer brewersSeen.Unlock()
	loadBrewers()
	if brewersSeen.m[name] {
		return
	}
	brewersSeen.m[name] = true
	if !coreInstance() {
		if raw, err := json.MarshalIndent(brewersSeen.m, "", " "); err == nil {
			os.WriteFile(filepath.Join(*dataDir, "brewers.json"), raw, 0644)
		}
	}
}

// guildBrewerChar: this account's character that is its guild's alchemist and owns the guide ("" none). Economy run
// 9: at 13:54 both such accounts (Neko.fang, lilzensenpaiVend) were off schedule - the guild got no potions.
func (b *Bot) guildBrewerChar() string {
	for i := range b.P.Chars {
		n := b.P.Chars[i].Name
		g := guildAlchemistChar(n)
		if g == "" {
			continue
		}
		// run 11: also the designated Alchemist (tier 3) without the guide yet - it is the guild's crafter and has to be
		// online to receive the guide fund, fetch the guide and brew (6 guilds' Alchemists sat offline all day)
		if knownBrewer(n) || g != humanGuildID && alchTier(knownLevels(&b.P.Chars[i])) == 3 {
			return n
		}
	}
	return ""
}

func knownBrewer(name string) bool {
	brewersSeen.Lock()
	defer brewersSeen.Unlock()
	loadBrewers()
	return brewersSeen.m[name]
}

// allBots: every bot of this instance (set by alchemyManager; read by the economy API)
var allBots atomic.Value

func alchemyManager(bots []*Bot, stop <-chan struct{}) {
	allBots.Store(bots)
	loadCharLevels() // run 11: real levels of offline characters (char_levels.json)
	pickGuildAlchemists(bots, time.Now())
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		pickGuildAlchemists(bots, time.Now())
		saveCharLevels()
	}
}

// ---------------------------------------------------------------- the brewer (b.mu held)

type alchState struct {
	Stage      string // "" idle, cast, make, dealer
	At         time.Time
	Item       int
	NextBrew   time.Time
	Pause      time.Time
	Timeouts   int
	Brewed     map[int]int // this session: potion -> made
	Failed     int
	Dealer     string // guide | bowls
	DealerNPC  uint32
	DealerNext time.Time
	BottleNext time.Time
	ShopNext   time.Time // vendShopClose: the vendor-alchemist's shop closes at most once per 20 min for the craft
	savedGoal  Goal
	goalSet    bool
	Last       string // last thing that happened, for the API
}

var alchStates sync.Map // *Bot -> *alchState

func (b *Bot) alchSt() *alchState {
	if v, ok := alchStates.Load(b); ok {
		return v.(*alchState)
	}
	st := &alchState{Brewed: map[int]int{}}
	alchStates.Store(b, st)
	return st
}

func (b *Bot) countItem(id int) int {
	n := 0
	for _, it := range b.inv {
		if it.ItemID == id && !it.Equipped {
			n += it.Count
		}
	}
	return n
}

func (b *Bot) skillLv(id int) int {
	if s := b.skills[id]; s != nil {
		return s.Lv
	}
	return 0
}

// canBrew: an Alchemist that knows Pharmacy
func (b *Bot) canBrew() bool { return isAlchemistClass(b.Class) && b.skillLv(skPharmacy) > 0 }

// isBrewer: can brew and owns the Potion Creation Guide
func (b *Bot) isBrewer() bool { return b.canBrew() && b.countItem(itemPotionGuide) > 0 }

// nextRecipe: the first recipe whose materials are all in the bag
func (b *Bot) nextRecipe() *alchRecipe {
	for i := range pharmacyRecipes {
		r := &pharmacyRecipes[i]
		ok := true
		for _, m := range r.Mats {
			if b.countItem(m[0]) < m[1] {
				ok = false
				break
			}
		}
		if ok {
			return r
		}
	}
	return nil
}

// herbsToBrew: herbs in the bag that a recipe uses (Blue, White, Yellow)
func (b *Bot) herbsToBrew() int {
	return b.countItem(itemBlueHerb) + b.countItem(itemWhiteHerb) + b.countItem(itemYellowHerb)
}

func (b *Bot) restoreAlchGoal(st *alchState) {
	if st.goalSet && b.Goal.Set == "alchemy" {
		b.Goal, b.route, b.path = st.savedGoal, nil, nil
	}
	st.goalSet = false
}

// alchemyThink runs the guild alchemist's craft: brewing, and the trips that supply it. True while it owns the bot.
func (b *Bot) alchemyThink(gm *GameMap, moving bool, now time.Time) bool {
	if !b.canBrew() || b.Goal.Set == "api" || b.trip != nil || b.mkt != nil ||
		(b.vend != nil && b.vend.Opened) {
		return false
	}
	st := b.alchSt()
	// economy run 9: a member of a human's party (pinned) only leaves for the Alchemist Union errand (the guide, or
	// Medicine Bowls) - owner rule 3, a necessary build step. It never ran for pinned bots, so Silent rogue (Ayla's
	// Alchemist) sat on 108k "saving for the guide" with every zeny held back (gearReserve), buying nothing.
	if b.pin != nil && st.Stage != "dealer" {
		hasGuide := b.countItem(itemPotionGuide) > 0
		guideRun := !hasGuide && b.Zeny >= guideCost+3000 && now.After(st.DealerNext)
		bowlRun := hasGuide && b.countItem(itemMedicineBowl) == 0 && b.Zeny >= 2500 && b.herbsToBrew() >= 10 && now.After(st.DealerNext)
		brew := hasGuide && b.countItem(itemMedicineBowl) > 0 && now.After(st.NextBrew) && b.SP >= max(5, b.MaxSP/5) &&
			(townOf(b.MapName) != "" || b.Sitting) && b.nextRecipe() != nil && st.Stage == "" // it brews while the party rests
		if !guideRun && !bowlRun && !brew && st.Stage != "cast" && st.Stage != "make" {
			return false
		}
	}
	if b.isBrewer() {
		noteBrewer(b.charName())
	}
	// run 7: the dealer trip's own Kafra teleport (travel -> kafraPlan) must keep running: with "b.kafra != nil" in the
	// guard above, Blaze.mage booked the Kafra in Payon for Al De Baran and then stood "idle in payon" (goal mode idle,
	// nothing else drives a Kafra trip) with the guild's 103k
	if b.kafra != nil && st.Stage != "dealer" {
		return false
	}
	switch st.Stage {
	case "cast", "make":
		if now.Sub(st.At) < 6*time.Second {
			b.setState("brewing " + itemName(st.Item))
			return true
		}
		// the server never answered (no Medicine Bowl, not enough SP, skill failed): back off after a few
		st.Stage = ""
		st.Timeouts++
		if st.Timeouts >= 3 {
			st.Timeouts = 0
			st.Pause = now.Add(10 * time.Minute)
			st.Last = "pharmacy did not answer 3 times; pausing 10 min"
			b.event("info", "", "pharmacy: no answer from the server 3 times (bowls "+fmt.Sprint(b.countItem(itemMedicineBowl))+
				", sp "+fmt.Sprint(b.SP)+"), pausing")
		}
		return false
	case "dealer":
		return b.dealerThink(gm, moving, now, st)
	}
	if now.Before(st.Pause) || b.Dead || b.underAttack(now) || b.target != 0 {
		return false
	}
	p := partyOf(b)
	inTown := townOf(b.MapName) != ""
	hasGuide := b.countItem(itemPotionGuide) > 0
	bowls := b.countItem(itemMedicineBowl)

	// 1. brew: in town, alone, or while the party rests
	if hasGuide && bowls > 0 && now.After(st.NextBrew) && b.SP >= max(5, b.MaxSP/5) && (inTown || p == nil || b.Sitting) {
		if r := b.nextRecipe(); r != nil {
			if b.Sitting {
				b.wantSit = false
				go b.stand()
				st.NextBrew = now.Add(800 * time.Millisecond)
				return true
			}
			st.Stage, st.At, st.Item = "cast", now, r.Out
			b.target, b.path = 0, nil
			b.setState("brewing " + itemName(r.Out))
			lv := b.skillLv(skPharmacy)
			go b.useSkill(skPharmacy, lv, b.aid)
			return true
		}
	}
	// in a party: supplies wait for a real need. Exceptions (batch 13/14): the guide (the guild's 100k would otherwise
	// sit in the bag for as long as the party lasts), and a brewer that is OUT of Medicine Bowls or potion bottles -
	// one solo errand restocks for ~30+ brews (coordinator 05:58: alchemists had 0 bowls and 0 bottles).
	guideRun := !hasGuide && b.Zeny >= guideCost+3000 && now.After(st.DealerNext)
	bowlRun := hasGuide && bowls == 0 && b.Zeny >= 2500 && b.herbsToBrew() >= 10 && now.After(st.DealerNext)
	bottleRun := hasGuide && b.countItem(itemEmptyPotionBottle) == 0 && b.Zeny >= 500 && now.After(st.BottleNext) && b.trip == nil
	if p != nil && !guideRun && !bowlRun && !bottleRun {
		return false
	}
	if p != nil {
		typing := b.P.Typing
		line := pick("brb, alchemist supplies", "1 sec, need bottles for brewing", "brb, getting brewing stuff")
		if guideRun {
			line = pick("guild money is in, going to buy my potion guide in aldebaran, brb",
				"brb, getting my alchemist book in aldebaran", "got the 100k for the guide! back soon")
		}
		go b.partySay(styled(typing, line))
	}
	// 2. the Alchemist Union in Al De Baran: the guide once, Medicine Bowls when low
	if now.After(st.DealerNext) {
		switch {
		case !hasGuide && b.Zeny >= guideCost+3000:
			st.Dealer = "guide"
		case hasGuide && bowls < 5 && b.Zeny >= 2500 && b.herbsToBrew() >= 10:
			st.Dealer = "bowls"
		default:
			st.Dealer = ""
		}
		if st.Dealer != "" {
			st.Stage, st.At, st.DealerNPC = "dealer", now, 0
			st.savedGoal, st.goalSet = b.Goal, true
			// run 7: a member's supply trip is an errand (errand.go): the leader doesn't collect it, nobody waits
			if p != nil && b.errand == nil && !(p.Human == "" && p.Leader == b) {
				b.errand = &errand{Kind: "alchemy", Why: "alchemist supplies (" + st.Dealer + ")", Started: now}
				b.event("party", "", "errand: "+b.errand.Why+" (back soon) "+b.tripNumbers())
			}
			b.event("info", "", fmt.Sprintf("heading to the Alchemist Union in Al De Baran (%s, %dz)", st.Dealer, b.Zeny))
			return b.dealerThink(gm, moving, now, st)
		}
	}
	// 3. bottles and Scell from the Goods Merchant at Prontera's south gate
	if hasGuide && now.After(st.BottleNext) && b.Zeny >= 500 && b.needBottles() > 0 && b.trip == nil {
		b.alchSupplyTrip(st, now)
		return false // tripThink takes over
	}
	return false
}

// alchSupplyTrip: a shop trip to the Goods Merchant (Prontera south gate) for Empty Potion Bottles and Scell
// (town.go onDeal -> alchemyBuy). False when no shop sells bottles (b.mu held).
func (b *Bot) alchSupplyTrip(st *alchState, now time.Time) bool {
	st.BottleNext = now.Add(20 * time.Minute)
	var shop *Shop
	for _, s := range gearShops(itemEmptyPotionBottle) {
		if shop == nil || s.Map == "prontera" {
			shop = s
		}
	}
	if shop == nil {
		return false
	}
	town := townOf(shop.Map)
	if town == "" {
		town = nearestTown(shop.Map)
	}
	b.trip = &Trip{Stage: "go", Town: town, Shop: shop, Started: now, At: now, Why: "alchemy"}
	b.target, b.path, b.route = 0, nil, nil
	b.event("info", "", fmt.Sprintf("heading to %s for potion bottles / Scell (%d herbs to brew, %d bottles, %d Scell)",
		shop.Map, b.herbsToBrew(), b.countItem(itemEmptyPotionBottle), b.countItem(itemScell)))
	return true
}

// brewSupplyNeed (economy run 10, b.mu held): a brewer with herbs it can't brew for lack of bottles or Scell. In a party
// the bottle run only started at 0 bottles, and Scell never: Neko.fang (9 Blue Herbs, 27 bottles, 0 Scell) and
// Silent rogue (Ayla's Alchemist: guide, 24 bowls, 4 Blue Herbs, 0 bottles) could not brew a single potion. The
// party member's errand (errand.go, one per party) fetches both from Prontera's Goods Merchant.
func (b *Bot) brewSupplyNeed(now time.Time) bool {
	if !b.isBrewer() || b.countItem(itemMedicineBowl) == 0 || b.Zeny < 800 || b.trip != nil {
		return false
	}
	if v, ok := alchStates.Load(b); ok && now.Before(v.(*alchState).BottleNext) {
		return false
	}
	herbs, bottles := b.herbsToBrew(), b.countItem(itemEmptyPotionBottle)
	blue, white, yellow := b.countItem(itemBlueHerb), b.countItem(itemWhiteHerb), b.countItem(itemYellowHerb)
	scellShort := blue >= 4 && b.countItem(itemScell) == 0 && white+yellow < 5
	return herbs >= 4 && (bottles < min(herbs, 5) || scellShort)
}

// alchShopClose (run 7): a vendor alchemist's open shop blocks alchemyThink (vending owns the bot), so guild07's
// vendor-alchemist sat on 102.9k in its shop and could never fetch the guide nor brew. True when the shop should close
// for the guide errand, a brew session or a supply run (b.mu held).
func (b *Bot) alchShopClose(now time.Time) bool {
	if !b.canBrew() || b.pin != nil || b.Goal.Set == "api" {
		return false
	}
	st := b.alchSt()
	if now.Before(st.Pause) || now.Before(st.ShopNext) {
		return false
	}
	hasGuide := b.countItem(itemPotionGuide) > 0
	bowls := b.countItem(itemMedicineBowl)
	ok := false
	switch {
	case !hasGuide:
		ok = b.Zeny >= guideCost+3000 && now.After(st.DealerNext)
	case bowls > 0 && b.nextRecipe() != nil && b.herbsToBrew() >= 5 && b.SP >= max(5, b.MaxSP/5) && now.After(st.NextBrew):
		ok = true
	case bowls == 0 && b.Zeny >= 2500 && b.herbsToBrew() >= 10 && now.After(st.DealerNext):
		ok = true
	case b.needBottles() > 0 && b.Zeny >= 500 && b.herbsToBrew() >= 10 && now.After(st.BottleNext):
		ok = true
	}
	if ok {
		st.ShopNext = now.Add(20 * time.Minute)
	}
	return ok
}

// needBottles: Empty Potion Bottles still needed for the herbs in the bag
func (b *Bot) needBottles() int {
	// a stock of 30 even before the herbs come in (batch 14): brewing starts the moment herbs arrive
	return max(0, min(100, max(30, b.herbsToBrew()))-b.countItem(itemEmptyPotionBottle))
}

func (b *Bot) dealerThink(gm *GameMap, moving bool, now time.Time, st *alchState) bool {
	end := func(why string, retry time.Duration) bool {
		st.Stage, st.DealerNPC, st.DealerNext = "", 0, now.Add(retry)
		st.Last = why
		b.restoreAlchGoal(st)
		return false
	}
	if now.Sub(st.At) > 45*time.Minute {
		b.event("info", "", "gave up on the Alchemist Union trip (too long)")
		return end("Alchemist Union trip took too long", 30*time.Minute)
	}
	if b.underAttack(now) || b.target != 0 {
		b.hunt(gm, moving, now)
		if b.target != 0 || b.Sitting {
			return true
		}
	}
	if b.MapName != alcheDealerMap {
		if b.Goal.Map != alcheDealerMap || b.Goal.Set != "alchemy" {
			b.Goal, b.route, b.path = Goal{Mode: "idle", Map: alcheDealerMap, Set: "alchemy"}, nil, nil
		}
		b.travel(gm, moving)
		if b.Goal.Map != alcheDealerMap {
			return end("no route to "+alcheDealerMap, time.Hour)
		}
		b.setState("going to the Alchemist Union in Al De Baran (" + st.Dealer + ")")
		return true
	}
	b.setState("at the Alchemist Union (" + st.Dealer + ")")
	if dist(b.X, b.Y, alcheDealer[0], alcheDealer[1]) > 3 {
		// nav.go talkCell: the nearest reachable cell within 6 of the NPC (the cell beside it can be behind the counter)
		tx, ty, ok := alcheDealer[0], alcheDealer[1]-1, false
		if gm != nil {
			if cx, cy, found := talkCell(gm, b.X, b.Y, alcheDealer[0], alcheDealer[1]); found {
				tx, ty, ok = cx, cy, true
			}
		}
		if !(ok && b.X == tx && b.Y == ty) { // at the talk cell: rAthena talks from up to 15 cells
			if !moving && gm != nil {
				b.stepAlong(gm, tx, ty)
			}
			return true
		}
	}
	if st.DealerNPC == 0 {
		npc := b.npcAt(alcheDealer[0], alcheDealer[1])
		if npc == 0 {
			if now.Sub(st.At) > 44*time.Minute {
				return end("Guild Dealer not seen", time.Hour)
			}
			return true
		}
		if b.Sitting {
			go b.stand()
		}
		st.DealerNPC, st.At = npc, now.Add(-44*time.Minute) // 1 min left for the dialog
		go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	}
	return true
}

// onAlchemyDialog answers the Guild Dealer's dialog (b.mu held). Other dialogs are left alone.
func (b *Bot) onAlchemyDialog(op uint16, pk []byte) {
	v, ok := alchStates.Load(b)
	if !ok {
		return
	}
	st := v.(*alchState)
	if st.Stage != "dealer" || st.DealerNPC == 0 || len(pk) < 6 {
		return
	}
	switch op {
	case 0x00b5: // next
		go b.send(newPkt(0x00b9).u32(le.Uint32(pk[2:])).b)
	case 0x00b6: // close: the purchase is done (or refused)
		go b.send(newPkt(0x0146).u32(le.Uint32(pk[2:])).b)
		now := time.Now()
		if st.Dealer == "guide" && b.countItem(itemPotionGuide) > 0 {
			b.event("market", "", fmt.Sprintf("bought a Potion Creation Guide (%dz left)", b.Zeny))
		}
		// guide in the bag and still short of bowls: talk again for the bowls
		if b.countItem(itemPotionGuide) > 0 && b.countItem(itemMedicineBowl) < 5 && st.Dealer != "bowls-done" && b.Zeny >= 1000 {
			st.Dealer, st.DealerNPC = "bowls", 0
			return
		}
		st.Stage, st.DealerNPC, st.DealerNext = "", 0, now.Add(30*time.Minute)
		st.Last = fmt.Sprintf("Alchemist Union: guide %d, bowls %d", b.countItem(itemPotionGuide), b.countItem(itemMedicineBowl))
		b.restoreAlchGoal(st)
	case 0x00b7: // menu
		if len(pk) < 9 {
			return
		}
		id := le.Uint32(pk[4:])
		opts := strings.Split(cstr(pk[8:]), ":")
		choice := 255
		for i, o := range opts {
			o = strings.TrimSpace(o)
			switch {
			case st.Dealer == "guide" && (strings.HasPrefix(o, "Purchase a production manual") || o == "Potion Creation Guide" || o == "Purchase."),
				strings.HasPrefix(st.Dealer, "bowls") && (strings.HasPrefix(o, "Purchase materials") || strings.HasPrefix(o, "Medicine Bowl")):
				choice = i + 1
			}
			if choice != 255 {
				break
			}
		}
		go b.send(newPkt(0x00b8).u32(id).u8(byte(choice)).b)
		if choice == 255 {
			st.Last = "dealer menu without our option: " + strings.Join(opts, "|")
		}
	case 0x0142: // how many bowls: one per brew, enough for the herbs in the bag (weight 1 each)
		id := le.Uint32(pk[2:])
		n := max(10, min(150, b.herbsToBrew()+20, (b.Zeny-500)/8))
		if room := (b.MaxWeight*45/100 - b.Weight) / 10; b.MaxWeight > 0 && room < n {
			n = max(5, room)
		}
		st.Dealer = "bowls-done"
		go b.send(newPkt(0x0143).u32(id).u32(uint32(n)).b)
		b.event("info", "", fmt.Sprintf("buying %d Medicine Bowls", n))
	}
}

// onPharmacy handles the produce packets (b.mu held).
func (b *Bot) onPharmacy(op uint16, pk []byte) {
	v, ok := alchStates.Load(b)
	if !ok {
		return
	}
	st := v.(*alchState)
	now := time.Now()
	switch op {
	case 0x018d: // what can be made now: len, then 16-byte entries (itemId u32, material[3] u32)
		if st.Stage != "cast" {
			return
		}
		pick := 0
		var avail []int
		for o := 4; o+16 <= len(pk); o += 16 {
			id := int(le.Uint32(pk[o:]))
			avail = append(avail, id)
			if id == st.Item {
				pick = id
			}
		}
		if pick == 0 { // our pick isn't makeable: take the best recipe the server offers
			for _, r := range pharmacyRecipes {
				for _, id := range avail {
					if id == r.Out && pick == 0 {
						pick = id
					}
				}
			}
		}
		// itemId 0 when nothing fits: the server just closes the menu
		st.Stage, st.At, st.Item, st.Timeouts = "make", now, pick, 0
		go b.send(newPkt(0x018e).u32(uint32(pick)).u32(0).u32(0).u32(0).b)
		if pick == 0 {
			st.Stage, st.Pause = "", now.Add(5*time.Minute)
			st.Last = fmt.Sprintf("nothing brewable in the pharmacy list %v", avail)
		}
	case 0x018f: // result i16, itemId u32
		if st.Stage != "make" || len(pk) < 8 {
			return
		}
		res, id := int(int16(le.Uint16(pk[2:]))), int(le.Uint32(pk[4:]))
		st.Stage = ""
		st.NextBrew = now.Add(time.Duration(700+crand(900)) * time.Millisecond)
		if res == 0 || res == 2 {
			st.Brewed[id]++
			st.Last = "brewed " + itemName(id)
			if n := st.Brewed[id]; n == 1 || n%20 == 0 {
				b.event("market", "", fmt.Sprintf("brewed %s (%d this session)", itemName(id), n))
			}
		} else {
			st.Failed++
			st.Last = "failed to brew " + itemName(id)
		}
	}
}

// ---------------------------------------------------------------- economy hooks

// alchemyWant: what the brewer asks the market for (b.mu held): herbs for its best recipe, in any amount.
func (b *Bot) alchemyWant() *Want {
	// only with the guide (batch 13): without it nothing can be brewed, and buying herbs spent the guild's guide fund
	// (Silent rogue asked for 31 Blue Herbs at 73z while saving; non-pick Alchemists bought herbs they never use)
	if !b.isBrewer() {
		return nil
	}
	// economy run 10: Yellow Herbs too (owner: healing items first in the market). Blue and White Herbs are scarce (18
	// Blue / 1 White on offer at 14:55 vs 282 Yellow), and the want asked only for them, so brewers with the guide,
	// bowls and bottles stood idle (Blaze.mage, 36k zeny, 0 herbs). An herb nobody offers is skipped for the next one;
	// Yellow Potions (herb ~45z + bottle 10z + bowl 8z) sell at ~160z.
	for _, h := range []int{itemBlueHerb, itemWhiteHerb, itemYellowHerb} {
		have := b.countItem(h)
		if have >= 40 {
			continue
		}
		if h != itemYellowHerb && marketSupply(h) >= 0 && marketSupply(h) < 5 {
			continue
		}
		each := bidPrice(b.P, h)
		n := min(40-have, max(0, (b.Zeny-1000)/max(each, 1)))
		if n >= 5 {
			return &Want{ItemID: h, Count: n, Each: each, Why: "brewing", Partial: true}
		}
	}
	return nil
}

// brewedSpare: brewed potions beyond what the alchemist keeps (b.mu held)
func (b *Bot) brewedSpare() map[int]int {
	out := map[int]int{}
	if !b.canBrew() {
		return out
	}
	for id, keep := range brewedKeep {
		if n := b.countItem(id) - keep; n > 0 {
			out[id] = n
		}
	}
	return out
}

// spPotionWant: an SP user low on SP potions asks for Blue Potions (b.mu held). Members of a guild leader's party
// ask even when they can't pay much: their guild alchemist gives them some.
func (b *Bot) spPotionWant(leaderParty bool) *Want {
	if !b.usesSP() || b.canBrew() {
		return nil
	}
	have, target := b.spPotionCount(), b.spPotTarget()
	if target <= 0 || have >= max(1, target/2) {
		return nil
	}
	each := bidPrice(b.P, itemBluePotion)
	n := min(target-have, max(0, (b.Zeny-b.gearReserve())/3/max(each, 1)))
	if n < 1 && leaderParty {
		n = min(5, target-have)
	}
	if n < 1 {
		return nil
	}
	return &Want{ItemID: itemBluePotion, Count: n, Each: each, Why: "sp potions", Partial: true}
}

// ---------------------------------------------------------------- API (economy.go registers it)

type alchRow struct {
	Guild      string         `json:"guild"`
	GuildName  string         `json:"guild_name"`
	Char       string         `json:"alchemist"`
	Player     string         `json:"player"`
	Role       string         `json:"role"`
	Class      string         `json:"class"`
	Base       int            `json:"base_level"`
	Job        int            `json:"job_level"`
	Candidates int            `json:"candidates"`
	Online     bool           `json:"online"`
	Map        string         `json:"map,omitempty"`
	State      string         `json:"state,omitempty"`
	Zeny       int            `json:"zeny,omitempty"`
	Pharmacy   int            `json:"pharmacy"`
	Research   int            `json:"potion_research"`
	Guide      bool           `json:"has_guide"`
	Bowls      int            `json:"medicine_bowls"`
	Bottles    int            `json:"empty_potion_bottles"`
	Scell      int            `json:"scell"`
	Herbs      map[string]int `json:"herbs,omitempty"`
	Potions    map[string]int `json:"potions,omitempty"`
	Brewed     map[string]int `json:"brewed_session,omitempty"`
	Failed     int            `json:"failed_session"`
	Last       string         `json:"last,omitempty"`
	Next       string         `json:"next_step"`
	Since      time.Time      `json:"designated_since"`
}

func alchemistsReport() []alchRow {
	alchemists.Lock()
	list := make([]guildAlch, 0, len(alchemists.byGuild))
	for _, a := range alchemists.byGuild {
		list = append(list, *a)
	}
	alchemists.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Guild < list[j].Guild })
	var rows []alchRow
	for _, a := range list {
		r := alchRow{Guild: a.Guild, Char: a.Char, Role: a.Role, Candidates: a.Candidates, Since: a.Since,
			Class: classNames[a.Lv.Class], Base: a.Lv.Base, Job: a.Lv.Job}
		if g := guildByID(a.Guild); g != nil {
			r.GuildName = g.Name
		}
		b := a.Bot
		if b != nil {
			r.Player = b.P.Name
			b.mu.Lock()
			if b.Online && b.charName() == a.Char {
				r.Online, r.Map, r.State, r.Zeny = true, b.MapName, b.State, b.Zeny
				r.Class, r.Base, r.Job = classNames[b.Class], b.BaseLvOr(), b.JobLv
				r.Pharmacy, r.Research = b.skillLv(skPharmacy), b.skillLv(skPotionResearch)
				r.Guide, r.Bowls = b.countItem(itemPotionGuide) > 0, b.countItem(itemMedicineBowl)
				r.Bottles, r.Scell = b.countItem(itemEmptyPotionBottle), b.countItem(itemScell)
				r.Herbs = map[string]int{}
				for _, h := range []int{itemRedHerb, itemYellowHerb, itemWhiteHerb, itemBlueHerb} {
					if n := b.countItem(h); n > 0 {
						r.Herbs[itemName(h)] = n
					}
				}
				r.Potions = map[string]int{}
				for _, id := range []int{itemBluePotion, itemWhitePotion, itemYellowPotion} {
					if n := b.countItem(id); n > 0 {
						r.Potions[itemName(id)] = n
					}
				}
				if v, ok := alchStates.Load(b); ok {
					st := v.(*alchState)
					r.Brewed = map[string]int{}
					for id, n := range st.Brewed {
						r.Brewed[itemName(id)] = n
					}
					r.Failed, r.Last = st.Failed, st.Last
				}
				r.Next = b.alchNextStep()
			}
			b.mu.Unlock()
		}
		if !r.Online {
			r.Next = offlineNextStep(a.Lv)
		}
		rows = append(rows, r)
	}
	return rows
}

func offlineNextStep(l charLv) string {
	switch alchTier(l) {
	case 1:
		return fmt.Sprintf("offline; Novice job %d: becomes a Merchant at job 10", l.Job)
	case 2:
		return fmt.Sprintf("offline; Merchant job %d: becomes an Alchemist at job 40", l.Job)
	case 3:
		return "offline; Alchemist"
	}
	return "offline"
}

// alchNextStep: plain-English next step on the alchemist path (b.mu held)
func (b *Bot) alchNextStep() string {
	switch {
	case b.Class == 0 || b.Class == 4001:
		return fmt.Sprintf("Novice job %d: becomes a Merchant at job 10", b.JobLv)
	case b.Class == 5 || b.Class == 4006:
		return fmt.Sprintf("Merchant job %d: becomes an Alchemist at job 40", b.JobLv)
	case !isAlchemistClass(b.Class):
		return "not on the alchemist path"
	case b.skillLv(skPotionResearch) < 5 && b.skillLv(skPharmacy) == 0:
		return fmt.Sprintf("learning Potion Research (%d/5) before Pharmacy", b.skillLv(skPotionResearch))
	case b.skillLv(skPharmacy) == 0:
		return "learning Pharmacy"
	case b.countItem(itemPotionGuide) == 0:
		return fmt.Sprintf("saving for the Potion Creation Guide (%s of 100k) - guild members can help", fmtZ(b.Zeny))
	case b.countItem(itemMedicineBowl) == 0:
		return "needs Medicine Bowls (Alchemist Union, Al De Baran, 8z)"
	case b.herbsToBrew() == 0:
		return "needs herbs (Blue Herb + Scell for Blue Potions, White Herb for White Potions)"
	case b.countItem(itemEmptyPotionBottle) == 0:
		return "needs Empty Potion Bottles (Goods Merchant, Prontera south gate)"
	case b.countItem(itemBlueHerb) > 0 && b.countItem(itemScell) == 0 && b.countItem(itemWhiteHerb) == 0:
		return "needs Scell for Blue Potions (Goods Merchant, Prontera south gate)"
	}
	return "brewing"
}

func guildByID(id string) *GuildDef {
	for _, g := range guildDefs {
		if g.ID == id {
			return g
		}
	}
	return nil
}

func registerAlchemy(mux *http.ServeMux) {
	mux.HandleFunc("/api/alchemists", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"alchemists": alchemistsReport(), "potions": potionMarket()})
	})
}

// potionMarket: price standard, board and recent trades for potions and alchemy materials
func potionMarket() []map[string]interface{} {
	ids := []int{itemBluePotion, itemWhitePotion, itemYellowPotion, itemRedPotion, 533, 11502, 11624,
		itemBlueHerb, itemWhiteHerb, itemYellowHerb, itemRedHerb, itemScell, itemEmptyPotionBottle, itemMedicineBowl, itemPotionGuide}
	econ.mu.Lock()
	offers := map[int]offerRow{}
	for _, o := range econ.offers {
		offers[o.ItemID] = o
	}
	wants := map[int][2]int{}
	for _, w := range econ.wants {
		x := wants[w.ItemID]
		wants[w.ItemID] = [2]int{x[0] + 1, x[1] + w.Count}
	}
	stats := map[int]itemStat{}
	for id, st := range econ.stats {
		stats[id] = *st
	}
	econ.mu.Unlock()
	var out []map[string]interface{}
	for _, id := range ids {
		row := map[string]interface{}{"item_id": id, "item": itemName(id), "standard": priceOf(id),
			"npc_sells": len(gearShops(id)) > 0}
		if o, ok := offers[id]; ok {
			row["offered_qty"], row["sellers"], row["lowest_ask"] = o.Qty, o.Sellers, o.LowAsk
		}
		if w, ok := wants[id]; ok {
			row["wanted_by"], row["wanted_qty"] = w[0], w[1]
		}
		if st, ok := stats[id]; ok {
			row["trades"], row["last_each"], row["avg_each"] = st.N, st.Last, int(st.Sum/int64(max(st.N, 1)))
		}
		out = append(out, row)
	}
	return out
}

// giftNext: buyer char -> next time its guild alchemist may give it potions (guarded by econ.mu)
var giftNext = map[string]time.Time{}

func isHealPotionOrSP(id int) bool { return isHealPotion(id) || isSPPotion(id) }

// econWantAll: what a bot asks the market for (b.mu held): an alchemist's herbs first, then the usual wants
// (field potions, gear), then SP potions for SP users.
func (b *Bot) econWantAll(leaderParty bool) *Want {
	if w := b.guildPoolWant(); w != nil { // guildpool.go: the guild pooled for a boss item for this member
		return w
	}
	if w := b.alchemyWant(); w != nil {
		return w
	}
	if w := b.creamyWant(); w != nil { // guildstock.go: the Teleport card first (owner rule 3b)
		return w
	}
	if w := b.econWant(); w != nil {
		return w
	}
	if w := b.spPotionWant(leaderParty); w != nil {
		return w
	}
	return b.refineWant() // refine.go (batch 14): Phracon / Emveretarcon for the weapon's safe refines
}

// alchemySpare: herbs (everyone but an alchemist has no use for them: they are kept for the alchemists, never sold to
// NPCs) and an alchemist's brewed potions beyond its own stock (b.mu held)
func (b *Bot) alchemySpare() []holding {
	var out []holding
	if b.canBrew() {
		for id, n := range b.brewedSpare() {
			out = append(out, holding{ItemID: id, Count: n, Ask: askPrice(b.P, id)})
		}
		return out
	}
	for _, h := range []int{itemBlueHerb, itemWhiteHerb, itemYellowHerb} {
		if n := b.countItem(h); n > 0 {
			out = append(out, holding{ItemID: h, Count: n, Ask: askPrice(b.P, h)})
		}
	}
	return out
}

// alchemyBuy answers the Goods Merchant's buy list (b.mu held; town.go onDeal): Empty Potion Bottles for every herb
// to brew, Scell for the Blue Herbs, within the zeny and the 45% weight line.
func (b *Bot) alchemyBuy(pk []byte) {
	prices := map[int]int{}
	for o := 4; o+13 <= len(pk); o += 13 {
		prices[int(le.Uint32(pk[o+9:]))] = int(le.Uint32(pk[o+4:]))
	}
	budget := b.Zeny - 500
	room := 1 << 30
	if b.MaxWeight > 0 {
		room = (b.MaxWeight*45/100 - b.Weight) / 10 // bottles and Scell weigh 1 each
	}
	p := newPkt(0x00c8).u16(0)
	var got []string
	for _, x := range [][2]int{{itemScell, max(0, b.countItem(itemBlueHerb)-b.countItem(itemScell))}, {itemEmptyPotionBottle, b.needBottles()}} {
		pr, ok := prices[x[0]]
		if !ok || pr <= 0 || x[1] <= 0 {
			continue
		}
		n := max(0, min(x[1], budget/pr, room))
		if n == 0 {
			continue
		}
		budget -= n * pr
		room -= n
		p.u16(uint16(n)).u32(uint32(x[0]))
		got = append(got, fmt.Sprintf("%d %s", n, itemName(x[0])))
	}
	if len(got) > 0 {
		go b.send(p.varlen())
		b.event("info", "", "bought "+strings.Join(got, ", ")+" for brewing")
	}
	if t := b.trip; t != nil {
		t.Stage, t.At = "done", time.Now()
	}
	b.npcDeal = nil
}

// ---------------------------------------------------------------- guild fund for the Potion Creation Guide

// The guide costs 100,000z and bots hold 1-3k each (2026-11-14), so no alchemist could ever brew. The guild pools
// its money (owner: farmers fund the characters that can't; the guild leader manages guild resources): a guild mate
// on the same map with money to spare hands the guild's alchemist a share through the trade window.

var guildFundGifts int64 // completed guild-fund gifts (atomic)

var guildFundNext = struct {
	sync.Mutex
	m map[string]time.Time // donor char -> next time it may give
}{m: map[string]time.Time{}}

// needsGuide: this character is its guild's alchemist, close to (or already) an Alchemist, without the guide and
// short of its price (b.mu held).
func (b *Bot) needsGuide() bool {
	return b.guideSaving() && b.Zeny < guideCost+3000
}

// brewerFund: what a guild brewer with the guide keeps for its supply errands (the Al De Baran trip for Medicine
// Bowls needs 2,500z: alchemyThink bowlRun). Economy run 9: TheShadowsenpai bought the guide and stood at 988z with 0
// bowls and 30 herbs - nothing could ever be brewed; the guild tops a broke brewer up like it funded the guide.
const brewerFund = 4000

// brewerBroke: the guild's brewer (guide in the bag) is short of its supply money (b.mu held)
func (b *Bot) brewerBroke() bool {
	return b.isBrewer() && guildAlchemistChar(b.charName()) != "" && b.Zeny < brewerFund-1000 &&
		(b.countItem(itemMedicineBowl) < 5 || b.countItem(itemEmptyPotionBottle) < 10)
}

// fundTarget: the zeny guildFund raises this character to (b.mu held)
func (b *Bot) fundTarget() int {
	if b.needsGuide() {
		return guideCost + 3000
	}
	return brewerFund
}

// guideSaving: the guild alchemist holds (or saves) the guild's money for the guide - not gear money even once the
// 103k is in (run 7: at 103,280z Blaze.mage's gear_next showed a 34k Buster "affordable" on the way to the dealer)
func (b *Bot) guideSaving() bool {
	if guildAlchemistChar(b.charName()) == "" || b.countItem(itemPotionGuide) > 0 {
		return false
	}
	return isAlchemistClass(b.Class) || (family(b.Class) == famMerchant && b.JobLv >= 30)
}

// guildFund: pair each guide-saving alchemist with one free guild mate on its map who has money to spare. Called by
// econRound after the boards are updated (no econ.mu or bot lock held).
func guildFund(snaps []*econSnap, now time.Time) {
	used := map[*econSnap]bool{}
	for _, a := range snaps {
		if !a.needGuide || a.guild == "" || used[a] {
			continue
		}
		var best *econSnap
		guildFundNext.Lock()
		for _, d := range snaps {
			if d == a || used[d] || !(d.free || d.keeper && d.idle) || d.guild != a.guild || d.gift < 1000 || now.Before(guildFundNext.m[d.name]) ||
				d.mapN == a.mapN && !a.idle { // a trade needs the alchemist free; a mail doesn't
				continue
			}
			// a guild mate on the same map hands it over in a trade (no fee) and comes first; others mail it (batch 10)
			if best == nil || (d.mapN == a.mapN) != (best.mapN == a.mapN) && d.mapN == a.mapN ||
				(d.mapN == a.mapN) == (best.mapN == a.mapN) && d.gift > best.gift {
				best = d
			}
		}
		guildFundNext.Unlock()
		if best == nil {
			continue
		}
		// run 8: same map but far apart (or in another town district) -> mail: the trade's party-mate rule
		// (together.go, 20 cells) failed every far same-map gift ("party mate too far for a trade", Hana_queen ->
		// Blaze.mage on pay_fild09)
		byMail := best.mapN != a.mapN || dist(best.x, best.y, a.x, a.y) > togetherCells()
		// run 7: the last gap is rounded UP to a 300z gift - rounding down (and the 300z minimum) left Blaze.mage at
		// 102,919 and lilzensenpaiVend at 102,933 for good: the remaining 81z / 67z could never be given
		need := max((a.fundTo-a.zeny+99)/100*100, 300)
		amount := min(best.gift, 3000, need) / 100 * 100
		if byMail {
			// a mailing member keeps half of what it has above 1,500z for its own gear (owner: nobody is left a
			// weakling), and the 2% mail fee
			amount = min(best.gift/2, 3000, need) / 100 * 100
		}
		if amount < 300 {
			continue
		}
		if byMail {
			if mailGuildFund(best.b, best.name, a.name, amount) {
				used[a], used[best] = true, true
				guildFundNext.Lock()
				guildFundNext.m[best.name] = now.Add(60 * time.Minute)
				guildFundNext.Unlock()
			}
			continue
		}
		if startGuildFundGift(best.b, a.b, best.name, a.name, amount, now) {
			used[a], used[best] = true, true
			guildFundNext.Lock()
			guildFundNext.m[best.name] = now.Add(60 * time.Minute)
			guildFundNext.Unlock()
		}
	}
}

// mailGuildFund: a guild mate on another map mails its part of the guide money to the alchemist (mail.go, RODEX,
// 2% fee). 2026-11-14: same-map gifts only: 0 gifts in batch 9's first hour, the guild's richer members were never on
// the alchemist's map.
func mailGuildFund(donor *Bot, dName, rName string, amount int) bool {
	donor.mu.Lock()
	defer donor.mu.Unlock()
	if !donor.Online || donor.charName() != dName || donor.mkt != nil || donor.trip != nil || donor.kafra != nil ||
		donor.Dead || donor.Zeny < mailCost(amount)+1500 {
		return false
	}
	typing := donor.P.Typing
	return donor.mailZeny(rName, amount, "guild fund", "for the guild potions, good luck with the brewing", func(ok bool, note string) {
		if !ok {
			return
		}
		atomic.AddInt64(&guildFundGifts, 1)
		econCount("guild fund: mailed")
		donor.event("market", rName, fmt.Sprintf("mailed %dz to %s for the guild's Potion Creation Guide", amount, rName))
		go func() {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			donor.whisper(rName, styled(typing, pick(fmt.Sprintf("mailed u %dz for the brewing (guild fund)", amount),
				fmt.Sprintf("sent %dz by mail, guild money for ur alchemy", amount), "check ur mail, guild money for the potions")))
		}()
	})
}

// startGuildFundGift: like gearpool.go startGift, a market Deal with no item (the alchemist requests the trade, the
// donor puts the zeny in), told in a whisper instead of party chat.
func startGuildFundGift(donor, rcpt *Bot, dName, rName string, amount int, now time.Time) bool {
	econ.mu.Lock()
	econ.nextID++
	id := econ.nextID
	econ.mu.Unlock()
	d := &Deal{ID: id, Seller: rcpt, Buyer: donor, SellerName: rName, BuyerName: dName, Price: amount, Created: now,
		Deadline: now.Add(3 * time.Minute), Why: "guild fund"}
	take := func(b *Bot, name, partner string, isRcpt bool) bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.Online || b.charName() != name || b.mkt != nil || b.trip != nil || b.kafra != nil || b.tradeFrom != "" ||
			b.Dead || (!isRcpt && b.Zeny < amount) {
			return false
		}
		b.mkt = &marketSide{d: d, seller: isRcpt, me: name, partner: partner}
		b.target, b.path = 0, nil
		return true
	}
	if !take(rcpt, rName, dName, true) {
		return false
	}
	if !take(donor, dName, rName, false) {
		rcpt.mu.Lock()
		if rcpt.mkt != nil && rcpt.mkt.d == d {
			rcpt.mkt = nil
		}
		rcpt.mu.Unlock()
		return false
	}
	econ.mu.Lock()
	econ.deals = append(econ.deals, d)
	econ.mu.Unlock()
	donor.event("info", rName, fmt.Sprintf("giving %dz to %s (guild fund for brewing)", amount, rName))
	go func() {
		donor.mu.Lock()
		dt := donor.P.Typing
		donor.mu.Unlock()
		donor.whisper(rName, styled(dt, pick(fmt.Sprintf("hey, %dz guild money for the brewing, come grab it", amount),
			fmt.Sprintf("got %dz for ur alchemist book, trade me", amount))))
	}()
	return true
}
