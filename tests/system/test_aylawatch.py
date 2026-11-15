"""Tests of tools/system/aylawatch.py (Ayla watch, plain code). No server, no docker, no live folder (rule G23): the
engine API and docker are fakes, and the data and Ayla-log folders are temp dirs.

The party fixtures copy the JSON that legion/partydash.go (partyView, partyMemberView, PartyMemberExtras) and
legion/meterapi.go (meterBotOut, meterBotsFile) write: the same json tags, the same omitempty rules. An engine party
has no "human" key (Party.Human is "", engineparty.go), so Ayla is an ordinary member and gets no dist_lead / exp_h.
Run: python -m unittest discover -s tests/system -v   (from the repo root)."""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import argparse
import http.client
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
from contextlib import redirect_stdout
from io import StringIO

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))

import aylawatch as W  # noqa: E402
import common as C  # noqa: E402

LOG_SAMPLE = (
    "\x1b[32mYou have gained 120/3400 base exp\x1b[0m\n"
    "You have gained 80/3400 base exp\n"
    "Target Monster Poring died\n"
    "Target Monster Lunatic died\n"
    "Map Change: prontera -> payon\n"
    "[watchdog] restart #1 sentence one\n"
    "You have died\n"
    "[watchdog] idle 300s\n"
    "[watchdog] stuck at payon\n"
)


def engine_member(name, x=100, y=150, map_="prt_fild08", online=True, leader=False, kills=0, deaths=0, zeny=500,
                  level=80):
    """partyMemberView JSON (partydash.go) for a bot in an engine party: no human, no PartyMemberExtras"""
    return {"name": name, "class": "Swordman", "level": level, "job_level": 40, "role": "melee", "online": online,
            "map": map_, "x": x, "y": y, "hp": 1.0, "sp": 1.0, "zeny": zeny, "state": "farming",
            "leader": leader, "with_leader": True, "kills": kills, "deaths": deaths}


def engine_party(members, map_="prt_fild08"):
    """partyView JSON for an engine party: "human" is omitted (omitempty) because Party.Human is ""."""
    return {"name": "Ayla's party", "leader": "Ayla", "map": map_, "checkpoint": "prontera", "formed_min": 42,
            "even_share": False, "online": sum(1 for m in members if m["online"]),
            "together": len(members), "avg_level": 80.0, "members": members}


def openkore_party(bots):
    """partyView JSON for Ayla's human-led party: the human row, then bots with the PartyMemberExtras fields"""
    human = {"name": "Ayla", "class": "human", "level": 80, "job_level": 50, "role": "leader", "online": True,
             "map": "prontera", "x": 100, "y": 150, "hp": -1, "sp": -1, "zeny": -1, "state": "played by a person / OpenKore",
             "leader": True, "human": True, "with_leader": True, "kills": 0, "deaths": 0}
    rows = [human]
    for name, dist in bots:
        m = engine_member(name)
        m.update({"dist_lead": dist, "lead_map": "prontera", "kpm": 3.5, "exp_h": 120000, "exp_min": 9.5})
        rows.append(m)
    return {"name": "Ayla core", "human": "Ayla", "leader": "Ayla", "map": "prontera", "members": rows}


def meter_bot(name, kpm=3.1, zph=54000.0, map_="prt_fild08", online=True):
    """meterBotOut JSON (meterapi.go) for one bot in meter_bots.json "bots"; there is no exp or kill count in it"""
    return {"name": name, "class": "Swordman", "lv": 80, "cohort": "mid", "instance": "", "online": online, "map": map_,
            "bucket": "fields", "party": "Ayla's party", "guild": "Starveil", "min": 60.0, "counted": True,
            "kpm": kpm, "kpm_own": kpm, "kpm_all": kpm, "zph": zph, "zph_own": zph, "deaths_h": 0.5,
            "par_kpm": 3.0, "par_zph": 50000.0, "gap_kpm": -0.1, "gap_zph": -4000.0}


class Base(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="aylawatch-test-")
        self.data = os.path.join(self.tmp, "data")
        self.logs = os.path.join(self.tmp, "logs")
        os.makedirs(self.data)
        os.makedirs(self.logs)
        self._old = {k: os.environ.get(k) for k in ("LEGION_DATA", "LEGION_AYLA_LOGS", "LEGION_FAKE_NOW")}
        os.environ["LEGION_DATA"] = self.data
        os.environ["LEGION_AYLA_LOGS"] = self.logs
        os.environ.pop("LEGION_FAKE_NOW", None)
        os.environ["LEGION_TEST"] = "1"

    def tearDown(self):
        for k, v in self._old.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        shutil.rmtree(self.tmp, ignore_errors=True)

    def write_status(self, obj):
        p = os.path.join(self.logs, "status.json")
        with open(p, "w", encoding="utf-8") as f:
            json.dump(obj, f)
        return p

    def write_meter(self, t, bots):
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": t, "window": "60m", "total": len(bots), "bots": bots}, f)

    @staticmethod
    def opener_ok(answer):
        class Resp:
            def __init__(self, raw):
                self.raw = raw

            def read(self):
                return self.raw

            def __enter__(self):
                return self

            def __exit__(self, *a):
                return False

        def op(url, timeout):
            return Resp(json.dumps(answer).encode("utf-8"))
        return op

    @staticmethod
    def opener_raising(exc):
        def op(url, timeout):
            raise exc
        return op

    @staticmethod
    def fake_run(stdout="", rc=0, exc=None):
        calls = []

        def run(cmd, **kw):
            calls.append((cmd, kw))
            if exc is not None:
                raise exc
            return subprocess.CompletedProcess(cmd, rc, stdout=stdout, stderr="")
        run.calls = calls
        return run


class TestParseLog(unittest.TestCase):
    def test_counts_from_console_lines(self):
        c = W.parse_log(LOG_SAMPLE)
        self.assertEqual(c["exp_5m"], 200)          # 120 + 80, colour codes stripped
        self.assertEqual(c["kills_5m"], 2)
        self.assertEqual(c["deaths_5m"], 1)
        self.assertEqual(c["map_changes"], 1)
        self.assertEqual(c["watchdog"], ["idle 300s", "stuck at payon"])  # the last two only

    def test_empty_text_is_all_zero(self):
        c = W.parse_log("")
        self.assertEqual((c["exp_5m"], c["kills_5m"], c["deaths_5m"], c["map_changes"], c["watchdog"]), (0, 0, 0, 0, []))
        self.assertEqual(W.parse_log(None)["exp_5m"], 0)


class TestStatusFile(Base):
    def test_reads_object_and_age(self):
        p = self.write_status({"map": "prontera", "x": 1, "y": 2, "zeny": 900, "lock_map": "prt_fild08",
                               "intent": "farm"})
        st, age, err = W.read_status(p, os.path.getmtime(p) + 7)
        self.assertEqual((st["map"], err, age), ("prontera", "", 7))

    def test_missing_file_is_an_error_not_a_crash(self):
        st, age, err = W.read_status(os.path.join(self.logs, "nope.json"), 0)
        self.assertEqual((st, age, err), ({}, -1, "status.json missing"))

    def test_bad_json_is_logged_and_reported(self):
        p = os.path.join(self.logs, "status.json")
        with open(p, "w", encoding="utf-8") as f:
            f.write("{not json")
        with self.assertLogs("aylawatch", level="WARNING"):
            st, _, err = W.read_status(p, 0)
        self.assertEqual((st, err), ({}, "status.json JSONDecodeError"))

    def test_non_object_json(self):
        p = self.write_status([1, 2])
        self.assertEqual(W.read_status(p, 0)[2], "status.json is not an object")


class TestContainerLog(Base):
    def test_success_returns_text_and_timeout(self):
        run = self.fake_run(stdout="You have gained 5/1 base exp\n")
        text, err = W.container_log(run)
        self.assertEqual((text, err), ("You have gained 5/1 base exp\n", ""))
        cmd, kw = run.calls[0]
        self.assertIn("ro-openkore-ayla", cmd)
        self.assertEqual(kw["timeout"], W.DOCKER_TIMEOUT)

    def test_timeout_is_caught(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            text, err = W.container_log(self.fake_run(exc=subprocess.TimeoutExpired(["wsl"], 60)))
        self.assertEqual((text, err), ("", "docker logs timeout"))

    def test_missing_wsl_is_caught(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            _, err = W.container_log(self.fake_run(exc=FileNotFoundError("wsl.exe")))
        self.assertEqual(err, "docker/wsl missing")

    def test_other_os_error_is_caught(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            _, err = W.container_log(self.fake_run(exc=PermissionError("denied")))
        self.assertEqual(err, "docker logs OSError")

    def test_nonzero_exit_is_reported(self):
        _, err = W.container_log(self.fake_run(stdout="", rc=1))
        self.assertEqual(err, "docker logs rc 1")


class TestEngineApi(Base):
    def test_ok_list(self):
        views, err = W.fetch_party_detail(self.opener_ok([engine_party([engine_member("Ayla", leader=True)])]),
                                          base="http://x")
        self.assertEqual(err, "")
        self.assertEqual(views[0]["name"], "Ayla's party")

    def test_urlerror_timeout_and_http_are_caught(self):
        for exc, name in ((urllib.error.URLError("refused"), "URLError"), (TimeoutError("slow"), "TimeoutError"),
                          (http.client.RemoteDisconnected("gone"), "RemoteDisconnected")):
            with self.subTest(name=name), self.assertLogs("aylawatch", level="WARNING"):
                views, err = W.fetch_party_detail(self.opener_raising(exc), base="http://x")
            self.assertIsNone(views)
            self.assertEqual(err, "api " + name)

    def test_bad_json_and_wrong_shape(self):
        def raw(_url, timeout):
            class R:
                def read(self):
                    return b"<html>"

                def __enter__(self):
                    return self

                def __exit__(self, *a):
                    return False
            return R()
        with self.assertLogs("aylawatch", level="WARNING"):
            self.assertEqual(W.fetch_party_detail(raw, base="http://x"), (None, "api bad json"))
        self.assertEqual(W.fetch_party_detail(self.opener_ok({"a": 1}), base="http://x"),
                         (None, "api answer is not a list"))

    def test_engine_party_is_found_by_member_name_not_human(self):
        eng = engine_party([engine_member("Bob"), engine_member("Ayla", leader=True)])
        ok = openkore_party([("Bob", 3)])
        other = {"name": "P2", "human": "Zen", "members": [engine_member("Cy")]}
        self.assertIs(W.ayla_party([other, eng]), eng)
        self.assertIs(W.ayla_party([other, ok]), ok)
        self.assertIsNone(W.ayla_party([other]))
        self.assertIsNone(W.ayla_party(None))


class TestMeterFile(Base):
    def test_fresh_ayla_bot_row_is_read(self):
        self.write_meter(1000, [meter_bot("Bob"), meter_bot("Ayla", kpm=4.2, zph=61000.0)])
        row, err = W.read_meter_ayla(self.data, 1100)
        self.assertEqual((row["name"], row["kpm"], row["zph"], err), ("Ayla", 4.2, 61000.0, ""))

    def test_missing_stale_and_no_row(self):
        self.assertEqual(W.read_meter_ayla(self.data, 0)[1], "meter_bots.json missing")
        self.write_meter(1000, [meter_bot("Ayla")])
        self.assertEqual(W.read_meter_ayla(self.data, 1000 + W.MAX_METER_AGE + 1)[1], "meter_bots.json stale")
        self.write_meter(1000, [meter_bot("Bob")])
        self.assertEqual(W.read_meter_ayla(self.data, 1001)[1], "meter_bots.json has no ayla row")


class TestEngineView(unittest.TestCase):
    def test_distance_from_positions_not_dist_lead(self):
        p = engine_party([engine_member("Ayla", x=100, y=150, leader=True),
                          engine_member("Bob", x=103, y=146),          # same map: max(3, 4) = 4
                          engine_member("Cy", x=100, y=150, map_="payon"),  # other map: -1
                          engine_member("Dee", x=100, y=150, online=False)])  # offline: -1
        v = W.engine_view(p, {}, None)
        self.assertEqual(v["distance"], {"Bob": 4, "Cy": -1, "Dee": -1})
        self.assertEqual(v["map"], "prt_fild08")
        self.assertEqual((v["x"], v["y"]), (100, 150))

    def test_no_previous_gives_no_deltas(self):
        v = W.engine_view(engine_party([engine_member("Ayla", leader=True), engine_member("Bob", kills=10, deaths=2)]),
                          {}, None)
        self.assertIsNone(v["kills_5m"])
        self.assertIsNone(v["deaths_5m"])
        self.assertIsNone(v["map_changes"])
        self.assertEqual((v["kills_total"], v["deaths_total"]), (10, 2))

    def test_kills_deaths_are_clamped_deltas_of_running_totals(self):
        prev = {"totals": {"Ayla": {"kills": 5, "deaths": 0}, "Bob": {"kills": 10, "deaths": 1},
                           "Cy": {"kills": 40, "deaths": 3}},
                "members_map": {"Ayla": "prt_fild08", "Bob": "prt_fild08", "Cy": "prt_fild08"}}
        p = engine_party([engine_member("Ayla", kills=7, leader=True), engine_member("Bob", kills=15, deaths=2),
                          engine_member("Cy", kills=4, deaths=3)])  # Cy's counter went down: a replaced bot
        v = W.engine_view(p, {}, prev)
        self.assertEqual(v["kills_5m"], 2 + 5 + 0)     # Ayla +2, Bob +5, Cy clamped to 0
        self.assertEqual(v["deaths_5m"], 1 + 0)        # Bob +1, Cy 0, Ayla 0

    def test_new_member_adds_no_delta_and_map_changes_are_listed(self):
        prev = {"totals": {"Ayla": {"kills": 0, "deaths": 0}}, "members_map": {"Ayla": "prt_fild08"}}
        p = engine_party([engine_member("Ayla", leader=True, map_="payon"), engine_member("Newbie", kills=30)])
        v = W.engine_view(p, {}, prev)
        self.assertEqual(v["kills_5m"], 0)
        self.assertEqual(v["map_changed"], ["Ayla"])
        self.assertEqual(v["map_changes"], 1)

    def test_rates_and_zeny_from_the_meter_row_and_api(self):
        row = meter_bot("Ayla", kpm=4.2, zph=61000.0)
        v = W.engine_view(engine_party([engine_member("Ayla", zeny=12345, leader=True), engine_member("Bob")]),
                          row, None)
        self.assertEqual((v["kpm"], v["zph"], v["deaths_h"]), (4.2, 61000.0, 0.5))
        self.assertEqual(v["zeny"], 12345)
        self.assertIsNone(v["exp_5m"])      # no exp total in the engine API for an engine party
        self.assertIsNone(v["exp_h"])

    def test_offline_ayla_gives_no_distances(self):
        p = engine_party([engine_member("Ayla", leader=True, online=False), engine_member("Bob", x=103, y=146)])
        v = W.engine_view(p, {}, None)
        self.assertEqual(v["distance"], {"Bob": -1})   # no distance from an offline Ayla's last position

    def test_exp_total_rate_and_delta_from_the_member_field(self):
        prev = {"totals": {"Ayla": {"kills": 0, "deaths": 0, "exp": 1000}}, "members_map": {"Ayla": "prt_fild08"}}
        ay = engine_member("Ayla", leader=True)
        ay["base_exp"], ay["exp_per_h"] = 1500, 360000   # partydash.go: every member carries these, engine parties too
        v = W.engine_view(engine_party([ay, engine_member("Bob")]), {}, prev)
        self.assertEqual((v["exp_5m"], v["exp_total"], v["exp_h"]), (500, 1500, 360000))

    def test_exp_absent_is_n_a_and_an_old_record_gives_no_delta(self):
        ay = engine_member("Ayla", leader=True)
        ay["base_exp"] = 1500
        old = {"totals": {"Ayla": {"kills": 0, "deaths": 0}}, "members_map": {"Ayla": "prt_fild08"}}  # no exp in it
        v = W.engine_view(engine_party([ay]), {}, old)
        self.assertIsNone(v["exp_5m"])                  # a delta from the total would be wrong here
        self.assertEqual(v["exp_total"], 1500)
        self.assertIsNone(v["exp_h"])
        v2 = W.engine_view(engine_party([engine_member("Ayla", leader=True)]), {}, None)
        self.assertIsNone(v2["exp_total"])


class TestSummaryLine(unittest.TestCase):
    def test_line_says_kpm_and_zph_are_a_60_minute_window(self):
        snap = {"t": 1000, "era": "engine", "map": "prt_fild08", "x": 1, "y": 2, "errors": [], "watchdog": [],
                "exp_5m": 5, "exp_h": 360000, "exp_total": 1500, "kpm": 4.2, "zph": 61000.0, "distance": {},
                "status_age": 3}
        line = W.summary(snap)
        self.assertIn("kpm 4.2 zph 61000.0 (60-min window)", line)
        self.assertIn("exp/h 360000 total 1500", line)


class TestOpenKoreView(unittest.TestCase):
    def test_distance_is_dist_lead_from_the_human_led_party(self):
        p = openkore_party([("Bob", 12), ("Cy", -1)])
        v = W.openkore_view({"map": "prontera", "x": 5, "y": 6, "zeny": 900}, LOG_SAMPLE, "", p)
        self.assertEqual(v["distance"], {"Bob": 12, "Cy": -1})
        self.assertEqual((v["exp_5m"], v["kills_5m"], v["deaths_5m"], v["map_changes"]), (200, 2, 1, 1))
        self.assertEqual(v["zeny"], 900)


class TestCollect(Base):
    def test_engine_snapshot_end_to_end(self):
        self.write_meter(1000, [meter_bot("Ayla", kpm=4.2, zph=61000.0)])
        p = self.write_status({"map": "prontera", "lock_map": "prt_fild08", "intent": "farm the field"})
        snap = W.collect("engine", 1100, p, self.data,
                         self.opener_ok([engine_party([engine_member("Ayla", leader=True, kills=3),
                                                       engine_member("Bob", x=101, y=150, kills=10)])]),
                         self.fake_run(stdout=LOG_SAMPLE))
        self.assertEqual(snap["era"], "engine")
        self.assertEqual(snap["distance"], {"Bob": 1})
        self.assertEqual(snap["kpm"], 4.2)
        self.assertEqual(snap["lock_map"], "prt_fild08")
        self.assertEqual(snap["watchdog"], ["idle 300s", "stuck at payon"])
        self.assertEqual(snap["errors"], [])
        self.assertTrue(snap["ok"])

    def test_openkore_snapshot_end_to_end(self):
        p = self.write_status({"map": "prontera", "x": 5, "y": 6, "zeny": 900, "lock_map": "prt_fild08"})
        snap = W.collect("openkore", 1000, p, self.data, self.opener_ok([openkore_party([("Bob", 12)])]),
                         self.fake_run(stdout=LOG_SAMPLE))
        self.assertEqual(snap["exp_5m"], 200)
        self.assertEqual(snap["distance"], {"Bob": 12})
        self.assertTrue(snap["ok"])

    def test_every_failed_source_is_listed(self):
        snap = W.collect("engine", 1000, os.path.join(self.logs, "missing.json"), self.data,
                         self.opener_raising(urllib.error.URLError("down")),
                         self.fake_run(exc=subprocess.TimeoutExpired(["wsl"], 60)))
        self.assertFalse(snap["ok"])
        for e in ("api URLError", "status.json missing", "docker logs timeout", "meter_bots.json missing"):
            self.assertIn(e, snap["errors"])

    def test_no_ayla_party_is_flagged(self):
        snap = W.collect("engine", 1100, "", self.data, self.opener_ok([engine_party([engine_member("Bob")])]),
                         self.fake_run())
        self.assertIn("no party with a member named Ayla", snap["errors"])
        self.assertIsNone(snap["map"])


class TestRunOnce(Base):
    def args(self, era="auto", no_record=False):
        return argparse.Namespace(era=era, no_record=no_record)

    def run_quiet(self, args, opener, run, now=1000):
        buf = StringIO()
        with redirect_stdout(buf):
            snap = W.run_once(args, now=now, opener=opener, run=run)
        return snap, buf.getvalue()

    def party_opener(self):
        return self.opener_ok([engine_party([engine_member("Ayla", leader=True), engine_member("Bob", kills=10)])])

    def test_auto_picks_openkore_without_the_switch(self):
        snap, line = self.run_quiet(self.args(), self.opener_ok([openkore_party([("Bob", 3)])]), self.fake_run())
        self.assertEqual(snap["era"], "openkore")
        self.assertIn("[openkore]", line)

    def test_auto_picks_engine_when_engine_parties_names_ayla(self):
        with open(os.path.join(self.data, "tuning.json"), "w", encoding="utf-8") as f:
            json.dump({"engine_parties": "Zen, Ayla"}, f)
        self.write_meter(1000, [meter_bot("Ayla")])
        snap, line = self.run_quiet(self.args(), self.party_opener(), self.fake_run())
        self.assertEqual(snap["era"], "engine")
        self.assertIn("[engine]", line)

    def test_forced_era_overrides_switch(self):
        with open(os.path.join(self.data, "tuning.json"), "w", encoding="utf-8") as f:
            json.dump({"engine_parties": "ayla"}, f)
        snap, _ = self.run_quiet(self.args(era="openkore"), self.party_opener(), self.fake_run())
        self.assertEqual(snap["era"], "openkore")

    def test_records_heartbeat_and_jsonl_and_kill_delta(self):
        self.write_meter(1000, [meter_bot("Ayla")])
        self.run_quiet(self.args(era="engine"), self.party_opener(), self.fake_run(), now=1000)
        opener = self.opener_ok([engine_party([engine_member("Ayla", leader=True),
                                               engine_member("Bob", kills=14)])])
        snap, _ = self.run_quiet(self.args(era="engine"), opener, self.fake_run(), now=1300)
        self.assertEqual(snap["kills_5m"], 4)
        self.assertEqual(len(C.read_jsonl(os.path.join(self.data, W.RECORD))), 2)
        with open(os.path.join(self.data, W.HEARTBEAT + ".heartbeat"), encoding="utf-8") as f:
            hb = f.read()
        self.assertEqual(hb.split()[1], "engine", hb)

    def test_no_record_writes_nothing(self):
        self.run_quiet(self.args(no_record=True), self.party_opener(), self.fake_run())
        self.assertFalse(os.path.exists(os.path.join(self.data, W.RECORD)))

    def test_heartbeat_names_the_error(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            self.run_quiet(self.args(era="engine"), self.opener_raising(urllib.error.URLError("x")), self.fake_run())
        with open(os.path.join(self.data, W.HEARTBEAT + ".heartbeat"), encoding="utf-8") as f:
            self.assertIn("api URLError", f.read())


class TestLoop(Base):
    class Stop(Exception):
        pass

    def test_second_loop_is_refused_by_the_lock(self):
        with open(os.path.join(self.data, W.LOCK), "w", encoding="utf-8") as f:
            f.write("1 %d\n" % int(time.time()))
        with self.assertLogs("aylawatch", level="ERROR"):
            self.assertEqual(W.main([]), 3)

    def test_loop_survives_a_source_error_and_records_it(self):
        real_run_once, real_sleep = W.run_once, W.time.sleep
        calls = []

        def boom(args):
            calls.append(1)
            raise KeyError("odd api shape")

        def stop(_secs):
            raise self.Stop()
        W.run_once, W.time.sleep = boom, stop
        try:
            with self.assertLogs("aylawatch", level="ERROR"), self.assertRaises(self.Stop):
                W.main([])
        finally:
            W.run_once, W.time.sleep = real_run_once, real_sleep
        self.assertEqual(len(calls), 1)
        with open(os.path.join(self.data, W.HEARTBEAT + ".heartbeat"), encoding="utf-8") as f:
            self.assertIn("loop error KeyError", f.read())


class TestWatchdogEntry(Base):
    def test_watchdog_checks_the_ayla_watch_heartbeat(self):
        import watchdog
        row = [a for a in watchdog.AGENTS if a["key"] == "aylawatch"]
        self.assertEqual(len(row), 1)
        self.assertEqual(row[0]["hb"], ["aylawatch.heartbeat"])
        self.assertGreaterEqual(row[0]["stale"], 360)          # 300 s round plus docker
        self.assertEqual(row[0]["lock"], "aylawatch.lock")
        self.assertEqual(row[0]["cmd"], ["tools/system/aylawatch.py"])

    def test_stale_ayla_watch_is_reported_as_down(self):
        import watchdog
        now = 1_800_000_000.0
        os.environ["LEGION_FAKE_NOW"] = str(now)
        with open(os.path.join(self.data, "aylawatch.heartbeat"), "w") as f:
            f.write(str(int(now - 900)))
        os.utime(os.path.join(self.data, "aylawatch.heartbeat"), (now - 900, now - 900))
        with open(os.path.join(self.data, "stack.up"), "w") as f:
            f.write("t")

        class Ops:
            started = []

            def start_agent(self, a):
                self.started.append(a["key"])

            def process_running(self, a):
                return False

            def docker_start(self, name):
                pass
        ops = Ops()
        res = watchdog.run_pass(now, ops, containers={}, queue={})
        row = [r for r in res["status"]["agents"] if r["key"] == "aylawatch"][0]
        self.assertEqual(row["state"], "down")
        self.assertTrue(row["action"])     # one restart per pass: the sentinel took it, the Ayla watch is queued


if __name__ == "__main__":
    unittest.main()
