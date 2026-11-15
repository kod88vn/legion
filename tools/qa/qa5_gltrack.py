import json,urllib.request,time,datetime
W=("Blazemage pt","TheShadowdragon pt","TheShadowsenpai pt","Starsoul pt")
out=open("qa5_gltrack.txt","a")
for i in range(30):
    try:
        P=json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/parties/detail",timeout=20).read())
        now=datetime.datetime.utcnow().strftime("%H:%M:%S")
        for p in P:
            if p["name"] in W or any(m.get("leader") and m["name"] in ("Blaze.mage","TheShadowdragon","TheShadowsenpai","Star.soul") for m in p["members"]):
                L=next((m for m in p["members"] if m.get("leader")),p["members"][0])
                out.write(f"{now} {p['name']} pmap={p.get('map')} lead={L['name']}@{L['map']} '{L.get('state','')[:40]}' maps={[m['map'] for m in p['members']]} lv={[m.get('level') for m in p['members']]}\n")
        out.flush()
    except Exception as e: out.write("err %s\n"%e)
    time.sleep(60)
