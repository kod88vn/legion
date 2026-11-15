import json,urllib.request,urllib.parse,random,collections,re,time
B=json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/bots"))
on=[b for b in B if b.get('online') and (b.get('level') or 0)>=30]
random.seed(1); S=random.sample(on,min(60,len(on)))
reds=[];zen=[];wpn=[]
for b in S:
    try: d=json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/bots/"+urllib.parse.quote(b['name'])))
    except Exception: continue
    inv=d.get('inventory') or []
    r=0
    for it in inv:
        if isinstance(it,dict) and it.get('ItemID') in (501,502,503,504): r+=it.get('Count',0)
    w=sum(it.get('Count',0) for it in inv if isinstance(it,dict) and not it.get('Equipped') and (1100<=it.get('ItemID',0)<2000 or 13000<=it.get('ItemID',0)<14000))
    wpn.append(w)
    reds.append(r); zen.append(d.get('zeny') or 0)
reds.sort(); print(time.strftime('%H:%M'),"lv30+ sample",len(reds),"HP pots median",reds[len(reds)//2],"p25",reds[len(reds)//4],"zero",sum(1 for x in reds if x==0),"<15",sum(1 for x in reds if x<15))
E=json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/events?n=20000"))
E=E if isinstance(E,list) else E.get('events',[])
ts=[e['t'] for e in E if isinstance(e,dict) and 'party town run called: out of potions' in (e.get('text') or '')]
wpn.sort(); print("spare weapons per bot: median",wpn[len(wpn)//2],"p90",wpn[int(len(wpn)*.9)],"max",wpn[-1],"total",sum(wpn))
print("out-of-potion run calls in event window",len(ts), (E[0]['t'][11:19], E[-1]['t'][11:19]) if E else '')
