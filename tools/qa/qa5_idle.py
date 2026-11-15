# all maps: bots that stay within 30 cells with 0 kills over ~2.5 min, not in towns
import json,urllib.request,time,collections
H=collections.defaultdict(list)
TOWN=("prontera","payon","geffen","izlude","alberta","morocc","aldebaran","yuno","_in","prt_","mjolnir_0","x")
for i in range(6):
    for b in json.loads(urllib.request.urlopen('http://127.0.0.1:8090/api/bots',timeout=20).read()):
        if b.get('online') and b['map']: H[b['name']].append((b['map'],b['x'],b['y'],b['kills'],b['state'][:14]))
    time.sleep(28)
idle=collections.defaultdict(list); tot=collections.Counter()
for n,L in H.items():
    if len(L)<5 or len(set(p[0] for p in L))>1: continue
    m=L[0][0]; tot[m]+=1
    if m in ("prontera","payon","geffen","izlude","alberta","morocc","aldebaran","yuno") or "_in" in m: continue
    xs=[p[1] for p in L]; ys=[p[2] for p in L]
    if max(max(xs)-min(xs),max(ys)-min(ys))<30 and L[-1][3]==L[0][3]: idle[m].append((n,xs[-1],ys[-1],L[-1][4]))
print(time.strftime("%H:%M"),"idle (0 kills, <30 cells, 2.5min) by map:")
for m,L in sorted(idle.items(),key=lambda x:-len(x[1]))[:10]:
    print(f"  {m}: {len(L)}/{tot[m]}", L[:4])
