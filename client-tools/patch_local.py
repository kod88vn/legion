"""Extra binary patches for the WARP-patched kRO 2025-09-03 Ragexe so it runs against a LOCAL rAthena.
(apply after WARP; idempotent)            usage: python patch_local.py <exe>

1) Epic Online Services (EOS) anti-cheat
   Gravity's EAC launcher normally supplies the EOS SDK DLL; without it the client aborts with
   "nullptr == m_hEOSLibrary" / "CEOSAntiCheatMgr::CreateInstance()".
   Inside CEOSAntiCheatMgr::CreateInstance():
       c6 46 08 00        mov byte [esi+8],0     ; m_bInitialized = false
       e8 xx xx xx xx     call Init              ; LoadLibrary(EOSSDK) + MessageBox on failure
       84 c0 74 08 c6 46 08 01
       ...
       80 7e 08 00        cmp byte [esi+8],0
       75 24              jne success            ; else delete instance, return false
   -> call Init => xor al,al (no DLL, no popup);  jne success => jmp success
      (keeps a valid but inactive manager instance).

2) nProtect KeyCrypt (npkcrypt.dll)
   The login-window init LoadLibrary("npkcrypt.dll")s and, if the DLL is missing, still calls its
   null function pointers -> crash on entering the login screen.
   -> that init function returns 0 ("KeyCrypt unavailable") immediately: xor eax,eax ; ret
"""
import re, struct, sys
import pefile

exe = sys.argv[1]
d = bytearray(open(exe, 'rb').read())
pe = pefile.PE(data=bytes(d), fast_load=True)
base = pe.OPTIONAL_HEADER.ImageBase


def off2va(o):
    for s in pe.sections:
        if s.PointerToRawData <= o < s.PointerToRawData + s.SizeOfRawData:
            return base + s.VirtualAddress + o - s.PointerToRawData


# ---- 1) EOS ---------------------------------------------------------------------------------
a = re.compile(rb'\xc6\x46\x08\x00\xe8....\x84\xc0\x74\x08\xc6\x46\x08\x01', re.S)
b = re.compile(rb'\x80\x7e\x08\x00\x75\x24\x8b\x06\x8b\xce\x6a\x01\xff\x10', re.S)
ma, mb = list(a.finditer(d)), list(b.finditer(d))
if not ma and d.find(b'\xc6\x46\x08\x00\x32\xc0\x90\x90\x90\x84\xc0\x74\x08\xc6\x46\x08\x01') >= 0:
    print('EOS: already patched')
else:
    assert len(ma) == 1 and len(mb) == 1, 'EOS pattern count a=%d b=%d' % (len(ma), len(mb))
    assert 0 < mb[0].start() - ma[0].start() < 0x80, 'EOS patterns not adjacent'
    d[ma[0].start() + 4:ma[0].start() + 9] = b'\x32\xc0\x90\x90\x90'
    d[mb[0].start() + 4] = 0xEB
    print('EOS: patched at va %x / %x' % (off2va(ma[0].start() + 4), off2va(mb[0].start() + 4)))

# ---- 2) npkcrypt ----------------------------------------------------------------------------
so = d.find(b'\0npkcrypt.dll\0') + 1
assert so > 0, 'npkcrypt string not found'
needle = b'\x68' + struct.pack('<I', off2va(so))
start, ref = 0, d.find(needle)
while ref > 0 and not start:
    w = bytes(d[ref - 0x80:ref])
    for pro in (b'\xcc\x55\x8b\xec', b'\xcc\x31\xc0\xc3'):   # prologue right after int3 padding
        i = w.rfind(pro)
        if i >= 0:
            start = ref - 0x80 + i + 1
            break
    ref = d.find(needle, ref + 1)
assert start > 0, 'npkcrypt init prologue not found'
if d[start:start + 3] == b'\x31\xc0\xc3':
    print('npkcrypt: already patched')
else:
    d[start:start + 3] = b'\x31\xc0\xc3'
    print('npkcrypt: patched init at va %x' % off2va(start))

open(exe, 'wb').write(d)
