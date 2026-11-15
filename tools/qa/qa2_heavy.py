import json,urllib.request,urllib.parse,time,collections
from concurrent.futures import ThreadPoolExecutor
A="http://127.0.0.1:8090/api"
def g(p):
    try: return json.loads(urllib.request.urlopen(A+p,timeout=20).read())
    except: return None
B=[b for b in g("/bots") if b.get("online")]
D=[d for d in ThreadPoolExecutor(24).map(lambda b:g("/bots/"+urllib.parse.quote(b["name"]).replace(".","%2E")),B) if d]
H=[d["persona"]["name"] for d in D if d.get("max_weight") and d["weight"]/d["max_weight"]>=0.5]
print("heavy now",len(H))
S=collections.defaultdict(list)
for i in range(5):
    for n,d in zip(H,ThreadPoolExecutor(16).map(lambda n:g("/bots/"+urllib.parse.quote(n).replace(".","%2E")),H)):
        if d: S[n].append((round(100*d["weight"]/d["max_weight"]),d["state"][:28],d["map"],d.get("party") is not None))
    time.sleep(50)
for n,L in S.items(): print(n,L[0][0],"->",L[-1][0],"|",L[0][1],"->",L[-1][1],L[-1][2])
