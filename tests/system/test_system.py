"""Tests for the system-agent harness. No server, no docker; a fake clock and temp data dirs.
Run: python -m unittest discover -s tests/system -v   (from the repo root)"""
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, os.path.join(REPO, "tools"))

T0 = 1_800_000_000.0   # fixed "now"


class Env(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        os.environ["LEGION_FAKE_NOW"] = str(T0)

    def tearDown(self):
        shutil.rmtree(self.d, ignore_errors=True)
        os.environ.pop("LEGION_DATA", None)
        os.environ.pop("LEGION_FAKE_NOW", None)

    def clock(self, t):
        os.environ["LEGION_FAKE_NOW"] = str(t)

    def touch(self, name, age=0, content=None):
        p = os.path.join(self.d, name)
        with open(p, "w") as f:
            f.write(content if content is not None else str(int(T0 - age)))
        os.utime(p, (T0 - age, T0 - age))


class TestBugq(Env):
    def test_rank_outage_then_neo_then_impact(self):
        import bugq
        bugq.add("watcher", "low", "small thing", bots=1)
        bugq.add("sentinel", "high", "many bots stuck", bots=200)
        bugq.add("sentinel", "normal", "few bots stuck", bots=5)
        bugq.add("neo", "neo", "Ayla: party loses time on regroup")
        bugq.add("sentinel", "outage", "legion api down")
        bugq.add("owner", "normal", "owner says fix X")
        order = [e["text"] for e in bugq.rank(list(bugq.load().values()))]
        self.assertEqual(order[0], "legion api down")
        self.assertTrue(order[1].startswith("Ayla"))
        self.assertEqual(order[2], "owner says fix X")
        self.assertEqual(order[3:], ["many bots stuck", "few bots stuck", "small thing"])

    def test_dedupe_by_key_and_bump(self):
        import bugq
        i1, new1 = bugq.add("sentinel", "high", "farming 70%", key="sentinel:farming")
        i2, new2 = bugq.add("sentinel", "high", "farming 66%", key="sentinel:farming")
        self.assertTrue(new1)
        self.assertFalse(new2)
        self.assertEqual(i1, i2)
        e = bugq.load()[i1]
        self.assertEqual(e["count"], 2)
        bugq.close(i1)
        i3, new3 = bugq.add("sentinel", "high", "farming 60%", key="sentinel:farming")
        self.assertTrue(new3)   # closed bug: a new entry
        self.assertNotEqual(i1, i3)

    def test_extra_queue_file_from_ayla_watchdog(self):
        import bugq
        self.assertEqual(bugq.extra_paths(), [])  # tests (LEGION_DATA set): no default extra file
        extra = os.path.join(self.d, "ayla-logs-bugqueue.jsonl")
        with open(extra, "w", encoding="utf-8") as f:
            f.write(json.dumps({"id": "w1-ayla", "t": "2027-01-15T08:00:00", "from": "ayla-watchdog", "priority": "high",
                                "status": "open", "key": "ayla-watchdog-stuck-izlude", "text": "Ayla watchdog STUCK on izlude",
                                "evidence": "cell 24,98"}) + "\n")
        os.environ["LEGION_BUGQ_EXTRA"] = extra
        try:
            self.assertIn("w1-ayla", bugq.load())
            bugq.close("w1-ayla", note="fixed")  # the update goes to the main file and merges by id
            self.assertEqual(bugq.load()["w1-ayla"]["status"], "closed")
        finally:
            os.environ.pop("LEGION_BUGQ_EXTRA", None)

    def test_bump_throttle(self):
        import bugq
        i, _ = bugq.add("sentinel", "high", "x", key="k", bump_min_secs=600)
        bugq.add("sentinel", "high", "x", key="k", bump_min_secs=600)
        self.assertEqual(bugq.load()[i]["count"], 1)
        self.clock(T0 + 700)
        bugq.add("sentinel", "high", "x", key="k", bump_min_secs=600)
        self.assertEqual(bugq.load()[i]["count"], 2)

    def test_dedupe_all_merges_text_variants(self):
        import bugq
        # entries from another writer (P): same text apart from numbers, no key
        for n, t in enumerate(("Bot Zed stuck at 12", "Bot Zed stuck at 99")):
            bugq.C.append_jsonl(bugq.qpath(), {"id": "p%d" % n, "t": T0 + n, "from": "minds", "priority": "normal",
                                               "text": t, "evidence": "", "status": "open"})
        self.assertEqual(bugq.dedupe_all(), 1)
        live = bugq.rank(list(bugq.load().values()))
        self.assertEqual(len(live), 1)
        self.assertEqual(live[0]["count"], 2)

    def test_watch_wakes_only_for_outage_neo_plan(self):
        import bugq
        bugq.add("watcher", "high", "just a bug", bots=50)
        self.assertEqual(bugq.wake_reasons(), [])
        i, _ = bugq.add("sentinel", "outage", "OUTAGE api")
        r = bugq.wake_reasons()
        self.assertEqual([k for k, _, _ in r], ["outage"])
        bugq.mark_woken(r)
        self.assertEqual(bugq.wake_reasons(), [])          # not woken twice for the same entry
        self.touch("neo.wake")
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["neo"])
        bugq.mark_woken(bugq.wake_reasons())
        self.assertEqual(bugq.wake_reasons(), [])
        # planning time: last deploy 3h - 20min ago
        bugq.C.append_jsonl(os.path.join(self.d, "deploys.jsonl"), {"epoch": T0 - 3 * 3600 + 10 * 60, "tag": "t1"})
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["plan"])
        bugq.mark_woken(bugq.wake_reasons())
        self.assertEqual(bugq.wake_reasons(), [])

    def test_urgent_wake_flag_and_file(self):
        import bugq
        i, new = bugq.add("partycheck", "high", "[party apart] Knight apart", key="partycheck:apart:Knight", wake=True)
        self.assertTrue(new)
        self.assertTrue(os.path.exists(os.path.join(self.d, "bugq.wake")))
        r = bugq.wake_reasons()
        self.assertEqual([k for k, _, _ in r], ["urgent"])
        self.assertEqual(r[0][2], i)
        bugq.mark_woken(r)
        self.assertEqual(bugq.wake_reasons(), [])
        # the same problem again: dedupe bumps the count, no second wake
        self.clock(T0 + 1000)
        i2, new2 = bugq.add("partycheck", "high", "[party apart] Knight apart", key="partycheck:apart:Knight", wake=True)
        self.assertEqual((i2, new2), (i, False))
        self.assertEqual(bugq.wake_reasons(), [])
        self.assertEqual(bugq.load()[i]["count"], 2)
        # a closed problem that comes back is new: it wakes again
        bugq.close(i)
        self.clock(T0 + 2000)
        i3, new3 = bugq.add("partycheck", "high", "[party apart] Knight apart", key="partycheck:apart:Knight", wake=True)
        self.assertTrue(new3)
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent"])

    def test_wake_file_alone_wakes(self):
        """another writer (a script without the flag in its own file) only touched bugq.wake"""
        import bugq
        bugq.touch_wake("stuck bots")
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent"])
        bugq.mark_woken(bugq.wake_reasons())
        self.assertEqual(bugq.wake_reasons(), [])

    def test_watch_exits_for_urgent(self):
        import bugq
        bugq.add("sentinel", "high", "[party] Ayla's party is split", key="sentinel:party", wake=True)
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = bugq.main(["watch", "--once"])
        self.assertEqual(rc, 0)
        self.assertIn("urgent", buf.getvalue())
        self.assertIn("split", buf.getvalue())

    def test_no_plan_wake_early(self):
        import bugq
        bugq.C.append_jsonl(os.path.join(self.d, "deploys.jsonl"), {"epoch": T0 - 3600, "tag": "t1"})
        self.assertEqual(bugq.wake_reasons(), [])

    def test_watch_once_prints_summary(self):
        import bugq
        bugq.add("sentinel", "outage", "OUTAGE ayla offline")
        buf = io.StringIO()
        with redirect_stdout(buf):
            rc = bugq.main(["watch", "--once"])
        self.assertEqual(rc, 0)
        self.assertIn("WHAT TO ASSIGN", buf.getvalue())
        self.assertIn("ayla offline", buf.getvalue())


class TestGate(Env):
    def run_gate(self, mode="prompt", inp=None):
        import importlib
        import mindgate
        importlib.reload(mindgate)
        return mindgate.evaluate(mode, inp or {}, T0)

    def test_marker_absent_is_silent(self):
        self.assertIsNone(self.run_gate())

    def test_healthy_is_silent(self):
        self.touch("stack.up", age=3600)
        self.touch("dispatch.heartbeat", age=30)
        self.touch("sentinel.heartbeat", age=30)
        self.assertIsNone(self.run_gate())

    def test_grace_after_marker(self):
        self.touch("stack.up", age=60)
        self.assertIsNone(self.run_gate())

    def test_stale_reports_once_then_silent(self):
        self.touch("stack.up", age=3600)
        self.touch("dispatch.heartbeat", age=1000)
        self.touch("sentinel.heartbeat", age=30)
        out = self.run_gate("prompt")
        self.assertIsNotNone(out)
        self.assertIn("dispatcher", out["systemMessage"])
        self.assertIn("hookSpecificOutput", out)
        self.assertIsNone(self.run_gate("prompt"))      # same problem: not again
        self.assertIsNone(self.run_gate("stop"))        # and the stop hook does not block it again
        self.touch("sentinel.heartbeat", age=2000)      # a new problem is reported
        self.assertIsNotNone(self.run_gate("prompt"))

    def test_stop_blocks_new_problem_once_not_when_active(self):
        self.touch("stack.up", age=3600)
        out = self.run_gate("stop", {"stop_hook_active": True})
        self.assertIsNone(out)
        out = self.run_gate("stop")
        self.assertEqual(out["decision"], "block")
        self.assertIsNone(self.run_gate("stop"))

    def test_recovery_then_relapse_reports_again(self):
        self.touch("stack.up", age=3600)
        self.assertIsNotNone(self.run_gate())            # both missing
        self.touch("dispatch.heartbeat", age=10)
        self.touch("sentinel.heartbeat", age=10)
        self.assertIsNone(self.run_gate())               # recovered (state cleared)
        os.remove(os.path.join(self.d, "sentinel.heartbeat"))
        self.assertIsNotNone(self.run_gate())

    def _run_script(self, args, stdin="{}", extra_files=()):
        env = dict(os.environ)
        return subprocess.run([sys.executable, os.path.join(REPO, "tools", "mindgate.py")] + args, input=stdin,
                              capture_output=True, text=True, env=env)

    def test_script_always_exit0_and_valid_json(self):
        self.touch("stack.up", age=3600)
        for mode in ("prompt", "stop"):
            r = self._run_script([mode])
            self.assertEqual(r.returncode, 0)
            if r.stdout.strip():
                json.loads(r.stdout)
        r = self._run_script(["prompt"], stdin="not json{{")
        self.assertEqual(r.returncode, 0)
        os.environ["LEGION_DATA"] = os.path.join(self.d, "does", "not", "exist")
        r = self._run_script(["stop"])
        self.assertEqual(r.returncode, 0)
        self.assertEqual(r.stdout.strip(), "")

    def test_pause_flag_silences(self):
        flag = os.path.join(REPO, "tools", "mindgate.paused")
        had = os.path.exists(flag)
        if not had:
            open(flag, "w").close()
        try:
            self.touch("stack.up", age=3600)
            r = self._run_script(["prompt"])
            self.assertEqual((r.returncode, r.stdout.strip()), (0, ""))
        finally:
            if not had:
                os.remove(flag)


class TestDeployGuard(Env):
    def test_three_hour_gap_and_critical(self):
        import deploylog as D
        ok, _ = D.check(fixes=1)
        self.assertTrue(ok)
        D.record("img1", ["legion"], None, [D.parse_fix("fix a|kpm|archer")])
        self.clock(T0 + 3600)
        ok, msg = D.check(fixes=1)
        self.assertFalse(ok)
        self.assertIn("REFUSED", msg)
        self.assertTrue(D.check(critical="boss wedge", fixes=0)[0])
        self.assertFalse(D.check(critical="  ")[0])
        self.clock(T0 + 3 * 3600 - 1)
        self.assertFalse(D.check(fixes=1)[0])
        self.clock(T0 + 3 * 3600)
        self.assertTrue(D.check(fixes=1)[0])
        self.assertFalse(D.check(fixes=0)[0])             # regular deploy needs a fix list
        self.clock(T0 + 3600)
        self.assertTrue(D.check(tag="img1", fixes=0)[0])  # core step on the same image

    def test_record_content_and_core_copy(self):
        import deploylog as D
        D.record("img1", ["legion"], None, [D.parse_fix("stuck ledge|kpm|archer 21-40"), D.parse_fix("pots|zph")])
        D.record("img1", ["legion-core"], None, [])
        recs = D.C.read_jsonl(D.C.dpath("deploys.jsonl"))
        self.assertEqual(len(recs), 2)
        self.assertEqual(recs[0]["fixes"][0], {"text": "stuck ledge", "metric": "kpm", "cohort": "archer 21-40"})
        self.assertEqual(recs[0]["fixes"][1]["metric"], "zph")
        self.assertEqual(recs[1]["fixes"], recs[0]["fixes"])
        D.record("img2", ["legion"], "api wedge", [])
        self.assertTrue(D.C.read_jsonl(D.C.dpath("deploys.jsonl"))[-1]["critical"])

    def test_rathena_once_per_hour(self):
        import deploylog as D
        self.assertTrue(D.rathena_check()[0])
        D.main(["rathena-record"])
        self.clock(T0 + 1800)
        self.assertFalse(D.rathena_check()[0])
        self.assertTrue(D.rathena_check("rathena is broken")[0])
        self.clock(T0 + 3601)
        self.assertTrue(D.rathena_check()[0])

    def test_shell_functions(self):
        import deploylog as D
        bash = shutil.which("bash")
        if not bash:
            self.skipTest("no bash")
        py = sys.executable.replace("\\", "/")
        guard = os.path.join(REPO, "tools", "deploy", "guard.sh").replace("\\", "/")
        D.record("img1", ["legion"], None, [D.parse_fix("a|kpm|x")])
        self.clock(T0 + 600)

        def sh(body, env_extra=None):
            env = dict(os.environ, PYTHON=py)
            env.update(env_extra or {})
            return subprocess.run([bash, "-c", ". '%s'; %s" % (guard, body)], capture_output=True, text=True, env=env)
        r = sh('parse_deploy_args --fix "b|kpm|y"; deploy_gate')
        self.assertEqual(r.returncode, 5, r.stdout + r.stderr)
        r = sh('parse_deploy_args --critical "api wedge"; deploy_gate')
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        r = sh('DRY_RUN=1; parse_deploy_args --fix "b|kpm|y"; deploy_gate')
        self.assertEqual(r.returncode, 0)
        self.assertIn("would be refused", r.stdout)
        r = sh('parse_deploy_args --critical "wedge" --fix "q|kpm" img9; echo "pos=${POS_ARGS[*]} fixes=${#FIX_ARGS[@]}"')
        self.assertIn("pos=img9 fixes=1", r.stdout)
        D.main(["rathena-record"])
        r = sh("rathena_gate")
        self.assertEqual(r.returncode, 5)
        r = sh("rathena_gate", {"RATHENA_HOUR_OVERRIDE": "broken"})
        self.assertEqual(r.returncode, 0)

    def test_scripts_parse(self):
        bash = shutil.which("bash")
        if not bash:
            self.skipTest("no bash")
        for f in ("deploy/guard.sh", "deploy/dep3.sh", "deploy/depcore.sh", "deploy/depcore_nobuild.sh",
                  "deploy/rathena_recreate.sh", "rogui/keeper.sh"):
            r = subprocess.run([bash, "-n", os.path.join(REPO, "tools", f)], capture_output=True, text=True)
            self.assertEqual(r.returncode, 0, f + r.stderr)
            with open(os.path.join(REPO, "tools", f), "rb") as fh:
                self.assertNotIn(b"\r", fh.read(), f + " must stay LF")


METER = {"server": {"kpm": {"mean": 0.5}, "zph": {"mean": 20000}},
         "cohorts": [
             {"cohort": "swordman 21-40 solo", "n": 100, "kpm": {"mean": 0.6, "median": 0.6, "p75": 1.0},
              "zph": {"mean": 10000, "p75": 20000},
              "shares": {"fighting": 0.45, "searching": 0.15, "travelling": 0.2, "resting": 0.05, "town": 0.05, "dead": 0.02,
                         "waiting": 0.02, "stuck": 0.04, "idle": 0.02}, "deaths_per_hour": 0.2},
             {"cohort": "archer 41-60 solo", "n": 50, "kpm": {"mean": 1.0, "p75": 1.1}, "zph": {"mean": 30000, "p75": 31000},
              "shares": {"fighting": 0.8, "searching": 0.1, "travelling": 0.03, "stuck": 0.01}},
             {"cohort": "priest 1-20 party", "n": 300, "kpm": {"mean": 0.3, "p75": 0.5}, "zph": {"mean": 3000, "p75": 6000},
              "shares": {"fighting": 0.6, "searching": 0.2, "resting": 0.1, "waiting": 0.05, "stuck": 0.05}}]}


class TestLeague(Env):
    def test_ranking_by_gap_times_n(self):
        import meterlib as M
        cs = sorted(M.cohorts(METER), key=lambda c: -M.score(c))
        self.assertEqual([c["key"] for c in cs], ["priest 1-20 party", "swordman 21-40 solo", "archer 41-60 solo"])
        self.assertAlmostEqual(M.prize(cs[0], "kpm"), 0.2 * 300)

    def test_breakdown_orders_by_excess(self):
        import meterlib as M
        sw = [c for c in M.cohorts(METER) if c["key"].startswith("swordman")][0]
        rows = M.gap_breakdown(sw)
        self.assertEqual(rows[0][0], "travelling")
        self.assertGreater(rows[0][4], 0)

    def test_report_text_and_neo_flags(self):
        import league
        neo = {"party": [{"name": "Ayla", "kpm": 0.9, "par_kpm": 1.0, "zph": 50000, "par_zph": 40000},
                         {"name": "Priestess", "kpm": {"mean": 1.2}, "par": {"kpm": 1.0, "zph": 1000}, "zph": 5000}],
               "guild": {"name": "Starveil", "kpm": 0.7, "par_kpm": 0.8}}
        text = league.render(METER, neo, "t")
        self.assertIn("priest 1-20 party", text.split("## Where")[0].split("\n")[8:][0] + "\n".join(text.split("\n")[8:12]))
        self.assertIn("kpm below par", text)
        self.assertIn("Starveil", text)
        self.assertIn("Ayla", text)

    def test_survives_garbage(self):
        import league
        for bad in (None, {}, {"cohorts": "x"}, {"cohorts": [1, 2]}, [], {"cohorts": {"a": 5}}):
            league.render(bad, None, "t")
            league.render(bad, bad, "t")

    def test_compare_deploy_keep_revert(self):
        import league
        W = 3 * 3600
        rows = []
        for h in range(-3, 3):                      # hourly rows around the deploy at T0
            t = T0 + h * 3600 + 1800
            after = h >= 0
            rows.append({"t": t, "cohort": "archer 21-40 solo", "n": 40, "kpm": {"mean": 1.2 if after else 1.0},
                         "zph": {"mean": 9000 if after else 10000}})
            rows.append({"t": t, "cohort": "monk 41-60 party", "n": 10, "kpm": {"mean": 0.5}, "zph": {"mean": 5000}})
        dep = {"epoch": T0, "tag": "img1", "fixes": [
            {"text": "archer ledge", "metric": "kpm", "cohort": "archer"},
            {"text": "archer pots", "metric": "zph", "cohort": "archer"},
            {"text": "monk thing", "metric": "kpm", "cohort": "monk"},
            {"text": "ghost", "metric": "kpm", "cohort": "sage"}]}
        self.clock(T0 + W + 10)
        res = league.compare(dep, rows)
        verdicts = [r[6] for r in res]
        self.assertTrue(verdicts[0].startswith("KEEP"))
        self.assertTrue(verdicts[1].startswith("REVERT"))
        self.assertTrue(verdicts[2].startswith("INCONCLUSIVE"))
        self.assertTrue(verdicts[3].startswith("NO DATA"))
        self.clock(T0 + 3600)
        self.assertIn("partial", league.compare(dep, rows)[0][6])
        self.assertIn("REVERT candidates: 1", league.render_compare(dep, res, 3))

    def test_cli_file_output(self):
        import league
        out = os.path.join(self.d, "reports")
        mf = os.path.join(self.d, "m.json")
        json.dump(METER, open(mf, "w"))
        with redirect_stdout(io.StringIO()):
            self.assertEqual(league.main(["--input", mf, "--neo-input", mf, "--out", out]), 0)
        files = os.listdir(out)
        self.assertEqual(len(files), 1)
        self.assertTrue(files[0].startswith("league-"))


class TestSentinelMeter(Env):
    def test_meter_findings(self):
        import sentinel
        f = sentinel.meter_findings(METER)
        self.assertTrue(f["farming"][0])      # (fighting+searching) well under 85%
        self.assertTrue(f["stuck"][0] is False or f["stuck"][0] is True)
        self.assertFalse(f["meter_deaths"][0])
        self.assertEqual(sentinel.meter_findings(None), {})
        self.assertEqual(sentinel.meter_findings({"cohorts": []}), {})

    def test_filing_and_outage_keys(self):
        import sentinel
        import bugq
        sentinel.DATA = self.d
        sentinel.file_problem("legion_api", "down")
        sentinel.file_problem("farming", "70%")
        sentinel.file_problem("farming", "69%")
        live = bugq.rank(list(bugq.load().values()))
        self.assertEqual([e["priority"] for e in live], ["outage", "high"])
        self.assertIn("legion_api", sentinel.OUTAGE_KEYS)
        self.assertNotIn("farming", sentinel.OUTAGE_KEYS)

    def test_party_and_stuck_wake(self):
        import sentinel
        import bugq
        sentinel.DATA = self.d
        sentinel.file_problem("party", "Ayla's party is split for 8+ min: Knight(prontera: idle)")
        sentinel.file_problem("deaths", "deaths/min 9")
        live = {e["key"]: e for e in bugq.rank(list(bugq.load().values()))}
        self.assertTrue(live["sentinel:party"].get("wake"))
        self.assertEqual(live["sentinel:party"]["priority"], "high")
        self.assertFalse(live["sentinel:deaths"].get("wake"))
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent"])
        self.assertEqual(sentinel.URGENT_KEYS, {"party", "stuck", "frozen"})

    def test_watch_client_checks_retired(self):
        src = open(os.path.join(REPO, "tools", "sentinel.py"), encoding="utf-8").read()
        for gone in ("RO-AILab.exe", "ai-client.sh", "roai-restarts.log", "'client_loop'", "'launcher'"):
            self.assertNotIn(gone, src.split('"""', 2)[2])

    def test_imports_without_legion_data(self):
        """Regression b1794706820063-060: with LEGION_DATA unset, ROOT was used before it was defined."""
        env = {k: v for k, v in os.environ.items() if k != "LEGION_DATA"}
        r = subprocess.run([sys.executable, "-c", "import sentinel; print(sentinel.DATA)"], capture_output=True,
                           text=True, env=env, cwd=os.path.join(REPO, "tools"))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(r.stdout.strip().replace("\\", "/").endswith("legion/data"), r.stdout)

    def test_dispatcher_down_needs_marker(self):
        import sentinel
        sentinel.DATA = self.d
        self.assertIsNone(sentinel.dispatcher_down())
        self.touch("stack.up", age=3600)
        self.assertIn("no dispatcher", sentinel.dispatcher_down())


class TestPartyCheck(Env):
    def party(self, **over):
        ms = [{"name": "Ayla", "map": "pay_fild09", "state": "fighting", "human": True, "leader": True, "online": True},
              {"name": "Knight", "map": "pay_fild09", "state": "fighting", "with_leader": True, "online": True,
               "save_map": "payon"},
              {"name": "Priest", "map": "pay_fild09", "state": "sitting", "with_leader": True, "online": True,
               "save_map": "payon"}]
        for k, v in over.items():
            n, f = k.split("__")
            for m in ms:
                if m["name"] == n:
                    m[f] = v
        return {"members": ms, "events": []}

    def test_clean_party(self):
        import partycheck as P
        self.assertEqual(P.PartyCheck().evaluate(self.party(), T0), [])

    def test_apart_needs_time(self):
        import partycheck as P
        pc = P.PartyCheck()
        s = self.party(Knight__with_leader=False)
        self.assertEqual(pc.evaluate(s, T0), [])
        v = pc.evaluate(s, T0 + 300)
        self.assertEqual([x[0] for x in v], ["apart"])
        self.assertEqual(P.PartyCheck(instant=True).evaluate(s, T0)[0][0], "apart")

    def test_errand_excuses_apart(self):
        import partycheck as P
        s = self.party(Knight__with_leader=False, Knight__state="brb, renting a peco")
        self.assertEqual(P.PartyCheck(instant=True).evaluate(s, T0), [])

    def test_wrong_save_point(self):
        import partycheck as P
        v = P.PartyCheck().evaluate(self.party(Knight__save_map="prontera"), T0)
        self.assertEqual([x[0] for x in v], ["save"])
        self.assertEqual(P.hunt_town("moc_pryd04"), "morocc")

    def test_deaths_and_idle_and_sit(self):
        import partycheck as P
        pc = P.PartyCheck()
        s = self.party(Knight__state="idle")
        s["events"] = [{"t": 1, "bot": "Knight", "kind": "death", "text": "died"},
                       {"t": 2, "bot": "Knight", "kind": "death", "text": "died again"}]
        v = pc.evaluate(s, T0)
        self.assertEqual([x[0] for x in v], ["deaths"])
        v = pc.evaluate(s, T0 + 200)
        self.assertIn("idle", [x[0] for x in v])
        s2 = self.party(Ayla__state="sitting", Knight__state="sitting")
        pc2 = P.PartyCheck()
        pc2.evaluate(s2, T0)
        self.assertIn("sit", [x[0] for x in pc2.evaluate(s2, T0 + 400)])

    def test_flip_between_maps(self):
        import partycheck as P
        pc = P.PartyCheck()
        out = []
        for i, mp in enumerate(["pay_fild09", "payon", "pay_fild09", "payon", "pay_fild09"]):
            s = self.party(Knight__map=mp)
            out = pc.evaluate(s, T0 + i * 60)
        self.assertIn("flip", [x[0] for x in out])

    def test_files_to_queue_deduped(self):
        import partycheck as P
        import bugq
        v = [("save", "Knight", "high", "Knight saved wrong")]
        P.file_violations(v)
        P.file_violations(v)
        live = bugq.rank(list(bugq.load().values()))
        self.assertEqual(len(live), 1)
        self.assertEqual(live[0]["from"], "partycheck")
        self.assertTrue(live[0]["wake"])

    def test_violations_filed_high_and_wake(self):
        import partycheck as P
        import bugq
        P.file_violations([("sit", "party", "normal", "party sitting for 5+ min"),
                           ("idle", "Knight", "normal", "Knight has been idle on payon")])
        live = bugq.rank(list(bugq.load().values()))
        self.assertEqual(sorted(e["priority"] for e in live), ["high", "high"])
        self.assertEqual([k for k, _, _ in bugq.wake_reasons()], ["urgent", "urgent"])
        self.assertEqual(P.file_priority("outage"), "outage")

    def test_snapshot_cli(self):
        import partycheck as P
        f = os.path.join(self.d, "snap.json")
        json.dump(self.party(Knight__save_map="prontera"), open(f, "w"))
        with redirect_stdout(io.StringIO()) as buf:
            P.main(["--once", "--snapshot", f, "--dry-run"])
        self.assertIn("saved at prontera", buf.getvalue())


class TestAgentsAndSkill(unittest.TestCase):
    def test_agent_frontmatter(self):
        want = {"fixer": "sonnet", "hard-fixer": "opus", "deploy": "sonnet", "watcher": "haiku", "qa": "haiku",
                "analyst-review": "sonnet"}
        for name, model in want.items():
            p = os.path.join(REPO, ".claude", "agents", name + ".md")
            self.assertTrue(os.path.exists(p), p)
            head = open(p, encoding="utf-8").read().split("---")[1]
            self.assertIn("name: " + name, head)
            self.assertIn("model: " + model, head)

    def test_old_mind_agents_kept(self):
        for n in ("legion-chat", "legion-guild", "legion-life", "legion-market"):
            self.assertTrue(os.path.exists(os.path.join(REPO, ".claude", "agents", n + ".md")))


if __name__ == "__main__":
    unittest.main()


# the real engine shapes (legion/meterapi.go), checked at the integration pass
ENGINE_METER = {"window": "1h", "server": {"bots": 120, "kpm": 1.5, "zph": 15000, "deaths_h": 0.2, "kpm_median": 1.4,
                                           "zph_median": 11000, "shares": {"fighting": 0.6, "searching": 0.3}},
                "cohorts": [
                    {"key": "swordman|41-60|solo", "line": "swordman", "band": "41-60", "mode": "solo", "n": 40,
                     "kpm": {"mean": 1.2, "median": 1.1, "p75": 1.6, "par": 1.6, "gap": 0.4, "prize": 16},
                     "zph": {"mean": 9000, "median": 8000, "p75": 13000, "par": 13000, "gap": 4000, "prize": 160000},
                     "deaths_h": 0.9, "shares": {"fighting": 0.6, "searching": 0.3, "stuck": 0.01}},
                    {"key": "merchant|41-60|vendor", "line": "merchant", "band": "41-60", "mode": "vendor", "n": 60,
                     "kpm": {"mean": 0, "p75": 0}, "zph": {"mean": 500, "p75": 900}, "deaths_h": 0,
                     "shares": {"town": 0.98, "travelling": 0.02}}]}


class TestEngineShapes(Env):
    def test_meterlib_reads_engine(self):
        import meterlib as M
        cs = M.cohorts(ENGINE_METER)
        sw = cs[0]
        self.assertEqual(sw["key"], "swordman|41-60|solo")
        self.assertEqual(sw["kpm"]["p75"], 1.6)
        self.assertEqual(sw["deaths_ph"], 0.9)
        self.assertEqual(M.prize(sw, "zph"), 4000 * 40)
        s = M.server(ENGINE_METER)
        self.assertEqual((s["kpm"]["mean"], s["deaths_ph"], s["n"]), (1.5, 0.2, 120))
        self.assertEqual([c["key"] for c in M.fighters(cs)], ["swordman|41-60|solo"])

    def test_sentinel_ignores_vendors(self):
        import sentinel
        f = sentinel.meter_findings(ENGINE_METER)
        self.assertFalse(f["farming"][0])       # vendors' town time is not a farming problem
        self.assertTrue(f["meter_deaths"][0])   # deaths_h is read

    def test_history_rows_and_neo(self):
        import league
        rows = [{"t": T0 - 1800, "kind": "server", "window": "1h", "n": 100, "deaths_h": 5.0},
                {"t": T0 - 1800, "kind": "cohort", "key": "archer|21-40|solo", "n": 10, "deaths_h": 0.5,
                 "kpm": {"mean": 1.0}, "zph": {"mean": 900}},
                {"t": T0 - 1800, "kind": "neo", "n": 6, "neo": {"party": {"n": 6}}}]
        self.assertEqual(league.window_mean(rows, "deaths", "", T0 - 3600, T0), 0.5)  # server row not mixed in
        neo = {"window": "1h", "ayla": {"name": "Ayla", "windows": {"1h": {"zph_purse": 4000, "par_zph": 9000}}},
               "party_summary": {"n": 1}, "party": [{"name": "Kira", "kpm": 0.5, "par_kpm": 1.0, "zph": 1, "par_zph": 2}],
               "starveil_summary": {"n": 0}, "starveil": []}
        text = "\n".join(league.neo_section(neo, []))
        self.assertIn("party: Kira", text)
        self.assertIn("Ayla (purse z/h, OpenKore)", text)
        self.assertNotIn("party_summary", text)
