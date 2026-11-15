"""Tests for tools/system/watchdog.py and the heartbeat changes (bugq watch, keeper, unwall, dashboard).
Offline: fake heartbeat files in a temp data dir, a fake clock, fake ops (no docker, no processes).
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "tools", "dashboard"))

T0 = 1_800_000_000.0
ALL = ["sentinel", "partycheck", "dispatcher", "aylalog", "bugq-watch", "unwall", "keeper", "dashboard"]
HB_FILE = {"dispatcher": "dispatch.heartbeat", "bugq-watch": "bugq-watch.heartbeat", "unwall": "unwall.heartbeat",
           "keeper": "keeper.heartbeat", "dashboard": "dashboard.heartbeat", "sentinel": "sentinel.heartbeat",
           "partycheck": "partycheck.heartbeat", "aylalog": "aylalog.heartbeat"}
OK_CONTAINERS = {n: ("running", "Up 3 hours") for n in
                 ["ro-db", "ro-rathena", "ro-agency", "ro-openkore-ayla", "ro-legion", "ro-legion-core"]}


class FakeOps:
    def __init__(self):
        self.started, self.docker = [], []

    def start_agent(self, a):
        self.started.append(a["key"])

    def docker_start(self, name):
        self.docker.append(name)


class Env(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        os.environ["LEGION_FAKE_NOW"] = str(T0)
        os.environ.pop("LEGION_BUGQ_EXTRA", None)

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)
        for k in ("LEGION_DATA", "LEGION_FAKE_NOW"):
            os.environ.pop(k, None)

    def put(self, name, content="", age=0, base=T0):
        p = os.path.join(self.d, name)
        with open(p, "w") as f:
            f.write(content)
        os.utime(p, (base - age, base - age))

    def beat_all(self, age=10, skip=(), notes=None, base=T0):
        """every heartbeat written at time base - age, except the skipped ones"""
        notes = notes or {}
        for k, fn in HB_FILE.items():
            if k in skip:
                continue
            self.put(fn, str(int(base - age)) + ((" " + notes[k]) if k in notes else ""), age=age, base=base)

    def stack(self, up=True, hold=False):
        for name, on in (("stack.up", up), ("stack.hold", hold)):
            p = os.path.join(self.d, name)
            if on:
                self.put(name, "t")
            elif os.path.exists(p):
                os.remove(p)

    def queue_rows(self):
        import bugq
        return list(bugq.load().values())

    def run_at(self, t, ops, containers=OK_CONTAINERS, dry=False):
        import watchdog
        os.environ["LEGION_FAKE_NOW"] = str(t)
        return watchdog.run_pass(t, ops, dry=dry, containers=containers)


class TestWatchdogPass(Env):
    def test_all_healthy_no_outage_no_restart(self):
        self.stack(up=True)
        self.beat_all(age=10)
        ops = FakeOps()
        res = self.run_at(T0, ops)
        self.assertEqual(res["filed"], [])
        self.assertEqual(ops.started, [])
        self.assertEqual(ops.docker, [])
        st = res["status"]
        self.assertEqual(st["summary"]["agents_ok"], 8)
        self.assertEqual(st["summary"]["containers_up"], 6)
        self.assertEqual(self.queue_rows(), [])

    def test_stale_script_is_filed_and_restarted_once(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        res = self.run_at(T0, ops)
        self.assertEqual(ops.started, ["sentinel"])
        rows = self.queue_rows()
        outs = [r for r in rows if r.get("priority") == "outage"]
        self.assertEqual(len(outs), 1)
        self.assertIn("Sentinel is down", outs[0]["text"])
        self.assertEqual(outs[0]["key"], "watchdog:sentinel")
        self.assertTrue(any(a["action"].startswith("restarted") for a in res["actions"]))
        # 2 minutes later: still down, throttled, and no second outage item (the others keep beating)
        self.beat_all(age=10, skip=("sentinel",), base=T0 + 120)
        self.run_at(T0 + 120, ops)
        self.assertEqual(ops.started, ["sentinel"])
        self.assertEqual(len([r for r in self.queue_rows() if r.get("priority") == "outage"]), 1)
        # 11 minutes later the restart is allowed again
        self.beat_all(age=10, skip=("sentinel",), base=T0 + 660)
        self.run_at(T0 + 660, ops)
        self.assertEqual(ops.started, ["sentinel", "sentinel"])

    def test_recovery_closes_the_outage(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        self.run_at(T0, ops)
        self.beat_all(age=10, base=T0 + 60)
        self.run_at(T0 + 60, ops)
        outs = [r for r in self.queue_rows() if r.get("priority") == "outage"]
        self.assertEqual(outs[0]["status"], "closed")

    def test_hold_blocks_restart_but_dashboard_still_restarts(self):
        self.stack(up=True, hold=True)
        self.beat_all(age=10, skip=("sentinel", "dashboard"))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        self.put("dashboard.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        res = self.run_at(T0, ops)
        self.assertEqual(ops.started, ["dashboard"])
        states = {a["key"]: a["state"] for a in res["status"]["agents"]}
        self.assertEqual(states["sentinel"], "held")
        self.assertEqual(states["dashboard"], "down")

    def test_stack_not_started_means_off_and_no_outage(self):
        self.stack(up=False)
        self.beat_all(age=10, skip=("sentinel", "dashboard"))
        self.put("dashboard.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        res = self.run_at(T0, ops)
        states = {a["key"]: a["state"] for a in res["status"]["agents"]}
        self.assertEqual(states["sentinel"], "off")
        self.assertEqual(ops.started, ["dashboard"])   # always on (DB06)
        self.assertEqual(len(self.queue_rows()), 1)    # only the dashboard outage

    def test_missing_heartbeat_with_stack_up_is_outage(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("keeper",))
        ops = FakeOps()
        res = self.run_at(T0, ops)
        row = [a for a in res["status"]["agents"] if a["key"] == "keeper"][0]
        self.assertEqual(row["state"], "missing")
        self.assertIn("keeper", ops.started)
        self.assertIn("no heartbeat file", self.queue_rows()[0]["text"])

    def test_bugq_watch_waiting_is_not_dead(self):
        self.stack(up=True)
        self.beat_all(age=10, notes={"bugq-watch": "waiting for the main session"})
        self.put("bugq-watch.heartbeat", str(int(T0 - 200)) + " waiting for the main session", age=200)
        ops = FakeOps()
        res = self.run_at(T0, ops)
        row = [a for a in res["status"]["agents"] if a["key"] == "bugq-watch"][0]
        self.assertEqual(row["state"], "waiting")
        self.assertNotIn("bugq-watch", ops.started)
        self.assertEqual(self.queue_rows(), [])

    def test_bugq_watch_waiting_an_hour_is_filed_once(self):
        self.stack(up=True)
        self.beat_all(age=10)
        self.put("bugq-watch.heartbeat", str(int(T0 - 4000)) + " waiting for the main session", age=4000)
        ops = FakeOps()
        self.run_at(T0, ops)
        self.run_at(T0 + 30, ops)
        items = [r for r in self.queue_rows() if "waited" in r.get("text", "")]
        self.assertEqual(len(items), 1)
        self.assertNotIn("bugq-watch", ops.started)

    def test_dead_bugq_watch_without_note_is_restarted(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("bugq-watch",))
        self.put("bugq-watch.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        self.run_at(T0, ops)
        self.assertIn("bugq-watch", ops.started)

    def test_container_exited_restarts_and_rathena_hourly_only(self):
        self.stack(up=True)
        self.beat_all(age=10)
        ps = dict(OK_CONTAINERS)
        ps["ro-rathena"] = ("exited", "Exited (1) 2 minutes ago")
        ops = FakeOps()
        self.run_at(T0, ops, containers=ps)
        self.assertEqual(ops.docker, ["ro-rathena"])
        self.beat_all(age=10, base=T0 + 1200)
        self.run_at(T0 + 1200, ops, containers=ps)     # 20 min later: rathena waits an hour (G16)
        self.assertEqual(ops.docker, ["ro-rathena"])
        self.beat_all(age=10, base=T0 + 3700)
        self.run_at(T0 + 3700, ops, containers=ps)
        self.assertEqual(ops.docker, ["ro-rathena", "ro-rathena"])

    def test_container_missing_is_filed_not_started(self):
        self.stack(up=True)
        self.beat_all(age=10)
        ps = dict(OK_CONTAINERS)
        del ps["ro-legion-core"]
        ops = FakeOps()
        res = self.run_at(T0, ops, containers=ps)
        self.assertEqual(ops.docker, [])
        row = [c for c in res["status"]["containers"] if c["key"] == "ro-legion-core"][0]
        self.assertEqual(row["state"], "missing")
        self.assertTrue(any("ro-legion-core" in r["text"] for r in self.queue_rows()))

    def test_docker_unanswered_is_one_item(self):
        self.stack(up=True)
        self.beat_all(age=10)
        ops = FakeOps()
        self.run_at(T0, ops, containers=None)
        self.beat_all(age=10, base=T0 + 30)
        self.run_at(T0 + 30, ops, containers=None)
        self.assertEqual(len([r for r in self.queue_rows() if "docker did not answer" in r["text"]]), 1)
        self.assertEqual(ops.docker, [])

    def test_thirty_minute_report_record(self):
        self.stack(up=True)
        self.beat_all(age=10)
        ops = FakeOps()
        self.run_at(T0, ops)
        self.beat_all(age=10, base=T0 + 60)
        self.run_at(T0 + 60, ops)
        self.beat_all(age=10, base=T0 + 1790)
        self.run_at(T0 + 1790, ops)
        path = os.path.join(self.d, "watchdog-reports.jsonl")
        with open(path) as f:
            recs = [json.loads(l) for l in f if l.strip()]
        self.assertEqual(len(recs), 1)
        self.beat_all(age=10, base=T0 + 1800)
        self.run_at(T0 + 1800, ops)
        with open(path) as f:
            recs = [json.loads(l) for l in f if l.strip()]
        self.assertEqual(len(recs), 2)
        r = recs[-1]
        self.assertEqual(len(r["agents"]), 8)
        self.assertEqual(len(r["containers"]), 6)
        self.assertEqual({a["key"] for a in r["agents"]}, set(ALL))
        self.assertIn("summary", r)
        self.assertTrue(os.path.exists(os.path.join(self.d, "watchdog.status.json")))

    def test_dry_run_changes_nothing(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = FakeOps()
        out = self.run_at(T0, ops, dry=True)
        self.assertEqual(ops.started, [])
        self.assertEqual(self.queue_rows(), [])
        self.assertFalse(os.path.exists(os.path.join(self.d, "watchdog-reports.jsonl")))
        self.assertFalse(os.path.exists(os.path.join(self.d, "watchdog.state.json")))
        self.assertTrue(any("would restart" in a["action"] for a in out["actions"]))

    def test_own_heartbeat_written_by_main(self):
        import watchdog
        self.stack(up=False)
        ops = FakeOps()
        orig = watchdog.Ops
        watchdog.Ops = lambda: ops
        try:
            with redirect_stdout(io.StringIO()):
                watchdog.main(["--once"])
        finally:
            watchdog.Ops = orig
        self.assertTrue(os.path.exists(os.path.join(self.d, "watchdog.heartbeat")))

    def test_real_docker_none_and_queue_unreadable_do_not_crash(self):
        import watchdog
        self.stack(up=True)
        self.beat_all(age=10)
        res = watchdog.run_pass(T0, FakeOps(), containers=None, queue={})  # docker not answering
        self.assertIn("summary", res["status"])


class TestHeartbeatWriters(Env):
    def test_bugq_watch_writes_waiting_note_on_wake(self):
        import bugq
        bugq.add("sentinel", "outage", "legion api down")
        with redirect_stdout(io.StringIO()):
            rc = bugq.main(["watch", "--interval", "0"])
        self.assertEqual(rc, 0)
        import common as C
        self.assertEqual(C.heartbeat_note(C.dpath("bugq-watch.heartbeat")), "waiting for the main session")

    def test_heartbeat_note_round_trip(self):
        import common as C
        C.heartbeat("x")
        self.assertEqual(C.heartbeat_note(C.dpath("x.heartbeat")), "")
        C.heartbeat("x", note="waiting for the main session")
        self.assertEqual(C.heartbeat_note(C.dpath("x.heartbeat")), "waiting for the main session")
        self.assertAlmostEqual(C.heartbeat_age(C.dpath("x.heartbeat")), 0.0)

    def test_keeper_script_writes_heartbeat(self):
        path = os.path.join(REPO, "tools", "rogui", "keeper.sh")
        with open(path, encoding="utf-8") as f:
            txt = f.read()
        self.assertIn("keeper.heartbeat", txt)
        self.assertIn("beat \"running\"", txt)
        self.assertIn("beat \"idle: stack.hold present\"", txt)
        try:   # read through stdin: Git Bash cannot open a Windows path
            with open(path, "rb") as src:
                r = subprocess.run(["bash", "-n"], stdin=src, capture_output=True, text=True, timeout=30)
        except (OSError, subprocess.TimeoutExpired):
            self.skipTest("no bash here")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_unwall_imports_common_and_writes_heartbeat_in_loop(self):
        path = os.path.join(REPO, "tools", "unwall.py")
        with open(path, encoding="utf-8") as f:
            txt = f.read()
        self.assertIn("C.heartbeat('unwall')", txt)
        import ast
        ast.parse(txt)


class TestDashboardReadsWatchdog(Env):
    def test_loops_show_waiting_and_watchdog_block(self):
        import dashserve
        self.stack(up=True)
        self.beat_all(age=10, notes={"bugq-watch": "waiting for the main session"})
        self.put("bugq-watch.heartbeat", str(int(T0 - 200)) + " waiting for the main session", age=200)
        rows = {r["key"]: r for r in dashserve.loops(T0)}
        self.assertEqual(rows["bugq-watch"]["state"], "waiting")
        self.assertTrue(rows["bugq-watch"]["up"])
        self.assertEqual(rows["unwall"]["state"], "ok")
        self.assertEqual(rows["keeper"]["state"], "ok")
        self.assertTrue(rows["keeper"]["up"])

    def test_watchdog_block_alarm_when_report_missing(self):
        import dashserve
        self.assertEqual(dashserve.watchdog_block(T0)["alarm"], "no 30-minute report on record yet")
        with open(os.path.join(self.d, "watchdog-reports.jsonl"), "w") as f:
            f.write(json.dumps({"t": int(T0 - 600), "iso": "x", "agents": [], "containers": []}) + "\n")
        w = dashserve.watchdog_block(T0)
        self.assertIsNone(w["alarm"])
        self.assertEqual(w["report_age_s"], 600)
        w = dashserve.watchdog_block(T0 + 3 * 3600)
        self.assertIn("missing", w["alarm"])

    def test_watchdog_block_shows_live_status(self):
        import dashserve
        with open(os.path.join(self.d, "watchdog.status.json"), "w") as f:
            json.dump({"t": T0, "summary": {"agents_ok": 8}}, f)
        w = dashserve.watchdog_block(T0)
        self.assertTrue(w["available"])
        self.assertEqual(w["status"]["summary"]["agents_ok"], 8)


class TestSingleCopy(Env):
    def test_fresh_lock_refuses_a_second_loop(self):
        import watchdog
        self.put("watchdog.lock", "pid 1 x", age=10)
        orig = watchdog.run_pass
        watchdog.run_pass = lambda *a, **k: (_ for _ in ()).throw(AssertionError("must not run a pass"))
        try:
            with redirect_stderr(io.StringIO()):
                rc = watchdog.main([])
        finally:
            watchdog.run_pass = orig
        self.assertEqual(rc, watchdog.EXIT_LOCKED)
        self.assertEqual(rc, 3)

    def test_stale_lock_is_taken_over(self):
        import watchdog
        self.put("watchdog.lock", "pid 1 x", age=300)
        self.assertTrue(watchdog.take_lock())
        import common as C
        self.assertAlmostEqual(C.marker_age("watchdog.lock"), 0.0)

    def test_no_lock_is_taken_and_second_take_refused(self):
        import watchdog
        self.assertTrue(watchdog.take_lock())
        self.assertFalse(watchdog.take_lock())

    def test_pass_refreshes_the_lock(self):
        import watchdog
        import common as C
        self.assertTrue(watchdog.take_lock(T0))
        os.environ["LEGION_FAKE_NOW"] = str(T0 + 50)
        self.assertAlmostEqual(C.marker_age("watchdog.lock"), 50.0)
        watchdog.touch_lock(T0 + 50)
        self.assertAlmostEqual(C.marker_age("watchdog.lock"), 0.0)

    def test_keep_script_single_copy_and_restart_text(self):
        path = os.path.join(REPO, "tools", "system", "watchdog-keep.cmd")
        with open(path, encoding="utf-8", newline="") as f:
            txt = f.read()
        self.assertIn(r"python tools\system\watchdog.py", txt)
        self.assertIn(r'if errorlevel 3 if "%first%"=="1"', txt)   # exits on a live lock at the first start
        self.assertIn("exit /b 0", txt)
        self.assertIn("timeout /t 3 /nobreak", txt)                      # restart 3 s after any exit
        self.assertIn("goto loop", txt)


if __name__ == "__main__":
    unittest.main()
