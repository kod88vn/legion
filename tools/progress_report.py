"""progress_report.py: how the Legion population is progressing on its build plans (run in WSL: python3 ...).
Levels/classes from the DB, plan status and events from the Legion API."""
import json, subprocess, urllib.request, collections, sys

API = 'http://127.0.0.1:8090'


def q(sql):
    out = subprocess.run(['docker', 'exec', 'ro-db', 'mariadb', '-uragnarok', '-pragnarok', 'ragnarok', '-N', '-e', sql],
                         capture_output=True, text=True).stdout
    return [l.split('\t') for l in out.strip().splitlines() if l]


def api(path):
    return json.load(urllib.request.urlopen(API + path, timeout=20))


rows = q("SELECT base_level, job_level, class, status_point, skill_point, zeny FROM `char` WHERE account_id BETWEEN 2100001 AND 2199999")
lv = collections.Counter(); cls = collections.Counter(); unspent = 0; skp = 0; zeny = []
for b, j, c, sp, kp, z in rows:
    b = int(b); lv[(b // 10) * 10] += 1; cls[int(c)] += 1; unspent += int(sp) > 10; skp += int(kp) > 3; zeny.append(int(z))
print('characters %d | base level buckets %s' % (len(rows), dict(sorted(lv.items()))))
print('classes', dict(cls.most_common(12)))
print('chars with >10 unspent stat points: %d, >3 unspent skill points: %d (offline chars keep what they had)' % (unspent, skp))
zeny.sort(); print('zeny median %d, top %d' % (zeny[len(zeny) // 2], zeny[-1]))
sk = q("SELECT COUNT(DISTINCT char_id), COUNT(*) FROM skill WHERE char_id IN (SELECT char_id FROM `char` WHERE account_id BETWEEN 2100001 AND 2199999)")
print('characters with learned skills: %s (rows %s)' % tuple(sk[0]))
eq = q("SELECT COUNT(*) FROM inventory WHERE equip<>0 AND nameid NOT IN (1201,2301) AND char_id IN (SELECT char_id FROM `char` WHERE account_id BETWEEN 2100001 AND 2199999)")
print('equipped items beyond the starter kit:', eq[0][0])

ev = api('/api/events?since=0')['events']
c = collections.Counter()
for e in ev:
    t = e['text']
    for k in ('bought ', 'restocked', 'heading to', 'died on', 'became', 'job', 'level'):
        if t.startswith(k) or (k == 'level' and 'level' in t and e['kind'] != 'chat'):
            c[k.strip()] += 1
print('recent events:', dict(c))
for e in [e for e in ev if e['text'].startswith('bought ')][-6:]:
    print('  ', e['bot'], e['text'])
names = sys.argv[1:] or ['Zendancer84', 'Echobunny', 'Zen.fox', 'xXRinfangXx', 'LilLilyblade', 'vex_x18']
for n in names:
    try:
        d = api('/api/bots/' + urllib.request.quote(n))
        print('%-12s %-22s %s' % (n, d.get('state', '')[:22], d.get('plan', '')))
    except Exception as ex:
        print(n, 'error', ex)
