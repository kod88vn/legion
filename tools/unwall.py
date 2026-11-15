"""unwall.py - move OFFLINE Legion bot characters off blocked map cells (2026-11-14).

rAthena's pc_setpos only relocates a character whose position is out of bounds or 0,0; a character saved on a
blocked cell logs back in on it. The old seed put Novices at "town spawn +-3" without a walkability check: 227 Legion
chars stood on blocked cells and 17 were walled in (geffen 116,56 is inside the fountain wall). The legion engine logs
a walled-in bot out (ai.go stepAlong) and keeps it offline for 3 minutes so this tool can fix its row.

Only bot accounts (2100001-2199999, the lg* accounts) that are OFFLINE are touched; human characters and Ayla's party
never. A blocked last position moves to the nearest walkable cell connected to the save point (else the save point);
a blocked save point moves to the nearest walkable cell.

  python tools/unwall.py            # one pass, prints what it changed
  python tools/unwall.py --dry      # report only
  python tools/unwall.py --loop     # every 30 s (run in the background)
"""
import collections, os, struct, subprocess, sys, tempfile, time, zlib

WSL = ['wsl', '-d', 'Ubuntu-24.04', '--']
CACHE = os.path.join(tempfile.gettempdir(), 'unwall_mapcache')


def sql(q):
    r = subprocess.run(WSL + ['docker', 'exec', '-i', 'ro-db', 'mariadb', '-uragnarok', '-pragnarok', 'ragnarok', '-N', '-B'],
                       input=q, capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(r.stderr)
    return [l.split('\t') for l in r.stdout.splitlines() if l]


def load_maps():
    os.makedirs(os.path.join(CACHE, 're'), exist_ok=True)
    wcache = subprocess.run(WSL + ['wslpath', '-a', CACHE.replace('\\', '/')], capture_output=True, text=True).stdout.strip()
    for f in ('map_cache.dat', 're/map_cache.dat'):
        subprocess.run(WSL + ['docker', 'cp', 'ro-rathena:/rathena/db/' + f, wcache + '/' + f], check=True)
    maps = {}
    for f in ('map_cache.dat', os.path.join('re', 'map_cache.dat')):  # Renewal cache on top, like the server
        d = open(os.path.join(CACHE, f), 'rb').read()
        n, p = struct.unpack_from('<H', d, 4)[0], 8
        for _ in range(n):
            name = d[p:p + 12].split(b'\0')[0].decode()
            xs, ys, l = struct.unpack_from('<hhI', d, p + 12)
            p += 20
            maps[name] = (xs, ys, zlib.decompress(d[p:p + l]))
            p += l
    return maps


def walk(m, x, y):
    xs, ys, c = m
    return 0 <= x < xs and 0 <= y < ys and c[x + y * xs] not in (1, 5)


def nearest(m, x, y, anchor=None, r=12):
    """nearest walkable cell to (x, y); with anchor, only cells connected to the anchor cell (BFS, max 4000 cells)."""
    ok = None
    if anchor and walk(m, *anchor):
        ok, q = {anchor}, collections.deque([anchor])
        while q and len(ok) < 4000:
            a, b = q.popleft()
            for dx in (-1, 0, 1):
                for dy in (-1, 0, 1):
                    n = (a + dx, b + dy)
                    if n not in ok and walk(m, *n) and (dx == 0 or dy == 0 or (walk(m, a + dx, b) and walk(m, a, b + dy))):
                        ok.add(n)
                        q.append(n)
    best = None
    for dy in range(-r, r + 1):
        for dx in range(-r, r + 1):
            c = (x + dx, y + dy)
            if walk(m, *c) and (ok is None or c in ok):
                d = max(abs(dx), abs(dy))
                if best is None or d < best[0]:
                    best = (d, c)
    return best[1] if best else None


def one_pass(maps, dry):
    rows = sql("select char_id,name,last_map,last_x,last_y,save_map,save_x,save_y from `char` "
               "where online=0 and account_id between 2100001 and 2199999 and "
               "account_id not in (select account_id from `char` where online=1);")
    fixed = 0
    for cid, name, lm, lx, ly, sm, sx, sy in rows:
        lx, ly, sx, sy = int(lx), int(ly), int(sx), int(sy)
        sets = []
        if sm in maps and not walk(maps[sm], sx, sy):
            c = nearest(maps[sm], sx, sy)
            if c:
                sets.append(f"save_x={c[0]},save_y={c[1]}")
                sx, sy = c
        if lm in maps and not walk(maps[lm], lx, ly):
            c = nearest(maps[lm], lx, ly, anchor=(sx, sy) if lm == sm else None)
            if c:
                sets.append(f"last_x={c[0]},last_y={c[1]}")
            elif lm == sm:
                sets.append(f"last_x={sx},last_y={sy}")
        if sets:
            fixed += 1
            print(f"{name}: {lm} {lx},{ly} / save {sm} -> {', '.join(sets)}")
            if not dry:
                sql(f"update `char` set {','.join(sets)} where char_id={int(cid)} and online=0;")
    return fixed


def main():
    dry, loop = '--dry' in sys.argv, '--loop' in sys.argv
    maps = load_maps()
    while True:
        n = one_pass(maps, dry)
        print(time.strftime('%H:%M:%S'), f"{n} offline bot chars {'to fix' if dry else 'fixed'}", flush=True)
        if not loop:
            break
        time.sleep(30)


if __name__ == '__main__':
    main()
