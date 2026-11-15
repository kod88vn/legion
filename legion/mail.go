package main

// mail.go: RODEX, the in-game mail, for zeny between guild mates who are not on the same map (economy batch 10).
//
// 2026-11-14: the guild fund for the 100k Potion Creation Guide only moved zeny between a donor and the alchemist on
// the same map (a trade window), and the guild's few members with money to spare were almost never there: 0 fund gifts
// in the first hour of batch 9. Real players mail zeny to a guild mate; rAthena charges a 2% fee (mail_zeny_fee 2),
// so the money is the members' own, moved at a cost: no free money.
//
// Packets (PACKETVER 20210107, rAthena clif.cpp):
//   sender: CZ_REQ_OPEN_WRITE_MAIL 0x0a08 <receiver>.24B -> ZC_ACK_OPEN_WRITE_MAIL 0x0a12 <receiver>.24B <ok>.B
//           CZ_REQ_WRITE_MAIL2 0x0a6e <len>.W <receiver>.24B <sender>.24B <zeny>.Q <titleLen>.W <textLen>.W
//           <receiverCharID>.L <title> <text>                    -> ZC_ACK_WRITE_MAIL 0x09ed <result>.B (0 = sent)
//           CZ_REQ_CANCEL_WRITE_MAIL 0x0a03: always afterwards - while "writing a mail" the server refuses item use
//           (potions!) and trade requests (pc_useitem / clif_parse_TradeRequest check state.mail_writing).
//   receiver: ZC_NOTIFY_UNREADMAIL 0x09e7 <unread>.B (also on map load) -> CZ_REQ_REFRESH_MAIL_LIST 0x0ac1
//           <mailID>.Q 16B -> ZC_ACK_MAIL_LIST 0x0ac2 <len>.W <?>.B {<type>.B <id>.Q <read>.B <mailType>.B
//           <sender>.24B <expire>.L <titleLen>.W <title>}*; CZ_REQ_ZENY_FROM_MAIL 0x09f1 <id>.Q <tab>.B ->
//           ZC_ACK_ZENY_FROM_MAIL 0x09f2 <id>.Q <tab>.B <result>.B (0 = ok); CZ_REQ_DELETE_MAIL 0x09f5 <tab>.B <id>.Q.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const mailZenyFeePct = 2 // rathena conf/battle/misc.conf mail_zeny_fee

type mailOut struct {
	To, Title, Body string
	Amount          int
	Items           []int       // parcel.go: inventory indices to attach (one whole stack each)
	Counts          map[int]int // mailtrade.go: index -> amount to attach (less than the whole stack)
	acks, attached  int
	stage           int // 1 open-write sent, 3 attaching items, 2 mail sent
	at              time.Time
	done            bool
	onDone          func(ok bool, note string) // called with the sender's b.mu held
}

var (
	mailOuts   sync.Map // *Bot -> *mailOut (one mail at a time per bot)
	mailChecks sync.Map // *Bot -> time.Time: last mailbox refresh
	mailInbox  sync.Map // *Bot -> map[uint64]string: mails with zeny we asked for -> sender
	mailSent   int64    // zeny mails sent (atomic), /api/economy
	mailZenyIn int64    // zeny mails collected
	mailFailed int64

	// what the senders put in, so the receiver can tell its diary how much it got ("receiver|sender" -> zeny)
	mailAmounts = struct {
		sync.Mutex
		m map[string][]int
	}{m: map[string][]int{}}
)

// mailCost: what sending amount by mail takes from the purse (amount + fee)
func mailCost(amount int) int { return amount + amount*mailZenyFeePct/100 }

// mailBusy: a mail is being written (b.mu held)
func (b *Bot) mailBusy() bool {
	if v, ok := mailOuts.Load(b); ok {
		m := v.(*mailOut)
		return !m.done && time.Since(m.at) < 20*time.Second
	}
	return false
}

// mailZeny starts mailing amount to a character (b.mu held). False if it can't start now.
func (b *Bot) mailZeny(to string, amount int, title, body string, onDone func(ok bool, note string)) bool {
	if b.mailBusy() || amount <= 0 || b.Zeny < mailCost(amount) || to == "" || to == b.charName() || b.tradeFrom != "" ||
		!aylaSpendOK(b.charName(), b.Zeny, mailCost(amount)) { // G12: Ayla's own mail never takes her under 10,000z
		return false
	}
	if len(title) > 30 {
		title = title[:30]
	}
	if title == "" {
		title = "hi"
	}
	m := &mailOut{To: to, Title: title, Body: body, Amount: amount, stage: 1, at: time.Now(), onDone: onDone}
	mailOuts.Store(b, m)
	go b.send(newPkt(0x0a08).str(to, 24).b)
	return true
}

func (b *Bot) endMail(m *mailOut, ok bool, note string) {
	if m.done {
		return
	}
	m.done = true
	go b.send(newPkt(0x0a03).b) // leave the "writing a mail" state (or potions can't be used)
	if ok && len(m.Items) > 0 {
		econCount("mail items: sent")
	} else if !ok && len(m.Items) > 0 {
		econCount("mail items: failed (" + note + ")")
	} else if ok {
		atomic.AddInt64(&mailSent, 1)
		econCount("mail zeny: sent")
		go friendNote(b.charName(), m.To, fkMail) // friends.go
		mailAmounts.Lock()
		k := m.To + "|" + b.charName()
		mailAmounts.m[k] = append(mailAmounts.m[k], m.Amount)
		mailAmounts.Unlock()
	} else {
		atomic.AddInt64(&mailFailed, 1)
		econCount("mail zeny: failed (" + note + ")")
	}
	if m.onDone != nil {
		m.onDone(ok, note)
	}
}

// mailThink: time out a mail the server never answered (b.mu held; called from the economy round)
func (b *Bot) mailThink(now time.Time) {
	if v, ok := mailOuts.Load(b); ok {
		m := v.(*mailOut)
		if !m.done && now.Sub(m.at) > 15*time.Second {
			b.endMail(m, false, "no answer")
		}
	}
}

// onMail: RODEX packets (b.mu held)
func (b *Bot) onMail(op uint16, pk []byte) {
	switch op {
	case 0x0a12: // open-write answer
		v, ok := mailOuts.Load(b)
		if !ok || len(pk) < 27 {
			return
		}
		m := v.(*mailOut)
		if m.done || m.stage != 1 {
			return
		}
		if pk[26] == 0 {
			b.endMail(m, false, "can't write now")
			return
		}
		if b.Zeny < mailCost(m.Amount) {
			b.endMail(m, false, "not enough zeny")
			return
		}
		if len(m.Items) > 0 { // parcel.go: attach the items first (0x0a04 -> 0x0a05 each), then send
			m.stage, m.at = 3, time.Now()
			var reqs [][2]int
			for _, idx := range m.Items {
				if it := b.inv[idx]; it != nil && it.Count > 0 {
					n := it.Count
					if c, ok := m.Counts[idx]; ok && c > 0 && c < n {
						n = c
					}
					reqs = append(reqs, [2]int{idx, n})
				}
			}
			m.Items = m.Items[:0]
			for _, r := range reqs {
				m.Items = append(m.Items, r[0])
			}
			if len(reqs) == 0 {
				b.endMail(m, false, "items gone")
				return
			}
			go func() {
				for _, r := range reqs {
					b.send(newPkt(0x0a04).u16(uint16(r[0])).u16(uint16(r[1])).b)
					time.Sleep(250 * time.Millisecond)
				}
			}()
			return
		}
		b.sendMailNow(m)
	case 0x0a05, 0x0b3f: // ZC_ACK_ADD_ITEM_RODEX: result 0 = attached (parcel.go). PACKETVER 20211103 sends 0x0b3f
		// (64 bytes; 0x0a05 is the pre-20200723 header): batch 17 waited for 0x0a05 and every parcel ended "no answer"
		v, ok := mailOuts.Load(b)
		if !ok || len(pk) < 3 {
			return
		}
		m := v.(*mailOut)
		if m.done || m.stage != 3 {
			return
		}
		m.acks++
		if pk[2] == 0 {
			m.attached++
		}
		if m.acks >= len(m.Items) {
			if m.attached == 0 {
				b.endMail(m, false, "items refused")
				return
			}
			b.sendMailNow(m)
		}
	case 0x09f4: // item taken out of a mail (parcel.go)
		if len(pk) >= 12 {
			if pk[11] == 0 {
				econCount("mail items: collected")
				what := "took a parcel of gear out of my mail for the shop"
				if b.charRole() != "vendor" {
					what = "took the item I bought out of my mail" // mailtrade.go
				}
				b.event("market", "", what)
				id := le.Uint64(pk[2:])
				go b.send(newPkt(0x09f5).u8(0).u32(uint32(id)).u32(uint32(id >> 32)).b)
			} else {
				econCount("mail items: collect failed")
			}
		}
	case 0x09ed: // mail sent?
		v, ok := mailOuts.Load(b)
		if !ok || len(pk) < 3 {
			return
		}
		m := v.(*mailOut)
		if m.done || m.stage != 2 {
			return
		}
		notes := map[byte]string{1: "failed", 2: "daily limit", 3: "item", 4: "no such character", 5: "blocked"}
		if pk[2] == 0 {
			b.endMail(m, true, "")
		} else {
			b.endMail(m, false, notes[pk[2]])
		}
	case 0x09e7: // unread mail
		if len(pk) >= 3 && pk[2] != 0 {
			b.checkMail(time.Now())
		}
	case 0x0ac2: // the mailbox: take the zeny out of every mail that has some
		if len(pk) < 5 {
			return
		}
		want := map[uint64]string{}
		var del, items []uint64
		for off := 5; off+41 <= len(pk); {
			id, read, mtype := le.Uint64(pk[off+1:]), pk[off+9] != 0, pk[off+10]
			sender := cstr(pk[off+11 : off+35])
			off += 41 + int(le.Uint16(pk[off+39:]))
			if mtype&0x2 != 0 {
				want[id] = sender
			} else if mtype&0x4 != 0 {
				if b.weightPct() < 60 {
					items = append(items, id) // parcel.go: gear for the shop
				}
			} else if mtype&0x4 == 0 && read {
				del = append(del, id) // read, no zeny, no item: tidy the box (30 mails max)
			}
		}
		if len(want) > 0 {
			mailInbox.Store(b, want)
		}
		go func() {
			for id := range want {
				b.send(newPkt(0x09f1).u32(uint32(id)).u32(uint32(id >> 32)).u8(0).b)
				time.Sleep(300 * time.Millisecond)
			}
			for _, id := range items {
				b.send(newPkt(0x09f3).u32(uint32(id)).u32(uint32(id >> 32)).u8(0).b)
				time.Sleep(300 * time.Millisecond)
			}
			for _, id := range del {
				b.send(newPkt(0x09f5).u8(0).u32(uint32(id)).u32(uint32(id >> 32)).b)
				time.Sleep(300 * time.Millisecond)
			}
		}()
	case 0x09f2: // zeny taken out of a mail
		if len(pk) < 12 {
			return
		}
		id := le.Uint64(pk[2:])
		sender := ""
		if v, ok := mailInbox.Load(b); ok {
			sender = v.(map[uint64]string)[id]
		}
		if pk[11] != 0 {
			econCount("mail zeny: collect failed")
			return
		}
		atomic.AddInt64(&mailZenyIn, 1)
		econCount("mail zeny: collected")
		amount := 0
		mailAmounts.Lock()
		k := b.charName() + "|" + sender
		if l := mailAmounts.m[k]; len(l) > 0 {
			amount, mailAmounts.m[k] = l[0], l[1:]
		}
		mailAmounts.Unlock()
		humanSalePaid(b, b.charName(), sender, amount) // humanwant.go: a human paying for an item mailed to it
		if amount > 0 {
			// economy run 9: a gift is for something (Ayla's money for a friend's piece was handed on to guild pools
			// within 30 s) - earmarked for 90 min or until spent on gear (giftableZeny)
			b.earmarkZ += amount
			b.earmarkUntil = time.Now().Add(90 * time.Minute)
			b.event("market", sender, fmt.Sprintf("got %dz by mail from %s", amount, sender))
		} else {
			b.event("market", sender, fmt.Sprintf("got zeny by mail from %s", sender))
		}
		go b.send(newPkt(0x09f5).u8(0).u32(uint32(id)).u32(uint32(id >> 32)).b) // empty now: delete it
	}
}

// checkMail asks for the mailbox (b.mu held), at most every 20 s.
func (b *Bot) checkMail(now time.Time) {
	if v, ok := mailChecks.Load(b); ok && now.Sub(v.(time.Time)) < 20*time.Second {
		return
	}
	mailChecks.Store(b, now)
	go b.send(newPkt(0x0ac1).u32(0).u32(0).bytes(make([]byte, 16)).b)
}

// sendMailNow: CZ_REQ_SEND_MAIL with the zeny (and the items attached before) (b.mu held)
func (b *Bot) sendMailNow(m *mailOut) {
	m.stage, m.at = 2, time.Now()
	title, body := append([]byte(m.Title), 0), append([]byte(m.Body), 0)
	p := newPkt(0x0a6e).u16(0).str(m.To, 24).str(b.charName(), 24).
		u32(uint32(m.Amount)).u32(0).u16(uint16(len(title))).u16(uint16(len(body))).u32(0).bytes(title).bytes(body)
	go b.send(p.varlen())
}
