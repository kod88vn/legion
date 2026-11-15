"""Gear alternatives, refine plan and card plan for every gear band of every build (EC40-EC43).

Used by tools/design/gen_milestones.py, which writes the result into legion/data/buildmilestones.json (additive: each
range gets "gear" and "cards"). Every number comes from the server's own rAthena database, read read-only from the
running container by fetch_extra (G23: never the live DB):
  db/re/item_db_equip.yml, item_db_etc.yml, item_db_usable.yml  Buy price, slots, jobs, locations, attack, defense
  db/re/refine.yml                                              refine chances and costs per grade and refine level
  db/re/mob_db.yml                                              monster drops (Drops, MvpDrops: Rate out of 10000)
  npc/ scripts                                                  monster spawns (map, monster id) and NPC shop stock
  conf/import/battle_conf.txt                                   item_rate_* drop multipliers (the server's live rates)

Rules encoded here (the Legion's own, see docs/rules/economy.md EC40-EC43):
  * Gear per slot: the curated pieces of legion/data/buildgear.json (weakest first, so the last is the best piece),
    then the item database's other pieces of the same slot that the build's class can wear at the band's level. Each
    piece says how it is bought (NPC shop: map, shop, price) or where it drops (monsters, rate, maps).
  * tier: "affordable" = bought at or below the gear budget of the band (GEAR_LV_BUDGET z per level of the band's top),
    or dropped by a monster at most 10 levels above the band's top with a rate of at least 0.5 %; "fallback" = the rest.
  * Refine targets: weapons +7 (grade 1), +8 (grade 2), +7 (grades 3-4), +8 (grade 5); every other refineable piece +7.
    The cost is the cheapest material per refine level, weighed by its chance: fee_z (NPC refine zeny) + ore_z (the
    material's price: NPC shop, else item db Buy; HD_/Enriched_ use the base ore's Buy as a floor). A level whose material
    breaks the item on a failure costs more (restart); break_risk_pct is the chance of losing the piece. Estimate only.
  * Cards are one-time bound: a card goes in only once its gear reaches the refine level in "insert_at". Tiers come from
    the monster drops at the server's rates (non-MVP drops): popular = 3+ monsters at 0.5 % or more; uncommon = any
    non-MVP drop at 0.1 % or more; rare = the rest. Popular and uncommon cards are used at any band: insert_at is the
    highest refine level the band can pay for, so a usable card goes in now. Rare cards only at end-game bands (top 60
    or more) with the full refine affordable, else the fallback card (popular, same slot).
"""
import os
import re
import subprocess
import tempfile

ROOT_DB_FILES = ("item_db_equip", "item_db_etc", "item_db_usable", "refine", "mob_db")
SLOT_LOCS = {"weapon": ("Right_Hand", "Both_Hand"), "shield": ("Left_Hand",), "armor": ("Armor",),
             "headgear": ("Head_Top", "Head_Mid", "Head_Low"), "garment": ("Garment",), "shoes": ("Shoes",),
             "accessory": ("Left_Accessory", "Right_Accessory", "Both_Accessory")}
SLOT_ORDER = ["weapon", "shield", "armor", "headgear", "garment", "shoes", "accessory"]
REFINE_TARGET_WEAPON = {1: 7, 2: 8, 3: 7, 4: 7, 5: 8}   # WeaponLevel -> target refine
REFINE_TARGET_OTHER = 7
GEAR_LV_BUDGET = 2000        # z per level of the band's top, for a bought piece (affordable)
REFINE_LV_BUDGET = 15000     # z per level of the band's top, for the refine of the best piece (affordable)
FARM_LV_GAP = 10             # a drop from a monster this many levels above the band's top still counts as farmable
FARM_RATE_PCT = 0.5          # ... at this drop rate (%) or more
POPULAR_PCT = 0.5            # card tier: 3+ non-MVP monsters at this rate (%) or more
UNCOMMON_PCT = 0.1           # card tier: any non-MVP monster at this rate (%) or more
MAX_ALTS = 3
JOB_ALIASES = {"Bard": "BardDancer", "Dancer": "BardDancer"}


# ----------------------------------------------------------------- fetch (read-only, from the running container)

def _docker_cat(path, env=None):
    env = dict(env or os.environ, MSYS_NO_PATHCONV="1")
    return subprocess.run(["wsl.exe", "docker", "exec", "ro-rathena", "cat", path], capture_output=True, check=True,
                          env=env).stdout


def _docker_sh(script):
    env = dict(os.environ, MSYS_NO_PATHCONV="1")
    return subprocess.run(["wsl.exe", "docker", "exec", "ro-rathena", "sh", "-c", script], capture_output=True,
                          check=True, env=env).stdout.decode("utf-8", "replace")


def fetch_extra(dst):
    """Write the item, refine and mob databases, the spawn and shop tables and the drop multipliers into dst.
    Reads only: cat and grep inside the game server container. Spawns and shops come from the loaded npc/ tree, not
    from the pre-renewal scripts."""
    for f in ROOT_DB_FILES:
        with open(os.path.join(dst, f + ".yml"), "wb") as out:
            out.write(_docker_cat("/rathena/db/re/%s.yml" % f))
    spawns, shops = [], []
    for line in _docker_sh("grep -rH -P '\\tmonster\\t' /rathena/npc --include=*.txt").splitlines():
        path, _, rest = line.partition(":")
        if "pre-re" in path or "/test" in path:
            continue
        parts = rest.split("\t")
        if len(parts) >= 4 and parts[1] == "monster":
            spawns.append("%s\t%s\t%s" % (parts[0].split(",")[0], parts[3].split(",")[0], parts[3].split(",")[1]))
    for line in _docker_sh("grep -rH -P '\\tshop\\t' /rathena/npc --include=*.txt").splitlines():
        path, _, rest = line.partition(":")
        if "pre-re" in path or "/test" in path:
            continue
        parts = rest.split("\t")
        if len(parts) >= 4 and parts[1] == "shop":
            shops.append("%s\t%s\t%s" % (parts[0].split(",")[0], parts[2], parts[3]))
    rates = []
    for line in _docker_sh("grep -h '^item_rate_' /rathena/conf/import/battle_conf.txt").splitlines():
        m = re.match(r"^(item_rate_\w+):\s*(\d+)", line.strip())
        if m:
            rates.append("%s\t%s" % (m.group(1), m.group(2)))
    for name, lines in (("spawns.tsv", spawns), ("shops.tsv", shops), ("rates.tsv", rates)):
        with open(os.path.join(dst, name), "w", encoding="utf-8", newline="\n") as f:
            f.write("\n".join(lines) + "\n")


# ----------------------------------------------------------------- catalog

def _yaml_body(path):
    import yaml
    L = getattr(yaml, "CSafeLoader", yaml.SafeLoader)
    with open(path, encoding="utf-8") as f:
        return yaml.load(f, Loader=L)["Body"]


class Catalog:
    """Everything the gear plan reads, loaded once from a folder written by fetch_extra."""

    def __init__(self, db_dir):
        self.items = {}
        for f in ("item_db_equip", "item_db_etc", "item_db_usable"):
            for it in _yaml_body(os.path.join(db_dir, f + ".yml")):
                self.items.setdefault(it["Id"], it)
        self.by_aegis = {it["AegisName"]: it for it in self.items.values()}
        self.refine = {}
        for g in _yaml_body(os.path.join(db_dir, "refine.yml")):
            if g["Group"] not in ("Weapon", "Armor"):
                continue
            tab = {}
            for lv in g["Levels"]:
                tab[lv["Level"]] = {r["Level"]: [{"type": c["Type"], "rate": c["Rate"], "price": c.get("Price", 0),
                                                  "material": c["Material"], "break": c.get("BreakingRate", 0)}
                                                 for c in r["Chances"]] for r in lv["RefineLevels"]}
            self.refine[g["Group"]] = tab
        self.mobs = {m["Id"]: m for m in _yaml_body(os.path.join(db_dir, "mob_db.yml"))}
        self.drops = {}   # item AegisName -> [(mob id, base rate 1/10000, mvp-only)]
        for m in self.mobs.values():
            for d in m.get("Drops") or []:
                self.drops.setdefault(d["Item"], []).append((m["Id"], d["Rate"], False))
            for d in m.get("MvpDrops") or []:
                self.drops.setdefault(d["Item"], []).append((m["Id"], d["Rate"], True))
        self.spawns = {}  # mob id -> {map: count}
        self.shops = {}   # item id -> [(map, shop name, price or None)]
        with open(os.path.join(db_dir, "spawns.tsv"), encoding="utf-8") as f:
            for line in f:
                p = line.rstrip("\n").split("\t")
                if len(p) == 3 and p[1].isdigit():
                    mp = self.spawns.setdefault(int(p[1]), {})
                    mp[p[0]] = mp.get(p[0], 0) + int(p[2] or 1)
        with open(os.path.join(db_dir, "shops.tsv"), encoding="utf-8") as f:
            for line in f:
                p = line.rstrip("\n").split("\t")
                if len(p) != 3:
                    continue
                toks = p[2].split(",")
                if toks and ":" not in toks[0]:
                    toks = toks[1:]   # the NPC view id
                for t in toks:
                    key, _, price = t.partition(":")
                    iid = self.resolve(key)
                    if iid is None:
                        continue
                    pr = int(price) if price.lstrip("-").isdigit() else -1
                    self.shops.setdefault(iid, []).append((p[0], p[1], None if pr < 0 else pr))
        self.rates = {}
        with open(os.path.join(db_dir, "rates.tsv"), encoding="utf-8") as f:
            for line in f:
                p = line.rstrip("\n").split("\t")
                if len(p) == 2 and p[1].isdigit():
                    self.rates[p[0]] = int(p[1])

    def resolve(self, key):
        if key.isdigit():
            return int(key)
        it = self.by_aegis.get(key)
        return it["Id"] if it else None

    def mult(self, item, mob, mvp_only):
        """the server's drop multiplier for this item and monster (item_rate_<kind>[_boss|_mvp], percent)"""
        t = item.get("Type")
        kind = {"Weapon": "equip", "Armor": "equip", "Shadowgear": "equip", "Card": "card", "Healing": "heal",
                "Usable": "use"}.get(t, "common")
        if mvp_only or (mob.get("Modes") or {}).get("Mvp"):
            suffix = "_mvp"
        elif (mob.get("Modes") or {}).get("Boss"):
            suffix = "_boss"
        else:
            suffix = ""
        return self.rates.get("item_rate_%s%s" % (kind, suffix), 100) / 100.0

    @staticmethod
    def is_dropper(mob):
        """a real monster: event copies (E_ prefix, level 0) never count as droppers (review ee67f75)"""
        return bool(mob) and (mob.get("Level") or 0) > 0 and not str(mob.get("AegisName", "")).startswith("E_")

    def drop_list(self, item):
        """[(rate pct at the server's rates, mob id, mvp_only)] for an item, best first"""
        out = []
        it = self.items.get(item["Id"], item)
        for mob_id, rate, mvp_only in self.drops.get(it["AegisName"], []):
            m = self.mobs.get(mob_id)
            if not self.is_dropper(m):
                continue
            eff = min(10000.0, rate * self.mult(it, m, mvp_only))
            out.append((round(eff / 100.0, 3), mob_id, mvp_only))
        out.sort(key=lambda x: (-x[0], x[1]))
        return out

    def card_tier(self, item):
        """popular / uncommon / rare, from the non-MVP drops at the server's rates"""
        drops = [d for d in self.drop_list(item) if not d[2]]
        if sum(1 for d in drops if d[0] >= POPULAR_PCT) >= 3:
            return "popular"
        if drops and drops[0][0] >= UNCOMMON_PCT:
            return "uncommon"
        return "rare"

    def source(self, item):
        """how a piece is obtained: the cheapest NPC shop (map, shop, price) and the best three drops with maps"""
        iid = item["Id"]
        shop = None
        if self.shops.get(iid):
            m, npc, pr = min(self.shops[iid], key=lambda s: (s[2] if s[2] is not None else item.get("Buy", 0), s[0]))
            shop = {"map": m, "npc": npc, "price_z": pr if pr is not None else item.get("Buy", 0)}
        drops = []
        for pct, mob_id, mvp_only in self.drop_list(item)[:2]:
            m = self.mobs[mob_id]
            maps = sorted(self.spawns.get(mob_id, {}).items(), key=lambda kv: (-kv[1], kv[0]))[:3]
            drops.append({"mob": m["Name"], "lv": m.get("Level"), "rate_pct": pct,
                          "mvp_only": mvp_only, "maps": [k for k, _ in maps[:2]]})
        how = "shop" if shop else ("drop" if drops else "none")
        if shop:
            drops = []   # a shop piece is bought; its drops are not listed (smaller file)
        return {"how": how, "shop": shop, "drops": drops}

    def material_price(self, name):
        """(zeny, source) of a refine material. The NPC shop price when a shop sells it; else the item db Buy value; the
        HD_ and Enriched_ variants carry Buy 20 in the item db (not a price), so they take the base ore's Buy as a floor.
        (None, reason) when no price is known: that part of the cost is left out and reported as unpriced."""
        mat = self.by_aegis.get(name)
        if mat is None:
            return None, "not in the item database"
        p = self.npc_price(mat)
        if p is not None:
            return p, "npc_shop"
        for pre in ("HD_", "Enriched_"):
            if name.startswith(pre):
                base = self.by_aegis.get(name[len(pre):])
                if base and base.get("Buy", 0) > 0:
                    return base["Buy"], "item_db_buy of the base ore (floor: %s Buy is not a price)" % pre.rstrip("_")
                return None, "unpriced"
        if mat.get("Buy", 0) > 0:
            return mat["Buy"], "item_db_buy"
        return None, "unpriced"

    def npc_price(self, item):
        """the cheapest NPC shop price of an item, or None when no shop sells it (the Buy field of the item database is
        not a shop price for materials such as HD_Elunium, so it is not used)"""
        prices = [pr if pr is not None else item.get("Buy", 0) for _, _, pr in self.shops.get(item["Id"], [])]
        return min(prices) if prices else None


# ----------------------------------------------------------------- refine plan

def grade_of(item):
    return item.get("WeaponLevel") if item.get("Type") == "Weapon" else item.get("ArmorLevel", 1)


def refine_target(item):
    if not item.get("Refineable"):
        return None
    if item.get("Type") == "Weapon":
        return REFINE_TARGET_WEAPON.get(grade_of(item), 7)
    return REFINE_TARGET_OTHER


def refine_plan(cat, item, target):
    """cost and risk to refine a piece from +0 to target. Per level: the material with the lowest expected cost (the
    zeny fee of the chance plus the material's price, over the chance); a material that breaks the piece on a failure
    is weighed by the restart it forces. cost_z = fee_z (NPC refine fee) + ore_z (materials, see Catalog.material_price)."""
    if target is None:
        return None
    group = "Weapon" if item.get("Type") == "Weapon" else "Armor"
    tab = cat.refine.get(group, {}).get(grade_of(item))
    if not tab:
        return None
    fee_t, ore_t, survive, steps, unpriced, mats = 0.0, 0.0, 1.0, [], set(), {}
    for lv in range(1, target + 1):
        opts = tab.get(lv)
        if not opts:
            return None
        best = None
        for o in opts:
            mp, src = cat.material_price(o["material"])
            ore = mp or 0
            rate = max(o["rate"], 1) / 10000.0
            mult = (1.0 / rate) if o["break"] else 1.0
            exp_fee = o["price"] / rate * mult
            exp_ore = ore / rate * mult
            key = (1 if o["break"] else 0, exp_fee + exp_ore)   # a material that cannot break the piece always wins
            if best is None or key < best[0]:
                best = (key, o, ore, src, exp_fee, exp_ore)
        _, o, ore, src, exp_fee, exp_ore = best
        rate = max(o["rate"], 1) / 10000.0
        if o["material"] not in mats:
            mats[o["material"]] = {"z": ore if src != "unpriced" and ore else None, "source": src}
        if mats[o["material"]]["z"] is None:
            unpriced.add(o["material"])   # no price known: the ore part is left out, and reported
        fee_t += exp_fee
        ore_t += exp_ore
        if o["break"]:
            survive *= rate
        steps.append({"level": lv, "material": o["material"], "rate_pct": round(o["rate"] / 100.0, 2),
                      "fee_z": o["price"], "ore_z": ore, "exp_z": int(round(exp_fee + exp_ore)),
                      "breaks": bool(o["break"])})
    fee, ore = int(round(fee_t)), int(round(ore_t))
    return {"target": target, "cost_z": fee + ore, "fee_z": fee, "ore_z": ore,
            "break_risk_pct": round((1 - survive) * 100.0, 1), "steps": steps, "unpriced": sorted(unpriced),
            "materials": mats}


def cumulative_costs(plan):
    """expected zeny to reach each refine level (index = level), 0 at +0"""
    out = [0]
    acc = 0.0
    for s in plan["steps"]:
        acc += s["exp_z"]
        out.append(int(round(acc)))
    return out


# ----------------------------------------------------------------- gear per band

def _jobs_ok(item, job_keys):
    jobs = item.get("Jobs")
    if not jobs:
        return True
    return any(jobs.get(k) for k in job_keys)


def _slot_ok(item, slot):
    locs = item.get("Locations") or {}
    if slot == "weapon":
        return item.get("Type") == "Weapon" and any(locs.get(k) for k in SLOT_LOCS["weapon"])
    if item.get("Type") not in ("Armor", "Shadowgear"):
        return False
    return any(locs.get(k) for k in SLOT_LOCS[slot])


def piece(cat, item, tier, why, band_max, job_keys=None):
    src = cat.source(item)
    tgt = refine_target(item)
    plan = refine_plan(cat, item, tgt)
    if src["how"] == "none":
        src["note"] = "not sold by an NPC shop or dropped by a monster in the server data (quest, craft or event piece)"
    return {
        "id": item["Id"], "name": item.get("Name", "#%d" % item["Id"]), "tier": tier, "why": why,
        "kind": "weapon" if item.get("Type") == "Weapon" else "armor", "grade": grade_of(item),
        "level_min": item.get("EquipLevelMin", 0), "source": src,
        "price_z": src["shop"]["price_z"] if src["shop"] else None,
        "refine": ({"target": tgt, "cost_z": plan["cost_z"], "fee_z": plan["fee_z"], "ore_z": plan["ore_z"],
                    "break_risk_pct": plan["break_risk_pct"], "materials": plan["materials"],
                    **({"unpriced": plan["unpriced"]} if plan["unpriced"] else {})} if plan else None),
        "slots": item.get("Slots", 0),
        "_plan": plan,
    }


def band_gear(cat, band, job_keys, band_min, band_max):
    """{slot: {"best": piece, "alternatives": [piece], "note": str}} for one gear band, from its curated lists and the
    item database. Curated pieces come first (the last curated piece is the best); database pieces follow by defense
    and attack, only those with a source (shop or drop) and wearable by the class at the band's top level."""
    out = {}
    for slot in SLOT_ORDER:
        curated = [int(x) for x in band.get(slot) or []]
        cand = []
        for cid in curated:
            it = cat.items.get(cid)
            if it:
                cand.append((it, "curated (legion/data/buildgear.json)"))
        seen = {c[0]["Id"] for c in cand}
        extra = []
        for iid, it in cat.items.items():
            if iid in seen or not _slot_ok(it, slot) or not _jobs_ok(it, job_keys):
                continue
            if it.get("EquipLevelMin", 0) > band_max or (slot == "weapon" and not it.get("Attack")):
                continue
            if cat.source(it)["how"] == "none" or "Costume" in it.get("AegisName", ""):
                continue
            extra.append((it, "item database: %s, same slot and class" % slot))
        extra.sort(key=lambda c: (-(c[0].get("Attack", 0) or c[0].get("Defense", 0)), c[0]["Id"]))
        if not cand and not extra:
            out[slot] = {"best": None, "alternatives": [], "note": "no piece for this slot in the curated table or the item database"}
            continue
        best = None
        if cand:
            best = cand[-1]
        alts_all = [c for c in cand[:-1]] + extra
        if best is None:
            best = alts_all.pop(0)
        pieces = []
        for it, why in alts_all:
            pieces.append(_tier_piece(cat, it, why, band_max, job_keys))
        bpiece = piece(cat, best[0], "best", best[1], band_max)
        bpiece["tier"] = "best"
        # affordable first, then the fallback pieces; the list is capped, every tier keeps at least one piece
        aff = [p for p in pieces if p["tier"] == "affordable"]
        fb = [p for p in pieces if p["tier"] == "fallback"]
        alts = (aff + fb)[:MAX_ALTS]
        note = "" if alts else "no other piece for this slot in the item database at this level and class"
        out[slot] = {"best": _public(bpiece), "alternatives": [_public(p) for p in alts], "note": note}
    return out


def _tier_piece(cat, it, why, band_max, job_keys):
    p = piece(cat, it, "fallback", why, band_max)
    src = p["source"]
    price = src["shop"]["price_z"] if src["shop"] else None
    cheap = price is not None and price <= GEAR_LV_BUDGET * band_max
    farm = any(d["lv"] is not None and d["lv"] <= band_max + FARM_LV_GAP and d["rate_pct"] >= FARM_RATE_PCT
               and not d["mvp_only"] for d in src["drops"])
    p["tier"] = "affordable" if (cheap or farm) else "fallback"
    return p


def _public(p):
    p = dict(p)
    p.pop("_plan", None)
    return p


def band_cards(cat, band_cards_list, gear, band_max, all_cards):
    """the card plan for one band: refine target and cost of the slot's best piece, the tier, the refine level the
    card goes in at (insert_at), its status and the fallback card (see the module doc)."""
    budget = REFINE_LV_BUDGET * band_max
    plans = {}
    for slot, g in gear.items():
        b = g.get("best")
        if b is None:
            continue
        item = cat.items[b["id"]]
        plan = refine_plan(cat, item, refine_target(item))
        plans[slot] = plan
    rows = []
    for c in band_cards_list:
        slot = c.get("slot")
        it = cat.items.get(c["id"])
        if it is None:
            continue
        tier = cat.card_tier(it)
        plan = plans.get(slot)
        if plan is None:
            insert_at, cost, target = 0, 0, None
        else:
            target, cost = plan["target"], plan["cost_z"]
            cum = cumulative_costs(plan)
            insert_at = max((lv for lv in range(0, plan["target"] + 1) if cum[lv] <= budget), default=0)
        full = plan is None or insert_at == target
        if tier == "rare":
            ok = band_max >= 60 and full
            status = "use_at_target" if ok else "fallback"
        else:
            status = "use_now"
        fallback = None
        if status == "fallback":
            fallback = _popular_fallback(cat, slot, rows, band_cards_list, all_cards, c["id"])
        rows.append({
            "id": c["id"], "name": it.get("Name", "#%d" % c["id"]), "slot": slot, "prio": c.get("prio"),
            "why": c.get("why", ""), "tier": tier, "status": status,
            "refine_target": target, "refine_cost_z": cost if plan is not None else 0,
            "insert_at": insert_at if plan is not None else 0,
            "source": cat.source(it),
            "fallback": fallback,
            "rule": _card_rule(tier, band_max, plan is not None, full),
        })
    return rows


def _card_rule(tier, band_max, refinable, full):
    if tier == "popular":
        return "popular: usable at any band; goes in at the refine level the band can pay for"
    if tier == "uncommon":
        return ("uncommon (tier threshold is the fixer's estimate, not an owner rule): usable at any band; goes in at the "
                "refine level the band can pay for")
    if band_max < 60:
        return "rare: end-game bands only (top 60+); fallback until then"
    return "rare: end-game band with the full refine affordable; else fallback"


def _popular_fallback(cat, slot, rows, band_list, all_cards, self_id):
    for c in list(band_list) + list(all_cards):
        if c.get("slot") != slot or c["id"] == self_id:
            continue
        it = cat.items.get(c["id"])
        if it is not None and cat.card_tier(it) == "popular":
            return {"id": c["id"], "name": it.get("Name", "#%d" % c["id"])}
    return None


def build_gear(cat, plan_job_keys, bands):
    """ranges for one build: a dict per gear band {"min", "max", "gear", "cards"}, from the buildgear.json bands"""
    all_cards = [c for b in bands for c in (b.get("cards") or [])]
    out = []
    for band in bands:
        bmin, bmax = band["min"], band["max"]
        gear = band_gear(cat, band, plan_job_keys, bmin, bmax)
        cards = band_cards(cat, band.get("cards") or [], gear, bmax, all_cards)
        gear_out = {}
        for slot, g in gear.items():
            gear_out[slot] = {"best": g["best"], "alternatives": g["alternatives"], "note": g["note"]}
        out.append({"min": bmin, "max": bmax, "gear": gear_out, "cards": cards})
    return out


def job_keys_for(second, first, aliases=JOB_ALIASES):
    keys = {second, first}
    for k in (second, first):
        if k in aliases:
            keys.add(aliases[k])
    keys.add("Novice")
    return sorted(keys)
