package main

// Owner 2026-11-14 ("in dashboard, bring the party panel up, and display what effect each member is having"): the
// party dashboard shows each member's active status effects (buffs such as Blessing, Increase AGI, Kyrie, Magnificat,
// Impositio, Assumptio, Angelus; debuffs such as poison, stun, curse, silence) with the time left when known.
//
// Engine bots: efstRecv notes the status icons the server sends about the bot itself (ZC_SC_ICON 0x0196, 0x043f with
// the remaining ms, 0x0983 with total and remaining ms) in efstBook; the body/health state of 0x0229 (poison.go:
// b.opt1 stone/freeze/stun/sleep, b.opt2 poison/curse/silence/blind...) adds the debuffs that have no icon.
// Ayla: statusout.pl exports OpenKore's $char->{statuses} ("effects" in status.json, humanstats.go).
// Read-only display: nothing here changes behaviour.

import (
	"encoding/binary"
	"sort"
	"strings"
	"sync"
	"time"
)

// effectView: one active status effect on a party member (partydash.go, /api/human-party).
type effectView struct {
	Name   string `json:"name"`
	LeftS  int    `json:"left_s"` // seconds left, -1 = unknown
	Debuff bool   `json:"debuff,omitempty"`
}

type efstEntry struct {
	until time.Time // zero = duration unknown
}

// efstBook: per-bot status icons (leaf lock, taken with b.mu held).
var efstBook = struct {
	sync.Mutex
	m map[*Bot]map[int]efstEntry
}{m: map[*Bot]map[int]efstEntry{}}

type efstInfo struct {
	name   string
	debuff bool
}

// efstNames: rAthena/kRO EFST ids (OpenKore tables/STATUS_id_handle.txt) worth showing; others are skipped.
var efstNames = map[int]efstInfo{
	0: {"Provoke", false}, 1: {"Endure", false}, 2: {"Two-Hand Quicken", false}, 3: {"Improve Concentration", false},
	4: {"Hiding", false}, 5: {"Cloaking", false}, 6: {"Enchant Poison", false}, 7: {"Poison React", false},
	8: {"Quagmire", true}, 9: {"Angelus", false}, 10: {"Blessing", false}, 11: {"Signum Crucis", true},
	12: {"Increase AGI", false}, 13: {"Decrease AGI", true}, 14: {"Slow Poison", false}, 15: {"Impositio Manus", false},
	16: {"Suffragium", false}, 17: {"Aspersio", false}, 18: {"Benedictio", false}, 19: {"Kyrie Eleison", false},
	20: {"Magnificat", false}, 21: {"Gloria", false}, 22: {"Lex Aeterna", true}, 23: {"Adrenaline Rush", false},
	24: {"Weapon Perfection", false}, 25: {"Power-Thrust", false}, 26: {"Maximize Power", false},
	29: {"Play Dead", false}, 30: {"Loud Exclamation", false}, 31: {"Energy Coat", false},
	32: {"Armor broken", true}, 33: {"Weapon broken", true}, 35: {"Weight 50%", true}, 36: {"Weight 90%", true},
	37: {"Concentration Potion", false}, 38: {"Awakening Potion", false}, 39: {"Berserk Potion", false},
	41: {"Speed Potion", false}, 43: {"Counter Attack", false}, 45: {"Ankle Snare", true},
	50: {"Weapon stripped", true}, 51: {"Shield stripped", true}, 52: {"Armor stripped", true}, 53: {"Helm stripped", true},
	54: {"Chemical Protection Weapon", false}, 55: {"Chemical Protection Shield", false},
	56: {"Chemical Protection Armor", false}, 57: {"Chemical Protection Helm", false},
	58: {"Auto Guard", false}, 59: {"Reflect Shield", false}, 60: {"Devotion", false}, 61: {"Providence", false},
	62: {"Defender", false}, 63: {"Magic Rod", false}, 64: {"Endow", false}, 65: {"Auto Spell", false},
	68: {"Spear Quicken", false}, 70: {"A Whistle", false}, 71: {"Assassin Cross of Sunset", false},
	72: {"A Poem of Bragi", false}, 73: {"Apple of Idun", false}, 74: {"Humming", false}, 75: {"Don't Forget Me", true},
	76: {"Fortune's Kiss", false}, 77: {"Service for You", false}, 85: {"Blade Stop", false},
	86: {"Fury", false}, 87: {"Steel Body", false}, 90: {"Endow Fire", false}, 91: {"Endow Water", false},
	92: {"Endow Wind", false}, 93: {"Endow Earth", false}, 95: {"Stop", true}, 103: {"Aura Blade", false},
	104: {"Parrying", false}, 105: {"Concentration", false}, 106: {"Tension Relax", false}, 107: {"Frenzy", false},
	108: {"Sacrifice", false}, 110: {"Assumptio", false}, 113: {"Mystical Amplification", false},
	114: {"Enchant Deadly Poison", false}, 115: {"True Sight", false}, 116: {"Wind Walk", false},
	117: {"Meltdown", false}, 118: {"Cart Boost", false}, 124: {"Bleeding", true}, 125: {"Joint Beat", true},
	126: {"Mind Breaker", true},
}

// body state (opt1) and health state (opt2 bits) of 0x0229 (rAthena OPT1_* / OPT2_*).
var opt1Names = map[uint16]string{1: "Petrified", 2: "Frozen", 3: "Stunned", 4: "Sleeping", 6: "Petrifying"}

var opt2Names = []struct {
	bit  uint16
	name string
}{{0x1, "Poisoned"}, {0x2, "Cursed"}, {0x4, "Silenced"}, {0x8, "Confused"}, {0x10, "Blinded"},
	{0x40, "Bleeding"}, {0x80, "Deadly Poison"}, {0x100, "Fear"}}

// efstRecv (b.mu held, every packet): status icons about the bot itself.
func (b *Bot) efstRecv(op uint16, pk []byte, now time.Time) {
	var remain int64 = -1
	switch {
	case op == 0x0196 && len(pk) >= 9:
	case op == 0x043f && len(pk) >= 13:
		remain = int64(int32(binary.LittleEndian.Uint32(pk[9:])))
	case op == 0x0983 && len(pk) >= 17:
		remain = int64(int32(binary.LittleEndian.Uint32(pk[13:])))
	default:
		return
	}
	efst, id, on := int(binary.LittleEndian.Uint16(pk[2:])), binary.LittleEndian.Uint32(pk[4:]), pk[8] != 0
	if b.aid == 0 || id != b.aid {
		return
	}
	efstNote(b, efst, on, remain, now)
}

// efstNote records one icon on/off for b. remain in ms; <= 0 or 9999 (the client's "no timer") = unknown.
func efstNote(b *Bot, efst int, on bool, remain int64, now time.Time) {
	efstBook.Lock()
	defer efstBook.Unlock()
	book := efstBook.m[b]
	if !on {
		delete(book, efst)
		return
	}
	if book == nil {
		book = map[int]efstEntry{}
		efstBook.m[b] = book
	}
	e := efstEntry{}
	if remain > 0 && remain != 9999 {
		e.until = now.Add(time.Duration(remain) * time.Millisecond)
	}
	book[efst] = e
}

// effectsOf (b.mu held): b's active effects, debuffs first, then by name. Offline: none (the book is cleared; the
// server sends every icon again at the next login).
func (b *Bot) effectsOf(now time.Time) []effectView {
	var out []effectView
	efstBook.Lock()
	if !b.Online {
		delete(efstBook.m, b)
	} else {
		for id, e := range efstBook.m[b] {
			if !e.until.IsZero() && !now.Before(e.until) {
				delete(efstBook.m[b], id) // ran out (the off icon was missed)
				continue
			}
			info, ok := efstNames[id]
			if !ok {
				continue
			}
			v := effectView{Name: info.name, LeftS: -1, Debuff: info.debuff}
			if !e.until.IsZero() {
				v.LeftS = int(e.until.Sub(now).Seconds())
			}
			out = append(out, v)
		}
	}
	efstBook.Unlock()
	if !b.Online || b.Dead {
		return sortEffects(out)
	}
	if now.Sub(b.opt2At) < poisonStale {
		if n, ok := opt1Names[b.opt1]; ok {
			out = append(out, effectView{Name: n, LeftS: -1, Debuff: true})
		}
		for _, o := range opt2Names {
			if b.opt2&o.bit != 0 {
				out = append(out, effectView{Name: o.name, LeftS: -1, Debuff: true})
			}
		}
	}
	return sortEffects(out)
}

func sortEffects(out []effectView) []effectView {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Debuff != out[j].Debuff {
			return out[i].Debuff
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// humanEffect: one entry of status.json "effects" (statusout.pl, from OpenKore's $char->{statuses}).
type humanEffect struct {
	Handle string  `json:"handle"` // EFST_BLESSING, HEALTHSTATE_POISON, BODYSTATE_STUN...
	Name   string  `json:"name"`   // OpenKore's statusnametable name ("" = unknown)
	LeftS  float64 `json:"left_s"` // at the file's ts; -1 = unknown
}

// humanEffectDebuffs: OpenKore handles that are harmful (body/health states are all harmful except Angelus).
var humanEffectDebuffs = map[string]bool{"EFST_QUAGMIRE": true, "EFST_DEC_AGI": true, "EFST_LEXAETERNA": true,
	"EFST_CRUCIS": true, "EFST_BROKENARMOR": true, "EFST_BROKENWEAPON": true, "EFST_ANKLESNARE": true,
	"EFST_NOEQUIPWEAPON": true, "EFST_NOEQUIPSHIELD": true, "EFST_NOEQUIPARMOR": true, "EFST_NOEQUIPHELM": true,
	"EFST_STOP": true, "EFST_BLOODING": true, "EFST_JOINTBEAT": true, "EFST_MINDBREAKER": true,
	"EFST_WEIGHTOVER50": true, "EFST_WEIGHTOVER90": true, "EFST_DONTFORGETME": true}

// humanEffectsView turns status.json effects into dashboard rows, the time left aged from the file's ts.
func humanEffectsView(s humanStatus, now time.Time) []effectView {
	var out []effectView
	age := now.Sub(time.Unix(s.TS, 0)).Seconds()
	if age < 0 {
		age = 0
	}
	for _, e := range s.Effects {
		h := e.Handle
		if h == "" || strings.HasSuffix(h, "_DELAY") || h == "BODYSTATE_UNDEAD" {
			continue
		}
		name := e.Name
		if name == "" {
			name = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(h,
				"EFST_"), "HEALTHSTATE_"), "BODYSTATE_")), "_", " ")
		}
		v := effectView{Name: name, LeftS: -1, Debuff: humanEffectDebuffs[h] ||
			(strings.HasPrefix(h, "BODYSTATE_") || strings.HasPrefix(h, "HEALTHSTATE_")) && h != "HEALTHSTATE_ANGELUS"}
		if e.LeftS >= 0 {
			left := e.LeftS - age
			if left <= 0 {
				continue // ran out since the file was written
			}
			v.LeftS = int(left)
		}
		out = append(out, v)
	}
	return sortEffects(out)
}
