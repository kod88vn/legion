#!/usr/bin/env python3
"""Record what the real rAthena login/char servers send the 2021 client (reference for the X-Kore 2 fix).
Client login address -> 127.0.0.1:6905 -> rathena <container IP>:6900. The server list is rewritten so the client's
char connection comes to 127.0.0.1:6906 -> rathena :6121. Everything is logged as hex to /tmp/realcap.txt."""
import socket, struct, threading, time, binascii, os, subprocess

try:  # container IP resolved at run time (ayla-ip.sh), the old value only as fallback
    RA = subprocess.check_output(['bash', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ayla-ip.sh')], text=True).strip()
except Exception:
    RA = '172.18.0.3'
LOG = open('/tmp/realcap.txt', 'a', buffering=1)


def log(tag, d):
    LOG.write('%s %s len=%d %s\n' % (time.strftime('%H:%M:%S'), tag, len(d), binascii.hexlify(d[:1200]).decode()))


def pump(a, b, tag, rewrite=False):
    try:
        while True:
            d = a.recv(65536)
            if not d:
                break
            log(tag, d)
            if rewrite:
                d = d.replace(b'\x7f\x00\x00\x01' + struct.pack('<H', 6121), b'\x7f\x00\x00\x01' + struct.pack('<H', 6906))
            b.sendall(d)
    except OSError:
        pass
    log(tag + ' CLOSED', b'')
    for s in (a, b):
        try:
            s.close()
        except OSError:
            pass


def serve(lp, rp, name):
    ls = socket.socket()
    ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    ls.bind(('127.0.0.1', lp))
    ls.listen(4)
    while True:
        c, _ = ls.accept()
        r = socket.create_connection((RA, rp))
        threading.Thread(target=pump, args=(c, r, name + ' C>S'), daemon=True).start()
        threading.Thread(target=pump, args=(r, c, name + ' S>C', name == 'login'), daemon=True).start()


threading.Thread(target=serve, args=(6905, 6900, 'login'), daemon=True).start()
threading.Thread(target=serve, args=(6906, 6121, 'char'), daemon=True).start()
LOG.write('--- up %s\n' % time.ctime())
while True:
    time.sleep(3600)
