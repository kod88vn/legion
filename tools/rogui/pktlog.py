#!/usr/bin/env python3
"""Logging TCP relay for debugging the OpenKore X-Kore 2 proxy with the 2021 client.
Listens on 127.0.0.1 ports and forwards to OpenKore inside the rathena container, logging every chunk as hex.
  6904 -> <rathena IP>:6901 (login)   6902 -> :6902 (char)   6903 -> :6903 (map)
Usage: [TARGET=ip BIND=ip PORTS=l:r,...] python3 pktlog.py [logfile]
Inside the openkore container (OpenKore binds 127.0.0.1 there): TARGET=127.0.0.1 BIND=<rathena IP> PORTS=6901:6901,6902:6902,6903:6903"""
import socket, sys, threading, time, binascii

import os, subprocess


def ayla_ip():  # same lookup as ayla-ip.sh (the container IP changes when the network is rebuilt)
    try:
        return subprocess.check_output(['bash', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'ayla-ip.sh')], text=True).strip()
    except Exception:
        return '172.18.0.3'


TARGET = os.environ.get('TARGET') or ayla_ip()
BIND = os.environ.get('BIND', '127.0.0.1')
PORTS = {int(a): int(b) for a, b in (x.split(':') for x in os.environ.get('PORTS', '6904:6901,6902:6902,6903:6903').split(','))}
LOG = open(sys.argv[1] if len(sys.argv) > 1 else '/tmp/pktlog.txt', 'a', buffering=1)
lock = threading.Lock()


def log(tag, data):
    with lock:
        LOG.write('%s %s len=%d %s\n' % (time.strftime('%H:%M:%S'), tag, len(data), binascii.hexlify(data[:400]).decode()))


def pump(src, dst, tag):
    try:
        while True:
            d = src.recv(65536)
            if not d:
                break
            log(tag, d)
            dst.sendall(d)
    except OSError:
        pass
    log(tag + ' CLOSED', b'')
    for s in (src, dst):
        try:
            s.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass


def serve(lport, rport):
    ls = socket.socket()
    ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    ls.bind((BIND, lport))
    ls.listen(8)
    while True:
        c, _ = ls.accept()
        try:
            r = socket.create_connection((TARGET, rport), timeout=5)
            r.settimeout(None)
        except OSError as e:
            log('%d CONNECT-FAIL %s' % (rport, e), b'')
            c.close()
            continue
        threading.Thread(target=pump, args=(c, r, '%d C>S' % rport), daemon=True).start()
        threading.Thread(target=pump, args=(r, c, '%d S>C' % rport), daemon=True).start()


for lp, rp in PORTS.items():
    threading.Thread(target=serve, args=(lp, rp), daemon=True).start()
LOG.write('--- relay up %s\n' % time.ctime())
while True:
    time.sleep(3600)
