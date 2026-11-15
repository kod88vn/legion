import json,urllib.request,sys,urllib.parse
for n in ["LilLilyblade","Zen.fox","vex_x18"]:
    try:
        d=json.load(urllib.request.urlopen('http://127.0.0.1:8091/api/memory/'+urllib.parse.quote(n),timeout=10))
        s=json.dumps(d)
        print(n, s[:1500]); print()
    except Exception as e: print(n,e)
