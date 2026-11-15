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
        self.put("meter.jsonl", json.dumps({"t": int(T0 - 2000)}) + "\n", age=2000)
        res = self.run_at(T0, W.FakeOps())
        self.assertEqual(res["status"]["meter"]["state"], "blind")
        self.assertFalse(res["status"]["deploy"]["available"])


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
