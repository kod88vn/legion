import json,urllib.request,time,math
def g(u):
    try: return json.load(urllib.request.urlopen(u,timeout=10))
    except Exception as e: return {"err":str(e)}
hp=g('http://127.0.0.1:8091/api/human-party?human=Ayla')
s=json.load(open('/opt/openkore/logs/status.json')) if False else None
h=hp.get('human_last_seen',{})
print(time.strftime('%H:%M:%SZ',time.gmtime()),"Ayla",h.get('map'),h.get('x'),h.get('y'),"ckpt",hp.get('checkpoint'))
for m in hp.get('members',[]):
    d='-'
    if m.get('map')==h.get('map'): d=int(max(abs(m['x']-h['x']),abs(m['y']-h['y'])))
    print(f"  {m['name'][:12]:12} {m['class'][:9]:9} L{m['level']} {m['map']}({m['x']},{m['y']}) d={d} hp={m['hp']} sp={m['sp']} w={m.get('weight')} save={m.get('saved_town')} goal={m.get('goal',{}).get('mode')}/{m.get('goal',{}).get('map')} | {m.get('state')}")
