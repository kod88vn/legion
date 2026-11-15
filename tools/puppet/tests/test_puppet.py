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
# never read or write the live owner-orders file or Ayla's log from tests (G23)
_TMP_ORDERS = tempfile.mkdtemp(prefix="ayla-orders-test-")
os.environ["AYLA_ORDERS"] = os.path.join(_TMP_ORDERS, "ayla_orders.jsonl")
os.environ["AYLA_ORDERS_LOG"] = os.path.join(_TMP_ORDERS, "owner_orders.log")
import briefing  # noqa: E402
import dispatch  # noqa: E402
import legionctl  # noqa: E402
import orders  # noqa: E402

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


# the real engine shapes (legion/guildduty_api.go, meterapi.go, chatjobs.go, exchange.go, minds.go)
FULL = {
    "/api/stats": {"bots": 100, "states": {"hunting": 60}},
    "/api/guilds": {"guild03": {"members": 80, "online": 41, "leader": "Mara"}},
    "/api/guild-rank": {"instance": "main", "rank": [
        {"guild": "guild03", "name": "Moonlit Bazaar", "place": 4, "score": 610.5, "wealth": 900000, "members": 80},
        {"guild": "guild01", "name": "Crimson Oath", "place": 1, "score": 820.0, "wealth": 2500000, "members": 90},
        {"guild": "human-guild", "name": "Starveil", "place": 7, "score": 300.0, "wealth": 120000, "members": 12}],
        "history": {}},
    "/api/guild-relations": {"instance": "main", "relations": [
        {"a": "guild01", "b": "guild03", "name_a": "Crimson Oath", "name_b": "Moonlit Bazaar", "value": -0.4,
         "label": "cool", "events": 3, "last_event": "member moved"},
        {"a": "guild02", "b": "guild03", "name_a": "Silver Lanterns", "name_b": "Moonlit Bazaar", "value": 0.5,
         "label": "ally", "events": 9}]},
    "/api/guild-direction/guild03": {"guild": "guild03", "name": "Moonlit Bazaar",
                                     "active": [{"id": "d1", "kind": "gear", "item": "Elunium", "set_by": "leader"}],
                                     "leader_set": [], "autopilot": []},
    "/api/guild-direction/human-guild": {"guild": "human-guild", "name": "Starveil", "active": [], "leader_set": [],
                                         "autopilot": []},
    "/api/guild-duty": {"duty_on": False, "standings": {"guild03": {"good": 1, "behind": 1}},
                        "members": [{"char": "a", "guild": "guild03", "standing": "good", "days": []},
                                    {"char": "b", "guild": "guild03", "standing": "behind", "short_days": 1, "days": []}]},
    "/api/meter": {"window": "1h", "server": {"kpm": 2.1, "zph": 1200, "deaths_h": 0.2},
                   "cohorts": [{"key": "swordman|41-60|party", "n": 12, "kpm": {"mean": 1.2, "par": 1.6, "prize": 4.8},
                                "zph": {"mean": 900, "par": 1300, "prize": 4800}, "deaths_h": 0.1, "shares": {"fighting": 0.5}},
                               {"key": "archer|21-40|solo", "n": 30, "kpm": {"mean": 1.0, "par": 1.4, "prize": 12},
                                "zph": {"mean": 700, "par": 1000, "prize": 9000}, "deaths_h": 0.3, "shares": {}}]},
    "/api/meter/bots": {"total": 2, "bots": [{"name": "a", "guild": "Moonlit Bazaar", "counted": True, "kpm": 1.0,
                                              "zph": 1000, "gap_zph": 300},
                                             {"name": "b", "guild": "Moonlit Bazaar", "counted": True, "kpm": 2.0,
                                              "zph": 3000, "gap_zph": -100}]},
    "/api/minds": [{"name": "build", "last_thought": {"text": "knights lag"}}],
    "/api/minds/guild03/thoughts": {"mind": "guild03", "messages": [{"text": "OWNER message, not a thought"}],
                                    "thoughts": [{"t": "2026-11-14T20:00:00", "text": "set elu direction"}], "next": 5},
}

# the shapes the puppet branch guessed before the engine was merged: still read (older or changed engine)
GUESSED = dict(FULL, **{
    "/api/guild-rank": {"guilds": [{"guild": "guild03", "rank": 4, "wealth": 900000},
                                   {"guild": "guild01", "rank": 1, "wealth": 2500000}]},
    "/api/guild-relations": {"guild03": {"guild01": -0.4, "guild02": 0.5}},
    "/api/guild-direction/guild03": {"directions": [{"id": "d1", "kind": "gear", "item": "Elunium", "set_by": "leader"}]},
    "/api/guild-duty": {"members": [{"name": "a", "standing": "good"}, {"name": "b", "standing": "behind"}]},
    "/api/meter": {"cohorts": [{"cohort": "knight 41-60 party", "n": 12, "gap_x_n": 55.0}],
                   "totals": {"zeny_per_hour": 1200, "kills_per_min": 2.1}},
})


class BriefingTests(unittest.TestCase):
    def test_guild_full(self):
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("guild03", briefing.Api(fetch=MockApi(FULL)), nd)
        self.assertIn("Rival guilds", text)
        self.assertIn("Crimson Oath", text)  # rival listed with place and wealth
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

    def test_guild_engine_shapes(self):
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("guild03", briefing.Api(fetch=MockApi(FULL)), nd)
        self.assertIn("Crimson Oath: -0.4 (cool)", text)
        self.assertIn("#1 Crimson Oath", text)
        self.assertIn("set elu direction", text)  # thoughts, not the owner's messages
        self.assertNotIn("OWNER message", text)
        self.assertIn("2 members: kills/min 1.5, zeny/h 2000", text)  # guild metrics from meter rows by guild name
        self.assertIn("archer|21-40|solo n=30", text.split("Biggest gaps")[1].splitlines()[1])  # biggest zeny prize first
        self.assertEqual(snap["metrics"]["zph"], 2000)

    def test_live_quirks(self):
        """seen on the first live dry-run: relations null before any event, empty cohorts listed first"""
        meter = dict(FULL["/api/meter"])
        meter["cohorts"] = [{"key": "acolyte|1-20|party", "n": 0, "kpm": {"mean": 0, "par": 0, "prize": 0},
                             "zph": {"mean": 0, "par": 0, "prize": 0}}] + FULL["/api/meter"]["cohorts"]
        data = dict(FULL, **{"/api/guild-relations": {"instance": "main", "relations": None}, "/api/meter": meter})
        with tempfile.TemporaryDirectory() as nd:
            text, _ = briefing.assemble("guild03", briefing.Api(fetch=MockApi(data)), nd)
        self.assertIn("(all neutral, no events yet)", text)
        self.assertNotIn("acolyte|1-20|party", text)

    def test_guessed_shapes_still_read(self):
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("guild03", briefing.Api(fetch=MockApi(GUESSED)), nd)
        self.assertEqual(snap["metrics"]["rank"], 4)
        self.assertEqual(snap["metrics"]["duty_behind"], 1)
        self.assertIn("Elunium", text)
        self.assertIn("-0.4", text)

    def test_ayla_starveil(self):
        neo = {"window": "1h", "ayla": {"name": "Ayla", "windows": {"1h": {"zph_purse": 4000}}},
               "party_summary": {"n": 6, "ratio_kpm": 1.1, "ratio_zph": 0.9}, "party": [{"name": "Kira", "kpm": 1}],
               "starveil_summary": {"n": 3}, "starveil": []}
        with tempfile.TemporaryDirectory() as nd:
            text, snap = briefing.assemble("ayla", briefing.Api(fetch=MockApi(dict(FULL, **{"/api/meter/neo": neo}))), nd)
        self.assertIn("Starveil rank and wealth", text)
        self.assertEqual(snap["metrics"]["starveil_place"], 7)
        self.assertEqual(snap["metrics"]["party_ratio_zph"], 0.9)
        self.assertEqual(snap["metrics"]["ayla_zph_purse"], 4000)

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
        data = dict(FULL, **{"/api/chat-jobs": {"on": True, "agent_present": True, "wait_sec": [20, 40], "counts": {},
                                                "jobs": [{"id": "j1", "kind": "ask", "speaker": "Kira", "template_line": "any elu?"}]}})
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
        self.assertEqual(s.due("ayla", NOW + dispatch.ROLES["ayla"]["min_gap"], [], {}), "event:death")

    def test_classify_ayla_event(self):
        names = {"kira", "bo"}
        self.assertEqual(dispatch.classify_ayla_event({"kind": "death", "bot": "Kira", "text": "died"}, names), "death")
        self.assertIsNone(dispatch.classify_ayla_event({"kind": "death", "bot": "Stranger", "text": "died"}, names))
        self.assertEqual(dispatch.classify_ayla_event({"kind": "party", "bot": "Bo", "text": "separated from party"}, names), "split")

    def test_ayla_idle_state(self):
        neo = {"ayla": {"windows": {"10m": {"shares": {"town": 0.9}}}}}
        self.assertEqual(dispatch.ayla_idle_state(neo), "idle")
        neo["ayla"]["windows"]["10m"]["shares"] = {"fighting": 0.8, "town": 0.1}
        self.assertIsNone(dispatch.ayla_idle_state(neo))
        self.assertIsNone(dispatch.ayla_idle_state({}))

    def test_signals_idle_wakes_ayla_once_per_cooldown(self):
        neo = {"ayla": {"windows": {"10m": {"shares": {"town": 1}}}}}
        class A:
            def get(self, p):
                return neo if p == "/api/meter/neo" else ({"events": [], "next": 1} if "events" in p else ([] if p != "/api/stats" else {}))
            def alive(self): return True
        s = dispatch.Signals(lambda: A())
        self.assertEqual(s.poll()["ayla_event"], "idle")
        s.consumed("ayla")
        self.assertIsNone(s.poll()["ayla_event"])


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
        self.assertEqual(dispatch.ROLES["ayla"]["model"], "haiku")  # PR38/AY05: a watcher
        self.assertEqual(dispatch.ROLES["build"]["model"], "sonnet")
        for r in ["market", "chat"] + dispatch.GUILDS:
            self.assertEqual(dispatch.ROLES[r]["model"], "haiku")

    def test_only_ayla_gets_ayla_sh(self):
        self.assertEqual(len(dispatch.allowed_tools("ayla")), 2 + len(dispatch.AYLA_READ) + len(dispatch.AYLA_ACT))
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


class IntegrationTests(unittest.TestCase):
    """agreements with the system tools (tools/system) and the engine, checked after the merge"""

    def test_heartbeat_file_for_gate_and_sentinel(self):
        with tempfile.TemporaryDirectory() as d:
            old = dispatch.DATA
            dispatch.DATA = d
            try:
                s = dispatch.Scheduler(dispatch.ROLES, NOW)
                dispatch.heartbeat("ok", s, [], True, NOW)
                with open(os.path.join(d, "dispatch.heartbeat")) as f:
                    self.assertEqual(int(f.read()), int(NOW))
                self.assertTrue(os.path.exists(os.path.join(d, "puppet_heartbeat.json")))
                dispatch.heartbeat("stopped", s, [], None, NOW)
                self.assertFalse(os.path.exists(os.path.join(d, "dispatch.heartbeat")))
            finally:
                dispatch.DATA = old
        sys.path.insert(0, os.path.join(TOOLS, ".."))
        import mindgate  # noqa: E402
        import sentinel  # noqa: E402
        self.assertIn("dispatch.heartbeat", mindgate.DISPATCH_FILES)
        self.assertIn("dispatch.heartbeat", sentinel.DISPATCH_FILES)

    def test_stack_hold_pauses(self):
        with tempfile.TemporaryDirectory() as d:
            self.assertFalse(dispatch.paused(d))
            open(os.path.join(d, "stack.hold"), "w").close()
            self.assertTrue(dispatch.paused(d))
            os.remove(os.path.join(d, "stack.hold"))
            open(os.path.join(d, "puppet.pause"), "w").close()
            self.assertTrue(dispatch.paused(d))

    def test_kick_denied_and_real_commands_exist(self):
        denied = " ".join(dispatch.denied_tools())
        self.assertIn("guild-kick:*", denied)
        import legionctl_guild  # noqa: E402
        for c in ("guild-direction", "guild-direction-set", "guild-pardon", "guild-rank", "guild-relations"):
            self.assertIn(c, legionctl_guild.COMMANDS)
        self.assertEqual(legionctl.PUPPET_CMDS, {"bug", "neo", "notes"})
        for name in ("guild", "chat", "ayla"):
            text = open(os.path.join(HERE, "..", "briefs", name + ".md"), encoding="utf-8").read()
            self.assertNotIn("guild-direction get", text)
            self.assertNotIn("guild-duty pardon", text)
            self.assertNotIn("--kind need", text)

    def test_queue_lines_read_by_bugq(self):
        sys.path.insert(0, os.path.join(TOOLS, "system"))
        import bugq  # noqa: E402
        with tempfile.TemporaryDirectory() as d:
            legionctl.queue_append(legionctl.queue_entry("ayla", "NEO party stuck", "neo", {"x": 1}), d)
            legionctl.queue_append(legionctl.queue_entry("market", "shops closed"), d)
            rows = [json.loads(l) for l in open(os.path.join(d, "bugqueue.jsonl"), encoding="utf-8")]
            for r in rows:
                for k in ("id", "t", "from", "priority", "text", "evidence", "status"):
                    self.assertIn(k, r)
            self.assertTrue(bugq.is_neo(rows[0]))
            self.assertFalse(bugq.is_neo(rows[1]))

    def test_ayla_sh_through_wsl_on_windows(self):
        self.assertEqual(dispatch.wsl_path("G:/RO/legion/tools/rogui/ayla.sh"), "/mnt/g/RO/legion/tools/rogui/ayla.sh")
        self.assertTrue(dispatch.ayla_cmd(nt=True).startswith("wsl -d "))
        self.assertTrue(dispatch.ayla_cmd(nt=True).endswith(" -- bash tools/rogui/ayla.sh"))
        self.assertEqual(dispatch.ayla_cmd(nt=False), "bash " + dispatch.wsl_path(dispatch.AYLA_SH))
        self.assertIn(dispatch.ayla_cmd(), dispatch.load_brief("ayla"))
        self.assertIn(f"Bash({dispatch.ayla_cmd()} log:*)", dispatch.allowed_tools("ayla"))

    def test_windows_mind_no_slash_args(self):
        # Git Bash rewrites any argument starting with '/' (2026-11-15: exit 127 on /mnt/g/.../ayla.sh)
        for part in dispatch.ayla_cmd(nt=True).split():
            self.assertFalse(part.startswith("/"), part)
        self.assertIn("--cd ", dispatch.ayla_cmd(nt=True))
        old = dispatch.WIN_MIND
        try:
            dispatch.WIN_MIND = True
            env = dispatch.mind_env()
            self.assertEqual(env["MSYS_NO_PATHCONV"], "1")
            self.assertIn("MSYS_NO_PATHCONV", env["WSLENV"].split(":"))
            for t in dispatch.allowed_tools("ayla") + dispatch.allowed_tools("chat"):
                self.assertNotIn("/mnt/", t)
        finally:
            dispatch.WIN_MIND = old

    def test_ayla_helper_guarded_commands_only(self):
        tl = dispatch.allowed_tools("ayla")
        tools = " ".join(tl)
        for w in ("cmd:", "say", "give", "whisper", "guild", "tell", "global", "trade", "recruit"):
            self.assertNotIn(f"ayla.sh {w}", tools)
        self.assertIn(f"Bash({dispatch.ayla_cmd()} cmd respawn)", tl)   # exact: no free console text (G05)
        for a in ("map", "rally", "party", "townrun", "done", "orders"):   # owner 2026-11-14: Haiku controls Ayla again (map / rally / party)
            self.assertIn(f"Bash({dispatch.ayla_cmd()} {a}:*)", tl)
        self.assertTrue(any("bugq.py add:*" in t for t in tl))
        denied = " ".join(dispatch.denied_tools("python", "ayla"))
        for c in ("party-say:*", "say:*", "chat-jobs:*", "guild-direction-set:*", "goal:*"):
            self.assertIn(c, denied)
        self.assertNotIn("party-say:*", " ".join(dispatch.denied_tools("python", "chat")))
        self.assertNotIn(" ai off`", dispatch.load_brief("ayla").replace("never `ai`, `ai off`", ""))

    def test_watchdog_reports_wake_ayla(self):
        with tempfile.TemporaryDirectory() as d:
            q = os.path.join(d, "bugqueue.jsonl")
            open(q, "w").write(json.dumps({"from": "ayla-watchdog", "text": "Ayla watchdog STUCK on izlude: x"}) + "\n")
            sig = dispatch.Signals(lambda: None, d)
            self.assertEqual(sig.watchdog_reports(q), [])  # first poll: offset only
            with open(q, "a") as f:
                f.write(json.dumps({"from": "ayla-watchdog", "text": "Ayla watchdog WRONG_MAP on pay_dun00: y"}) + "\n")
                f.write(json.dumps({"from": "sentinel", "text": "Ayla watchdog IDLE fake"}) + "\n")
                f.write("junk\n")
            self.assertEqual(sig.watchdog_reports(q), ["wrong_map"])
            self.assertEqual(sig.watchdog_reports(q), [])
            c = os.path.join(d, "console.txt")
            open(c, "w").write("[partylead] rally: going to the Kafra in yuno\n")
            self.assertEqual(sig.watchdog_console(c), [])  # first poll: offset only
            with open(c, "a") as f:
                f.write("\x1b[1;33m[watchdog] STUCK izlude 24,98 for 3 min -> respawn\x1b[0m\n[watchdog] STUCK again\n")
            self.assertEqual(sig.watchdog_console(c), ["stuck"])
            self.assertEqual(sig.watchdog_console(c), [])


class OwnerOrderTests(unittest.TestCase):
    """AY20: ayla.sh tell -> orders file -> owner wake past min_gap/hourly cap (30 s floor) -> briefing top -> result"""

    def setUp(self):
        self.d = tempfile.mkdtemp(prefix="ayla-orders-")
        self.path = os.path.join(self.d, "o.jsonl")
        self.log = os.path.join(self.d, "owner.log")
        self.env = {k: os.environ.get(k) for k in ("AYLA_ORDERS", "AYLA_ORDERS_LOG")}
        os.environ["AYLA_ORDERS"], os.environ["AYLA_ORDERS_LOG"] = self.path, self.log

    def tearDown(self):
        for k, v in self.env.items():
            os.environ[k] = v

    def test_add_done_roundtrip_and_log(self):
        o = orders.add("do a town run\nwith the party\x07", path=self.path, now=NOW)
        self.assertEqual(o["text"], "do a town run with the party")  # one line, no control chars
        self.assertEqual([x["id"] for x in orders.open_orders(self.path)], [o["id"]])
        self.assertIsNone(orders.done("nope", "x", path=self.path, log=self.log))
        r = orders.done(o["id"], "townrun started, party told", path=self.path, log=self.log)
        self.assertEqual(r["result"], "townrun started, party told")
        self.assertEqual(orders.open_orders(self.path), [])
        self.assertIsNone(orders.done(o["id"], "again", path=self.path, log=self.log))  # closed once
        self.assertIn("[owner] do a town run with the party -> townrun started, party told", open(self.log).read())
        self.assertEqual(orders.results(self.path)[0]["order"], "do a town run with the party")
        with self.assertRaises(ValueError):
            orders.add("  \n ", path=self.path)

    def test_owner_bypasses_min_gap_and_hourly_cap_but_30s_floor(self):
        s = dispatch.Scheduler(dispatch.ROLES, NOW, [{"role": "ayla", "t": NOW - 40}])
        rows = [{"role": "ayla", "t": NOW - 100, "cost_usd": 5.0}]  # far over the hourly cap
        self.assertIsNone(s.due("ayla", NOW, rows, {}))  # the normal path is blocked (gap 300 s + cap)
        self.assertEqual(dispatch.owner_due(s, NOW, ["o1"]), "owner")
        self.assertIsNone(dispatch.owner_due(s, NOW, []))  # no open order, no wake
        s.started("ayla", NOW)
        self.assertIsNone(dispatch.owner_due(s, NOW + 60, ["o1"]))  # never two calls at once
        s.finished("ayla")
        self.assertIsNone(dispatch.owner_due(s, NOW + dispatch.OWNER_GAP - 1, ["o1"]))  # at most one per 30 s
        self.assertEqual(dispatch.owner_due(s, NOW + dispatch.OWNER_GAP, ["o1"]), "owner")
        only = dispatch.Scheduler({"chat": dispatch.ROLES["chat"]}, NOW)
        self.assertIsNone(dispatch.owner_due(only, NOW, ["o1"]))  # loop restricted to other roles

    def test_briefing_puts_order_on_top(self):
        self.assertNotIn("OWNER ORDER", briefing.assemble("ayla", briefing.Api(fetch=MockApi(FULL)), self.d)[0])
        o = orders.add("go farm gl_church", path=self.path)
        text, _ = briefing.assemble("ayla", briefing.Api(fetch=MockApi(FULL)), self.d, trigger="owner")
        self.assertTrue(text.startswith("== OWNER ORDER - do this now"))
        self.assertIn(f"[{o['id']}]", text.split("\n")[1])
        self.assertIn("go farm gl_church", text.split("\n")[1])
        self.assertIn("done <id>", dispatch.load_brief("ayla"))
        self.assertIn("OWNER ORDERS FIRST", dispatch.load_brief("ayla"))

    def test_unreported_orders_closed_after_call(self):
        a = orders.add("wait for the priest", path=self.path)
        b = orders.add("go farm gl_church", path=self.path)
        orders.done(a["id"], "rally prontera", path=self.path, log=self.log)
        dispatch.close_unreported([a["id"], b["id"]], {"exit": 0, "result": "moved the party"}, self.path)
        res = {r["id"]: r for r in orders.results(self.path)}
        self.assertEqual(res[a["id"]]["result"], "rally prontera")  # the helper's own report stays
        self.assertIn("moved the party", res[b["id"]]["result"])
        c = orders.add("town run", path=self.path)
        dispatch.close_unreported([c["id"]], {"exit": "timeout"}, self.path)
        self.assertEqual(orders.open_orders(self.path), [])
        self.assertIn("not carried out", orders.results(self.path)[-1]["result"])

    def test_nap_wakes_on_new_order(self):
        import threading
        threading.Timer(0.3, lambda: orders.add("town run", path=self.path)).start()
        t0 = time.time()
        self.assertTrue(dispatch.nap(5, step=0.1))
        self.assertLess(time.time() - t0, 3)
        self.assertFalse(dispatch.nap(0.2, step=0.1))

    def test_cli(self):
        env = dict(os.environ)
        r = subprocess.run([sys.executable, os.path.join(PUP, "orders.py"), "add", "do", "a town run"],
                           capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("queued", r.stdout)
        oid = orders.open_orders(self.path)[0]["id"]
        r = subprocess.run([sys.executable, os.path.join(PUP, "orders.py"), "done", oid, "townrun", "started"],
                           capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        r = subprocess.run([sys.executable, os.path.join(PUP, "orders.py"), "list"], capture_output=True, text=True,
                           env=env)
        self.assertIn("[owner] do a town run -> townrun started", r.stdout)

    def test_tell_ayla_cmd_exists(self):
        p = os.path.join(TOOLS, "rogui", "tell-ayla.cmd")
        body = open(p, encoding="utf-8").read()
        self.assertIn("ayla.sh tell %*", body)
        sh = open(os.path.join(TOOLS, "rogui", "ayla.sh"), encoding="utf-8").read()
        self.assertIn("tell)", sh)
        self.assertNotIn("\n  tell) send", sh)  # tell only queues, never writes to her console
