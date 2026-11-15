"""Offline test of tools/dev/gen_mapdensity.py: only the npc files the server loads are counted (the conf import tree
from conf/map_athena.conf and npc/re/scripts_main.conf: npc:, delnpc:, import:), and within them only live monster lines."""
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GEN = os.path.join(HERE, "..", "..", "tools", "dev", "gen_mapdensity.py")

FILES = {
    # map_athena.conf -> import conf/import/map_conf.txt -> npc: custom-local (loaded)
    "conf/map_athena.conf": "map_ip: 127.0.0.1\nimport: conf/import/map_conf.txt\n",
    "conf/import/map_conf.txt": (
        "npc: npc/custom-local/legion.txt\n"
        "npc: npc/old.txt\n"            # removed again by scripts_main.conf (delnpc)
        "npc: npc/missing.txt\n"        # no such file: not loaded
        "// npc: npc/commented.txt\n"   # commented out: not loaded
    ),
    "npc/custom-local/legion.txt": "prt_fild08,0,0\tmonster\tPoring\t1002,10,5000\n",
    "npc/old.txt": "old_map,0,0\tmonster\tGone\t1,999,5000\n",
    "npc/commented.txt": "commented_map,0,0\tmonster\tNever\t1,888,5000\n",
    "npc/unused.txt": "unused_map,0,0\tmonster\tNever\t1,777,5000\n",  # never listed anywhere
    # npc/re/scripts_main.conf: a file, a duplicate, an import of another conf, a delnpc, a commented line
    "npc/re/scripts_main.conf": (
        "npc: npc/re/mobs/a.txt\n"
        "import: npc/re/sub.conf\n"
        "delnpc: npc/old.txt\n"
        "npc: npc/re/mobs/a.txt\n"      # listed once
        "// npc: npc/unused.txt\n"
    ),
    "npc/re/sub.conf": "npc: npc/re/mobs/b.txt\n",
    "npc/re/mobs/a.txt": (
        "prt_fild08,0,0\tmonster\tLunatic\t1063,100,5000\n"
        "// pay_dun02,1,1\tmonster\tOld\t1001,500,5000\n"   # commented monster line
        "pay_dun02,20,30,4\tmonster\tSkeleton\t1005,7,5000,5000\n"
        "pay_dun02,0,0\tboss_monster\tMVP\t1100,1,0\n"      # boss spawns are not counted
    ),
    "npc/re/mobs/b.txt": "tra_fild01,0,0\tmonster\tTrader\t1,4,5000\n",
}


class GenMapDensity(unittest.TestCase):
    def test_follows_the_server_import_tree(self):
        with tempfile.TemporaryDirectory() as root:
            for rel, text in FILES.items():
                path = os.path.join(root, *rel.split("/"))
                os.makedirs(os.path.dirname(path), exist_ok=True)
                with open(path, "w", encoding="utf-8") as f:
                    f.write(text)
            out = os.path.join(root, "mapdensity.txt")
            subprocess.run([sys.executable, GEN, root, out, "400"], check=True, capture_output=True)
            with open(out, encoding="utf-8") as f:
                text = f.read()
            lines = [l.rstrip("\n") for l in text.splitlines() if l.strip() and not l.startswith("#")]
            self.assertEqual(lines[0], "rate 400")
            counts = dict(l.split(" ") for l in lines[1:])
            # loaded: custom-local/legion.txt, re/mobs/a.txt, re/mobs/b.txt (3 files)
            self.assertEqual(counts, {"prt_fild08": "110", "pay_dun02": "7", "tra_fild01": "4"})
            self.assertIn("(3 files", text.splitlines()[0])


if __name__ == "__main__":
    unittest.main()
