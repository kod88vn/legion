# background: append all new events (deduped by t+bot+text) to qa2_events.jsonl, polling every 45 s
import json,urllib.request,time,os
SD=os.path.dirname(os.path.abspath(__file__)); OUT=os.path.join(SD,"qa2_events.jsonl")
seen=set(); last=""
while True:
    try:
        E=json.loads(urllib.request.urlopen("http://127.0.0.1:8090/api/events?limit=5000",timeout=20).read())["events"]
        with open(OUT,"a",encoding="utf-8") as f:
            for e in E:
                if e["kind"]=="chat": continue
                k=(e["t"],e.get("bot"),e["text"])
                if k in seen: continue
                seen.add(k); f.write(json.dumps(e)+"\n")
        if len(seen)>400000: seen=set(list(seen)[-100000:])
    except Exception as ex:
        pass
    time.sleep(45)
