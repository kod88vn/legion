import json,urllib.request,urllib.parse,collections
from concurrent.futures import ThreadPoolExecutor
A="http://127.0.0.1:8090/api"
def g(p):
    try: return json.loads(urllib.request.urlopen(A+p,timeout=20).read())
    except: return None
B=[b for b in g("/bots") if b.get("online") and b["class"] in("Mage","Wizard")]
D=[d for d in ThreadPoolExecutor(24).map(lambda b:g("/bots/"+urllib.parse.quote(b["name"])),B) if d and d.get("skills") is not None]
AOE={"17":"FireBall","21":"Thunderstorm","18":"FireWall","83":"Meteor","85":"LoV","89":"StormGust","91":"HeavensDrive"}
r=collections.Counter(); ex=[]
for d in D:
    lv=d["base_level"]; sk=d["skills"]; a=[AOE[k] for k in sk if k in AOE]
    band="Mage<20" if d["class"]=="Mage" and lv<20 else d["class"]
    r[(band,"aoe" if a else "none")]+=1
    if not a and lv>=25: ex.append((d["persona"]["name"] if d.get("persona") else "?",d["class"],lv,d.get("job_level"),sorted(sk,key=int)))
print(sorted(r.items())); print("lv25+ without AoE:",len(ex),ex[:6])
