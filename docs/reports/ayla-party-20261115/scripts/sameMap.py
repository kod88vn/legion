import csv, json, collections, datetime as dt
names = {172745:'Zendancer84(Priest)',171589:'Zen.fox(Wizard)',172337:'xXRinfangXx(Hunter)',171222:'vex_x18(Crusader)',150010:'Ayla',172407:'Silversage13(Rogue)',171129:'Echo.0783(Bard)',172064:'LilLilyblade(Monk)',173870:'Echobunny(Knight)',170917:'Silent rogue'}
rows = list(csv.reader(open('pick.tsv'), delimiter='\t'))[1:]
ev = collections.defaultdict(list)  # char -> [(local minute dt, map, type, nameid)]
for t, cid, typ, nid, amt, mp in rows:
    ts = dt.datetime.strptime(t, '%Y-%m-%d %H:%M:%S') - dt.timedelta(hours=5)
    ev[int(cid)].append((ts, mp, typ, int(nid), int(amt)))
# Ayla per-minute map from her log
perm = json.load(open('permin.json'))
amap = {}
for r in perm:
    if r['map']:
        amap[dt.datetime.strptime('2026-' + r['t'], '%Y-%m-%d %H:%M')] = r['map']
# Ayla map from picklog for minutes before log start
for ts, mp, *_ in ev[150010]:
    k = ts.replace(second=0)
    amap.setdefault(k, mp)
def ayla_at(ts):
    k = ts.replace(second=0)
    for d in (0, -1, 1, -2, 2):
        kk = k + dt.timedelta(minutes=d)
        if kk in amap: return amap[kk]
    return None
TOWN = {'payon','prontera','geffen','aldebaran','izlude','yuno','payon_in01','payon_in02','prt_in','geffen_in','alberta','morocc'}
periods = [('A2 11-14 12:23-20:00', dt.datetime(2026,11,14,12,23), dt.datetime(2026,11,14,20,0)),
           ('B 11-14 20:00-11-15 07:00', dt.datetime(2026,11,14,20,0), dt.datetime(2026,11,15,7,0)),
           ('C 11-15 07:00-09:08', dt.datetime(2026,11,15,7,0), dt.datetime(2026,11,15,9,8))]
out = {}
for pname, a, b in periods:
    print('==', pname)
    for cid, nm in names.items():
        if cid == 150010: continue
        same = diff = unk = 0; pick_same = pick_diff = 0; town = 0
        maps = collections.Counter(); arrows = 0; pots = 0; wings = 0; bwings = 0; shop = 0; pickups = 0
        for ts, mp, typ, nid, amt in ev[cid]:
            if not (a <= ts < b): continue
            if typ == 'C' and nid == 1750: arrows += 1
            if typ == 'C' and nid in (501,502,503,504,569,11500,507,508,511,512): pots += 1
            if typ == 'C' and nid == 601: wings += 1
            if typ == 'C' and nid == 602: bwings += 1
            if typ == 'S': shop += 1
            if typ == 'P': pickups += 1
            if typ not in ('P','C'): continue
            maps[mp] += 1
            am = ayla_at(ts)
            if am is None: unk += 1
            elif am == mp: same += 1
            else: diff += 1
        n = same + diff
        if n + unk == 0: continue
        print('%-22s events=%5d same=%4.0f%% (n=%d, unk=%d) arrows=%d pots=%d flywing=%d bwing=%d shop=%d pick=%d top=%s' % (nm, n+unk, 100*same/max(n,1), n, unk, arrows, pots, wings, bwings, shop, pickups, maps.most_common(5)))
