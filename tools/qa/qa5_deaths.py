# deaths/min overall and party vs solo, per window: qa5_deaths.py START END
import json,sys,collections,urllib.request
s="2026-11-14T"+sys.argv[1]; t="2026-11-14T"+sys.argv[2]
mins=(int(sys.argv[2][:2])*60+int(sys.argv[2][3:5]))-(int(sys.argv[1][:2])*60+int(sys.argv[1][3:5]))
D=[];P=set()
for l in open("qa2_events.jsonl",encoding="utf-8"):
    e=json.loads(l)
    if s<=e["t"]<t:
        if e["kind"]=="death": D.append(e)
        if e["kind"]=="party": P.add(e["bot"]); [P.add(h) for h in e.get("heard_by") or []]
pd=[e for e in D if e["bot"] in P]
print(f"{sys.argv[1]}-{sys.argv[2]}Z deaths {len(D)} ({len(D)/mins:.2f}/min), party-member deaths {len(pd)} ({len(pd)/mins:.2f}/min)", collections.Counter(e["map"] for e in D).most_common(6))
