"""pcapdump.py <pcap> <recvpackets.txt> [maxpkts] : split every TCP flow on ports 6900/6121/5121 into RO packets
(using the client packet-length table; -1 = u16 length at offset 2) and print them in time order."""
import struct, sys, collections
pc, tbl = sys.argv[1], sys.argv[2]; maxp = int(sys.argv[3]) if len(sys.argv) > 3 else 400
L = {}
for line in open(tbl):
    p = line.split()
    if len(p) >= 2 and len(p[0]) == 4:
        try: L[int(p[0], 16)] = int(p[1])
        except ValueError: pass
d = open(pc, 'rb').read(); lt, = struct.unpack('<I', d[20:24]); off = 24
segs = collections.defaultdict(dict)
while off + 16 <= len(d):
    ts, tu, incl, _ = struct.unpack('<IIII', d[off:off + 16]); pkt = d[off + 16:off + 16 + incl]; off += 16 + incl
    l2 = {113: 16, 276: 20, 0: 4}.get(lt, 14); ip = pkt[l2:]
    if not ip or ip[0] >> 4 != 4 or ip[9] != 6: continue
    ihl = (ip[0] & 15) * 4; tcp = ip[ihl:]; sp, dp, seq = struct.unpack('>HHI', tcp[:8]); pl = tcp[(tcp[12] >> 4) * 4:]
    if pl: segs[(sp, dp)].setdefault(seq, (ts + tu / 1e6, pl))
events = []
for (sp, dp), m in segs.items():
    srv = sp if sp in (6900, 6121, 5121) else dp
    tag = {6900: 'LOGIN', 6121: 'CHAR', 5121: 'MAP'}[srv] + (' S>C' if sp == srv else ' C>S')
    buf = b''; times = []
    for seq in sorted(m):
        t, pl = m[seq]; times.append((len(buf), t)); buf += pl
    def tat(o):
        r = times[0][1]
        for oo, t in times:
            if oo > o: break
            r = t
        return r
    p = 0
    if tag == 'CHAR S>C' and len(buf) >= 4:   # char server sends the raw 4-byte account id first
        events.append((tat(0), tag, 'raw4', buf[:4].hex())); p = 4
    while p + 2 <= len(buf):
        op, = struct.unpack_from('<H', buf, p); n = L.get(op)
        if n is None:
            events.append((tat(p), tag, '%04x' % op, 'UNKNOWN LEN, rest %d bytes: %s' % (len(buf) - p, buf[p:p + 32].hex()))); break
        if n == -1: n, = struct.unpack_from('<H', buf, p + 2)
        if n < 2: events.append((tat(p), tag, '%04x' % op, 'bad len')); break
        events.append((tat(p), tag, '%04x' % op, '%3d  %s' % (n, buf[p:p + min(n, 48)].hex()))); p += n
events.sort()
t0 = events[0][0] if events else 0
for e in events[:maxp]:
    print('%8.3f %-9s %s %s' % (e[0] - t0, e[1], e[2], e[3]))
print('total', len(events))
