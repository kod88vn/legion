#!/usr/bin/env python3
"""gen_fields_from_mapcache.py - write OpenKore field files (.fld2.gz) from rAthena's map cache, for every map whose
walkable cells differ from OpenKore's shipped field (2026-11-14 travel QA: 99 maps differed, e.g. prontera 1,286
cells, aldebaran 137, comodo 112, yuno 53, yuno_fild02 669, moc_fild08-20, anthell01/02, izlude_in).

Usage (inside an OpenKore container, python3 -I):
  gen_fields_from_mapcache.py <db/map_cache.dat> <db/re/map_cache.dat> <out_dir> [--fields /openkore/fields] [--all]
Cell types (rathena map_gat2cell): 0/2/4/6 walkable, 1 not, 3 walkable water, 5 not walkable but shootable.
fld2 (OpenKore Field.pm): 1 walk, 0 no-walk, 4 water (5 = walkable water), 2 snipe (shootable).
"""
import gzip, os, struct, sys, zlib

GAT2FLD = {0: 1, 1: 0, 2: 1, 3: 5, 4: 1, 5: 2, 6: 1}


def load(p):
    d = open(p, 'rb').read()
    _, n = struct.unpack('<IH', d[:6])
    o, m = 8, {}
    for _ in range(n):
        name = d[o:o + 12].split(b'\0')[0].decode()
        xs, ys, ln = struct.unpack('<hhi', d[o + 12:o + 20])
        o += 20
        m[name] = (xs, ys, d[o:o + ln])
        o += ln
    return m


def read_fld(fields, name):
    for p in (os.path.join(fields, name + '.fld2.gz'), os.path.join(fields, name + '.fld2')):
        if os.path.exists(p):
            return gzip.open(p).read() if p.endswith('.gz') else open(p, 'rb').read()
    return None


def main():
    a = sys.argv[1:]
    all_maps = '--all' in a
    fields = a[a.index('--fields') + 1] if '--fields' in a else '/openkore/fields'
    base, re_, out = a[0], a[1], a[2]
    maps = load(base)
    maps.update(load(re_))  # the RE cache overrides
    os.makedirs(out, exist_ok=True)
    n = 0
    for name, (xs, ys, z) in sorted(maps.items()):
        cells = zlib.decompress(z)
        fld = bytes(GAT2FLD.get(c, 0) for c in cells)
        old = read_fld(fields, name)
        if not all_maps and old is not None:
            w, h = struct.unpack('<HH', old[:4])
            if (w, h) == (xs, ys) and all((x & 1) == (y & 1) for x, y in zip(old[4:], fld)):
                continue
        if not all_maps and old is None:
            continue  # OpenKore has no such map: leave it alone
        with gzip.open(os.path.join(out, name + '.fld2.gz'), 'wb') as f:
            f.write(struct.pack('<HH', xs, ys) + fld)
        n += 1
        print(name, xs, ys)
    print('wrote', n, 'field files to', out)


if __name__ == '__main__':
    main()
