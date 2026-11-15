"""Offline test of tools/dev/gen_mapdensity.py: monster lines are counted per map, comments and other spawn types are not."""
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GEN = os.path.join(HERE, "..", "..", "tools", "dev", "gen_mapdensity.py")

SAMPLE = (
    "prt_fild08,0,0\tmonster\tPoring\t1002,110,5000\n"
    "prt_fild08,0,0\tmonster\tLunatic\t1063,100,5000\n"
    "// pay_dun02,1,1\tmonster\tOld\t1001,500,5000\n"
    "pay_dun02,20,30,4\tmonster\tSkeleton\t1005,7,5000,5000\n"
    "pay_dun02,0,0\tboss_monster\tMVP\t1100,1,0\n"
    "pay_dun02,0,0\tmapflag\tnoteleport\n"
)


class GenMapDensity(unittest.TestCase):
    def test_counts_monster_lines_only(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = os.path.join(tmp, "mobs")
            os.makedirs(src)
            with open(os.path.join(src, "a.txt"), "w", encoding="utf-8") as f:
                f.write(SAMPLE)
            out = os.path.join(tmp, "mapdensity.txt")
            subprocess.run([sys.executable, GEN, src, out, "400"], check=True, capture_output=True)
            lines = [l.rstrip("\n") for l in open(out, encoding="utf-8") if l.strip() and not l.startswith("#")]
            self.assertEqual(lines[0], "rate 400")
            counts = dict(l.split(" ") for l in lines[1:])
            self.assertEqual(counts, {"prt_fild08": "210", "pay_dun02": "7"})


if __name__ == "__main__":
    unittest.main()
