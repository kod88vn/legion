"""Extract  Table.key = value  assignments (string key -> number/string value) from a Lua 5.1 (x86) .lub
by simulating LOADK / NEWTABLE / SETTABLE / SETGLOBAL in the main chunk.
usage: luadump.py file.lub  -> prints  TABLE<TAB>key<TAB>value"""
import struct, sys

class R:
    def __init__(s, d): s.d, s.p = d, 0
    def u8(s): v = s.d[s.p]; s.p += 1; return v
    def i32(s): v = struct.unpack_from('<i', s.d, s.p)[0]; s.p += 4; return v
    def u32(s): v = struct.unpack_from('<I', s.d, s.p)[0]; s.p += 4; return v
    def num(s): v = struct.unpack_from('<d', s.d, s.p)[0]; s.p += 8; return v
    def str(s):
        n = s.u32()
        if n == 0: return None
        v = s.d[s.p:s.p+n-1]; s.p += n; return v.decode('cp949', 'replace')

def func(r):
    r.str(); r.i32(); r.i32(); r.u8(); r.u8(); r.u8(); r.u8()
    code = [r.u32() for _ in range(r.i32())]
    K = []
    for _ in range(r.i32()):
        t = r.u8()
        K.append(None if t == 0 else bool(r.u8()) if t == 1 else r.num() if t == 3 else r.str())
    protos = [func(r) for _ in range(r.i32())]
    for _ in range(r.i32()): r.i32()            # lineinfo
    for _ in range(r.i32()): r.str(); r.i32(); r.i32()  # locvars
    for _ in range(r.i32()): r.str()            # upvalues
    return code, K, protos

def run(path):
    d = open(path, 'rb').read()
    assert d[:4] == b'\x1bLua' and d[4] == 0x51, 'not lua5.1 bytecode'
    r = R(d); r.p = 12
    code, K, _ = func(r)
    reg, tables, out = {}, {}, []
    RK = lambda x: K[x - 256] if x >= 256 else reg.get(x)
    for ins in code:
        op = ins & 0x3f; A = (ins >> 6) & 0xff; C = (ins >> 14) & 0x1ff; B = (ins >> 23) & 0x1ff; Bx = ins >> 14
        if op == 1:   reg[A] = K[Bx]                       # LOADK
        elif op == 10: t = {}; reg[A] = ('T', id(t)); tables[id(t)] = t   # NEWTABLE
        elif op == 9:                                     # SETTABLE
            tv = reg.get(A)
            if isinstance(tv, tuple): tables[tv[1]][RK(B)] = RK(C)
        elif op == 7:                                     # SETGLOBAL
            tv = reg.get(A)
            if isinstance(tv, tuple): out.append((K[Bx], tables[tv[1]]))
        elif op == 5: reg[A] = ('G', K[Bx])               # GETGLOBAL
        elif op == 0: reg[A] = reg.get(B)                 # MOVE
    return out

if __name__ == '__main__':
    for name, t in run(sys.argv[1]):
        for k, v in t.items():
            if isinstance(v, float) and v.is_integer(): v = int(v)
            print('%s\t%s\t%s' % (name, k, v))
