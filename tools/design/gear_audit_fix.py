#!/usr/bin/env python3
"""One-time repair of legion/data/buildgear.json from the build audit (docs/reports/build-audit-20261115.md, F2-F9, F19).

It is idempotent: a second run changes nothing. Reads the server's item and mob tables from a folder (default: fetched
read-only from the running container with gearalts.fetch_extra, G23), writes buildgear.json in the file's own format.
  * a listed piece the class cannot wear (Jobs; "All" = every class) or that needs a higher level than the band top is
    removed; a slot the class is left without in a band is refilled with the best sourced, wearable piece;
  * F6: no shield in a band whose best (last) weapon is two-handed;
  * F8: bard and dancer bands get a sourced instrument (Musical) or whip (Whip) at the front of the weapon list;
  * F9: a card whose lowest dropper (real monsters only: level > 0, no E_ event copy) is more than CARD_SLACK levels
    above the band top is replaced by a same-slot card of the build that fits the band, or dropped;
  * F19: the card "why" texts that disagree with the card script are rewritten.
Usage: python tools/design/gear_audit_fix.py [--db-dir DIR]
"""
import argparse
import json
import os
import pathlib
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import gearalts as g  # noqa: E402
import planlib as pl  # noqa: E402

GEAR = pl.ROOT / "legion" / "data" / "buildgear.json"
CARD_SLACK = 20
WHY = {4058: "-30% damage from Demi-Human and Player-Human monsters",
       4004: "DEX+1 HIT+3",
       4159: "AGI+2; FLEE+20 at refine above +8"}


def wear(item, keys):
    """a class can wear a piece when Jobs is empty, lists "All", or lists one of its job keys (raw item db dict or list)"""
    jobs = item.get("Jobs")
    if not jobs:
        return True
    have = {k for k, v in jobs.items() if v} if isinstance(jobs, dict) else set(jobs)
    return "All" in have or any(k in have for k in keys)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db-dir")
    a = ap.parse_args()
    db = a.db_dir
    if not db:
        db = tempfile.mkdtemp(prefix="gearfix_")
        g.fetch_extra(db)
    cat = g.Catalog(db)
    plans = pl.parse_builds()
    doc = json.load(open(GEAR, encoding="utf-8"))
    log = []

    def card_min_level(cid):
        it = cat.items[cid]
        lv = [cat.mobs[m]["Level"] for m, _, _ in cat.drops.get(it["AegisName"], [])
              if cat.is_dropper(cat.mobs.get(m))]
        return min(lv) if lv else None

    def card_fits(cid, slot):
        it = cat.items.get(cid)
        locs = it.get("Locations") or {}
        return it.get("Type") == "Card" and any(locs.get(k) for k in g.SLOT_LOCS[slot])

    def sourced_wearable(it, keys, lv_max, slot):
        return wear(it, keys) and it.get("EquipLevelMin", 0) <= lv_max and g._slot_ok(it, slot) \
            and cat.source(it)["how"] != "none"

    for bid, v in doc["builds"].items():
        p = plans[bid]
        second, first = pl.SECOND_JOB[p["cls"]], pl.FIRST_JOB[p["first"]]
        keys = g.job_keys_for(second, first)
        all_cards = [c for b in v["bands"] for c in (b.get("cards") or [])]
        for b in v["bands"]:
            mx = b["max"]
            before = {s: list(b.get(s) or []) for s in g.SLOT_ORDER}
            for s in g.SLOT_ORDER:
                keep = []
                for iid in b.get(s) or []:
                    it = cat.items[iid]
                    if not wear(it, keys) or it.get("EquipLevelMin", 0) > mx or not g._slot_ok(it, s):
                        log.append((bid, b["min"], mx, s, iid, it["Name"], "removed"))
                        continue
                    keep.append(iid)
                b[s] = keep
            if b.get("weapon") and cat.items[b["weapon"][-1]].get("Locations", {}).get("Both_Hand") and b.get("shield"):
                log.append((bid, b["min"], mx, "shield", b["shield"], "two-handed weapon", "shield cleared"))
                b["shield"] = []
            for s in ["weapon", "armor", "headgear", "garment", "shoes", "accessory"]:
                if before[s] and not b[s]:
                    pool = [it for it in cat.items.values() if it["Id"] not in before[s]
                            and sourced_wearable(it, keys, mx, s)]
                    pool.sort(key=lambda it: (-(it.get("Attack", 0) or it.get("Defense", 0)), it["Id"]))
                    if pool:
                        b[s] = [pool[0]["Id"]]
                        log.append((bid, b["min"], mx, s, pool[0]["Id"], pool[0]["Name"], "filled"))
                    else:
                        log.append((bid, b["min"], mx, s, None, None, "EMPTY SLOT: no sourced candidate"))
            if bid in ("bard", "bard_arrow", "dancer", "dancer_arrow"):
                sub = "Musical" if second == "Bard" else "Whip"
                if not any(cat.items[i].get("SubType") == sub for i in b.get("weapon") or []):
                    pool = [it for it in cat.items.values() if it.get("Type") == "Weapon"
                            and it.get("SubType") == sub and sourced_wearable(it, keys, mx, "weapon")]
                    pool.sort(key=lambda it: (-it.get("Attack", 0), it["Id"]))
                    if pool:
                        b["weapon"] = [pool[0]["Id"]] + b["weapon"]
                        log.append((bid, b["min"], mx, "weapon", pool[0]["Id"], pool[0]["Name"], "instrument/whip added"))
                    else:
                        log.append((bid, b["min"], mx, "weapon", None, sub, "NO sourced %s in the database" % sub))
            cards = []
            for c in b.get("cards") or []:
                ml = card_min_level(c["id"])
                if card_fits(c["id"], c["slot"]) and ml is not None and ml <= mx + CARD_SLACK:
                    cards.append(c)
                    continue
                log.append((bid, b["min"], mx, "card", c["id"], cat.items[c["id"]]["Name"],
                            "removed (min dropper lv %s)" % ml))
                have = [x["id"] for x in cards] + [x["id"] for x in b.get("cards") or []]
                fits = [o for o in all_cards if o["slot"] == c["slot"] and o["id"] not in have and card_fits(o["id"], o["slot"])
                        and card_min_level(o["id"]) is not None and card_min_level(o["id"]) <= mx + CARD_SLACK]
                if fits:
                    o = max(fits, key=lambda o: (o["prio"], -o["id"]))
                    cards.append(dict(o))
                    log.append((bid, b["min"], mx, "card", o["id"], cat.items[o["id"]]["Name"], "replacement"))
            b["cards"] = cards
    for v in doc["builds"].values():
        for b in v["bands"]:
            for c in b.get("cards") or []:
                if c["id"] in WHY:
                    c["why"] = WHY[c["id"]]
    doc["updated"] = ("2026-11-15 | build audit fixes (F2-F9, F19): class-wearable gear only, shield clear with two-hander, "
                      "bard/dancer instrument or whip, cards fit band (real droppers only), card text from scripts")
    with open(GEAR, "w", encoding="utf-8", newline="\n") as f:
        json.dump(doc, f, indent=1, ensure_ascii=False)
        f.write("\n")
    for line in log:
        print(*line, sep="\t")
    print(len(log), "changes")


if __name__ == "__main__":
    main()
