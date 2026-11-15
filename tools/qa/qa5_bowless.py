import json,urllib.request,urllib.parse
from concurrent.futures import ThreadPoolExecutor
A="http://127.0.0.1:8090/api"
def g(p):
    try: return json.loads(urllib.request.urlopen(A+p,timeout=20).read())
    except: return None
B=[b["name"] for b in g("/bots") if b.get("online") and b["class"] in ("Archer","Hunter","Bard","Dancer")]
D=[d for d in ThreadPoolExecutor(20).map(lambda n:g("/bots/"+urllib.parse.quote(n).replace(".","%2E")),B) if d]
nob=[];noar=[]
for d in D:
    inv=d.get("inventory") or []
    eq=[i["ItemID"] for i in inv if i.get("Equipped")]
    hasbow=any(1700<=x<1750 for x in eq); inst=any(1900<=x<2000 for x in eq)
    arrows=sum(i["Count"] for i in inv if 1750<=i["ItemID"]<1800)
    if not hasbow and not inst: nob.append((d["persona"]["name"],d["class"],d["base_level"],d["zeny"],eq[:3],d["state"][:30]))
    if hasbow and arrows==0: noar.append((d["persona"]["name"],d["base_level"],d["zeny"],d["state"][:30]))
print("bow line online",len(D),"no bow/instrument",len(nob),"afford(>=1500z)",sum(1 for x in nob if x[3]>=1500)); [print("  ",x) for x in nob[:10]]
print("bow but 0 arrows",len(noar),noar[:6])
