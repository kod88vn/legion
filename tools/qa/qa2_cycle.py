import urllib.parse
import json, sys, time, urllib.request, os, random, collections, threading, math
API = "http://127.0.0.1:8090/api"
SD = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(SD, "qa2_state.json")
TICKS = int(sys.argv[1]) if len(sys.argv) > 1 else 28
DT = 2.8
EXTRA = sys.argv[2].split(",") if len(sys.argv) > 2 and sys.argv[2] else []

def get(p):
    try:
        with urllib.request.urlopen(API + p, timeout=8) as r:
            return json.loads(r.read())
    except Exception as e:
        return None

st = {"seen": []}
if os.path.exists(STATE):
    st = json.load(open(STATE))
seen = set(st["seen"][-400:])

bots = get("/bots")
if not bots:
    print("API DOWN"); sys.exit(1)
on = [b for b in bots if b.get("online")]
bycls = collections.defaultdict(list)
for b in on:
    bycls[b["class"]].append(b)
pick = []
for c, lst in sorted(bycls.items()):
    random.shuffle(lst)
    # prefer unseen; for big classes pick 2 (one of them in an interesting state)
    n = 2 if len(lst) > 50 else 1
    cand = [b for b in lst if b["name"] not in seen] or lst
    chosen = cand[:n]
    # bias: one fighting/roaming if possible
    for b in cand:
        if b not in chosen and any(k in b["state"] for k in ("roaming", "regroup", "town", "rest", "no ")):
            chosen.append(b); break
    pick += chosen
pick += [{"name": n} for n in EXTRA]
names = [b["name"] for b in pick]
st["seen"] = (st["seen"] + names)[-400:]
json.dump(st, open(STATE, "w"))

POTS = {501, 502, 503, 504, 505, 545, 546, 547}
samples = {n: [] for n in names}
party_s = []

def poll(n):
    d = get("/bots/" + urllib.parse.quote(n).replace(".", "%2E"))
    if d:
        d["_t"] = time.time()
        samples[n].append(d)

t0 = time.time()
for i in range(TICKS):
    ths = [threading.Thread(target=poll, args=(n,)) for n in names]
    for t in ths: t.start()
    if i % 3 == 0:
        p = get("/parties/detail")
        if p: party_s.append((time.time(), p))
    for t in ths: t.join()
    dt = DT - (time.time() - t0 - i * DT)
    if dt > 0: time.sleep(dt)

def short(s): return (s or "")[:70]
out = []
for n in names:
    S = samples[n]
    if not S:
        out.append(f"## {n}: no data"); continue
    a, z = S[0], S[-1]
    cls = a.get("class"); lv = a.get("base_level")
    home = (a.get("persona") or {}).get("home_town")
    flags = []
    states = collections.Counter(short(s.get("state")) for s in S)
    pos = [(s.get("map"), s.get("x"), s.get("y")) for s in S]
    maps = [p[0] for p in pos]
    kills = (z.get("kills") or 0) - (a.get("kills") or 0)
    deaths = (z.get("deaths") or 0) - (a.get("deaths") or 0)
    hps = [s.get("hp", 0) for s in S]
    inv = z.get("inventory") or []
    weap = [i["ItemID"] for i in inv if i.get("Equipped") and (i.get("Loc", 0) & 2)]
    pots = sum(i["Count"] for i in inv if i["ItemID"] in POTS)
    wpct = 100 * (z.get("weight") or 0) / max(1, z.get("max_weight") or 1)
    # stuck: longest run same position
    run = best = 1; bests = ""
    for k in range(1, len(pos)):
        if pos[k] == pos[k-1]:
            run += 1
            if run > best: best = run; bests = short(S[k].get("state"))
        else: run = 1
    stuck_s = best * DT
    # target in range but nothing happening
    tgt_idle = 0; mx = 0
    for k in range(1, len(S)):
        s = S[k]
        if s.get("target") and 0 <= (s.get("target_dist") or -1) <= 2 and pos[k] == pos[k-1] and hps[k] == hps[k-1] and s.get("kills") == S[k-1].get("kills"):
            tgt_idle += 1; mx = max(mx, tgt_idle)
        else: tgt_idle = 0
    # sit/stand flicker: state transitions count
    trans = sum(1 for k in range(1, len(S)) if short(S[k].get("state")) != short(S[k-1].get("state")))
    mapchg = sum(1 for k in range(1, len(maps)) if maps[k] != maps[k-1])
    pingpong = len(maps) > 2 and mapchg >= 3
    moving = len(set(pos))
    mobsn = max((len(s.get("mobs") or []) for s in S), default=0)
    if stuck_s >= 30 and any(k in bests for k in ("travel", "regroup", "roam", "follow", "collect")):
        flags.append(f"STUCK {stuck_s:.0f}s in '{bests}'")
    if mx * DT >= 6: flags.append(f"TARGET-IDLE {mx*DT:.0f}s")
    if pingpong: flags.append(f"MAP-PINGPONG {mapchg} changes")
    sitflip = sum(1 for k in range(1, len(S)) if ("rest" in short(S[k].get("state")) or "sit" in short(S[k].get("state")) or "stand" in short(S[k].get("state"))) != ("rest" in short(S[k-1].get("state")) or "sit" in short(S[k-1].get("state")) or "stand" in short(S[k-1].get("state"))))
    if sitflip >= 6: flags.append(f"SIT-FLICKER {sitflip}")
    if not weap: flags.append("NO-WEAPON")
    if wpct >= 50: flags.append(f"WEIGHT {wpct:.0f}%")
    if pots > 60: flags.append(f"POTS {pots}")
    if deaths: flags.append(f"DIED x{deaths}")
    rs = sum(c for s, c in states.items() if "roam" in s)
    if rs >= len(S) * 0.8 and kills == 0: flags.append("ROAM-NO-FIGHT")
    if any("town" in s or "selling" in s or "restock" in s for s in states) and wpct < 70 and pots > 10:
        flags.append(f"TOWNTRIP? w{wpct:.0f}% pots{pots}")
    NEAR={"prontera":("prontera","prt_","izlude"),"izlude":("izlude","prt_fild08","prt_fild05","prt_fild07","prt_fild06","prt_fild09"),"payon":("pay","payon"),"geffen":("gef",),"morocc":("moc","morocc"),"alberta":("alberta","pay_fild","alb")}
    if cls == "Novice" and home and maps[-1] and not maps[-1].startswith(NEAR.get(home,(home,))):
        flags.append(f"NOVICE-AWAY home={home}")
    fightn = sum(1 for s in S if any(k in (s.get("state") or "") for k in ("fighting","tanking","engaging","assisting","defending")))
    spfull = all((s.get("sp") or 0) >= (s.get("max_sp") or 1) for s in S)
    if cls not in ("Novice","Acolyte","Priest","Merchant","Archer","Hunter","Dancer","Bard","Thief") and fightn >= len(S)*0.4 and spfull and (z.get("max_sp") or 0) > 40:
        flags.append(f"SP-FULL-WHILE-FIGHTING {fightn}/{len(S)} skills={[k.get('name', k) if isinstance(k, dict) else k for k in (z.get('skills') or [])][:6]}")
    arrows = sum(i["Count"] for i in inv if 1750 <= i["ItemID"] < 1800)
    if cls in ("Archer","Hunter","Bard","Dancer") and not arrows: flags.append(f"NO-ARROWS weap={weap} zeny={z.get('zeny')}")
    if not flags:
        out.append(f"ok {n} {cls} lv{lv} {pos[-1]} k+{kills} {states.most_common(1)[0][0][:40]}"); continue
    out.append(f"## {n} {cls} lv{lv} home={home} zeny={z.get('zeny')} w={wpct:.0f}% pots={pots} weap={weap} kills+{kills} deaths+{deaths} hp {min(hps)}-{max(hps)}/{z.get('max_hp')} sp {z.get('sp')}/{z.get('max_sp')} distinct_pos={moving} maxstuck={stuck_s:.0f}s mobs<= {mobsn}")
    out.append(f"   pos {pos[0]} -> {pos[-1]}  goal={z.get('goal')}")
    out.append(f"   states: " + "; ".join(f"{s} x{c}" for s, c in states.most_common(5)))
    out.append(f"   plan: {short(z.get('plan'))[:200] if z.get('plan') else ''} | {str(z.get('plan',''))[70:260]}")
    out.append("   FLAGS: " + " | ".join(flags))

# party spread
spread = collections.defaultdict(list)
for t, P in party_s:
    for p in P:
        mem = [m for m in p.get("members", []) if m.get("online")]
        L = next((m for m in mem if m.get("leader")), None)
        if not L: continue
        far = []
        for m in mem:
            if m is L: continue
            if m.get("map") != L.get("map"): far.append(f"{m['name']}@{m.get('map')}")
            else:
                d = max(abs(m["x"] - L["x"]), abs(m["y"] - L["y"]))
                if d > 20: far.append(f"{m['name']}:{d}")
        spread[p["name"]].append((far, L.get("state"), (L.get("map"), L.get("x"), L.get("y"))))
out.append("## PARTIES with members far from leader in every sample:")
cnt = 0
for pn, lst in spread.items():
    if len(lst) >= 3 and all(f for f, _, _ in lst):
        cnt += 1
        if cnt <= 12 or pn.startswith("Ayla"):
            out.append(f"   {pn}: {lst[-1][0][:4]} leader={short(lst[-1][1])} at {lst[-1][2]} (first: {lst[0][2]})")
    if pn.startswith("Ayla"):
        out.append(f"   AYLA PT: " + " / ".join(f"{f} L={short(s)[:40]}@{p}" for f, s, p in lst))
out.append(f"   total spread parties: {cnt} of {len(spread)}")
print("\n".join(out))
