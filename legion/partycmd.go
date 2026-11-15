package main

// Party commands (owner 2026-11-14: "Ayla also needs to command the party"; AY04, PT04, PT10, PT26, PT32). The leader -
// Ayla or a bot party leader - gives short commands in party chat and the members OBEY them ahead of their own goals:
//
//	follow me / come with me / lets go (to <map>)      -> go to the leader (her map, then within 6 cells) and stay with her
//	come to me (at <map> x,y) / waiting for u at ...    -> go to the leader (or the named map/cell when she can't be seen)
//	<name> come to <map> / meet us on <map>             -> that member goes to the map
//	stay close / crossing <map> together                -> with the leader, within 4 cells
//	wait here / hold here                               -> to the leader, then hold there (fight back only)
//	coming to u / falling back to u / wait for me       -> stay where you are, she comes
//	pull back / back to me / help, 3 on me              -> to the leader now (within 3)
//	heal me / buffs pls                                 -> the party priest goes within 5 cells (priestbuff.go buffs)
//
// Lines addressed to one member ("Echobunny come to me, im at gef_fild10 52,326", "coming to u, Zendancer84") apply to
// that member only. Plans (PT30 "going X - meet me there", "new spot: X") and town runs (rally.go) are left to their own
// code. A member answers now and then ("omw", "coming", "ok"). Safety first: a member on a town trip, an errand, waiting
// out a deadly map, resting or low on HP keeps doing that; a weak party never runs ahead into a dungeon.
// The leader side for bot parties (PR16): a bot leader calls a far member to it ("<name> come to me, im at <map> x,y").
// tuning party_cmd (default on) turns it all off.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type partyCmd struct {
	Kind string // follow | come | close | wait | stay | pullback | goto | map | heal | buff
	Map  string
	X, Y int
	To   string // the member it is addressed to (as written), "" = everyone
}

type partyCmdOrder struct {
	partyCmd
	By    string
	At    time.Time
	Until time.Time
	Acked bool
	LastD int // stay / wait: the leader's last distance (stayends.go: growing = she walks on, the order ends)
}

var partyOrders sync.Map // member char name (lower) -> partyCmdOrder

func partyCmdTTL(kind string) time.Duration {
	switch kind {
	case "pullback", "heal":
		return 30 * time.Second
	case "buff", "stay":
		return 60 * time.Second
	case "close", "wait":
		return 2 * time.Minute
	case "goto":
		return 4 * time.Minute
	case "map":
		return 5 * time.Minute
	}
	return 3 * time.Minute // follow, come
}

// partyCmdRadius: how close to the leader the order wants the member.
func partyCmdRadius(kind string) int {
	switch kind {
	case "pullback":
		return 3
	case "close":
		return 4
	case "heal", "buff":
		return 5
	}
	return 6
}

var (
	pcCoordRe = regexp.MustCompile(`\b(\d{1,3})\s*[, ]\s*(\d{1,3})\b`)
	pcMapRe   = regexp.MustCompile(`\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+|[a-z]{3,}\d{1,2}|prontera|payon|geffen|morocc|alberta|izlude|aldebaran|yuno|comodo|umbala|lighthalzen|einbroch|hugel|rachel|veins|louyang|amatsu|gonryun|ayothaya|niflheim|jawaii)\b`)

	pcStayRe     = regexp.MustCompile(`falling back to (u|you)|coming (to|for|back for|back to) (u|you|ya)|i'?ll come (get|to|for)|ill come (get|to|for)|wait for me|stay (there|put)|dont move|don't move|hold on there`)
	pcHealRe     = regexp.MustCompile(`\bheal (me|pls|plz|please|here)|need (a )?heal|heals? (pls|plz|please)`)
	pcBuffRe     = regexp.MustCompile(`\bbuffs? (me|pls|plz|please)|need buffs|\bbless (me|pls|plz)|\bagi (me|pls|plz)`)
	pcPullRe     = regexp.MustCompile(`pull back|fall back to me|back to me|regroup (on|with|at) me|get back here|to me now|\bhelp\b|\d+ (mobs |monsters )?on me|need help`)
	pcCloseRe    = regexp.MustCompile(`stay close|stick together|stay together|keep close|close to me|stay near|dont run off|don't run off|stay with me|together, stay`)
	pcWaitRe     = regexp.MustCompile(`wait here|hold here|wait a (sec|bit|min)|stop here|hold up|everyone wait|all wait`)
	pcComeRe     = regexp.MustCompile(`waiting for (u|you|ya|everyone|the rest|all)|come to me|come here|come straight here|come over|meet me (there|here|at|in|on)|i'?m (at|on|in) |im (at|on|in) |we'?re (at|on|in) `)
	pcFollowRe   = regexp.MustCompile(`follow me|come with me|with me guys|on me guys|all: follow|everyone follow`)
	pcGotoRe     = regexp.MustCompile(`let'?s go|lets head|let'?s head|we go to|off to|move out|set off`)
	pcAddrMapRe  = regexp.MustCompile(`come to|meet (me|us) (on|at|in)|go on to|catch up|go to|head to|we start`)
	pcPlanRe     = regexp.MustCompile(`^(going|heading|next|new spot)\b|next spot|new spot|next town|next time|next run`)
	pcAllPrefixR = regexp.MustCompile(`^(all|everyone|guys|party)\s*[:,]?\s+`)
)

// parsePartyCmd reads one leader line (the text after "Name : "). names: the party's member names, so an addressee is
// recognised (and a name like vex_x18 is never read as a map).
func parsePartyCmd(text string, names []string) (partyCmd, bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimRight(t, " .!")
	if t == "" || strings.Contains(t, "?") || strings.Contains(t, "town run") {
		return partyCmd{}, false
	}
	var c partyCmd
	sorted := append([]string(nil), names...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, n := range sorted {
		ln := strings.ToLower(n)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(t, ln) && len(t) > len(ln) && strings.ContainsRune(" ,:", rune(t[len(ln)])) {
			c.To, t = n, strings.TrimLeft(t[len(ln):], " ,:")
			break
		}
		if strings.HasSuffix(t, ln) && len(t) > len(ln) && strings.ContainsRune(" ,", rune(t[len(t)-len(ln)-1])) {
			c.To, t = n, strings.TrimRight(t[:len(t)-len(ln)], " ,")
			break
		}
	}
	if c.To == "" {
		t = pcAllPrefixR.ReplaceAllString(t, "")
	}
	// other names never read as maps or coordinates
	clean := t
	for _, n := range sorted {
		if ln := strings.ToLower(n); ln != "" {
			clean = strings.ReplaceAll(clean, ln, " ")
		}
	}
	if m := pcMapRe.FindString(clean); m != "" && validCmdMap(m) {
		c.Map = m
		if xy := pcCoordRe.FindStringSubmatch(clean[strings.Index(clean, m)+len(m):]); xy != nil {
			c.X, _ = strconv.Atoi(xy[1])
			c.Y, _ = strconv.Atoi(xy[2])
		}
	}
	switch {
	case pcStayRe.MatchString(t):
		c.Kind = "stay"
	case pcHealRe.MatchString(t):
		c.Kind = "heal"
	case pcBuffRe.MatchString(t):
		c.Kind = "buff"
	case c.To != "" && c.Map != "" && pcAddrMapRe.MatchString(t) && !pcComeRe.MatchString(t):
		c.Kind = "map" // "vex_x18 come to orcsdun02, we start there", "Silent rogue we go on, meet us on orcsdun02"
	case pcPlanRe.MatchString(t):
		return partyCmd{}, false // PT30 plans: partydest.go / rally.go
	case pcPullRe.MatchString(t):
		c.Kind = "pullback"
	case pcCloseRe.MatchString(t):
		c.Kind = "close"
	case pcWaitRe.MatchString(t):
		c.Kind = "wait"
	case pcFollowRe.MatchString(t):
		c.Kind = "follow"
	case pcGotoRe.MatchString(t):
		c.Kind = "goto"
	case pcComeRe.MatchString(t):
		c.Kind = "come"
	default:
		return partyCmd{}, false
	}
	return c, true
}

// validCmdMap: a token that names a real map (when the map data is loaded; in tests every map-shaped token passes).
func validCmdMap(m string) bool {
	if strings.HasPrefix(m, "lv") {
		return false
	}
	if _, ok := townSpawn[m]; ok {
		return true
	}
	if len(maps) < 50 {
		return true
	}
	_, ok := maps[m]
	return ok
}

// partyCmdHeard (b.mu held; bot.go party chat): the party leader gave a command - remember it as this member's order.
func (b *Bot) partyCmdHeard(msg string, now time.Time) {
	if !tune().PartyCmd {
		return
	}
	i := strings.Index(msg, " : ")
	if i <= 0 {
		return
	}
	who, txt := strings.TrimSpace(msg[:i]), msg[i+3:]
	me := b.charName()
	if strings.EqualFold(who, me) {
		return
	}
	p := partyOf(b)
	if p == nil {
		return
	}
	lead := p.Human
	if lead == "" && p.Leader != nil && p.Leader != b {
		if s, ok := peek(p.Leader.P.Name); ok {
			lead = s.Name
		}
	}
	if lead == "" || !strings.EqualFold(who, lead) {
		return
	}
	var names []string
	for _, m := range p.Members {
		if s, ok := peek(m.P.Name); ok && s.Name != "" {
			names = append(names, s.Name)
		}
	}
	names = append(names, me)
	c, ok := parsePartyCmd(txt, names)
	if !ok || c.To != "" && !strings.EqualFold(c.To, me) {
		return
	}
	if (c.Kind == "heal" || c.Kind == "buff") && !b.isHealer() {
		return
	}
	partyOrders.Store(strings.ToLower(me), partyCmdOrder{partyCmd: c, By: who, At: now, Until: now.Add(partyCmdTTL(c.Kind))})
	b.event("party", "", fmt.Sprintf("party: %s's command %q -> %s %s", who, strings.TrimSpace(txt), c.Kind, c.Map))
}

// partyCmdOrderOf: this member's live order.
func partyCmdOrderOf(name string, now time.Time) (partyCmdOrder, bool) {
	v, ok := partyOrders.Load(strings.ToLower(name))
	if !ok {
		return partyCmdOrder{}, false
	}
	o := v.(partyCmdOrder)
	if now.After(o.Until) {
		partyOrders.Delete(strings.ToLower(name))
		return partyCmdOrder{}, false
	}
	return o, true
}

// partyCmdTarget: where an order sends the member - the leader's live position when known (her current map, the
// town for a building), else the map / cell the line named. cell=false: go to the map only.
func partyCmdTarget(o partyCmdOrder, lead pubState, leadOK bool) (m string, x, y int, cell bool) {
	if leadOK && lead.Map != "" {
		m, x, y = lead.Map, lead.X, lead.Y
		if t := townOf(m); t != "" && t != m {
			return t, 0, 0, false // in a shop / the Kafra office: wait outside in the town
		}
		if o.Kind == "map" && o.Map != "" && o.Map != m {
			return o.Map, 0, 0, false // "come to orcsdun02, we start there": the map she named, she is on her way
		}
		return m, x, y, x > 0 && y > 0
	}
	if o.Map != "" {
		return o.Map, o.X, o.Y, o.X > 0 && o.Y > 0
	}
	return "", 0, 0, false
}

// partyCmdThink (b.mu held; ai.go think, after the party goal syncs): carry out the leader's order. True = it acted.
func (b *Bot) partyCmdThink(gm *GameMap, moving bool, now time.Time) bool {
	if !tune().PartyCmd {
		return false
	}
	p := partyOf(b)
	if p == nil {
		return false
	}
	if p.Human == "" && p.Leader == b {
		b.partyCmdLead(p, now)
		return false
	}
	o, ok := partyCmdOrderOf(b.charName(), now)
	if !ok {
		return false
	}
	// safety and real needs first: a town trip, an errand, a Kafra plan, a deadly map, a rest, low HP, a fight on us
	if b.trip != nil || b.errand != nil || b.kafra != nil || b.waitingAtCheckpoint || b.restOn || b.hpFrac() < 0.35 || b.underAttack(now) {
		return false
	}
	var lead pubState
	leadOK := false
	if p.Human != "" && strings.EqualFold(o.By, p.Human) {
		lead, leadOK = b.humanState(p.Human, now)
	} else if p.Leader != nil && p.Leader != b {
		if s, ok := peek(p.Leader.P.Name); ok && s.Online && strings.EqualFold(s.Name, o.By) {
			lead, leadOK = s, true
			if s.LX > 0 {
				lead.X, lead.Y = s.LX, s.LY
			}
		}
	}
	if !leadOK && o.Map == "" {
		return false
	}
	if !o.Acked {
		o.Acked = true
		partyOrders.Store(strings.ToLower(b.charName()), o)
		b.partyCmdAck(o, now)
	}
	if (o.Kind == "stay" || o.Kind == "wait") && leadOK && lead.Map == b.MapName { // stayends.go
		d := dist(b.X, b.Y, lead.X, lead.Y)
		if stayOrderOver(d, o.LastD) {
			partyOrders.Delete(strings.ToLower(b.charName()))
			return false // she is here / walks on: the normal party follow takes over
		}
		o.LastD = d
		partyOrders.Store(strings.ToLower(b.charName()), o)
	}
	if o.Kind == "stay" {
		b.target = 0
		b.setState("waiting here, " + o.By + " is coming")
		return true
	}
	tm, tx, ty, cell := partyCmdTarget(o, lead, leadOK)
	if tm == "" {
		return false
	}
	if tm != b.MapName {
		// never run ahead alone into a dungeon / hostile map with a weak party (QA 19:00Z Sphinx: three died)
		if (dungeonMap(tm) || hostileTransit(tm, b.powerLv())) && !partyStrong(p) && !(leadOK && lead.Map == tm) {
			return false
		}
		if b.Goal.Map == tm {
			return false // the normal travel already goes there (with its fight-back on the way)
		}
		b.target = 0
		saved := b.Goal
		b.Goal = Goal{Mode: "party", Map: tm, Who: o.By, Set: saved.Set}
		b.travel(gm, moving)
		b.Goal = saved
		b.setState(fmt.Sprintf("going to %s on %s (%s)", o.By, tm, o.Kind))
		return true
	}
	if !cell {
		return false // on the map she named: the normal party follow takes over
	}
	r := partyCmdRadius(o.Kind)
	if d := dist(b.X, b.Y, tx, ty); d > r {
		b.target = 0
		if b.Sitting && now.Sub(b.sitSentAt) > time.Second {
			b.sitSentAt, b.wantSit = now, false
			go b.stand()
		}
		if !moving || now.Sub(b.lastMoveTx) > 3*time.Second {
			wx, wy := tx+max(-2, min(2, b.X-tx)), ty+max(-2, min(2, b.Y-ty))
			if gm != nil && !gm.Walkable(wx, wy) {
				wx, wy = tx, ty
			}
			go b.walkTo(wx, wy)
		}
		b.setState(fmt.Sprintf("going to %s (%s, %d cells)", o.By, o.Kind, d))
		return true
	}
	if o.Kind == "wait" {
		b.target = 0
		b.setState("holding by " + o.By + " (wait here)")
		return true
	}
	return false // close enough: fight / support near her as usual (partyThink leash)
}

// partyCmdAck: a member answers a command now and then, like a real partymate (PT26).
func (b *Bot) partyCmdAck(o partyCmdOrder, now time.Time) {
	chance := 0.35
	if o.To != "" {
		chance = 0.8
	}
	if now.Before(b.buffAt["cmdack"]) || b.rng == nil || b.rng.Float64() >= chance {
		return
	}
	b.buffAt["cmdack"] = now.Add(2 * time.Minute)
	var line string
	switch o.Kind {
	case "stay":
		line = pick("ok", "k, waiting", "ok here", "ty")
	case "wait":
		line = pick("ok", "k", "ok waiting")
	case "heal", "buff":
		line = pick("ok", "on it", "coming")
	case "close", "pullback":
		line = pick("ok", "k", "coming", "right behind u")
	default:
		line = pick("omw", "coming", "ok coming", "on my way", "k omw")
	}
	typing := b.P.Typing
	go b.partySay(styled(typing, line))
}

// partyCmdLead (b.mu held): a bot party leader calls a member that is far off to it (PR16: what Ayla does). A member on
// another map for 90 s, or 25+ cells away on this map for 40 s, while the leader hunts here: "<name> come to me, im at
// <map> x,y" - once per 4 min per member, one line per 30 s.
func (b *Bot) partyCmdLead(p *Party, now time.Time) {
	b.partyCmdCall(p, now, false)
}

// partyCmdCall (b.mu held): the leader's calls. owner (AY25, an owner party_order): every far member is called at once,
// without the waits and the 30 s throttle; returns false while the leader cannot act (trip, errand, town, not hunting
// here), so the owner's order is tried again. The normal path returns after one call and ignores its result.
func (b *Bot) partyCmdCall(p *Party, now time.Time, owner bool) bool {
	if b.trip != nil || b.errand != nil || isTown(b.MapName) || townOf(b.MapName) != "" || b.Goal.Map != b.MapName || (!owner && now.Before(b.buffAt["cmdlead"])) {
		return false
	}
	called := 0
	for _, m := range p.Members {
		if m == b {
			continue
		}
		s, ok := peek(m.P.Name)
		if !ok || !s.Online || s.Name == "" {
			continue
		}
		key := "cmdfar:" + s.Name
		far := s.Map != b.MapName || dist(b.X, b.Y, s.X, s.Y) > 25
		if !far || s.Trip || s.Errand || s.Waiting || s.Resting {
			delete(b.buffAt, key)
			continue
		}
		if owner {
			typing, where := b.P.Typing, fmt.Sprintf("%s %d,%d", b.MapName, b.X, b.Y)
			go b.partySay(styled(typing, pick(s.Name+" come to me, im at "+where, s.Name+" come here, im at "+where, s.Name+" where r u? come to me at "+where)))
			called++
			continue
		}
		since, seen := b.buffAt[key]
		if !seen {
			b.buffAt[key] = now
			continue
		}
		wait := 40 * time.Second
		if s.Map != b.MapName {
			wait = 90 * time.Second
		}
		if now.Sub(since) < wait || now.Before(b.buffAt["cmdlead:"+s.Name]) {
			continue
		}
		b.buffAt["cmdlead:"+s.Name] = now.Add(4 * time.Minute)
		b.buffAt["cmdlead"] = now.Add(30 * time.Second)
		typing, where := b.P.Typing, fmt.Sprintf("%s %d,%d", b.MapName, b.X, b.Y)
		go b.partySay(styled(typing, pick(s.Name+" come to me, im at "+where, s.Name+" come here, im at "+where, s.Name+" where r u? come to me at "+where)))
		return true
	}
	return called > 0 && owner
}
