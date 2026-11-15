"""Tests for the standalone dashboard server. No docker, no engine, no live data: temp folders and a fake engine.
Run: python -m unittest discover -s tests/dashboard -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import json
import os
import shutil
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
os.environ["DASH_AYLA_LOGS"] = tempfile.mkdtemp()   # never the real OpenKore logs
os.environ["DASH_SNAP"] = tempfile.mkdtemp()        # never the real snapshots
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "dashboard"))
sys.path.insert(0, HERE)


def get(url):
    try:
        with urllib.request.urlopen(url, timeout=10) as r:
            return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()


def call_api(base, method, path, body=None, headers=None, raw=None):
    """one request; returns (status, headers, bytes) for HTTP errors too. By default it looks like the dashboard's own
    page: Origin is the dashboard, and a JSON body is sent as application/json. headers=... replaces every header
    (a cross-site or rebound request); raw=... sends those bytes as the body."""
    data = raw if raw is not None else (None if body is None else json.dumps(body).encode())
    if headers is None:   # every write declares JSON, bodyless DELETE included (the page does the same)
        headers = {"Origin": base}
        if data is not None or method == "DELETE":
            headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, method=method)
    for k, v in headers.items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()


class Base(unittest.TestCase):
    engine_alive = False
    data_alive = False
    with_data = True

    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.cache = tempfile.mkdtemp()
        os.environ["LEGION_DATA"] = self.d
        import dashserve
        import fake_engine
        self.ds, self.fe = dashserve, fake_engine
        dashserve.CACHE_DIR = self.cache
        if self.with_data:
            fake_engine.sample_data(self.d, alive=self.data_alive)
        self.fake = ThreadingHTTPServer(("127.0.0.1", 0), fake_engine.H)
        self.fake_port = self.fake.server_address[1]
        if self.engine_alive:
            threading.Thread(target=self.fake.serve_forever, daemon=True).start()
        else:
            self.fake.server_close()  # the port is now closed: an engine that is down
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), dashserve.Handler)
        self.srv.verbose, self.srv.started = False, time.time()
        self.srv.engine = dashserve.Engine("http://127.0.0.1:%d" % self.fake_port)
        self.srv.watch = dashserve.ContainerWatch(enabled=False, engine=self.srv.engine)
        self.srv.watch.poll()
        self.srv.players = dashserve.Players(self.srv.watch)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.url = "http://127.0.0.1:%d" % self.srv.server_address[1]

    def tearDown(self):
        self.srv.shutdown(); self.srv.server_close()
        if self.engine_alive:
            self.fake.shutdown(); self.fake.server_close()
        shutil.rmtree(self.d, ignore_errors=True)
        shutil.rmtree(self.cache, ignore_errors=True)
        testenv.isolate()

    def status(self):
        code, _, body = get(self.url + "/status")
        self.assertEqual(code, 200)
        return json.loads(body)


class EverythingDown(Base):
    """no engine, an empty data folder: the page and /status still answer and say what is missing"""
    with_data = False

    def test_page_and_status(self):
        code, _, body = get(self.url + "/")
        self.assertEqual(code, 200)
        self.assertIn(b"Puppet master decisions", body)
        st = self.status()
        self.assertFalse(st["engine"]["up"])
        self.assertTrue(all(l["state"] == "missing" for l in st["loops"]))
        self.assertFalse(st["bugq"]["available"])
        self.assertFalse(st["deploy"]["available"])
        self.assertEqual(st["summary"]["loops_up"], 0)

    def test_api_says_down(self):
        code, _, body = get(self.url + "/api/overview")
        self.assertEqual(code, 503)
        self.assertTrue(json.loads(body)["down"])


class StackStopped(Base):
    """no engine, files left from the last run: stale heartbeats, the queue counts, the last deploy, saved minds"""

    def test_status_from_files(self):
        st = self.status()
        by = {l["key"]: l for l in st["loops"]}
        self.assertEqual(by["sentinel"]["state"], "stale")
        self.assertEqual(by["dispatcher"]["state"], "stale")
        self.assertEqual(by["watchdog"]["state"], "missing")
        self.assertTrue(st["markers"]["stack.hold"])
        b = st["bugq"]
        self.assertEqual((b["total"], b["open"], b["unassigned"], b["assigned"], b["closed"]), (12, 11, 9, 2, 1))
        self.assertEqual(st["deploy"]["last"]["tag"], "ro-legion:deploy-sample-2")
        self.assertEqual(st["dispatcher"]["roles"]["ayla"]["last_call"]["exit"], 0)
        self.assertTrue(all(c["source"] == "ports" for c in st["containers"] if c["ports"]))

    def test_minds_from_the_save_file(self):
        code, hdr, body = get(self.url + "/api/minds")
        self.assertEqual(code, 200)
        self.assertEqual(hdr.get("X-Dash-Source"), "file")
        self.assertIn("X-Dash-Stale", hdr)
        self.assertIn("market", [m["name"] for m in json.loads(body)])
        code, _, body = get(self.url + "/api/minds/all/thoughts?since=50&limit=500")
        self.assertEqual(code, 200)
        self.assertTrue(all(t["id"] > 50 for t in json.loads(body)["thoughts"]))

    def test_post_fails_cleanly(self):
        code, _, body = call_api(self.url, "POST", "/api/minds/chat/message", {"text": "hi"})
        self.assertEqual(code, 503)
        self.assertTrue(json.loads(body)["down"])


class StackUp(Base):
    engine_alive = True
    data_alive = True

    def test_live_then_saved(self):
        code, hdr, body = get(self.url + "/api/overview")
        self.assertEqual(code, 200)
        self.assertNotIn("X-Dash-Stale", hdr)
        self.assertEqual(json.loads(body)["population"]["online"], 812)
        st = self.status()
        self.assertTrue(st["engine"]["up"])
        self.assertEqual({l["key"]: l["state"] for l in st["loops"]}["partycheck"], "ok")
        # the engine goes away: the last good answer is served, marked as saved
        self.fake.shutdown(); self.fake.server_close(); self.engine_alive = False
        code, hdr, body = get(self.url + "/api/overview")
        self.assertEqual(code, 200)
        self.assertEqual(hdr.get("X-Dash-Source"), "cache")
        self.assertEqual(json.loads(body)["population"]["online"], 812)
        code, _, _ = get(self.url + "/api/leaderboard?limit=0")   # never asked while it was up
        self.assertEqual(code, 503)

    def test_incremental_polls_are_not_saved(self):
        self.assertIsNone(self.ds.cache_key("/api/minds/all/thoughts?since=41&limit=500"))
        self.assertIsNotNone(self.ds.cache_key("/api/minds/all/thoughts?since=0&limit=500"))


class FromFiles(Base):
    """engine down: the engine's files stand in for its endpoints, and the Players tab reads a snapshot"""

    def test_stand_ins(self):
        self.fe.engine_files(self.d)
        code, hdr, body = get(self.url + "/api/leaderboard?limit=0")
        self.assertEqual(code, 200)
        self.assertEqual(hdr.get("X-Dash-Source"), "files")
        top = json.loads(body)["top"]
        self.assertEqual([r["name"] for r in top[:2]], ["Silent Heals", "Zendancer84"])
        for route in ("/api/guild-rank", "/api/guilds/detail", "/api/friends?n=15", "/api/meter", "/api/meter/neo",
                      "/api/overview/history", "/api/parties/detail"):
            code, hdr, body = get(self.url + route)
            self.assertEqual((route, code), (route, 200))
        parties = json.loads(get(self.url + "/api/parties/detail")[2])
        self.assertEqual(parties[0]["members"][0]["detail"]["kpm"], 6.9)
        self.assertEqual(get(self.url + "/api/overview")[0], 503)   # no file has the full overview
        off = json.loads(get(self.url + "/offline")[2])
        self.assertEqual(off["population"]["characters"], 2)
        self.assertEqual(off["health"]["online"], 970)

    def test_players(self):
        self.fe.engine_files(self.d)
        self.fe.snapshot(os.environ["DASH_SNAP"])
        r = json.loads(get(self.url + "/players?q=zen")[2])
        self.assertEqual(r["rows"][0]["name"], "Zendancer84")
        self.assertEqual(r["rows"][0]["kind"], "Ayla's party")
        self.assertFalse(r["db_up"])
        d = json.loads(get(self.url + "/players/char?id=172745&name=Zendancer84")[2])
        self.assertEqual(d["source"], "snapshot")
        self.assertEqual(d["equipment"][0]["name"], "Biretta")
        self.assertEqual(d["char"]["zeny"], 6113)
        self.assertNotIn("password", json.dumps(d))
        self.assertEqual(d["engine"]["persona"]["account"], "lg2745")
        gm = json.loads(get(self.url + "/players?q=wander")[2])["rows"][0]
        self.assertEqual(gm["kind"], "GM")
        only_files = json.loads(get(self.url + "/players/char?name=Silent%20Heals")[2])
        self.assertIsNone(only_files["source"])
        self.assertEqual(only_files["char"]["base_level"], 80)


class OwnerTool(Base):
    """AY25 / DB10: the Ayla control panel. The engine is up (the fake engine wants the owner token on writes) or
    down. The browser never gets the owner token: dashserve adds it server-side on POST and DELETE only."""
    engine_alive = True
    data_alive = True

    def setUp(self):
        super().setUp()
        self.token = self.fe.OWNER_TOKEN
        with open(os.path.join(self.d, "owner.token"), "w", encoding="utf-8") as f:
            f.write(self.token + "\n")

    def call(self, method, path, body=None):
        return call_api(self.url, method, path, body)

    def test_panel_is_in_the_page(self):
        code, _, body = get(self.url + "/")
        self.assertEqual(code, 200)
        for part in (b'id="aylaCtl"', b"Ayla control", b'id="ctlFarmHours"', b'data-kind="guild_dir"', b'data-order="town_run"',
                     b'data-order="rally" data-value="cancel"', b'data-order="party_order" data-value="follow_me"',
                     b'id="ctlActive"', b'<option value="720">until cleared', b"Force anyway", b"/api/ayla/overrides",
                     b"/api/spots?level="):
            self.assertIn(part, body, part)
        self.assertIn(b"Puppet master decisions", body)   # G08: the existing tabs and panels are still there
        self.assertIn(b"Ayla's party", body)

    def test_script_parses(self):
        import re
        import shutil
        import subprocess
        import tempfile
        node = shutil.which("node")
        if not node:
            self.skipTest("node not on PATH")
        html = get(self.url + "/")[2].decode("utf-8")
        scripts = re.findall(r"<script>(.*?)</script>", html, re.S)
        self.assertTrue(scripts)
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "page.js")
            with open(path, "w", encoding="utf-8") as f:
                f.write(max(scripts, key=len))
            r = subprocess.run([node, "--check", path], capture_output=True, text=True, timeout=60)
            self.assertEqual(r.returncode, 0, r.stderr)
        finally:
            shutil.rmtree(d, ignore_errors=True)

    def run_panel(self, mode=None):
        """the page's own script in node with a fake DOM, against this server (panel_dom.js). Skips without node."""
        import re
        import shutil
        import subprocess
        import tempfile
        node = shutil.which("node")
        if not node:
            self.skipTest("node not on PATH")
        html = get(self.url + "/")[2].decode("utf-8")
        script = max(re.findall(r"<script>(.*?)</script>", html, re.S), key=len)
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "page.js")
            with open(path, "w", encoding="utf-8", newline="\n") as f:
                f.write(script)
            args = [node, os.path.join(HERE, "panel_dom.js"), path, self.url] + ([mode] if mode else [])
            r = subprocess.run(args, capture_output=True, text=True, timeout=120)
        finally:
            shutil.rmtree(d, ignore_errors=True)
        self.assertEqual(r.returncode, 0, (r.stdout + r.stderr)[-3000:])
        return json.loads(r.stdout.strip().splitlines()[-1])

    def test_flags_are_rendered_and_optional(self):
        # fix/ayla-hookin adds "flags" to GET /api/ayla/overrides: shown as a table, additive; absent = nothing shown
        self.fake.flags = [{"kind": "death_loop_kept", "party": "Ayla's party", "map": "pay_dun02", "time": 1794704772,
                            "text": "party died twice; override kept", "kept": 30}]
        res = self.run_panel()
        self.assertEqual([e for e in res["errors"] if not e.startswith("background:")], [])
        self.assertIn("death_loop_kept", res["flagsHtml"])
        self.assertIn("override kept", res["flagsHtml"])
        self.assertIn("30 min", res["flagsHtml"])
        self.fake.flags = None                                 # the field absent: no table, no error
        res = self.run_panel()
        self.assertEqual(res["flagsHtml"], "")
        self.assertEqual([e for e in res["errors"] if not e.startswith("background:")], [])

    def test_panel_script_fake_dom(self):
        """the page's own script, in node with a fake DOM, against this server: the 409 warning, "Force anyway"
        (confirm=true) and Clear. Skipped when node is not installed."""
        res = self.run_panel()
        # background refreshes (charts need real layout) are noted by the harness and ignored here
        self.assertEqual([e for e in res["errors"] if not e.startswith("background:")], [])
        posts = [c for c in res["calls"] if c["method"] == "POST"]
        self.assertEqual([c["status"] for c in posts], [409, 200])
        self.assertNotIn("confirm", posts[0]["body"])            # first try: no confirm
        self.assertTrue(posts[1]["body"]["confirm"])              # "Force anyway" re-sends with confirm=true
        self.assertEqual(posts[0]["body"]["value"], "moc_fild12")
        self.assertEqual((posts[0]["body"]["base_lv"], posts[0]["body"]["max_lv"]), (78, 83))   # from her party
        self.assertIn("Force anyway", res["msgs"][0])
        self.assertIn("fair gate", res["msgs"][0])
        self.assertIn("stored", res["msgs"][1])
        dels = [c for c in res["calls"] if c["method"] == "DELETE"]
        self.assertEqual([c["status"] for c in dels], [200])
        self.assertIn("cleared", res["msgs"][2])
        self.assertNotIn(self.token, json.dumps(res))

    def test_gate_warning_then_force_then_clear(self):
        farm = {"kind": "farm_map", "value": "moc_fild12", "hours": 24, "base_lv": 50, "max_lv": 54, "note": "test"}
        code, _, body = self.call("POST", "/api/ayla/overrides", farm)
        self.assertEqual(code, 409)                       # the gate answers 409 with the warnings; nothing stored
        warn = json.loads(body)
        self.assertTrue(warn["warnings"])
        self.assertFalse(warn["stored"])
        self.assertIsNone(json.loads(self.call("GET", "/api/ayla/overrides")[2])["owner"]["farm_map"])
        code, _, body = self.call("POST", "/api/ayla/overrides", dict(farm, confirm=True))   # "Force anyway"
        self.assertEqual(code, 200)
        oid = json.loads(body)["id"]
        view = json.loads(self.call("GET", "/api/ayla/overrides")[2])
        self.assertEqual(view["owner"]["farm_map"]["value"], "moc_fild12")
        self.assertEqual(view["puppet"]["starveil_guild"]["autopilot"][0]["value"], "pay_fild08")   # puppet shown beside
        # a second farm map ends the first ("replaced"); a guild direction is a separate kind
        self.assertEqual(self.call("POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08", "hours": 6})[0], 200)
        self.assertEqual(self.call("POST", "/api/ayla/overrides", {"kind": "guild_dir", "value": "level at pay_fild08", "hours": 720})[0], 200)
        self.assertEqual(self.call("DELETE", "/api/ayla/overrides/" + oid)[0], 409)   # already replaced: it ended
        self.assertEqual(self.call("DELETE", "/api/ayla/overrides/nope")[0], 404)
        gid = json.loads(self.call("GET", "/api/ayla/overrides")[2])["owner"]["guild_dir"]["id"]
        code, _, body = self.call("DELETE", "/api/ayla/overrides/" + gid)
        self.assertEqual(code, 200)
        self.assertEqual(json.loads(body)["end_reason"], "clear")
        self.assertIsNone(json.loads(self.call("GET", "/api/ayla/overrides")[2])["owner"]["guild_dir"])

    def test_bad_input_is_refused_with_the_reason(self):
        code, _, body = self.call("POST", "/api/ayla/overrides", {"kind": "weather", "value": "rain"})
        self.assertEqual(code, 400)
        self.assertIn("kind must be one of", json.loads(body)["error"])
        code, _, _ = self.call("POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08", "hours": 1000})
        self.assertEqual(code, 400)

    def test_token_never_reaches_the_browser(self):
        seen = [get(self.url + "/"), get(self.url + "/status"), get(self.url + "/api/ayla/overrides"),
                get(self.url + "/api/spots?level=50")]
        farm = {"kind": "farm_map", "value": "moc_fild12", "hours": 1}
        seen.append(self.call("POST", "/api/ayla/overrides", farm))
        seen.append(self.call("POST", "/api/ayla/overrides", dict(farm, confirm=True)))
        seen.append(self.call("DELETE", "/api/ayla/overrides/ov99"))
        seen.append(self.call("POST", "/api/ayla/overrides", {"kind": "weather", "value": "rain"}))
        for code, headers, body in seen:
            self.assertNotIn(self.token.encode(), body)
            self.assertNotIn(self.token, json.dumps(headers))
        self.assertEqual(seen[5][0], 200)   # the engine did get the token: the confirmed write was stored

    def test_engine_refusal_is_passed_on_not_faked(self):
        # no token file: the dashboard sends none and the engine refuses; the browser sees that refusal
        os.remove(os.path.join(self.d, "owner.token"))
        code, _, _ = self.call("POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08", "hours": 1})
        self.assertEqual(code, 403)


class WriteGuard(Base):
    """review of 74908d1: every write (POST or DELETE, any route) comes from this dashboard's own origin, as JSON, to a
    localhost Host; a write path reaches the engine unaltered or not at all; the token goes only on an exact path"""
    engine_alive = True
    data_alive = True

    def setUp(self):
        super().setUp()
        with open(os.path.join(self.d, "owner.token"), "w", encoding="utf-8") as f:
            f.write(self.fe.OWNER_TOKEN + "\n")
        self.port = self.srv.server_address[1]
        self.base = self.url
        self.farm = json.dumps({"kind": "farm_map", "value": "moc_fild12", "hours": 1, "confirm": True}).encode()

    def owner_farm(self):
        return json.loads(call_api(self.base, "GET", "/api/ayla/overrides")[2])["owner"]["farm_map"]

    def engine_writes(self):
        return list(getattr(self.fake, "writes", []))

    def test_refused_writes_never_store(self):
        port = self.port
        cases = [
            ("cross-site text/plain with confirm", {"Origin": "http://evil.example", "Content-Type": "text/plain"}),
            ("same origin, text/plain", {"Origin": self.base, "Content-Type": "text/plain"}),
            ("same origin, form body", {"Origin": self.base, "Content-Type": "application/x-www-form-urlencoded"}),
            ("json, no origin and no referer", {"Content-Type": "application/json"}),
            ("origin null", {"Origin": "null", "Content-Type": "application/json"}),
            ("origin of another site", {"Origin": "http://evil.example", "Content-Type": "application/json"}),
            ("localhost on another port", {"Origin": "http://localhost:%d" % (port + 1), "Content-Type": "application/json"}),
            ("https origin", {"Origin": "https://127.0.0.1:%d" % port, "Content-Type": "application/json"}),
            ("referer of another site", {"Referer": "http://evil.example/page", "Content-Type": "application/json"}),
            ("rebound host name", {"Host": "evil.example:%d" % port, "Origin": self.base, "Content-Type": "application/json"}),
        ]
        for why, headers in cases:
            code, _, body = call_api(self.base, "POST", "/api/ayla/overrides", headers=headers, raw=self.farm)
            self.assertEqual(code, 403, why)
            self.assertTrue(json.loads(body)["refused"], why)
            self.assertIsNone(self.owner_farm(), why)
        self.assertEqual(self.engine_writes(), [])   # nothing reached the engine

    def test_refusals_on_delete_and_other_routes(self):
        code, _, body = call_api(self.base, "POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08", "hours": 1})
        self.assertEqual(code, 200)
        oid = json.loads(body)["id"]
        evil = {"Origin": "http://evil.example", "Content-Type": "application/json"}
        self.assertEqual(call_api(self.base, "DELETE", "/api/ayla/overrides/" + oid, headers=evil)[0], 403)
        self.assertEqual(self.owner_farm()["id"], oid)                 # still active
        self.assertEqual(call_api(self.base, "POST", "/api/minds/chat/message", headers=evil, raw=b'{"text":"x"}')[0], 403)
        self.assertEqual(call_api(self.base, "DELETE", "/api/minds/x", headers=evil)[0], 403)
        self.assertEqual(self.engine_writes(), ["/api/ayla/overrides"])
        # the owner's own page still clears it
        self.assertEqual(call_api(self.base, "DELETE", "/api/ayla/overrides/" + oid)[0], 200)

    def test_accepted_origins(self):
        ok = {"Content-Type": "application/json"}
        for origin in ("http://127.0.0.1:%d" % self.port, "http://localhost:%d" % self.port):
            code, _, _ = call_api(self.base, "POST", "/api/ayla/overrides", headers=dict(ok, Origin=origin), raw=self.farm)
            self.assertIn(code, (200, 409), origin)     # reached the engine (409 is the gate on moc_fild12)
        code, _, _ = call_api(self.base, "POST", "/api/ayla/overrides",
                              headers={"Referer": self.base + "/", "Content-Type": "application/json; charset=utf-8"}, raw=self.farm)
        self.assertIn(code, (200, 409))                 # no Origin, but a Referer of this dashboard, with a charset

    def test_paths_are_refused_before_forwarding(self):
        bad = ["/api/ayla/overrides/../x", "/../../stats", "//x", "/api/ayla/overrides/%2e%2e/x", "/%2e%2e/x",
               "/api/ayla/overrides/a\\b", "/api//ayla/overrides"]
        for path in bad:
            for method in ("POST", "DELETE"):
                code, _, _ = call_api(self.base, method, path, {"kind": "farm_map", "value": "moc_fild12", "confirm": True})
                self.assertIn(code, (400, 404), (method, path))
                self.assertNotEqual(code, 200, (method, path))
        self.assertEqual(self.engine_writes(), [])      # none of them reached the engine

    def test_delete_is_only_for_the_override_route(self):
        self.assertEqual(call_api(self.base, "DELETE", "/api/minds/x")[0], 404)
        self.assertEqual(call_api(self.base, "DELETE", "/api/ayla/overrides/")[0], 404)
        self.assertEqual(self.engine_writes(), [])


class OwnerRequests(Base):
    """owner orders awaiting a decision: list (24 h expiry), decline, confirm against the fake engine, the guards.
    The file is a fixture in the test's own data folder; its format is the one tools/puppet/orders.py writes."""
    engine_alive = True
    data_alive = True

    def setUp(self):
        super().setUp()
        self.token = self.fe.OWNER_TOKEN
        with open(os.path.join(self.d, "owner.token"), "w", encoding="utf-8") as f:
            f.write(self.token + "\n")
        now = time.time()
        rows = [
            {"kind": "order", "id": "r_edge", "t": now - 23 * 3600, "text": "rally to prontera"},          # 23 h: listed
            {"kind": "order", "id": "r_old", "t": now - 25 * 3600, "text": "an old order"},              # 25 h: expired
            {"kind": "order", "id": "r_fresh", "t": now - 600, "text": "take the party to pay_fild08 for two hours",
             "value": "pay_fild08", "hours": 2},
            {"kind": "order", "id": "r_done", "t": now - 900, "text": "already confirmed"},
            {"kind": "status", "id": "r_done", "status": "confirmed", "by": "owner", "t": now - 800},
            {"kind": "order", "id": "r_gate", "t": now - 120, "text": "camp at moc_fild12", "value": "moc_fild12", "hours": 6},
            {"kind": "order", "id": "r_dec", "t": now - 60, "text": "decline this one"},
        ]
        self.path = os.path.join(self.d, "ayla_override_requests.jsonl")
        with open(self.path, "w", encoding="utf-8") as f:
            f.write("".join(json.dumps(r) + "\n" for r in rows))

    def listed(self):
        code, _, body = call_api(self.url, "GET", "/api/ayla/requests")
        self.assertEqual(code, 200)
        return json.loads(body)

    def lines(self):
        with open(self.path, encoding="utf-8") as f:
            return [json.loads(x) for x in f if x.strip()]

    def status(self, rid, st, headers=None):
        return call_api(self.url, "POST", "/api/ayla/requests/%s/status" % rid, {"status": st}, headers=headers)

    def test_list_pending_within_24_hours(self):
        rows = self.listed()["requests"]
        self.assertEqual([r["id"] for r in rows], ["r_edge", "r_fresh", "r_gate", "r_dec"])   # by time; not r_old, r_done
        fresh = next(r for r in rows if r["id"] == "r_fresh")
        self.assertEqual((fresh["value"], fresh["hours"], fresh["text"][:4]), ("pay_fild08", 2, "take"))
        self.assertEqual(self.listed()["lifetime_h"], 24)
        self.assertNotIn(self.fe.OWNER_TOKEN, json.dumps(rows))

    def test_decline_appends_a_status_row(self):
        code, _, body = self.status("r_dec", "declined")
        self.assertEqual(code, 200)
        last = self.lines()[-1]
        self.assertEqual({k: last[k] for k in ("kind", "id", "status", "by")},
                         {"kind": "status", "id": "r_dec", "status": "declined", "by": "owner"})
        self.assertNotIn("r_dec", [r["id"] for r in self.listed()["requests"]])
        self.assertEqual(self.status("r_dec", "declined")[0], 404)     # answered: no longer pending
        self.assertEqual(self.status("r_old", "declined")[0], 404)     # expired: no longer pending
        self.assertEqual(self.status("r_fresh", "maybe")[0], 400)      # only confirmed or declined
        self.assertEqual(self.status("r_fresh", "declined", headers={"Origin": "http://evil.example",
                                                                    "Content-Type": "application/json"})[0], 403)

    def raw_post(self, path, length_header):
        """one raw HTTP POST, so the header can say anything (urllib would fix Content-Length itself)"""
        import socket
        host, port = self.srv.server_address
        head = ("POST %s HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nOrigin: %s\r\nContent-Type: application/json\r\n"
                "Content-Length: %s\r\nConnection: close\r\n\r\n" % (path, port, self.url, length_header)).encode()
        with socket.create_connection((host, port), timeout=5) as s:
            s.sendall(head + b'{"status":"declined"}')
            data = b""
            while True:   # a hung handler shows up as socket.timeout here, not as a silent pass
                chunk = s.recv(65536)
                if not chunk:
                    return data
                data += chunk

    def test_negative_content_length_is_refused_not_hung(self):
        """rfile.read(-1) waits for the peer to close: a negative Content-Length must get a 400, never a hang"""
        before = len(self.lines())
        for bad in ("-1", "+5", "abc"):
            data = self.raw_post("/api/ayla/requests/r_dec/status", bad)
            self.assertTrue(data.startswith(b"HTTP/1.1 400"), (bad, data[:40]))
            self.assertIn(b"bad Content-Length", data)
        self.assertEqual(len(self.lines()), before)            # nothing was recorded
        self.assertEqual(self.status("r_dec", "declined")[0], 200)   # the request is still pending and answerable

    def test_guards_on_the_request_routes(self):
        before = len(self.lines())
        evil = {"Origin": "http://evil.example", "Content-Type": "text/plain"}
        code, _, _ = call_api(self.url, "POST", "/api/ayla/requests/r_fresh/status", headers=evil,
                              raw=b'{"status":"confirmed"}')
        self.assertEqual(code, 403)
        # a leading "//" never gets here: http.server collapses it before the handler (the path that arrives is normal)
        for bad in ("/api/ayla/requests/../r_fresh/status", "/api/ayla/requests/%2e%2e/status", "/api/ayla/requests//r_fresh/status"):
            self.assertEqual(call_api(self.url, "POST", bad, {"status": "declined"})[0], 400, bad)
        self.assertEqual(len(self.lines()), before)     # nothing was written by a refused request
        self.assertIn("r_fresh", [r["id"] for r in self.listed()["requests"]])

    def test_confirm_flow_against_the_fake_engine(self):
        # the page's order: the override first (token added by the dashboard), then the order is marked confirmed
        code, _, _ = call_api(self.url, "POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08", "hours": 2})
        self.assertEqual(code, 200)
        self.assertEqual(self.status("r_fresh", "confirmed")[0], 200)
        self.assertNotIn("r_fresh", [r["id"] for r in self.listed()["requests"]])
        self.assertEqual(self.lines()[-1]["status"], "confirmed")
        # a gate warning (409) stores nothing, so the order must stay pending until the owner forces it
        code, _, _ = call_api(self.url, "POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "moc_fild12", "hours": 6})
        self.assertEqual(code, 409)
        self.assertIn("r_gate", [r["id"] for r in self.listed()["requests"]])
        code, _, _ = call_api(self.url, "POST", "/api/ayla/overrides",
                              {"kind": "farm_map", "value": "moc_fild12", "hours": 6, "confirm": True})
        self.assertEqual(code, 200)
        self.assertEqual(self.status("r_gate", "confirmed")[0], 200)
        self.assertNotIn("r_gate", [r["id"] for r in self.listed()["requests"]])

    def test_confirm_form_in_the_page(self):
        import re
        import shutil
        import subprocess
        import tempfile
        node = shutil.which("node")
        if not node:
            self.skipTest("node not on PATH")
        html = get(self.url + "/")[2].decode("utf-8")
        script = max(re.findall(r"<script>(.*?)</script>", html, re.S), key=len)
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "page.js")
            with open(path, "w", encoding="utf-8", newline="\n") as f:
                f.write(script)
            r = subprocess.run([node, os.path.join(HERE, "panel_dom.js"), path, self.url, "confirm"],
                               capture_output=True, text=True, timeout=120)
        finally:
            shutil.rmtree(d, ignore_errors=True)
        self.assertEqual(r.returncode, 0, (r.stdout + r.stderr)[-3000:])
        res = json.loads(r.stdout.strip().splitlines()[-1])
        self.assertEqual([e for e in res["errors"] if not e.startswith("background:")], [])
        self.assertIn("r_fresh", res["listed"])
        self.assertEqual(res["prefill"]["value"], "pay_fild08")           # prefilled from the order
        self.assertEqual(res["prefill"]["kind"], "farm_map")              # the owner's pick, the default
        self.assertEqual(res["prefill"]["hours"], "6")                    # 2 h asked: the next choice up
        self.assertTrue(res["prefill"]["shown"])
        posts = [c for c in res["calls"] if c["method"] == "POST"]
        self.assertEqual([c["url"] for c in posts], ["/api/ayla/overrides", "/api/ayla/requests/r_fresh/status"])
        self.assertEqual([c["status"] for c in posts], [200, 200])
        self.assertEqual(posts[1]["body"], {"status": "confirmed"})
        self.assertNotIn("r_fresh", res["listedAfter"])
        self.assertTrue(res["formHidden"])
        self.assertNotIn(self.token, json.dumps(res))

class OwnerToolEngineDown(Base):
    """the engine is down: the page is read-only, writes answer 503 and nothing is stored"""
    engine_alive = False
    data_alive = True

    def setUp(self):
        super().setUp()
        self.token = self.fe.OWNER_TOKEN
        with open(os.path.join(self.d, "owner.token"), "w", encoding="utf-8") as f:
            f.write(self.token + "\n")

    def test_read_only_when_down(self):
        code, _, body = get(self.url + "/")
        self.assertEqual(code, 200)
        self.assertIn(b"read-only: the engine is not answering", body)
        self.assertEqual(call_api(self.url, "GET", "/api/ayla/overrides")[0], 503)
        self.assertEqual(call_api(self.url, "GET", "/api/ayla/requests")[0], 200)   # read here: answers with the engine down
        for method, path, data in (("POST", "/api/ayla/overrides", {"kind": "farm_map", "value": "pay_fild08"}),
                                   ("DELETE", "/api/ayla/overrides/ov1", None)):
            code, _, body = call_api(self.url, method, path, data)
            self.assertEqual(code, 503)
            self.assertTrue(json.loads(body)["down"])
        self.assertNotIn(self.token.encode(), get(self.url + "/")[2])


class Small(unittest.TestCase):
    def test_go_time(self):
        import dashserve
        self.assertAlmostEqual(dashserve.go_time("2026-11-15T01:06:12.123456789Z"), 1794704772.123456, places=3)
        self.assertAlmostEqual(dashserve.go_time("2026-11-15T09:06:12.5+08:00"), 1794704772.5, places=3)
        self.assertIsNone(dashserve.go_time("0001-01-01T00:00:00Z"))

    def test_read_only_guard(self):
        import dbsnap
        for bad in ("UPDATE `char` SET zeny = 0", "SELECT 1; DELETE FROM login", "delete from login", "INSERT INTO x VALUES (1)"):
            with self.assertRaises(ValueError):
                dbsnap.run([bad])

    def test_owner_token_only_on_override_writes(self):
        import tempfile
        import dashserve
        d = tempfile.mkdtemp()
        with open(os.path.join(d, "owner.token"), "w", encoding="utf-8") as f:
            f.write("tok123\n")
        old = os.environ.get("LEGION_DATA")
        os.environ["LEGION_DATA"] = d
        try:
            self.assertEqual(dashserve.owner_headers("POST", "/api/ayla/overrides"), {"X-Owner-Token": "tok123"})
            self.assertEqual(dashserve.owner_headers("DELETE", "/api/ayla/overrides/ov1"), {"X-Owner-Token": "tok123"})
            self.assertEqual(dashserve.owner_headers("POST", "/api/ayla/overrides?x=1"), {"X-Owner-Token": "tok123"})
            # GET is never sent the token, nor any other route, nor a look-alike path
            self.assertEqual(dashserve.owner_headers("GET", "/api/ayla/overrides"), {})
            self.assertEqual(dashserve.owner_headers("POST", "/api/guild-direction/guild01"), {})
            self.assertEqual(dashserve.owner_headers("POST", "/api/ayla/overridesX"), {})
            self.assertEqual(dashserve.owner_headers("POST", "/api/bots/x/goal"), {})
            # review of 74908d1: the token goes only on an exact override path, never on a traversal or escape
            for bad in ("/api/ayla/overrides/../x", "/../../api/ayla/overrides", "//api/ayla/overrides/x",
                        "/api/ayla/overrides/%2e%2e/x", "/api/ayla/overrides/a\\b", "/api/ayla/overrides/a b",
                        "/api/ayla/overrides/ov1\n", "/api/ayla/overrides/ov1?x=%2e"):
                self.assertEqual(dashserve.owner_headers("POST", bad), {}, bad)
                self.assertEqual(dashserve.owner_headers("DELETE", bad), {}, bad)
        finally:
            if old is None:
                os.environ.pop("LEGION_DATA", None)
            else:
                os.environ["LEGION_DATA"] = old


if __name__ == "__main__":
    unittest.main()
