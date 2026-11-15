package main

// Login errors while the game server is still starting (bug b1794711155cth, 2026-11-14).
//
// rathena starts login, char and map server in that order, seconds to a minute apart (17:xx restart: login ready at
// log line 412, char at 510, map at 861). Legion logs in as soon as port 6900 answers, so during that window:
//   - dial 127.0.0.1:6900 -> "connection refused" (login server not listening yet);
//   - login server answers 0x0081 code 1 "server closed" ("Connection refused: there is no char-server online",
//     18 accounts that restart);
//   - char server answers the char select with 0x0840 HC_NOTIFY_ACCESSIBLE_MAPNAME (no map server holds the
//     character's map yet; OpenKore logs it as "Unknown switch 0840"). The char-select loop ignored 0x0840 and
//     waited for 0x0ac5 until its 10 s read timed out ("char select: read tcp ...: i/o timeout").
// The bots retried and got in, but LastErr was only ever set, never cleared on a good login, so QA saw
// "login refused (0081, code 1)" on bots that had been online for an hour.
//
// Now: 0x0840 ends the char select at once with a clear reason, 0x0081 codes are named, and a successful login moves
// the last error to PrevErr (shown as prev_error, with its time) instead of leaving it as the current error.

import (
	"fmt"
	"strings"
	"time"
)

// loginRefusedReason: rAthena's AC_REFUSE_LOGIN / SC_NOTIFY_BAN codes as the login and char servers use them.
func loginRefusedReason(code byte) string {
	switch code {
	case 0:
		return "unregistered id"
	case 1:
		return "server closed (char server not up yet)"
	case 2:
		return "someone has already logged in with this id"
	case 3:
		return "time gap / rejected"
	case 4:
		return "server jammed"
	case 6:
		return "banned"
	case 8:
		return "server still recognises the last login"
	case 9:
		return "ip capacity full"
	}
	return ""
}

// loginRefusedErr formats a refused login; the "login refused (%04x, code %d" prefix is kept.
func loginRefusedErr(op uint16, code byte) error {
	if r := loginRefusedReason(code); r != "" {
		return fmt.Errorf("login refused (%04x, code %d: %s)", op, code, r)
	}
	return fmt.Errorf("login refused (%04x, code %d)", op, code)
}

// accessibleMapsErr reads 0x0840 HC_NOTIFY_ACCESSIBLE_MAPNAME (u16 op, u16 len, then {i32 status, char map[16]}
// entries; status 1 = no map server holds that map). The char server sends it instead of 0x0ac5 when the
// character's map is not on any connected map server - at startup, before the map server has connected.
func accessibleMapsErr(pk []byte) error {
	down, maps := 0, []string{}
	for o := 4; o+20 <= len(pk); o += 20 {
		if le.Uint32(pk[o:]) != 0 {
			down++
		}
		if m := trimGat(cstr(pk[o+4 : o+20])); m != "" && len(maps) < 3 {
			maps = append(maps, m)
		}
	}
	s := "char select: map server not ready (0840"
	if down > 0 {
		s += fmt.Sprintf(", %d maps unavailable", down)
	}
	if len(maps) > 0 {
		s += ": " + strings.Join(maps, ",")
	}
	return fmt.Errorf("%s)", s)
}

// serverStarting: the error came from a game server that is not fully up yet (not from this account).
func serverStarting(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "code 1:") || strings.Contains(s, "(0840") || strings.Contains(s, "connection refused")
}

// noteLoginOK: called with b.mu held after a successful login. The last error belongs to an earlier attempt or
// session: keep it as PrevErr (with its time), so the current error field only shows what is wrong now.
func (b *Bot) noteLoginOK(now time.Time) {
	if b.LastErr != "" {
		b.PrevErr, b.PrevErrAt = b.LastErr, now
		b.LastErr = ""
	}
}

// prevErrAt: the API's time of the login that cleared PrevErr ("" when there is none).
func prevErrAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
