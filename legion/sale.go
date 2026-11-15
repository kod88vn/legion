package main

// Selling surplus when not grinding: in RO everyone who isn't a merchant advertises with a chat room titled
// "S> <items>" in town (merchants vend - vend.go). A bot hanging out in town with valuable loot opens one.
//   CZ_CREATE_CHATROOM 0x00d5: len u16, limit u16, public u8, password[8], title[]
//   CZ_EXIT_ROOM       0x00e3

import (
	"sort"
	"strings"
)

// saleItems: the most valuable things in the bag worth advertising (cards first, then pricey Etc loot).
func (b *Bot) saleItems() []string {
	type it struct {
		name  string
		value int
	}
	var list []it
	for _, inv := range b.inv {
		x := items[inv.ItemID]
		if x == nil || inv.Count <= 0 || inv.Equipped {
			continue
		}
		v := x.Buy / 2 * inv.Count
		switch x.Type {
		case "Card":
			v += 100000
		case "Etc":
		case "Weapon", "Armor":
			if inv.Equipped {
				continue
			}
		default:
			continue
		}
		if v < 200 {
			continue
		}
		list = append(list, it{x.Name, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].value > list[j].value })
	var out []string
	for _, x := range list {
		out = append(out, x.name)
		if len(out) == 3 {
			break
		}
	}
	return out
}

func (b *Bot) openSaleRoom() {
	its := b.saleItems()
	if len(its) == 0 {
		return
	}
	title := "S> " + strings.Join(its, ", ")
	if b.rng.Float64() < 0.3 {
		title += " cheap"
	}
	if len(title) > 36 {
		title = title[:36]
	}
	b.saleRoom = title
	p := newPkt(0x00d5).u16(0).u16(uint16(5 + b.rng.Intn(10))).u8(1)
	p.b = append(p.b, make([]byte, 8)...) // no password
	p.b = append(p.b, []byte(title)...)
	go b.send(p.varlen())
	b.event("market", "", "opened sale room: "+title)
}

func (b *Bot) closeSaleRoom() {
	b.saleRoom = ""
	go b.send(newPkt(0x00e3).b)
}
