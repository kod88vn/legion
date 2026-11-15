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


def call_api(base, method, path, body=None):
    """one request with a JSON body (or none); returns (status, headers, bytes) for HTTP errors too"""
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
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
        req = urllib.request.Request(self.url + "/api/minds/chat/message", data=b'{"text":"hi"}', method="POST")
        with self.assertRaises(urllib.error.HTTPError) as cm:
            urllib.request.urlopen(req, timeout=10)
        self.assertEqual(cm.exception.code, 503)


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
        finally:
            if old is None:
                os.environ.pop("LEGION_DATA", None)
            else:
                os.environ["LEGION_DATA"] = old


if __name__ == "__main__":
    unittest.main()
