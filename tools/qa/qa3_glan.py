import json,collections,sys
since=sys.argv[1] if len(sys.argv)>1 else "00:00"
R=[json.loads(l) for l in open("qa3_gl_log.jsonl") if json.loads(l)["t"]>=since]
by=collections.defaultdict(list)
for r in R: by[(r["g"],r["n"])].append(r)
for (g,n),L in sorted(by.items()):
    # longest run of travel/party-move states
    run=0;best=0;bs=""
    for r in L:
        if any(k in r["s"] for k in ("travel","heading","party:","town trip","going")): run+=1
        else: run=0
        if run>best: best=run;bs=r["s"]
    seq=[L[0]["m"]]+[r["m"] for i,r in enumerate(L[1:]) if r["m"]!=L[i]["m"]]
    bounce=sum(1 for i in range(2,len(seq)) if seq[i]==seq[i-2])
    off=sum(1 for r in L if not r["on"])
    flag=("TRAVEL %dmin '%s'"%(best//2,bs) if best>=20 else "")+(" BOUNCE %d %s"%(bounce,seq[-6:]) if bounce>=2 else "")+(" OFFLINE %d"%off if off else "")
    print(g,n,len(L),"samples maps",len(seq),"|",flag or "ok", "| now",L[-1]["s"][:30],L[-1]["m"])
