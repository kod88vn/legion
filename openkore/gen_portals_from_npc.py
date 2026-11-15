#!/usr/bin/env python3
"""gen_portals_from_npc.py - compare OpenKore's tables/portals.txt with the warps this rAthena really loads, and write
openkore/portals-srv.txt (lines to drop / lines to add) for fix_srv_portals.py.

2026-11-15: Ayla routed gef_fild10 -> gef_fild14 -> gef_fild13: those links are commented out in Renewal
(npc/re/warps/fields/geffen_fild.txt), so she walked to warp cells that do nothing. Also 1,600+ server warps were
missing from portals.txt (indoor doors, dungeon floors) and ~270 had the wrong landing cell.

Usage (python3 -I): gen_portals_from_npc.py <rathena root with npc/> <portals.txt after fix_re_portals.sh>
                    <db/map_cache.dat> <db/re/map_cache.dat> <out portals-srv.txt>
Loaded npc files: npc/re/scripts_main.conf + npc/scripts_custom.conf (npc:/import:/delete:/delnpc:), plus custom-local.
/* block comments */ are blanked first (rAthena skips them: e.g. the commented-out te_alde_gld gate in
npc/re/warps/guildcastles.txt), keeping line numbers.
The Go engine (legion/srvwarps.go) implements the same rules for its warp graph; keep the two in step.
Warps: `warp`/`warp2` lines, duplicate()s of them, and unconditional OnTouch WARPNPC scripts with a single warp.
Rules (plain 6-field portals.txt lines only; NPC talk lines are never touched):
  drop  - a plain line whose source cell has NO server NPC/warp of any kind within 3 cells (a removed warp)
  fix   - a plain line whose server warp (same maps, source within 3 cells) lands > 4 cells away: replaced
  add   - a server warp with no portals.txt line (same maps, source within 3 cells); instance maps (@) skipped
"""
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen_fields_from_mapcache import load  # noqa: E402

HDR = re.compile(r'^([\w@-]+),(\d+),(\d+),\d+\t(warp2?|script|duplicate\(([^)]+)\)|shop|cashshop|itemshop|pointshop|'
                 r'marketshop|trader)\t([^\t]+)\t(.*)$')


def conf(root, path, files, seen):
    p = os.path.join(root, path)
    if path in seen or not os.path.exists(p):
        return
    seen.add(path)
    for line in open(p, encoding='latin1'):
        line = line.split('//')[0].strip()
        m = re.match(r'(npc|import|delete|delnpc):\s*(\S+)', line)
        if not m:
            continue
        k, v = m.groups()
        if k == 'npc':
            files.append(v)
        elif k == 'import':
            conf(root, v, files, seen)
        elif v in files:
            files.remove(v)


def strip_block_comments(text):
    """Blank /* ... */ (newlines kept) the way rAthena's script parser skips them. A /* inside a // comment or a
    "string" does not open a block. legion/srvwarps.go stripBlockComments is the same scanner."""
    out, i, n, instr = [], 0, len(text), False
    while i < n:
        c = text[i]
        if instr:
            out.append(c)
            if c == '\\' and i + 1 < n:
                out.append(text[i + 1])
                i += 2
                continue
            if c == '"' or c == '\n':
                instr = False
            i += 1
        elif c == '"':
            instr = True
            out.append(c)
            i += 1
        elif text.startswith('//', i):
            e = text.find('\n', i)
            e = n if e < 0 else e
            out.append(text[i:e])
            i = e
        elif text.startswith('/*', i):
            e = text.find('*/', i + 2)
            e = n if e < 0 else e + 2
            out.append('\n' * text.count('\n', i, e))
            i = e
        else:
            out.append(c)
            i += 1
    return ''.join(out)


def server_warps(root):
    files, seen = [], set()
    for c in ('npc/re/scripts_main.conf', 'npc/scripts_custom.conf'):
        conf(root, c, files, seen)
    for f in sorted(os.listdir(os.path.join(root, 'npc/custom-local'))) if os.path.isdir(
            os.path.join(root, 'npc/custom-local')) else []:
        files.append('npc/custom-local/' + f)
    warps, anynpc, byname = [], {}, {}
    for f in files:
        p = os.path.join(root, f)
        if not os.path.exists(p) or not p.endswith('.txt'):
            continue
        lines = strip_block_comments(open(p, encoding='latin1').read()).split('\n')
        i = 0
        while i < len(lines):
            line = lines[i].rstrip('\r')
            i += 1
            m = HDR.match(line)
            if not m:
                continue
            sm, sx, sy, typ, dup, name, rest = m.groups()
            sx, sy = int(sx), int(sy)
            anynpc.setdefault(sm, []).append((sx, sy))
            if typ.startswith('warp'):
                r = re.match(r'(\d+),(\d+),([\w@-]+),(\d+),(\d+)', rest)
                if r:
                    d = (r.group(3), int(r.group(4)), int(r.group(5)))
                    byname[name] = d
                    warps.append((sm, sx, sy) + d)
            elif dup:
                if dup in byname:
                    warps.append((sm, sx, sy) + byname[dup])
            elif typ == 'script' and re.match(r'(WARPNPC|45|HIDDEN_WARP_NPC|139),\d+,\d+,\{', rest):
                body = []
                while i < len(lines) and not lines[i].startswith('}'):
                    body.append(lines[i])
                    i += 1
                b = '\n'.join(body)
                ws = re.findall(r'warp\s+"([\w@-]+)",\s*(\d+),\s*(\d+)', b)
                if len(set(ws)) == 1 and 'OnTouch' in b and 'select' not in b and 'if' not in b:
                    d = (ws[0][0], int(ws[0][1]), int(ws[0][2]))
                    byname[name] = d
                    warps.append((sm, sx, sy) + d)
    return sorted(set(warps)), anynpc


def near(a, b, c, d, r):
    return abs(a - c) <= r and abs(b - d) <= r


def portal_lines(lines):
    """the 6-field plain lines of a portals.txt: [(tuple, raw line)]; NPC talk lines (more fields) are returned apart"""
    plain, allp = [], []
    for line in lines:
        line = line.rstrip('\r\n')
        s = line.split()
        if not s or line.startswith('#') or len(s) < 6:
            continue
        try:
            t = (s[0], int(s[1]), int(s[2]), s[3], int(s[4]), int(s[5]))
        except ValueError:
            continue
        allp.append(t)
        if len(s) == 6:
            plain.append((t, line))
    return plain, allp


def diff_portals(maps, warps, anynpc, lines):
    """The generator's rules (see the module doc) for the plain lines of one portals.txt. maps: the merged map cache
    (names); warps, anynpc: server_warps(). Returns (drop, add): drop = the raw lines to remove, add = the sorted
    'map x y dmap dx dy' lines to add. fieldcheck.py reports both for the container's file."""
    by_src = {}
    for w in warps:
        by_src.setdefault(w[0], []).append(w)
    plain, allp = portal_lines(lines)
    drop, add = [], []
    for t, line in plain:
        sm, sx, sy, dm, dx, dy = t
        if sm not in maps:
            continue
        cand = [w for w in by_src.get(sm, ()) if w[3] == dm and near(w[1], w[2], sx, sy, 3)]
        if cand:
            if not any(near(w[4], w[5], dx, dy, 4) for w in cand):
                w = min(cand, key=lambda w: abs(w[1] - sx) + abs(w[2] - sy))
                drop.append(line)
                add.append('%s %d %d %s %d %d' % (sm, sx, sy, dm, w[4], w[5]))
        elif not any(near(x, y, sx, sy, 3) for x, y in anynpc.get(sm, [])):
            drop.append(line)
    for w in warps:
        sm, sx, sy, dm, dx, dy = w
        if '@' in sm or '@' in dm or sm not in maps or dm not in maps:
            continue
        if not any(p[0] == sm and p[3] == dm and near(p[1], p[2], sx, sy, 3) for p in allp):
            add.append('%s %d %d %s %d %d' % w)
    return drop, sorted(set(add))


def main():
    root, portals, mc, mcre, out = sys.argv[1:6]
    maps = load(mc)
    maps.update(load(mcre))
    warps, anynpc = server_warps(root)
    with open(portals, encoding='latin1') as f:
        drop, add = diff_portals(maps, warps, anynpc, f.readlines())
    with open(out, 'w', encoding='utf-8', newline='\n') as f:
        f.write("# portals-srv.txt - generated by gen_portals_from_npc.py from the server's loaded npc warps; applied by\n"
                '# fix_srv_portals.py. "-" = drop this exact portals.txt line, "+" = add this line.\n')
        for line in sorted(set(drop)):
            f.write('- %s\n' % line)
        for line in add:
            f.write('+ %s\n' % line)
    print('server warps %d, drop %d, add %d -> %s' % (len(warps), len(set(drop)), len(add), out))


if __name__ == '__main__':
    main()
