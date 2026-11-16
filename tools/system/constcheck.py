#!/usr/bin/env python3
"""constcheck.py - every AI prompt carries the whole constitution, first, exactly once, loaded at run time (PR42).

  python tools/system/constcheck.py            list every prompt source and how it gets the constitution
  python tools/system/constcheck.py --strict   also fail on the sources that are known to be uncovered
Exit code 0 = ok, 1 = a source lacks it, carries it twice, carries a copy, or a new sender is not registered.
Called by tools/system/gatecheck.py. Reads only; starts nothing; spends nothing.

The routes (one per kind of agent, never two):
  A  CLAUDE.md import      G:\\RO\\CLAUDE.md starts with `@legion/CONSTITUTION.md`. The main session and every subagent
                           that loads CLAUDE.md get it there (Claude Code docs: memory "Import additional files";
                           sub-agents "What loads at startup").
  B  SubagentStart hook    tools/system/constitution.py subagent-start, for the agent types that start without
                           CLAUDE.md (Explore, Plan, custom agents with omitClaudeMd). Silent for all others.
  C  the dispatcher        tools/puppet/dispatch.py reads the file for every call and refuses to send without it;
                           --safe-mode keeps routes A and B out of those calls.
  D  the mastermind        mastermind/mastermind.py (fallback container) reads the mounted file for every call.
  E  GitHub Copilot        .github/ instruction files (PR48) have no run-time import: each POINTS to CONSTITUTION.md by
                           name (check_pointers); the copy check still runs over them.
A copy of the text anywhere else is an error: one file, loaded at run time.
"""
import glob
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, HERE)
import constitution  # noqa: E402

MAX_HOPS = 4            # Claude Code: imports nest "with a maximum depth of four hops"
WIN_CMDLINE = 32767     # CreateProcess limit; the dispatcher passes the system prompt as one argument
COPY_MIN = 60           # a constitution line this long found verbatim in another file is a copy

# code that sends a prompt to a model. A file that matches SENDER_RE and is not listed here fails the check, so a new
# sender cannot be added without wiring it to constitution.py.
KNOWN_SENDERS = {
    "tools/puppet/dispatch.py": "dispatcher",
    "mastermind/mastermind.py": "mastermind",
}
SENDER_RE = re.compile(r"messages\.(create|parse|stream)\(|anthropic\.Anthropic\(|--append-system-prompt|"
                       r"--system-prompt|[\"']claude[\"'],\s*[\"']-p[\"']|\bclaude(\.exe)? (-p|--print)\b")
SENDER_SKIP_DIRS = {".git", "rathena", "openkore", "openkore-ayla", "openkore-qa", "client-tools", "backups",
                    "__pycache__", "node_modules", "logs", "docs", "tests", "data", "state", "notes"}
SENDER_SKIP_FILES = {"tools/system/constcheck.py", "tools/system/constitution.py"}
SENDER_EXT = (".py", ".sh", ".go", ".pl", ".ps1", ".cmd", ".bat", ".js", ".ts")

# prompt sources that cannot load a file at run time. They are reported, never given a copy (owner, PR42).
# Empty now: the GitHub Copilot files (PR48) are POINTER sources, checked by check_pointers below.
UNCOVERED = []

# GitHub Copilot instruction files (PR48). Copilot has no run-time import, so each file must POINT to the constitution
# (naming CONSTITUTION.md) and must carry no copy of it (check_copies, PR42). A file that is superseded fails.
COPILOT_SOURCES = [".github/copilot-instructions.md", ".github/agents/*.agent.md", ".github/prompts/*.prompt.md"]
CONST_NAME_RE = re.compile(r"(?<![\w/.-])CONSTITUTION\.md(?![\w.-])")


class Report:
    def __init__(self):
        self.rows, self.bad, self.warn = [], [], []

    def ok(self, source, how):
        self.rows.append(("OK  ", source, how))

    def fail(self, source, why):
        self.rows.append(("FAIL", source, why))
        self.bad.append("%s: %s" % (source, why))

    def warning(self, source, why):
        self.rows.append(("WARN", source, why))
        self.warn.append("%s: %s" % (source, why))


def read(p):
    with open(p, encoding="utf-8", errors="replace") as f:
        return f.read()


def native_path(p):
    """a hook command's path as this interpreter sees it. settings.json names hooks by Windows path (G:/RO/legion/...)
    or Git Bash path (/g/RO/legion/...); under WSL the same file is /mnt/g/RO/legion/... Returns p unchanged when it is
    already a file, or when it is not a drive path."""
    if os.path.isfile(p):
        return p
    m = re.match(r"^([A-Za-z]):[\\/](.*)$", p) or re.match(r"^/([A-Za-z])/(.*)$", p)
    if not m:
        return p
    drive, rest = m.group(1), m.group(2).replace("\\", "/")
    return "%s:/%s" % (drive.upper(), rest) if os.name == "nt" else "/mnt/%s/%s" % (drive.lower(), rest)


def main_checkout(repo):
    """the main checkout of the repo (G:/RO/legion). A git worktree (G:/RO/legion-wt/x) names it in its .git file."""
    try:
        text = read(os.path.join(repo, ".git")).strip()
    except OSError:                       # a main checkout has a .git folder
        return repo
    m = re.match(r"gitdir:\s*(.+)$", text)
    mm = re.match(r"^(.*)/\.git/worktrees/", m.group(1).strip().replace("\\", "/")) if m else None
    return native_path(mm.group(1)) if mm else repo


def session_root(repo):
    """the folder the owner's session starts in (G:/RO: CLAUDE.md, .claude/settings.json): parent of the main checkout"""
    return os.path.dirname(main_checkout(repo))


def rel(p):
    try:
        r = os.path.relpath(p, REPO)
    except ValueError:
        return p.replace("\\", "/")
    return (p if r.startswith("..") else r).replace("\\", "/")


# ----------------------------------------------------------------- route A: CLAUDE.md imports
def strip_unimported(text):
    """what the import parser looks at: no fenced blocks, no code spans, no block-level HTML comments"""
    text = re.sub(r"<!--.*?-->", "", text, flags=re.S)
    text = re.sub(r"^(```|~~~).*?^\1[^\n]*$", "", text, flags=re.S | re.M)
    return re.sub(r"`[^`\n]*`", "", text)


def imports_of(path):
    """files an instruction file imports with @path, in order (only paths that exist as files)"""
    out = []
    try:
        text = strip_unimported(read(path))
    except OSError:
        return out
    for m in re.finditer(r"(?:^|(?<=\s))@((?:\\ |[^\s\\])+)", text):
        p = m.group(1).replace("\\ ", " ").rstrip(".,;:)")
        if p.startswith("~/"):
            p = os.path.join(os.path.expanduser("~"), p[2:])
        full = os.path.normpath(p if os.path.isabs(p) else os.path.join(os.path.dirname(path), p))
        if os.path.isfile(full):
            out.append(full)
    return out


def load_order(path, hops=0, seen_chain=()):
    """every file loaded because of one instruction file, in order: the file itself, then its imports (depth first)"""
    out = [os.path.normcase(os.path.normpath(path))]
    if hops >= MAX_HOPS:
        return out
    for imp in imports_of(path):
        key = os.path.normcase(imp)
        if key in seen_chain:
            continue
        out += load_order(imp, hops + 1, seen_chain + (os.path.normcase(os.path.normpath(path)),))
    return out


def first_content_is_import(claude_md, target):
    """True when the first thing in the file (after comments and blank lines) is the import of `target`"""
    try:
        text = re.sub(r"<!--.*?-->", "", read(claude_md), flags=re.S).replace("\ufeff", "").lstrip()
    except OSError:
        return False
    m = re.match(r"@((?:\\ |[^\s\\])+)", text)
    if not m:
        return False
    full = os.path.normpath(os.path.join(os.path.dirname(claude_md), m.group(1).replace("\\ ", " ")))
    return os.path.normcase(full) == os.path.normcase(os.path.normpath(target))


def instruction_files(root_dir, repo):
    """the instruction files a session rooted at root_dir can load: at start (root and above are the owner's), and on
    demand in the folders below that hold agent prompts"""
    home = os.path.expanduser("~")
    files = [os.path.join(home, ".claude", "CLAUDE.md")]
    files += sorted(glob.glob(os.path.join(home, ".claude", "rules", "*.md")))
    for d in (root_dir, repo):
        files += [os.path.join(d, "CLAUDE.md"), os.path.join(d, ".claude", "CLAUDE.md"),
                  os.path.join(d, "CLAUDE.local.md")]
        files += sorted(glob.glob(os.path.join(d, ".claude", "rules", "**", "*.md"), recursive=True))
    seen, out = set(), []
    for f in files:
        k = os.path.normcase(os.path.normpath(f))
        if k not in seen and os.path.isfile(f):
            seen.add(k)
            out.append(f)
    return out


def check_claude_md(rep, root_dir=None, repo=None, const_path=None):
    repo = repo or REPO
    root_dir = root_dir or session_root(repo)
    # the file to match is the one the tools read: LEGION_CONSTITUTION when set (as constitution.path() does), else the
    # main checkout's CONSTITUTION.md, which is what the live session loads through CLAUDE.md
    const_path = const_path or os.environ.get("LEGION_CONSTITUTION") or os.path.join(main_checkout(repo), "CONSTITUTION.md")
    ckey = os.path.normcase(os.path.normpath(const_path))
    root_md = os.path.join(root_dir, "CLAUDE.md")
    src = "main session (and every subagent that loads CLAUDE.md)"
    if not os.path.isfile(root_md):
        rep.fail(src, "%s is missing" % root_md)
        return
    loads = []
    for f in instruction_files(root_dir, repo):
        loads += load_order(f)
    n = loads.count(ckey)
    twice = sorted({rel(p) for p in loads if loads.count(p) > 1 and p != ckey})
    if n == 0:
        rep.fail(src, "no CLAUDE.md imports CONSTITUTION.md")
    elif n > 1:
        rep.fail(src, "CONSTITUTION.md is loaded %d times through CLAUDE.md files and imports" % n)
    elif not first_content_is_import(root_md, const_path):
        rep.fail(src, "%s does not start with the import of CONSTITUTION.md" % root_md)
    else:
        rep.ok(src, "run-time import: first line of %s, loaded once" % root_md.replace("\\", "/"))
    for p in twice:
        rep.fail("CLAUDE.md imports", "%s is loaded more than once (duplication, PR42)" % p)


# ----------------------------------------------------------------- route B: hooks and agent definitions
def settings_files(root_dir, repo):
    out = []
    for d in (root_dir, repo):
        for n in ("settings.json", "settings.local.json"):
            p = os.path.join(d, ".claude", n)
            if os.path.isfile(p):
                out.append(p)
    return out


def hook_commands(settings_path):
    """[(event, command)] of every command hook in one settings file"""
    out = []
    try:
        d = json.loads(read(settings_path))
    except (OSError, ValueError):
        return None
    for event, groups in (d.get("hooks") or {}).items():
        for g in groups if isinstance(groups, list) else []:
            for h in (g.get("hooks") or []) if isinstance(g, dict) else []:
                if isinstance(h, dict) and h.get("type") == "command":
                    out.append((event, str(h.get("command") or "")))
    return out


def check_hooks(rep, root_dir=None, repo=None):
    repo = repo or REPO
    root_dir = root_dir or session_root(repo)
    for name in ("settings.json",):
        for d in (root_dir, repo):
            p = os.path.join(d, ".claude", name)
            src = "hooks in %s" % p.replace("\\", "/")
            cmds = hook_commands(p) if os.path.isfile(p) else None
            if cmds is None:
                rep.fail(src, "settings file missing or not valid JSON")
                continue
            inject = [(e, c) for e, c in cmds if "constitution.py" in c]
            guard = [(e, c) for e, c in cmds if "mainguard.py" in c]
            if [e for e, _ in inject] != ["SubagentStart"] or "subagent-start" not in inject[0][1]:
                rep.fail(src, "expected exactly one constitution.py hook, on SubagentStart (found: %s)"
                         % (", ".join(e for e, _ in inject) or "none"))
            elif [e for e, _ in guard] != ["PreToolUse"]:
                rep.fail(src, "expected exactly one mainguard.py hook, on PreToolUse (Constitution 1.9; found: %s)"
                         % (", ".join(e for e, _ in guard) or "none"))
            else:
                missing = []
                for _e, c in inject + guard:
                    for tok in c.split():
                        if tok.endswith(".py") and not os.path.isfile(native_path(tok)):
                            missing.append(tok)
                if missing:
                    rep.fail(src, "hook script not found: %s" % ", ".join(missing))
                else:
                    rep.ok(src, "SubagentStart -> constitution.py subagent-start; PreToolUse -> mainguard.py")
    for p in settings_files(root_dir, repo):          # a local settings file must not add a second injection
        if p.endswith("settings.local.json"):
            if any("constitution.py" in c for _e, c in (hook_commands(p) or [])):
                rep.fail("hooks in %s" % p.replace("\\", "/"), "a second constitution.py hook (double load)")


def agent_defs(dirs):
    out = []
    for d in dirs:
        for p in sorted(glob.glob(os.path.join(d, "**", "*.md"), recursive=True)):
            try:
                fm = constitution.agent_frontmatter(read(p))
            except OSError:
                continue
            if fm.get("name"):
                out.append((fm["name"], p, fm))
    return out


def check_agents(rep, root_dir=None, repo=None):
    repo = repo or REPO
    root_dir = root_dir or session_root(repo)
    dirs = [os.path.join(repo, ".claude", "agents"), os.path.join(root_dir, ".claude", "agents"),
            os.path.join(os.path.expanduser("~"), ".claude", "agents")]
    try:
        const = constitution.text()
    except constitution.ConstitutionError as e:
        rep.fail("subagents", str(e))
        return
    # built-in types: Explore and Plan start without CLAUDE.md -> hook; the others load CLAUDE.md -> hook silent
    for t in constitution.NO_CLAUDE_MD:
        src = "subagent %s (built-in, starts without CLAUDE.md)" % t
        try:
            out = constitution.subagent_start({"agent_type": t}, dirs)
        except constitution.ConstitutionError as e:
            rep.fail(src, str(e))
            continue
        ctx = ((out or {}).get("hookSpecificOutput") or {}).get("additionalContext")
        if ctx != const:
            rep.fail(src, "the SubagentStart hook does not hand over the full text")
        else:
            rep.ok(src, "SubagentStart hook reads the file at spawn: %d of %d characters allowed"
                   % (len(ctx), constitution.HOOK_CAP))
    if len(const) > constitution.HOOK_CAP:
        rep.fail("SubagentStart hook", "the constitution is %d characters; a hook's additionalContext is cut above %d. "
                 "Explore and Plan would get a shortened text: they need another route" % (len(const), constitution.HOOK_CAP))
    for t in ("general-purpose", "claude", "fork"):
        src = "subagent %s (built-in, loads CLAUDE.md)" % t
        if constitution.subagent_start({"agent_type": t}, dirs) is not None:
            rep.fail(src, "gets the constitution from CLAUDE.md and from the hook (double load)")
        else:
            rep.ok(src, "CLAUDE.md import; the hook stays silent")
    names = {}
    for name, p, fm in agent_defs(dirs):
        src = "agent %s (%s)" % (name, rel(p))
        if name in names:
            rep.warning(src, "same agent name as %s" % rel(names[name]))
        names[name] = p
        omit = fm.get("omitClaudeMd", "").lower() == "true"
        served = constitution.subagent_start({"agent_type": name}, dirs) is not None
        if name in constitution.NO_CLAUDE_MD and not omit:
            rep.fail(src, "overrides a built-in that the hook serves, but loads CLAUDE.md as well (double load)")
        elif omit and not served:
            rep.fail(src, "omitClaudeMd and the hook does not serve it: no constitution")
        elif omit:
            rep.ok(src, "omitClaudeMd: SubagentStart hook")
        elif served:
            rep.fail(src, "CLAUDE.md and the hook both carry it (double load)")
        else:
            rep.ok(src, "CLAUDE.md import; the hook stays silent")


# ----------------------------------------------------------------- copies
def copy_lines(text, const):
    """constitution lines (long ones) found verbatim in another text"""
    t = constitution.normalise(text)
    return [ln for ln in const.split("\n") if len(ln.strip()) >= COPY_MIN and ln.strip() in t]


def prompt_files(repo=None, root_dir=None):
    """every file that is loaded into a prompt or is a prompt"""
    repo = repo or REPO
    root_dir = root_dir or session_root(repo)
    pats = ["CLAUDE.md", "RULES.md", "STATUS.md", "AGENTS.md", "CLASSES.md", "HANDOVER.md", "README.md",
            ".claude/agents/**/*.md", ".claude/skills/**/*.md", ".claude/commands/**/*.md", ".claude/rules/**/*.md",
            "tools/puppet/briefs/*.md", "tools/puppet/workdir/CLAUDE.md", "tools/puppet/README.md",
            "tools/puppet/*.py", "mastermind/*.py", "docs/rules/*.md", "docs/classes/*.md", "docs/handover/*.md",
            "docs/design/*.md", ".github/**/*.md"]
    out = []
    for pat in pats:
        out += glob.glob(os.path.join(repo, pat), recursive=True)
    out += [os.path.join(root_dir, "CLAUDE.md")] + glob.glob(os.path.join(root_dir, ".claude", "**", "*.md"), recursive=True)
    const_key = os.path.normcase(os.path.normpath(constitution.path()))
    return sorted({os.path.normpath(p) for p in out if os.path.isfile(p)
                   and os.path.normcase(os.path.normpath(p)) != const_key})


def check_copies(rep, files=None):
    try:
        const = constitution.text()
    except constitution.ConstitutionError as e:
        rep.fail("copies", str(e))
        return
    files = prompt_files() if files is None else files
    hit = 0
    for p in files:
        try:
            lines = copy_lines(read(p), const)
        except OSError:
            continue
        if lines:
            hit += 1
            rep.fail(rel(p), "holds a copy of %d constitution line(s), e.g. \"%s...\" (one file, no copies)"
                     % (len(lines), lines[0].strip()[:50]))
    if not hit:
        rep.ok("agent definitions, skills, briefs, rule files (%d files)" % len(files),
               "no copy of the constitution's text; they are read after it")


# ----------------------------------------------------------------- routes C and D: code that sends prompts
def check_dispatcher(rep):
    sys.path.insert(0, os.path.join(REPO, "tools", "puppet"))
    try:
        import dispatch
    except Exception as e:
        rep.fail("puppet dispatcher", "tools/puppet/dispatch.py cannot be imported: %r" % (e,))
        return
    for role in dispatch.ROLES:
        src = "puppet role %s (tools/puppet/dispatch.py%s)" % (role, ", Ayla's helper" if role == "ayla" else "")
        try:
            cmd = dispatch.build_command(role, claude="claude")
            sp = cmd[cmd.index("--append-system-prompt") + 1]
            constitution.require(sp, "system prompt")
            if "--safe-mode" not in cmd:
                raise constitution.ConstitutionError("no --safe-mode: CLAUDE.md would load it a second time")
            size = sum(len(c) + 3 for c in cmd)
            if size > WIN_CMDLINE - 2000:
                raise constitution.ConstitutionError("command line is %d characters, too close to the Windows limit "
                                                     "of %d: the prompt would not be delivered" % (size, WIN_CMDLINE))
        except Exception as e:
            rep.fail(src, str(e))
            continue
        rep.ok(src, "read from the file for every call; first, once; refuses to send without it; --safe-mode")
    src_text = read(os.path.join(REPO, "tools", "puppet", "dispatch.py"))
    runs = len(re.findall(r"subprocess\.(run|Popen|call|check_output)\(", src_text))
    if runs != 1 or "check_command(build_command(" not in src_text:
        rep.fail("puppet dispatcher", "expected one subprocess call, fed by check_command(build_command(...)) "
                 "(found %d subprocess calls)" % runs)


def check_mastermind(rep):
    src = "mastermind fallback container (mastermind/mastermind.py)"
    try:
        text = read(os.path.join(REPO, "mastermind", "mastermind.py"))
        compose = read(os.path.join(REPO, "docker-compose.yml"))
    except OSError as e:
        rep.fail(src, str(e))
        return
    m = re.search(r"def ask\(self, system, prompt, schema, max_tokens=4000\):(.*?)\n    def |\Z", text, re.S)
    body = text[text.find("class ClaudeBrain"):text.find("class RuleBrain")]
    calls = len(re.findall(r"messages\.(create|parse|stream)\(", text))
    if calls != 1 or "system = with_constitution(system)" not in body or \
            body.find("system = with_constitution(system)") > body.find("messages.parse("):
        rep.fail(src, "the one API call must be fed by with_constitution(system)")
    elif "./CONSTITUTION.md:/constitution/CONSTITUTION.md:ro" not in compose:
        rep.fail(src, "docker-compose.yml does not mount CONSTITUTION.md into the container")
    else:
        rep.ok(src, "read from the mounted file for every call; first; refuses to call without it")
    return m


def check_senders(rep, repo=None):
    """every file that talks to a model is a registered sender"""
    repo = repo or REPO
    found = []
    for root, dirs, files in os.walk(repo):
        dirs[:] = [d for d in dirs if d not in SENDER_SKIP_DIRS]
        for fn in files:
            if not fn.endswith(SENDER_EXT):
                continue
            p = os.path.join(root, fn)
            r = os.path.relpath(p, repo).replace("\\", "/")
            if r in SENDER_SKIP_FILES:
                continue
            try:
                if os.path.getsize(p) < 2_000_000 and SENDER_RE.search(read(p)):
                    found.append(r)
            except OSError:
                continue
    for r in found:
        if r not in KNOWN_SENDERS:
            rep.fail(r, "sends a prompt to a model but is not registered in constcheck.py (KNOWN_SENDERS) and "
                        "not checked for the constitution")
    for r in KNOWN_SENDERS:
        if r not in found and repo == REPO:
            rep.warning(r, "registered as a sender but no model call found in it any more")
    return found


def check_pointers(rep, repo=None):
    """the GitHub Copilot files (PR48) exist, are current (no SUPERSEDED), and point to CONSTITUTION.md. A pointer
    is a name, not a copy: the copy check (check_copies) runs over the same files."""
    repo = repo or REPO
    files = []
    for pat in COPILOT_SOURCES:
        files += sorted(glob.glob(os.path.join(repo, pat)))
    if not files:
        rep.fail(".github", "no GitHub Copilot instruction files (PR48: Copilot must be able to take over)")
        return
    if not os.path.isfile(os.path.join(repo, ".github", "copilot-instructions.md")):
        rep.fail(".github/copilot-instructions.md", "missing: the Copilot entry point (PR48)")
    for p in files:
        src = rel(p)
        text = read(p)
        if "SUPERSEDED" in text:
            rep.fail(src, "is marked SUPERSEDED: it must be current (PR48)")
        elif not CONST_NAME_RE.search(text):
            rep.fail(src, "does not point to CONSTITUTION.md (PR48: a pointer, not a copy)")
        else:
            rep.ok(src, "current; points to CONSTITUTION.md (pointer, no copy)")


def check_uncovered(rep, strict=False, repo=None):
    repo = repo or REPO
    for pat, why in UNCOVERED:
        hits = glob.glob(os.path.join(repo, pat))
        if hits:
            msg = "%d file(s) NOT covered: %s. No copy is pasted in (PR42); reported to the owner" % (len(hits), why)
            (rep.fail if strict else rep.warning)(pat, msg)


def run(strict=False):
    rep = Report()
    try:
        const = constitution.text()
        head = "constitution: %s (%d characters, %d words, sha256 %s)" % (
            rel(constitution.path()), len(const), len(const.split()), constitution.digest()[:16])
    except constitution.ConstitutionError as e:
        rep.fail("CONSTITUTION.md", str(e))
        return rep, "constitution: NOT READABLE"
    check_claude_md(rep)
    check_hooks(rep)
    check_agents(rep)
    check_dispatcher(rep)
    check_mastermind(rep)
    check_senders(rep)
    check_copies(rep)
    check_pointers(rep)
    check_uncovered(rep, strict)
    return rep, head


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    rep, head = run(strict="--strict" in argv)
    print(head)
    for tag, source, how in rep.rows:
        print("%s %s\n       %s" % (tag, source, how))
    print("constcheck: %d source(s) checked, %d problem(s), %d warning(s)"
          % (len(rep.rows), len(rep.bad), len(rep.warn)))
    print("OK" if not rep.bad else "FAIL")
    return 1 if rep.bad else 0


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    sys.exit(main())
