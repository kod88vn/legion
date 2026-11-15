import re, sys, json, collections, datetime as dt
src = sys.argv[1]
MEM = ['Zendancer84','Zen.fox','xXRinfangXx','vex_x18','Silversage13','Echo.0783','LilLilyblade','Echobunny','Silent rogue']
title = re.compile(r'Ayla:\s+B(\d+) \(([\d.]+)%\).*?: (\d+),(\d+) (\S+) \[([^\]]*)\]')
gain = re.compile(r'You have gained (\d+)/(\d+)')
playerre = re.compile(r'Player (' + '|'.join(re.escape(m) for m in MEM) + r') \(\d+\) (is casting|uses) ([A-Za-z\' ]+?) (?:\(Lv: \d+\) )?on (you|herself|himself|Player (\S+)|Monster|location)')
anyp = re.compile(r'Player (' + '|'.join(re.escape(m) for m in MEM) + r') \(')
partyl = re.compile(r'\[Party\] (\S+(?: rogue)?) : (.*)')
mins = collections.OrderedDict()
def M(k):
    if k not in mins:
        mins[k] = dict(map=collections.Counter(), st=collections.Counter(), exp=0, ev=0, kills=0, seen=set(), lv=None, sk=collections.Counter(), plines=collections.Counter(), dead=0, x=None, y=None)
    return mins[k]
for line in open(src, encoding='utf-8', errors='replace', newline=chr(10)):
    if len(line) < 31 or not line[:4].isdigit() or line[10] != 'T': continue
    ts = dt.datetime.strptime(line[:19], '%Y-%m-%dT%H:%M:%S') - dt.timedelta(hours=5)
    k = ts.strftime('%m-%d %H:%M')
    m = M(k)
    body = line[31:]
    for t in title.finditer(body):
        m['map'][t.group(5)] += 1; m['st'][t.group(6) or 'none'] += 1; m['lv'] = t.group(1); m['x']=int(t.group(3)); m['y']=int(t.group(4))
    g = gain.search(body)
    if g and 'Party]' not in body:
        m['exp'] += int(g.group(1)); m['ev'] += 1
    if 'Target Monster' in body and 'died' in body: m['kills'] += 1
    if 'You have died' in body or 'You are dead' in body: m['dead'] += 1
    if '[Party]' in body:
        p = partyl.search(body)
        if p: m['plines'][p.group(1)] += 1
        continue
    if '[dist=unknown]' in body: continue
    for p in anyp.finditer(body): m['seen'].add(p.group(1))
    for p in playerre.finditer(body):
        if p.group(2) == 'uses' or p.group(3) in ('Storm Gust',):
            tgt = p.group(4)
            if tgt.startswith('Player'): tgt = p.group(5)
            elif tgt in ('herself','himself'): tgt = 'self'
            elif tgt == 'you': tgt = 'Ayla'
            if p.group(2)=='uses': m['sk'][(p.group(1), p.group(3), tgt)] += 1
out = []
for k, m in mins.items():
    out.append(dict(t=k, map=(m['map'].most_common(1)[0][0] if m['map'] else ''), st=dict(m['st']), exp=m['exp'], ev=m['ev'], kills=m['kills'], seen=sorted(m['seen']), lv=m['lv'], dead=m['dead'], sk=[[a,b,c,n] for (a,b,c),n in m['sk'].items()], plines=dict(m['plines'])))
json.dump(out, open(sys.argv[2], 'w'))
print(len(out))
