"""Safe restarts for tools/system/watchdog.py: the swallowed wake, a second keeper, a stale-but-alive process, grace after
a hold, failed starts, one restart per pass, the meter and deploy in the 30-minute record, and unwall's single instance
and mid-pass beats. Offline: temp data dir, fake clock, fake ops. Run: python -m unittest discover -s tests/system"""
import io
import json
import os
import shutil
import subprocess
import sys
import unittest
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import test_watchdog as W  # noqa: E402  (shared fixtures: Env, FakeOps, beat helpers, T0)

REPO = W.REPO
T0 = W.T0
_SETUP = {}


def _bash_style(p):
    """(bash executable, path style) that can read the Windows path p: the bash on PATH may be WSL's (no G: drive),
    so the Git Bash form (/g/x) and the WSL form (/mnt/g/x) are both tried. None when no bash can read it."""
    if "found" in _SETUP:
        return _SETUP["found"]
    drive, rest = os.path.splitdrive(os.path.abspath(p))
    letter, rest = drive[:1].lower(), rest.replace("\\", "/")
    found = None
    for bash in (shutil.which("bash"), r"C:\Program Files\Git\bin\bash.exe"):
        if not bash or not os.path.exists(bash):
            continue
        for style in ("/", "/mnt/"):
            probe = "%s%s%s" % (style, letter, rest)
            try:
                r = subprocess.run([bash, "-c", 'test -f "%s" && echo ok' % probe], capture_output=True, text=True,
                                   timeout=60)
            except (OSError, subprocess.TimeoutExpired):
                continue
            if "ok" in r.stdout:
                found = (bash, style)
                break
        if found:
            break
    _SETUP["found"] = found
    return found


def bash_path(p):
    """the path form of Windows path p for the bash that _bash_style found"""
    p = os.path.abspath(p)
    drive, rest = os.path.splitdrive(p)
    style = _SETUP["found"][1] if _SETUP.get("found") else "/"
    if not drive:
        return p.replace("\\", "/")
    return style + drive[:1].lower() + rest.replace("\\", "/")


def bash_lib(cmd, env_extra):
    """run `cmd` in bash after sourcing keeper-lock.sh, with the given environment"""
    script_win = os.path.join(REPO, "tools", "rogui", "keeper-lock.sh")
    _bash_style(script_win)
    bash = _SETUP["found"][0]
    script = bash_path(script_win)
    env = dict(os.environ, **env_extra)
    return subprocess.run([bash, "-c", '. "%s"; %s' % (script, cmd)], env=env, capture_output=True, text=True,
                          timeout=60)


def have_bash():
    return _bash_style(os.path.join(REPO, "tools", "rogui", "keeper-lock.sh")) is not None


class TestSwallowedWakeAndKeeper(W.Env):
    def test_keeper_lock_fresh_refuses_second_keeper_and_stale_is_taken(self):
        if not have_bash():
            self.skipTest("no bash here")
        local = os.path.join(self.d, "keeper.lock")
        lock = bash_path(local)
        with open(local, "w") as f:
            f.write("1234 1\n")
        os.utime(local, None)                             # refreshed just now: a live keeper
        r = bash_lib("keeper_take_lock && echo TOOK || echo REFUSED", {"KEEPER_LOCK": lock, "KEEPER_LOCK_STALE": "150"})
        self.assertIn("REFUSED", r.stdout, r.stderr)
        os.utime(local, (1_000_000, 1_000_000))           # not refreshed for ages: a dead keeper
        r = bash_lib("keeper_take_lock && echo TOOK && keeper_refresh_lock && echo OWN || echo LOST",
                     {"KEEPER_LOCK": lock, "KEEPER_LOCK_STALE": "150"})
        self.assertIn("TOOK", r.stdout, r.stderr)
        self.assertIn("OWN", r.stdout, r.stderr)

    def test_keeper_whose_lock_was_taken_exits_on_refresh(self):
        if not have_bash():
            self.skipTest("no bash here")
        local = os.path.join(self.d, "keeper.lock")
        lock = bash_path(local)
        with open(local, "w") as f:
            f.write("999999 1\n")                        # another keeper's pid
        r = bash_lib("keeper_refresh_lock && echo STILL || echo EXIT", {"KEEPER_LOCK": lock})
        self.assertIn("EXIT", r.stdout, r.stderr)

    def test_keeper_sh_wires_lock_and_beats_in_long_steps(self):
        with open(os.path.join(REPO, "tools", "rogui", "keeper.sh"), encoding="utf-8") as f:
            txt = f.read()
        self.assertIn('. "$(dirname "$0")/keeper-lock.sh"', txt)
        self.assertIn("keeper_take_lock ||", txt)
        self.assertIn("keeper_refresh_lock ||", txt)
        self.assertIn('beat "restarted ayla"', txt)

    def test_swallowed_wake_bugq_watch_never_started_only_filed(self):
        import watchdog
        self.stack(up=True)
        self.beat_all(age=10, skip=("bugq-watch",))
        self.put("bugq-watch.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps()
        res = self.run_at(T0, ops)
        self.assertNotIn("bugq-watch", ops.started)
        row = [a for a in res["status"]["agents"] if a["key"] == "bugq-watch"][0]
        self.assertEqual(row["state"], "down")
        self.assertIn("not started by the watchdog", row["action"])
        self.assertTrue(any("Bug queue watch is down" in r["text"] for r in self.queue_rows()))
        bq = [a for a in watchdog.AGENTS if a["key"] == "bugq-watch"][0]
        with self.assertRaises(RuntimeError):
            watchdog.Ops().start_agent(bq)


class TestStaleButAlive(W.Env):
    def test_fresh_lock_with_stale_heartbeat_is_not_started_again(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("keeper",))
        self.put("keeper.heartbeat", str(int(T0 - 900)), age=900)
        self.put("keeper.lock", "77 %d" % int(T0), age=10)
        ops = W.FakeOps()
        res = self.run_at(T0, ops)
        self.assertNotIn("keeper", ops.started)
        row = [a for a in res["status"]["agents"] if a["key"] == "keeper"][0]
        self.assertEqual(row["state"], "stuck")
        self.assertTrue(any("still running" in r["text"] for r in self.queue_rows()))

    def test_running_process_without_lock_is_not_started_again(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps(running={"sentinel"})
        res = self.run_at(T0, ops)
        self.assertEqual(ops.started, [])
        self.assertEqual([a for a in res["status"]["agents"] if a["key"] == "sentinel"][0]["state"], "stuck")

    def test_process_check_that_fails_counts_as_alive(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("__check_fails__",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps()
        import watchdog
        orig = watchdog.AGENTS
        watchdog.AGENTS = [dict(a, key="__check_fails__") if a["key"] == "sentinel" else a for a in orig]
        try:
            self.run_at(T0, ops)
        finally:
            watchdog.AGENTS = orig
        self.assertEqual(ops.started, [])


class TestGraceThrottleAndRecord(W.Env):
    def test_failed_start_is_not_counted_against_the_throttle(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps(fail={"sentinel"})
        res = self.run_at(T0, ops)
        self.assertTrue(any("restart failed" in a["action"] for a in res["actions"]))
        ops.fail.clear()
        self.beat_all(age=10, skip=("sentinel",), base=T0 + 30)
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900, base=T0 + 30)
        self.run_at(T0 + 30, ops)                         # 30 s later, not 10 min: the retry is allowed
        self.assertEqual(ops.started, ["sentinel"])

    def test_only_one_restart_per_pass(self):
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel", "partycheck"))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        self.put("partycheck.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps()
        self.run_at(T0, ops)
        self.assertEqual(len(ops.started), 1)

    def test_grace_after_hold_ends_then_restart(self):
        self.stack(up=True, hold=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = W.FakeOps()
        self.run_at(T0, ops)                              # held: the state is saved as not running
        self.stack(up=True, hold=False)
        res = self.run_at(T0 + 60, ops)                   # the hold ended 60 s ago
        row = [a for a in res["status"]["agents"] if a["key"] == "sentinel"][0]
        self.assertEqual(row["state"], "starting")
        self.assertEqual(ops.started, [])
        self.assertFalse(any("Sentinel" in r.get("text", "") for r in self.queue_rows()))
        self.beat_all(age=10, skip=("sentinel",), base=T0 + 700)     # the others keep beating
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900, base=T0 + 700)
        self.run_at(T0 + 700, ops)                        # 640 s after the hold ended: grace is over
        self.assertEqual(ops.started, ["sentinel"])

    def test_record_has_meter_state_and_last_deploy_verdict(self):
        self.stack(up=True)
        self.beat_all(age=10)
        self.put("meter.jsonl", json.dumps({"t": int(T0 - 60), "kpm": 1.0}) + "\n", age=60)
        with open(os.path.join(self.d, "deploys.jsonl"), "w") as f:
            f.write(json.dumps({"t": "2026-11-15T10:00:00", "epoch": int(T0 - 3600), "tag": "legion-v7",
                                "services": ["legion"], "critical": None, "fixes": []}) + "\n")
        res = self.run_at(T0, W.FakeOps())
        rec = res["status"]
        self.assertEqual(rec["meter"]["state"], "reading")
        self.assertEqual(rec["meter"]["last_row_t"], int(T0 - 60))
        self.assertEqual(rec["deploy"]["tag"], "legion-v7")
        self.assertEqual(rec["deploy"]["verdict"], "no fix list on the deploy record")
        self.assertEqual(rec["deploy"]["age_s"], 3600)
        with open(os.path.join(self.d, "watchdog-reports.jsonl")) as f:
            saved = json.loads(f.readline())
        self.assertEqual(saved["deploy"]["tag"], "legion-v7")
        self.assertIn("meter", saved)

    def test_verdict_line_is_the_verdict_and_is_not_a_deploy(self):
        self.stack(up=True)
        self.beat_all(age=10)
        with open(os.path.join(self.d, "deploys.jsonl"), "w") as f:
            f.write(json.dumps({"t": "2026-11-15T10:00:00", "epoch": int(T0 - 3600), "tag": "legion-v7",
                                "services": ["legion"], "fixes": [{"text": "x", "metric": "kpm"}]}) + "\n")
            f.write(json.dumps({"kind": "verdict", "t": "2026-11-15T11:00:00", "tag": "legion-v7",
                                "deploy_epoch": int(T0 - 3600), "partial": False, "verdict": "KEEP 1",
                                "items": [{"text": "x"}]}) + "\n")
            f.write(json.dumps({"kind": "verdict", "t": "2026-11-15T12:00:00", "tag": "other",
                                "deploy_epoch": int(T0 - 60), "partial": True, "verdict": "REVERT 1"}) + "\n")
        res = self.run_at(T0, W.FakeOps())
        d = res["status"]["deploy"]
        self.assertEqual(d["tag"], "legion-v7")          # the verdict line (kind verdict) is not the last deploy
        self.assertEqual(d["verdict"], "KEEP 1")
        self.assertEqual(d["verdict_source"], "deploys.jsonl verdict line")
        self.assertEqual(d["items"], 1)

    def test_meter_blind_and_no_deploy(self):
        self.stack(up=True)
        self.beat_all(age=10)
        self.put("meter.jsonl", json.dumps({"t": int(T0 - 5000)}) + "\n", age=5000)   # 83 min: past the hourly backup
        res = self.run_at(T0, W.FakeOps())
        self.assertEqual(res["status"]["meter"]["state"], "blind")
        self.assertFalse(res["status"]["deploy"]["available"])


class TestMeterAges(W.Env):
    """the meter from its real cadence: meter_bots.json every 5 min (reading under 12 min), meter.jsonl hourly (the
    backup, reading under 75 min)"""

    def meter(self, bots_age=None, jsonl_age=None):
        self.stack(up=True)
        self.beat_all(age=10)
        if bots_age is not None:
            self.put("meter_bots.json", "{}", age=bots_age)
        if jsonl_age is not None:
            self.put("meter.jsonl", json.dumps({"t": int(T0 - jsonl_age)}) + "\n", age=jsonl_age)
        return self.run_at(T0, W.FakeOps())["status"]["meter"]

    def test_bots_file_five_minutes_old_is_reading(self):
        m = self.meter(bots_age=300, jsonl_age=3000)
        self.assertEqual(m["state"], "reading")
        self.assertEqual(m["source"], "meter_bots.json")

    def test_bots_file_eleven_minutes_old_still_reading(self):
        self.assertEqual(self.meter(bots_age=660)["state"], "reading")

    def test_bots_file_thirteen_minutes_old_falls_back_to_jsonl(self):
        m = self.meter(bots_age=780, jsonl_age=600)
        self.assertEqual(m["state"], "reading")
        self.assertIn("meter.jsonl", m["source"])

    def test_hourly_jsonl_fifty_minutes_old_is_reading_without_bots(self):
        m = self.meter(jsonl_age=3000)
        self.assertEqual(m["state"], "reading")
        self.assertIn("hourly backup", m["source"])

    def test_both_stale_is_blind_with_reasons(self):
        m = self.meter(bots_age=900, jsonl_age=4600)
        self.assertEqual(m["state"], "blind")
        self.assertIn("meter_bots.json not written for 15 min", m["why"])
        self.assertIn("meter.jsonl not written for 76 min", m["why"])

    def test_no_meter_files_is_blind(self):
        self.assertEqual(self.meter()["state"], "blind")


class TestProcessCheckClassification(W.Env):
    """a failed or unclear process check counts as alive: only a clean "none" lets a restart through"""

    def test_pgrep_found_and_clean_none(self):
        import watchdog
        self.assertTrue(watchdog.proc_found(0, "123\n", "", pgrep=True))
        self.assertFalse(watchdog.proc_found(1, "", "", pgrep=True))

    def test_pgrep_unclear_exits_raise(self):
        import watchdog
        for rc, err in ((1, "wsl: no such distro\n"), (255, ""), (4294967295, "")):
            with self.assertRaises(RuntimeError):
                watchdog.proc_found(rc, "", err, pgrep=True)

    def test_powershell_count_and_failures(self):
        import watchdog
        self.assertFalse(watchdog.proc_found(0, "0\n", "", pgrep=False))
        self.assertTrue(watchdog.proc_found(0, "2\n", "", pgrep=False))
        for rc, out in ((0, ""), (1, "0"), (1, ""), (0, "Access denied")):
            with self.assertRaises(RuntimeError):
                watchdog.proc_found(rc, out, "", pgrep=False)

    def test_failed_check_means_alive_in_alive(self):
        import watchdog

        class Unclear(W.FakeOps):
            def process_running(self, a):
                return watchdog.proc_found(0, "", "", pgrep=False)   # empty output: unclear
        self.stack(up=True)
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops = Unclear()
        self.run_at(T0, ops)
        self.assertEqual(ops.started, [])


class TestStartBackoff(W.Env):
    def test_failing_start_backs_off_30_then_60_then_120_seconds(self):
        self.stack(up=True)
        ops = W.FakeOps(fail={"sentinel"})
        for t in (T0, T0 + 10, T0 + 30, T0 + 60, T0 + 90, T0 + 100):
            self.beat_all(age=10, skip=("sentinel",), base=t)
            self.put("sentinel.heartbeat", str(int(t - 900)), age=900, base=t)
            ops.now = t
            self.run_at(t, ops)
        # attempts at T0 (fails, next 30 s), T0+30 (fails, next 60 s), T0+90 (fails, next 120 s); not at +10, +60, +100
        self.assertEqual([t for k, t in ops.attempts], [T0, T0 + 30, T0 + 90])

    def test_backoff_is_capped_at_ten_minutes(self):
        import watchdog
        self.assertEqual(min(watchdog.RETRY_MAX, watchdog.RETRY_BASE * 2 ** (10 - 1)), 600)

    def test_success_clears_the_backoff(self):
        self.stack(up=True)
        ops = W.FakeOps(fail={"sentinel"})
        self.beat_all(age=10, skip=("sentinel",))
        self.put("sentinel.heartbeat", str(int(T0 - 900)), age=900)
        ops.now = T0
        self.run_at(T0, ops)
        ops.fail.clear()
        self.beat_all(age=10, skip=("sentinel",), base=T0 + 30)
        self.put("sentinel.heartbeat", str(int(T0 + 30 - 900)), age=900, base=T0 + 30)
        ops.now = T0 + 30
        self.run_at(T0 + 30, ops)
        self.assertEqual(ops.started, ["sentinel"])
        import watchdog
        st = watchdog.load_state()
        self.assertNotIn("sentinel", st["retry"])


class TestSingleInstances(W.Env):
    """the five scripts refuse a second copy while another holds a fresh lock, and each lock name the watchdog checks
    is the one the script takes"""

    def test_each_lock_name_in_agents_is_taken_by_its_script(self):
        import watchdog
        expect = {"sentinel": "tools/sentinel.py", "partycheck": "tools/system/partycheck.py",
                  "dispatcher": "tools/puppet/dispatch.py", "aylalog": "tools/system/aylalog.py",
                  "dashboard": "tools/dashboard/dashserve.py", "unwall": "tools/unwall.py",
                  "keeper": "tools/rogui/keeper-lock.sh"}
        by = {a["key"]: a for a in watchdog.AGENTS}
        for key, script in expect.items():
            lock = by[key].get("lock")
            self.assertTrue(lock, key)
            with open(os.path.join(REPO, script), encoding="utf-8") as f:
                self.assertIn(lock, f.read(), "%s does not take %s" % (script, lock))

    def test_partycheck_main_refuses_with_a_fresh_lock(self):
        import partycheck
        self.put("partycheck.lock", "77 1", age=10)
        self.assertEqual(partycheck.main(["--interval", "1"]), 0)     # refused before the loop starts

    def test_dashboard_main_refuses_with_a_fresh_lock(self):
        import dashserve
        self.put("dashboard.lock", "77 1", age=10)
        self.assertEqual(dashserve.main(["--port", "1"]), 0)          # refused before the port is bound

    def test_sentinel_main_refuses_with_a_fresh_lock(self):
        import sentinel
        self.put("sentinel.lock", "77 1", age=10)
        self.assertIsNone(sentinel.main())                             # refused: returns before the loop

    def test_dispatch_main_refuses_with_a_fresh_lock(self):
        sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))
        import dispatch
        self.put("dispatch.lock", "77 1", age=10)
        self.assertEqual(dispatch.main([]), 3)


class TestUnwall(W.Env):
    def setUp(self):
        super().setUp()
        import unwall
        self.unwall = unwall

    def tearDown(self):
        self.unwall.LOOP_LOCK = False
        super().tearDown()

    def test_beat_is_a_noop_in_one_shot_and_exits_when_lock_taken(self):
        self.unwall.LOOP_LOCK = False
        self.unwall.beat()
        self.unwall.LOOP_LOCK = True
        self.put("unwall.lock", "999999 %d" % int(T0), age=0)
        with self.assertRaises(SystemExit):
            self.unwall.beat()

    def test_beat_runs_during_a_pass_not_only_at_the_end(self):
        calls = []
        orig_beat, orig_sql = self.unwall.beat, self.unwall.sql
        self.unwall.beat = lambda: calls.append(1)
        self.unwall.sql = lambda q: [["1", "lg1", "prontera", "1", "1", "prontera", "1", "1"],
                                     ["2", "lg2", "prontera", "2", "2", "prontera", "2", "2"]]
        try:
            n = self.unwall.one_pass({}, dry=True)
        finally:
            self.unwall.beat, self.unwall.sql = orig_beat, orig_sql
        self.assertEqual(n, 0)
        self.assertEqual(len(calls), 3)                   # at the top of the pass and once per row

    def test_second_loop_refuses_with_fresh_lock(self):
        self.put("unwall.lock", "999999 %d" % int(T0), age=10)
        orig_load, orig_argv = self.unwall.load_maps, sys.argv

        def no_maps():
            raise AssertionError("a refused loop must not load maps")
        self.unwall.load_maps = no_maps
        sys.argv = ["unwall.py", "--loop"]
        try:
            with redirect_stdout(io.StringIO()) as out:
                self.unwall.main()
        finally:
            self.unwall.load_maps, sys.argv = orig_load, orig_argv
        self.assertIn("another copy holds", out.getvalue())


if __name__ == "__main__":
    unittest.main()
