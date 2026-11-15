package main

// Bot parties survive Legion restarts (guild minds, 2026-11-14: "every deploy dissolves ALL bot parties"; deploys
// happen every ~30 min).
//
//   - data/parties.json holds every bot-led party (name, leader, members' characters, map, formed time, checkpoint,
//     rally and the party's gear-turn pool). Written on change (debounced) and every minute, atomically (tmp +
//     rename), only by the instance that owns the bot parties: the main one (instances.go; the core instance runs
//     only Ayla's human-led party, which has its own human-parties.json). Never written during shutdown: the
//     bots' logouts would otherwise save an empty file.
//   - On startup the saved parties are registered at once (so partyManager / leaderPartyManager don't regroup their
//     members), members log in on the saved character (pickChar), and once the leader is online each member gets
//     its party goal back. rAthena keeps parties in its DB across our restarts, so when the bots log back in they
//     are usually still in the in-game party (member list 0x0ae5): the engine Party is just re-attached. Otherwise
//     botPartyKeeper re-creates it (leader creates, invites; members accept in acceptInvite). Members that don't
//     log in within restoreWait are dropped (a party left with fewer than 2 disbands).
//   - botPartyKeeper also keeps every bot party's in-game party in step with the engine (create / invite / leave a
//     stale party / hand the in-game lead to the engine leader) and makes bots that belong to no engine party
//     leave a leftover bot-led in-game party, so the next invite works.
//
// Lesson (2026-11-14): rAthena runs with char_name_option 1 (party names may contain only letters, digits and
// spaces, conf/char_athena.conf), so "Novaboy's pt" was refused by the char server: NO bot party ever existed
// in-game (DB `party` table held only "Ayla pt") - no party chat, no exp share. botPartyName sanitises the name.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var partySuffix = regexp.MustCompile(`( pt[0-9]*)+$`)

const restoreWait = 10 * time.Minute // how long a restored party waits for its members to log in

// ownsBotParties: this instance saves and restores data/parties.json (not the core instance, see instances.go).
func ownsBotParties() bool { return !coreInstance() }

func botPartyFile() string { return filepath.Join(*dataDir, "parties.json") }

// cleanPartyName keeps what rAthena's char_name_letters allows (letters, digits, spaces).
func cleanPartyName(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// botPartyName: a party name the server accepts and no other party uses (parties.Lock held).
func botPartyName(leader string) string {
	base := cleanPartyName(leader)
	if base == "" {
		base = "Hunters"
	}
	if len(base) > 18 {
		base = strings.TrimSpace(base[:18])
	}
	name := base + " pt"
	for i := 2; parties.byName[name] != nil; i++ {
		name = fmt.Sprintf("%s pt%d", base, i)
	}
	return name
}

// ---------------------------------------------------------------- saving

type savedParty struct {
	Name       string     `json:"name"`
	Leader     string     `json:"leader"`
	Members    []string   `json:"members"` // character names, leader first
	Map        string     `json:"map"`
	Checkpoint string     `json:"checkpoint,omitempty"`
	Formed     time.Time  `json:"formed"`
	Rally      rallyState `json:"rally"`
	Gear       *gearPool  `json:"gear,omitempty"`
	// lfg.go need parties: a party formed around goals stays one across a restart (its goal checks resume)
	Need    bool   `json:"need,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
}

var partySave = struct {
	sync.Mutex
	last  []byte
	dirty chan struct{}
}{dirty: make(chan struct{}, 1)}

// markPartiesDirty: the bot parties changed; the saver writes the file a few seconds later.
func markPartiesDirty() {
	select {
	case partySave.dirty <- struct{}{}:
	default:
	}
}

// snapshotBotParties: the bot-led parties as saved (human-led ones are humanparty.go's).
func snapshotBotParties() []savedParty {
	restoring.Lock()
	chars := make(map[*Bot]string, len(restoring.char))
	for b, c := range restoring.char {
		chars[b] = c
	}
	restoring.Unlock()
	charOf := func(b *Bot) string {
		if c := chars[b]; c != "" {
			return c
		}
		return b.charName()
	}
	parties.Lock()
	var out []savedParty
	for _, p := range parties.byName {
		if p.Human != "" || p.Leader == nil || len(p.Members) < 2 {
			continue
		}
		s := savedParty{Name: p.Name, Leader: charOf(p.Leader), Map: p.Map, Checkpoint: p.Checkpoint, Formed: p.Formed,
			Rally: p.Rally, Need: p.Need, Purpose: p.Purpose, Anchor: p.Anchor}
		for _, m := range p.Members {
			s.Members = append(s.Members, charOf(m))
		}
		out = append(out, s)
	}
	parties.Unlock()
	gearPools.Lock()
	for i := range out {
		if pl := gearPools.m[out[i].Name]; pl != nil {
			cp := *pl
			cp.LastGot = make(map[string]time.Time, len(pl.LastGot))
			for k, v := range pl.LastGot {
				cp.LastGot[k] = v
			}
			// zeny the members received for their unbought turn item: a deploy must not make it giftable again
			cp.Raised = nil
			for _, n := range out[i].Members {
				if z := gearPools.raised[n]; z > 0 {
					if cp.Raised == nil {
						cp.Raised = map[string]int{}
					}
					cp.Raised[n] = z
				}
			}
			out[i].Gear = &cp
		}
	}
	gearPools.Unlock()
	// stable order: the file only changes when a party does
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// writeFileAtomic writes via a temp file + rename, so a crash never leaves half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// partyFileGuard: never let a mass disconnect wipe the saved parties (2026-11-14 17:03: a rathena recreate 3 min after
// a Legion deploy took every bot offline, partyManager disbanded ~350 parties and the next save wrote a near-empty
// file - the following start restored nothing). A save that would drop below half of the last saved count is held
// for 10 minutes (the drop must last), and the previous file is kept as a rolling backup (parties.json.bak1..3).
var partyFileGuard = struct {
	sync.Mutex
	lastN    int       // parties in the last file written / loaded
	dropFrom time.Time // since when saves would drop below half of lastN
}{}

const partyDropHold = 10 * time.Minute

// partySaveAllowed: the new set (n parties) may replace the file now.
func partySaveAllowed(n int, now time.Time) bool {
	partyFileGuard.Lock()
	defer partyFileGuard.Unlock()
	if partyFileGuard.lastN < 6 || n*2 >= partyFileGuard.lastN {
		partyFileGuard.dropFrom = time.Time{}
		return true
	}
	if partyFileGuard.dropFrom.IsZero() {
		partyFileGuard.dropFrom = now
		log.Printf("parties: holding the save - %d parties now vs %d saved (a mass disconnect?), file kept for %v", n, partyFileGuard.lastN, partyDropHold)
	}
	return now.Sub(partyFileGuard.dropFrom) >= partyDropHold
}

// rotatePartyBackups: parties.json -> .bak1 -> .bak2 -> .bak3 (only when the file holds parties)
func rotatePartyBackups(path string) {
	if old, err := loadSavedParties(path); err != nil || len(old) == 0 {
		return
	}
	os.Remove(path + ".bak3")
	os.Rename(path+".bak2", path+".bak3")
	os.Rename(path+".bak1", path+".bak2")
	if raw, err := os.ReadFile(path); err == nil {
		writeFileAtomic(path+".bak1", raw)
	}
}

func saveBotParties(path string, ps []savedParty) error {
	if !partySaveAllowed(len(ps), time.Now()) {
		return nil
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	if ps == nil {
		b = []byte("[]")
	}
	partySave.Lock()
	defer partySave.Unlock()
	if bytes.Equal(b, partySave.last) {
		return nil
	}
	partyFileGuard.Lock()
	big := len(ps)*2 < partyFileGuard.lastN // a big drop that outlasted the hold: keep the old set as a backup
	partyFileGuard.Unlock()
	if big || time.Since(partyBackupAt) > 10*time.Minute {
		rotatePartyBackups(path)
		partyBackupAt = time.Now()
	}
	if err := writeFileAtomic(path, b); err != nil {
		return err
	}
	partySave.last = b
	partyFileGuard.Lock()
	partyFileGuard.lastN = len(ps)
	partyFileGuard.Unlock()
	return nil
}

var partyBackupAt time.Time

// loadPartiesForRestore: parties.json, or the fullest backup written in the last 2 hours when that holds more than
// twice as many parties (the file was overwritten after a mass disconnect before the guard existed).
func loadPartiesForRestore(path string) ([]savedParty, string, error) {
	best, err := loadSavedParties(path)
	src := path
	for _, bk := range []string{path + ".bak1", path + ".bak2", path + ".bak3"} {
		st, e := os.Stat(bk)
		if e != nil || time.Since(st.ModTime()) > 2*time.Hour {
			continue
		}
		if ps, e := loadSavedParties(bk); e == nil && len(ps) > 2*len(best)+5 {
			best, src, err = ps, bk, nil
		}
	}
	return best, src, err
}

func loadSavedParties(path string) ([]savedParty, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ps []savedParty
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// partySaver: writes data/parties.json on change and every minute; stops (without saving) at shutdown.
func partySaver(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		case <-partySave.dirty:
			select { // debounce: a burst of forms / disbands is one write
			case <-stop:
				return
			case <-time.After(5 * time.Second):
			}
		}
		select {
		case <-stop:
			return // shutting down: the logouts mustn't overwrite the file with no parties
		default:
		}
		if err := saveBotParties(botPartyFile(), snapshotBotParties()); err != nil {
			log.Printf("parties: save failed: %v", err)
		}
	}
}

// ---------------------------------------------------------------- restoring

type partyRestore struct {
	chars    map[*Bot]string // member -> saved character
	deadline time.Time
	armed    map[*Bot]bool // member got its party goal back
	greeted  bool
}

var restoring = struct {
	sync.Mutex
	m    map[*Party]*partyRestore
	char map[*Bot]string // bot -> character to log in with (pickChar)
}{m: map[*Party]*partyRestore{}, char: map[*Bot]string{}}

// restoreCharFor: the character a bot of a restored party must log in with ("" = any).
func restoreCharFor(b *Bot) string {
	restoring.Lock()
	defer restoring.Unlock()
	return restoring.char[b]
}

func isRestoring(p *Party) bool {
	restoring.Lock()
	defer restoring.Unlock()
	return restoring.m[p] != nil
}

// restoreBotParties registers the saved bot parties (members still offline); restoreTick brings them back.
func restoreBotParties(bots []*Bot) {
	if !ownsBotParties() {
		return
	}
	saved, src, err := loadPartiesForRestore(botPartyFile())
	partyFileGuard.Lock()
	partyFileGuard.lastN = len(saved) // the first saves after the start must not drop below half of this unheld
	partyFileGuard.Unlock()
	if src != botPartyFile() {
		log.Printf("parties: %s is near-empty, restoring from the backup %s (%d parties)", botPartyFile(), src, len(saved))
	}
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("parties: can't read %s: %v", botPartyFile(), err)
		}
		return
	}
	byChar := map[string]*Bot{}
	for _, b := range bots {
		for _, c := range b.P.Chars {
			byChar[strings.ToLower(c.Name)] = b
		}
	}
	now := time.Now()
	n := 0
	for _, s := range saved {
		lead := byChar[strings.ToLower(s.Leader)]
		if lead == nil || s.Map == "" {
			continue
		}
		r := &partyRestore{chars: map[*Bot]string{lead: s.Leader}, deadline: now.Add(restoreWait), armed: map[*Bot]bool{}}
		members := []*Bot{lead}
		for _, c := range s.Members {
			if m := byChar[strings.ToLower(c)]; m != nil && r.chars[m] == "" {
				r.chars[m] = c
				members = append(members, m)
			}
		}
		if len(members) < 2 {
			continue
		}
		p := &Party{Name: s.Name, Leader: lead, Members: members, Map: s.Map, Formed: s.Formed, Checkpoint: s.Checkpoint,
			Rally: s.Rally, Restored: now, Need: s.Need, Purpose: s.Purpose, Anchor: s.Anchor}
		if p.Checkpoint == "" {
			p.Checkpoint = nearestTown(p.Map)
		}
		parties.Lock()
		taken := false
		for _, m := range members {
			taken = taken || parties.ofBot[m] != nil
		}
		if taken {
			parties.Unlock()
			continue
		}
		if p.Name != cleanPartyName(p.Name) || parties.byName[p.Name] != nil { // a name the server refuses (old files)
			p.Name = botPartyName(s.Leader)
		}
		parties.byName[p.Name] = p
		for _, m := range members {
			parties.ofBot[m] = p
		}
		parties.Unlock()
		if s.Gear != nil {
			if s.Gear.LastGot == nil {
				s.Gear.LastGot = map[string]time.Time{}
			}
			if s.Gear.Turn != "" {
				s.Gear.TurnSince = time.Now() // grace for the holder to log back in (an offline holder yields later)
			}
			gearPools.Lock()
			gearPools.m[p.Name] = s.Gear
			for n, z := range s.Gear.Raised {
				gearPools.raised[n] = max(gearPools.raised[n], z) // restore the raised-zeny record (gearpool.go)
			}
			gearPools.Unlock()
		}
		restoring.Lock()
		restoring.m[p] = r
		for m, c := range r.chars {
			restoring.char[m] = c
		}
		restoring.Unlock()
		n++
		log.Printf("restored bot party %s: leader %s, %d members, %s (waiting for them to log in)", p.Name, s.Leader, len(members), p.Map)
	}
	log.Printf("parties: restored %d of %d saved bot parties from %s", n, len(saved), src)
}

// onSavedChar: the member is online, on a map, playing the character its restored party knows.
func onSavedChar(m *Bot, char string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Online && m.MapName != "" && (char == "" || strings.EqualFold(m.charName(), char))
}

// restoreTick: members of restored parties that are online get their party goal back; at the deadline (or when
// everyone is back) the party continues with whoever came.
func restoreTick(now time.Time) {
	restoring.Lock()
	list := make(map[*Party]*partyRestore, len(restoring.m))
	for p, r := range restoring.m {
		list[p] = r
	}
	restoring.Unlock()
	for p, r := range list {
		parties.Lock()
		alive := parties.byName[p.Name] == p
		lead, members, mapName := p.Leader, append([]*Bot(nil), p.Members...), p.Map
		parties.Unlock()
		if !alive {
			endRestore(p, r)
			continue
		}
		if onSavedChar(lead, r.chars[lead]) {
			for _, m := range members {
				if r.armed[m] || !onSavedChar(m, r.chars[m]) {
					continue
				}
				r.armed[m] = true
				m.mu.Lock()
				if m == lead {
					m.Goal = Goal{Mode: "hunt", Map: mapName, Set: "party-leader"}
				} else {
					m.Goal = Goal{Mode: "party", Map: mapName, Who: lead.charName(), Set: "party"}
				}
				m.route, m.path = nil, nil
				m.mu.Unlock()
			}
			if !r.greeted {
				r.greeted = true
				lead.mu.Lock()
				typing := lead.P.Typing
				lead.mu.Unlock()
				go func() {
					time.Sleep(20 * time.Second)
					lead.partySay(styled(typing, pick("ok back, we keep going at "+mapTalk(mapName), "back! same spot, "+mapTalk(mapName),
						"re, regroup at "+mapTalk(mapName))))
				}()
				lead.event("party", "", fmt.Sprintf("restored %s on %s after the restart", p.Name, mapName))
			}
		}
		all := true
		for _, m := range members {
			all = all && r.armed[m]
		}
		if all || now.After(r.deadline) {
			finishRestore(p, r)
		}
	}
}

// finishRestore drops the members that never came back; the party goes on if 2 or more are left.
func finishRestore(p *Party, r *partyRestore) {
	parties.Lock()
	var rest []*Bot
	if r.armed[p.Leader] {
		for _, m := range p.Members {
			if r.armed[m] {
				rest = append(rest, m)
			} else if parties.ofBot[m] == p {
				delete(parties.ofBot, m)
			}
		}
	}
	keep := len(rest) >= 2
	if keep {
		p.Members = rest
	}
	parties.Unlock()
	endRestore(p, r)
	if !keep {
		log.Printf("parties: restored %s didn't get its members back in time, disbanding", p.Name)
		disband(p)
		return
	}
	log.Printf("parties: %s is back with %d members", p.Name, len(rest))
	markPartiesDirty()
}

func endRestore(p *Party, r *partyRestore) {
	restoring.Lock()
	delete(restoring.m, p)
	for m := range r.chars {
		delete(restoring.char, m)
	}
	restoring.Unlock()
}

// ---------------------------------------------------------------- keeping the in-game party in step

type partySync struct {
	createAt time.Time
	tries    int
	invited  map[*Bot]time.Time
	acted    map[*Bot]time.Time // leave / leader-change requests
}

// leaveGameParty: leave whatever in-game party the bot is in.
func (b *Bot) leaveGameParty() {
	b.send(newPkt(0x0100).b)
	b.mu.Lock()
	b.PartyName, b.PartyID, b.partyLeader = "", 0, ""
	b.roster = map[string]string{}
	b.mu.Unlock()
}

type memberPartyView struct {
	online        bool
	char          string
	pid           uint32
	pname, leader string
	aid           uint32
}

func viewOf(b *Bot) memberPartyView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return memberPartyView{online: b.Online && b.MapName != "", char: b.charName(), pid: b.PartyID, pname: b.PartyName,
		leader: b.partyLeader, aid: b.aid}
}

// renameParty: the server refused the name (another party holds it): pick a new one.
func renameParty(p *Party) {
	parties.Lock()
	if parties.byName[p.Name] != p {
		parties.Unlock()
		return
	}
	old := p.Name
	delete(parties.byName, old)
	base := cleanPartyName(partySuffix.ReplaceAllString(old, "")) // "echodragon87 pt2" -> "echodragon87", not "... pt2 pt2"
	name := ""
	for i := 2; name == "" || parties.byName[name] != nil; i++ {
		name = fmt.Sprintf("%.17s pt%d", base, i)
	}
	p.Name = name
	parties.byName[name] = p
	parties.Unlock()
	gearPools.Lock()
	if pl := gearPools.m[old]; pl != nil {
		delete(gearPools.m, old)
		gearPools.m[name] = pl
	}
	gearPools.Unlock()
	log.Printf("parties: %s refused by the server, renamed to %s", old, name)
	markPartiesDirty()
}

func syncBotParty(p *Party, st *partySync, chars map[*Bot]string, now time.Time) {
	parties.Lock()
	name, lead, human, formed := p.Name, p.Leader, p.Human, p.Formed
	members := append([]*Bot(nil), p.Members...)
	parties.Unlock()
	if human != "" || lead == nil || (chars == nil && now.Sub(formed) < 20*time.Second) {
		return // formParty is still creating / inviting
	}
	right := func(m *Bot, v memberPartyView) bool {
		return v.online && (chars == nil || strings.EqualFold(v.char, chars[m]))
	}
	acted := func(m *Bot, gap time.Duration) bool {
		if now.Sub(st.acted[m]) < gap {
			return true
		}
		st.acted[m] = now
		return false
	}
	lv := viewOf(lead)
	if !right(lead, lv) {
		return
	}
	switch {
	case lv.pid != 0 && !strings.EqualFold(lv.pname, name): // still in some other in-game party
		if !acted(lead, 20*time.Second) {
			lead.leaveGameParty()
		}
		return
	case lv.pid == 0:
		if now.Sub(st.createAt) < 30*time.Second {
			return
		}
		if st.tries >= 3 {
			renameParty(p)
			st.tries = 0
			return
		}
		st.createAt = now
		st.tries++
		lead.mu.Lock()
		lead.PartyName = name
		lead.mu.Unlock()
		lead.send(newPkt(0x01e8).str(name, 24).u8(1).u8(0).b)
		go func() {
			time.Sleep(1500 * time.Millisecond)
			lead.send(newPkt(0x07d7).u32(1).u8(1).u8(0).b) // even exp share
		}()
		return
	}
	st.tries = 0
	if lv.leader != "" && !strings.EqualFold(lv.leader, lv.char) {
		// the in-game lead is another member (e.g. it founded the party): hand it to the engine's leader, who invites
		for _, m := range members {
			if v := viewOf(m); m != lead && strings.EqualFold(v.char, lv.leader) && v.online && v.pid != 0 &&
				strings.EqualFold(v.pname, name) && !acted(m, 30*time.Second) {
				m.send(newPkt(0x07da).u32(lv.aid).b)
			}
		}
		return
	}
	for _, m := range members {
		if m == lead {
			continue
		}
		v := viewOf(m)
		if !right(m, v) || (v.pid != 0 && strings.EqualFold(v.pname, name)) {
			continue
		}
		if v.pid != 0 { // in a leftover party: leave it first
			if !acted(m, 20*time.Second) {
				m.leaveGameParty()
			}
			continue
		}
		if now.Sub(st.invited[m]) < 30*time.Second {
			continue
		}
		st.invited[m] = now
		lead.send(newPkt(0x02c4).str(v.char, 24).b)
	}
}

// botPartyKeeper: restores saved parties, keeps in-game parties in step with the engine's, saves the file.
func botPartyKeeper(bots []*Bot, stop <-chan struct{}) {
	if ownsBotParties() {
		go partySaver(stop)
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	syncs := map[*Party]*partySync{}
	leftAt := map[*Bot]time.Time{}
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		now := time.Now()
		restoreTick(now)
		parties.Lock()
		ps := make([]*Party, 0, len(parties.byName))
		for _, p := range parties.byName {
			if p.Human == "" {
				ps = append(ps, p)
			}
		}
		parties.Unlock()
		live := map[*Party]bool{}
		for _, p := range ps {
			live[p] = true
			st := syncs[p]
			if st == nil {
				st = &partySync{invited: map[*Bot]time.Time{}, acted: map[*Bot]time.Time{}}
				syncs[p] = st
			}
			var chars map[*Bot]string
			restoring.Lock()
			if r := restoring.m[p]; r != nil {
				chars = r.chars
			}
			restoring.Unlock()
			syncBotParty(p, st, chars, now)
		}
		for p := range syncs {
			if !live[p] {
				delete(syncs, p)
			}
		}
		// bots in no engine party but still in a bot-led in-game party (left over from a restart or a disband
		// whose leave packet was lost): leave it, so the next invite works. A human's party is left alone.
		for _, b := range bots {
			if b.alumniLeave(now) { // humanpartyroster.go: left the human's party on the owner's order
				continue
			}
			if partyOf(b) != nil || now.Sub(leftAt[b]) < time.Minute {
				continue
			}
			b.mu.Lock()
			_, inRoster := b.roster[b.partyLeader]
			stale := b.Online && b.pin == nil && b.PartyID != 0 && b.partyLeader != "" && inRoster && allChars[b.partyLeader]
			b.mu.Unlock()
			if stale && partyOf(b) == nil {
				leftAt[b] = now
				b.leaveGameParty()
			}
		}
	}
}

// farmingWell: every member is online and on the party's map (parties.Lock held) - such a party isn't refreshed
// at 90 minutes (partyManager).
func (p *Party) farmingWell() bool {
	if p.Map == "" {
		return false
	}
	for _, m := range p.Members {
		s, ok := peek(m.P.Name)
		if ok && s.Errand {
			continue // errand.go: back soon
		}
		if !ok || !s.Online || s.Map != p.Map {
			return false
		}
	}
	return true
}
