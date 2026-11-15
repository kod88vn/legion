package main

// humanwant.go (economy run 9). A human's "B> <item>" on #trade got no answer: Ayla's gear plan posts "B> Clip [1],
// paying up to 5k" and nothing in Legion responded (the owner may trade too). Real players answer a B> with a
// whisper. Now a #trade buy line from a human becomes a human want: on the next market rounds a bot holding the item
// (its sellable spare, or a vendor listing it) answers in a whisper after a natural pause, at its own asking price
// (pricing rules) - no more than the human's stated price. A field bot sells by mail (it mails the item, the buyer
// pays by mail; the payment is recognised when the seller collects zeny mail from that buyer); a vendor points the
// buyer to its shop at the south gate.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type humanWant struct {
	Buyer    string    `json:"buyer"`
	ItemID   int       `json:"item_id"`
	Item     string    `json:"item"`
	Max      int       `json:"max_price"` // 0 = no price given
	Line     string    `json:"line"`
	At       time.Time `json:"at"`
	Answered string    `json:"answered,omitempty"` // seller that answered
	AnswerAt time.Time `json:"answer_at,omitempty"`
}

type humanSale struct {
	Seller, Buyer string
	ItemID, Price int
	At            time.Time
	Paid          bool
}

var hwants = struct {
	sync.Mutex
	m     map[string]*humanWant // buyer|item id
	sales []*humanSale
}{m: map[string]*humanWant{}}

var (
	reBuyPrefix = regexp.MustCompile(`(?i)^\s*(b>|b ?>|wtb|buying|lf)\s*:?\s*`)
	rePrice     = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(kk|k|m|z)?\b`)
	reNoise     = regexp.MustCompile(`(?i)\b(pm( me)?|paying( up to)?|up to|for|each|ea|pls|please|anyone|cheap|offers?)\b`)
)

// parseBuyLine: a #trade buy line -> item id, max price (0 = none). ok=false when it isn't a buy line.
func parseBuyLine(text string) (id, maxP int, ok bool) {
	loc := reBuyPrefix.FindStringIndex(text)
	if loc == nil {
		return 0, 0, false
	}
	rest := text[loc[1]:]
	namePart := rest
	if i := strings.IndexAny(rest, ",;"); i > 0 {
		namePart = rest[:i]
		for _, m := range rePrice.FindAllStringSubmatch(rest[i:], -1) {
			if p := priceNum(m[1], m[2]); p > maxP {
				maxP = p
			}
		}
	} else if m := rePrice.FindAllStringSubmatchIndex(rest, -1); len(m) > 0 {
		// "B> Clip 5k": a trailing number with a unit is the price
		last := m[len(m)-1]
		if last[4] > 0 {
			unit := rest[last[4]:last[5]]
			if p := priceNum(rest[last[2]:last[3]], unit); p > 0 {
				maxP = p
				namePart = rest[:last[0]]
			}
		}
	}
	namePart = reNoise.ReplaceAllString(namePart, " ")
	namePart = strings.TrimSpace(namePart)
	if len(namePart) < 3 {
		return 0, 0, false
	}
	hits := findItems(namePart, 1)
	if len(hits) == 0 || hits[0].Score < 60 {
		return 0, 0, false
	}
	return hits[0].ID, maxP, true
}

func priceNum(num, unit string) int {
	f, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", "."), 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "k":
		f *= 1000
	case "kk", "m":
		f *= 1000000
	case "z", "":
		if strings.Contains(num, ",") && len(num) > 4 { // "5,000"
			f, _ = strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
		}
	}
	return int(f)
}

// noteHumanTradeLine (chatter.go onChannel): a human's line on a channel; a buy line becomes a human want.
func noteHumanTradeLine(from, text string) {
	if coreInstance() {
		return // the main instance answers (one answer per line)
	}
	id, maxP, ok := parseBuyLine(text)
	if !ok {
		return
	}
	hwants.Lock()
	defer hwants.Unlock()
	k := from + "|" + strconv.Itoa(id)
	if w := hwants.m[k]; w != nil && w.Answered != "" && time.Since(w.AnswerAt) < 20*time.Minute {
		return // already being served
	}
	hwants.m[k] = &humanWant{Buyer: from, ItemID: id, Item: itemName(id), Max: maxP, Line: text, At: time.Now()}
	econCount("human want: heard")
}

// humanWantRound (econRound, no lock held): answer open human wants from the snapshots.
func humanWantRound(snaps []*econSnap, now time.Time) {
	hwants.Lock()
	var open []*humanWant
	for k, w := range hwants.m {
		if now.Sub(w.At) > 45*time.Minute {
			delete(hwants.m, k)
			continue
		}
		if w.Answered == "" && now.Sub(w.At) > 15*time.Second { // a person takes a moment to read #trade
			open = append(open, w)
		}
	}
	hwants.Unlock()
	for _, w := range open {
		var best *econSnap
		bestP, shop := 0, false
		for _, s := range snaps {
			if strings.EqualFold(s.name, w.Buyer) {
				continue
			}
			for _, l := range s.shop {
				if l.ItemID == w.ItemID && l.Count > 0 && (w.Max == 0 || l.Price <= w.Max) && (best == nil || l.Price < bestP) {
					best, bestP, shop = s, l.Price, true
				}
			}
			if !s.free {
				continue
			}
			for _, h := range s.spare {
				if h.ItemID != w.ItemID || h.Count <= 0 {
					continue
				}
				p := h.Ask
				if p <= 0 {
					p = priceOf(w.ItemID)
				}
				if w.Max > 0 && p > w.Max {
					if w.Max*100 >= p*85 {
						p = w.Max // within 15% of its ask: it takes the offer
					} else {
						continue
					}
				}
				if p >= mailItemFee*3 && s.zeny >= mailItemFee && (best == nil || p < bestP) {
					best, bestP, shop = s, p, false
				}
			}
		}
		if best == nil {
			continue
		}
		hwants.Lock()
		if w.Answered != "" {
			hwants.Unlock()
			continue
		}
		w.Answered, w.AnswerAt = best.name, now
		hwants.Unlock()
		econCount("human want: answered")
		go answerHumanWant(best.b, best.name, w, bestP, shop)
	}
}

// answerHumanWant: the seller whispers the buyer after a natural pause; a field seller then mails the item.
func answerHumanWant(sb *Bot, sName string, w *humanWant, price int, shop bool) {
	time.Sleep(time.Duration(20+crand(40)) * time.Second)
	sb.mu.Lock()
	if !sb.Online || sb.charName() != sName {
		sb.mu.Unlock()
		hwants.Lock()
		w.Answered = ""
		hwants.Unlock()
		return
	}
	typing, mapN := sb.P.Typing, sb.MapName
	sb.mu.Unlock()
	item, pz := w.Item, fmtZ(price)
	if shop {
		sb.whisper(w.Buyer, styled(typing, pick(
			fmt.Sprintf("hey, saw ur B> %s. got one in my shop at prt south gate, %s", item, pz),
			fmt.Sprintf("i have %s in my shop (south gate prontera) for %s", item, pz),
			fmt.Sprintf("%s in my vend at prt, %s :)", item, pz))))
		econCount("human want: pointed to shop")
		return
	}
	sb.whisper(w.Buyer, styled(typing, pick(
		fmt.Sprintf("hey, i have %s. %s ok? can mail it to u", item, pz),
		fmt.Sprintf("got a %s for u, %s. im in %s, ill send it by mail", item, pz, mapN),
		fmt.Sprintf("selling %s %s, ill rodex it, pay by mail when u get it", item, pz))))
	time.Sleep(time.Duration(8+crand(8)) * time.Second)
	sb.mu.Lock()
	defer sb.mu.Unlock()
	idx := -1
	for i, it := range sb.inv {
		if it != nil && it.ItemID == w.ItemID && !it.Equipped && !it.Unidentified && it.Refine == 0 && it.Count > 0 {
			idx = i
			break
		}
	}
	if idx < 0 || !sb.Online || sb.Dead || sb.mailBusy() || sb.Zeny < mailItemFee || sb.tradeFrom != "" || sb.mkt != nil ||
		sb.charName() != sName {
		hwants.Lock()
		w.Answered = ""
		hwants.Unlock()
		return
	}
	buyer, itemID := w.Buyer, w.ItemID
	m := &mailOut{To: buyer, Title: item, Body: fmt.Sprintf("%s for %dz, pay by mail pls", item, price),
		Items: []int{idx}, Counts: map[int]int{idx: 1}, stage: 1, at: time.Now(),
		onDone: func(ok bool, note string) { // the seller's b.mu held
			if !ok {
				econCount("human want: item mail failed (" + note + ")")
				hwants.Lock()
				w.Answered = ""
				hwants.Unlock()
				return
			}
			econCount("human want: item mailed")
			sb.event("market", buyer, fmt.Sprintf("mailed %s to %s (asked %dz by mail)", item, buyer, price))
			hwants.Lock()
			hwants.sales = append(hwants.sales, &humanSale{Seller: sName, Buyer: buyer, ItemID: itemID, Price: price, At: time.Now()})
			if len(hwants.sales) > 100 {
				hwants.sales = hwants.sales[len(hwants.sales)-100:]
			}
			hwants.Unlock()
			go func() {
				time.Sleep(time.Duration(2+crand(4)) * time.Second)
				sb.whisper(buyer, styled(typing, pick("sent, check ur mail", "mailed it! "+pz+" when u can", "its in ur mailbox :)")))
			}()
		}}
	mailOuts.Store(sb, m)
	go sb.send(newPkt(0x0a08).str(buyer, 24).b)
}

// humanSalePaid (mail.go, the seller's b.mu held): zeny mail from a human buyer settles its open sale.
func humanSalePaid(b *Bot, seller, sender string, amount int) {
	hwants.Lock()
	var sale *humanSale
	for _, s := range hwants.sales {
		if !s.Paid && s.Seller == seller && strings.EqualFold(s.Buyer, sender) {
			sale = s
			break
		}
	}
	if sale != nil {
		sale.Paid = true
	}
	hwants.Unlock()
	if sale == nil {
		return
	}
	econCount("human want: paid")
	now := time.Now()
	econ.mu.Lock()
	econ.trades = append(econ.trades, Trade{T: now, Seller: seller, Buyer: sender, ItemID: sale.ItemID,
		Item: itemName(sale.ItemID), Count: 1, Price: sale.Price, Map: "(mail)"})
	if len(econ.trades) > 300 {
		econ.trades = econ.trades[len(econ.trades)-300:]
	}
	econ.mu.Unlock()
	b.earmarkZ = max(0, b.earmarkZ-max(amount, sale.Price)) // a payment, not a gift
	typing := b.P.Typing
	go func() {
		time.Sleep(time.Duration(2+crand(4)) * time.Second)
		b.whisper(sender, styled(typing, pick("got it, ty!", "payment received, thx :)", "ty, enjoy the "+itemName(sale.ItemID))))
	}()
}

// humanWantsReport: for /api/economy
func humanWantsReport() map[string]interface{} {
	hwants.Lock()
	defer hwants.Unlock()
	var ws []humanWant
	for _, w := range hwants.m {
		ws = append(ws, *w)
	}
	var ss []humanSale
	for _, s := range hwants.sales {
		ss = append(ss, *s)
	}
	return map[string]interface{}{"wants": ws, "sales": ss}
}
