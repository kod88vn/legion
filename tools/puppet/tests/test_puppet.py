"""Tests for the puppet harness: no server, no tokens. Run: python -m unittest discover -s tools/puppet/tests -v"""
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PUP = os.path.dirname(HERE)
TOOLS = os.path.dirname(PUP)
sys.path.insert(0, PUP)
sys.path.insert(0, TOOLS)
import briefing  # noqa: E402
import dispatch  # noqa: E402
import legionctl  # noqa: E402

NOW = 1_800_000_000.0


class MockApi:
    """records of what the new endpoints might return; unknown paths raise (= endpoint missing)"""

    def __init__(self, data):
        self.data = data

    def __call__(self, path):
        base = path.split("?")[0]
        if base in self.data:
            return self.data[base]
        raise OSError("404 " + path)


FULL = {
    "/api/stats": {"bots": 100, "states": {"hunting": 60}},
    "/api/guilds": {"guild03": {"members": 80, "online": 41, "leader": "Mara"}},
    "/api/guild-rank": {"guilds": [{"guild": "guild03", "rank": 4, "wealth": 900000},
                                   {"guild": "guild01", "rank": 1, "wealth": 2500000}]},
    "/api/guild-relations": {"guild03": {"guild01": -0.4, "guild02": 0.5}},
    "/api/guild-direction/guild03": {"directions": [{"id": "d1", "kind": "gear", "item": "Elunium", "set_by": "leader"}]},
    "/api/guild-duty": {"members": [{"name": "a", "standing": "good"}, {"name": "b", "standing": "behind"}]},
    "/api/meter": {"cohorts": [{"cohort": "knight 41-60 party", "n": 12, "gap_x_n": 55.0},
                               {"cohort": "hunter 21-40 solo", "n": 30, "gap_x_n": 90.0}],
                   "totals": {"zeny_per_hour": 1200, "kills_per_min": 2.1}},
    "/api/minds": [{"name": "build", "last_thought": {"text": "knights lag"}}],
}


class BriefingTests(unittest.TestCase):
    def test_guild_full(self):
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("guild03", briefing.Api(fetch=MockApi(FULL)), nd)
        self.assertIn("Rival guilds", text)
        self.assertIn("guild01", text)  # rival listed with rank and wealth
        self.assertIn("-0.4", text)  # relations
        self.assertIn("Elunium", text)
        self.assertIn("First call", text)
        self.assertEqual(snap["metrics"]["rank"], 4)
        self.assertEqual(snap["metrics"]["duty_behind"], 1)
        self.assertEqual(snap["directions"], ["d1:gear:Elunium:leader"])
        self.assertLess(len(text), briefing.MAX_BRIEFING_CHARS + 200)

    def test_guild_feedback_diff(self):
        with tempfile.TemporaryDirectory() as nd:
            prev = {"metrics": {"rank": 6, "wealth": 800000}, "directions": ["d1:gear:Elunium:leader", "d0:stock::autopilot"]}
            text, _ = briefing.assemble("guild03", briefing.Api(fetch=MockApi(FULL)), nd, prev, NOW - 1500)
        self.assertIn("rank: 6 -> 4 (-2)", text)
        self.assertIn("wealth: 800000 -> 900000 (+100000)", text)
        self.assertIn("still active: 1", text)

    def test_old_engine_degrades(self):
        old = {"/api/stats": FULL["/api/stats"], "/api/guilds": FULL["/api/guilds"]}
        with tempfile.TemporaryDirectory() as nd:
            for role in ("guild03", "market", "build", "chat", "ayla"):
                text, snap = briefing.assemble(role, briefing.Api(fetch=MockApi(old)), nd)
                self.assertTrue(text.strip())
            text, _ = briefing.assemble("guild03", briefing.Api(fetch=MockApi(old)), nd)
        self.assertIn("not available right now", text)
        self.assertIn("/api/guild-rank", text)

    def test_api_down_no_crash(self):
        with tempfile.TemporaryDirectory() as nd:
            text, _ = briefing.assemble("market", briefing.Api(fetch=MockApi({})), nd)
        self.assertIn("market master", text)

    def test_notes_included(self):
        with tempfile.TemporaryDirectory() as nd:
            open(os.path.join(nd, "market.md"), "w").write("watch potions")
            text, _ = briefing.assemble("market", briefing.Api(fetch=MockApi(FULL)), nd)
        self.assertIn("watch potions", text)

    def test_chat_jobs_listed(self):
        data = dict(FULL, **{"/api/chat-jobs": [{"id": "j1", "kind": "ask", "speaker": "Kira", "template_line": "any elu?"}]})
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("chat", briefing.Api(fetch=MockApi(data)), nd, trigger="jobs")
        self.assertIn("j1", text)
        self.assertEqual(snap["metrics"]["queued_jobs"], 1)


class ScheduleTests(unittest.TestCase):
    def sched(self, rows=None, start=NOW):
        return dispatch.Scheduler(dispatch.ROLES, start, rows)

    def test_guilds_staggered_first_call(self):
        s = self.sched()
        due0 = [r for r in dispatch.GUILDS if s.due(r, NOW + 1, [], {})]
        due_later = [r for r in dispatch.GUILDS if s.due(r, NOW + 60 * 25, [], {})]
        self.assertEqual(due0, ["guild01"])
        self.assertEqual(len(due_later), 10)
        offsets = [dispatch.ROLES[g]["offset"] for g in dispatch.GUILDS]
        self.assertEqual(offsets, sorted(set(offsets)))  # all different

    def test_interval_after_call(self):
        s = self.sched([{"role": "market", "t": NOW - 29 * 60}])
        self.assertIsNone(s.due("market", NOW, [], {}))
        self.assertEqual(s.due("market", NOW + 120, [], {}), "schedule")

    def test_restart_does_not_wake_all(self):
        rows = [{"role": g, "t": NOW - 60} for g in dispatch.GUILDS]
        s = self.sched(rows, start=NOW - 7200)
        self.assertEqual([g for g in dispatch.GUILDS if s.due(g, NOW, [], {})], [])

    def test_never_two_at_once(self):
        s = self.sched()
        s.started("ayla", NOW)
        self.assertIsNone(s.due("ayla", NOW + 10000, [], {"orders": {"ayla": 1}}))
        s.finished("ayla")
        self.assertIsNotNone(s.due("ayla", NOW + 10000, [], {}))

    def test_orders_trigger_and_min_gap(self):
        s = self.sched([{"role": "guild02", "t": NOW - 10}])
        self.assertIsNone(s.due("guild02", NOW, [], {"orders": {"guild02": 1}}))  # min gap 180 s
        self.assertEqual(s.due("guild02", NOW + 200, [], {"orders": {"guild02": 1}}), "orders")

    def test_chat_has_no_schedule_only_triggers(self):
        s = self.sched()
        self.assertIsNone(s.due("chat", NOW + 99999, [], {}))
        s.note("chat", "human")
        self.assertEqual(s.due("chat", NOW + 99999, [], {}), "human")
        s.started("chat", NOW)
        self.assertNotIn("chat", s.pending)

    def test_build_on_demand_only(self):
        s = self.sched()
        self.assertIsNone(s.due("build", NOW + 99999, [], {}))
        self.assertEqual(s.due("build", NOW + 99999, [], {"wake": {"build": True}}), "wake")

    def test_ayla_event_wake_respects_gap(self):
        s = self.sched([{"role": "ayla", "t": NOW - 30}])
        s.note("ayla", "event:death")
        self.assertIsNone(s.due("ayla", NOW, [], {}))
        self.assertEqual(s.due("ayla", NOW + 100, [], {}), "event:death")

    def test_classify_ayla_event(self):
        names = {"kira", "bo"}
        self.assertEqual(dispatch.classify_ayla_event({"kind": "death", "bot": "Kira", "text": "died"}, names), "death")
        self.assertIsNone(dispatch.classify_ayla_event({"kind": "death", "bot": "Stranger", "text": "died"}, names))
        self.assertEqual(dispatch.classify_ayla_event({"kind": "party", "bot": "Bo", "text": "separated from party"}, names), "split")


class BudgetTests(unittest.TestCase):
    def test_hourly_cap_blocks_then_slides(self):
        cap = dispatch.ROLES["market"]["hourly_cap"]
        rows = [{"role": "market", "t": NOW - 1800, "cost_usd": cap}]
        s = dispatch.Scheduler(dispatch.ROLES, NOW - 99999)
        self.assertIsNone(s.due("market", NOW, rows, {"orders": {"market": 1}}))
        self.assertIsNotNone(s.due("market", NOW + 1900, rows, {"orders": {"market": 1}}))  # call left the window

    def test_budget_is_per_role(self):
        rows = [{"role": "guild01", "t": NOW - 10, "cost_usd": 99}]
        self.assertEqual(dispatch.spent_last_hour(rows, "guild02", NOW), 0)
        self.assertEqual(dispatch.spent_last_hour(rows, "guild01", NOW), 99)

    def test_parse_result(self):
        out = json.dumps({"type": "result", "total_cost_usd": 0.012, "num_turns": 3, "is_error": False,
                          "usage": {"input_tokens": 100, "output_tokens": 50, "cache_read_input_tokens": 900},
                          "result": "done"})
        r = dispatch.parse_result(out)
        self.assertEqual(r["cost_usd"], 0.012)
        self.assertEqual(r["output_tokens"], 50)
        self.assertEqual(dispatch.parse_result("garbage"), {})

    def test_log_roundtrip(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "x", "calls.jsonl")
            dispatch.append_log({"t": NOW, "role": "chat", "cost_usd": 0.01}, p)
            dispatch.append_log({"t": NOW + 1, "role": "chat", "cost_usd": 0.02}, p)
            self.assertEqual(len(dispatch.read_log(p)), 2)
            self.assertEqual(len(dispatch.read_log(p, since=NOW + 1)), 1)


class CommandTests(unittest.TestCase):
    def test_command_line(self):
        cmd = dispatch.build_command("guild03", claude="claude")
        self.assertEqual(cmd[:2], ["claude", "-p"])
        self.assertEqual(cmd[cmd.index("--model") + 1], "haiku")
        self.assertIn("--safe-mode", cmd)
        self.assertEqual(cmd[cmd.index("--output-format") + 1], "json")
        self.assertIn("--max-budget-usd", cmd)
        allowed = cmd[cmd.index("--allowedTools") + 1]
        self.assertTrue(allowed.startswith("Bash(python ") and allowed.endswith("legionctl.py:*)"))
        self.assertIn("goal", " ".join(cmd[cmd.index("--disallowedTools") + 1:cmd.index("--permission-mode")]))
        brief = cmd[cmd.index("--append-system-prompt") + 1]
        self.assertIn("guild03", brief)
        self.assertNotIn("{CTL}", brief)
        self.assertNotIn("{ROLE}", brief)

    def test_models(self):
        self.assertEqual(dispatch.ROLES["ayla"]["model"], "sonnet")
        self.assertEqual(dispatch.ROLES["build"]["model"], "sonnet")
        for r in ["market", "chat"] + dispatch.GUILDS:
            self.assertEqual(dispatch.ROLES[r]["model"], "haiku")

    def test_only_ayla_gets_ayla_sh(self):
        self.assertEqual(len(dispatch.allowed_tools("ayla")), 2)
        self.assertEqual(len(dispatch.allowed_tools("market")), 1)

    def test_all_briefs_load(self):
        for r in dispatch.ROLES:
            self.assertGreater(len(dispatch.load_brief(r)), 500)

    def test_dry_run_spends_nothing(self):
        env = dict(os.environ, LEGION_API="http://127.0.0.1:1", LEGION_DATA=tempfile.mkdtemp())
        p = subprocess.run([sys.executable, os.path.join(PUP, "dispatch.py"), "--dry-run", "--roles", "market"],
                           capture_output=True, text=True, env=env, timeout=60)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn("COMMAND:", p.stdout)
        self.assertIn("not available right now", p.stdout)
        self.assertFalse(os.path.exists(os.path.join(env["LEGION_DATA"], "puppet_calls.jsonl")))


class QueueTests(unittest.TestCase):
    def test_bug_entry_format(self):
        e = legionctl.queue_entry("market", "shops never open", "normal", ["Kira, prt_fild08"])
        self.assertEqual(set(e), {"id", "t", "from", "priority", "text", "evidence", "status"})
        self.assertEqual(e["status"], "open")
        self.assertEqual(e["from"], "market")

    def test_append_one_json_per_line(self):
        with tempfile.TemporaryDirectory() as d:
            legionctl.queue_append(legionctl.queue_entry("a", "one"), d)
            legionctl.queue_append(legionctl.queue_entry("b", "two", "neo", {"x": 1}), d)
            lines = open(os.path.join(d, "bugqueue.jsonl"), encoding="utf-8").read().splitlines()
        self.assertEqual(len(lines), 2)
        self.assertEqual(json.loads(lines[1])["priority"], "neo")

    def test_cli_bug_and_neo(self):
        with tempfile.TemporaryDirectory() as d:
            env = dict(os.environ, LEGION_DATA=d, LEGION_API="http://127.0.0.1:1")
            ctl = os.path.join(TOOLS, "legionctl.py")
            r = subprocess.run([sys.executable, ctl, "bug", "knights stand still", "--from", "build"], env=env,
                               capture_output=True, text=True, timeout=60)
            self.assertEqual(r.returncode, 0, r.stderr)
            r = subprocess.run([sys.executable, ctl, "neo", "give my party a faster map"], env=env,
                               capture_output=True, text=True, timeout=60)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertTrue(os.path.exists(os.path.join(d, "neo.wake")))
            rows = [json.loads(l) for l in open(os.path.join(d, "bugqueue.jsonl"), encoding="utf-8")]
            self.assertEqual([x["priority"] for x in rows], ["normal", "neo"])
            self.assertEqual(rows[1]["from"], "ayla")
            self.assertIsInstance(rows[1]["evidence"], dict)  # API down: evidence empty but present


if __name__ == "__main__":
    unittest.main()
