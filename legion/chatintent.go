package main

// chatintent.go (redesign package X, 2026-11-14): POST /api/chat-intent lets the chat agent register what a human
// (or a messy conversation) meant - a need or an offer of an item - so the engine treats it like a bot's want or offer.
//
//	{"from": "Ayla", "kind": "need", "item": "Clip", "count": 1, "max_price": 5000, "note": "said in party"}
//	{"from": "Kitty", "kind": "offer", "item": "Poring Card", "price": 800}
//
// From a Legion character of this instance: a need becomes its market want (the matcher fills it as any want);
// an offer is only noted (bots list their own spares). From a character the other instance runs: forwarded there.
// From a human (acted on only with chat_jobs_on; kin prices also need exchange_tiers_on):
//   - need: a party member (of the human's party), guild mate or friend holding the item as a spare hands it over -
//     free for party / spare, else kin_discount_pct of market, by RODEX; the human pays a price only by its own mail
//     (humanwant.go sale record). Otherwise it becomes a market human want (humanwant.go), as if said on #trade.
//   - offer: a bot that wants the item (or a kin bot that would use it) answers in a whisper; the human hands it over
//     by its own action (a trade window with a party / guild mate, or mail).
// Humans are never charged or traded without their own action. Ayla keeps 10,000z: a price is quoted to her only if
// her purse (status.json) stays at or above the floor after paying it; with her purse unknown only gifts reach her.
// When main runs with a core instance, a human need is first offered to the core instance (Ayla's party), which claims
// it when one of the human's own party members holds a spare.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type chatIntent struct {
	ID       int64     `json:"id"`
	T        time.Time `json:"t"`
	From     string    `json:"from"`
	Kind     string    `json:"kind"` // need | offer
	ItemID   int       `json:"item_id"`
	Item     string    `json:"item"`
	Count    int       `json:"count"`
	MaxPrice int       `json:"max_price,omitempty"` // need: most it pays per unit (0 = not said)
	Price    int       `json:"price,omitempty"`     // offer: asked per unit (0 = not said)
	Note     string    `json:"note,omitempty"`
	Human    bool      `json:"human"`
	Guild    string    `json:"guild,omitempty"`    // the human's guild id (Starveil = human-guild), from status.json
	ZenyCap  int       `json:"zeny_cap,omitempty"` // Ayla: most she may pay (purse - 10,000); -1 none
	Status   string    `json:"status"`
	By       string    `json:"by,omitempty"`     // the bot that answered
	Answer   int       `json:"answer,omitempty"` // price quoted (total)
	Tier     string    `json:"tier,omitempty"`
	Done     bool      `json:"done"`
}

var intents = struct {
	sync.Mutex
	list   []*chatIntent
	nextID int64
}{}

const intentLife = 45 * time.Minute

func intentsReport() []chatIntent {
	intents.Lock()
	defer intents.Unlock()
	out := make([]chatIntent, 0, len(intents.list))
	for i := len(intents.list) - 1; i >= 0; i-- {
		out = append(out, *intents.list[i])
	}
	return out
}

type intentReq struct {
	From     string `json:"from"`
	Kind     string `json:"kind"`
	Item     any    `json:"item"`
	Count    int    `json:"count"`
	MaxPrice int    `json:"max_price"`
	Price    int    `json:"price"`
	Note     string `json:"note"`
}

// parseIntent: validates a request into an intent (pure apart from the item lookup).
func parseIntent(req intentReq, lookup func(string) (int, string, bool)) (*chatIntent, error) {
	from := strings.TrimSpace(req.From)
	if from == "" || len(from) > 24 {
		return nil, fmt.Errorf("from: a character name (1-24 characters)")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != "need" && kind != "offer" {
		return nil, fmt.Errorf("kind: need or offer")
	}
	q := ""
	switch v := req.Item.(type) {
	case string:
		q = strings.TrimSpace(v)
	case float64:
		q = strconv.Itoa(int(v))
	}
	if q == "" {
		return nil, fmt.Errorf("item: a name or an id")
	}
	id, name, ok := lookup(q)
	if !ok {
		return nil, fmt.Errorf("item %q not found", q)
	}
	n := req.Count
	if n <= 0 {
		n = 1
	}
	if n > 100 {
		return nil, fmt.Errorf("count: 1-100")
	}
	if req.MaxPrice < 0 || req.Price < 0 {
		return nil, fmt.Errorf("prices are >= 0")
	}
	note := req.Note
	if len(note) > 200 {
		note = note[:200]
	}
	return &chatIntent{From: from, Kind: kind, ItemID: id, Item: name, Count: n, MaxPrice: req.MaxPrice, Price: req.Price,
		Note: note, ZenyCap: -1}, nil
}

func intentLookup(q string) (int, string, bool) {
	hits := findItems(q, 1)
	if len(hits) == 0 || hits[0].Score < 60 {
		return 0, "", false
	}
	return hits[0].ID, hits[0].Name, true
}

// aylaCap: most a human may be asked to pay in total (-1 = no cap). Ayla: purse - 10,000 (0 when unknown/below).
func aylaCap(name string, known bool, zeny int) int {
	if !isAyla(name) {
		return -1
	}
	if !known {
		return 0
	}
	return max(0, zeny-aylaZenyFloor)
}

func registerChatIntent(mux *http.ServeMux, bots []*Bot) {
	mux.HandleFunc("/api/chat-intent", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, map[string]any{"intents": intentsReport()})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST {from, kind: need|offer, item, count?, max_price?, price?, note?}", http.StatusMethodNotAllowed)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req intentReq
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		in, err := parseIntent(req, intentLookup)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		local := r.URL.Query().Get("local") == "1"
		writeJSON(w, handleIntent(in, raw, local, time.Now()))
	})
}

// handleIntent (API handler, no lock held): routes one intent.
func handleIntent(in *chatIntent, raw []byte, local bool, now time.Time) map[string]any {
	in.T = now
	if b := botByCharName(in.From); b != nil {
		return intentFromBot(b, in, now)
	}
	if allChars[in.From] {
		// a character the other instance runs
		if base := coreAPIURL(); base != "" && !local {
			if resp, err := coreClient.Post(base+"/api/chat-intent?local=1", "application/json", bytes.NewReader(raw)); err == nil {
				defer resp.Body.Close()
				var out map[string]any
				if json.NewDecoder(resp.Body).Decode(&out) == nil {
					out["forwarded"] = "core"
					return out
				}
			}
		}
		return map[string]any{"ok": false, "error": "that character is run by the other instance"}
	}
	in.Human = true
	if st, ok := humanStatusOf(in.From); ok { // file read: no lock held here
		if st.Guild != "" && isHumanGuild(st.Guild) {
			in.Guild = humanGuildID
		}
		in.ZenyCap = aylaCap(in.From, true, st.Zeny)
	} else {
		in.ZenyCap = aylaCap(in.From, false, 0)
	}
	if coreInstance() && local {
		// main asks the core instance first: claim only a need one of the human's own party members can fill
		if in.Kind != "need" || !exchangeOn() || !chatJobsOn() || !coreHasPartySpare(in) {
			return map[string]any{"ok": true, "claimed": false}
		}
		in.Status = "claimed by the human's party"
		intentAdd(in)
		return map[string]any{"ok": true, "claimed": true, "id": in.ID}
	}
	if in.Kind == "need" && !local {
		if base := coreAPIURL(); base != "" {
			if resp, err := coreClient.Post(base+"/api/chat-intent?local=1", "application/json", bytes.NewReader(raw)); err == nil {
				var out map[string]any
				if json.NewDecoder(resp.Body).Decode(&out) == nil && out["claimed"] == true {
					resp.Body.Close()
					out["forwarded"] = "core"
					return out
				}
				resp.Body.Close()
			}
		}
	}
	in.Status = "open"
	if !chatJobsOn() {
		in.Status = "noted (chat_jobs_on is off: not acted on)"
		in.Done = true
	}
	intentAdd(in)
	return map[string]any{"ok": true, "id": in.ID, "status": in.Status, "human": true, "zeny_cap": in.ZenyCap}
}

func intentAdd(in *chatIntent) {
	intents.Lock()
	defer intents.Unlock()
	intents.nextID++
	in.ID = intents.nextID
	intents.list = append(intents.list, in)
	if len(intents.list) > 100 {
		intents.list = intents.list[len(intents.list)-100:]
	}
	exCount("intent: " + in.Kind)
}

// intentFromBot: a bot's need becomes its market want; an offer is noted.
func intentFromBot(b *Bot, in *chatIntent, now time.Time) map[string]any {
	in.Human = false
	if in.Kind == "offer" {
		in.Status, in.Done = "noted (its spares are listed by the engine)", true
		intentAdd(in)
		return map[string]any{"ok": true, "id": in.ID, "status": in.Status}
	}
	std := priceOf(in.ItemID) // econ.mu: not held
	b.mu.Lock()
	online, mapN, name := b.Online, b.MapName, b.charName()
	each := bidPrice(b.P, in.ItemID)
	b.mu.Unlock()
	if !online || name != in.From {
		return map[string]any{"ok": false, "error": in.From + " is not online"}
	}
	if in.MaxPrice > 0 {
		each = min(each, in.MaxPrice)
	}
	econ.mu.Lock()
	if old := econ.wants[name]; old != nil && old.InDeal {
		econ.mu.Unlock()
		in.Status, in.Done = "busy: its current want is in a deal", true
		intentAdd(in)
		return map[string]any{"ok": false, "id": in.ID, "status": in.Status}
	}
	econ.wants[name] = &Want{Buyer: b, Name: name, Map: mapN, ItemID: in.ItemID, Item: in.Item, Count: in.Count, Each: each,
		Why: "said so in chat", Created: now, Standard: std, Partial: in.Count > 1}
	econ.nextWant[name] = now.Add(econWantLife) // its own needs are not re-checked over it meanwhile
	econ.mu.Unlock()
	in.Status, in.Done = fmt.Sprintf("want registered at %dz each", each), true
	intentAdd(in)
	return map[string]any{"ok": true, "id": in.ID, "status": in.Status}
}

// coreHasPartySpare (core instance, no lock held): one of the human's own party members holds the item as a spare.
func coreHasPartySpare(in *chatIntent) bool {
	var ms []*Bot
	parties.Lock()
	for _, p := range parties.byName {
		if strings.EqualFold(p.Human, in.From) {
			ms = append(ms, p.Members...)
		}
	}
	parties.Unlock()
	for _, m := range ms {
		m.mu.Lock()
		ok := m.Online && m.countItem(in.ItemID) >= in.Count && !m.ownUse(items[in.ItemID])
		m.mu.Unlock()
		if ok {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- the round

// exchangeRound (econRound, no lock held, before humanWantRound): acts on open human intents.
func exchangeRound(snaps []*econSnap, now time.Time) {
	if !chatJobsOn() {
		return
	}
	intents.Lock()
	var open []*chatIntent
	for _, in := range intents.list {
		if in.Done {
			continue
		}
		if now.Sub(in.T) > intentLife {
			in.Done, in.Status = true, "expired"
			continue
		}
		if in.Human && now.Sub(in.T) > 10*time.Second { // a person takes a moment
			open = append(open, in)
		}
	}
	intents.Unlock()
	for _, in := range open {
		if in.Kind == "need" {
			intentNeed(snaps, in, now)
		} else {
			intentOffer(snaps, in, now)
		}
	}
}

// humanRel: relation of bot snap s to a human (its party is led by the human, its guild, or a friendship).
func humanRel(s *econSnap, in *chatIntent) int {
	exch.Lock()
	r := exch.rel[s.name]
	exch.Unlock()
	if r.partyHuman != "" && strings.EqualFold(r.partyHuman, in.From) {
		return relParty
	}
	return exRelation("", "", s.guild, in.Guild, FriendScore(s.name, in.From) >= friendTierFriend)
}

func intentNeed(snaps []*econSnap, in *chatIntent, now time.Time) {
	claimed := strings.HasPrefix(in.Status, "claimed")
	if exchangeOn() {
		var best *econSnap
		bestRel, bestEach := relStranger, 0
		for _, s := range snaps {
			if !s.free || s.courier {
				continue
			}
			has := false
			for _, h := range s.spare {
				if h.ItemID == in.ItemID && h.Count >= in.Count {
					has = true
				}
			}
			if !has {
				continue
			}
			rel := humanRel(s, in)
			if rel == relStranger || claimed && rel != relParty {
				continue
			}
			exch.Lock()
			spare := exch.spare[s.name][in.ItemID]
			var ok bool
			exch.favours[s.name], ok = exFavourOK(exch.favours[s.name], exGiftsPerHour(), now)
			exch.Unlock()
			if !ok {
				continue
			}
			each, _ := exTier(rel, spare, priceOf(in.ItemID), kinDiscountPct())
			total := each * in.Count
			if in.MaxPrice > 0 && each > in.MaxPrice || in.ZenyCap >= 0 && total > in.ZenyCap && total > 0 {
				continue // Ayla's floor / the human's own limit
			}
			if best == nil || rel > bestRel || rel == bestRel && each < bestEach {
				best, bestRel, bestEach = s, rel, each
			}
		}
		if best != nil {
			intentGive(best, in, bestRel, bestEach, now)
			return
		}
	}
	if claimed {
		return // the core instance only serves the human's party
	}
	// the market: a human want as if said on #trade (humanwant.go answers it at the market price)
	maxEach := in.MaxPrice
	if in.ZenyCap >= 0 {
		if in.ZenyCap == 0 {
			intents.Lock()
			in.Status = "waiting: gifts only (Ayla keeps 10,000z)"
			intents.Unlock()
			return
		}
		capEach := in.ZenyCap / max(in.Count, 1)
		if maxEach == 0 || capEach < maxEach {
			maxEach = capEach
		}
	}
	if maxEach == 0 && in.ZenyCap >= 0 {
		return
	}
	hwants.Lock()
	k := in.From + "|" + strconv.Itoa(in.ItemID)
	if w := hwants.m[k]; w == nil || w.Answered == "" {
		hwants.m[k] = &humanWant{Buyer: in.From, ItemID: in.ItemID, Item: in.Item, Max: maxEach, Line: "(chat) " + in.Note, At: now.Add(-15 * time.Second)}
	}
	hwants.Unlock()
	intents.Lock()
	in.Done, in.Status = true, "on the market (human want)"
	intents.Unlock()
	exCount("intent: need to market")
}

// intentGive: a kin bot hands a human what it needs: free by mail, or at the kin price via the human-want sale.
func intentGive(s *econSnap, in *chatIntent, rel, each int, now time.Time) {
	_, tier := exTier(rel, each == 0, 0, kinDiscountPct())
	if rel == relParty {
		tier = tierParty
	}
	total := each * in.Count
	market := priceOf(in.ItemID) // econ.mu: before exch.mu
	exch.Lock()
	exch.favours[s.name] = append(exch.favours[s.name], now)
	exCountLocked("matched: " + tier)
	exRecordLocked(exRecord{T: now, Giver: s.name, Taker: in.From, ItemID: in.ItemID, Item: in.Item, Count: in.Count, Tier: tier,
		Rel: relNames[rel], Price: total, Market: market, Status: "matched", Human: true})
	exch.Unlock()
	intents.Lock()
	in.Done, in.By, in.Answer, in.Tier, in.Status = true, s.name, total, tier, "answered by "+s.name
	intents.Unlock()
	if each > 0 {
		// the usual human sale: whisper, mail the item, the human pays by its own mail
		hw := &humanWant{Buyer: in.From, ItemID: in.ItemID, Item: in.Item, Max: each, Line: "(chat) " + in.Note, At: now,
			Answered: s.name, AnswerAt: now}
		hwants.Lock()
		hwants.m[in.From+"|"+strconv.Itoa(in.ItemID)] = hw
		hwants.Unlock()
		go answerHumanWant(s.b, s.name, hw, each, false)
		return
	}
	go intentGiftMail(s.b, s.name, in, tier)
}

// intentGiftMail: mail one gift (count units) to the human after a natural pause, with a whisper via a chat job.
func intentGiftMail(sb *Bot, sName string, in *chatIntent, tier string) {
	time.Sleep(time.Duration(15+crand(20)) * time.Second)
	sb.mu.Lock()
	defer sb.mu.Unlock()
	idx := -1
	for i, it := range sb.inv {
		if it != nil && it.ItemID == in.ItemID && !it.Equipped && !it.Unidentified && it.Count >= in.Count {
			idx = i
			break
		}
	}
	if idx < 0 || !sb.Online || sb.Dead || sb.mailBusy() || sb.Zeny < mailItemFee || sb.tradeFrom != "" || sb.mkt != nil ||
		sb.charName() != sName {
		exCount("intent: gift not sent (busy)")
		return
	}
	typing := sb.P.Typing
	to, item := in.From, in.Item
	m := &mailOut{To: to, Title: item, Body: "for you, no charge", Items: []int{idx}, Counts: map[int]int{idx: in.Count}, stage: 1,
		at: time.Now(), onDone: func(ok bool, note string) { // the giver's b.mu held
			if !ok {
				exCount("intent: gift mail failed")
				return
			}
			exCount("done: " + tier)
			sb.event("market", to, fmt.Sprintf("mailed %s to %s as a gift (%s)", item, to, tier))
			line := styled(typing, pick("sent u my "+item+", its free :)", "check ur mail, "+item+" for u", "mailed u the "+item+", no charge"))
			go EnqueueChatJob(ChatJobSpec{Kind: ChatKindOffer, Channel: "whisper", To: to, Speaker: sb, Hearers: []string{to},
				Facts:    map[string]any{"item": item, "taker": to, "tier": tier, "price": 0},
				Template: []ChatLine{{Speaker: sName, Channel: "whisper", To: to, Text: line, DelaySec: 2}}})
		}}
	mailOuts.Store(sb, m)
	go sb.send(newPkt(0x0a08).str(to, 24).b)
}

// intentOffer: a human offers an item; a bot that wants it (or a kin bot that would use it) answers in a whisper.
// The hand-over is the human's own action.
func intentOffer(snaps []*econSnap, in *chatIntent, now time.Time) {
	type cand struct {
		s    *econSnap
		bid  int
		rel  int
		want bool
	}
	econ.mu.Lock()
	wanting := map[string]int{}
	for n, w := range econ.wants {
		if !w.InDeal && wantAccepts(w, in.ItemID) {
			wanting[n] = w.Each
		}
	}
	econ.mu.Unlock()
	var best *cand
	for _, s := range snaps {
		if !s.free {
			continue
		}
		rel := humanRel(s, in)
		bid, want := wanting[s.name]
		if !want {
			if rel == relStranger || !lootCanUse(s.b, items[in.ItemID], now) {
				continue
			}
		}
		if want && in.Price > 0 && in.Price > bid && rel == relStranger {
			continue // a stranger's price above what it bids
		}
		c := &cand{s: s, bid: bid, rel: rel, want: want}
		if best == nil || c.rel > best.rel || c.rel == best.rel && c.want && !best.want {
			best = c
		}
	}
	if best == nil {
		return
	}
	s := best.s
	s.b.mu.Lock()
	typing := s.b.P.Typing
	s.b.mu.Unlock()
	item := in.Item
	var line string
	switch {
	case best.rel != relStranger:
		line = pick("oh i could use that "+item+"! trade me when ur near?", "me! i need "+item+", can u send it?", item+"? id love that, ty")
	default:
		p := best.bid
		if in.Price > 0 {
			p = min(p, in.Price)
		}
		line = pick(fmt.Sprintf("hey, ill take ur %s for %s", item, fmtZ(p)), fmt.Sprintf("%s for %s? mail it and i pay when it comes", item, fmtZ(p)))
		intents.Lock()
		in.Answer = p * in.Count
		intents.Unlock()
	}
	intents.Lock()
	in.Done, in.By, in.Status = true, s.name, "answered by "+s.name+" (the hand-over is the human's move)"
	intents.Unlock()
	exCount("intent: offer answered")
	go EnqueueChatJob(ChatJobSpec{Kind: ChatKindAccept, Channel: "whisper", To: in.From, Speaker: s.b, Hearers: []string{in.From},
		Facts:    map[string]any{"item": item, "offered_by": in.From, "relation": relNames[best.rel], "bid_each": best.bid, "asked_each": in.Price},
		Template: []ChatLine{{Speaker: s.name, Channel: "whisper", To: in.From, Text: styled(typing, line), DelaySec: 5 + crand(10)}}})
}
