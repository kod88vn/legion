"""Tests for tools/puppet/progress.py (rule EC43): server progress for the market master. Fixtures only, no server.
Run: python -m unittest discover -s tools/puppet/tests -v"""
import os as _os  # G23: tests never touch the live legion/data folder
import sys as _sys
_sys.path.insert(0, _os.path.join(_os.path.dirname(_os.path.abspath(__file__)), "../../../tests"))
import testenv  # noqa: E402
testenv.isolate()  # LEGION_DATA -> a temp folder before any tool module is imported

import json
import os
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PUP = os.path.dirname(HERE)
sys.path.insert(0, PUP)
import briefing  # noqa: E402
import progress  # noqa: E402

NOW = 1_800_000_000.0

MILESTONES = {"status": "draft", "builds": {"knight": {
    "name": "Two-Hand Sword Knight", "class": "Knight", "first_job": "Swordman", "ranges": [
        {"min": 1, "max": 32, "class_at_end": "Swordman"},
        {"min": 33, "max": 60, "class_at_end": "Knight"},
        {"min": 48, "max": 99, "class_at_end": "Knight"}]},
    "priest": {"name": "Priest", "class": "Priest", "first_job": "Acolyte", "ranges": [
        {"min": 1, "max": 40, "class_at_end": "Acolyte"},
        {"min": 41, "max": 99, "class_at_end": "Priest"}]}}}

GEAR = {"builds": {"knight": {"bands": [
    {"min": 1, "max": 32, "weapon": [1234], "armor": [2301], "cards": [{"id": 4040, "slot": "accessory", "prio": 3}]},
    {"min": 33, "max": 60, "weapon": [1500], "armor": [], "cards": [4043]}]}}}

BOTS = {"bots": [
    {"name": "A", "class": "Swordman", "level": 20, "online": True, "guild": "guild01"},
    {"name": "B", "class": "Knight", "level": 40, "online": True, "guild": "guild01"},
    {"name": "C", "class": "Knight", "level": 55, "online": False, "guild": "guild02"},
    {"name": "D", "class": "Priest", "level": 50, "online": True, "guild": "guild02"},
    {"name": "E", "class": "Mage", "level": 70, "online": True, "guild": ""},
]}
FIT = {"bots": 3, "weapon_fit_pct": 50, "armor_fit_pct": 30,
       "by_build": {"knight": {"bots": 2, "weapon_fit_pct": 50, "armor_fit_pct": 0}}}
METER = {"bots": [
    {"name": "A", "lv": 20, "zph": 1000, "counted": True},
    {"name": "B", "lv": 40, "zph": 3000, "counted": True},
    {"name": "C", "lv": 41, "zph": 9000, "counted": False},   # not counted: left out
    {"name": "D", "lv": 55, "zph": 5000, "counted": True},
]}
ECON = {
    "wants": [{"item": "Teleport card", "item_id": 4040, "count": 4}, {"item": "Oridecon", "item_id": 1234, "count": 3}],
    "offers": [{"item": "Oridecon", "item_id": 1234, "sellers": 1, "qty": 1}],
}
STOCK = [{"guild": "guild01", "needs": {"Elunium": 2, "Oridecon": 5}, "stock": {"Potion": 3}},
         {"guild": "guild02", "needs": {}, "stock": {}}]


class MockApi:
    """endpoints that answer; anything else raises (= endpoint missing), like briefing.Api"""

    def __init__(self, data):
        self.data = data

    def __call__(self, path):
        base = path.split("?")[0]
        if base in self.data:
            return self.data[base]
        raise OSError("404 " + path)


def full_api():
    return briefing.Api(fetch=MockApi({"/api/bots": BOTS, "/api/buildgear/fit": FIT, "/api/meter/bots": METER,
                                       "/api/economy": ECON, "/api/guild-stock": STOCK}))


class TestProgress(unittest.TestCase):
    def full(self):
        return progress.compute(bots=BOTS, milestones=MILESTONES, gear=GEAR, fit=FIT, meter_bots=METER,
                                economy=ECON, stock=STOCK, missing=[], now=NOW)

    def test_no_data_says_so_and_invents_nothing(self):
        p = progress.compute(now=NOW)
        self.assertEqual(p["status"], "no_data")
        self.assertTrue(any("no live data" in n for n in p["notes"]))
        for key in ("server", "levels", "builds", "short_gear", "guilds", "zeny_per_hour"):
            self.assertNotIn(key, p)
        text = progress.compact_json(p)
        self.assertIn("no_data", text)
        self.assertNotIn("zeny_per_hour", text)

    def test_no_bots_no_numbers(self):
        # economy answers but bots do not: partial, and no server or level numbers are made up
        p = progress.compute(milestones=MILESTONES, gear=GEAR, economy=ECON, missing=["/api/bots"], now=NOW)
        self.assertEqual(p["status"], "partial")
        self.assertNotIn("server", p)
        self.assertNotIn("levels", p)
        self.assertIn("/api/bots", p["missing"])

    def test_no_milestone_table_no_band_numbers(self):
        p = progress.compute(bots=BOTS, gear=GEAR, fit=FIT, meter_bots=METER, economy=ECON, stock=STOCK, now=NOW)
        self.assertNotIn("levels", p)
        self.assertNotIn("zeny_per_hour", p)
        self.assertTrue(any("buildmilestones" in n for n in p["notes"]))

    def test_server_and_level_bands(self):
        p = self.full()
        self.assertEqual(p["status"], "ok")
        self.assertEqual(p["server"]["bots"], 5)
        self.assertEqual(p["server"]["online"], 4)
        self.assertEqual(p["server"]["level_max"], 70)
        # breakpoints from the milestone table: 1, 33, 41, 48 -> bands 1-32, 33-40, 41-47, 48-99
        bands = {r["band"]: r["bots"] for r in p["levels"]}
        self.assertEqual(bands, {"1-32": 1, "33-40": 1, "41-47": 0, "48-99": 3})

    def test_milestones_by_class_line_and_gear_fit(self):
        p = self.full()
        knight = next(b for b in p["builds"] if b["build"] == "knight")
        self.assertEqual(knight["bots"], 3)  # Swordman A, Knight B and C (Swordman and Knight are the line)
        reached = {m["range"]: m["reached"] for m in knight["milestones"]}
        self.assertEqual(reached["1-32"], 3)
        self.assertEqual(reached["33-60"], 2)  # B at 40 and C at 55; A is at 20
        self.assertEqual(knight["gear_fit"], {"weapon_pct": 50, "armor_pct": 0})
        priest = next(b for b in p["builds"] if b["build"] == "priest")
        self.assertNotIn("gear_fit", priest)  # the engine's fit has no entry for this build: nothing invented

    def test_zeny_by_band_skips_uncounted(self):
        p = self.full()
        zb = {r["band"]: r for r in p["zeny_per_hour"]}
        self.assertEqual(zb["1-32"], {"band": "1-32", "n": 1, "zph_mean": 1000})
        self.assertEqual(zb["33-40"]["zph_mean"], 3000)  # C (41, uncounted) is left out, not averaged in
        self.assertEqual(zb["41-47"], {"band": "41-47", "n": 0, "zph_mean": None})
        self.assertEqual(zb["48-99"]["zph_mean"], 5000)

    def test_short_and_no_supply_goods(self):
        p = self.full()
        by = {r["id"]: r for r in p["short_gear"]}
        # online knight line median level is 40 (band 33-60): weapon 1500 is in the table, nobody wants or offers it
        self.assertIn(1500, by)
        self.assertEqual(by[1500]["kind"], "none")
        # card 4040 (band 1-32, not in the 33+ band) wanted 4, nothing offered: none
        self.assertEqual(by[4040]["kind"], "none")
        self.assertEqual(by[4040]["demand"], 4)
        # Oridecon 1234: wanted 3 (wants) + 5 (guild01 needs by name), one offered
        self.assertEqual(by[1234]["kind"], "short")
        self.assertEqual(by[1234]["demand"], 8)
        self.assertEqual(by[1234]["supply"], 1)
        self.assertEqual(by[1234]["item"], "Oridecon")

    def test_guild_needs_and_members(self):
        p = self.full()
        g1 = next(g for g in p["guilds"] if g["guild"] == "guild01")
        self.assertEqual(g1["members"], 2)
        self.assertEqual(g1["online"], 2)
        self.assertEqual(g1["mean_lv"], 30)
        self.assertEqual(g1["needs"][0], {"item": "Oridecon", "n": 5})
        g2 = next(g for g in p["guilds"] if g["guild"] == "guild02")
        self.assertEqual(g2["needs"], [])

    def test_compact_json_keeps_valid_json_under_limit(self):
        many = {"builds": {f"b{i}": {"bands": [{"min": 1, "max": 99, "weapon": [1000 + i]}]} for i in range(200)}}
        bots = {"bots": [{"name": f"n{i}", "class": "Knight", "level": i % 99 + 1, "online": True, "guild": "guild01"}
                         for i in range(400)]}
        ms = {"builds": {"knight": {"class": "Knight", "first_job": "Swordman", "ranges": [{"min": 1, "max": 99}]}}}
        p = progress.compute(bots=bots, milestones=ms, gear=many, now=NOW)
        text = progress.compact_json(p, limit=1500)
        self.assertLessEqual(len(text), 1500)
        json.loads(text)  # still valid JSON

    def test_collect_reads_static_tables_from_data_dir(self):
        with tempfile.TemporaryDirectory() as d:
            json.dump(MILESTONES, open(os.path.join(d, "buildmilestones.json"), "w"))
            json.dump(GEAR, open(os.path.join(d, "buildgear.json"), "w"))
            p = progress.collect(full_api(), data_dir=d, now=NOW)
        self.assertEqual(p["status"], "ok")
        self.assertIn("levels", p)
        self.assertEqual(p["missing"], [])

    def test_collect_with_api_down_is_no_data(self):
        with tempfile.TemporaryDirectory() as d:
            p = progress.collect(briefing.Api(fetch=MockApi({})), data_dir=d, now=NOW)
        self.assertEqual(p["status"], "no_data")
        self.assertIn("/api/bots", p["missing"])
        self.assertNotIn("server", p)

    def test_market_briefing_carries_progress_block(self):
        with tempfile.TemporaryDirectory() as d, tempfile.TemporaryDirectory() as nd:
            json.dump(MILESTONES, open(os.path.join(d, "buildmilestones.json"), "w"))
            json.dump(GEAR, open(os.path.join(d, "buildgear.json"), "w"))
            old = os.environ.get("LEGION_DATA")
            os.environ["LEGION_DATA"] = d  # the static tables come from here (G23: a temp folder)
            try:
                text, _ = briefing.assemble("market", full_api(), nd)
            finally:
                if old is None:
                    os.environ.pop("LEGION_DATA", None)
                else:
                    os.environ["LEGION_DATA"] = old
        self.assertIn("Server progress and build information", text)
        self.assertIn('"status":"ok"', text)
        self.assertIn("Oridecon", text)

    def test_market_briefing_without_data_says_no_data(self):
        with tempfile.TemporaryDirectory() as nd:
            text, _ = briefing.assemble("market", briefing.Api(fetch=MockApi({})), nd)
        self.assertIn('"status":"no_data"', text)


class TestHelpers(unittest.TestCase):
    def test_band_of(self):
        bands = progress.level_bands(progress.milestone_builds(MILESTONES))
        self.assertEqual([b[0] for b in bands], ["1-32", "33-40", "41-47", "48-99"])
        self.assertEqual(progress.level_bands({}), [])
        self.assertEqual(progress.band_of(bands, 1), "1-32")
        self.assertEqual(progress.band_of(bands, 40), "33-40")
        self.assertEqual(progress.band_of(bands, 99), "48-99")
        self.assertIsNone(progress.band_of(bands, None))
        self.assertIsNone(progress.band_of([], 10))

    def test_unknown_fields_are_ignored(self):
        # the alternatives and card-plan fields of the other fixer are not in this shape yet: they must not break reads
        ms = json.loads(json.dumps(MILESTONES))
        ms["builds"]["knight"]["ranges"][0]["alternatives"] = {"weapon": [9999]}
        ms["builds"]["knight"]["card_plan"] = [{"card": 4040, "refine": 7}]
        p = progress.compute(bots=BOTS, milestones=ms, gear=GEAR, economy=ECON,
                             missing=["/api/buildgear/fit", "/api/meter/bots", "/api/guild-stock"], now=NOW)
        self.assertEqual(p["status"], "partial")  # no fit, meter and stock sources here: named as not answered
        self.assertIn("levels", p)

    def test_load_static_missing_file(self):
        with tempfile.TemporaryDirectory() as d:
            self.assertIsNone(progress.load_static("nope.json", d))


if __name__ == "__main__":
    unittest.main()
