package main

// mailtrade.go (economy run 7): a sale by RODEX between players who are far apart. Most bots play in parties and a
// party member only trades within 10 cells (partyTradeOK) or with a solo seller who walks over, so 15:30 wants for
// cards and gear priced right (every card bid >= the lowest ask: Snake Card 2.8-3.2k vs 2.65k) missed on distance:
// "too far / party" 8k vs "price too high" 201. Real players sell far-away goods by mail. The owner lowered RODEX's
// attachment fee to 100z per item (10:22), so for goods worth 1,000z+ the seller mails the item (100z) and the buyer
// mails the money (+2% fee) as soon as the item has gone out. Gated by tuning mail_trades (or mail_trade_test_bots).

import (
	"fmt"
	"strings"
	"time"
)

const (
	mailTradeMin = 1000 // total price: below this the 100z fee and 2% are too much of the deal
	mailItemFee  = 100  // rathena conf/import/battle_conf.txt mail_attachment_price (owner, 2026-11-14 10:22)
)

// mailTradeOn: mail trades enabled for this pair (tuning)
func mailTradeOn(seller, buyer string) bool {
	tn := tune()
	if tn.MailTrades {
		return true
	}
	set := nameSet(tn.MailTradeTestBots)
	return set[strings.ToLower(seller)] || set[strings.ToLower(buyer)]
}

// mailTradeOK: the matcher may settle this sale by mail (econ.mu held; snapshots only)
func mailTradeOK(s, buyer *econSnap, itemID, count, price int) bool {
	if s.courier || s.stocker || s.brewer || price < mailTradeMin || buyer.zeny < mailCost(price) || s.zeny < mailItemFee {
		return false
	}
	x := items[itemID]
	if x == nil || !(x.Type == "Card" || x.Type == "Weapon" || x.Type == "Armor" || valuableEtc[itemID]) {
		return false
	}
	return mailTradeOn(s.name, buyer.name)
}

// startMailTrade: the seller mails the item; once it has gone out, the buyer mails the money. No locks held.
func startMailTrade(s, buyer *econSnap, w *Want, itemID, count, price int, now time.Time) bool {
	sb, bb := s.b, buyer.b
	sName, bName := s.name, buyer.name
	item := itemName(itemID)
	if count > 1 {
		item = fmt.Sprintf("%dx %s", count, item)
	}
	release := func() {
		econ.mu.Lock()
		if cur := econ.wants[w.Name]; cur == w {
			cur.InDeal = false
		}
		econ.noPair[sName+"|"+bName] = time.Now().Add(30 * time.Minute)
		econ.mu.Unlock()
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	idx := -1
	for i, it := range sb.inv {
		if it != nil && it.ItemID == itemID && it.Count >= count && !it.Equipped && !it.Unidentified &&
			it.Refine == 0 && (idx < 0 || it.Count < sb.inv[idx].Count) {
			idx = i
		}
	}
	if idx < 0 || !sb.Online || sb.Dead || sb.mailBusy() || sb.Zeny < mailItemFee || sb.tradeFrom != "" || sb.mkt != nil ||
		sb.charName() != sName {
		return false
	}
	m := &mailOut{To: bName, Title: "your " + itemName(itemID), Body: fmt.Sprintf("%s for %dz, as agreed", item, price),
		Items: []int{idx}, Counts: map[int]int{idx: count}, stage: 1, at: now,
		onDone: func(ok bool, note string) { // the seller's b.mu held
			if !ok {
				econCount("mail trade: item mail failed (" + note + ")")
				go release()
				return
			}
			econCount("mail trade: item sent")
			sb.event("market", bName, fmt.Sprintf("mailed %s to %s (sold for %dz, paid by mail)", item, bName, price))
			typing := sb.P.Typing
			go func() {
				time.Sleep(time.Duration(1+crand(3)) * time.Second)
				sb.whisper(bName, styled(typing, pick("sent u the "+itemName(itemID)+" by mail", "check ur mail, "+itemName(itemID)+" sent",
					"mailed it, "+fmt.Sprint(price)+"z when u can")))
			}()
			go payMailTrade(bb, sb, bName, sName, w, itemID, count, price, item)
		}}
	mailOuts.Store(sb, m)
	go sb.send(newPkt(0x0a08).str(bName, 24).b)
	econCount("mail trade: started")
	return true
}

// payMailTrade: the buyer mails the agreed price to the seller (retries while it is busy for up to 3 min). No locks.
func payMailTrade(bb, sb *Bot, bName, sName string, w *Want, itemID, count, price int, item string) {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		bb.mu.Lock()
		if !bb.Online || bb.charName() != bName {
			bb.mu.Unlock()
			break
		}
		started := bb.mailZeny(sName, price, "payment: "+itemName(itemID), item+", thanks!", func(ok bool, note string) {
			if !ok { // the buyer's b.mu held
				econCount("mail trade: payment failed (" + note + ")")
				bb.event("info", sName, fmt.Sprintf("could not mail %dz to %s for the %s (%s)", price, sName, item, note))
				return
			}
			econCount("mail trade: done")
			noteMarketFlow("market_sell", price, itemID, count) // marketflow.go (run 11)
			bb.event("market", sName, fmt.Sprintf("bought %s from %s by mail for %dz", item, sName, price))
			noteBought(bb, itemID) // refine.go: not offered again for an hour
			now := time.Now()
			econ.mu.Lock()
			marketSold(sb.P.Name, itemID) // market.go: the markdown starts over
			econ.trades = append(econ.trades, Trade{T: now, Seller: sName, Buyer: bName, ItemID: itemID,
				Item: itemName(itemID), Count: count, Price: price, Map: "(mail)"})
			if len(econ.trades) > 300 {
				econ.trades = econ.trades[len(econ.trades)-300:]
			}
			each := price / max(count, 1)
			st := econ.stats[itemID]
			if st == nil {
				st = &itemStat{Min: each, Max: each}
				econ.stats[itemID] = st
			}
			st.N++
			st.Sum += int64(each)
			st.Min, st.Max, st.Last = min(st.Min, each), max(st.Max, each), each
			if cur := econ.wants[bName]; cur != nil && wantAccepts(cur, itemID) {
				delete(econ.wants, bName)
			}
			econ.nextWant[bName] = now.Add(time.Minute)
			econ.mu.Unlock()
			bb.checkMail(now) // collect the item (mail.go 0x0ac2)
		})
		bb.mu.Unlock()
		if started {
			return
		}
	}
	econCount("mail trade: payment not sent")
	bb.mu.Lock()
	bb.event("info", sName, fmt.Sprintf("owes %s %dz for the %s (mail busy)", sName, price, item))
	bb.mu.Unlock()
}
