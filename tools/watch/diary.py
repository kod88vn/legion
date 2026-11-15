import json,urllib.request,sys,urllib.parse,os
since=os.environ.get('SINCE','19:40')
names=os.environ.get('NAMES',"Zendancer84,Echobunny,Zen.fox,xXRinfangXx,LilLilyblade,vex_x18").split(',')
for n in names:
    try:
        d=json.load(urllib.request.urlopen('http://127.0.0.1:8091/api/memory/'+urllib.parse.quote(n),timeout=10))
        for e in d.get('diary',[]):
            t=e['t'][11:19]
            if t>=since: print(t,e['text'][:200])
    except Exception as e: print(n,e)
