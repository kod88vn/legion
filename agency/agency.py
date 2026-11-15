"""Minimal stand-in for kRO's login "agency" (kro-agency.ragnarok.co.kr:6953) for the local lab.

The 2021 kRO client (RagexeRE) logs in in two steps:
  1. client -> agency : 0x0ACF  game_code[4] username[25] password(encrypted)[32] flag[5]
     agency -> client : 0x0AE3  len.W login_type.L flag[20] token[]
  2. client -> login server (clientinfo.xml address, rAthena) : 0x0825 SSO login carrying that token;
     rAthena checks the token as the account password.

The client's password field is encrypted with a scheme we don't implement, so this stub returns
token = TOKEN_MAP.get(username, username). With the seeded accounts (password == username, e.g.
player/player) that logs straight in. NOTE: the password typed into the client is therefore NOT
checked. Bind this only to 127.0.0.1 (see docker-compose.yml)."""
import os, socket, struct, threading

PORT = int(os.environ.get('AGENCY_PORT', '6953'))
TOKEN_MAP = dict(kv.split(':', 1) for kv in os.environ.get('AGENCY_TOKENS', '').split(',') if ':' in kv)


def handle(c, addr):
    buf = b''
    try:
        while True:
            d = c.recv(4096)
            if not d:
                break
            buf += d
            while len(buf) >= 2:
                pid = struct.unpack_from('<H', buf)[0]
                if pid == 0x0ACF and len(buf) >= 68:
                    user = buf[6:31].split(b'\0', 1)[0].decode('latin1')
                    token = TOKEN_MAP.get(user, user).encode('latin1')
                    body = struct.pack('<l', 0) + b'S1000'.ljust(20, b'\0') + token + b'\0'
                    c.sendall(struct.pack('<HH', 0x0AE3, 4 + len(body)) + body)
                    print('agency: token issued for %r (%s)' % (user, addr[0]), flush=True)
                    buf = buf[68:]
                elif pid == 0x0ACF:
                    break  # wait for the rest
                else:
                    print('agency: unknown packet %04x len %d, ignoring' % (pid, len(buf)), flush=True)
                    buf = b''
    except OSError:
        pass
    finally:
        c.close()


s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('0.0.0.0', PORT)); s.listen(16)
print('agency: listening on', PORT, flush=True)
while True:
    conn, a = s.accept()
    threading.Thread(target=handle, args=(conn, a), daemon=True).start()
