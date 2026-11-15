"""Translate a few Korean UI strings that are hard-coded in the 2021 RagexeRE exe (not covered by msgstringtable or
WARP's TranslateClient), e.g. the default chat-tab names.   usage: python patch_strings_2021.py <exe>
Each string is matched as a whole NUL-terminated CP949 string and replaced in place (shorter, NUL padded); idempotent."""
import sys
exe = sys.argv[1]
d = bytearray(open(exe, 'rb').read())
table = {
    '일반 메시지': 'General',
    '배틀 메시지': 'Battle',
}
for ko, en in table.items():
    k = ko.encode('cp949'); e = en.encode('ascii')
    assert len(e) <= len(k)
    n = 0; i = d.find(b'\0' + k + b'\0')
    while i >= 0:
        d[i + 1:i + 1 + len(k)] = e + b'\0' * (len(k) - len(e)); n += 1
        i = d.find(b'\0' + k + b'\0', i + 1)
    print('%s -> %s x%d' % (ko, en, n))
open(exe, 'wb').write(d)
