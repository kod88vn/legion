#!/usr/bin/env python3
"""Build openkore-ayla/control/mapdensity.txt from rathena's NPC spawn scripts (read-only input).

Each `monster` line in npc/*/mobs/*.txt spawns `amount` monsters (the field after the mob id). The table keeps the
STOCK sum per map; the plugin multiplies by mob_count_rate / 100 at load (the rate on the `rate` line, 400 on this server).

usage: python tools/dev/gen_mapdensity.py <rathena npc dir> [out file] [rate]
  <rathena npc dir>  the copy of rathena's npc/ tree (e.g. npc/re/mobs and npc/mobs), read only
"""
import os
import re
import sys
from collections import defaultdict

LINE = re.compile(r"^([a-z0-9_]+),-?\d+,-?\d+[^\t]*\tmonster\t[^\t]*\t\d+,(\d+)")


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    src = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(os.path.dirname(__file__), "..", "..",
                                                            "openkore-ayla", "control", "mapdensity.txt")
    rate = sys.argv[3] if len(sys.argv) > 3 else "400"
    stock = defaultdict(int)
    files = 0
    for root, _, names in os.walk(src):
        for n in names:
            if not n.endswith(".txt"):
                continue
            files += 1
            with open(os.path.join(root, n), encoding="utf-8", errors="replace") as f:
                for line in f:
                    if line.lstrip().startswith("//") or "\tmonster\t" not in line:
                        continue
                    m = LINE.match(line)
                    if m:
                        stock[m.group(1)] += int(m.group(2))
    with open(out, "w", encoding="utf-8", newline="\n") as f:
        f.write("# Monster spawns per map from rathena npc/*/mobs (%d files, sum of the monster lines' amounts).\n" % files)
        f.write("# Stock counts; the plugin scales them by rate/100 (mob_count_rate of this server).\n")
        f.write("rate %s\n" % rate)
        for m in sorted(stock):
            f.write("%s %d\n" % (m, stock[m]))
    print("wrote %s: %d maps, rate %s" % (os.path.abspath(out), len(stock), rate))


if __name__ == "__main__":
    main()
