"""Tests for tools/system/aylalog.py (Ayla log reporter). Sample lines are copied from her real console log.
Run: python -m unittest discover -s tests/system -v   (from the repo root). No server, temp data dir only."""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import io
import os
import shutil
import sys
import tempfile
import unittest

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))

import aylalog as A  # noqa: E402

SAMPLES = {
    "[getRoute] Called with no valid closest coordinates on field izlude (24, 98) to (206, 55). Invalid coordinates: closest walkable spot to start (24, 98)": ("route-loop", "izlude"),
    "[getRoute] Called with invalid coordinates on field pay_fild01. Missing 'dest.x, dest.y'.": ("route-loop", "pay_fild01"),
    "Unable to walk from izlude (24,98) to NPC at (206,55).": ("route-unreachable-npc", "izlude"),
    "Cannot calculate a route from payon_in01 (20,129) to payon_in01 (182,16)": ("route-calc-fail", "payon_in01"),
    "Stuck at pay_dun00 (139,22), while walking from (139,22) to (139,26).": ("route-stuck", "pay_dun00"),
    "Stuck during route.": ("route-stuck", None),
    "[partylead] standing still too long, stepping to 34,45": ("standing-still", None),
    "Coordinates 5 5 are not walkable on the map payon_in01": ("unwalkable", "payon_in01"),
    "[partylead] sweep: waypoint 33,107 unreachable, next": ("unwalkable", None),
    "Can't reach or damage target": ("target-unreachable", None),
    "Unable to calculate a meetingPosition to target, dropping target": ("target-unreachable", None),
    "Disconnected from Map Server, connecting to Account Server in 5 seconds...": ("disconnect", None),
    "Timeout on Character Select Server, reconnecting...": ("disconnect", None),
    "Error: The server still recognizes your last connection": ("disconnect", None),
    "[reconnect] Login retry number 2, setting reconnect timeout to 30 seconds.": ("disconnect", None),
    "Packet Parser: Unknown switch: 0B3F": ("unknown-switch", None),
    "NPC error: Could not find an NPC..": ("npc-fail", None),
    "buyAuto index 1 (Main Gauche) failed, item doesn't exist in npc sell list.": ("buy-fail", None),
    "Error in function 'buy' (Buy Store Item)": ("buy-fail", None),
    "[aylagear] give: mail to xXRinfangXx failed: Ayla must keep 10k (has 8000z)": ("gift-held-loop", None),
    "[aylagear] give: mail to xXRinfangXx failed: no answer at step 2": ("gift-fail", None),
    "[aylagear] mailbox: no answer at step 1": ("gift-fail", None),
    "[partylead] rally in prontera over: cancelled by the mind": ("rally-fail", "prontera"),
    "[partylead] waiting for the party: Zendancer84 is 23 cells away": ("party-apart", None),
    "[partylead] going back for Zen.fox: it is not moving (12 s)": ("party-apart", None),
    "[processDead] You died, clearing AI queue, queueing dead and setting status Dead.": ("death", None),
    "Party member Zen.fox is dead.": ("party-death", None),
    "Teleporting due to insufficient HP/SP or too many aggressives": ("hp-flee", None),
    "You tried too long to teleport": ("route-stuck", None),
    "[partylead] no looting now: 4 monsters on her, HP 31%": ("mobbed", None),
    "You failed to use item: Fly Wing (3)": ("item-fail", None),
    "Can't loot item...wait...": ("loot-fail", None),
    "Failed to gather Decayed Nail (2) : Timeout": ("loot-fail", None),
    "Syntax Error in function 'move' (Move Player)": ("cmd-error", None),
    "Undefined subroutine &main::ai_buyAutoCheck called at (eval 149) line 1.": ("perl-error", None),
    "Can't find string terminator '\"' anywhere before EOF at (eval 12) line 1.": ("perl-error", None),
    "[partylead] aylagear run_start failed: oops": ("plugin-error", None),
    "[watchdog] IDLE no productive action for 60 s on payon 176,96 (holding=1, #1) -> farm gl_church": ("watchdog", "payon"),
}
NOT_PROBLEMS = [
    "[dist=unknown] [Global] Echoboy9 (): I'm stuck lol, can't find a route",
    "#1: Task::Route::getRoute at src/Task/CalcMapRoute.pm line 400",
    "  12: main::__start at openkore.pl line 216",
    "[print_callers] Printing start",
    "[cmdfile] > partylead status",
    "[partylead] no looting now: 2 monsters on her, HP 88%",
    "You have gained 85/56 (0.04%/0.03%) Exp",
    "[partylead] party is together, carrying on",
    "[Party] Ayla : say if ur overweight, we go sell",
]


def ts(t):
    import time
    return ("[%s.00] " % time.strftime("%Y.%m.%d %H:%M:%S", time.gmtime(t))).encode()


T0 = 1_794_700_000.0


class TestClassify(unittest.TestCase):
    def test_samples(self):
        for line, (kind, mp) in SAMPLES.items():
            r, m = A.classify(line)
            self.assertIsNotNone(r, line)
            self.assertEqual(r.kind, kind, line)
            if mp:
                self.assertEqual(m.groupdict().get("map"), mp, line)

    def test_not_problems(self):
        for line in NOT_PROBLEMS:
            r, _ = A.classify(line)
            self.assertIsNone(r, line)

    def test_parse_ts_utc(self):
        t, rest = A.parse_ts(b"[2026.11.15 02:53:35.70] [partylead] idle")
        self.assertEqual(rest, b"[partylead] idle")
        import calendar
        self.assertEqual(t, calendar.timegm((2026, 11, 15, 2, 53, 35, 0, 0, 0)))


class TestTracker(unittest.TestCase):
    def test_map_cell_and_watchdog_key(self):
        tr = A.Tracker()
        tr.feed(T0, "Map Change: pay_dun00.gat (139, 22)")
        evs = tr.feed(T0 + 1, "Stuck during route.")
        self.assertEqual((evs[0].map, evs[0].cell), ("pay_dun00", "139,22"))
        self.assertEqual(evs[0].key(), "ayla:route-stuck:pay_dun00")
        ev = tr.feed(T0 + 2, SAMPLES and "[watchdog] IDLE no productive action for 60 s on payon 176,96 (holding=1, #1) -> farm x")[0]
        self.assertEqual(ev.key(), "ayla-watchdog-idle-payon")
        self.assertEqual(ev.prio, "high")
        ev = tr.feed(T0 + 3, "Packet Parser: Unknown switch: 0B3F")[0]
        self.assertEqual(ev.key(), "ayla:unknown-switch:0B3F")
        ev = tr.feed(T0 + 4, "Disconnected from Map Server, x")[0]
        self.assertEqual(ev.key(), "ayla:disconnect:any")

    def test_watchdog_bare_word_is_not_a_map(self):
        # b1794736126l06: "TELEPORT allowed" set her map to "allowed"; a map is named only after "on" or before a cell
        tr = A.Tracker()
        tr.feed(T0, "Map Change: payon.gat (30, 63)")
        ev = tr.feed(T0 + 1, "[watchdog] TELEPORT allowed: party town run (Butterfly Wing to payon)")[0]
        self.assertEqual(ev.key(), "ayla-watchdog-teleport-payon")
        self.assertEqual(tr.map, "payon")
        self.assertEqual(ev.map, "payon")
        for line in ("[watchdog] PORTAL_LOOP crossed 3 times in 3 min", "[watchdog] RESPAWN refused: no wing",
                     "[watchdog] PLAN_CHANGE pay_fild04 -> pay_fild07: re-pick"):
            tr.feed(T0 + 2, line)
            self.assertEqual(tr.map, "payon", line)
        ev = tr.feed(T0 + 3, "[watchdog] STUCK izlude 24,98 for 3 min, route to payon fails")[0]
        self.assertEqual((ev.map, ev.key()), ("izlude", "ayla-watchdog-stuck-izlude"))
        self.assertEqual(tr.map, "izlude")

    def test_no_exp_on_field_every_5_min(self):
        tr = A.Tracker()
        tr.feed(T0, "Map Change: pay_dun00.gat (10, 10)")
        tr.feed(T0 + 10, "You have gained 85/56 (0.04%/0.03%) Exp")
        kinds = []
        for s in range(20, 1300, 20):
            kinds += [e.kind for e in tr.feed(T0 + s, "Finished attacking")]
        self.assertEqual(kinds.count("no-exp"), 4)        # 5, 10, 15, 20 min
        self.assertEqual([e.kind for e in tr.feed(T0 + 1310, "You have gained 1/1 (0%) Exp")], [])
        self.assertEqual(tr.noexp_marks, 0)

    def test_town_stall_not_no_exp(self):
        tr = A.Tracker()
        tr.feed(T0, "Map Change: prontera.gat (151, 32)")
        kinds = []
        for s in range(30, 1900, 30):
            kinds += [e.kind for e in tr.feed(T0 + s, "Sending request to open rodex normal mailbox.")]
        self.assertEqual(kinds.count("town-stall"), 2)   # 15 and 30 min
        self.assertNotIn("no-exp", kinds)

    def test_dead_suppresses_metrics(self):
        tr = A.Tracker()
        tr.feed(T0, "Map Change: pay_dun00.gat (10, 10)")
        tr.feed(T0 + 1, "[processDead] You died, clearing AI queue, queueing dead and setting status Dead.")
        self.assertEqual(tr.tick(T0 + 900), [])

    def test_log_gap(self):
        tr = A.Tracker()
        tr.feed(T0, "Map Change: prontera.gat (151, 32)")
        evs = tr.feed(T0 + 2000, "hello")
        self.assertIn("log-silent", [e.kind for e in evs])


class TestTail(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.p = os.path.join(self.d, "console.txt")

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)

    def w(self, data, mode="ab"):
        with open(self.p, mode) as f:
            f.write(data)

    def test_partial_binary_truncate_rotate(self):
        self.w(b"a\nb\xff\xfe\npart", "wb")
        t = A.Tail(self.p)
        self.assertEqual(t.read(), [b"a", b"b\xff\xfe"])
        self.assertIn("�", A.decode(b"b\xff\xfe"))
        self.w(b"ial\n")
        self.assertEqual(t.read(), [b"partial"])
        self.assertEqual(t.read(), [])
        self.w(b"x\n", "wb")                 # truncated
        self.assertEqual(t.read(), [b"x"])
        self.w(b"y" * 300 + b"\n", "wb")     # rotated: new head, bigger file
        self.assertEqual(t.read(), [b"y" * 300])
        self.w(b"z" * 300 + b"\nmore\n", "wb")   # rotated again, same size class
        self.assertEqual(t.read(), [b"z" * 300, b"more"])


class TestFiler(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        os.environ["LEGION_BUGQ_EXTRA"] = os.path.join(self.d, "extra.jsonl")
        os.environ["LEGION_FAKE_NOW"] = str(T0)

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)
        for k in ("LEGION_BUGQ_EXTRA", "LEGION_FAKE_NOW"):
            os.environ.pop(k, None)
        testenv.isolate()

    def ev(self, t, line, mp="pay_dun00"):
        r, m = A.classify(line)
        return A.Event(t, r.kind, mp, "1,2", line, r.prio, m.groupdict().get("extra") or "")

    def test_rate_limit_and_min_count(self):
        f = A.Filer(dry_run=True, out=io.StringIO())
        for i in range(5):
            f.add(self.ev(T0 + i, "Stuck during route."))
        for i in range(5):                   # loot-fail needs 30 in a window
            f.add(self.ev(T0 + i, "Can't loot item...wait..."))
        self.assertEqual(f.flush(T0 + 10), 1)
        self.assertEqual(f.writes[0][0], "ayla:route-stuck:pay_dun00")
        self.assertEqual(f.writes[0][1], "high")
        self.assertIn("count 5", f.writes[0][3])
        f.add(self.ev(T0 + 60, "Stuck during route."))
        self.assertEqual(f.flush(T0 + 70), 0)            # within 10 min of the last write
        f.add(self.ev(T0 + 620, "Stuck during route."))
        self.assertEqual(f.flush(T0 + 630), 1)           # next window

    def test_unknown_switch_filed_once(self):
        f = A.Filer(dry_run=True, out=io.StringIO())
        for i in range(12):
            f.add(self.ev(T0 + i, "Packet Parser: Unknown switch: 0B3F"))
        f.flush(T0 + 20)
        self.assertEqual(f.writes[0][1], "low")
        f.add(self.ev(T0 + 700, "Packet Parser: Unknown switch: 0B3F"))
        self.assertEqual(f.flush(T0 + 710), 0)           # already filed once: hits only from now on

    def test_high_at_escalates(self):
        f = A.Filer(dry_run=True, out=io.StringIO())
        for i in range(12):
            f.add(self.ev(T0 + i, "Unable to walk from izlude (24,98) to NPC at (206,55).", "izlude"))
        f.flush(T0 + 20)
        self.assertEqual(f.writes[0][1], "high")

    def test_seed_and_live_into_bugq(self):
        import bugq
        f = A.Filer()
        evs = [self.ev(T0 - 3600 + i * 60, "Stuck during route.") for i in range(30)]   # 30 min -> 3-4 windows
        self.assertEqual(f.seed(evs, T0, 6), 1)
        e = [x for x in bugq.load().values() if x.get("key") == "ayla:route-stuck:pay_dun00"][0]
        self.assertEqual(e["from"], "ayla-log")
        self.assertEqual(e["priority"], "high")
        self.assertEqual(e["bots"], 1)
        self.assertEqual(e["hits"], 30)
        wins = len(set(int(x.t // A.WINDOW) for x in evs))
        self.assertEqual(e["count"], wins)
        os.environ["LEGION_FAKE_NOW"] = str(T0 + 700)
        f.add(self.ev(T0 + 690, "Stuck during route."))
        self.assertEqual(f.flush(T0 + 700), 1)
        e = bugq.load()[e["id"]]
        self.assertEqual(e["count"], wins + 1)            # same item, bumped
        self.assertEqual(e["hits"], 31)

    def test_watchdog_line_bumps_watchdog_item(self):
        import bugq
        import json
        with open(os.environ["LEGION_BUGQ_EXTRA"], "w") as fh:
            fh.write(json.dumps({"id": "w1-ayla", "t": int(T0 - 100), "from": "ayla-watchdog", "priority": "high",
                                 "status": "open", "key": "ayla-watchdog-idle-payon", "text": "Ayla watchdog IDLE"}) + "\n")
        f = A.Filer()
        tr = A.Tracker()
        for e in tr.feed(T0, "[watchdog] IDLE no productive action for 60 s on payon 176,96 (holding=1, #1) -> farm gl_church"):
            f.add(e)
        self.assertEqual(f.flush(T0), 1)
        e = bugq.load()["w1-ayla"]
        self.assertEqual(e.get("count"), 2)
        self.assertEqual(len([x for x in bugq.load().values() if x.get("status") == "open"]), 1)

    def test_new_key_cap(self):
        f = A.Filer(dry_run=True, out=io.StringIO())
        for i in range(A.MAX_NEW_KEYS_PER_H + 10):
            f.add(self.ev(T0, "Packet Parser: Unknown switch: %04X" % i))
        n = f.flush(T0)
        self.assertLessEqual(n, A.MAX_WRITES_PER_CYCLE)
        total = n
        for k in range(1, 6):
            total += f.flush(T0 + k)
        self.assertEqual(total, A.MAX_NEW_KEYS_PER_H)


class TestMeter(unittest.TestCase):
    def test_below_par_and_town_share(self):
        tr = A.Tracker()
        tr.last_t = T0
        tr.exp_times = [T0 - i * 120 for i in range(30)]   # 0.5 kills/min
        neo = {"ayla": {"online": True, "map": "gef_fild07", "cohort": "thief|81-99|party",
                        "windows": {"1h": {"min": 52, "par_kpm": 2.455, "zph_purse": -550, "par_zph": 22373,
                                           "shares": {"hunting": 0.3, "town": 0.66}}}}}
        kinds = {e.kind: e for e in A.meter_events(neo, tr, T0)}
        self.assertIn("below-par", kinds)
        self.assertEqual(kinds["below-par"].prio, "high")
        self.assertIn("town-share", kinds)
        neo["ayla"]["windows"]["1h"]["min"] = 10
        self.assertEqual(A.meter_events(neo, tr, T0), [])


class TestDryRun(unittest.TestCase):
    def test_dry_run_writes_nothing(self):
        d = tempfile.mkdtemp()
        try:
            os.environ["LEGION_DATA"] = os.path.join(d, "data")
            p = os.path.join(d, "console.txt")
            with open(p, "wb") as f:
                f.write(ts(T0) + b"Map Change: pay_dun00.gat (139, 22)\n")
                for i in range(5):
                    f.write(ts(T0 + i) + b"Stuck at pay_dun00 (139,22), while walking from (139,22) to (139,26).\n")
            out = io.StringIO()
            old = sys.stdout
            sys.stdout = out
            try:
                rc = A.main(["--log", p, "--dry-run", "--once", "--no-meter"])
            finally:
                sys.stdout = old
            self.assertEqual(rc, 0)
            self.assertIn("WOULD FILE [high] ayla:route-stuck:pay_dun00", out.getvalue())
            self.assertFalse(os.path.exists(os.environ["LEGION_DATA"]))
        finally:
            testenv.isolate()
            shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
