"""Point kRO's hard-coded online-service hostnames at 127.0.0.1 in a (WARP-patched) 2021 RagexeRE copy, so the
client doesn't wait on Gravity's servers when used with a local rAthena.   usage: python patch_hosts_2021.py <exe>
Only plain hostname strings are rewritten (same length or shorter, NUL padded); idempotent."""
import sys
exe = sys.argv[1]
d = bytearray(open(exe, 'rb').read())
hosts = [b'kro-agency-s.ragnarok.co.kr:6954', b'kro-agency.ragnarok.co.kr',
         b'kro-assit-s.ragnarok.co.kr:3000',
         # login servers the client uses after getting its agency token
         b'kro-acc1.ragnarok.co.kr:6900', b'kro-acc3.ragnarok.co.kr:6900',
         b'kro-qm-1a.ragnarok.co.kr:6900', b'kro-qm-1a.ragnarok.co.kr:6951', b'kro-qm-2a.ragnarok.co.kr:6900']
for h in hosts:
    new = b'127.0.0.1' + (h[h.index(b':'):] if b':' in h else b'')
    n = 0; i = d.find(b'\0' + h + b'\0')
    while i >= 0:
        d[i + 1:i + 1 + len(h)] = new + b'\0' * (len(h) - len(new)); n += 1
        i = d.find(b'\0' + h + b'\0', i + 1)
    print('%-40s -> %-20s x%d' % (h.decode(), new.decode(), n))
open(exe, 'wb').write(d)
