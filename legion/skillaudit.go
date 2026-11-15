package main

// skillaudit.go: skills sent while the server still holds the character's "can act" delay are refused silently
// (build-and-strategy run 6). rAthena clif_parse_skill_toid answers a skill request before ud.canact_tick with
// clif_skill_fail(USESKILL_FAIL_SKILLINTERVAL), and clif_skill_fail drops that message unless the player typed
// @showdelay - so the bot never knew (the combo Monk sent 0-for-25 Chain Combos this way). canact_tick is set by:
//   - every normal attack swing of a player: tick + amotion (+ a small max-ASPD term) (unit_set_attackdelay
//     DELAY_EVENT_ATTACK) - the attack's own 0x08c8 carries amotion as attackMT;
//   - every skill: cast begin = cast time (min 100 ms), cast end = skill_delayfix = AfterCastActDelay from the skill
//     db (negative = that + amotion; min_skill_delay_limit 100 ms).
//
// This file (a) audits every skill request: sent / answered (0x01de, 0x011a, 0x09cb, 0x0117, 0x07fb) / refused
// (0x0110) / silent (nothing within 2.5 s), and counts "early" sends made before the predicted canact time;
// GET /api/skillaudit (?reset=1) shows it per skill; (b) predicts canact from those packets and pushes b.skillReady
// to it, so the fight code (which waits for skillReady) never sends a skill into the delay. tuning.json
// "skill_canact_off": true turns (b) off for a before/after measurement.

import (
	"bufio"
	"encoding/binary"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type skillPend struct {
	id int
	at time.Time
}

var skAudit = struct {
	sync.Mutex
	pend   map[*Bot][]skillPend
	stats  map[int]*[5]int64 // sent, answered, refused, silent, early
	canact map[*Bot]time.Time
	since  time.Time
}{pend: map[*Bot][]skillPend{}, stats: map[int]*[5]int64{}, canact: map[*Bot]time.Time{}, since: time.Now()}

// skill db after-cast delays (ms) per skill and level
var (
	acdOnce sync.Once
	acdTab  map[int][]int
)

func loadACD() {
	acdTab = map[int][]int{}
	f, err := os.Open(filepath.Join(*rathena, "db", "re", "skill_db.yml"))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	id, in := 0, false
	for sc.Scan() {
		l := sc.Text()
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "  - Id:"):
			id, _ = strconv.Atoi(strings.TrimSpace(t[5:]))
			in = false
		case strings.HasPrefix(l, "    AfterCastActDelay:"):
			v := strings.TrimSpace(t[len("AfterCastActDelay:"):])
			if v == "" {
				in = true // per level below
			} else if n, err := strconv.Atoi(v); err == nil {
				acdTab[id] = []int{n}
			}
		case in && strings.HasPrefix(t, "Time:"):
			n, _ := strconv.Atoi(strings.TrimSpace(t[5:]))
			acdTab[id] = append(acdTab[id], n)
		case in && strings.HasPrefix(t, "- Level:"):
		case strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "      "):
			in = false
		}
	}
}

// skillACD: the after-cast delay (ms) of a skill at a level (the last listed level for higher ones).
func skillACD(id, lv int) int {
	acdOnce.Do(loadACD)
	l := acdTab[id]
	if len(l) == 0 {
		return 0
	}
	if lv < 1 {
		lv = 1
	}
	return l[min(lv, len(l))-1]
}

// auditSend: called from send() (b.mu may or may not be held): a skill request goes out.
func (b *Bot) auditSend(p []byte) {
	if len(p) < 6 {
		return
	}
	op := binary.LittleEndian.Uint16(p)
	if op != 0x0438 && op != 0x0366 {
		return
	}
	id := int(binary.LittleEndian.Uint16(p[4:]))
	now := time.Now()
	skAudit.Lock()
	defer skAudit.Unlock()
	st := skAudit.stats[id]
	if st == nil {
		st = &[5]int64{}
		skAudit.stats[id] = st
	}
	st[0]++
	if now.Before(skAudit.canact[b]) {
		st[4]++
	}
	b.auditExpire(now)
	skAudit.pend[b] = append(skAudit.pend[b], skillPend{id, now})
}

// auditExpire (skAudit held): pending requests older than 2.5 s are silent refusals.
func (b *Bot) auditExpire(now time.Time) {
	l := skAudit.pend[b]
	k := 0
	for _, x := range l {
		if now.Sub(x.at) > 2500*time.Millisecond {
			if st := skAudit.stats[x.id]; st != nil {
				st[3]++
			}
			continue
		}
		l[k] = x
		k++
	}
	if k == 0 {
		delete(skAudit.pend, b)
	} else {
		skAudit.pend[b] = l[:k]
	}
}

// auditAnswer (skAudit held): the oldest pending request for this skill got an answer (ok) or a refusal.
func (b *Bot) auditAnswer(id int, ok bool) {
	l := skAudit.pend[b]
	for i, x := range l {
		if x.id == id {
			if st := skAudit.stats[id]; st != nil {
				if ok {
					st[1]++
				} else {
					st[2]++
				}
			}
			skAudit.pend[b] = append(l[:i], l[i+1:]...)
			return
		}
	}
}

// auditRecv (b.mu held, from handle): answers to our skills, and the can-act time they (and our swings) set.
func (b *Bot) auditRecv(op uint16, pk []byte, now time.Time) {
	le := binary.LittleEndian
	canact := time.Time{}
	switch op {
	case 0x08c8: // our normal attack swing: canact = attackMT (amotion)
		if len(pk) >= 30 && le.Uint32(pk[2:]) == b.aid && le.Uint32(pk[6:]) != b.aid {
			switch pk[29] {
			case 0, 4, 8, 9, 10, 11:
				canact = now.Add(time.Duration(le.Uint32(pk[14:])+20) * time.Millisecond)
			}
		}
	case 0x01de: // skill damage: skill, src, dst, tick, srcDelay, dstDelay, damage, level
		if len(pk) >= 30 && le.Uint32(pk[4:]) == b.aid {
			id, lv := int(le.Uint16(pk[2:])), int(le.Uint16(pk[28:]))
			skAudit.Lock()
			b.auditAnswer(id, true)
			skAudit.Unlock()
			canact = now.Add(time.Duration(acdFor(id, lv, int(le.Uint32(pk[16:])))) * time.Millisecond)
		}
	case 0x011a: // skill no damage: skill, lv, dst, src, result
		if len(pk) >= 15 && le.Uint32(pk[10:]) == b.aid {
			id, lv := int(le.Uint16(pk[2:])), int(le.Uint16(pk[4:]))
			skAudit.Lock()
			b.auditAnswer(id, true)
			skAudit.Unlock()
			canact = now.Add(time.Duration(acdFor(id, lv, 0)) * time.Millisecond)
		}
	case 0x09cb: // ZC_USE_SKILL (this server): skill, level.L, dst, src, result
		if len(pk) >= 17 && le.Uint32(pk[12:]) == b.aid {
			id, lv := int(le.Uint16(pk[2:])), int(int32(le.Uint32(pk[4:])))
			skAudit.Lock()
			b.auditAnswer(id, true)
			skAudit.Unlock()
			canact = now.Add(time.Duration(acdFor(id, lv, 0)) * time.Millisecond)
		}
	case 0x0117: // ground skill: skill, src, lv, x, y
		if len(pk) >= 10 && le.Uint32(pk[4:]) == b.aid {
			id, lv := int(le.Uint16(pk[2:])), int(le.Uint16(pk[8:]))
			skAudit.Lock()
			b.auditAnswer(id, true)
			skAudit.Unlock()
			canact = now.Add(time.Duration(acdFor(id, lv, 0)) * time.Millisecond)
		}
	case 0x07fb, 0x0b1a: // our cast bar: src, dst, x, y, skill, element, cast time (0x0b1a on this server)
		if len(pk) >= 24 && le.Uint32(pk[2:]) == b.aid {
			id := int(le.Uint16(pk[14:]))
			skAudit.Lock()
			b.auditAnswer(id, true)
			skAudit.Unlock()
			canact = now.Add(time.Duration(le.Uint32(pk[20:])+100) * time.Millisecond)
		}
	case 0x012d: // ZC_OPENSTORE: the Vending skill opened the shop setup window (vend.go)
		skAudit.Lock()
		b.auditAnswer(mcVending, true)
		skAudit.Unlock()
	case 0x0abe, 0x0091, 0x0092: // Teleport's warp list, or a map change: AL_TELEPORT (skill or wing) worked
		skAudit.Lock()
		b.auditAnswer(alTeleport, true)
		skAudit.Unlock()
	case 0x0110: // refused
		if len(pk) >= 4 {
			skAudit.Lock()
			b.auditAnswer(int(le.Uint16(pk[2:])), false)
			skAudit.Unlock()
		}
	}
	if canact.IsZero() {
		return
	}
	if canact.After(b.canactAt) {
		b.canactAt = canact
		skAudit.Lock()
		skAudit.canact[b] = canact
		skAudit.Unlock()
	}
	if !tune().SkillCanactOff && b.canactAt.After(b.skillReady) {
		b.skillReady = b.canactAt
	}
}

// acdFor: rAthena skill_delayfix in short: the db's after-cast delay (negative = that + amotion), at least 100 ms.
func acdFor(id, lv, amotion int) int {
	d := skillACD(id, lv)
	if d < 0 {
		d = -d + amotion
	}
	return max(d, 100) + 20
}

func registerSkillAuditAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/skillaudit", func(w http.ResponseWriter, r *http.Request) {
		skAudit.Lock()
		now := time.Now()
		for b := range skAudit.pend {
			b.auditExpire(now)
		}
		type row struct {
			Skill    int     `json:"skill"`
			Name     string  `json:"name"`
			Sent     int64   `json:"sent"`
			Answered int64   `json:"answered"`
			Refused  int64   `json:"refused"`
			Silent   int64   `json:"silent"`
			Early    int64   `json:"early"`
			Wasted   float64 `json:"wasted_pct"`
		}
		var rows []row
		var tot [5]int64
		for id, st := range skAudit.stats {
			rw := row{Skill: id, Name: skillName(id), Sent: st[0], Answered: st[1], Refused: st[2], Silent: st[3], Early: st[4]}
			if st[0] > 0 {
				rw.Wasted = float64(st[2]+st[3]) * 100 / float64(st[0])
			}
			rows = append(rows, rw)
			for i := range tot {
				tot[i] += st[i]
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Sent > rows[j].Sent })
		since := skAudit.since
		if r.URL.Query().Get("reset") == "1" {
			skAudit.stats, skAudit.since = map[int]*[5]int64{}, now
		}
		skAudit.Unlock()
		wasted := 0.0
		if tot[0] > 0 {
			wasted = float64(tot[2]+tot[3]) * 100 / float64(tot[0])
		}
		writeJSON(w, map[string]interface{}{"since": since, "gating": !tune().SkillCanactOff, "sent": tot[0], "answered": tot[1],
			"refused": tot[2], "silent": tot[3], "early": tot[4], "wasted_pct": wasted, "skills": rows})
	})
}

// registerDebugAPI: Go's goroutine / heap profiles at /debug/pprof/ (run 7: the live API hung on one bot's lock and
// there was no way to see which goroutine held it without killing the process). Local API only (127.0.0.1:8090).
func registerDebugAPI(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
