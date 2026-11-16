"""Tests for the Market tab (DB11): the no-hard-code rule (a), the endpoint with a fake engine and fixture files (b), the
offline behaviour (c) and the node DOM run of the tab (d). No docker, no engine, no live data: temp folders and the fake
engine. Run: python -m unittest discover -s tests/dashboard -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import ast
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "dashboard"))
sys.path.insert(0, HERE)
from test_dashserve import Base, get  # noqa: E402  (the server harness; its own tests are not re-run here)
import marketview  # noqa: E402

DASH = os.path.join(REPO, "legion", "dashboard.html")
FACTS = marketview.FACTS_MD
NUM = re.compile(r"(?<![\w.#-])\d+(?:\.\d+)?")
ALLOWED_JS = {"0", "1", "2", "30000"}          # array index and the 30 s refresh of the tab: not facts
UNIT_OK = {0, 1, 2, 4, 24, 60, 100, 3600}    # 24 h, 60 s, 100 percent, 3600 s, 4 rounding digits: not facts


def read_text(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def market_parts(html):
    """(the Market tab's markup, the Market script block) of the dashboard page"""
    i = html.index('id="tab-market"')
    j = html.index("</section>", i)
    k = html.index("// ======================= Market (DB11)")
    m = html.index("// ======================= From files (DB08)")
    return html[i:j], html[k:m]


def strip_html(s):
    s = re.sub(r"<!--.*?-->", "", s, flags=re.S)
    return re.sub(r'style="[^"]*"', "", s)   # style attributes carry CSS sizes, not facts


def hardcoded(part_html, part_js, fact_values):
    """what the Market part of the page types in: number literals (other than the allowed ones) and fact values"""
    js = re.sub(r"//[^\n]*", "", part_js)
    js = re.sub(r"font-size:[\d.]+px", "", js)   # CSS sizes in the page's own style strings, not facts
    html = strip_html(part_html)
    found = []
    found += ["js literal " + m.group(0) for m in NUM.finditer(js) if m.group(0) not in ALLOWED_JS]
    found += ["html literal " + m.group(0) for m in NUM.finditer(html)]
    for v in fact_values:
        if re.search(r"(?<!\d)" + re.escape(v) + r"(?!\d)", html + js):
            found.append("fact value " + v)
    return found


def node_or_skip(test):
    import shutil as sh
    node = sh.which("node")
    if not node:
        test.skipTest("node not on PATH")
    return node


class NoHardcode(unittest.TestCase):
    """(a) the page and the endpoint code show what the server sends; a typed-in fact fails the test"""

    @classmethod
    def setUpClass(cls):
        with open(DASH, encoding="utf-8") as f:
            cls.html = f.read()
        cls.part_html, cls.part_js = market_parts(cls.html)
        block = marketview.parse_facts(read_text(FACTS))
        cls.values = sorted({str(int(f["value"])) for f in block["facts"]
                             if isinstance(f.get("value"), int) and not isinstance(f.get("value"), bool) and f["value"] >= 100})

    def test_page_market_part_hard_codes_nothing(self):
        self.assertEqual(hardcoded(self.part_html, self.part_js, self.values), [])

    def test_the_rule_catches_a_typed_in_fact(self):   # the check itself works: a mutated copy must fail
        typed_js = self.part_js + "\nconst CREAMY = 4040;\n"
        self.assertTrue(any("4040" in f for f in hardcoded(self.part_html, typed_js, self.values)))
        typed_html = self.part_html.replace("<h2>Market engine</h2>", "<h2>Market engine</h2><p>cap 12</p>", 1)
        self.assertTrue(any("12" in f for f in hardcoded(typed_html, self.part_js, self.values)))

    def test_endpoint_code_has_no_literal_numbers(self):
        with open(marketview.__file__, encoding="utf-8") as f:
            tree = ast.parse(f.read())
        bad = [(n.lineno, n.value) for n in ast.walk(tree)
               if isinstance(n, ast.Constant) and isinstance(n.value, (int, float)) and not isinstance(n.value, bool)
               and n.value not in UNIT_OK]
        self.assertEqual(bad, [])

    def test_the_tab_is_registered(self):
        self.assertIn("'market'", self.html)
        self.assertIn('data-tab="market"', self.html)


class MarketUp(Base):
    """(b) the engine answers: every section takes its data from the engine's API or from the files it names"""
    engine_alive = True
    with_data = False

    def setUp(self):
        super().setUp()
        self.fe.market_files(self.d)

    def view(self):
        code, _, body = get(self.url + "/api/market/view")
        self.assertEqual(code, 200)
        return json.loads(body)

    def test_engine_section(self):
        S = self.view()["sections"]
        self.assertEqual(S["engine"]["source"], "/api/market/engine")
        self.assertEqual(S["engine"]["engine_status"], "on")
        flags = {f["flag"]: f["state"] for f in S["engine"]["flags"]}
        self.assertEqual(flags["market_engine_on"], "on")               # the engine's own flag
        self.assertEqual(flags["farmer_orders_on"], "off")
        self.assertEqual(flags["market_broadcast_on"], "on")            # from tuning.json
        self.assertEqual(flags["dynamic_price_on"], "not in tuning.json")
        self.assertEqual(S["engine"]["needs"][0]["item"], "Creamy Card")  # the biggest demand in its band
        self.assertEqual([n["rank"] for n in S["engine"]["needs"]], [1, 1])
        self.assertEqual(S["engine"]["allocation"][0]["farmers"], 3)
        self.assertEqual(S["engine"]["caps"][0], {"name": "per_item_max", "value": 12})
        self.assertIn("engine missing: caps.farmer_max", S["engine"]["missing"])
        self.assertEqual(S["engine"]["feed_source"], "/api/market/feed")

    def test_farmers_section(self):
        F = self.view()["sections"]["farmers"]
        self.assertEqual(F["roster"]["count"], 3)
        self.assertEqual(F["roster"]["phases"], [{"phase": "1", "n": 2}, {"phase": "2", "n": 1}])
        self.assertEqual(F["orders"]["count"], 1)
        self.assertEqual(F["acceptance"]["rate_pct"], 75.0)            # 18 accepted of 24
        self.assertEqual(F["declines"][0], {"reason": "unsafe map", "n": 4})
        self.assertEqual((F["kite"]["steps"], F["kite"]["deaths"]), (120, 3))
        self.assertEqual(F["kite"]["source"], "/api/economy (kite)")
        self.assertEqual(F["zeny_h"][0]["zph_mean"], 8300)

    def test_prices_section(self):
        P = self.view()["sections"]["prices"]
        row = P["rows"][0]
        self.assertEqual((row["price"], row["reference"], row["scarcity"]), (82000, 70000, 1.2))
        self.assertAlmostEqual(row["vs_reference_pct"], 82000 * 100 / 70000, places=6)
        self.assertEqual((row["history_n"], row["first"], row["last"]), (2, 78000, 82000))

    def test_broadcast_section(self):
        B = self.view()["sections"]["broadcast"]
        self.assertEqual(len(B["notices"]), 2)
        self.assertEqual(B["sent_n"], 1)
        self.assertIn({"name": "max_per_day", "value": 4}, B["cadence"])
        self.assertEqual(B["flag"]["state"], "on")
        self.assertEqual(B["notices_source"], "data/market_notices.jsonl")
        self.assertTrue(any("legion_notice" in m for m in B["missing"]))

    def test_facts_section_from_the_facts_file(self):
        Fa = self.view()["sections"]["facts"]
        self.assertEqual(Fa["source"], "docs/design/market-engine-facts.md")
        by = {r["id"]: r for r in Fa["rows"]}
        self.assertEqual(by["creamy.card.id"]["value"], 4040)
        self.assertIn("item_db_etc.yml:3200", by["creamy.card.id"]["source"])
        self.assertAlmostEqual(by["derived.creamy.drop.normal_rate.pct"]["value"], 0.05, places=4)
        self.assertAlmostEqual(by["derived.creamy.drop.champion_rate.pct"]["value"], 0.25, places=4)
        self.assertEqual(by["derived.creamy.drop.normal_rate.kills"]["value"], 2000.0)
        self.assertIsNotNone(by["creamy.card.id"]["age_s"])

    def test_engine_routes_answer_directly(self):
        code, hdr, body = get(self.url + "/api/market/feed")
        self.assertEqual(code, 200)
        self.assertEqual(json.loads(body)["short"][0]["item_id"], 4040)


class MarketEngineDown(Base):
    """(c) the engine is down: the files it writes stand in (DB06, DB08), and the page says where each number came from"""
    engine_alive = False
    with_data = False

    def setUp(self):
        super().setUp()
        self.fe.market_files(self.d)

    def view(self):
        code, _, body = get(self.url + "/api/market/view")
        self.assertEqual(code, 200)
        return json.loads(body)

    def test_sections_come_from_the_files(self):
        S = self.view()["sections"]
        self.assertEqual(S["engine"]["source"], "data/market_engine.json")
        self.assertEqual(S["engine"]["feed_source"], "data/market_feed.json")
        self.assertEqual(S["engine"]["needs"][0]["item"], "Creamy Card")
        self.assertEqual(S["farmers"]["orders"]["source"], "data/farmer_orders.json")
        self.assertEqual((S["farmers"]["kite"]["steps"], S["farmers"]["kite"]["deaths"]), (120, 3))
        self.assertEqual(S["farmers"]["kite"]["source"], "data/kite_counters.json")
        self.assertEqual(S["broadcast"]["notices_source"], "data/market_notices.jsonl")
        self.assertEqual(S["prices"]["source"], "data/market_engine.json")

    def test_stand_in_routes_say_files(self):
        for route in ("/api/market/engine", "/api/market/feed", "/api/market/orders"):
            code, hdr, _ = get(self.url + route)
            self.assertEqual(code, 200, route)
            self.assertEqual(hdr.get("X-Dash-Source"), "files", route)
        code, _, body = get(self.url + "/api/market/engine")
        self.assertEqual(json.loads(body)["status"], "on")

    def test_flags_stay_the_tuning_file_when_the_engine_is_silent(self):
        flags = {f["flag"]: f["state"] for f in self.view()["sections"]["engine"]["flags"]}
        self.assertEqual(flags["market_engine_on"], "on")   # from the file's own flags
        self.assertEqual(flags["guild_market_feed_on"], "off")


class MarketNothingOnDisk(Base):
    """(c) the engine is down and no file exists: every section says what is not available, nothing is invented"""
    engine_alive = False
    with_data = False

    def test_sections_say_source_not_available(self):
        code, _, body = get(self.url + "/api/market/view")
        self.assertEqual(code, 200)
        S = json.loads(body)["sections"]
        self.assertEqual(S["engine"]["status"], "source not available")
        self.assertIn("source not available: /api/market/engine, data/market_engine.json", S["engine"]["missing"])
        self.assertEqual(S["engine"]["needs"], [])
        self.assertIn("source not available: data/farmers.json", S["farmers"]["missing"])
        self.assertIn("source not available: data/market_notices.jsonl", S["broadcast"]["missing"])
        self.assertEqual(S["prices"]["status"], "source not available")
        self.assertEqual(S["broadcast"]["notices"], [])
        self.assertGreater(len(S["facts"]["rows"]), 0)   # the facts file is in the repo: it does not depend on the engine

    def test_engine_route_is_down(self):
        code, _, body = get(self.url + "/api/market/engine")
        self.assertEqual(code, 503)
        self.assertTrue(json.loads(body)["down"])

    def test_page_has_the_tab(self):
        code, _, body = get(self.url + "/")
        self.assertEqual(code, 200)
        self.assertIn(b'id="tab-market"', body)


class MarketDomRun(Base):
    """(d) the page's own Market script in node with a fake DOM, against the server (market_dom.js)"""
    engine_alive = True
    with_data = False
    down_files = True

    def setUp(self):
        super().setUp()
        if self.down_files:
            self.fe.market_files(self.d)

    def run_market(self):
        node = node_or_skip(self)
        html = get(self.url + "/")[2].decode("utf-8")
        script = max(re.findall(r"<script>(.*?)</script>", html, re.S), key=len)
        d = tempfile.mkdtemp()
        try:
            path = os.path.join(d, "page.js")
            with open(path, "w", encoding="utf-8", newline="\n") as f:
                f.write(script)
            r = subprocess.run([node, os.path.join(HERE, "market_dom.js"), path, self.url],
                               capture_output=True, text=True, timeout=120)
        finally:
            shutil.rmtree(d, ignore_errors=True)
        self.assertEqual(r.returncode, 0, (r.stdout + r.stderr)[-3000:])
        return json.loads(r.stdout.strip().splitlines()[-1])


class MarketDomUp(MarketDomRun):
    """(d) the engine answers: the cards show the engine's data and name their sources"""

    def test_cards_show_the_engine_data_and_their_sources(self):
        out = self.run_market()
        self.assertEqual([e for e in out["errors"] if not e.startswith("background")], [])
        self.assertEqual(out["tabShown"], "market")
        self.assertIn("Creamy Card", out["cards"]["mkEngine"])
        self.assertIn("/api/market/engine", out["cards"]["mkEngine"])
        self.assertIn("item_db_etc.yml:3200", out["cards"]["mkFacts"])
        self.assertIn("4,040", out["cards"]["mkFacts"])
        self.assertIn("market_notices.jsonl", out["cards"]["mkBroadcast"])
        self.assertIn("as of", out["note"])


class MarketDomNothing(MarketDomRun):
    """(d) engine down and no files: each card names the missing source; the facts still come from the facts file"""
    engine_alive = False
    down_files = False

    def test_cards_name_the_missing_sources(self):
        out = self.run_market()
        self.assertIn("source not available: /api/market/engine", out["cards"]["mkEngine"])
        self.assertIn("source not available: data/farmers.json", out["cards"]["mkFarmers"])
        self.assertIn("4,040", out["cards"]["mkFacts"])

    def test_no_card_is_blank(self):
        out = self.run_market()
        self.assertTrue(all(out["cards"][k].strip() for k in out["cards"]))


class Small(unittest.TestCase):
    """the parsing and the derived values, with no server"""

    def test_facts_block_parses_and_every_fact_has_a_source(self):
        block = marketview.parse_facts(read_text(FACTS))
        self.assertIsNotNone(block)
        self.assertGreaterEqual(len(block["facts"]), 20)
        for f in block["facts"]:
            self.assertTrue(f.get("id") and f.get("source"), f)

    def test_derived_values_follow_the_inputs(self):
        by = {f["id"]: f for f in marketview.parse_facts(read_text(FACTS))["facts"]}
        got = {d["id"]: d["value"] for d in marketview.derive(by)}
        self.assertAlmostEqual(got["derived.creamy.drop.normal_rate.pct"], 0.05, places=4)
        self.assertAlmostEqual(got["derived.creamy.drop.champion_rate.pct"], 0.25, places=4)
        self.assertEqual(got["derived.creamy.drop.champion_rate.kills"], 400.0)
        # a changed server rate changes the result: nothing is fixed in the code
        by["server.item_rate_card"] = dict(by["server.item_rate_card"], value=200)
        got2 = {d["id"]: d["value"] for d in marketview.derive(by)}
        self.assertAlmostEqual(got2["derived.creamy.drop.normal_rate.pct"], 0.02, places=4)

    def test_no_facts_block_is_not_invented(self):
        d = tempfile.mkdtemp()
        try:
            p = os.path.join(d, "facts.md")
            with open(p, "w", encoding="utf-8") as f:
                f.write("# no block here\n")
            S = marketview.section_facts(time.time(), p)
            self.assertEqual(S["status"], "source not available")
            self.assertEqual(S["rows"], [])
        finally:
            shutil.rmtree(d, ignore_errors=True)

    def test_ranking_is_per_band_by_demand(self):
        rows = [{"band": "b", "demand": 1, "item": "x"}, {"band": "a", "demand": 5, "item": "y"},
                {"band": "b", "demand": 9, "item": "z"}, {"band": "a", "demand": 2, "item": "w"}]
        got = [(r["band"], r["item"], r["rank"]) for r in marketview.rank_needs(rows)]
        self.assertEqual(got, [("a", "y", 1), ("a", "w", 2), ("b", "z", 1), ("b", "x", 2)])

    def test_epoch_readings(self):
        self.assertEqual(marketview.epoch(12345), 12345.0)
        self.assertIsNone(marketview.epoch("0001-01-01T00:00:00Z"))
        self.assertIsNotNone(marketview.epoch("2026-11-15"))
        self.assertIsNotNone(marketview.epoch("2026-11-15T10:00:00.123456789+01:00"))
        self.assertIsNone(marketview.epoch(True))

    def test_engine_that_says_off_is_shown_off(self):
        eng = ({"status": "off", "caps": {"per_item_max": 12}, "allocation": [], "flags": {"market_engine_on": False}},
               "/api/market/engine", 5.0)
        S = marketview.section_engine(eng, None, {"market_engine_on": True})
        self.assertEqual(S["status"], "off")
        self.assertEqual({f["flag"]: f["state"] for f in S["flags"]}["market_engine_on"], "off")   # the engine says so

    def test_view_with_no_engine_and_no_files(self):
        d = tempfile.mkdtemp()
        try:
            v = marketview.view(lambda p: None, lambda n: os.path.join(d, n), now=time.time(), tail=None,
                                facts_path=FACTS)
            S = v["sections"]
            self.assertEqual(set(S), {"engine", "farmers", "prices", "broadcast", "facts"})
            self.assertEqual(S["prices"]["status"], "source not available")
            self.assertEqual(S["farmers"]["roster"]["count"], None)
            self.assertEqual(S["broadcast"]["flag"]["state"], "not in tuning.json")
        finally:
            shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
