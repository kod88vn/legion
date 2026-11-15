"""Build plans for the dashboard's Builds tab (DB09).

Reads the engine's build plans from legion/builds.go, the gear bands from legion/data/buildgear.json, the family map
from legion/progress.go, the class names from legion/persona.go and the class docs from docs/classes/*.md, and writes
the static part to tools/dashboard/builds.json. Nothing about a build is typed in here: stats, skills, routes and
cards are parsed from the Go source and the gear table; names come from the item and skill tables the dashboard
already reads (items_lut.tsv, skills.tsv); every playstyle line carries its source (a doc line or a code file).

The live part (how many bots follow each build and where each one stands now) is added at serve time by
`view()`, from the newest DB snapshot and legion/data/personas.json. Read-only.

Usage (from the repo root):  python tools/dashboard/buildexport.py [--items PATH] [--skills PATH]
"""
import argparse
import datetime
import gzip
import json
import os
import re
import sys
import threading

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)   # dbsnap, the snapshot reader, sits beside this file
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
OUT = os.path.join(HERE, "builds.json")
BUILDS_GO = os.path.join(REPO, "legion", "builds.go")
GEAR_JSON = os.path.join(REPO, "legion", "data", "buildgear.json")
# the design draft (feat/build-design): per-band stats and skills, plan_check and draft playstyles. Read at export
# time, so a regenerated file (the plan rebalance) shows up on the next export. Every value from it is "draft".
MILESTONES_JSON = os.path.join(REPO, "legion", "data", "buildmilestones.json")
# HOOK for fix/gear-gate: when the engine writes its own milestone gear table (legion/data/buildmilestones_gear.json). Until the
# file exists the gear rows come from GEAR_JSON (buildgear.json). Expected shape, to confirm when it lands:
# {"builds": {id: {"ranges": [{"min": n, "max": n, "slots": {slot: [item ids]}}]}}}
ENGINE_GEAR_JSON = os.path.join(REPO, "legion", "data", "buildmilestones_gear.json")
STAT_ORDER_OUT = ["STR", "AGI", "VIT", "INT", "DEX", "LUK"]
DRAFT_NOTE = "draft for owner review"
PERSONAS = os.path.join(REPO, "legion", "data", "personas.json")
PROGRESS_GO = os.path.join(REPO, "legion", "progress.go")
PERSONA_GO = os.path.join(REPO, "legion", "persona.go")
DOCS_DIR = os.path.join(REPO, "docs", "classes")
ITEMS_TSV = os.path.join(REPO, "openkore-ayla", "logs", "items_lut.tsv")   # the item table the dashboard reads
SKILLS_TSV = os.path.join(REPO, "tools", ".state", "dbsnap", "skills.tsv")  # the skill table the dashboard reads
# fallbacks when the git-ignored tables above are missing: the client's item table and the tracked rAthena skill DB
CLIENT_ITEMS_LUA = os.environ.get("DASH_CLIENT_ITEMS") or r"G:\RO\ClientEN\SystemEN\LuaFiles514\itemInfo.lua"
SKILL_DB = os.path.join(REPO, "tools", "measure", "rd", "skill_db.yml")

STAT_KEYS = ["str", "agi", "vit", "int", "dex", "luk"]   # the char table columns, in builds.go iota order
GEAR_SLOTS = ["weapon", "armor", "shield", "headgear", "garment", "shoes", "accessory"]
ROLE_NAMES = ["tank / bait", "ranged", "support", "melee damage", "none"]

# where the engine's roles and tactics live (code), shown as the source of every party line
ROLE_TACTICS = {
    "tank": ("the tank leads: picks and pulls the target, peels monsters off squishy members", "legion/tanklead.go"),
    "ranged": ("hangs back on the tank's target: kites walkers, casts area spells on groups",
               "legion/kite.go, legion/casterkite.go, legion/aoe.go"),
    "support": ("heals and buffs the party from behind the tank", "legion/heal.go, legion/healreach.go, legion/healerstick.go"),
    "melee": ("assists the tank's target", "legion/tanklead.go"),
    "none": ("no party role", "legion/partyroles.go"),
}
ROLE_KEYS = ["tank", "ranged", "support", "melee", "none"]
MVP_SOURCE = "legion/bosshunt.go (party makeup, tactics and retreat; bossReady needs a tank and a healer)"
PVP_NOTE = ("no PvP playstyle exists in the code or the docs: legion/persona.go has only a flavour line "
            "(\"lives for WoE\") and legion/guilds.go names War of Emperium as a goal")

_lock = threading.Lock()
_cache = {}


# ----------------------------------------------------------------- Go source readers

def _strip_comment(line):
    """drop a Go // comment that is not inside a string"""
    out, q = [], False
    i = 0
    while i < len(line):
        c = line[i]
        if c == '"' and (i == 0 or line[i - 1] != "\\"):
            q = not q
        if not q and line.startswith("//", i):
            break
        out.append(c)
        i += 1
    return "".join(out).rstrip()


def _ints(s):
    return [int(x) for x in re.findall(r"-?\d+", s)]


def evaluate(expr, names):
    """a builds.go value: "str", int, []int{...}, st(...)/sk(...) pairs, append(append([]int{}, A...), B...), a name"""
    e = expr.strip().rstrip(",").strip()
    if e.startswith('"') and e.endswith('"'):
        return e[1:-1]
    if re.fullmatch(r"-?\d+", e):
        return int(e)
    m = re.fullmatch(r"append\(append\(\[\]int\{\}, (\w+)\.\.\.\), (\w+)\.\.\.\)", e)
    if m:
        return list(names[m.group(1)]) + list(names[m.group(2)])
    m = re.fullmatch(r"\[\]int\{([^}]*)\}", e)
    if m:
        return _ints(m.group(1))
    m = re.fullmatch(r"(st|sk)\((.*)\)", e)
    if m:
        # st(stSTR, 20, ...): a stat constant reads as its index in the iota order (STAT_ORDER)
        v = []
        for tok in m.group(2).split(","):
            sm = re.fullmatch(r"\s*st([A-Z]{3})\s*", tok)
            v.append(STAT_ORDER.index(sm.group(1)) if sm else int(tok))
        return [(v[i], v[i + 1]) for i in range(0, len(v) - 1, 2)]
    if re.fullmatch(r"\w+", e) and e in names:
        return names[e]
    raise ValueError("builds.go: cannot read %r" % expr)


def parse_names(src):
    """the package-level routes (armorLight, extrasMelee, noviceSkills, ...) as values"""
    names = {}
    lines = [_strip_comment(l) for l in src.splitlines()]
    for line in lines:
        m = re.match(r"^\s*(?:var\s+)?([a-z]\w*)\s*=\s*(\[\]int|sk\(|st\(|append\()", line)
        if m:
            try:
                names[m.group(1)] = evaluate(line.split("=", 1)[1], names)
            except ValueError:
                pass
    return names


STAT_ORDER = []


def parse_stat_order(src):
    """stSTR, stAGI, ... as consts (the iota order); kept in STAT_ORDER for the st(...) values"""
    m = re.search(r"const \(\n(\s*st[A-Z]{3}.*?)\n\)", src, re.S)
    order = re.findall(r"^\s*st([A-Z]{3})\b", m.group(1), re.M) if m else []
    STAT_ORDER[:] = order
    return order


def parse_class_names(src):
    m = re.search(r"var classNames = map\[int\]string\{(.*?)\n\}", src, re.S)
    if not m:
        return {}
    return {int(k): v for k, v in re.findall(r'(\d+):\s*"([^"]+)"', m.group(1))}


def parse_families(src):
    """progress.go family(): class id -> family name (famSword, famArcher, ...)"""
    out = {}
    for fam, ids in re.findall(r"add\((fam\w+)((?:,\s*\d+)+)\)", src):
        for i in _ints(ids):
            out[i] = fam
    return out


FIELD = re.compile(r"\b(ID|Base|Name|Class|First|Branch|LegacyStats|Legacy|Stats|Skills|Weapons|Armors|Extras|Summary):\s*")


def parse_builds(src, names):
    """the `builds` map: {id: {field: value}} in source order"""
    m = re.search(r"\nvar builds = map\[string\]\*Build\{\n(.*?)\n\}\n", src, re.S)
    if not m:
        raise ValueError("builds.go: no `var builds` map")
    entries, cur = [], None
    for raw in m.group(1).split("\n"):
        line = _strip_comment(raw)
        head = re.match(r'^\t"(\w+)": \{', line)
        if head:
            cur = [head.group(1), line[line.index("{") + 1:]]   # the text after the entry's opening brace
            entries.append(cur)
        elif cur is not None and line.strip():
            cur[1] += " " + line.strip()
    out = {}
    for bid, body in entries:
        body = re.sub(r"\}\s*,?\s*$", "", body.strip())   # the entry's closing brace
        spots = list(FIELD.finditer(body))
        fields = {}
        for i, f in enumerate(spots):
            end = spots[i + 1].start() if i + 1 < len(spots) else len(body)
            fields[f.group(1)] = evaluate(body[f.end():end].strip().rstrip(",").strip(), names)
        out[bid] = {"fields": fields}
    return out


# ----------------------------------------------------------------- docs

def parse_docs(docs_dir):
    """plan id -> {"source": "docs/classes/x.md:line", "bullets": [(label, text, line)]} from the class docs' headings"""
    out = {}
    if not os.path.isdir(docs_dir):
        return out
    for fn in sorted(os.listdir(docs_dir)):
        if not fn.endswith(".md"):
            continue
        rel = "docs/classes/" + fn
        cur = None
        with open(os.path.join(docs_dir, fn), encoding="utf-8") as f:
            lines = f.read().split("\n")
        for n, line in enumerate(lines, 1):
            if line.startswith("#"):
                cur = None
                m = re.search(r"\(plan `([a-z_]+)`", line)
                if m and line.startswith("###"):
                    cur = out.setdefault(m.group(1), {"bullets": []})
                    cur["source"] = "%s:%d" % (rel, n)
                    cur["heading"] = line.strip("# ").strip()
                continue
            if cur is None:
                continue
            b = re.match(r"^- \*\*(.+?):\*\*\s*(.*)$", line)
            if b:
                cur["bullets"].append([b.group(1).strip().lower(), b.group(2).strip(), n])
            elif line.startswith("  ") and cur["bullets"] and line.strip():
                cur["bullets"][-1][1] += " " + line.strip()
    return out


# ----------------------------------------------------------------- names and gear tables

# a solo sentence counts only when it says how the build fights or farms alone (not where it travels, what it sells
# or buys, or who sources from it): it needs a fight or farm word and at least three words after "solo"
SOLO_FIGHT = re.compile(r"\b(fight|farm|kill|attack|hit|cast|skill|monster|boss|heal|buff|target|SP|HP|walker|swarm|"
                        r"crowd|group|spell|strafe|combo|tank|damage|pull|hunt)\w*", re.I)
SOLO_SKIP = re.compile(r"\b(wing|trip|errand|economy|sourc\w*|seller|buy\w*|shop\w*|farmers|come from|guild)\b", re.I)


def solo_clauses(text):
    """the clauses of a doc bullet that describe solo fighting or farming: from the word "solo" to the end of its sentence"""
    out = []
    for m in re.finditer(r"\bsolo\b", text, re.I):
        end = len(text)
        for sep in (";", ". "):
            i = text.find(sep, m.start())
            if i != -1:
                end = min(end, i)
        clause = text[m.start():end].strip(" ,.)(")
        if len(clause.split()[1:]) < 3 or SOLO_SKIP.search(clause) or not SOLO_FIGHT.search(clause):
            continue
        if clause not in out:
            out.append(clause)
    return out


def load_items_lua(path):
    """the client's item table (LuaFiles514/itemInfo.lua): [id] = { ... identifiedDisplayName = "..." }"""
    out, cur = {}, None
    try:
        f = open(path, encoding="utf-8", errors="replace")
    except OSError:
        return out
    with f:
        for line in f:
            m = re.match(r"^\t\[(\d+)\] = \{", line)
            if m:
                cur = int(m.group(1))
                continue
            m = re.match(r'^\t\tidentifiedDisplayName = "(.*)",\s*$', line)
            if m and cur is not None and cur not in out:
                out[cur] = m.group(1)
    return out


def load_skill_db(path):
    """the rAthena skill database (tools/measure/rd/skill_db.yml, tracked): id -> Description (the display name)"""
    out, cur = {}, None
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                m = re.match(r"^  - Id:\s*(\d+)\s*$", line)
                if m:
                    cur = int(m.group(1))
                    continue
                m = re.match(r"^    Description:\s*(.+?)\s*$", line)
                if m and cur is not None and cur not in out:
                    out[cur] = m.group(1)
    except OSError:
        pass
    return out


def load_items(path):
    out = {}
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 2 and parts[0].isdigit():
                    out[int(parts[0])] = parts[1]
    except OSError:
        pass
    return out


def load_skills(path):
    out = {}
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                parts = line.rstrip("\n").split("\t")
                if len(parts) >= 3 and parts[0].isdigit():
                    out[int(parts[0])] = parts[2]
    except OSError:
        pass
    return out


def _count_unnamed(doc):
    """how many names in the export are placeholders ("#id ...") because no table had the id"""
    n = 0
    stack = [doc["builds"]]
    while stack:
        cur = stack.pop()
        if isinstance(cur, dict):
            stack.extend(cur.values())
        elif isinstance(cur, list):
            stack.extend(cur)
        elif isinstance(cur, str) and cur.startswith("#") and "no name" in cur:
            n += 1
    return n


def item_name(items, i):
    return items.get(i) or "#%d (no name in the local item table)" % i


def load_json_or_none(path):
    """a JSON file read at export time; None when it is missing or unreadable"""
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def band_for(bands, rmin, rmax):
    """the gear band that overlaps a level range the most (None when no band overlaps it)"""
    best, best_n = None, 0
    for bd in bands:
        n = min(bd["max"], rmax) - max(bd["min"], rmin) + 1
        if n > best_n:
            best, best_n = bd, n
    return best


def part_counts(builds):
    """per playstyle part (solo, MVP, PvP): how many builds are defined in code or docs, how many are draft only
    (the design draft, owner review), and how many have no text at all"""
    out = {}
    for part in ("solo", "mvp", "pvp"):
        c = {"defined": 0, "draft": 0, "gap": 0}
        for b in builds:
            st = b["playstyle"][part]["status"]
            c[st] = c.get(st, 0) + 1
        out[part] = c
    return out


def plan_warnings(pc):
    """the warnings a draft plan_check shows: points the plan needs against what exists, and goals never reached"""
    out = []
    if not pc:
        return out
    cost, avail = pc.get("stat_points_goals_cost"), pc.get("stat_points_at_99")
    if cost is not None and avail is not None and cost > avail:
        out.append("stat goals need %d points; the plan has %d at level 99" % (cost, avail))
    if pc.get("stat_goals_unreached"):
        out.append("stat goals never reached at 99: %s" % ", ".join(pc["stat_goals_unreached"]))
    for s in pc.get("skills_unreached_at_99") or []:
        out.append("skill never reached at 99: %s (wants %s, reaches %s)" % (s["name"], s["want"], s["reached"]))
    listed, reach = pc.get("skill_points_listed"), pc.get("skill_points_reachable")
    if listed is not None and reach is not None and listed > reach:
        out.append("skill rows list %d points, %d are reachable" % (listed, reach))
    return out


def gear_table(gear, bid, base):
    """the engine's gear table for a build: its own, else its base build's (buildgear.go falls back to Base)"""
    b = gear.get("builds") or {}
    if bid in b:
        return bid, b[bid]
    if base and base in b:
        return base, b[base]
    return None, None


# ----------------------------------------------------------------- the export

def build_export(items, skills, paths=None):
    p = dict(BUILDS_GO=BUILDS_GO, GEAR_JSON=GEAR_JSON, PROGRESS_GO=PROGRESS_GO, PERSONA_GO=PERSONA_GO, DOCS_DIR=DOCS_DIR,
             MILESTONES=MILESTONES_JSON, ENGINE_GEAR=ENGINE_GEAR_JSON)
    p.update(paths or {})
    ms_all = load_json_or_none(p["MILESTONES"]) or {}
    ms_builds = ms_all.get("builds") or {}
    engine_gear = load_json_or_none(p["ENGINE_GEAR"])   # the hook (inert until the engine file exists)
    engine_gear_builds = (engine_gear or {}).get("builds") or {}
    with open(p["BUILDS_GO"], encoding="utf-8") as f:
        src = f.read()
    with open(p["PROGRESS_GO"], encoding="utf-8") as f:
        fam_of = parse_families(f.read())
    with open(p["PERSONA_GO"], encoding="utf-8") as f:
        cls_names = parse_class_names(f.read())
    with open(p["GEAR_JSON"], encoding="utf-8") as f:
        gear = json.load(f)
    docs = parse_docs(p["DOCS_DIR"])
    stat_order = parse_stat_order(src)
    names = parse_names(src)
    raw = parse_builds(src, names)
    if not raw:
        raise ValueError("builds.go: no builds parsed")
    def cname(i):
        return cls_names.get(i, "class %d" % i)

    builds = []
    gaps = []
    for bid, rec in raw.items():
        f = rec["fields"]
        cls = f["Class"]
        stats = [(stat_order[s], v) for s, v in f.get("Stats", [])]
        end_stats = {}
        for s, v in stats:
            end_stats[s] = max(end_stats.get(s, 0), v)
        end_skills = {}
        for sid, lv in f.get("Skills", []):
            end_skills[sid] = max(end_skills.get(sid, 0), lv)
        # party role, mirrored from partyroles.go partyRole() (the family and the thief's AGI rule)
        fam = fam_of.get(cls, "famNovice")
        if cls in (15, 4016):
            role = "tank"
            why = "Monk / Champion: frontline (partyRole case 15, 4016)"
        elif fam == "famSword":
            role, why = "tank", "Swordman line: tank (partyRole)"
        elif fam in ("famArcher", "famMage"):
            role, why = "ranged", "Archer / Mage line: ranged (partyRole)"
        elif fam == "famAcolyte":
            role, why = "support", "Acolyte line: support (partyRole)"
        elif fam == "famThief":
            agi, strn = end_stats.get("AGI", 0), end_stats.get("STR", 0)
            if agi >= 2 * strn or agi >= 30:
                role, why = "tank", "Thief line with AGI >= 2 x STR or AGI >= 30: evasion tank (partyRole)"
            else:
                role, why = "melee", "Thief line, STR build: melee damage (partyRole)"
        elif fam == "famMerchant":
            role, why = "melee", "Merchant line: melee damage (partyRole)"
        else:
            role, why = "none", "no family (partyRole returns roleNone)"

        # gear: the engine's table (own or the base's), top band = end goal, cards from that band
        table_id, table = gear_table(gear, bid, f.get("Base", ""))
        bands = []
        end_gear, cards = {}, []
        if table:
            for bd in table.get("bands", []):
                row = {"min": bd.get("min"), "max": bd.get("max"),
                       "slots": {s: [item_name(items, i) for i in bd.get(s, [])] for s in GEAR_SLOTS if bd.get(s)},
                       "cards": [{"name": item_name(items, c["id"]), "slot": c.get("slot"), "prio": c.get("prio"),
                                  "why": c.get("why", "")} for c in bd.get("cards", [])],
                       "source": "legion/data/buildgear.json builds.%s band %s-%s" % (table_id, bd.get("min"), bd.get("max"))}
                bands.append(row)
            top = max(table.get("bands", []), key=lambda b: b.get("max", 0)) if table.get("bands") else {}
            for s in GEAR_SLOTS:
                if top.get(s):
                    end_gear[s] = item_name(items, top[s][-1])
            cards = [{"name": item_name(items, c["id"]), "slot": c.get("slot"), "prio": c.get("prio"),
                      "why": c.get("why", "")} for c in top.get("cards", [])]
            gear_src = "legion/data/buildgear.json builds.%s, top band %s-%s" % (
                table_id, top.get("min"), top.get("max")) + ("" if table_id == bid else " (falls back to %s)" % table_id)
        else:
            gear_src = "no buildgear.json table: the builds.go route only"
        route = {"weapons": [item_name(items, i) for i in f.get("Weapons", [])],
                 "armors": [item_name(items, i) for i in f.get("Armors", [])],
                 "extras": [item_name(items, i) for i in f.get("Extras", [])]}
        if not end_gear:
            if route["weapons"]:
                end_gear["weapon"] = route["weapons"][-1]
            if route["armors"]:
                end_gear["armor"] = route["armors"][-1]

        # playstyle, four parts, each with its source
        doc = docs.get(bid)
        tactic, tactic_src = ROLE_TACTICS[role]
        party = {"status": "defined", "lines": [
            {"text": "engine role: %s (%s)" % (ROLE_NAMES[ROLE_KEYS.index(role)], why),
             "source": "legion/partyroles.go partyRole()"},
            {"text": "tactics: " + tactic, "source": tactic_src}], "note": ""}
        mvp = {"status": "gap", "lines": [], "note": "no build-specific boss role in docs/classes/*.md; the generic boss "
                                                     "hunt applies (" + MVP_SOURCE + ")"}
        solo = {"status": "gap", "lines": [], "note": "no solo-farming line for this build in docs/classes/*.md; "
                                                      "only the rules for every class (docs/classes/all.md)"}
        pvp = {"status": "gap", "lines": [], "note": PVP_NOTE}
        if doc:
            for label, text, n in doc["bullets"]:
                src_ref = "%s:%d" % (doc["source"].split(":")[0], n)
                if label == "party role":
                    party["lines"].insert(0, {"text": text, "source": src_ref})
                if label == "boss role":
                    mvp = {"status": "defined", "lines": [{"text": text, "source": src_ref}], "note": ""}
                for clause in solo_clauses(text):   # every solo sentence the docs give, from any bullet of the section
                    solo["lines"].append({"text": "%s: %s" % (label, clause), "source": src_ref})
            if solo["lines"]:
                solo = {"status": "defined", "lines": solo["lines"], "note": ""}
        playstyle = {"solo": solo, "party": party, "mvp": mvp, "pvp": pvp}
        draft = ms_builds.get(bid) or {}
        for part in ("solo", "mvp", "pvp"):
            # a gap is filled by the design draft's text, labelled draft; the doc or code line always wins
            if playstyle[part]["status"] == "gap" and (draft.get("playstyle") or {}).get(part):
                playstyle[part] = {"status": "draft", "note": DRAFT_NOTE, "lines": [
                    {"text": t, "source": "draft: legion/data/buildmilestones.json builds.%s.playstyle.%s[%d]" % (bid, part, i)}
                    for i, t in enumerate(draft["playstyle"][part])]}
        for part, val in playstyle.items():
            if val["status"] == "gap":
                gaps.append({"build": bid, "part": part, "note": val["note"]})
        pc = draft.get("plan_check")
        plan_check = ({"status": "draft", "note": DRAFT_NOTE, "warnings": plan_warnings(pc), "figures": pc,
                       "source": "draft: legion/data/buildmilestones.json builds.%s.plan_check" % bid}
                      if pc else {"status": "gap", "note": "no plan_check in the design draft", "warnings": []})

        # the engine's middle steps, in the order the plan uses them
        stat_steps = [{"label": "%s %d" % (s, v)} for s, v in stats]
        skill_steps = [{"id": sid, "name": skills.get(sid, "skill %d" % sid), "lv": lv} for sid, lv in f.get("Skills", [])]
        # EC39 milestones: one row per gear band of the engine's table (the levels the engine buys gear for). The
        # engine's stat and skill steps have no level (builds.go planStat / planSkill pick the next unmet goal), so
        # those are gaps in every row, never invented.
        # gear rows: the engine's own milestone gear when its file exists (hook ENGINE_GEAR_JSON), else buildgear.json
        eng = engine_gear_builds.get(bid)
        if eng and eng.get("ranges"):
            gear_rows = [{"min": r["min"], "max": r["max"], "cards": [],
                          "slots": {s: [item_name(items, i) for i in ids] for s, ids in (r.get("slots") or {}).items() if ids}}
                         for r in eng["ranges"]]
            row_gear_src = "engine milestone gear (legion/data/buildmilestones_gear.json)"
        else:
            gear_rows, row_gear_src = bands, gear_src

        def gear_of(bd):
            return {"slots": {s: v[-1] for s, v in bd["slots"].items()}, "options": bd["slots"],
                    "source": bd.get("source", row_gear_src)}

        milestones = []
        if draft.get("ranges"):
            # stats and skills per level range come from the design draft (labelled draft); gear from the band overlapping it
            for r in draft["ranges"]:
                rng = r["range"]
                bd = band_for(gear_rows, r["min"], r["max"])
                gear = ({"status": "ok", **gear_of(bd), "cards": bd["cards"]} if bd
                        else {"status": "gap", "slots": {}, "options": {}, "cards": [],
                              "note": "no gear band overlaps levels %s" % rng, "source": row_gear_src})
                milestones.append({
                    "range": rng, "min": r["min"], "max": r["max"], "class_at_end": r.get("class_at_end", ""),
                    "gear": gear,
                    "cards": bd["cards"] if bd else [],
                    "stats": {"status": "draft", "note": DRAFT_NOTE, "values": {s: (r.get("stats") or {}).get(s) for s in STAT_ORDER_OUT},
                              "points": {"available": r.get("stat_points_available"), "used": r.get("stat_points_used"),
                                         "spare": r.get("stat_points_spare")},
                              "source": "draft: legion/data/buildmilestones.json builds.%s.ranges[%s].stats" % (bid, rng)},
                    "skills": {"status": "draft", "note": DRAFT_NOTE, "items": r.get("skills") or [],
                               "unspent": r.get("skill_points_unspent"),
                               "source": "draft: legion/data/buildmilestones.json builds.%s.ranges[%s].skills" % (bid, rng)},
                })
        else:
            for bd in gear_rows:
                rng = "%s-%s" % (bd["min"], bd["max"])
                milestones.append({
                    "range": rng, "min": bd["min"], "max": bd["max"], "class_at_end": "",
                    "gear": dict(gear_of(bd), status="ok"), "cards": bd["cards"],
                    "stats": {"status": "gap", "value": None,
                              "note": "no level milestone for stats in the design draft or the engine"},
                    "skills": {"status": "gap", "value": None,
                               "note": "no level milestone for skills in the design draft or the engine"},
                })
        builds.append({
            "id": bid, "base": f.get("Base", ""), "name": f.get("Name", bid), "summary": f.get("Summary", ""),
            "milestones": milestones,
            "plan_check": plan_check,
            "class": {"id": cls, "name": cname(cls), "first": cname(f.get("First", 0)),
                      "first_id": f.get("First", 0), "branch": f.get("Branch", 0),
                      "line": "%s (1st job) -> %s (2nd job, end goal)" % (cname(f.get("First", 0)), cname(cls))},
            "end": {
                "gear": {"slots": end_gear, "cards": cards, "source": gear_src},
                "stats_note": "stat goals are reached in order (see steps.stats); the top goal is the end",
                "stats": [{"stat": s, "value": end_stats[s]} for s in stat_order if s in end_stats],
                "skills": [{"id": sid, "name": skills.get(sid, "skill %d" % sid), "lv": lv}
                           for sid, lv in sorted(end_skills.items())],
                "role": role,
            },
            "steps": {"stats": stat_steps, "skills": skill_steps,
                      "gear": [{"min": b["min"], "max": b["max"], "slots": b["slots"]} for b in bands],
                      "routes": route, "weapon_ids": list(f.get("Weapons", []))},
            "playstyle": playstyle,
            "source": {"builds": "legion/builds.go builds[\"%s\"]" % bid,
                       "docs": doc["source"] if doc else None},
        })
    gaps.sort(key=lambda g: (g["build"], g["part"]))
    return {
        "generated": datetime.datetime.now().isoformat(timespec="seconds"),
        "generated_epoch": int(datetime.datetime.now().timestamp()),
        "sources": {"builds": "legion/builds.go", "gear": "legion/data/buildgear.json", "families": "legion/progress.go",
                    "classes": "legion/persona.go", "docs": "docs/classes/*.md", "items": "openkore-ayla/logs/items_lut.tsv",
                    "skills": "tools/.state/dbsnap/skills.tsv"},
        "item_names": len(items), "skill_names": len(skills),
        "transcendent": ("Transcendent classes (already-reborn characters only): a 4th-job or transcendent character "
                         "keeps the plan of its 2nd job (builds.go transToSecond); ST01 caps the server at 2nd classes."),
        "part_counts": part_counts(builds),
        "draft": {"status": DRAFT_NOTE, "source": "legion/data/buildmilestones.json",
                  "file_status": ms_all.get("status"), "drafted": ms_all.get("drafted"),
                  "found": bool(ms_builds)},
        "builds": builds, "gaps": gaps,
    }


def _show(path):
    """a path for the export: relative to the repo when it is inside it"""
    try:
        return os.path.relpath(path, REPO)
    except ValueError:   # another drive
        return path


def lookup_names(items_path=ITEMS_TSV, skills_path=SKILLS_TSV, client_items=CLIENT_ITEMS_LUA, skill_db=SKILL_DB):
    """(items, skills, sources): the dashboard's tables first, the fallbacks for any id they lack"""
    items, skills = load_items(items_path), load_skills(skills_path)
    src = {"items": _show(items_path) if items else None,
           "skills": _show(skills_path) if skills else None}
    if os.path.exists(client_items):
        for i, n in load_items_lua(client_items).items():
            items.setdefault(i, n)
        src["items_fallback"] = "client itemInfo.lua"
    if os.path.exists(skill_db):
        for i, n in load_skill_db(skill_db).items():
            skills.setdefault(i, n)
        src["skills_fallback"] = "tools/measure/rd/skill_db.yml"
    return items, skills, src


def export(out=OUT, items_path=ITEMS_TSV, skills_path=SKILLS_TSV, paths=None):
    items, skills, src = lookup_names(items_path, skills_path)
    doc = build_export(items, skills, paths)
    doc["name_sources"] = src
    doc["unnamed"] = _count_unnamed(doc)
    tmp = out + ".tmp"
    with open(tmp, "w", encoding="utf-8", newline="\n") as f:   # LF on every platform
        json.dump(doc, f, ensure_ascii=False, indent=1)
    os.replace(tmp, out)
    return doc


# ----------------------------------------------------------------- live counts (serve time)

def _snapshot(path):
    import dbsnap
    t = os.path.getmtime(path)
    with _lock:
        hit = _cache.get(path)
        if hit and hit[0] == t:
            return hit[1]
    snap = dbsnap.Snapshot(path)
    with _lock:
        _cache[path] = (t, snap)
    return snap


def live_counts(doc, snap_path, personas_path):
    """per build: the bots on it (personas.json plan) and where each stands in the snapshot. Read-only."""
    with open(personas_path, encoding="utf-8") as f:
        personas = json.load(f)
    snap = _snapshot(snap_path)
    plan_of = {}
    for pe in personas:
        for c in pe.get("characters") or []:
            if c.get("plan"):
                plan_of[c["char_id"]] = c["plan"]
    out = {}
    for b in doc["builds"]:
        bid = b["id"]
        rows = [cid for cid, pl in plan_of.items() if pl == bid and cid in snap.chars]
        res = {"bots": len(rows), "online": 0, "snapshot": snap.t,
               "stat_steps": [{"label": s["label"], "bots": 0} for s in b["steps"]["stats"]], "stat_done": 0,
               "skill_steps": [{"name": s["name"], "lv": s["lv"], "bots": 0} for s in b["steps"]["skills"]],
               "skill_done": 0, "gear_steps": {}}
        weapon_ids = b["steps"]["weapon_ids"]
        for cid in rows:
            ch = snap.chars[cid]
            res["online"] += 1 if ch.get("online") else 0
            # stat step: the first milestone not yet reached (the plan's order); done when all are reached
            stat_goals = b["steps"]["stats"]
            cur = None
            for i, s in enumerate(stat_goals):
                label = s["label"].split()
                key = label[0].lower()
                if (ch.get(key) or 0) < int(label[1]):
                    cur = i
                    break
            if cur is None:
                res["stat_done"] += 1
            else:
                res["stat_steps"][cur]["bots"] += 1
            have = {}
            for sk in snap.by_char.get("skill", {}).get(cid, []):
                have[sk["id"]] = sk["lv"]
            cur = None
            for i, s in enumerate(b["steps"]["skills"]):
                if have.get(s["id"], 0) < s["lv"]:
                    cur = i
                    break
            if cur is None:
                res["skill_done"] += 1
            else:
                res["skill_steps"][cur]["bots"] += 1
            # gear step: the highest weapon of the route that is equipped
            equipped = {r.get("nameid") for r in snap.by_char.get("inventory", {}).get(cid, []) if r.get("equip")}
            top = None
            for i, nid in enumerate(weapon_ids):
                if nid in equipped:
                    top = i
            key = b["steps"]["routes"]["weapons"][top] if top is not None else "none of the route"
            res["gear_steps"][key] = res["gear_steps"].get(key, 0) + 1
        out[bid] = res
    return out


def view(out=OUT, snap_dir=None, personas=PERSONAS):
    """the Builds tab's data: the export as written, plus the live counts from the newest snapshot"""
    with open(out, encoding="utf-8") as f:
        doc = json.load(f)
    import dbsnap
    last = dbsnap.latest(snap_dir or os.path.join(REPO, "tools", ".state", "dbsnap"))
    live = {"snapshot": None, "snapshot_t": None, "per_build": {}, "error": None}
    if last:
        live["snapshot"], live["snapshot_t"] = os.path.basename(last), _snapshot(last).t
        try:
            live["per_build"] = live_counts(doc, last, personas)
        except (OSError, ValueError, KeyError) as e:
            live["error"] = "live counts unavailable: %s" % e
    else:
        live["error"] = "no DB snapshot yet"
    doc = dict(doc)
    doc["live"] = live
    return doc


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--items", default=ITEMS_TSV)
    ap.add_argument("--skills", default=SKILLS_TSV)
    ap.add_argument("--out", default=OUT)
    a = ap.parse_args(argv)
    doc = export(a.out, a.items, a.skills)
    print("wrote %s: %d builds, %d gaps" % (a.out, len(doc["builds"]), len(doc["gaps"])))
    return 0


if __name__ == "__main__":
    sys.exit(main())
