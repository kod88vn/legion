exec(open("qa6_gear.py").read().split("random.seed(7)")[0])
def score(it, s):
    if s == "weapon": return int(it.get("Attack", "0") or 0) + int(it.get("MagicAttack", "0") or 0)
    return int(it.get("Defense", "0") or 0)
import collections
emp=collections.Counter(); upg=collections.Counter(); chars_hit=set(); ex=[]; mag=0; accown=0; tot=0
for cid,(name,cls,lv,z) in chars.items():
    rows=inv[cid]; tot+=1
    if any(r[0]==611 for r in rows): mag+=1
    if any(r[0] in EQ and slots_of(EQ[r[0]])&{"acc"} for r in rows): accown+=1
    worn={}
    for r in rows:
        if r[2] and r[0] in EQ:
            for s in slots_of(EQ[r[0]]):
                if r[2]&BIT[s]: worn[s]=r
    shieldok = not ("weapon" in worn and "Both_Hand" in EQ[worn["weapon"][0]]["loc"])
    for r in rows:
        if r[2] or r[3]!=0 or r[0] not in EQ: continue
        it=EQ[r[0]]
        if not canwear(it,cls,lv): continue
        for s in slots_of(it):
            if s=="shield" and not shieldok: continue
            if s not in worn and s!="weapon": emp[s]+=1; chars_hit.add(name); ex.append((name,lv,s,it.get("Name")))
            elif s in worn and s!="weapon" and score(it,s)>score(EQ.get(worn[s][0],{}),s)+worn[s][4]: upg[s]+=1; chars_hit.add(name); ex.append((name,lv,s,EQ.get(worn[s][0],{}).get("Name"),"->",it.get("Name")))
print("chars",tot,"with Magnifier",mag,"owning any accessory item",accown)
print("UNIDENTIFIED but wearable: fills an empty slot",dict(emp),"| higher DEF than worn",dict(upg),"| chars affected",len(chars_hit))
print(ex[:12])
c=collections.Counter(EQ[r[0]].get("Name") for cid in chars for r in inv[cid] if r[3]==0 and r[0] in EQ); print("top unidentified:",c.most_common(12))
