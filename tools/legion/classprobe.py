#!/usr/bin/env python3
"""Class probe: watch representative online bots of every class and measure whether they make progress.
Usage: python3 classprobe.py [minutes=3] [per_class=2]
Samples /api/bots/<name> every 5 s; prints per bot: kills/min, share of samples per state, distance moved, HP range,
and flags (NO KILLS, STUCK, NEVER HURT). Run from WSL or Windows (needs the Legion API on 127.0.0.1:8090)."""
import json, sys, time, random, urllib.request, collections

API = 'http://127.0.0.1:8090'
mins = float(sys.argv[1]) if len(sys.argv) > 1 else 3
per = int(sys.argv[2]) if len(sys.argv) > 2 else 2


def get(path):
    with urllib.request.urlopen(API + path, timeout=10) as r:
        return json.load(r)


bots = [b for b in get('/api/bots') if b.get('online') and b.get('role') != 'vendor']
by = collections.defaultdict(list)
for b in bots:
    by[b['class']].append(b)
pick = []
for cls, bs in sorted(by.items()):
    fighting = [b for b in bs if not b['state'].startswith(('travel', 'town trip', 'going to the Kafra'))]
    pool = fighting or bs
    random.shuffle(pool)
    pick += pool[:per]
print('probing', len(pick), 'bots:', ', '.join('%s(%s %s)' % (b['name'], b['class'], b.get('level')) for b in pick), flush=True)

samples = {b['name']: [] for b in pick}
end = time.time() + mins * 60
while time.time() < end:
    for n in samples:
        try:
            d = get('/api/bots/' + urllib.request.quote(n))
            samples[n].append(d)
        except Exception:
            pass
    time.sleep(5)

print()
for n, ss in samples.items():
    if len(ss) < 2:
        print(n, 'no data'); continue
    a, z = ss[0], ss[-1]
    secs = mins * 60
    kills = (z.get('kills') or 0) - (a.get('kills') or 0)
    st = collections.Counter(s['state'].split(' ->')[0].split(' (')[0].split(' off ')[0].split(' near ')[0][:24] for s in ss)
    moved = sum(abs(ss[i]['x'] - ss[i - 1]['x']) + abs(ss[i]['y'] - ss[i - 1]['y']) for i in range(1, len(ss)) if ss[i]['map'] == ss[i - 1]['map'])
    hps = [s['hp'] for s in ss if s.get('hp') is not None]
    flags = []
    if kills == 0: flags.append('NO KILLS')
    if moved < 5: flags.append('STUCK')
    if hps and min(hps) == max(hps): flags.append('NEVER HURT')
    print('%-18s %-9s lv%-3s map %-11s kills/min %.1f moved %4d hp %s-%s %s' % (
        n, a['class'][:9], a.get('base_level'), z.get('map'), kills / (secs / 60), moved, min(hps or [0]), max(hps or [0]), ' '.join(flags)))
    print('      states:', ', '.join('%s %d%%' % (k, 100 * v // len(ss)) for k, v in st.most_common(5)))
