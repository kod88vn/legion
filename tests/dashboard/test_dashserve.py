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
