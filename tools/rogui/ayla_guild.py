"""ayla_guild.py - formats Legion's guild API for ayla.sh (kept out of ayla.sh: no shell quoting inside Python).

  python3 ayla_guild.py guild    < /api/human-guild JSON      Starveil at a glance + ACTIONS
  python3 ayla_guild.py recruit  < /api/guild-recruits JSON   header line, then one "ginvite <aid> <name>\t# why" per recruit
  python3 ayla_guild.py recruits < /api/guild-recruits JSON   the same, read-only (no ginvite lines)
"""
import json
import sys


def load():
    raw = sys.stdin.read()
    try:
        return json.loads(raw)
    except ValueError:
        print("Legion API answered: " + (raw.strip()[:300] or "(nothing)"))
        sys.exit(0)


def gaps_text(d):
    g = d.get("role_gaps")
    if isinstance(g, dict) and g:
        return ", ".join("%d %s" % (v, k) for k, v in g.items())
    m = d.get("missing_jobs")  # older Legion build
    if m:
        return "jobs " + ", ".join(m)
    return "none"


def guild(d):
    print("guild: %s (%s)  size %s/%s  roles %s  gaps: %s  pool ~%sz" % (
        d.get("guild") or "-", "founded" if d.get("founded") else "NOT founded", d.get("size"), d.get("max"),
        d.get("roles"), gaps_text(d), d.get("pool")))
    for m in d.get("members") or []:
        n = m.get("gear_next") or {}
        print("  %-16s %-10s %-6s lv%-3s %-12s %-3s %-6s z%-8s next: %s %s" % (
            m.get("name"), m.get("class"), m.get("role"), m.get("level"), m.get("map") or "offline",
            "pt" if m.get("party") else "", ("guild" if m.get("in_guild") else "-") + ("/V" if m.get("vendor") else ""), m.get("zeny"),
            n.get("item", "-"), n.get("cost", "")))
    print("alchemist: %s" % d.get("alchemist"))
    w = d.get("week") or {}
    print("week: %s (done %s/%s, +%s levels, ends in %sh)" % (w.get("goal"), w.get("members_done"), w.get("members"),
                                                          w.get("levels_gained"), w.get("ends_in_h")))
    print("storage: %s" % d.get("storage"))
    print("ACTIONS:")
    for a in d.get("actions") or []:
        print("  [%s] %s" % (a.get("kind"), a.get("what")))
        if a.get("cmd"):
            print("      -> %s" % a["cmd"])


def recruits(d, invite):
    if not d.get("founded"):
        print("no guild yet: ayla.sh guild-create Starveil first")
        return
    print("guild %s: %s/%s members; role gaps: %s" % (d.get("guild"), d.get("members"), d.get("max"), gaps_text(d)))
    rs = d.get("recruits") or []
    if not rs:
        print("no recruit to suggest right now")
    for r in rs:
        why = "%s lv%s %s%s - %s" % (r.get("class"), r.get("level"), r.get("map"), " [vendor]" if r.get("vendor") else "", r.get("why"))
        if invite:
            print("ginvite %d %s\t# %s" % (r["account_id"], r["name"], why))
        else:
            print("  %s (aid %s): %s" % (r.get("name"), r.get("account_id"), why))


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else "guild"
    d = load()
    if mode == "guild":
        guild(d)
    else:
        recruits(d, mode == "recruit")
