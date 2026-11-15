"""Tests of tools/system/aylawatch.py (Ayla watch, plain code). No server, no docker, no live folder (rule G23): the
engine API and docker are fakes, and the data and Ayla-log folders are temp dirs.
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


def party(members, human="Ayla", map_="prt_fild08"):
    """an engine API party view: the leader row (human) plus bot members"""
    rows = [{"name": human, "human": True, "map": map_, "x": 100, "y": 150, "zeny": -1, "level": 80}]
    rows += members
    return {"name": "P1", "human": human, "map": map_, "members": rows}


def bot(name, map_="prt_fild08", dist=3, kills=10, deaths=1, zeny=500, exp_h=0):
    return {"name": name, "human": False, "map": map_, "dist_lead": dist, "kills": kills, "deaths": deaths,
            "zeny": zeny, "exp_h": exp_h}


class Base(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="aylawatch-test-")
        self.data = os.path.join(self.tmp, "data")
        self.logs = os.path.join(self.tmp, "logs")
        os.makedirs(self.data)
        os.makedirs(self.logs)
        self._old = {k: os.environ.get(k) for k in ("LEGION_DATA", "LEGION_AYLA_LOGS")}
        os.environ["LEGION_DATA"] = self.data
        os.environ["LEGION_AYLA_LOGS"] = self.logs
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
            return Resp(answer.encode("utf-8") if isinstance(answer, str) else json.dumps(answer).encode("utf-8"))
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
    def test_reads_object(self):
        p = self.write_status({"map": "prontera", "x": 1, "y": 2, "zeny": 900})
        st, age, err = W.read_status(p, os.path.getmtime(p) + 7)
        self.assertEqual((st["map"], err), ("prontera", ""))
        self.assertEqual(age, 7)

    def test_missing_file_is_an_error_not_a_crash(self):
        st, age, err = W.read_status(os.path.join(self.logs, "nope.json"), 0)
        self.assertEqual((st, age, err), ({}, -1, "status.json missing"))

    def test_bad_json_is_logged_and_reported(self):
        p = os.path.join(self.logs, "status.json")
        with open(p, "w", encoding="utf-8") as f:
            f.write("{not json")
        with self.assertLogs("aylawatch", level="WARNING"):
            st, age, err = W.read_status(p, 0)
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
        self.assertIn("--since", cmd)
        self.assertEqual(kw["timeout"], W.DOCKER_TIMEOUT)

    def test_timeout_is_caught(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            text, err = W.container_log(self.fake_run(exc=subprocess.TimeoutExpired(["wsl"], 60)))
        self.assertEqual((text, err), ("", "docker logs timeout"))

    def test_missing_wsl_is_caught(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            text, err = W.container_log(self.fake_run(exc=FileNotFoundError("wsl.exe")))
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
        views, err = W.fetch_party_detail(self.opener_ok([party([bot("Bob")])]), base="http://x")
        self.assertEqual(err, "")
        self.assertEqual(views[0]["human"], "Ayla")

    def test_urlerror_timeout_and_http_are_caught(self):
        for exc, name in ((urllib.error.URLError("refused"), "URLError"), (TimeoutError("slow"), "TimeoutError"),
                          (http.client.RemoteDisconnected("gone"), "RemoteDisconnected")):
            with self.subTest(name=name), self.assertLogs("aylawatch", level="WARNING"):
                views, err = W.fetch_party_detail(self.opener_raising(exc), base="http://x")
            self.assertIsNone(views)
            self.assertEqual(err, "api " + name)

    def test_bad_json_and_wrong_shape(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            self.assertEqual(W.fetch_party_detail(self.opener_ok("<html>"), base="http://x"), (None, "api bad json"))
        self.assertEqual(W.fetch_party_detail(self.opener_ok({"a": 1}), base="http://x"),
                         (None, "api answer is not a list"))

    def test_ayla_party_is_picked_by_human_name(self):
        other = {"name": "P2", "human": "Zen", "members": []}
        self.assertEqual(W.ayla_party([other, party([])])["human"], "Ayla")
        self.assertIsNone(W.ayla_party([other]))
        self.assertIsNone(W.ayla_party(None))


class TestMeterFile(Base):
    def test_fresh_ayla_row_is_read(self):
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": 1000, "ayla": {"exp_h": 3000, "kills": 7, "zeny": 42, "map": "prt_fild08"}}, f)
        row, err = W.read_meter_ayla(self.data, 1100)
        self.assertEqual((row["exp_h"], err), (3000, ""))

    def test_missing_stale_and_no_row(self):
        self.assertEqual(W.read_meter_ayla(self.data, 0)[1], "meter_bots.json missing")
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": 1000, "ayla": {"exp_h": 1}}, f)
        self.assertEqual(W.read_meter_ayla(self.data, 1000 + W.MAX_METER_AGE + 1)[1], "meter_bots.json stale")
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": 1000, "bots": []}, f)
        self.assertEqual(W.read_meter_ayla(self.data, 1001)[1], "meter_bots.json has no ayla row")


class TestEngineView(unittest.TestCase):
    def test_no_previous_snapshot_gives_no_deltas(self):
        v = W.engine_view(party([bot("Bob", kills=10), bot("Cy", kills=4, deaths=2)]), {}, None)
        self.assertIsNone(v["kills_5m"])
        self.assertIsNone(v["map_changes"])
        self.assertEqual(v["kills_total"], 14)
        self.assertEqual(v["deaths_total"], 3)
        self.assertEqual(v["distance"], {"Bob": 3, "Cy": 3})

    def test_kills_deaths_and_map_changes_against_previous(self):
        prev = {"kills_total": 10, "deaths_total": 1, "members_map": {"Ayla": "prt_fild08", "Bob": "prt_fild08"}}
        p = party([bot("Bob", map_="payon", kills=15, deaths=1), bot("Cy", kills=4, deaths=2)])
        v = W.engine_view(p, {}, prev)
        self.assertEqual(v["kills_5m"], 9)           # 19 - 10
        self.assertEqual(v["deaths_5m"], 2)          # 3 - 1
        self.assertEqual(v["map_changed"], ["Bob"])
        self.assertEqual(v["map_changes"], 1)

    def test_zeny_falls_back_to_meter_and_exp_adds_own_row(self):
        v = W.engine_view(party([bot("Bob", exp_h=600)]), {"zeny": 4242, "exp_h": 3000}, None)
        self.assertEqual(v["zeny"], 4242)
        self.assertEqual(v["exp_h"], 3600)
        self.assertEqual(v["exp_5m"], 300)          # 3600 * 5 / 60

    def test_leader_zeny_wins_when_known(self):
        p = party([bot("Bob")])
        p["members"][0]["zeny"] = 77
        self.assertEqual(W.engine_view(p, {"zeny": 4242}, None)["zeny"], 77)


class TestCollect(Base):
    def test_openkore_snapshot_from_logs_status_and_api(self):
        p = self.write_status({"map": "prontera", "x": 5, "y": 6, "lock_map": "prt_fild08", "zeny": 900,
                               "intent": "farm"})
        snap = W.collect("openkore", 1000, p, self.data, self.opener_ok([party([bot("Bob", dist=12)])]),
                         self.fake_run(stdout=LOG_SAMPLE))
        self.assertEqual(snap["map"], "prontera")
        self.assertEqual(snap["zeny"], 900)
        self.assertEqual(snap["exp_5m"], 200)
        self.assertEqual(snap["distance"], {"Bob": 12})
        self.assertEqual(snap["errors"], [])
        self.assertTrue(snap["ok"])

    def test_every_failed_source_is_listed(self):
        snap = W.collect("openkore", 1000, os.path.join(self.logs, "missing.json"), self.data,
                         self.opener_raising(urllib.error.URLError("down")),
                         self.fake_run(exc=subprocess.TimeoutExpired(["wsl"], 60)))
        self.assertFalse(snap["ok"])
        self.assertIn("api URLError", snap["errors"])
        self.assertIn("status.json missing", snap["errors"])
        self.assertIn("docker logs timeout", snap["errors"])

    def test_engine_snapshot_from_api_and_meter(self):
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": 1000, "ayla": {"exp_h": 1200, "kills": 3, "zeny": 9, "map": "prt_fild08"}}, f)
        snap = W.collect("engine", 1100, os.path.join(self.logs, "x.json"), self.data,
                         self.opener_ok([party([bot("Bob", kills=5)])]), self.fake_run())
        self.assertEqual(snap["era"], "engine")
        self.assertEqual(snap["exp_h"], 1200)
        self.assertEqual(snap["kills_total"], 5)
        self.assertEqual(snap["errors"], [])   # the meter file was written above: fresh, so no error

    def test_engine_snapshot_with_no_party_is_flagged(self):
        snap = W.collect("engine", 1100, "", self.data, self.opener_ok([]), self.fake_run())
        self.assertIn("no party with human Ayla", snap["errors"])
        self.assertIsNone(snap["map"])
        self.assertFalse(snap["ok"])


class TestRunOnce(Base):
    def args(self, era="auto", no_record=False):
        return argparse.Namespace(era=era, no_record=no_record)

    def run_quiet(self, args, opener, run, now=1000):
        buf = StringIO()
        with redirect_stdout(buf):
            snap = W.run_once(args, now=now, opener=opener, run=run)
        return snap, buf.getvalue()

    def test_auto_picks_openkore_without_the_switch(self):
        snap, line = self.run_quiet(self.args(), self.opener_ok([party([])]), self.fake_run())
        self.assertEqual(snap["era"], "openkore")
        self.assertIn("[openkore]", line)

    def test_auto_picks_engine_when_engine_parties_names_ayla(self):
        with open(os.path.join(self.data, "tuning.json"), "w", encoding="utf-8") as f:
            json.dump({"engine_parties": "Zen, Ayla"}, f)
        with open(os.path.join(self.data, W.METER_BOTS), "w", encoding="utf-8") as f:
            json.dump({"t": 1000, "ayla": {"exp_h": 100}}, f)
        snap, line = self.run_quiet(self.args(), self.opener_ok([party([bot("Bob")])]), self.fake_run())
        self.assertEqual(snap["era"], "engine")
        self.assertIn("[engine]", line)

    def test_forced_era_overrides_switch(self):
        with open(os.path.join(self.data, "tuning.json"), "w", encoding="utf-8") as f:
            json.dump({"engine_parties": "ayla"}, f)
        snap, _ = self.run_quiet(self.args(era="openkore"), self.opener_ok([party([])]), self.fake_run())
        self.assertEqual(snap["era"], "openkore")

    def test_records_heartbeat_and_jsonl_and_kill_delta(self):
        opener = self.opener_ok([party([bot("Bob", kills=10)])])
        self.run_quiet(self.args(era="engine"), opener, self.fake_run(), now=1000)
        opener = self.opener_ok([party([bot("Bob", kills=14)])])
        snap, _ = self.run_quiet(self.args(era="engine"), opener, self.fake_run(), now=1300)
        self.assertEqual(snap["kills_5m"], 4)
        rows = C.read_jsonl(os.path.join(self.data, W.RECORD))
        self.assertEqual(len(rows), 2)
        with open(os.path.join(self.data, W.HEARTBEAT + ".heartbeat"), encoding="utf-8") as f:
            hb = f.read()
        self.assertEqual(hb.split()[1], "engine", hb)     # the epoch is the real clock; the note names the era

    def test_no_record_writes_nothing(self):
        self.run_quiet(self.args(no_record=True), self.opener_ok([party([])]), self.fake_run())
        self.assertFalse(os.path.exists(os.path.join(self.data, W.RECORD)))

    def test_heartbeat_names_the_error(self):
        with self.assertLogs("aylawatch", level="WARNING"):
            self.run_quiet(self.args(era="engine"), self.opener_raising(urllib.error.URLError("x")), self.fake_run())
        with open(os.path.join(self.data, W.HEARTBEAT + ".heartbeat"), encoding="utf-8") as f:
            hb = f.read()
        self.assertIn("api URLError", hb)


class TestGuards(Base):
    def test_logs_folder_in_live_checkout_is_refused_under_test(self):
        os.environ["LEGION_AYLA_LOGS"] = C.LIVE_AYLA_LOGS
        with self.assertRaises(RuntimeError):
            W.logs_dir()

    def test_temp_logs_folder_is_accepted(self):
        self.assertEqual(os.path.abspath(W.logs_dir()), os.path.abspath(self.logs))

    def test_summary_line_shows_errors(self):
        line = W.summary({"t": 0, "era": "openkore", "errors": ["api URLError"], "map": "x", "x": 1, "y": 2,
                          "exp_5m": 0, "kills_5m": 0, "deaths_5m": 0, "map_changes": 0, "zeny": 5, "distance": {}})
        self.assertIn("ERR api URLError", line)


if __name__ == "__main__":
    unittest.main()
