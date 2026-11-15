import json,sys,collections
s="2026-11-14T"+sys.argv[1]; t="2026-11-14T"+sys.argv[2]
W=collections.defaultdict(list)
for l in open("qa2_events.jsonl",encoding="utf-8"):
    e=json.loads(l)
    if s<=e["t"]<t and e["kind"]=="wing": W[e["bot"]].append(e["t"][11:19])
def burst(L):  # max wings in any 2-min window
    import datetime
    ts=[int(x[:2])*3600+int(x[3:5])*60+int(x[6:8]) for x in L]; b=0
    for i in range(len(ts)):
        j=i
        while j<len(ts) and ts[j]-ts[i]<=120: j+=1
        b=max(b,j-i)
    return b
B=sorted(((burst(v),k,len(v)) for k,v in W.items()),reverse=True)
print(sys.argv[1],sys.argv[2],"wings",sum(len(v) for v in W.values()),"bots",len(W),"bots with >=6 wings in 2min:",sum(1 for b in B if b[0]>=6),B[:6])
