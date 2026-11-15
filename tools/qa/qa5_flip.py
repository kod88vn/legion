# goal/travel flips per bot in window: count travel -> X events, A->B->A patterns
import json,re,sys,collections
s="2026-11-14T"+sys.argv[1]; t="2026-11-14T"+sys.argv[2]
tr=collections.defaultdict(list)
for l in open("qa2_events.jsonl",encoding="utf-8"):
    if l[8:10]!="20": pass
    try: e=json.loads(l)
    except: continue
    if not (s<=e["t"]<t): continue
    m=re.search(r"(?:heading to|travel ->|Kafra teleport \S+ ->) (\w+)",e["text"])
    if m: 
        d=m.group(1).lower()
        L=tr[e["bot"]]
        if not L or L[-1][1]!=d: L.append((e["t"][11:16],d,e["map"]))
ab=[]
for b,L in tr.items():
    n=sum(1 for i in range(2,len(L)) if L[i][1]==L[i-2][1] and L[i][1]!=L[i-1][1])
    if n>=2: ab.append((n,b,[x[1] for x in L[-8:]]))
ab.sort(reverse=True)
print("bots with >=2 A-B-A destination flips:",len(ab))
for x in ab[:12]: print(x)
