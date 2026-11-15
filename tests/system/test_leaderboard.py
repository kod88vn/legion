"""Offline tests of tools/system/leaderboard.py (fake DB rows; never touches ro-db) and league.py's n=0 warning."""
import io
import os
import sys
import unittest
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "tools", "system"))
import leaderboard as LB  # noqa: E402


def row(cid, name, lv, exp, party=0, guild="", master=0, userid="lg1", online=1):
    return {"char_id": str(cid), "account_id": "1", "name": name, "class": "7", "base_level": str(lv), "job_level": "40",
            "base_exp": str(exp), "party_id": str(party), "guild_id": "1", "online": str(online), "userid": userid,
            "guild": guild, "guild_master": str(master)}


ROWS = [
    row(1, "Top", 90, 500, guild="A", master=1),
    row(2, "TieA", 85, 100),
    row(3, "TieB", 85, 100),
    row(150010, "Ayla", 83, 2000, party=2, guild="Starveil", master=150010, userid="player"),
    row(5, "Zen", 81, 10, party=2, guild="Starveil", master=150010),
    row(6, "Wanderer", 99, 0, userid="wander"),
    row(7, "Trailmark", 61, 0, userid="qatest"),
    row(8, "Admin", 13, 0, userid="player"),
    row(9, "Low", 10, 0, party=3),
]


class LeaderboardTest(unittest.TestCase):
    def test_ranking_and_ayla(self):
        b = LB.board(ROWS)
        names = [c["name"] for c in b["ranked"]]
        self.assertNotIn("Wanderer", names)  # staff left out
        self.assertNotIn("Trailmark", names)
        self.assertNotIn("Admin", names)
        self.assertEqual(b["total"], 6)
        r = {c["name"]: c["rank"] for c in b["ranked"]}
        self.assertEqual((r["Top"], r["TieA"], r["TieB"], r["Ayla"], r["Zen"]), (1, 2, 2, 4, 5))  # ties share a rank
        self.assertEqual(b["ayla"]["rank"], 4)
        self.assertEqual([c["name"] for c in b["party"]], ["Zen"])
        self.assertEqual([c["name"] for c in b["leaders"]], ["Top", "Ayla"])
        self.assertEqual(LB.brief(b), "rank 4/6 lv83 | party Zen 5")

    def test_staff_flag_and_main(self):
        self.assertEqual(LB.board(ROWS, staff=True)["ranked"][0]["name"], "Wanderer")
        buf = io.StringIO()
        with redirect_stdout(buf):
            self.assertEqual(LB.main(["--top", "3"], rows=ROWS), 0)
        out = buf.getvalue()
        self.assertIn("Ayla:", out)
        self.assertIn("Guild leaders:", out)

    def test_only_select(self):
        with self.assertRaises(ValueError):
            LB.db_rows("UPDATE `char` SET base_level=99")
        with self.assertRaises(ValueError):
            LB.db_rows("SELECT 1; DELETE FROM `char`")

    def test_no_ayla(self):
        b = LB.board([row(1, "Solo", 50, 1)])
        self.assertIsNone(b["ayla"])
        self.assertIn("not found", LB.brief(b))


class LeagueEmptyWindowTest(unittest.TestCase):
    def test_n0_warning(self):
        import league
        meter = {"server": {"bots": 0, "kpm": 0, "zph": 0}, "cohorts": [{"key": "x|1-20|solo", "n": 0}]}
        self.assertIn("no bot counted", league.render(meter, None, "t"))
        meter["server"]["bots"] = 5
        self.assertNotIn("no bot counted", league.render(meter, None, "t"))

    def test_ayla_row_from_engine(self):
        import league
        neo = {"window": "1h", "ayla": {"windows": {"1h": {"kpm": 9.5, "par_kpm": 2.0, "zph_purse": 100}}},
               "party": [{"name": "Ayla", "instance": "openkore", "kpm": 9.5, "par_kpm": 2.0, "zph": 100, "par_zph": 50}]}
        text = "\n".join(league.neo_section(neo, []))
        self.assertIn("party: Ayla", text)
        self.assertNotIn("Ayla (purse", text)  # no duplicate header row when her own row is there


if __name__ == "__main__":
    unittest.main()
