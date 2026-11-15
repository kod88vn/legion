import json,collections
E=collections.defaultdict(list); D=[]
for l in open("qa2_events.jsonl",encoding="utf-8"):
    e=json.loads(l)
    if e["t"]<"2026-11-14T19:50": continue
    E[e["bot"]].append(e)
    if e["kind"]=="death" and e["map"]=="pay_fild03": D.append(e)
print("pay_fild03 deaths since 19:50Z:",len(D))
seen=set()
for d in D:
    k=(d["bot"],d["t"][:19])
    dup=sum(1 for x in D if x["bot"]==d["bot"] and abs((int(x["t"][14:16])*60+float(x["t"][17:23]))-(int(d["t"][14:16])*60+float(d["t"][17:23])))<3)
    if k in seen: continue
    seen.add(k)
    prev=[x for x in E[d["bot"]] if x["t"]<d["t"] and x["kind"]!="said"][-6:]
    print("\n##",d["t"][11:19],d["bot"],"dups<3s:",dup)
    for p in prev: print("   ",p["t"][11:19],p["map"],p["kind"],p["text"][:110])
