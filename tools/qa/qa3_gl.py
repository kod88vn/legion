import json,urllib.request,time,datetime
while True:
    try:
        G=json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/guild-leaders",timeout=15).read())
        now=datetime.datetime.utcnow().strftime("%H:%M:%S")
        with open("qa3_gl_log.jsonl","a") as f:
            for g in G: f.write(json.dumps({"t":now,"g":g["guild"],"n":g["name"],"on":g["online"],"s":g["state"][:50],"m":g["map"],"pm":g.get("party_map")})+"\n")
    except Exception: pass
    time.sleep(30)
