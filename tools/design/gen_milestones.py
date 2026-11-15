#!/usr/bin/env python3
"""Regenerate the stat / skill checkpoints of legion/data/buildmilestones.json from the plans in legion/builds.go.

The playstyle text in the JSON is hand-written and kept as is (pass --playstyle-src to load it from a Python file
defining P = {build_id: {solo, mvp, pvp}} on the first run).

The checkpoints are not invented: they replay the engine's own rules at each band end (builds.go planStat / planSkill),
so the milestone is exactly what a bot following its plan has at that base level:
  * stats: points per base level from db/re/statpoint.yml, goals in order, one point at a time (cost (v-1)/10+2);
  * skills: skill points from the job levels (Novice job 10 = 9 points, first job changes at job 40 = 39 points, second
    job 50 = 49 points; this server's second jobs all cap at job level 50), the first goal that still needs points and
    that the server's skill_tree.yml lets it raise (stage, prerequisites, max level).

Server data is read from the running container, read-only (G23: never the live DB):
  wsl docker exec ro-rathena cat /rathena/db/re/{skill_tree,skill_db,job_exp}.yml
Pass the directory holding copies with --db-dir (default: fetch via wsl into a temp dir).

Usage: python tools/design/gen_milestones.py [--db-dir DIR] [--playstyle-src FILE]
"""
import argparse, json, os, pathlib, subprocess, sys, tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import planlib as pl

OUT = pl.ROOT / "legion" / "data" / "buildmilestones.json"
DATE = "2026-11-15"
STATUS = "draft for owner review"


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


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db-dir")
    ap.add_argument("--playstyle-src")
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
            nov, first, second = pl.job_points(hi, tree.maxjob.get(job2, 50))
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
        # whole-plan check at base 99
        stats99, spare99 = pl.sim_stats(plan["stats"], 99)
        goals_max = [1] * 6
        for s, v in plan["stats"]:
            goals_max[s] = max(goals_max[s], min(v, 99))
        unreached_stats = [f"{pl.STATS[s]} {v}" for s, v in plan["stats"] if stats99[s] < min(v, 99)]
        lv99, pools99 = pl.sim_skills(plan, tree, 99)
        unmet = []
        listed = 0
        cur = {}
        for sid, want in plan["skills"]:
            n = tree.id2name[sid]
            mx = st[n][1]
            w = min(want, mx)
            listed += max(0, w - cur.get(n, 0))
            cur[n] = max(cur.get(n, 0), w)
        for n, w in cur.items():
            if lv99.get(n, 0) < w:
                unmet.append({"name": desc[n], "want": w, "reached": lv99.get(n, 0)})
        builds[bid] = {
            "name": plan["name"], "base": plan["base"], "class": job2, "first_job": pl.FIRST_JOB[plan["first"]],
            "status": STATUS, "drafted": DATE,
            "ranges": ranges,
            "plan_check": {
                "stat_points_goals_cost": pl.spent(goals_max), "stat_points_at_99": pl.stat_points(99),
                "stats_at_99": dict(zip(pl.STATS, stats99)), "stat_points_spare_at_99": spare99,
                "stat_goals_unreached": unreached_stats,
                "skill_points_listed": listed, "skill_points_reachable": 39 + tree.maxjob.get(job2, 50) - 1,
                "skills_unreached_at_99": unmet,
            },
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
    OUT.write_text(json.dumps(doc, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    print("wrote", OUT, len(builds), "builds")


if __name__ == "__main__":
    main()
