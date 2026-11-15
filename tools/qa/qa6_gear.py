# gear audit from DB dumps: itemdb/chars.tsv, itemdb/inv.tsv, itemdb/equip.yml, itemdb/etc.yml
import re, collections, random, sys
D = "itemdb/"
def parse(fn):
    items = {}; cur = None; sect = None
    for line in open(D + fn, encoding="utf-8"):
        m = re.match(r"  - Id: (\d+)", line)
        if m:
            cur = {"id": int(m.group(1)), "jobs": {}, "loc": set(), "classes": {}}; items[cur["id"]] = cur; sect = None; continue
        if cur is None: continue
        m = re.match(r"    (\w+):\s*(.*)$", line)
        if m:
            k, v = m.group(1), m.group(2).strip(); sect = None
            if k in ("Jobs", "Locations", "Classes") and v == "": sect = k; continue
            cur[k] = v; continue
        m = re.match(r"      (\w+):\s*(\w+)", line)
        if m and sect:
            val = m.group(2) == "true"
            if sect == "Jobs": cur["jobs"][m.group(1)] = val
            elif sect == "Locations" and val: cur["loc"].add(m.group(1))
            else: cur["classes"][m.group(1)] = val
    return items
EQ = parse("equip.yml"); ETC = parse("etc.yml")
JOB = {0:"Novice",1:"Swordman",2:"Mage",3:"Archer",4:"Acolyte",5:"Merchant",6:"Thief",7:"Knight",8:"Priest",9:"Wizard",10:"Blacksmith",
       11:"Hunter",12:"Assassin",14:"Crusader",15:"Monk",16:"Sage",17:"Rogue",18:"Alchemist",19:"BardDancer",20:"BardDancer"}
def jobname(c):
    if c >= 4001 and c <= 4022: c = c - 4001  # high/trans roughly map
    if c == 4008 - 4001: pass
    return JOB.get(c, "?")
TRANS = {4008:7,4009:8,4010:9,4011:10,4012:11,4013:12,4015:14,4016:15,4017:16,4018:17,4019:18,4020:19,4021:20,4002:1,4003:2,4004:3,4005:4,4006:5,4007:6,4001:0}
def canwear(it, cls, lv):
    c = TRANS.get(cls, cls); j = JOB.get(c, "?")
    jobs = it["jobs"]
    ok = jobs.get(j) if j in jobs else jobs.get("All", False)
    if not jobs: ok = True
    if not ok: return False
    if int(it.get("EquipLevelMin", "0") or 0) > lv: return False
    g = it.get("Gender")
    if g == "Female" and cls in (19, 4020): return False
    if g == "Male" and cls in (20, 4021): return False
    return True
SLOTMAP = {"Right_Hand":"weapon","Both_Hand":"weapon","Left_Hand":"shield","Armor":"armor","Garment":"garment","Shoes":"shoes",
           "Head_Top":"head_top","Head_Mid":"head_mid","Head_Low":"head_low","Right_Accessory":"acc","Left_Accessory":"acc","Both_Accessory":"acc"}
BIT = {"head_low":1,"weapon":2,"garment":4,"acc":8|128,"armor":16,"shield":32,"shoes":64,"head_top":256,"head_mid":512}
def slots_of(it):
    return {SLOTMAP[l] for l in it["loc"] if l in SLOTMAP}
chars = {}
for l in open(D + "chars.tsv", encoding="utf-8"):
    cid, name, cls, lv, z = l.rstrip("\n").split("\t"); chars[cid] = (name, int(cls), int(lv), int(z))
inv = collections.defaultdict(list)
for l in open(D + "inv.tsv", encoding="utf-8"):
    f = l.rstrip("\n").split("\t"); inv[f[0]].append([int(x) for x in f[1:]])  # nameid amount equip identify refine c0..c3
random.seed(7)
ids = [c for c in chars if chars[c][1] not in (5, 10, 18) or True]
sample = random.sample(ids, min(int(sys.argv[1]) if len(sys.argv) > 1 else 150, len(ids)))
wear = collections.Counter(); unid = []; better = []; cardfit = []; emptywear = []
def score(it, s):
    if s == "weapon": return int(it.get("Attack", "0") or 0) + int(it.get("MagicAttack", "0") or 0)
    return int(it.get("Defense", "0") or 0)
for cid in sample:
    name, cls, lv, z = chars[cid]; rows = inv[cid]
    worn = {}
    for r in rows:
        if r[2] and r[0] in EQ:
            for s in slots_of(EQ[r[0]]):
                if r[2] & BIT[s]: worn[s] = r
            if r[2] & (8 | 128): worn.setdefault("acc", r)
    for s in BIT: wear[s] += (1 if s in worn else 0)
    accs = sum(1 for r in rows if r[2] & 8) + sum(1 for r in rows if r[2] & 128)
    wear["acc2"] += (1 if accs >= 2 else 0)
    shieldok = not ("weapon" in worn and "Both_Hand" in EQ[worn["weapon"][0]]["loc"])
    for r in rows:
        if r[2] or r[0] not in EQ: continue
        it = EQ[r[0]]
        if it.get("Type") not in ("Weapon", "Armor"): continue
        if r[3] == 0: unid.append((name, lv, r[0], it.get("Name"))); continue
        if not canwear(it, cls, lv): continue
        for s in slots_of(it):
            if s == "shield" and not shieldok: continue
            if s not in worn:
                if s == "weapon" and it.get("Type") != "Weapon": continue
                emptywear.append((name, cls, lv, s, it.get("Name")))
            else:
                w = worn[s]; wi = EQ.get(w[0], {})
                if s != "weapon" and score(it, s) + r[4] > score(wi, s) + w[4] and int(it.get("Slots", "0") or 0) >= sum(1 for c in w[5:9] if c):
                    better.append((name, cls, lv, s, wi.get("Name"), "->", it.get("Name")))
    # cards in bag
    for r in rows:
        if r[0] in ETC and ETC[r[0]].get("Type") == "Card":
            cl = slots_of(ETC[r[0]])
            for s in cl:
                w = worn.get(s)
                if w and int(EQ.get(w[0], {}).get("Slots", "0") or 0) > sum(1 for c in w[5:9] if c):
                    cardfit.append((name, lv, ETC[r[0]].get("Name"), s, EQ[w[0]].get("Name"))); break
n = len(sample)
print(f"sample {n} online lv20+ chars (DB at last save)")
print("wearing per slot %:", {s: round(100 * wear[s] / n) for s in list(BIT) + ["acc2"]})
print(f"unidentified equipment: {len(unid)} items on {len(set(u[0] for u in unid))} chars", unid[:8])
print(f"wearable identified gear in bag for an EMPTY slot: {len(emptywear)} items on {len(set(e[0] for e in emptywear))} chars", collections.Counter(e[3] for e in emptywear).most_common(), emptywear[:8])
print(f"higher-DEF identified gear unworn: {len(better)} on {len(set(b[0] for b in better))} chars", better[:8])
print(f"cards in bag that fit a free slot on worn gear: {len(cardfit)} on {len(set(c[0] for c in cardfit))} chars", cardfit[:8])
