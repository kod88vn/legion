# bow users: arrows used and kills over 90 s; 0-kill share among those fighting at both ends
import json,urllib.request,urllib.parse,time
from concurrent.futures import ThreadPoolExecutor
A="http://127.0.0.1:8090/api"
def g(p):
    try: return json.loads(urllib.request.urlopen(A+p,timeout=20).read())
    except: return None
B=[b["name"] for b in g("/bots") if b.get("online") and b["class"] in ("Archer","Hunter","Bard","Dancer")]
snap=lambda: {d["persona"]["name"]:d for d in ThreadPoolExecutor(20).map(lambda n:g("/bots/"+urllib.parse.quote(n).replace(".","%2E")),B) if d}
F=("fighting","assisting","engaging")
arr=lambda d: sum(i["Count"] for i in d.get("inventory") or [] if 1750<=i["ItemID"]<1800)
bow=lambda d: any(i.get("Equipped") and 1700<=i["ItemID"]<1750 for i in d.get("inventory") or [])
a=snap(); time.sleep(90); z=snap()
fight=[];used=0;zero=[]
for n,x in a.items():
    y=z.get(n)
    if not y or not bow(x) or not bow(y): continue
    if x["state"].startswith(F) and y["state"].startswith(F+("looting",)):
        fight.append(n); k=y["kills"]-x["kills"]; u=arr(x)-arr(y)
        if u>0: used+=1
        if k==0: zero.append((n,x["base_level"],u,y["state"][:22]))
print(time.strftime("%H:%M"),"bow users fighting",len(fight),"used arrows",used,"zero-kill",len(zero),f"({round(100*len(zero)/max(1,len(fight)))}%)")
for r in zero[:10]: print("  ",r)
