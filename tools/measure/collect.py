import json,urllib.request
bots=json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/bots"))
by={b["name"]:b for b in bots}
lead=0;bad=0
for b in bots:
    s=b.get("state","")
    if b.get("online") and s.startswith("collecting "):
        t=by.get(s[len("collecting "):])
        if not t: continue
        lead+=1
        d=max(abs(b["x"]-t["x"]),abs(b["y"]-t["y"])) if t["map"]==b["map"] else 999
        busy=t["state"].startswith("fight")
        flag = d<=20 or busy
        bad+=flag
        print(("BAD " if flag else "ok  ")+f'{b["name"]} -> {t["name"]} d={d} tstate={t["state"]}')
w=[b for b in bots if b.get("online") and "coming to get me" in b.get("state","")]
print("leaders collecting",lead,"bad",bad)
for b in w:
    g=b["goal"].get("who") or ""
    print("WAIT",b["name"],b["state"])
