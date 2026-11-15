import json,sys,time,urllib.request,collections,re
dur=int(sys.argv[1]) if len(sys.argv)>1 else 120
def get():
    return json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/parties/detail",timeout=20))
norm=lambda s: re.sub(r"(vs|off|with|assisting|tanking|pulling|taking|provoking|defending|fighting|following|catching up with|near|for) .*",r"\1 *",s or "")
states={'leadtank':collections.Counter(),'membertank':collections.Counter(),'ranged':collections.Counter(),'support':collections.Counter()}
k0={};k1={};dists=collections.defaultdict(list)
t0=time.time();n=0
while time.time()-t0<dur:
    try: d=get()
    except Exception as e: print(e); time.sleep(3); continue
    n+=1
    for p in d:
        ms=p.get('members',[]); lead=[m for m in ms if m.get('leader')]
        if not lead or p.get('online',0)<3: continue
        L=lead[0]
        for m in ms:
            if not m.get('online') or m.get('human'): continue
            r=m.get('role')
            key = ('leadtank' if m.get('leader') else 'membertank') if r=='tank' else r
            if key not in states: continue
            states[key][norm(m.get('state'))]+=1
            if r=='tank':
                k0.setdefault(m['name'],m.get('kills',0)); k1[m['name']]=m.get('kills',0)
                if not m.get('leader') and m.get('map')==L.get('map'):
                    dists[m['name']].append(max(abs(m['x']-L['x']),abs(m['y']-L['y'])))
    time.sleep(4)
print("samples",n)
for k,c in states.items():
    tot=sum(c.values()); print("==",k,tot)
    for s,v in c.most_common(14): print("  %5.1f%% %s"%(100*v/tot,s))
kd=[k1[x]-k0[x] for x in k1]; print("tanks",len(kd),"kills total",sum(kd),"per tank per min %.2f"%(sum(kd)/max(1,len(kd))/(dur/60)))
alld=[x for v in dists.values() for x in v]
if alld: print("member tank dist to leader avg %.1f max %d  >12: %.1f%%"%(sum(alld)/len(alld),max(alld),100*sum(1 for x in alld if x>12)/len(alld)))
