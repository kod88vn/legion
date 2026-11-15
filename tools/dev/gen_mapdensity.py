#!/usr/bin/env python3
"""Build openkore-ayla/control/mapdensity.txt from the rathena NPC files the server loads (read-only input).

The server loads its npc files in two steps (rathena src/map/map.cpp, renewal build: src/config/renewal.hpp):
  1. conf/map_athena.conf (map_config_read): npc: adds a source file, delnpc: removes it, import: reads another conf.
  2. npc/re/scripts_main.conf (map_reloadnpc_sub): the same three directives, so the renewal scripts and mob files.
This script follows those directives from the same two roots, in the same order, and counts only the files that
still stand at the end (existing files, not removed by delnpc, not commented out). Each `monster` line in those files
spawns `amount` monsters (the field after the mob id). The table keeps the STOCK sum per map; the plugin multiplies by
mob_count_rate / 100 at load (the rate on the `rate` line, 400 on this server).

usage: python tools/dev/gen_mapdensity.py <rathena dir> [out file] [rate]
  <rathena dir>  a rathena tree with conf/ and npc/ (read only). Local copy: tools/dev/rathena-src/ (git-ignored),
                 made from the running ro-rathena container:
                   docker cp ro-rathena:/rathena/conf  tools/dev/rathena-src/conf
                   docker cp ro-rathena:/rathena/npc   tools/dev/rathena-src/npc
                 (docker cp needs the destination given without Git Bash path conversion: MSYS_NO_PATHCONV=1)
"""
import os
import re
import sys
from collections import defaultdict

LINE = re.compile(r"^([a-z0-9_]+),-?\d+,-?\d+[^\t]*\tmonster\t[^\t]*\t\d+,(\d+)")
MAP_CONF = "conf/map_athena.conf"
RENEWAL_MAIN = "npc/re/scripts_main.conf"  # renewal build (the image's PACKETVER and no PRERE define)
MAX_DEPTH = 20  # guards against an import loop


def directives(path):
    """(key, value) pairs of a rathena conf file, parsed the way map.cpp does: a line starting with // is skipped, the
    text after the first // is dropped, the key is everything before the first colon, the value runs up to a tab or
    line end and loses its trailing spaces. Empty keys and values are skipped."""
    with open(path, encoding="utf-8", errors="replace") as f:
        for raw in f:
            if raw.startswith("//"):
                continue
            key, sep, rest = raw.split("//", 1)[0].partition(":")
            if not sep or not key:
                continue
            value = re.split(r"[\t\r\n]", rest.lstrip(" \t"), 1)[0].rstrip(" ")
            if value:
                yield key.lower(), value


def read_conf(root, conf, srcs, depth=0):
    """Apply one conf file to the source list srcs, as map_config_read / map_reloadnpc_sub do."""
    path = os.path.join(root, conf)
    if depth > MAX_DEPTH or not os.path.isfile(path):
        return
    for key, value in directives(path):
        if key == "npc":
            # npc_addsrcfile: the file must exist, and a file is listed once
            if os.path.isfile(os.path.join(root, value)) and value not in srcs:
                srcs.append(value)
        elif key == "delnpc":
            # npc_delsrcfile: exact path; "all" clears the list
            if value.lower() == "all":
                del srcs[:]
            elif value in srcs:
                srcs.remove(value)
        elif key == "import":
            read_conf(root, value, srcs, depth + 1)


def loaded_files(root):
    """The npc source files the server loads, in its order: map_athena.conf first, then the renewal scripts."""
    srcs = []
    read_conf(root, MAP_CONF, srcs)
    read_conf(root, RENEWAL_MAIN, srcs)
    return srcs


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    root = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(os.path.dirname(__file__), "..", "..",
                                                            "openkore-ayla", "control", "mapdensity.txt")
    rate = sys.argv[3] if len(sys.argv) > 3 else "400"
    stock = defaultdict(int)
    srcs = loaded_files(root)
    for rel in srcs:
        with open(os.path.join(root, rel), encoding="utf-8", errors="replace") as f:
            for line in f:
                if line.lstrip().startswith("//") or "\tmonster\t" not in line:
                    continue
                m = LINE.match(line)
                if m:
                    stock[m.group(1)] += int(m.group(2))
    with open(out, "w", encoding="utf-8", newline="\n") as f:
        f.write("# Monster spawns per map from the rathena npc files the server loads (%d files: conf/map_athena.conf "
                "and npc/re/scripts_main.conf imports).\n" % len(srcs))
        f.write("# Stock counts; the plugin scales them by rate/100 (mob_count_rate of this server).\n")
        f.write("rate %s\n" % rate)
        for m in sorted(stock):
            f.write("%s %d\n" % (m, stock[m]))
    print("wrote %s: %d maps from %d loaded files, rate %s" % (os.path.abspath(out), len(stock), len(srcs), rate))


if __name__ == "__main__":
    main()
