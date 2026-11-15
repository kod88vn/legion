#!/usr/bin/env python3
"""Regenerate the stat / skill checkpoints of legion/data/buildmilestones.json from the plans in legion/builds.go, and the
"Build milestones" prose of docs/classes/*.md and docs/classes/all.md from the same data.

The playstyle text in the JSON is hand-written and kept as is (pass --playstyle-src to load it from a Python file
defining P = {build_id: {solo, mvp, pvp}} on the first run).

The checkpoints are not invented: they replay the engine's own rules at each band end (builds.go planStat / planSkill),
so the milestone is exactly what a bot following its plan has at that base level:
  * stats: points per base level from db/re/statpoint.yml, goals in order, one point at a time (cost (v-1)/10+2);
  * skills: skill points from the job levels (Novice job 10 = 9 points, first job changes at job 40 = 39 points, second
    job 50 = 49 points; this server's second jobs all cap at job level 50), the first goal that still needs points and
    that the server's skill_tree.yml lets it raise (stage, prerequisites, max level).

The prose compares each plan with the one before the 2026-11-15 rebalance (tests/tools/fixtures/builds_before_rebalance.go),
replayed the same way, so the "before" numbers are computed, not typed.

Server data is read from the running container, read-only (G23: never the live DB):
  wsl docker exec ro-rathena cat /rathena/db/re/{skill_tree,skill_db,job_exp}.yml
Pass the directory holding copies with --db-dir (default: fetch via wsl into a temp dir). All files are written as LF.

Usage: python tools/design/gen_milestones.py [--db-dir DIR] [--playstyle-src FILE]
"""
import argparse, json, os, pathlib, re, subprocess, sys, tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import planlib as pl

OUT = pl.ROOT / "legion" / "data" / "buildmilestones.json"
BEFORE = pl.ROOT / "tests" / "tools" / "fixtures" / "builds_before_rebalance.go"
DOCS = pl.ROOT / "docs" / "classes"
DATE = "2026-11-15"
STATUS = "draft for owner review"
HEAD_PROSE = "### Build milestones and playstyle"
HEAD_ALL = "### Build milestones"
FIRST_FILE = {"Swordman": "swordman", "Mage": "mage", "Archer": "archer", "Acolyte": "acolyte", "Merchant": "merchant",
              "Thief": "thief"}


def write_lf(path, text):
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(text)


def fetch_db(dst):
    for f in ("skill_tree", "skill_db", "job_exp"):
        env = dict(os.environ, MSYS_NO_PATHCONV="1")
        out = subprocess.run(["wsl.exe", "docker", "exec", "ro-rathena", "cat", f"/rathena/db/re/{f}.yml"],
                             capture_output=True, check=True, env=env).stdout
        (pathlib.Path(dst) / f"{f}.yml").write_bytes(out)


def stage_label(plan, base_level):
    first = pl.FIRST_JOB[plan["first"]]
    second = pl.SECOND_JOB[plan["cls"]]
    if base_level < 10:
        return "Novice"
    if base_level <= 40:
        return first
    return second


def plan_check(plan, tree, desc):
    """The whole-plan check at base 99: budgets, reached and unreached goals (the Go budget test checks the same)."""
    job2 = pl.SECOND_JOB[plan["cls"]]
    st = tree.stage_tree(job2)
    stats99, spare99 = pl.sim_stats(plan["stats"], 99)
    goals_max = [1] * 6
    for s, v in plan["stats"]:
        goals_max[s] = max(goals_max[s], min(v, 99))
    unreached_stats = [f"{pl.STATS[s]} {v}" for s, v in plan["stats"] if stats99[s] < min(v, 99)]
    lv99, _ = pl.sim_skills(plan, tree, 99)
    listed = 0
    cur = {}
    for sid, want in plan["skills"]:
        n = tree.id2name[sid]
        mx = st[n][1]
        w = min(want, mx)
        listed += max(0, w - cur.get(n, 0))
        cur[n] = max(cur.get(n, 0), w)
    unmet = [{"name": desc[n], "want": w, "reached": lv99.get(n, 0)} for n, w in cur.items() if lv99.get(n, 0) < w]
    return {
        "stat_points_goals_cost": pl.spent(goals_max), "stat_points_at_99": pl.stat_points(99),
        "stats_at_99": dict(zip(pl.STATS, stats99)), "stat_points_spare_at_99": spare99,
        "stat_goals_unreached": unreached_stats,
        "skill_points_listed": listed, "skill_points_reachable": 39 + tree.maxjob.get(job2, 50) - 1,
        "skills_unreached_at_99": unmet,
    }


def ends_text(stats):
    return ", ".join(f"{k} {v}" for k, v in stats.items() if v > 1) or "no stat above 1"


def unreached_text(pc):
    parts = list(pc["stat_goals_unreached"]) + [f"{u['name']} {u['reached']}/{u['want']}" for u in pc["skills_unreached_at_99"]]
    return "; ".join(parts) if parts else "none"


def build_line(bid, plan, old, new):
    """One prose bullet: the plan before the rebalance and now, against the budgets."""
    before = "" if old is None else (
        f"stat goals {old['stat_points_goals_cost']:,} before (budget 1,273), skill points {old['skill_points_listed']} before "
        f"(budget 88), not reached before: {unreached_text(old)}; ")
    return (f"  - `{bid}` ({plan['name']}): {before}now stat goals {new['stat_points_goals_cost']:,} "
            f"(spare {new['stat_points_spare_at_99']}), skill points {new['skill_points_listed']}, ends {ends_text(new['stats_at_99'])}, "
            f"not reached now: {unreached_text(new)}.")


def class_body(group, old_checks, new_checks, plans):
    lines = [
        "- Per-range stats and skills (replayed from the plans at each gear-band end) and the solo / MVP / PvP playstyle of every "
        f"build of this line are in `legion/data/buildmilestones.json` ({', '.join(f'`{b}`' for b in group)}). "
        "Gear is by band in `buildgear.json`; the party role stays in code.",
        "- Plan budget at level 99 (1,273 stat points, 88 skill points), before and after the 2026-11-15 rebalance:",
    ]
    lines += [build_line(b, plans[b], old_checks.get(b), new_checks[b]) for b in group]
    lines.append("- Existing characters: a changed plan keeps the earlier goals in `Legacy` / `LegacyStats` (builds.go), so they "
                 "are not wrong builds; `tests/tools/test_wrongbuilds_replay.py` replays the pre-rebalance plans through the "
                 "wrongbuilds.py rules.")
    return "\n".join(lines) + "\n"


def all_body(old_checks, new_checks):
    over_stat = [b for b, c in old_checks.items() if c["stat_points_goals_cost"] > 1273]
    over_skill = [b for b, c in old_checks.items() if c["skill_points_listed"] > 88]
    unreached_before = [b for b, c in old_checks.items() if c["skills_unreached_at_99"] or c["stat_goals_unreached"]]
    unreached_now = [b for b, c in new_checks.items() if c["skills_unreached_at_99"] or c["stat_goals_unreached"]]
    max_stat = max(c["stat_points_goals_cost"] for c in new_checks.values())
    max_skill = max(c["skill_points_listed"] for c in new_checks.values())
    return "\n".join([
        f"- `legion/data/buildmilestones.json` holds, for every build in `builds.go`, the stats and skills reached by the end of each "
        "gear band (replay of the ordered plans, `tools/design/gen_milestones.py`) and the solo / MVP / PvP playstyle. Tests: "
        "`tests/tools/test_buildmilestones.py` (budgets, reach) and `tests/tools/test_wrongbuilds_replay.py` (old plans flag nothing). "
        "Gear stays in `buildgear.json`; the party role stays in code.",
        "- Limits that shape every plan: 1,273 stat points at level 99; skill points 9 (Novice, Basic Skill, outside the plan) + "
        "39 (first job, changes at job 40) + 49 (every second job caps at job level 50 here) = 88 for the plan.",
        f"- Before the 2026-11-15 rebalance {len(over_stat)} plans listed stat goals above 1,273 points, {len(over_skill)} listed more "
        f"than 88 skill points, and {len(unreached_before)} left at least one goal unreached. Now {len(new_checks) - len(unreached_now)} of "
        f"{len(new_checks)} plans reach every goal; the largest stat total is {max_stat:,} and the largest skill total {max_skill}. "
        "Per line: the class file's \"Build milestones\" section.",
    ]) + "\n"


def replace_section(path, heading_prefix, new_heading, body):
    text = open(path, encoding="utf-8").read()
    i = text.index(heading_prefix)
    line_end = text.index("\n", i)
    m = re.compile(r"^#{2,3} ", re.M).search(text, line_end + 1)
    end = m.start() if m else len(text)
    tail = text[end:]
    write_lf(path, text[:i] + new_heading + "\n" + body + tail)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db-dir")
    ap.add_argument("--playstyle-src")
    ap.add_argument("--no-prose", action="store_true", help="only the JSON (docs untouched)")
    a = ap.parse_args()
    db = a.db_dir
    if not db:
        db = tempfile.mkdtemp(prefix="skilltree_")
        fetch_db(db)
    tree = pl.Tree(f"{db}/skill_tree.yml", f"{db}/skill_db.yml", f"{db}/job_exp.yml")
    import yaml
    desc = {s["Name"]: s.get("Description", s["Name"]) for s in
            yaml.load(open(f"{db}/skill_db.yml", encoding="utf-8"), Loader=getattr(yaml, "CSafeLoader", yaml.SafeLoader))["Body"]}
    plans = pl.parse_builds()
    old_plans = pl.parse_builds(str(BEFORE))
    gear = json.load(open(pl.ROOT / "legion" / "data" / "buildgear.json", encoding="utf-8"))["builds"]
    old = json.load(open(OUT, encoding="utf-8")) if OUT.exists() else {"builds": {}}
    ps_src = {}
    if a.playstyle_src:
        ns = {}
        exec(open(a.playstyle_src, encoding="utf-8").read(), ns)
        ps_src = ns["P"]

    builds = {}
    for bid, plan in plans.items():
        g = gear.get(bid) or gear[plan["base"]]
        job2 = pl.SECOND_JOB[plan["cls"]]
        st = tree.stage_tree(job2)
        ranges = []
        for band in g["bands"]:
            lo, hi = band["min"], band["max"]
            stats, spare = pl.sim_stats(plan["stats"], hi)
            lv, pools = pl.sim_skills(plan, tree, hi)
            skills = []
            for sid, _ in plan["skills"]:
                n = tree.id2name[sid]
                if n in lv and lv[n] > 0 and n not in [tree.id2name[s["id"]] for s in skills]:
                    skills.append({"id": sid, "name": desc[n], "lv": lv[n]})
            ranges.append({
                "range": f"{lo}-{hi}", "min": lo, "max": hi,
                "class_at_end": stage_label(plan, hi),
                "stats": dict(zip(pl.STATS, stats)),
                "stat_points_available": pl.stat_points(hi),
                "stat_points_used": pl.spent(stats),
                "stat_points_spare": spare,
                "skills": skills,
                "skill_points_unspent": sum(pools.values()),
            })
        builds[bid] = {
            "name": plan["name"], "base": plan["base"], "class": job2, "first_job": pl.FIRST_JOB[plan["first"]],
            "status": STATUS, "drafted": DATE,
            "ranges": ranges,
            "plan_check": plan_check(plan, tree, desc),
            "playstyle": ps_src.get(bid) or old["builds"].get(bid, {}).get("playstyle"),
        }
    doc = {
        "status": STATUS, "drafted": DATE,
        "about": "Per-build level checkpoints (stats, skills) and solo / MVP / PvP playstyle. Keyed by the build IDs in "
                 "legion/builds.go. Stats and skills replay the engine's ordered plans at each gear-band end; gear is "
                 "referenced by band only (legion/data/buildgear.json). The party role comes from code and is not repeated.",
        "assumptions": {
            "stat_points": "db/re/statpoint.yml (48 at level 1, 1273 at 99); every stat starts at 1; raising a stat from v costs (v-1)/10+2; "
                           "job-level stat bonuses are not counted (the engine's plan reads base stats only)",
            "skill_points": "Novice 9 (job 10), first job 39 (changes at job 40), second job 49 (job cap 50 for every second job on "
                            "this server, db/re/job_exp.yml); a goal is raised only when the server's skill_tree.yml allows it",
            "pace": "assumed: Novice job 10 by base 10, first job 40 at base 40, second job level = base level - 39 after the "
                    "change; the engine changes jobs at job level 10 and 40 whatever the base level is, so the skill rows are "
                    "approximate by up to a few levels either side",
            "vendors": "vendor characters learn vendorFirst (Enlarge Weight Limit 5, Pushcart 3, Vending 3) before the plan's skills (builds.go planSkill). Merchant-line plans already start with these goals, so for them the replay is the same; a vendor of another line is not modelled (vendors are Merchant-line)",
            "ranges": "gear bands from legion/data/buildgear.json (variants use their main plan's bands); a bands overlap, so "
                      "'by the end of the range' is the max level of the band",
            "stats_keys": pl.STATS,
        },
        "builds": builds,
    }
    write_lf(OUT, json.dumps(doc, indent=1, ensure_ascii=False) + "\n")
    print("wrote", OUT, len(builds), "builds")

    if a.no_prose:
        return
    old_checks = {bid: plan_check(p, tree, desc) for bid, p in old_plans.items()}
    new_checks = {bid: builds[bid]["plan_check"] for bid in builds}
    by_file = {}
    for bid, p in plans.items():
        by_file.setdefault(FIRST_FILE[pl.FIRST_JOB[p["first"]]], []).append(bid)
    for fname, group in sorted(by_file.items()):
        path = DOCS / f"{fname}.md"
        replace_section(path, HEAD_PROSE, HEAD_PROSE + " (rebalanced 2026-11-15)",
                        class_body(sorted(group, key=lambda b: list(plans).index(b)), old_checks, new_checks, plans))
        print("prose", path)
    replace_section(DOCS / "all.md", HEAD_ALL, HEAD_ALL + " (rebalanced 2026-11-15; EC39)", all_body(old_checks, new_checks))
    print("prose", DOCS / "all.md")


if __name__ == "__main__":
    main()
