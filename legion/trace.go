package main

// Per-bot combat trace (debug). Turn it on for a character with the env var LEGION_TRACE=Name1,Name2 or at runtime
// with GET /api/trace?bot=Name&on=1 (on=0 turns it off, no args lists). Every line goes to the container log with
// the prefix "TRACE <name>": attack / skill / walk packets sent (with our position, the target's interpolated
// position and the distance), damage packets where we are the source or the target, the server's "target too far"
// (0x0139) and skill failures (0x0110), and every move / stop / vanish of our current target.
// Built to find the "swordman standing still and not attacking" bug (lessons.md 2026-11-14).

import (
	"encoding/binary"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var traceNames sync.Map // lower-case char name -> true

// traceFile keeps the API toggles across redeploys (one name per line).
const traceFile = "/data/trace.txt"

func init() {
	names := os.Getenv("LEGION_TRACE")
	if raw, err := os.ReadFile(traceFile); err == nil {
		names += "," + strings.ReplaceAll(string(raw), "\n", ",")
	}
	for _, n := range strings.Split(names, ",") {
		if n = strings.TrimSpace(n); n != "" {
			traceNames.Store(strings.ToLower(n), true)
		}
	}
}

func (b *Bot) tracing() bool {
	_, ok := traceNames.Load(strings.ToLower(b.charName()))
	return ok
}

func (b *Bot) tracef(format string, args ...interface{}) {
	log.Printf("TRACE %s "+format, append([]interface{}{b.charName()}, args...)...)
}

// traceSend: called from send() (b.mu may or may not be held by the caller, so positions are read only when the
// lock can be taken right now).
func (b *Bot) traceSend(p []byte) {
	if len(p) < 2 || !b.tracing() {
		return
	}
	op := binary.LittleEndian.Uint16(p)
	pos := func(id uint32) string {
		if !b.mu.TryLock() {
			return "(pos n/a: locked)"
		}
		defer b.mu.Unlock()
		s := "me=" + itoa(b.X) + "," + itoa(b.Y)
		if e := b.ents[id]; e != nil {
			ex, ey := e.pos(time.Now())
			s += " tgt=" + itoa(ex) + "," + itoa(ey) + " d=" + itoa(dist(b.X, b.Y, ex, ey))
		} else if id != 0 && id != b.aid {
			s += " tgt=unknown"
		}
		return s
	}
	switch op {
	case 0x0437:
		if len(p) >= 7 {
			id := binary.LittleEndian.Uint32(p[2:])
			b.tracef("SEND action %d -> %d %s", p[6], id, pos(id))
		}
	case 0x0438:
		if len(p) >= 10 {
			id := binary.LittleEndian.Uint32(p[6:])
			b.tracef("SEND skill %d lv%d -> %d %s", binary.LittleEndian.Uint16(p[4:]), binary.LittleEndian.Uint16(p[2:]), id, pos(id))
		}
	case 0x0366: // skill on a cell (Hammer Fall)
		if len(p) >= 10 {
			le := binary.LittleEndian
			b.tracef("SEND ground skill %d lv%d -> %d,%d %s", le.Uint16(p[4:]), le.Uint16(p[2:]), le.Uint16(p[6:]), le.Uint16(p[8:]), pos(0))
		}
	case 0x0439:
		if len(p) >= 8 {
			b.tracef("SEND 0439 use item idx=%d", binary.LittleEndian.Uint16(p[2:]))
		}
	case 0x035f:
		if len(p) >= 5 {
			x, y, _ := decodePos(p[2:5])
			b.tracef("SEND walk -> %d,%d %s", x, y, pos(0))
		}
	}
}

// traceRecv: called from handle() with b.mu held.
func (b *Bot) traceRecv(op uint16, pk []byte, now time.Time) {
	if !b.tracing() {
		return
	}
	le := binary.LittleEndian
	tpos := func(id uint32) string {
		s := "me=" + itoa(b.X) + "," + itoa(b.Y) + " target=" + itoa(int(b.target))
		if e := b.ents[id]; e != nil {
			ex, ey := e.pos(now)
			s += " ent=" + itoa(ex) + "," + itoa(ey) + " d=" + itoa(dist(b.X, b.Y, ex, ey))
		}
		return s + " state=" + b.State
	}
	switch op {
	case 0x08c8:
		if len(pk) >= 30 {
			src, dst := le.Uint32(pk[2:]), le.Uint32(pk[6:])
			if src == b.aid || dst == b.aid {
				b.tracef("RECV 08c8 dmg %d -> %d dmg=%d type=%d %s", src, dst, int32(le.Uint32(pk[22:])), pk[29], tpos(dst))
			}
		}
	case 0x01de:
		if len(pk) >= 28 {
			src, dst := le.Uint32(pk[4:]), le.Uint32(pk[8:])
			if src == b.aid || dst == b.aid {
				b.tracef("RECV 01de skill %d %d -> %d dmg=%d %s", le.Uint16(pk[2:]), src, dst, int32(le.Uint32(pk[24:])), tpos(dst))
			}
		}
	case 0x0139:
		if len(pk) >= 16 {
			b.tracef("RECV 0139 TOO FAR target=%d at %d,%d me(server)=%d,%d range=%d | %s", le.Uint32(pk[2:]),
				le.Uint16(pk[6:]), le.Uint16(pk[8:]), le.Uint16(pk[10:]), le.Uint16(pk[12:]), le.Uint16(pk[14:]), tpos(le.Uint32(pk[2:])))
		}
	case 0x01c8:
		if len(pk) >= 15 && le.Uint32(pk[8:]) == b.aid {
			b.tracef("RECV 01c8 use item ack idx=%d item=%d left=%d ok=%d", le.Uint16(pk[2:]), le.Uint32(pk[4:]), le.Uint16(pk[12:]), pk[14])
		}
	case 0x013b:
		if len(pk) >= 4 {
			b.tracef("RECV 013b action failure type=%d (0 = no ammo, 3 = ammo ok)", le.Uint16(pk[2:]))
		}
	case 0x013c:
		if len(pk) >= 4 {
			b.tracef("RECV 013c ammo slot = idx %d", le.Uint16(pk[2:]))
		}
	case 0x0110:
		if len(pk) >= 10 {
			b.tracef("RECV 0110 skill fail skill=%d result=%d cause=%d", le.Uint16(pk[2:]), pk[8], pk[9])
		}
	case 0x01d0, 0x01e1: // spirit spheres (run 6: the combo Monk's Chain Combo never registered - trace the spheres)
		if len(pk) >= 8 && le.Uint32(pk[2:]) == b.aid {
			b.tracef("RECV %04x spheres=%d", op, le.Uint16(pk[6:]))
		}
	case 0x01d2: // ZC_COMBODELAY: aid, delay - the combo window the server opened
		if len(pk) >= 10 && le.Uint32(pk[2:]) == b.aid {
			b.tracef("RECV 01d2 combo window %d ms", le.Uint32(pk[6:]))
		}
	case 0x09cb: // ZC_USE_SKILL (no-damage skill: buffs, Call Spirits): skill, level.L, dst, src, result
		if len(pk) >= 17 && le.Uint32(pk[12:]) == b.aid {
			b.tracef("RECV 09cb skill %d lv%d -> %d ok=%d", le.Uint16(pk[2:]), int32(le.Uint32(pk[4:])), le.Uint32(pk[8:]), pk[16])
		}
	case 0x07fb, 0x0b1a: // our cast bar starts
		if len(pk) >= 24 && le.Uint32(pk[2:]) == b.aid {
			b.tracef("RECV %04x cast skill %d %d ms", op, le.Uint16(pk[14:]), le.Uint32(pk[20:]))
		}
	case 0x0087:
		if len(pk) >= 12 {
			x0, y0, x1, y1 := decodeMove(pk[6:12])
			b.tracef("RECV 0087 own walk %d,%d -> %d,%d", x0, y0, x1, y1)
		}
	case 0x0088, 0x0086, 0x0080:
		if len(pk) >= 7 {
			id := le.Uint32(pk[2:])
			if id == b.aid || (id != 0 && id == b.target) {
				extra := ""
				switch op {
				case 0x0088:
					extra = "stop at " + itoa(int(le.Uint16(pk[6:]))) + "," + itoa(int(le.Uint16(pk[8:])))
				case 0x0086:
					x0, y0, x1, y1 := decodeMove(pk[6:12])
					extra = "move " + itoa(x0) + "," + itoa(y0) + " -> " + itoa(x1) + "," + itoa(y1)
				case 0x0080:
					extra = "vanish type " + itoa(int(pk[6]))
				}
				b.tracef("RECV %04x id=%d %s | %s", op, id, extra, tpos(id))
			}
		}
	}
}

func itoa(v int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func registerTraceAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/trace", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if n := strings.ToLower(strings.TrimSpace(q.Get("bot"))); n != "" {
			if q.Get("on") == "0" {
				traceNames.Delete(n)
			} else {
				traceNames.Store(n, true)
			}
		}
		var on []string
		traceNames.Range(func(k, _ interface{}) bool { on = append(on, k.(string)); return true })
		if q.Get("bot") != "" {
			os.WriteFile(traceFile, []byte(strings.Join(on, "\n")+"\n"), 0o644)
		}
		writeJSON(w, map[string]interface{}{"tracing": on})
	})
}
