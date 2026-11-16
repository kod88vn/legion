"""Tests for the Market tab (DB11): the no-hard-code rule (a), the endpoint with the fixtures and a fake engine (b), the
offline behaviour (c), the node DOM run of the tab (d), and the contract: the fixtures hold the real shapes and the
game constants the facts block copies are the Go code's. No docker, no engine, no live data: temp folders and fixtures.
Run: python -m unittest discover -s tests/dashboard -v   (from the repo root)"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), ".."))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import ast
import json
import os
import random
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
sys.path.insert(0, os.path.join(REPO, "tools", "system"))
sys.path.insert(0, HERE)
from test_dashserve import Base, get  # noqa: E402  (the server harness; its own tests are not re-run here)
import marketview  # noqa: E402
import crier  # noqa: E402

DASH = os.path.join(REPO, "legion", "dashboard.html")
FACTS = marketview.FACTS_MD
FIX = os.path.join(HERE, "fixtures")
GO = os.path.join(REPO, "legion")
NUM = re.compile(r"(?<![\w.#-])\d+(?:\.\d+)?")
ALLOWED_JS = {"0", "1", "2", "30000"}          # array index and the 30 s refresh of the tab: not facts
UNIT_OK = {0, 1, 2, 4, 24, 60, 100, 3600}      # 24 h, 60 s, 100 percent, 3600 s, 4 rounding digits: not facts


def read_text(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def fixture(name):
    return json.loads(read_text(os.path.join(FIX, name)))


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
    node = shutil.which("node")
    if not node:
        test.skipTest("node not on PATH")
    return node


def go_struct_keys(src, name):
    """the json field names of a Go struct, from its tags; None when the struct is not in src"""
    m = re.search(r"type %s struct \{(.*?)\n\}" % re.escape(name), src, re.S)
    if not m:
        return None
    return set(re.findall(r'json:"([^",]+)', m.group(1)))


def go_func_text(src, name):
    m = re.search(r"func %s\(.*?\n\}\n" % re.escape(name), src, re.S)
    return m.group(0) if m else ""


def go_const(src, name):
    m = re.search(r"\b%s\s*=\s*(\d+)" % re.escape(name), src)
    return int(m.group(1)) if m else None


class NoHardcode(unittest.TestCase):
    """(a) the page and the endpoint code show what the server sends; a typed-in fact fails the test"""

    @classmethod
    def setUpClass(cls):
        cls.html = read_text(DASH)
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


class Contract(unittest.TestCase):
    """the fixtures hold the real shapes: every key the page reads exists in the Go struct or encoder that writes it,
    and the crier's rows and rules are what crier.py produces now (no drift)"""

    @classmethod
    def setUpClass(cls):
        cls.go = {}
        for f in ("marketengine.go", "marketmetrics.go", "kite.go", "guildstock.go", "teleport.go", "farmers.go"):
            p = os.path.join(GO, f)
            cls.go[f] = read_text(p) if os.path.exists(p) else None

    def go_keys(self, f, struct):
        src = self.go[f]
        if src is None:
            self.skipTest(f + " not on this branch")
        keys = go_struct_keys(src, struct)
        self.assertIsNotNone(keys, struct + " in " + f)
        return keys

    def test_engine_answer_keys_are_encoded_by_registerMarketEngine(self):
        src = self.go["marketengine.go"]
        reg = src[src.index("func registerMarketEngine"):src.index('mux.HandleFunc("/api/market/feed"')]
        encoded = set(re.findall(r'out\["(\w+)"\]', reg))
        encoded |= set(re.findall(r'"(\w+)":\s', next(l for l in reg.splitlines() if "out := map" in l)))
        self.assertLessEqual(set(fixture("market_engine.json")) - {"metrics"}, encoded | {"status", "runs", "last_run"})

    def test_needs_orders_caps_distribution_match_the_structs(self):
        e = fixture("market_engine.json")
        for row in e["needs"]:
            self.assertLessEqual(set(row), self.go_keys("marketengine.go", "MEGap"))
        for row in e["orders"]:
            self.assertLessEqual(set(row), self.go_keys("marketengine.go", "MEOrder"))
            for r in row.get("rank", []):
                self.assertLessEqual(set(r), self.go_keys("marketengine.go", "MERank"))
        self.assertLessEqual(set(e["caps"]), self.go_keys("marketengine.go", "MECaps"))
        self.assertLessEqual(set(e["distribution"]), self.go_keys("marketengine.go", "MEDist"))

    def test_feed_matches_the_feed_structs(self):
        f = fixture("market_feed.json")
        self.assertLessEqual(set(f), self.go_keys("marketengine.go", "MEFeed"))
        self.assertLessEqual(set(f["server"]), self.go_keys("marketengine.go", "MEFeedServer"))
        for row in f["short"]:
            self.assertLessEqual(set(row), self.go_keys("marketengine.go", "MEFeedItem"))
        for row in f["glut"]:
            self.assertLessEqual(set(row), self.go_keys("marketengine.go", "MEFeedGlut"))

    def test_metrics_block_keys_are_encoded_by_marketMetricsBlock(self):
        body = go_func_text(self.go["marketmetrics.go"], "marketMetricsBlock")
        encoded = set(re.findall(r'"(\w+)":\s', body))
        m = fixture("market_engine.json")["metrics"]
        names = set(m)
        for block in marketview.METRIC_BLOCKS:
            names |= set(m[block])
            for v in m[block].values():
                if isinstance(v, list) and v:
                    names |= set(v[0])
        self.assertLessEqual(names, encoded)

    def test_kite_file_matches_kiteSaved(self):
        k = self.go_keys("kite.go", "kiteSaved")
        self.assertLessEqual(set(fixture("kite_counters.json")), k)
        self.assertLessEqual(set(fixture("kite_counters_core.json")), k)

    def test_farmer_orders_file_matches_farmers_go(self):
        f = self.go["farmers.go"]
        if f is None:
            self.skipTest("farmers.go is on fix/farmer-orders, not on this branch yet: re-run after that merge")
        doc = fixture("farmer_orders.json")
        self.assertLessEqual(set(doc), go_struct_keys(f, "farmerFile"))
        self.assertLessEqual(set(doc["stats"]), go_struct_keys(f, "farmerFileStats"))
        for row in doc["orders"]:
            self.assertLessEqual(set(row), go_struct_keys(f, "farmerOrderRow"))

    def test_crier_rows_are_what_send_writes(self):
        d = tempfile.mkdtemp()
        try:
            flag = os.path.join(d, "tuning.json")
            with open(flag, "w", encoding="utf-8") as f:
                json.dump({"market_broadcast_on": True}, f)
            crier.send("want", "Poring Coin", "Prontera", live=False, now=1794744000, rng=random.Random(7),
                       log_path=os.path.join(d, "n.jsonl"), flag_path=flag)
            fresh = json.loads(read_text(os.path.join(d, "n.jsonl")).splitlines()[0])
        finally:
            shutil.rmtree(d, ignore_errors=True)
        rows = [json.loads(line) for line in read_text(os.path.join(FIX, "market_notices.jsonl")).splitlines() if line]
        for r in rows:
            self.assertEqual(set(r), set(fresh))
            self.assertIn(r["mode"], ("live", "dry"))

    def test_crier_rules_fixture_is_the_rules_crier_publishes(self):
        self.assertEqual(fixture("crier_rules.json"), crier.rules())


class GoConstants(unittest.TestCase):
    """the typed copies of the item ids in the facts block are the Go constants, and their cited lines still hold them"""

    @classmethod
    def setUpClass(cls):
        cls.facts = {f["id"]: f for f in marketview.parse_facts(read_text(FACTS))["facts"]}

    def check(self, fact_id, go_file, name):
        path = os.path.join(GO, go_file)
        if not os.path.exists(path):
            self.skipTest(go_file + " not on this branch")
        src = read_text(path)
        self.assertEqual(self.facts[fact_id]["value"], go_const(src, name), fact_id + " vs " + name)
        line = int(re.search(r":(\d+)$", self.facts[fact_id]["source"].split(" ")[0]).group(1))
        self.assertIn(name, src.splitlines()[line - 1], fact_id + " cites the wrong line")

    def test_creamy_card_id_is_itemCreamyCard(self):
        self.check("engine.item_creamy_card", "guildstock.go", "itemCreamyCard")
        self.assertEqual(self.facts["creamy.card.id"]["value"], self.facts["engine.item_creamy_card"]["value"])

    def test_creamy_teleport_is_itemCreamy(self):
        self.check("engine.item_creamy", "teleport.go", "itemCreamy")

    def test_trap_is_itemTrap(self):
        self.check("engine.item_trap", "kite.go", "itemTrap")
        self.assertEqual(self.facts["item.trap.id"]["value"], self.facts["engine.item_trap"]["value"])


class MarketUp(Base):
    """(b) the engine answers: every section takes its data from the engine's API or the files it names"""
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
        E = self.view()["sections"]["engine"]
        self.assertEqual((E["status"], E["engine_status"], E["source"]), ("ok", "ok", "/api/market/engine"))
        self.assertEqual(E["runs"], 42)
        self.assertEqual([n["rank"] for n in E["needs"]], [1, 2])
        self.assertEqual(E["needs"][0]["farmers_on"], 3)
        alloc = {a["item_id"]: a for a in E["allocation"]}
        self.assertEqual((alloc["4040"]["count"], alloc["4040"]["maps"]), (3, ["prt_fild02"]))
        self.assertEqual({m["map"]: m["count"] for m in E["by_map"]}, {"prt_fild02": 2, "gef_fild05": 3})
        self.assertEqual(E["caps"][0], {"name": "per_item", "value": 3})
        flags = {f["flag"]: f["state"] for f in E["flags"]}
        self.assertEqual(flags["market_broadcast_on"], "on")            # tuning.json
        self.assertEqual(flags["dynamic_price_on"], "not in tuning.json")
        self.assertEqual(E["feed"]["status"], "ok")
        self.assertIn("meter feed not wired", " ".join(E["feed"]["missing"]))

    def test_metrics_section_is_its_own(self):
        M = self.view()["sections"]["metrics"]
        self.assertEqual(M["source"], "/api/market/engine (metrics)")
        self.assertEqual(sorted(M["blocks"]), sorted(marketview.METRIC_BLOCKS))
        self.assertEqual(M["blocks"]["kite"]["steps"], 120)
        self.assertEqual(M["blocks"]["farmers"]["declined"]["unsafe map"], 12)

    def test_farmers_section(self):
        F = self.view()["sections"]["farmers"]
        self.assertEqual((F["roster"]["count"], F["roster"]["target"], F["roster"]["vendor_adds"], F["roster"]["vendor_present"]),
                         (3, 150, 1, 2))
        self.assertEqual(F["orders"]["count"], 1)
        self.assertEqual(F["orders"]["rows"][0]["maps"], "prt_fild02")
        self.assertAlmostEqual(F["acceptance"]["rate"], 41 / 60, places=6)
        self.assertEqual(F["declines"][0], {"reason": "unsafe map", "n": 12})
        self.assertEqual(F["supplied"][0], {"item": "Poring Coin", "n": 30})
        self.assertEqual(F["deaths"][0], {"account": "lg0124", "n": 1})
        self.assertEqual((F["kite"]["steps"], F["kite"]["deaths"]), (120, 3))
        self.assertEqual(F["kite"]["source"], "/api/economy (kite)")
        self.assertEqual(F["zeny_h"], [])                               # the feed's zeny_per_hour is empty on this sample

    def test_prices_are_not_invented(self):
        P = self.view()["sections"]["prices"]
        self.assertEqual(P["status"], "source not available")
        self.assertEqual(P["rows"], [])

    def test_broadcast_section(self):
        B = self.view()["sections"]["broadcast"]
        self.assertEqual(len(B["notices"]), 3)
        self.assertEqual((B["live_n"], B["dry_n"]), (1, 2))
        self.assertEqual({r["mode"] for r in B["notices"]}, {"live", "dry"})
        self.assertIn({"name": "max_per_day", "value": 4}, B["cadence"])
        self.assertEqual(B["cadence_source"], "tools/system/crier.py rules()")
        self.assertEqual(B["flag"]["state"], "on")
        self.assertEqual(B["notices_source"], "data/market_notices.jsonl")

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

    def test_feed_route_answers_from_the_engine(self):
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

    def test_engine_has_no_file_so_its_section_says_so(self):
        E = self.view()["sections"]["engine"]
        self.assertEqual(E["status"], "source not available")
        self.assertIn("/api/market/engine", " ".join(E["missing"]))
        self.assertEqual(E["needs"], [])

    def test_feed_and_farmers_come_from_the_files(self):
        S = self.view()["sections"]
        self.assertEqual(S["engine"]["feed"]["source"], "data/market_feed.json")
        self.assertEqual(S["engine"]["feed"]["status"], "ok")
        self.assertEqual(S["farmers"]["orders"]["source"], "data/farmer_orders.json")
        self.assertEqual(S["farmers"]["roster"]["source"], "data/farmers.json")
        self.assertEqual((S["farmers"]["kite"]["steps"], S["farmers"]["kite"]["deaths"]), (125, 3))   # core file too
        self.assertIn("data/kite_counters_core.json", S["farmers"]["kite"]["source"])
        self.assertEqual(S["broadcast"]["notices_source"], "data/market_notices.jsonl")

    def test_metrics_say_they_are_engine_only(self):
        M = self.view()["sections"]["metrics"]
        self.assertEqual(M["status"], "source not available")

    def test_stand_in_feed_route_says_files(self):
        code, hdr, body = get(self.url + "/api/market/feed")
        self.assertEqual(code, 200)
        self.assertEqual(hdr.get("X-Dash-Source"), "files")
        self.assertEqual(json.loads(body)["status"], "ok")
        self.assertEqual(get(self.url + "/api/market/engine")[0], 503)   # no file: no stand-in


class MarketNothingOnDisk(Base):
    """(c) the engine is down and no file exists: every section says what is not available, nothing is invented"""
    engine_alive = False
    with_data = False

    def test_sections_say_source_not_available(self):
        code, _, body = get(self.url + "/api/market/view")
        self.assertEqual(code, 200)
        S = json.loads(body)["sections"]
        self.assertEqual(S["engine"]["status"], "source not available")
        self.assertIn("/api/market/engine", " ".join(S["engine"]["missing"]))
        self.assertIn("source not available: data/farmers.json", S["farmers"]["missing"])
        self.assertEqual(S["prices"]["status"], "source not available")
        self.assertEqual(S["broadcast"]["notices"], [])
        self.assertIn("source not available: data/market_notices.jsonl", S["broadcast"]["missing"])
        self.assertEqual(S["metrics"]["status"], "source not available")
        self.assertGreater(len(S["facts"]["rows"]), 0)   # the facts file is in the repo: it does not depend on the engine

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
        self.assertIn("Poring Coin", out["cards"]["mkBroadcast"])
        self.assertIn("market_notices.jsonl", out["cards"]["mkBroadcast"])
        self.assertIn("item_db_etc.yml:3200", out["cards"]["mkFacts"])
        self.assertIn("4,040", out["cards"]["mkFacts"])
        self.assertIn("deaths per step", out["cards"]["mkMetrics"])
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
    """the parsing, the derived values and the engine's own off answer, with no server"""

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

    def test_engine_that_says_off_is_shown_off(self):
        off = {"status": "off", "flag": "market_engine_on", "note": "off", "metrics": {}}
        S = marketview.section_engine(off, None, {"market_engine_on": False}, time.time())
        self.assertEqual(S["status"], "off")
        self.assertEqual({f["flag"]: f["state"] for f in S["flags"]}["market_engine_on"], "off")
        self.assertEqual(S["missing"], [])   # an off engine computes nothing: its missing lists are not errors

    def test_allocation_from_orders_when_there_is_no_distribution(self):
        orders = [{"item_id": 4040, "map": "prt_fild02"}, {"item_id": 4040, "map": "gef_fild05"}]
        rows = marketview.allocation_rows(None, orders, {"4040": "Creamy Card"})
        self.assertEqual(rows, [{"item_id": "4040", "item": "Creamy Card", "count": 2,
                                 "maps": ["gef_fild05", "prt_fild02"]}])

    def test_view_with_no_engine_and_no_files(self):
        d = tempfile.mkdtemp()
        try:
            v = marketview.view(lambda p: None, lambda n: os.path.join(d, n), now=time.time(), tail=None,
                                facts_path=FACTS, rules=lambda: None)
            S = v["sections"]
            self.assertEqual(set(S), {"engine", "metrics", "farmers", "prices", "broadcast", "facts"})
            self.assertEqual(S["prices"]["status"], "source not available")
            self.assertEqual(S["farmers"]["roster"]["count"], None)
            self.assertEqual(S["broadcast"]["flag"]["state"], "not in tuning.json")
            self.assertIn("source not available: tools/system/crier.py rules()", S["broadcast"]["missing"])
        finally:
            shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
