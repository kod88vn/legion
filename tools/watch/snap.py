import json,urllib.request,time
def g(u):
    try: return json.load(urllib.request.urlopen(u,timeout=10))
    except Exception as e: return {"err":str(e)}
hp=g('http://127.0.0.1:8091/api/human-party?human=Ayla')
h=hp.get('human_last_seen',{})
out=[]
for m in hp.get('members',[]):
    d='-'
    if m.get('map')==h.get('map'): d=str(int(max(abs(m['x']-h['x']),abs(m['y']-h['y']))))
    out.append(f"{m['name'][:6]}:{m['map']}/{d}/{m['hp'].split('/')[0]}")
print(time.strftime('%H:%M:%SZ',time.gmtime()),"SNAP Ayla",h.get('map'),h.get('x'),h.get('y'),"|"," ".join(out),flush=True)
