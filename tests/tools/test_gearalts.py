"""Tests for the gear alternatives, refine plan and card plan (EC40-EC43): tools/design/gearalts.py and the data it
writes into legion/data/buildmilestones.json (and the export in tools/dashboard/builds.json).

No docker, no live data: the rule tests build a small catalog in memory from values copied from the server's
refine.yml; the shape tests read the committed files. Run: python -m unittest discover -s tests/tools -t tests/tools"""
import json
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "design"))

import gearalts as ga  # noqa: E402

MILESTONES = os.path.join(REPO, "legion", "data", "buildmilestones.json")
EXPORT = os.path.join(REPO, "tools", "dashboard", "builds.json")


def _load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def _chance(typ, rate, price, material, brk=0, down=0):
    return {"type": typ, "rate": rate, "price": price, "material": material, "break": brk}


def _mini_catalog():
    """refine rows copied from db/re/refine.yml (Weapon grade 1, levels 1-8; Armor grade 1, levels 1-7), with the
    NPC shop price of Phracon (200) and no shop for HD_Oridecon / HD_Elunium"""
    cat = ga.Catalog.__new__(ga.Catalog)
    weapon = {}   # refine level -> the chances at that level (the Catalog's shape: group -> grade -> level -> chances)
    for lv in range(1, 8):
        weapon[lv] = [_chance("Normal", 10000, 50, "Phracon"), _chance("HD", 10000, 20000, "HD_Oridecon")]
    weapon[8] = [_chance("Normal", 6000, 50, "Phracon", 10000), _chance("HD", 6000, 20000, "HD_Oridecon", 0, 1)]
    armor = {}
    for lv in range(1, 5):
        armor[lv] = [_chance("Normal", 10000, 2000, "Elunium"), _chance("HD", 10000, 20000, "HD_Elunium")]
    armor[5] = [_chance("Normal", 6000, 2000, "Elunium", 10000), _chance("HD", 6000, 20000, "HD_Elunium", 0, 1)]
    cat.refine = {"Weapon": {1: weapon}, "Armor": {1: armor}}
    phracon = {"Id": 1010, "AegisName": "Phracon", "Name": "Phracon", "Type": "Etc", "Buy": 200}
    cat.items = {1010: phracon}
    cat.by_aegis = {"Phracon": phracon, "HD_Oridecon": {"Id": 984, "AegisName": "HD_Oridecon", "Buy": 0},
                    "HD_Elunium": {"Id": 985, "AegisName": "HD_Elunium", "Buy": 0}}
    cat.shops = {1010: [("prontera", "Ore Dealer", 200)]}
    cat.mobs = {}
    cat.drops = {}
    cat.spawns = {}
    cat.rates = {"item_rate_card": 500, "item_rate_card_mvp": 500}
    return cat


def _mob(mid, name, mvp=False):
    return {"Id": mid, "Name": name, "Level": 30, "Modes": {"Mvp": mvp} if mvp else {}}


class RefineRuleTests(unittest.TestCase):
    def test_target_follows_the_plus_seven_eight_rule(self):
        weapon = {"Type": "Weapon", "Refineable": True}
        for grade, want in ((1, 7), (2, 8), (3, 7), (4, 7), (5, 8)):
            self.assertEqual(ga.refine_target(dict(weapon, WeaponLevel=grade)), want, "weapon grade %d" % grade)
        self.assertEqual(ga.refine_target({"Type": "Armor", "ArmorLevel": 1, "Refineable": True}), 7)
        self.assertEqual(ga.refine_target({"Type": "Armor", "ArmorLevel": 2, "Refineable": True}), 7)
        self.assertIsNone(ga.refine_target({"Type": "Armor", "Refineable": False}))

    def test_a_breaking_material_is_replaced_by_a_safe_one(self):
        # Normal Phracon breaks the weapon at +8 (BreakingRate 10000); HD_Oridecon does not, so +8 takes HD
        cat = _mini_catalog()
        item = {"Id": 1702, "Type": "Weapon", "WeaponLevel": 1, "Refineable": True}
        plan = ga.refine_plan(cat, item, 8)
        self.assertEqual(plan["steps"][6]["material"], "Phracon")
        self.assertEqual(plan["steps"][7]["material"], "HD_Oridecon")
        self.assertEqual(plan["break_risk_pct"], 0.0)
        # +1..+7 at 250z each (50 refine zeny + Phracon 200), +8 at 20,000z over a 60 % chance
        self.assertEqual(plan["cost_z"], 1750 + 33333)
        self.assertIn("HD_Oridecon", plan["unpriced"])   # not sold by an NPC shop: its price is left out

    def test_a_piece_that_only_breaks_reports_the_risk(self):
        cat = _mini_catalog()
        # armor +5: the only option that does not break is HD_Elunium, so the plan takes it; a plan with a breaking
        # level only would report risk: check the arithmetic of break_risk on a catalog with no safe material
        cat.refine["Armor"][1][5] = [_chance("Normal", 6000, 2000, "Elunium", 10000)]
        item = {"Id": 2306, "Type": "Armor", "ArmorLevel": 1, "Refineable": True}
        plan = ga.refine_plan(cat, item, 5)
        self.assertEqual(plan["break_risk_pct"], 40.0)   # one breaking level at a 60 % chance: 1 - 0.6

    def test_cumulative_costs_match_the_plan_total(self):
        cat = _mini_catalog()
        item = {"Id": 1702, "Type": "Weapon", "WeaponLevel": 1, "Refineable": True}
        plan = ga.refine_plan(cat, item, 8)
        cum = ga.cumulative_costs(plan)
        self.assertEqual(cum[0], 0)
        self.assertEqual(cum[-1], plan["cost_z"])
        self.assertEqual(len(cum), 9)


class CardTierTests(unittest.TestCase):
    def test_tiers_from_the_server_rates(self):
        cat = _mini_catalog()
        cat.mobs = {1001: _mob(1001, "A"), 1002: _mob(1002, "B"), 1003: _mob(1003, "C"), 1004: _mob(1004, "D", True)}
        # 3 non-MVP monsters at 100/10000 x 5 (card multiplier) = 5 %: popular
        cat.drops["Popular_Card"] = [(1001, 100, False), (1002, 100, False), (1003, 100, False)]
        # one monster at 100 (5 %): not 3 of them, so uncommon
        cat.drops["Uncommon_Card"] = [(1001, 100, False)]
        # 1/10000 x 5 = 0.05 %: rare
        cat.drops["Rare_Card"] = [(1002, 1, False)]
        # an MVP-only drop does not count for the tier: rare
        cat.drops["Boss_Card"] = [(1004, 5000, True)]
        tiers = {name: cat.card_tier({"Id": 9000 + i, "AegisName": name, "Type": "Card"})
                 for i, name in enumerate(["Popular_Card", "Uncommon_Card", "Rare_Card", "Boss_Card"])}
        self.assertEqual(tiers, {"Popular_Card": "popular", "Uncommon_Card": "uncommon", "Rare_Card": "rare",
                                 "Boss_Card": "rare"})


class ShapeTests(unittest.TestCase):
    """the committed data: every slot of every band has a best piece, alternatives where the item database has one,
    and refine targets and card rules that the module states"""

    @classmethod
    def setUpClass(cls):
        cls.ms = _load(MILESTONES)["builds"]
        cls.ex = _load(EXPORT)

    def test_every_build_has_gear_in_every_band(self):
        self.assertEqual(len(self.ms), 37)
        for bid, b in self.ms.items():
            self.assertTrue(b["ranges"], bid)
            for r in b["ranges"]:
                self.assertEqual(sorted(r["gear"]), sorted(ga.SLOT_ORDER), (bid, r["range"]))
                self.assertIn("cards", r, (bid, r["range"]))

    def test_every_slot_has_a_best_piece_and_alternatives_where_the_database_has_some(self):
        for bid, b in self.ms.items():
            for r in b["ranges"]:
                for slot, g in r["gear"].items():
                    where = (bid, r["range"], slot)
                    self.assertIsNotNone(g["best"], where)
                    self.assertEqual(g["best"]["tier"], "best", where)
                    if not g["alternatives"]:
                        # no other piece in the database for this slot and class at this level: the note says so
                        self.assertIn("no other piece", g["note"], where)
                    self.assertLessEqual(len(g["alternatives"]), ga.MAX_ALTS, where)
                    for a in g["alternatives"]:
                        self.assertIn(a["tier"], ("affordable", "fallback"), where)
                        self.assertNotEqual(a["id"], g["best"]["id"], where)

    def test_every_piece_is_a_real_item_with_a_source_or_a_reason(self):
        for bid, b in self.ms.items():
            for r in b["ranges"]:
                for slot, g in r["gear"].items():
                    for p in [g["best"]] + g["alternatives"]:
                        where = (bid, r["range"], slot, p["id"])
                        self.assertFalse(p["name"].startswith("#"), where)
                        self.assertIn(p["source"]["how"], ("shop", "drop", "none"), where)
                        if p["source"]["how"] == "none":
                            self.assertIn("quest, craft or event", p["source"]["note"], where)
                        if p["source"]["how"] == "shop":
                            self.assertIsNotNone(p["source"]["shop"], where)
                            self.assertEqual(p["price_z"], p["source"]["shop"]["price_z"], where)

    def test_refine_targets_within_the_plus_seven_eight_rule(self):
        for bid, b in self.ms.items():
            for r in b["ranges"]:
                for slot, g in r["gear"].items():
                    for p in [g["best"]] + g["alternatives"]:
                        rf = p.get("refine")
                        if rf is None:
                            continue
                        where = (bid, r["range"], slot, p["id"])
                        self.assertIn(rf["target"], (7, 8), where)
                        if p["kind"] == "weapon":
                            self.assertEqual(rf["target"], ga.REFINE_TARGET_WEAPON[p["grade"]], where)
                        else:
                            self.assertEqual(rf["target"], ga.REFINE_TARGET_OTHER, where)
                        self.assertGreaterEqual(rf["cost_z"], 0, where)
                        self.assertGreaterEqual(rf["break_risk_pct"], 0.0, where)

    def test_card_plan_follows_the_one_time_bound_rules(self):
        for bid, b in self.ms.items():
            for r in b["ranges"]:
                band_max = r["max"]
                budget = ga.REFINE_LV_BUDGET * band_max
                for c in r["cards"]:
                    where = (bid, r["range"], c["id"])
                    self.assertIn(c["tier"], ("popular", "uncommon", "rare"), where)
                    self.assertIn(c["status"], ("use_now", "use_at_target", "fallback"), where)
                    self.assertIn(c["slot"], ga.SLOT_ORDER, where)
                    if c["refine_target"] is not None:
                        self.assertLessEqual(c["insert_at"], c["refine_target"], where)
                        self.assertGreaterEqual(c["insert_at"], 0, where)
                    if c["tier"] == "rare" and c["status"] == "use_at_target":
                        self.assertGreaterEqual(band_max, 60, where)   # rare: end-game bands only
                        if c["refine_target"] is not None:   # a non-refineable slot goes in at once
                            self.assertEqual(c["insert_at"], c["refine_target"], where)
                    if c["status"] == "fallback" and c["fallback"] is not None:
                        self.assertEqual(set(c["fallback"]), {"id", "name"}, where)
                    if c["refine_target"] is not None and c["insert_at"] < c["refine_target"]:
                        # a card goes in at the highest refine the band pays for: the full refine is over budget
                        self.assertGreater(c["refine_cost_z"], budget, where)
                    self.assertIn("rule", c, where)

    def test_export_carries_the_gear_plan_of_every_band(self):
        by = {b["id"]: b for b in self.ex["builds"]}
        self.assertEqual(set(by), set(self.ms))
        for bid, b in self.ms.items():
            rows = by[bid]["milestones"]
            self.assertEqual([x["range"] for x in rows], [r["range"] for r in b["ranges"]], bid)
            for row, r in zip(rows, b["ranges"]):
                self.assertEqual(row["gear_plan"], r["gear"], (bid, row["range"]))
                self.assertEqual(row["card_plan"], r["cards"], (bid, row["range"]))
            self.assertEqual(by[bid]["end"]["gear"]["plan_range"], b["ranges"][-1]["range"], bid)


if __name__ == "__main__":
    unittest.main()
