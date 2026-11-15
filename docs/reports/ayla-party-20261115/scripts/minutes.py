import csv, json, collections, datetime as dt
names = {150010:'Ayla',172745:'Priest Zendancer84',171589:'Wizard Zen.fox',172337:'Hunter xXRinfangXx',171222:'Crusader vex_x18'}
rows = list(csv.reader(open('pick.tsv'), delimiter='\t'))[1:]
TOWNS = {'payon','payon_in01','payon_in02','prontera','prt_in','geffen','geffen_in','aldebaran','aldeba_in','izlude','yuno','pay_arche'}
perm = json.load(open('permin.json'))
amap = {}
for r in perm:
    if r['map']: amap[dt.datetime.strptime('2026-' + r['t'], '%Y-%m-%d %H:%M')] = r['map']
A, B = dt.datetime(2026,11,14,20,43), dt.datetime(2026,11,15,9,7)
total = (B - A).total_seconds() / 60
loc = collections.defaultdict(dict)  # char -> minute -> Counter(map)
for t, cid, typ, nid, amt, mp in rows:
    cid = int(cid)
    if cid not in names or typ not in ('P','C','S','R'): continue
    ts = dt.datetime.strptime(t, '%Y-%m-%d %H:%M:%S') - dt.timedelta(hours=5)
    if not (A <= ts < B): continue
    k = ts.replace(second=0)
    loc[cid].setdefault(k, collections.Counter())[mp] += 1
print('window %s - %s, %d minutes; Ayla map known in %d minutes' % (A, B, total, len(amap)))
for cid, nm in names.items():
    mins = loc[cid]
    same = diff = town = 0; seq = []
    for k in sorted(mins):
        mp = mins[k].most_common(1)[0][0]
        seq.append(mp)
        if mp in TOWNS: town += 1; continue
        am = amap.get(k)
        if cid == 150010 or am == mp: same += 1
        elif am is not None: diff += 1
    changes = sum(1 for a, b in zip(seq, seq[1:]) if a != b)
    print('%-20s minutes with activity %3d (%2.0f%% of window): on her map %3d, on another hunting map %3d, in town %3d; map switches between active minutes %d' % (nm, len(mins), 100*len(mins)/total, same, diff, town, changes))
