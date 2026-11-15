package main

// Instance handoff (AY15 rotation, 2026-11-15): moves one account between the two Legion instances without a container
// recreate. The core instance (-only LEGION_CORE) runs Ayla's party friends, the main one (-exclude LEGION_CORE) every
// other bot; LEGION_CORE in .env is the static split, data/instance_handoff.json the live overrides on top of it.
//
//   - data/instance_handoff.json (written by the core instance only, aylarotate.go):
//       to_core:       char -> stamp: the core instance runs this character's account from then (a new party member)
//       to_main:       char -> stamp: the main instance runs it (a member rotated out, now an alumnus)
//       core_released: char -> the to_main stamp the core instance has logged out and parked
//   - data/instance_handoff_main.json (written by the main instance only):
//       released:      char -> the to_core stamp the main instance has logged out and parked
//
// The releasing side goes first: it parks the account's run loop (bot.go run -> handoffParked) and logs it out, then
// writes its "released" stamp; the adopting side's run loop waits for that stamp before it logs in (no double login).
// The core instance lets an alumnus say goodbye and leave the human's in-game party first (humanpartyroster.go
// alumniLeave). At startup both instances apply the overrides to their profile list (main.go -> handoffSelect), so a
// later deploy keeps the new roster without editing LEGION_CORE. A single instance (no -only/-exclude) never hands off.

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type handoffFile struct {
	ToCore       map[string]int64 `json:"to_core,omitempty"`
	ToMain       map[string]int64 `json:"to_main,omitempty"`
	CoreReleased map[string]int64 `json:"core_released,omitempty"`
}

type handoffAck struct {
	Released map[string]int64 `json:"released,omitempty"`
}

func handoffPath() string    { return filepath.Join(*dataDir, "instance_handoff.json") }
func handoffAckPath() string { return filepath.Join(*dataDir, "instance_handoff_main.json") }

// handoffActive: two instances share the server (the only case where an account can move between them).
func handoffActive() bool { return *onlyNames != "" || *excludeNames != "" }

var handoffCache struct {
	sync.Mutex
	at time.Time
	f  handoffFile
	a  handoffAck
}

// handoffWriteMu: read-modify-write of this instance's own handoff file (rotation + release loop).
var handoffWriteMu sync.Mutex

func readJSONFile(path string, v any) { // missing / broken file = zero value
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, v)
	}
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// handoffNow: both files, re-read at most every 5 s.
func handoffNow() (handoffFile, handoffAck) {
	handoffCache.Lock()
	defer handoffCache.Unlock()
	if !handoffCache.at.IsZero() && time.Since(handoffCache.at) < 5*time.Second {
		return handoffCache.f, handoffCache.a
	}
	var f handoffFile
	var a handoffAck
	readJSONFile(handoffPath(), &f)
	readJSONFile(handoffAckPath(), &a)
	handoffCache.at, handoffCache.f, handoffCache.a = time.Now(), f, a
	return f, a
}

func handoffInvalidate() {
	handoffCache.Lock()
	handoffCache.at = time.Time{}
	handoffCache.Unlock()
}

// handoffOverride: "core" / "main" when a live override names one of the persona's characters, "" = static split.
func handoffOverride(p *Persona, f handoffFile) (string, string, int64) {
	for _, c := range p.Chars {
		k := strings.ToLower(c.Name)
		if t := f.ToCore[k]; t > 0 {
			return "core", k, t
		}
		if t := f.ToMain[k]; t > 0 {
			return "main", k, t
		}
	}
	return "", "", 0
}

// handoffRuns: this instance (core or main) runs persona p; static = the -only/-exclude answer.
func handoffRuns(p *Persona, core, static bool, f handoffFile) bool {
	switch o, _, _ := handoffOverride(p, f); o {
	case "core":
		return core
	case "main":
		return !core
	}
	return static
}

// handoffMayLogin: the other instance has released the account (or no handoff is pending for it).
func handoffMayLogin(p *Persona, core bool, f handoffFile, a handoffAck) bool {
	o, k, t := handoffOverride(p, f)
	switch {
	case o == "core" && core:
		return a.Released[k] >= t
	case o == "main" && !core:
		return f.CoreReleased[k] >= t
	}
	return true
}

// handoffSelect (main.go, startup): the profile list with the live overrides applied.
func handoffSelect(all, ps []*Persona) []*Persona {
	if !handoffActive() {
		return ps
	}
	f, _ := handoffNow()
	core := coreInstance()
	in := map[*Persona]bool{}
	var out []*Persona
	for _, p := range ps {
		in[p] = true
		if handoffRuns(p, core, true, f) {
			out = append(out, p)
		}
	}
	for _, p := range all {
		if !in[p] && handoffRuns(p, core, false, f) {
			out = append(out, p)
			log.Printf("handoff: this instance runs %s (%s) now", p.Name, p.Chars[0].Name)
		}
	}
	return out
}

// handoffCoreName (instances.go coreForward): the core instance runs this character now.
func handoffCoreName(static map[string]bool, name string) bool {
	k := strings.ToLower(name)
	f, _ := handoffNow()
	if f.ToCore[k] > 0 {
		return true
	}
	if f.ToMain[k] > 0 {
		return false
	}
	return static[k]
}

// parkedBots: run loops waiting in handoffParked (the account is offline and stays so).
var parkedBots sync.Map // *Bot -> true

// handoffParked (bot.go run, top of the loop): the account belongs to the other instance, or waits for it to let go.
func (b *Bot) handoffParked(stop <-chan struct{}) bool {
	if !handoffActive() {
		return false
	}
	f, a := handoffNow()
	core := coreInstance()
	runs := handoffRuns(b.P, core, true, f)
	if runs && handoffMayLogin(b.P, core, f, a) {
		parkedBots.Delete(b)
		return false
	}
	parkedBots.Store(b, true)
	b.mu.Lock()
	if runs {
		b.State = "offline (joining a party soon)"
	} else {
		b.State = "offline (playing elsewhere)"
	}
	b.mu.Unlock()
	select {
	case <-stop:
	case <-time.After(10 * time.Second):
	case <-b.wake:
	}
	return true
}

// ---------------------------------------------------------------- this instance's bots (static + adopted)

var instBots = struct {
	sync.Mutex
	byAcct  map[string]*Bot
	adopted []*Bot
	login   string
	stop    <-chan struct{}
}{byAcct: map[string]*Bot{}}

func registerInstanceBots(bots []*Bot, login string, stop <-chan struct{}) {
	instBots.Lock()
	defer instBots.Unlock()
	for _, b := range bots {
		instBots.byAcct[b.P.Account] = b
	}
	instBots.login, instBots.stop = login, stop
}

// adoptedBots: bots this instance took over after startup (not in the startup lists of the managers / API).
func adoptedBots() []*Bot {
	instBots.Lock()
	defer instBots.Unlock()
	return append([]*Bot(nil), instBots.adopted...)
}

// withAdopted (humanguild.go guild-recruits): the startup list plus the adopted bots.
func withAdopted(bots []*Bot) []*Bot {
	ad := adoptedBots()
	if len(ad) == 0 {
		return bots
	}
	return append(append([]*Bot(nil), bots...), ad...)
}

// adoptedBotByName (api.go): an adopted bot by any of its characters' names.
func adoptedBotByName(name string) *Bot {
	if n, err := url.PathUnescape(name); err == nil {
		name = n
	}
	for _, b := range adoptedBots() {
		for _, c := range b.P.Chars {
			if strings.EqualFold(c.Name, name) {
				return b
			}
		}
	}
	return nil
}

func allInstanceBots() []*Bot {
	instBots.Lock()
	defer instBots.Unlock()
	out := make([]*Bot, 0, len(instBots.byAcct))
	for _, b := range instBots.byAcct {
		out = append(out, b)
	}
	return out
}

// instanceBotFor: this instance's Bot for the persona; a new one (run loop started, parked until it may log in).
func instanceBotFor(p *Persona) *Bot {
	instBots.Lock()
	if b := instBots.byAcct[p.Account]; b != nil {
		instBots.Unlock()
		return b
	}
	b := NewBot(p)
	instBots.byAcct[p.Account] = b
	instBots.adopted = append(instBots.adopted, b)
	login, stop := instBots.login, instBots.stop
	instBots.Unlock()
	if cur, ok := allBots.Load().([]*Bot); ok { // alchemy.go: the economy / trader scans see it too
		allBots.Store(append(append([]*Bot(nil), cur...), b))
	}
	dropMemoryCache(p) // the other instance wrote it last
	log.Printf("handoff: adopted %s (%s)", p.Name, p.Chars[0].Name)
	if stop != nil {
		go b.run(login, stop)
	}
	return b
}

// dropMemoryCache: write a dirty memory and forget it, so the instance running the account now loads it fresh.
func dropMemoryCache(p *Persona) {
	v, ok := memories.Load(p.Account)
	if !ok {
		return
	}
	m := v.(*Memory)
	m.mu.Lock()
	if m.dirty {
		if raw, err := json.Marshal(m); err == nil {
			os.WriteFile(filepath.Join(memDir, m.Account+".json"), raw, 0644)
			m.dirty = false
		}
	}
	m.mu.Unlock()
	memories.Delete(p.Account)
}

// handoffManager (main.go): releases accounts handed to the other instance, adopts those handed to this one; the main
// instance also writes the rotation candidates, the instance owning human-parties.json runs the rotation.
func handoffManager(all []*Persona, bots []*Bot, login string, stop <-chan struct{}) {
	registerInstanceBots(bots, login, stop)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var lastSnap, lastRot time.Time
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		now := time.Now()
		if handoffActive() {
			handoffStep(all, now)
		}
		if !coreInstance() && now.Sub(lastSnap) >= 2*time.Minute {
			lastSnap = now
			writeRotationCandidates(allInstanceBots(), now) // aylarotate.go
		}
		if ownsHumanParties() && now.Sub(lastRot) >= time.Minute {
			lastRot = now
			rotationTick(all, now) // aylarotate.go
		}
	}
}

func handoffStep(all []*Persona, now time.Time) {
	handoffInvalidate()
	f, _ := handoffNow()
	core := coreInstance()
	for _, b := range allInstanceBots() {
		if handoffRuns(b.P, core, true, f) {
			continue
		}
		_, k, stamp := handoffOverride(b.P, f)
		b.mu.Lock()
		on := b.Online
		b.mu.Unlock()
		if on {
			// the core instance: the alumnus says goodbye and leaves the human's party first (up to 3 min)
			if core && now.Sub(time.Unix(stamp, 0)) < 3*time.Minute && b.alumniLeave(now) {
				continue
			}
			b.kickSession()
			select {
			case b.wake <- struct{}{}:
			default:
			}
			continue
		}
		if _, parked := parkedBots.Load(b); !parked {
			select { // offline on its schedule: let the run loop reach the park check now
			case b.wake <- struct{}{}:
			default:
			}
			continue
		}
		dropMemoryCache(b.P)
		handoffMarkReleased(core, k, stamp)
	}
	// adopt: accounts handed to this instance that it doesn't run yet
	byChar := map[string]*Persona{}
	for _, p := range all {
		for _, c := range p.Chars {
			byChar[strings.ToLower(c.Name)] = p
		}
	}
	mine := f.ToMain
	if core {
		mine = f.ToCore
	}
	for k := range mine {
		if p := byChar[k]; p != nil && handoffRuns(p, core, false, f) {
			instanceBotFor(p)
		}
	}
}

// handoffMarkReleased: write this instance's "released" stamp for char k.
func handoffMarkReleased(core bool, k string, stamp int64) {
	handoffWriteMu.Lock()
	defer handoffWriteMu.Unlock()
	if core {
		var f handoffFile
		readJSONFile(handoffPath(), &f)
		if f.CoreReleased[k] >= stamp {
			return
		}
		if f.CoreReleased == nil {
			f.CoreReleased = map[string]int64{}
		}
		f.CoreReleased[k] = stamp
		writeJSONFile(handoffPath(), f)
	} else {
		var a handoffAck
		readJSONFile(handoffAckPath(), &a)
		if a.Released[k] >= stamp {
			return
		}
		if a.Released == nil {
			a.Released = map[string]int64{}
		}
		a.Released[k] = stamp
		writeJSONFile(handoffAckPath(), a)
	}
	handoffInvalidate()
	log.Printf("handoff: released %s to the other instance", k)
}

// handoffAssign (core, aylarotate.go): in moves to the core instance, out to the main instance.
func handoffAssign(in, out string, now time.Time) {
	handoffWriteMu.Lock()
	defer handoffWriteMu.Unlock()
	var f handoffFile
	readJSONFile(handoffPath(), &f)
	if f.ToCore == nil {
		f.ToCore = map[string]int64{}
	}
	if f.ToMain == nil {
		f.ToMain = map[string]int64{}
	}
	i, o := strings.ToLower(in), strings.ToLower(out)
	delete(f.ToMain, i)
	delete(f.ToCore, o)
	f.ToCore[i] = now.Unix()
	f.ToMain[o] = now.Unix()
	writeJSONFile(handoffPath(), f)
	handoffInvalidate()
}
