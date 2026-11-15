"""Realism checks for legion/data/buildgear.json (build audit 2026-11-15, findings F2-F6, F8, F9, F14, F19).

Offline: the item rows of every listed id come from tests/tools/fixtures/buildgear_items.json (copied from the
server's item_db and mob_db, read-only, G23). Each build's class is read from legion/builds.go through planlib.
Rules tested: a listed piece is one the class can wear (Jobs, with "All" meaning every class) at the band's top level;
it fits its slot's location; every slot has a wearable piece; no shield in a band whose best weapon is two-handed;
bards need an instrument, dancers a whip; a card fits its slot and is dropped by a monster at most 20 levels above
the band top; a card's stated stat bonuses are the ones its script gives.
Run: python -m unittest discover -s tests/tools -t tests/tools"""
import json
import os
import re
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "tools", "design"))

import gearalts as ga  # noqa: E402
import planlib as pl  # noqa: E402

GEAR = os.path.join(REPO, "legion", "data", "buildgear.json")
FIXTURE = os.path.join(HERE, "fixtures", "buildgear_items.json")
CARD_SLACK = 20
NON_SHIELD = [s for s in ga.SLOT_ORDER if s != "shield"]
BONUS_KEY = {"STR": "bStr", "AGI": "bAgi", "VIT": "bVit", "INT": "bInt", "DEX": "bDex", "LUK": "bLuk",
             "HIT": "bHit", "FLEE": "bFlee", "ATK": "bBaseAtk", "MaxHP": "bMaxHP"}


def _keys(plan):
    return ga.job_keys_for(pl.SECOND_JOB[plan["cls"]], pl.FIRST_JOB[plan["first"]])


def _wearable(item, keys):
    jobs = item.get("Jobs") or []
    return not jobs or "All" in jobs or any(k in jobs for k in keys)


class GearRealism(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with open(GEAR, encoding="utf-8") as f:
            cls.gear = json.load(f)["builds"]
        with open(FIXTURE, encoding="utf-8") as f:
            cls.items = {int(k): v for k, v in json.load(f)["items"].items()}
        cls.plans = pl.parse_builds()

    def _bands(self):
        for bid, v in self.gear.items():
            keys = _keys(self.plans[bid])
            for b in v["bands"]:
                yield bid, b, keys

    def test_every_listed_id_is_in_the_item_database(self):
        for bid, b, _ in self._bands():
            for s in ga.SLOT_ORDER:
                for iid in b.get(s) or []:
                    self.assertIn(iid, self.items, (bid, b["min"], s, iid))
                    self.assertIn(self.items[iid]["Type"], ("Weapon", "Armor"), (bid, s, iid))
            for c in b.get("cards") or []:
                self.assertEqual(self.items[c["id"]]["Type"], "Card", (bid, c["id"]))

    def test_no_duplicate_piece_in_a_slot(self):
        for bid, b, _ in self._bands():
            for s in ga.SLOT_ORDER:
                lst = b.get(s) or []
                self.assertEqual(len(lst), len(set(lst)), (bid, b["min"], s))

    def test_every_listed_piece_fits_its_slot_location(self):
        for bid, b, _ in self._bands():
            for s in ga.SLOT_ORDER:
                for iid in b.get(s) or []:
                    locs = self.items[iid].get("Locations") or []
                    self.assertTrue(any(k in locs for k in ga.SLOT_LOCS[s]), (bid, b["min"], s, iid, locs))

    def test_every_listed_piece_is_wearable_by_the_class_at_the_band_top(self):
        for bid, b, keys in self._bands():
            for s in ga.SLOT_ORDER:
                for iid in b.get(s) or []:
                    it = self.items[iid]
                    where = (bid, b["min"], b["max"], s, iid, it["Name"])
                    self.assertTrue(_wearable(it, keys), where)
                    self.assertLessEqual(it.get("EquipLevelMin", 0), b["max"], where)

    def test_every_slot_has_a_wearable_piece_in_every_band(self):
        for bid, b, keys in self._bands():
            for s in NON_SHIELD:
                ok = [iid for iid in b.get(s) or []
                      if _wearable(self.items[iid], keys) and self.items[iid].get("EquipLevelMin", 0) <= b["max"]]
                self.assertTrue(ok, (bid, b["min"], b["max"], s, "no piece the class can wear"))

    def test_no_shield_with_a_two_handed_best_weapon(self):
        # F6: a shield cannot be worn with a two-handed weapon; the band's best (last) weapon decides
        for bid, b, _ in self._bands():
            weapons = b.get("weapon") or []
            if weapons and b.get("shield"):
                best = self.items[weapons[-1]]
                self.assertNotIn("Both_Hand", best.get("Locations") or [], (bid, b["min"], best["Name"]))

    def test_bard_has_an_instrument_and_dancer_a_whip(self):
        # F8: the song and dance skills need the instrument or whip in hand, not only a bow
        for bid, sub in (("bard", "Musical"), ("dancer", "Whip")):   # the variants use these tables (CL04 note, F15)
            for b in self.gear[bid]["bands"]:
                subs = [self.items[i].get("SubType") for i in b.get("weapon") or []]
                self.assertIn(sub, subs, (bid, b["min"], b["max"]))

    def test_no_unobtainable_clip_in_the_gear_tables(self):
        # F14: Clip 2607 is not sold by any stock shop and has no dropper: no band lists it
        for bid, b, _ in self._bands():
            for s in ga.SLOT_ORDER:
                self.assertNotIn(2607, b.get(s) or [], (bid, b["min"], s))

    def test_no_unobtainable_clip_in_the_shared_routes(self):
        # F14: the shared route lists in builds.go (armorLight, extrasMelee, ...) hold no Clip 2607
        with open(os.path.join(REPO, "legion", "builds.go"), encoding="utf-8") as f:
            src = f.read()
        for m in re.finditer(r"^	(armor\w+|extras\w+)\s*= \[\]int\{([^}]*)\}", src, re.M):
            ids = [int(x) for x in re.findall(r"\d+", m.group(2))]
            self.assertNotIn(2607, ids, m.group(1))

    def test_cards_fit_their_slot_and_drop_near_the_band(self):
        for bid, b, _ in self._bands():
            for c in b.get("cards") or []:
                it = self.items[c["id"]]
                where = (bid, b["min"], b["max"], c["id"], it["Name"])
                locs = it.get("Locations") or []
                self.assertTrue(any(k in locs for k in ga.SLOT_LOCS[c["slot"]]), where)
                lv = it.get("dropper_min_level")
                self.assertIsNotNone(lv, where)   # every card has a dropper in mob_db
                self.assertLessEqual(lv, b["max"] + CARD_SLACK, where)   # F9

    def test_card_text_matches_the_card_script(self):
        # F19: the numbers a card's "why" states are the bonuses its script gives
        pat = re.compile(r"\b(STR|AGI|VIT|INT|DEX|LUK|HIT|FLEE|ATK|MaxHP)\+(\d+)(?![\d%])")
        for bid, b, _ in self._bands():
            for c in b.get("cards") or []:
                script = self.items[c["id"]].get("Script") or ""
                for stat, num in pat.findall(c.get("why", "")):
                    key = BONUS_KEY[stat]
                    self.assertRegex(script, r"%s,\s*%s\b" % (key, num),
                                     (bid, c["id"], c["why"], "script has no %s %s" % (key, num)))


if __name__ == "__main__":
    unittest.main()
