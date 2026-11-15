"""Briefings of the two scheduled system reviewers of the dispatcher (PR17, PR36, PR38, PR39):

  qa     class QA (haiku): after each deploy and every 3 hours, samples bots of every class and files what looks wrong
  build  build/class master (haiku, once a day): reviews each class line's builds and skill use against the class
         files (docs/classes/*.md) and the meter, files code fixes

Both only observe and file into the bug queue (`legionctl bug --from qa|build`); they never play bots (PR38).
The minds cannot read files (Bash is limited to legionctl), so the class files are digested into the briefing here.
Standard library only.
"""
import glob
import os
import random
import re

import briefing as B

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
CLASS_DIR = os.path.join(REPO, "docs", "classes")
MAX_QA_CHARS = 12000
MAX_BUILD_CHARS = 16000
SKIP_SECTIONS = re.compile(r"Rules|Engine notes|Open items|Run \d|Build variety|Teleport card", re.I)


def class_digest(fields=("Stats", "Skills"), class_dir=None, only=None):
    """{line title: [lines]} from docs/classes/*.md (all.md skipped): each build heading ("### Knight: ...") with the
    bullet fields named in `fields` (continuation lines joined). `only`: lower-case class names; a section is kept if
    its heading mentions one of them."""
    out = {}
    pat = re.compile(r"- \*\*(%s)[^*]*\*\*" % "|".join(re.escape(f) for f in fields))
    for p in sorted(glob.glob(os.path.join(class_dir or CLASS_DIR, "*.md"))):
        if os.path.basename(p) == "all.md":
            continue
        title, rows, keep, cur = os.path.basename(p)[:-3], [], False, False
        try:
            with open(p, encoding="utf-8") as f:
                lines = f.readlines()
        except OSError:
            continue
        for ln in lines:
            if ln.startswith("# "):
                title = ln[2:].strip()
                continue
            if ln.startswith("### "):
                head = ln[4:].strip()
                # a first job ("Swordman") matches its line's title, a 2nd/trans job its build heading
                keep = not SKIP_SECTIONS.search(head) and (
                    only is None or any(c in head.lower() or c in title.lower().split(":")[0] for c in only))
                if keep:
                    rows.append(head)
                cur = False
                continue
            if ln.startswith("#"):
                keep = cur = False
                continue
            if not keep:
                continue
            if ln.startswith("- **"):
                cur = bool(pat.match(ln))
                if cur:
                    rows.append("  " + ln.strip()[2:])
            elif ln.startswith("  ") and cur:
                rows[-1] += " " + ln.strip()
            else:
                cur = False
        if rows:
            out[title] = rows
    return out


def digest_text(d, per_row=None):
    parts = []
    for title, rows in d.items():
        parts.append(title + ":")
        parts.extend((r[:per_row] if per_row else r) for r in rows)
    return "\n".join(parts)


def sample_by_class(bots, per_class=1, rng=None):
    """{class: [bot summaries]}: online, non-vendor bots, `per_class` picked at random from each class"""
    rng = rng or random
    groups = {}
    for b in B.rows_of(bots):
        if not isinstance(b, dict) or not b.get("online") or b.get("role") == "vendor":
            continue
        groups.setdefault(str(b.get("class") or "?"), []).append(b)
    return {c: rng.sample(bs, min(per_class, len(bs))) for c, bs in sorted(groups.items())}, {
        c: len(bs) for c, bs in groups.items()}


def bot_line(b):
    return (f"{b.get('name')} lv{b.get('level')} on {b.get('map')} ({b.get('x')},{b.get('y')}): {b.get('state')!s:.70} "
            f"| kills {b.get('kills')} deaths {b.get('deaths')} hp {b.get('hp')} weight {b.get('weight') or '?'}")


def suspicious(b):
    """cheap pre-check the reviewer looks at first: states that are rarely right for long"""
    s = str(b.get("state") or "").lower()
    return any(w in s for w in ("stuck", "idle", "no route", "afk", "lost", "waiting", "error")) or bool(b.get("error"))


def _finish(parts, api, cap):
    text = "\n".join(p for p in parts if p)
    gaps = sorted(set(api.missing))
    if gaps:
        text += "\n(not available right now: " + ", ".join(gaps) + ")\n"
    if len(text) > cap:
        text = text[:cap] + "\n...[briefing cut]\n"
    return text


def qa_briefing(api, notes_dir, prev=None, prev_ts=None, trigger="", rng=None):
    snap = {"metrics": {}}
    parts = [f"Role: class QA. Wake reason: {trigger or 'schedule'} (after a deploy: check what the deploy changed first)."]
    bots = api.get("/api/bots")
    picks, counts = sample_by_class(bots or [], per_class=2, rng=rng)
    if picks:
        lines = []
        for c, bs in picks.items():
            lines.append(f"{c} ({counts[c]} online):")
            lines.extend(("  ! " if suspicious(b) else "  ") + bot_line(b) for b in bs)
        parts.append(B.section("Sample: two online bots of every class (! = suspicious state)", "\n".join(lines)))
        snap["metrics"]["classes"] = len(picks)
        snap["metrics"]["suspicious"] = sum(1 for bs in picks.values() for b in bs if suspicious(b))
    else:
        parts.append("(no online bots in /api/bots: if the server should be up, file that as a bug)")
    meter = api.get("/api/meter")
    if meter is not None:
        parts.append(B.section("Cohorts furthest below par", "\n".join(B.cohort_line(c) for c in B.cohort_summary(meter, 6))))
        snap["metrics"].update(B.server_metrics(meter))
    parts.append(B.section("Feedback", B.feedback(prev, snap, prev_ts)))
    parts.append(B.section("Your notes from earlier calls", B.read_notes(notes_dir, "qa") or "(none yet)"))
    only = {c.lower() for c in picks} if picks else None
    d = class_digest(fields=("Party role", "Use"), only=only)
    if d:
        parts.append(B.section("How each class should play (docs/classes, digest)", digest_text(d, 260)))
    return _finish(parts, api, MAX_QA_CHARS), snap


def build_briefing(api, notes_dir, prev=None, prev_ts=None, trigger="", rng=None):
    snap = {"metrics": {}}
    parts = [f"Role: build and class master, daily review. Wake reason: {trigger or 'schedule'}."]
    meter = api.get("/api/meter")
    if meter is not None:
        parts.append(B.section("Cohorts furthest below par (class line|level band|solo/party/vendor)",
                               "\n".join(B.cohort_line(c) for c in B.cohort_summary(meter, 8))))
        snap["metrics"].update(B.server_metrics(meter))
    else:
        parts.append("(meter endpoint not available on this engine: judge from the server state only)")
    picks, counts = sample_by_class(api.get("/api/bots") or [], per_class=1, rng=rng)
    if picks:
        parts.append(B.section("One online bot per class to inspect (`bot <name>`: stats, skills, gear)",
                               "\n".join(f"{c} ({counts[c]}): {bot_line(bs[0])}" for c, bs in picks.items())))
    parts.append(B.section("Your last decisions", B.bus_lines(api, "build")))
    parts.append(B.section("Feedback", B.feedback(prev, snap, prev_ts)))
    parts.append(B.section("Your notes from earlier calls", B.read_notes(notes_dir, "build") or "(none yet)"))
    d = class_digest(fields=("Stats", "Skills"))   # last: the longest part; a cut only shortens the digest
    if d:
        parts.append(B.section("Planned builds (docs/classes: stats and skill order per plan)", digest_text(d)))
    return _finish(parts, api, MAX_BUILD_CHARS), snap
