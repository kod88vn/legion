"""Tests for the engine's milestone gear rows in buildexport.py.
Run: python -m unittest discover -s tests/dashboard -v   (from the repo root)"""
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "..", "tools", "dashboard"))

import buildexport  # noqa: E402

ITEMS = {2314: "Chain Mail", 2325: "Mantle", 4006: "Poring Card", 4020: "Hydra Card"}

# an engine file (legion/data/buildmilestones_gear.json) as the engine writes it: cards carry card_entries
ENGINE = {"builds": {"swordman_bs": {"ranges": [
    {"min": 41, "max": 60,
     "slots": {"weapon": [1162], "armor": [2314], "shield": [], "headgear": [2325], "garment": [], "shoes": [],
               "accessory": []},
     "cards": [4006, 4020],
     "card_entries": [{"id": 4006, "slot": "weapon", "prio": 7, "why": "ATK for the build"},
                      {"id": 4020, "slot": "armor", "prio": 3, "why": "DEF"}],
     "milestone": {"weapon": 1162, "armor": 2314}, "weapon_atk": 115, "armor_def": 55, "cards_required": 1,
     "magic": False},
    {"min": 1, "max": 20, "slots": {}, "cards": [], "card_entries": [], "milestone": {"weapon": 0, "armor": 0},
     "weapon_atk": 0, "armor_def": 0, "cards_required": 0, "magic": False},
]}}}


class EngineCardRows(unittest.TestCase):
    def test_cards_resolve_with_slot_prio_and_why(self):
        rows = buildexport.engine_gear_rows(ENGINE["builds"]["swordman_bs"], ITEMS)
        band = rows[0]
        self.assertEqual(band["min"], 41)
        self.assertEqual(band["cards"], [
            {"id": 4006, "name": "Poring Card", "slot": "weapon", "prio": 7, "why": "ATK for the build"},
            {"id": 4020, "name": "Hydra Card", "slot": "armor", "prio": 3, "why": "DEF"},
        ])

    def test_no_card_row_is_lost(self):
        # a card id with no entry keeps its id and name (slot, prio and why empty), it is not dropped
        eng = {"ranges": [{"min": 41, "max": 60, "cards": [4006, 9999], "card_entries": [{"id": 4006, "slot": "weapon",
                                                                                          "prio": 1, "why": "x"}]}]}
        cards = buildexport.engine_gear_rows(eng, ITEMS)[0]["cards"]
        self.assertEqual(len(cards), 2)
        self.assertEqual(cards[1]["id"], 9999)
        self.assertIsNone(cards[1]["slot"])
        self.assertIn("no name", cards[1]["name"])

    def test_engine_file_without_cards_still_has_rows(self):
        rows = buildexport.engine_gear_rows(ENGINE["builds"]["swordman_bs"], ITEMS)
        self.assertEqual(rows[1]["cards"], [])
        self.assertEqual(rows[1]["min"], 1)

    def test_slots_keep_names_and_drop_empty_slots(self):
        band = buildexport.engine_gear_rows(ENGINE["builds"]["swordman_bs"], ITEMS)[0]
        self.assertEqual(band["slots"]["armor"], ["Chain Mail"])
        self.assertEqual(band["slots"]["headgear"], ["Mantle"])
        self.assertNotIn("shield", band["slots"])

    def test_card_ids_without_entries_come_from_cards_only(self):
        eng = {"ranges": [{"min": 1, "max": 20, "cards": [4006]}]}
        self.assertEqual(buildexport.engine_gear_rows(eng, ITEMS)[0]["cards"][0]["slot"], None)


if __name__ == "__main__":
    unittest.main()
