#!/usr/bin/env python3
"""fix_srv_portals.py - apply openkore/portals-srv.txt (from gen_portals_from_npc.py) to tables/portals.txt: drop the
plain warp lines the server does not have (or has with another landing cell) and add the server warps OpenKore lacks.
Idempotent: drops the "-" lines and its own marked block, appends the block again; rewrites only on change.
Run after fix_re_portals.sh and before fix_kafra_portals.py (it never touches NPC talk lines).
Usage: python3 -I fix_srv_portals.py tables/portals.txt portals-srv.txt
"""
import sys

START = '# Server warps (fix_srv_portals.py, from portals-srv.txt)'
END = '# end Server warps'


def main():
    portals, patch = sys.argv[1], sys.argv[2]
    drop, add = set(), []
    for line in open(patch, encoding='utf-8'):
        line = line.rstrip('\r\n')
        if line.startswith('- '):
            drop.add(line[2:].strip())
        elif line.startswith('+ '):
            add.append(line[2:].strip())
    raw = open(portals, 'rb').read().decode('latin1')
    nl = '\r\n' if '\r\n' in raw else '\n'
    old = raw.split(nl)
    if old and old[-1] == '':
        old.pop()
    kept, inblock, block_at = [], False, None
    for line in old:
        s = line.strip()
        if s == START:
            inblock = True
            block_at = len(kept)
            continue
        if inblock:
            if s == END:
                inblock = False
            continue
        if s in drop:
            continue
        kept.append(line)
    blk = [START] + add + [END]
    if block_at is None:
        # before the Kafra block (fix_kafra_portals.py keeps that one last), else at the end
        k = [i for i, l in enumerate(kept) if l.startswith('# Kafra teleports (fix_kafra_portals.py')]
        block_at = k[0] if k else len(kept)
    new = kept[:block_at] + blk + kept[block_at:]
    out = nl.join(new) + nl
    if out == raw:
        print('portals.txt already has the server warps (%d dropped lines, %d added)' % (len(drop), len(add)))
        return
    open(portals, 'wb').write(out.encode('latin1'))
    print('portals.txt: server warps applied (%d lines dropped, %d added)' % (len(drop), len(add)))


if __name__ == '__main__':
    main()
