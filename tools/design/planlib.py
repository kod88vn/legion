"""Shared helpers for the build milestone design: parses legion/builds.go plans and the server's skill tree.
Used by tools/design/gen_milestones.py (generator) and tests/test_buildmilestones.py."""
import re, pathlib

ROOT = pathlib.Path(__file__).resolve().parents[2]
SERVER_DB = ROOT / "tests" / "tools" / "fixtures" / "server"   # committed copies of rathena db/re (rule G23)
CHANGE2 = 40        # the engine changes to the 2nd job at job level 40 (legion.txt)
NOVICE_POINTS = 9   # Basic Skill (job 10 gives 9 points)
STATS = ["STR", "AGI", "VIT", "INT", "DEX", "LUK"]
STAT_IDX = {"stSTR": 0, "stAGI": 1, "stVIT": 2, "stINT": 3, "stDEX": 4, "stLUK": 5}


def stat_points(level):
    """Total stat points at a base level (statpoint.yml, same formula as persona.go statPoints)."""
    t = 48
    for l in range(1, level):
        t += l // 5 + 3
    return t


def stat_cost(v):
    """Points to raise a stat from v to v+1."""
    return (v - 1) // 10 + 2


def spent(stats):
    """Points needed to reach the given stat values from 1 each."""
    return sum(sum(stat_cost(v) for v in range(1, x)) for x in stats)


def parse_builds(path=None):
    """{id: {base, name, class, first, branch, stats: [(statidx, value)], skills: [(id, lv)]}} from builds.go."""
    src = pathlib.Path(path or ROOT / "legion" / "builds.go").read_text(encoding="utf-8")
    out = {}
    for m in re.finditer(r'^\t"([a-z_]+)": \{ID: "([^"]+)"(.*?)^\t\t(?:Summary:)', src, re.S | re.M):
        bid, body = m.group(1), m.group(3)
        head = body.split("\n", 1)[0]
        base = re.search(r'Base: "([^"]*)"', head)
        cls = int(re.search(r'Class: (\d+)', head).group(1))
        first = int(re.search(r'First: (\d+)', head).group(1))
        name = re.search(r'Name: "([^"]*)"', head).group(1)
        sm = re.search(r'^\t\tStats: *st\(([^)]*)\)', body, re.M)
        km = re.search(r'^\t\tSkills: *sk\(([^)]*)\)', body, re.M)
        st = [x.strip() for x in sm.group(1).split(",")]
        stats = [(STAT_IDX[st[i]], int(st[i + 1])) for i in range(0, len(st), 2)]
        kk = [int(x) for x in km.group(1).replace(" ", "").split(",")]
        skills = [(kk[i], kk[i + 1]) for i in range(0, len(kk), 2)]
        out[bid] = dict(id=bid, base=base.group(1) if base else "", name=name, cls=cls, first=first,
                        stats=stats, skills=skills)
    return out


def build_ids(path=None):
    """Every build ID in builds.go (a plain key scan, independent of the plan parser)."""
    src = pathlib.Path(path or ROOT / "legion" / "builds.go").read_text(encoding="utf-8")
    return re.findall(r'^\t"([a-z_]+)": \{ID: "', src, re.M)


# ---------------------------------------------------------------- server skill tree (read from the rAthena db)
FIRST_JOB = {1: "Swordman", 2: "Mage", 3: "Archer", 4: "Acolyte", 5: "Merchant", 6: "Thief"}
SECOND_JOB = {7: "Knight", 8: "Priest", 9: "Wizard", 10: "Blacksmith", 11: "Hunter", 12: "Assassin", 14: "Crusader",
              15: "Monk", 16: "Sage", 17: "Rogue", 18: "Alchemist", 19: "Bard", 20: "Dancer"}


def server_tree(db=None):
    """Tree of the committed server fixtures (default tests/tools/fixtures/server)."""
    d = pathlib.Path(db or SERVER_DB)
    return Tree(d / "skill_tree.yml", d / "skill_db.yml", d / "job_exp.yml")


class Tree:
    """Skill tree of the server (db/re/skill_tree.yml + skill_db.yml + job_exp.yml), passed as file paths."""

    def __init__(self, tree_yml, skill_db_yml, job_exp_yml):
        import yaml
        L = getattr(yaml, "CSafeLoader", yaml.SafeLoader)
        self.id2name = {}
        for s in yaml.load(open(skill_db_yml, encoding="utf-8"), Loader=L)["Body"]:
            self.id2name[s["Id"]] = s["Name"]
        self.name2id = {v: k for k, v in self.id2name.items()}
        self.jobs = {j["Job"]: j for j in yaml.load(open(tree_yml, encoding="utf-8"), Loader=L)["Body"]}
        self.maxjob = {}
        for g in yaml.load(open(job_exp_yml, encoding="utf-8"), Loader=L)["Body"]:
            for j in g["Jobs"]:
                self.maxjob[j] = g.get("MaxJobLevel", 99)

    def own(self, job):
        out = {}
        for t in (self.jobs.get(job, {}).get("Tree") or []):
            out[t["Name"]] = t
        return out

    def stage_tree(self, job2):
        """{aegis name: (stage, maxlevel, [(req name, lv)])}; stage 0 novice, 1 first job, 2 second job."""
        res = {}
        first = [f for f in FIRST_JOB.values() if f in (self.jobs[job2].get("Inherit") or {})]
        for n, t in self.own("Novice").items():
            res[n] = (0, t["MaxLevel"], [(r["Name"], r["Level"]) for r in (t.get("Requires") or [])])
        for f in first:
            for n, t in self.own(f).items():
                res[n] = (1, t["MaxLevel"], [(r["Name"], r["Level"]) for r in (t.get("Requires") or [])])
        for n, t in self.own(job2).items():
            res[n] = (2, t["MaxLevel"], [(r["Name"], r["Level"]) for r in (t.get("Requires") or [])])
        return res


def job_points(base_level, cap2, change2=CHANGE2):
    """(novice, first, second) skill points the character has at a base level (assumed pace: Novice job 10 by base 10,
    first job 40 at base 40, then second job level = base - 39; the engine changes job at job level 10 / 40). The job
    level resets at each job change, so each stage's points count from 1 again."""
    nov = min(NOVICE_POINTS, max(0, base_level - 1))
    if base_level <= 10:
        first = 0
    elif base_level >= change2:
        first = change2 - 1
    else:
        first = round((change2 - 1) * (base_level - 10) / (change2 - 10))
    second = 0 if base_level <= change2 else max(0, min(cap2 - 1, base_level - change2))
    return nov, first, second


def skill_lock(change2=CHANGE2):
    """Spent skill points needed before any 2nd-job skill can be learned: player_skillup_limit (pc.cpp
    pc_calc_skilltree_normalize_job_sub) keeps the 2nd class locked while (spent - 9 Novice) < change_level_2nd - 1."""
    return NOVICE_POINTS + (change2 - 1)


def sim_stats(goals, level):
    """Spend the points of a base level on the ordered goals (the engine's planStat). Returns (stats[6], spare)."""
    st = [1] * 6
    pts = stat_points(level)
    for s, v in goals:
        v = min(v, 99)
        while st[s] < v and stat_cost(st[s]) <= pts:
            pts -= stat_cost(st[s])
            st[s] += 1
        if st[s] < v:
            break  # the engine stalls on the first goal it cannot afford
    return st, pts


def sim_skills(plan, tree, level, noviceSkills=((1, 9),), log=None):
    """Skill levels reached at a base level when the engine follows the ordered goals (builds.go planSkill): the first
    goal that still needs points and that the server lets it raise. The points are one pool (the job levels of every
    stage add to it); a 2nd-job skill is refused until the spent points reach skill_lock() (player_skillup_limit), so
    the 2nd-job goals wait while the Novice and 1st-job points are spent. Returns (levels, unspent points).
    log, if a list, gets (skill name, stage, spent before the point) for every point taken."""
    job2 = SECOND_JOB[plan["cls"]]
    st = tree.stage_tree(job2)
    cap2 = tree.maxjob.get(job2, 50)
    nov, first, second = job_points(level, cap2)
    avail = nov + first + second
    lock = skill_lock()
    lv = {}
    spent = [0]
    changed2 = level > CHANGE2

    def can(name, want):
        info = st.get(name)
        if info is None:
            return False
        stage, mx, reqs = info
        if lv.get(name, 0) >= min(mx, want):
            return False
        if stage == 2 and (not changed2 or spent[0] < lock):
            return False
        for rn, rl in reqs:
            if lv.get(rn, 0) < rl:
                return False
        return spent[0] < avail

    goals = [tuple(g) for g in noviceSkills] + [tuple(g) for g in plan["skills"]]
    progressed = True
    while progressed:
        progressed = False
        for sid, want in goals:
            name = tree.id2name.get(sid)
            if name and can(name, want):
                if log is not None:
                    log.append((name, st[name][0], spent[0]))
                lv[name] = lv.get(name, 0) + 1
                spent[0] += 1
                progressed = True
                break  # the engine always restarts from the first unmet goal
    return lv, avail - spent[0]
