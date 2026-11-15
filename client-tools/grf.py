"""Minimal GRF (0x200/0x300) reader.  usage:
  grf.py <grf> list [substr]
  grf.py <grf> get <name> <outfile>
"""
import struct, sys, zlib

def load(path):
    f = open(path, 'rb')
    h = f.read(46)
    ver = struct.unpack('<I', h[42:46])[0]
    if ver == 0x300:
        off = struct.unpack('<Q', h[30:38])[0]
        cnt = struct.unpack('<I', h[38:42])[0]
    else:
        off, seed, cnt = struct.unpack('<III', h[30:42]); cnt -= seed + 7
    f.seek(off + 46)
    if ver == 0x300:
        f.read(4)
    zs, rs = struct.unpack('<II', f.read(8))
    tbl = zlib.decompress(f.read(zs))
    ents, p = {}, 0
    while p < len(tbl):
        e = tbl.index(b'\0', p)
        name = tbl[p:e].decode('cp949', 'replace'); p = e + 1
        if ver == 0x300:
            zsz, zal, sz, fl, of = struct.unpack('<IIIBQ', tbl[p:p+21]); p += 21
        else:
            zsz, zal, sz, fl, of = struct.unpack('<IIIBI', tbl[p:p+17]); p += 17
        ents[name.lower()] = (name, zsz, zal, sz, fl, of)
    return f, ents

def get(f, ent):
    name, zsz, zal, sz, fl, of = ent
    f.seek(of + 46)
    d = f.read(zal)
    if fl & 6:
        raise SystemExit('encrypted entry: ' + name)
    return zlib.decompress(d[:zsz]) if sz != zsz or True else d

if __name__ == '__main__':
    f, ents = load(sys.argv[1])
    if sys.argv[2] == 'list':
        s = sys.argv[3].lower() if len(sys.argv) > 3 else ''
        for k, v in ents.items():
            if s in k: print(v[0], v[3])
    elif sys.argv[2] == 'get':
        open(sys.argv[4], 'wb').write(get(f, ents[sys.argv[3].lower()]))
