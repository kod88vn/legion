#!/usr/bin/env python3
"""fix_kafra_portals.py - rewrite the Kafra teleport lines of OpenKore's tables/portals.txt to match this server.

OpenKore ships the official kRO/iRO Kafra destination lists; this rAthena runs npc/kafras/kafras.txt with the
destination lists of our functions_kafras.txt (F_KafSet), so most "c r2 c rN" indices were wrong (2026-11-14, travel
QA: Prontera Kafra r4 = "Orc Dungeon" on the server but "alberta" in OpenKore, r5 = Alberta vs "aldebaran", r6/r7 =
Cancel; Izlude Kafra r0 = Geffen, not Prontera; Juno Kafra lists only Al De Baran; Prontera Kafra 29,207 was 35,208).
Ayla aimed for Aldebaran from the Prontera Kafra and landed in Alberta, then walked into the ship to Izlude.

Usage: python3 fix_kafra_portals.py tables/portals.txt [functions_kafras.txt]
Idempotent: removes every Kafra teleport line of the towns below and appends the generated block.
"""
import re
import sys

# Kafra NPCs with the teleport menu (F_Kafra menu_num 0 -> Save, Storage, Teleport = r2), from npc/kafras/kafras.txt
# and npc/re/kafras/kafras.txt (Izlude 128,148).
KAFRAS = {
    "prontera": [(146, 89), (151, 29), (152, 326), (29, 207), (282, 200)],
    "payon": [(181, 104), (175, 226)],
    "geffen": [(120, 62), (203, 123)],
    "morocc": [(156, 97), (160, 258)],
    "alberta": [(113, 60), (28, 229)],
    "aldebaran": [(143, 119)],
    "izlude": [(128, 148)],
    "yuno": [(152, 187), (327, 108), (277, 221)],
    "comodo": [(195, 150)],
    "cmd_fild07": [(136, 134)],
    "umbala": [(87, 160)],
}
# F_KafTele landing cells
LAND = {
    "Al De Baran": ("aldebaran", 168, 112), "Alberta": ("alberta", 117, 56), "Comodo": ("comodo", 209, 143),
    "Comodo Pharos Beacon": ("cmd_fild07", 127, 134), "Geffen": ("geffen", 120, 39), "Izlude": ("izlude", 128, 98),
    "Juno": ("yuno", 158, 125), "Mjolnir Dead Pit": ("mjolnir_02", 99, 351), "Morocc": ("morocc", 156, 46),
    "Orc Dungeon": ("gef_fild10", 52, 326), "Payon": ("payon", 161, 58), "Prontera": ("prontera", 116, 72),
    "Umbala": ("umbala", 100, 154),
}
# fallback copy of F_KafSet (rathena/npc-custom/functions_kafras.txt, 2026-11-14) if the file isn't given
DEFAULT_SETS = {
    "prontera": (["Izlude", "Geffen", "Payon", "Morocc", "Orc Dungeon", "Alberta"], [60, 120, 120, 120, 170, 180]),
    "alberta": (["Payon", "Morocc", "Prontera"], [120, 180, 180]),
    "aldebaran": (["Geffen", "Juno", "Izlude", "Mjolnir Dead Pit"], [120, 120, 180, 170]),
    "comodo": (["Morocc", "Comodo Pharos Beacon", "Umbala"], [180, 120, 180]),
    "cmd_fild07": (["Comodo", "Morocc"], [120, 120]),
    "geffen": (["Prontera", "Al De Baran", "Orc Dungeon", "Mjolnir Dead Pit"], [120, 120, 170, 170]),
    "izlude": (["Geffen", "Payon", "Morocc", "Al De Baran"], [120, 120, 120, 180]),
    "morocc": (["Prontera", "Payon", "Alberta", "Comodo", "Comodo Pharos Beacon"], [120, 120, 180, 180, 120]),
    "umbala": (["Comodo"], [180]),
    "payon": (["Prontera", "Alberta", "Morocc"], [120, 120, 120]),
    "yuno": (["Al De Baran"], [120]),
}


def parse_sets(path):
    txt = open(path, encoding="utf-8", errors="replace").read()
    body = txt[txt.index("function\tscript\tF_KafSet"):]
    sets = {}
    for m in re.finditer(r'\.@map\$ == "([a-z_0-9]+)"\s*\)\s*\{(.*?)\}', body, re.S):
        town, blk = m.group(1), m.group(2)
        d = re.search(r'setarray @wrpD\$\[0\],([^;]*);', blk)
        p = re.search(r'setarray @wrpP\[0\],([^;]*);', blk)
        if d and p:
            sets[town] = ([s.strip().strip('"') for s in d.group(1).split(",")],
                          [int(s) for s in p.group(1).split(",")])
    return sets


def main():
    portals = sys.argv[1]
    sets = parse_sets(sys.argv[2]) if len(sys.argv) > 2 else DEFAULT_SETS
    towns = "|".join(KAFRAS)
    kafra_line = re.compile(r"^(%s) \d+ \d+ [a-z_0-9]+ \d+ \d+ \d+ 1 (c )?c r2 c r\d+\s*$" % towns)
    lines = open(portals, encoding="utf-8", errors="replace").read().splitlines()
    kept, dropped = [], 0
    in_block = False
    for ln in lines:
        if ln.startswith("# Kafra teleports (fix_kafra_portals.py"):
            in_block = True
            continue
        if in_block:
            if ln.startswith("# end Kafra teleports"):
                in_block = False
            continue
        if kafra_line.match(ln):
            dropped += 1
            continue
        kept.append(ln)
    out = ["# Kafra teleports (fix_kafra_portals.py, from the server's F_KafSet + kafras.txt)"]
    for town, npcs in KAFRAS.items():
        dests, prices = sets[town]
        for x, y in npcs:
            for i, (d, price) in enumerate(zip(dests, prices)):
                m, lx, ly = LAND[d]
                out.append("%s %d %d %s %d %d %d 1 c r2 c r%d" % (town, x, y, m, lx, ly, price, i))
    out.append("# end Kafra teleports")
    open(portals, "w", encoding="utf-8").write("\n".join(kept + out) + "\n")
    print("removed %d old Kafra lines, wrote %d" % (dropped, len(out) - 2))


if __name__ == "__main__":
    main()
