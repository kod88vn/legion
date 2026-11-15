import json, collections, sys
d = json.load(open(sys.argv[1]))
MEM = ['Zendancer84','Zen.fox','xXRinfangXx','vex_x18','Silversage13','Echo.0783','LilLilyblade','Echobunny','Silent rogue']
hours = collections.OrderedDict()
for r in d:
    h = r['t'][:8]
    hours.setdefault(h, []).append(r)
print('hour  mins exp  ev  ownk  evPerMin ownPerMin  ' + ' '.join(m[:6] for m in MEM) + '  maps')
tot = collections.Counter(); totmin = 0
for h, rs in hours.items():
    n = len(rs); totmin += n
    exp = sum(r['exp'] for r in rs); ev = sum(r['ev'] for r in rs); k = sum(r['kills'] for r in rs)
    seen = collections.Counter()
    for r in rs:
        for m in r['seen']: seen[m] += 1
    for m in MEM: tot[m] += seen[m]
    maps = collections.Counter(r['map'] for r in rs)
    print(h, n, exp, ev, k, round(ev/n,2), round(k/n,2), ' '.join('%5.0f%%' % (100*seen[m]/n) for m in MEM), dict(maps.most_common(4)))
print('total minutes', totmin, {m: round(100*tot[m]/totmin) for m in MEM})
# exp events: own kill vs not
own = sum(r['kills'] for r in d); ev = sum(r['ev'] for r in d)
print('exp events', ev, 'own kills', own, 'share events not own', ev-own)
# seen-count distribution
dist = collections.Counter(len([m for m in r['seen'] if m in MEM]) for r in d)
print('members seen per minute', sorted(dist.items()))
