# usage: qa3_metrics.py START_UTC END_UTC  (e.g. 12:10 12:40, today)
import json,collections,re,sys,urllib.request
D="2026-11-14T"
s=D+sys.argv[1]; t=D+sys.argv[2]
E=[]; 
for l in open("qa2_events.jsonl",encoding="utf-8"):
    if l[7:9]!="20": pass
    e=json.loads(l)
    if s<=e["t"]<t: E.append(e)
hrs=max(1e-3,(int(sys.argv[2][:2])*60+int(sys.argv[2][3:5])-int(sys.argv[1][:2])*60-int(sys.argv[1][3:5]))/60)
print(f"window {sys.argv[1]}-{sys.argv[2]}Z {len(E)} ev, {hrs:.2f}h")
# party town runs by reason
pr=collections.Counter()
for e in E:
    m=re.match(r"party town run called: (.*?) \[",e["text"])
    if m: pr[re.sub(r"\d+","N",m.group(1))[:40]]+=1
try:
    P=json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/parties/detail",timeout=20).read()); npt=len(P)
except: P=[]; npt=0
tot=sum(pr.values()); print(f"party runs {tot} ({tot/max(1,npt)/hrs:.2f}/party-h, {npt} parties):",pr.most_common(6))
# kafra
kt=collections.defaultdict(list)
for e in E:
    m=re.match(r"Kafra teleport (\S+) -> (\S+)",e["text"])
    if m: kt[e["bot"]].append((e["t"][11:16],m.group(1).lower(),m.group(2).lower()))
n=sum(len(v) for v in kt.values())
hi=[(b,len(v)) for b,v in kt.items() if len(v)>=3]
aba=[]
for b,v in kt.items():
    for i in range(1,len(v)):
        if v[i][2]==v[i-1][1] and v[i][1]==v[i-1][2]: aba.append((b,v[i-1],v[i]))
print(f"kafra tp {n} by {len(kt)} bots; >=3 in window: {len(hi)} {sorted(hi,key=lambda x:-x[1])[:5]}; A->B->A {len(aba)} {aba[:3]}")
d=collections.Counter(e["map"] for e in E if e["kind"]=="death"); print("deaths",sum(d.values()),d.most_common(8))
wg=collections.Counter(re.sub(r"\d+","N",e["text"].split(":")[1] if ":" in e["text"] else e["text"])[:30] for e in E if e["kind"]=="wing"); print("wings",wg.most_common(5))
