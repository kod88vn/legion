import json,urllib.request,collections
B=[b for b in json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/bots",timeout=20).read()) if b.get("online") and b["map"]]
m=collections.defaultdict(lambda:[0,0])
for b in B:
    m[b["map"]][0]+=1
    if b["state"].startswith("roaming"): m[b["map"]][1]+=1
W=["pay_fild07","gef_fild02","pay_fild09","moc_fild01","mjolnir_02"]
print("watched:",[(k,m[k][0],m[k][1]) for k in W],"| top:",sorted(((k,v[0],v[1]) for k,v in m.items()),key=lambda x:-x[1])[:6],"| roaming",sum(v[1] for v in m.values()),"/",len(B))
