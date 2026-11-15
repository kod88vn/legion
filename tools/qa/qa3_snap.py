import json,urllib.request,collections,re
A="http://127.0.0.1:8090/api"
def g(p): return json.loads(urllib.request.urlopen(A+p,timeout=30).read())
BEG={"prt_fild01","prt_fild05","prt_fild06","prt_fild07","prt_fild08","pay_fild01","pay_fild02","pay_fild03","pay_fild06","gef_fild07"}
P=g("/parties/detail"); strong=[]; coll=[]; ls=collections.Counter()
for p in P:
    M=[m for m in p["members"] if m.get("online")]
    if len(M)>=2 and all(m["level"]>=45 for m in M) and p.get("map") in BEG: strong.append((p["name"],p["map"],min(m["level"] for m in M)))
    L=next((m for m in M if m.get("leader")),None)
    if not L or L.get("human"): continue
    st=L.get("state") or ""; ls[re.sub(r"[A-Z][\w. ]+","X",st)[:25]]+=1
    if any(k in st for k in ("collect","regroup","going to","fetch")):
        far=[max(abs(m["x"]-L["x"]),abs(m["y"]-L["y"])) for m in M if m is not L and m["map"]==L["map"]]
        off=[m for m in M if m["map"]!=L["map"]]
        if far and max(far)<=20 and not off: coll.append((p["name"],st[:40],far))
print(f"strong(all lv45+) parties on beginner fields: {len(strong)} {strong[:6]}")
print(f"leaders collecting with all members <=20: {len(coll)} {coll[:4]}")
print("leader states:",ls.most_common(10))
B=[b for b in g("/bots") if b.get("online")]
sh=[(b["name"],b["map"]) for b in B if "setting up shop" in b["state"]]
print("setting up shop:",len(sh),sh[:6])
