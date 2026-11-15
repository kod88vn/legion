import json, sys, time, urllib.request, collections
n = int(sys.argv[1]); gap = float(sys.argv[2])
tot = collections.Counter(); rest = collections.Counter(); fight = collections.Counter()
for i in range(n):
    bots = json.load(urllib.request.urlopen("http://127.0.0.1:8090/api/bots"))
    for b in bots:
        if not b.get("online") or b.get("level", 0) < 1: continue
        c = b["class"]; s = b.get("state", "")
        tot[c] += 1
        if s.startswith("resting"): rest[c] += 1
        if s.startswith("fighting") or s.startswith("engaging"): fight[c] += 1
    if i < n - 1: time.sleep(gap)
for c in sorted(tot, key=lambda c: -tot[c]):
    if tot[c] >= 20:
        print(f"{c:18s} n={tot[c]:5d} rest={100*rest[c]/tot[c]:5.1f}% fight={100*fight[c]/tot[c]:5.1f}%")
