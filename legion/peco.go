package main

// peco.go: Knights and Crusaders ride (owner 2026-11-14: "the knight class ... does not use any ride"). iRO Wiki
// Knight/Crusader: Peco Peco Riding (+25% move speed; with Cavalier Mastery 5 the ASPD penalty is gone) is the class
// standard; every Knight plan learns Riding 1 + Cavalier Mastery 5, but no bot ever rented the bird (QA: Hana.fox1,
// Knight lv74 with both skills, on foot).
//
// Stock rAthena breeders (npc/merchants/renters.txt, loaded in Renewal too): Knights at prontera 55,350 (2,500z),
// Crusaders at prontera 232,318 (3,500z). Dialog: mes -> next -> menu "Rent Peco Peco:Cancel" (answer 1) -> setriding
// -> close. The ride is a saved option (0x0229 effectState bit 0x20) and lasts until dismounted, so this is a
// one-time errand (the option is saved with the character, so it survives logins): a party's next town run goes to
// Prontera while a member wants a Peco (ai.go partyRunTown), and a solo rider's next trip goes there (town.go).

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const optRiding = 0x20 // rAthena OPTION_RIDING

type pecoTask struct {
	Stage   string // walk, talk
	Started time.Time
	At      time.Time
	X, Y    int
}

var (
	pecoTasks   sync.Map     // *Bot -> *pecoTask
	pecoRented  atomic.Int64 // rentals seen through (riding bit came back after the visit)
	pecoFunded  atomic.Int64 // zeny mails from guild / party mates for a rider's Peco
	katarFunded atomic.Int64 // katarFundRound: zeny mails toward an Assassin's katar
	// pecoFundNext: rider char -> next time a mate may mail it Peco money
	pecoFundNext = struct {
		sync.Mutex
		m map[string]time.Time
	}{m: map[string]time.Time{}}
)

// pecoBreeder: where this class rents its Peco Peco and the price (ok=false: not a riding class).
func pecoBreeder(class int) (x, y, price int, ok bool) {
	c := class
	if b, t := transToSecond[c]; t {
		c = b
	}
	switch c {
	case 7: // Knight / Lord Knight
		return 55, 350, 2500, true
	case 14: // Crusader / Paladin
		return 232, 318, 3500, true
	}
	return 0, 0, 0, false
}

const (
	// pecoReserve: what a rider keeps above the rental for a few potions (owner 2026-11-14: the Peco is the first
	// purchase; the old price + 1,000z rule let 4 of 125 Knights qualify)
	pecoReserve = 200
	// pecoRetry: the rest after a failed visit / trip before the next try (was 2-6 h: one miss parked a rider all day)
	pecoRetry = 40 * time.Minute
)

// pecoNeed (b.mu held): a Knight/Crusader with Peco Peco Riding, known not to ride. Its zeny is the Peco's until it
// rides (builds.go gearReserve), unless the breeder refused it (peco:holdoff - something is off: don't block gear).
func (b *Bot) pecoNeed() bool {
	_, _, _, rides := pecoBreeder(b.Class)
	if !rides || !b.optionSeen || b.option&optRiding != 0 || time.Now().Before(b.buffAt["peco:holdoff"]) {
		return false
	}
	si := b.skills[63]
	return si != nil && si.Lv > 0
}

// pecoShortfall (b.mu held): zeny the rider still lacks for its Peco (rental + reserve); 0 = none / not a rider.
func (b *Bot) pecoShortfall() int {
	if !b.pecoNeed() {
		return 0
	}
	_, _, price, _ := pecoBreeder(b.Class)
	return max(0, price+pecoReserve-b.Zeny)
}

// wantsPeco (b.mu held): a Knight/Crusader with Riding, known not to ride, with the rental in its purse.
func (b *Bot) wantsPeco(now time.Time) (x, y int, ok bool) {
	x, y, price, _ := pecoBreeder(b.Class)
	if !b.pecoNeed() || now.Before(b.buffAt["peco:wait"]) {
		return 0, 0, false
	}
	// Riding 1 is all rAthena needs. Cavalier Mastery only lowers the ASPD penalty (the plan keeps raising it); the
	// owner wants every Knight mounted (2026-11-14: "why the knight is not riding peco is still a puzzle"): the old
	// "Cavalier 5 first" rule left most Knights lv66+ on foot (Cavalier 0-2).
	if b.Zeny < price+pecoReserve {
		return 0, 0, false
	}
	return x, y, true
}

// pecoThink (b.mu held, from think): run the breeder visit; true = it owns the bot this tick.
func (b *Bot) pecoThink(gm *GameMap, moving bool, now time.Time) bool {
	v, ok := pecoTasks.Load(b)
	if !ok {
		if b.MapName != "prontera" || b.mkt != nil || b.trip != nil || b.kafra != nil || b.Dead || b.Sitting {
			return false
		}
		x, y, want := b.wantsPeco(now)
		if !want {
			return false
		}
		if p := partyOf(b); p != nil && p.Human == "" && p.rallyOn() && b.errand == nil {
			return false // a bot party is gathering: not now (a human-led party's rallies are frequent and the visit
			// is short - Echobunny stood in Prontera with 14k and Riding and never rented)
		}
		t := &pecoTask{Stage: "walk", Started: now, X: x, Y: y}
		pecoTasks.Store(b, t)
		b.event("info", "", "going to the Peco Peco breeder to rent a ride")
		v = t
	}
	t := v.(*pecoTask)
	end := func(why string, wait time.Duration) bool {
		pecoTasks.Delete(b)
		b.buffAt["peco:wait"] = now.Add(wait)
		if why != "" {
			b.event("info", "", "Peco Peco: "+why)
		}
		return false
	}
	if b.option&optRiding != 0 {
		b.pecoFails = 0
		pecoRented.Add(1)
		b.remember("rented a Peco Peco in Prontera")
		return end("riding now", 10*time.Minute)
	}
	if b.MapName != "prontera" || b.Dead || now.Sub(t.Started) > 4*time.Minute {
		return end("gave up (left town / took too long)", pecoRetry)
	}
	if b.mkt != nil || b.trip != nil || b.kafra != nil {
		return false
	}
	switch t.Stage {
	case "talk":
		if now.Sub(t.At) > 10*time.Second {
			if b.dialogNPC != 0 {
				go b.send(newPkt(0x0146).u32(b.dialogNPC).b)
				b.dialogNPC = 0
			}
			if b.pecoFails++; b.pecoFails >= 3 {
				// three talks in a row and no ride: something is off (money, skill) - stop holding the gear money
				b.pecoFails = 0
				b.buffAt["peco:holdoff"] = now.Add(6 * time.Hour)
			}
			return end("the breeder did not rent one", pecoRetry)
		}
		b.setState("renting a Peco Peco")
		return true
	}
	tx, ty, ok := t.X, t.Y-1, false
	if gm != nil {
		tx, ty, ok = talkCell(gm, b.X, b.Y, t.X, t.Y)
	}
	if !ok {
		return end("can't reach the breeder", 2*pecoRetry)
	}
	if dist(b.X, b.Y, t.X, t.Y) > 3 && !(b.X == tx && b.Y == ty) {
		if !moving && gm != nil {
			b.stepAlong(gm, tx, ty)
		}
		b.setState("going to the Peco Peco breeder")
		return true
	}
	if moving {
		return true
	}
	npc := b.npcAt(t.X, t.Y)
	if npc == 0 {
		if now.Sub(t.Started) > 3*time.Minute {
			return end("breeder not seen", 2*pecoRetry)
		}
		return true
	}
	t.Stage, t.At = "talk", now
	go b.send(newPkt(0x0090).u32(npc).u8(1).b)
	return true
}

// pecoMenu: the answer to the breeder's menu (1 = rent) while we talk to it; 0 = not ours (b.mu held).
func (b *Bot) pecoMenu() byte {
	if v, ok := pecoTasks.Load(b); ok && v.(*pecoTask).Stage == "talk" {
		return 1
	}
	return 0
}

// pecoTown (b.mu held): "prontera" when this rider should take its next town trip there to rent its Peco.
func (b *Bot) pecoTown(now time.Time) string {
	if _, _, ok := b.wantsPeco(now); ok && b.MapName != "prontera" {
		return "prontera"
	}
	return ""
}

// pecoSaving (b.mu held): a Knight/Crusader with Riding, known not to ride, still short of the rental (price +
// pecoReserve): its zeny is saved for the bird - other gear purchases wait (builds.go gearHold). Run 4: 0 riders, only
// 12 of 105 Knights / 4 of 81 Crusaders held the money while gear runs kept spending it.
func (b *Bot) pecoSaving() bool {
	return b.pecoShortfall() > 0
}

// pecoFundRound (econRound, no lock held): owner 2026-11-14 - "guild/party funds help a member who can't afford it".
// Each rider short of its Peco gets the shortfall mailed by its richest free guild or party mate that has money to
// spare (keeps 2,000z, never a member saving for its own Peco / core weapon). One mail per rider per 20 min, per
// donor per 30 min.
func pecoFundRound(snaps []*econSnap, now time.Time) {
	mateFundRound(snaps, now, func(s *econSnap) int { return s.pecoShort }, false, mailPecoFund)
}

// katarFundRound (econRound, no lock held): the same guild / party help for an Assassin with Sonic Blow and no katar
// (coreweapon.go: the katar is its savings goal; the Jur is 19,500z, far above most purses). Mates mail part of it
// (half of what they hold above 2,000z, at most 10,000z a mail) until the purse covers the katar + coreReserve.
func katarFundRound(snaps []*econSnap, now time.Time) {
	mateFundRound(snaps, now, func(s *econSnap) int { return s.katarShort }, true, mailKatarFund)
}

// mateFundRound: pair each member short of a build step (short > 0) with its richest free guild / party mate that has
// money to spare, and mail it the shortfall (partial: part of it). fundNext holds the per-member / per-donor pace.
func mateFundRound(snaps []*econSnap, now time.Time, short func(*econSnap) int, partial bool,
	send func(donor *Bot, dName, rName string, amount int) bool) {
	parties := map[*econSnap]*Party{}
	for _, s := range snaps {
		parties[s] = partyOf(s.b)
	}
	used := map[*econSnap]bool{}
	for _, r := range snaps {
		if short(r) <= 0 || used[r] {
			continue
		}
		pecoFundNext.Lock()
		wait := now.Before(pecoFundNext.m[r.name])
		pecoFundNext.Unlock()
		if wait {
			continue
		}
		need := max((short(r)+99)/100*100, 300)
		var best *econSnap
		guildFundNext.Lock()
		for _, d := range snaps {
			if d == r || used[d] || !d.free || d.pecoNeed || d.coreNeed || d.needGuide || now.Before(guildFundNext.m[d.name]) {
				continue
			}
			mate := r.guild != "" && d.guild == r.guild || parties[r] != nil && parties[d] == parties[r]
			if !mate || !partial && d.zeny < mailCost(need)+2000 || partial && d.zeny < 2600 {
				continue
			}
			if best == nil || d.zeny > best.zeny {
				best = d
			}
		}
		guildFundNext.Unlock()
		if best == nil {
			continue
		}
		amount := need
		if partial {
			amount = min(need, 10000, (best.zeny-2000)/2/100*100)
			if amount < 300 {
				continue
			}
		}
		if send(best.b, best.name, r.name, amount) {
			used[r], used[best] = true, true
			pecoFundNext.Lock()
			pecoFundNext.m[r.name] = now.Add(20 * time.Minute)
			pecoFundNext.Unlock()
			guildFundNext.Lock()
			guildFundNext.m[best.name] = now.Add(30 * time.Minute)
			guildFundNext.Unlock()
		}
	}
}

// mailKatarFund: a guild / party mate mails an Assassin part of its katar money (mail.go, 2% fee).
func mailKatarFund(donor *Bot, dName, rName string, amount int) bool {
	donor.mu.Lock()
	defer donor.mu.Unlock()
	if !donor.Online || donor.charName() != dName || donor.mkt != nil || donor.trip != nil || donor.kafra != nil ||
		donor.Dead || donor.Zeny < mailCost(amount)+2000 {
		return false
	}
	typing := donor.P.Typing
	return donor.mailZeny(rName, amount, "for your katar", "an assassin needs a katar, good luck", func(ok bool, note string) {
		if !ok {
			return
		}
		katarFunded.Add(1)
		econCount("katar fund: mailed")
		donor.event("market", rName, fmt.Sprintf("mailed %dz to %s toward a katar", amount, rName))
		go func() {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			donor.whisper(rName, styled(typing, pick(fmt.Sprintf("mailed u %dz for ur katar", amount),
				"check ur mail, money for a katar", fmt.Sprintf("sent %dz, get that katar", amount))))
		}()
	})
}

// mailPecoFund: a guild / party mate mails a rider the zeny its Peco Peco still needs (mail.go, 2% fee).
func mailPecoFund(donor *Bot, dName, rName string, amount int) bool {
	donor.mu.Lock()
	defer donor.mu.Unlock()
	if !donor.Online || donor.charName() != dName || donor.mkt != nil || donor.trip != nil || donor.kafra != nil ||
		donor.Dead || donor.Zeny < mailCost(amount)+2000 {
		return false
	}
	typing := donor.P.Typing
	return donor.mailZeny(rName, amount, "for your peco", "go rent your peco peco, a knight should ride", func(ok bool, note string) {
		if !ok {
			return
		}
		pecoFunded.Add(1)
		econCount("peco fund: mailed")
		donor.event("market", rName, fmt.Sprintf("mailed %dz to %s for a Peco Peco", amount, rName))
		go func() {
			time.Sleep(time.Duration(2+crand(3)) * time.Second)
			donor.whisper(rName, styled(typing, pick(fmt.Sprintf("mailed u %dz, go get ur peco", amount),
				"check ur mail, money for ur peco", fmt.Sprintf("sent %dz for the peco, ride it", amount))))
		}()
	})
}
