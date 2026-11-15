D = 'G:/RO/server/legion/'


def ed(p, pairs):
    s = open(D + p, encoding='utf-8', newline='').read()
    for a, b in pairs:
        assert s.count(a) == 1, (p, a)
        s = s.replace(a, b)
    open(D + p, 'w', encoding='utf-8', newline='').write(s)


# (a) a bot party leader that flees to town by Butterfly Wing takes the party with it (party town run + chat)
ed('danger.go', [
    ("""	case town && b.escItemOK(itemButterflyWing, now):
		b.useEscapeItem(itemButterflyWing, now, "losing: "+why)
		b.setState("fled to town (" + why + ")")""",
     """	case town && b.escItemOK(itemButterflyWing, now):
		b.useEscapeItem(itemButterflyWing, now, "losing: "+why)
		b.leaderFledCall(now) // together.go: a leader leaving the map takes the party along (never strand a member)
		b.setState("fled to town (" + why + ")")"""),
    ("""		b.useEscapeItem(itemButterflyWing, now, "losing, no Fly Wing: "+why)
		b.setState("fled to town (" + why + ")")""",
     """		b.useEscapeItem(itemButterflyWing, now, "losing, no Fly Wing: "+why)
		b.leaderFledCall(now)
		b.setState("fled to town (" + why + ")")"""),
])

# (b) partyGoalSync: a member left on another map than its leader - the leader hunting elsewhere for 60 s - follows it
ed('party.go', [
    ("""	if townOf(lead.Map) == "" && lead.Goal == lead.Map && now.Sub(b.leadMapSince) >= 60*time.Second {
		want = lead.Map
	}""",
     """	if townOf(lead.Map) == "" && lead.Goal == lead.Map && now.Sub(b.leadMapSince) >= 60*time.Second {
		want = lead.Map
	}
	// never stranded (QA 2026-11-14 21:13-21:21Z: priests Thornsong / silverdragon / Mint chan alone on pay_fild10 /
	// pay_dun01 while their leaders hunted another map): a healer, or anyone standing on a dangerous map, follows the
	// leader's current hunting map after 30 s, whatever the party's stored map says
	if want != lead.Map && townOf(lead.Map) == "" && lead.Map != "" && !lead.Trip && now.Sub(b.leadMapSince) >= 30*time.Second &&
		(b.isHealer() || hostileTransit(b.MapName, b.powerLv())) && b.MapName != lead.Map {
		want = lead.Map
	}"""),
])

add = '''
// ---- Never strand a member (owner rule: the leader leads; life-mind reports 2026-11-14 20:54-21:21Z) ----

// leaderFledCall (b.mu held): a bot party's leader that just Butterfly-winged to town calls a party town run there
// and says so, so its members follow instead of staying alone on the hunting map (Neko_z left Mint chan on pay_fild10
// with Eddga 8 cells away; she died later).
func (b *Bot) leaderFledCall(now time.Time) {
	p := partyOf(b)
	if p == nil || p.Leader != b || p.Human != "" || b.P == nil {
		return
	}
	town, known := b.knownSave()
	if !known || town == "" {
		town = nearestTown(b.MapName)
	}
	if town == "" {
		return
	}
	typing, me := b.P.Typing, b.charName()
	go func() {
		if p.callTownRun(town, me, time.Now()) {
			b.partySay(styled(typing, pick("had to wing out, everyone to "+town+", meet at the kafra",
				"too hot, winged to "+town+" - come to the kafra there, all of u", "fled to "+town+", regroup at the kafra guys")))
		}
	}()
}

// leaderSyncPartyMap (b.mu held): a bot party leader whose own hunting goal is not the party map moves the party there
// (announced plan; members follow via moveTo) instead of leaving its members on the old map (LilEchodancer went to
// pay_fild10, its priest silverdragon stayed in pay_dun01 and died).
func (b *Bot) leaderSyncPartyMap(now time.Time) {
	p := partyOf(b)
	if p == nil || p.Leader != b || p.Human != "" || b.Goal.Set != "party-leader" || b.Goal.Map == "" || isTown(b.Goal.Map) ||
		townOf(b.Goal.Map) != "" || b.trip != nil || now.Before(b.buffAt["partysync"]) {
		return
	}
	parties.Lock()
	pm := p.Map
	parties.Unlock()
	if pm == b.Goal.Map {
		return
	}
	b.buffAt["partysync"] = now.Add(time.Minute)
	m, typing := b.Goal.Map, b.P.Typing
	b.event("party", "", fmt.Sprintf("my goal is %s but the party map was %s: moving the party", m, pm))
	go func() {
		b.partySay(styled(typing, planLine(p, m, "new spot")))
		p.moveTo(m)
	}()
}
'''
open(D + 'together.go', 'a', encoding='utf-8', newline='').write(add)

# call leaderSyncPartyMap every think (cheap; guarded) right after partyGoalSync
ed('ai.go', [
    ("""	b.partyGoalSync(now)
""",
     """	b.partyGoalSync(now)
	b.leaderSyncPartyMap(now) // together.go: a leader's own goal map becomes the party map (members never stranded)
"""),
])
print('ok')
