#!/usr/bin/env python3
"""mainguard.py - Claude Code PreToolUse hook that enforces Constitution 1.3 / 1.9 on the MAIN session only.

"The main session's tools that change the system are blocked." The main session listens, assigns and keeps the rule
records; it does not write code, configuration or data, commit, build, deploy, start, stop or restart anything.

Who is the main session: the hook input of a tool call made inside a subagent (or an in-process teammate) carries
`agent_id`; a call made by the main session does not (Claude Code docs, hooks: "Common input fields" and "Hooks ...
also run inside subagents ... the input carries the agent_id and agent_type"). A call WITH agent_id is never checked.

For the main session everything is denied unless tools/system/mainguard.allow.json allows it:
  - the read, search, agent and ask-the-owner tools,
  - Write/Edit only on the rule records, the status note, the reports, its memory and its scratchpad,
  - shell (Bash, PowerShell) only for read-only inspection and queue upkeep; a chained command passes only when every
    part passes; anything the parser does not fully understand is denied (fail closed).
Every denial is appended to logs/constitution-denied.log (JSON lines) so that breaches can be reported.

Wired in G:\\RO\\.claude\\settings.json and legion\\.claude\\settings.json (same place as tools/mindgate.py).
Switch off (owner only): create the empty file tools/system/mainguard.off, or remove the PreToolUse block from the
settings file. The main session can do neither: both paths are outside its allow-list.

  python tools/system/mainguard.py            hook mode: JSON on stdin, a deny decision on stdout (or silence)
  python tools/system/mainguard.py --explain  same, but prints the verdict as plain text (for tests by hand)
Standard library only.
"""
import json
import os
import posixpath
import re
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))
ALLOW_FILE = os.path.join(HERE, "mainguard.allow.json")
OFF_FILE = os.path.join(HERE, "mainguard.off")
LOG_FILE = os.path.join(REPO, "logs", "constitution-denied.log")
DEFAULT_MESSAGE = "Constitution 1.3: the main session does not do this. Assign an agent."
MAX_DEPTH = 3          # wsl -> bash -c -> ... nesting that is still parsed
SAFE_WORD = re.compile(r"^[\w@%+=:,./-]+$")


class Deny(Exception):
    pass


# ----------------------------------------------------------------- paths
def canon(p, cwd=None):
    """One spelling for a path, or None when it cannot be pinned down: lower case, forward slashes, drive form
    'g:/ro/x'. Understands G:\\RO, G:/RO, /g/RO (Git Bash), /mnt/g/RO (WSL) and '..'. A relative path needs `cwd`
    (already canonical). POSIX paths outside a drive come back as 'posix:/...'."""
    if not isinstance(p, str) or not p or len(p) > 1000:
        return None
    p = p.strip().replace("\\", "/")
    if any(c in p for c in "~$%*?<>|\"'`\n\r\t\0"):
        return None
    if p.startswith("//"):                      # UNC, \\wsl$\..., \\.\ devices, \\?\ long paths
        return None
    m = re.match(r"^/mnt/([A-Za-z])(/.*)?$", p) or re.match(r"^/([A-Za-z])(/.*)?$", p)
    if m:
        p = m.group(1) + ":" + (m.group(2) or "/")
    if re.match(r"^[A-Za-z]:", p):
        if len(p) > 2 and p[2] != "/":          # 'G:file' is relative to the drive's current directory
            return None
        drive, rest = p[0].lower() + ":", p[2:] or "/"
    elif p.startswith("/"):
        drive, rest = "posix:", p
    else:
        if not cwd:
            return None
        drive, _, base = cwd.partition(":")
        drive += ":"
        rest = posixpath.join(base or "/", p)
    rest = posixpath.normpath(rest)
    if rest.startswith("//"):
        rest = rest[1:]
    parts = [x for x in rest.split("/") if x]
    for part in parts:                          # NTFS streams, trailing dots and blanks name the same file
        if ":" in part or part.endswith((".", " ")) or part.startswith(" "):
            return None
        if re.search(r"~\d", part):             # 8.3 short names (RULES~1.MD)
            return None
    return (drive + "/" + "/".join(parts)).lower()


def matches(cp, patterns):
    """cp (canonical) against canonical patterns: exact, or 'dir/**' = strictly under dir"""
    for pat in patterns:
        if pat.endswith("/**"):
            if cp.startswith(pat[:-2]) and len(cp) > len(pat) - 2:
                return True
        elif cp == pat:
            return True
    return False


def real_canon(raw):
    """canonical form of where the path really leads (junctions, symlinks), None when unknown. Windows only."""
    try:
        return canon(os.path.realpath(raw))
    except Exception:
        return None


# ----------------------------------------------------------------- shell parsing
def split_shell(cmd, flavour="bash"):
    """-> [(words, redirect_targets)] for each part of a chained command. Raises Deny for anything that could hide a
    command: substitution, variables, subshells, groups, background jobs, heredocs, input redirection, comments."""
    if not isinstance(cmd, str) or not cmd.strip():
        raise Deny("empty command")
    if len(cmd) > 8000:
        raise Deny("command too long to check")
    bash = flavour == "bash"
    segs, words, redirs = [], [], []
    cur, has = [], False            # the word being built; has = a (possibly empty, quoted) word exists
    want_target = False             # the next word is the target of a > redirection
    i, n = 0, len(cmd)

    def end_word():
        nonlocal cur, has, want_target
        if has:
            w = "".join(cur)
            if want_target:
                redirs.append(w)
                want_target = False
            else:
                words.append(w)
        cur, has = [], False

    def end_seg():
        nonlocal words, redirs
        end_word()
        if want_target:
            raise Deny("redirection without a target")
        if words or redirs:
            if not words:
                raise Deny("redirection without a command")
            segs.append((words, redirs))
        words, redirs = [], []

    def dollar_ok(j):
        """a '$' that cannot start an expansion: at the end, or before a blank or a closing quote"""
        nxt = cmd[j + 1] if j + 1 < n else ""
        return nxt == "" or nxt in " \t\n\"'/|)" and nxt != "("

    while i < n:
        c = cmd[i]
        if c == "'":
            j = cmd.find("'", i + 1)
            if j < 0:
                raise Deny("unclosed quote")
            cur.append(cmd[i + 1:j])
            has = True
            i = j + 1
            continue
        if c == '"':
            i += 1
            while True:
                if i >= n:
                    raise Deny("unclosed quote")
                d = cmd[i]
                if d == '"':
                    break
                if d == "`":
                    raise Deny("backtick")
                if d == "$":
                    if not dollar_ok(i):
                        raise Deny("variable or command substitution")
                elif d == "\\" and bash and i + 1 < n and cmd[i + 1] in '"\\$`':
                    i += 1
                    d = cmd[i]
                    if d in "$`":
                        cur.append(d)
                        i += 1
                        continue
                cur.append(d)
                i += 1
            has = True
            i += 1
            continue
        if c == "\\" and bash:
            if i + 1 >= n or cmd[i + 1] in "\r\n":
                raise Deny("line continuation")
            cur.append(cmd[i + 1])
            has = True
            i += 2
            continue
        if c in " \t":
            end_word()
            i += 1
            continue
        if c in "\r\n;":
            end_seg()
            i += 1
            continue
        if c == "|":
            if cmd[i:i + 2] == "|&":
                raise Deny("|&")
            end_seg()
            i += 2 if cmd[i:i + 2] == "||" else 1
            continue
        if c == "&":
            if cmd[i:i + 2] == "&&":
                end_seg()
                i += 2
                continue
            if cmd[i:i + 2] == "&>":            # &> file : both streams
                end_word()
                i += 1
                continue                        # the '>' is handled next
            raise Deny("background job or call operator (&)")
        if c == ">":
            if has and "".join(cur).isdigit() and len(cur) <= 2:
                cur, has = [], False            # 2> : a file-descriptor prefix, not a word
            else:
                end_word()
            if want_target:
                raise Deny("redirection without a target")
            i += 2 if cmd[i:i + 2] == ">>" else 1
            if i < n and cmd[i] == "&":         # >&1, 2>&1 : duplicate a descriptor
                m = re.match(r"&(\d|-)(?=[\s;|&]|$)", cmd[i:])
                if not m:
                    raise Deny("redirection to a descriptor that is not a number")
                i += len(m.group(0))
                continue
            if not bash and re.match(r"\s*\$null(?=[\s;|&]|$)", cmd[i:], re.I):
                i += len(re.match(r"\s*\$null", cmd[i:], re.I).group(0))
                continue
            want_target = True
            continue
        if c == "<":
            raise Deny("input redirection or heredoc")
        if c == "`":
            raise Deny("backtick")
        if c == "$":
            if not dollar_ok(i):
                raise Deny("variable or command substitution")
            cur.append(c)
            has = True
            i += 1
            continue
        if c in "(){}":
            raise Deny("subshell, group or script block")
        if c == "#" and not has:
            raise Deny("comment")
        if not bash and c in "@":
            raise Deny("PowerShell splat or array")
        cur.append(c)
        has = True
        i += 1
    end_seg()
    if not segs:
        raise Deny("empty command")
    return segs


# ----------------------------------------------------------------- the guard
class Guard:
    def __init__(self, allow, scratchpad=None, cwd=None, resolve_real=None):
        self.a = allow
        self.scratch = canon(scratchpad) if scratchpad else None
        self.cwd0 = canon(cwd) if cwd else None
        self.repo = canon(allow.get("repo_root") or REPO)
        self.resolve_real = (os.name == "nt") if resolve_real is None else resolve_real
        self.writable = self._patterns(allow.get("writable", []))
        self.redirect_writable = self._patterns(allow.get("redirect_writable", []))
        py = allow.get("python", {})
        self.scripts = {}
        for rel, subs in (py.get("scripts") or {}).items():
            cp = canon(self.repo + "/" + rel)
            if cp:
                self.scripts[cp] = subs
        self.interpreters = [x.lower() for x in py.get("interpreters", [])]

    def _patterns(self, raw):
        out = []
        for p in raw:
            if "{SCRATCHPAD}" in p:
                if not self.scratch:
                    continue
                p = p.replace("{SCRATCHPAD}", self.scratch)
            if p == "/dev/null":
                out.append("posix:/dev/null")
                continue
            tail = ""
            if p.endswith("/**"):
                p, tail = p[:-3], "/**"
            cp = canon(p)
            if cp:
                out.append(cp + tail)
        return out

    # ---- file tools
    def path_allowed(self, raw, patterns, cwd=None):
        cp = canon(raw, cwd)
        if not cp or not matches(cp, patterns):
            return False
        if self.resolve_real and not cp.startswith("posix:"):
            rp = real_canon(raw if re.match(r"^[A-Za-z]:|^[\\/]", raw.strip()) else cp)
            if not rp or not matches(rp, patterns):
                return False
        return True

    def check_write(self, tool_input):
        paths = [tool_input.get(k) for k in ("file_path", "notebook_path", "path") if tool_input.get(k)]
        if not paths:
            raise Deny("no file path")
        for p in paths:
            if not self.path_allowed(p, self.writable):
                raise Deny("write outside the rule records: %s" % p)

    # ---- shell
    def check_shell(self, cmd, flavour, cwd=None, depth=0):
        if depth > MAX_DEPTH:
            raise Deny("nested too deep")
        state = {"cwd": self.cwd0 if cwd is None and depth == 0 else cwd}
        for words, redirs in split_shell(cmd, flavour):
            for r in redirs:
                if not self.path_allowed(r, self.redirect_writable, state["cwd"]):
                    raise Deny("writes by redirection to %s" % r)
            self.check_argv(words, flavour, state, depth)

    @staticmethod
    def _forbidden(args, rule):
        for a in args:
            for f in rule.get("forbid", []):
                if (f.endswith("*") and a.startswith(f[:-1])) or a == f:
                    return a
            fs = rule.get("forbid_short")
            if fs and re.match(r"^-[A-Za-z]", a) and not a.startswith("--") and any(ch in a[1:] for ch in fs):
                return a
        if "only" in rule:
            for a in args:
                if a not in rule["only"]:
                    return a
        if "first" in rule and (not args or args[0] not in rule["first"]):
            return args[0] if args else "(nothing)"
        return None

    def check_argv(self, argv, flavour, state, depth):
        name = argv[0]
        args = argv[1:]
        low = name.lower()
        base = low.replace("\\", "/").rsplit("/", 1)[-1]
        if base.endswith(".exe"):
            base = base[:-4]
        bare = "/" not in name and "\\" not in name

        if base in self.interpreters:
            return self._python(args, state)
        if not bare:
            raise Deny("command given by path: %s" % name)
        if low.endswith(".exe"):
            low = low[:-4]

        if low == "git":
            return self._git(args)
        if low == "docker":
            subs = self.a.get("docker", {}).get("subcommands", {})
            if not args or args[0] not in subs:
                raise Deny("docker %s" % (args[0] if args else ""))
            bad = self._forbidden(args[1:], subs[args[0]])
            if bad:
                raise Deny("docker %s %s" % (args[0], bad))
            return
        if low == "wsl":
            return self._wsl(args, state, depth)
        if low in ("bash", "sh"):
            if len(args) == 2 and args[0] in ("-c", "-lc"):
                return self.check_shell(args[1], "bash", state["cwd"], depth + 1)
            raise Deny("%s runs a script" % low)

        table = self.a.get("shell_commands", {})
        rule = table.get(name) if isinstance(table.get(name), dict) else None
        if rule is None and flavour != "bash":
            ps = self.a.get("powershell_cmdlets", {})
            rule = ps.get(low) if isinstance(ps.get(low), dict) else None
            if rule is None and isinstance(table.get(low), dict):
                rule = table[low]
        if rule is None:
            raise Deny(name)
        bad = self._forbidden(args, rule)
        if bad:
            raise Deny("%s %s" % (name, bad))
        if rule.get("cd"):
            target = [a for a in args if not a.startswith("-")]
            state["cwd"] = canon(target[0], state["cwd"]) if len(target) == 1 else None

    def _git(self, args):
        g = self.a.get("git", {})
        i = 0
        while i < len(args) and args[i].startswith("-"):
            if args[i] in g.get("global_options_with_value", []) and i + 1 < len(args):
                i += 2
            elif args[i] in g.get("global_options", []):
                i += 1
            else:
                raise Deny("git %s" % args[i])
        subs = g.get("subcommands", {})
        if i >= len(args) or args[i] not in subs:
            raise Deny("git %s" % (args[i] if i < len(args) else ""))
        bad = self._forbidden(args[i + 1:], subs[args[i]])
        if bad:
            raise Deny("git %s %s" % (args[i], bad))

    def _python(self, args, state):
        if not args or args[0].startswith("-"):
            raise Deny("python without an allowed script")
        cp = canon(args[0], state["cwd"])
        if not cp or cp not in self.scripts:
            raise Deny("python %s" % args[0])
        if self.resolve_real and not cp.startswith("posix:"):
            if real_canon(cp) != cp:
                raise Deny("python %s (not the real file)" % args[0])
        subs = self.scripts[cp]
        if subs == "*":
            return
        if len(args) < 2 or args[1] not in subs:
            raise Deny("python %s %s" % (posixpath.basename(cp), args[1] if len(args) > 1 else ""))

    def _wsl(self, args, state, depth):
        w = self.a.get("wsl", {})
        cwd = state["cwd"]
        i = 0
        while i < len(args):
            a = args[i]
            if a in w.get("separators", []):
                i += 1
                break
            if a in w.get("options_with_value", []) and i + 1 < len(args):
                if a == "--cd":
                    cwd = canon(args[i + 1], cwd)
                i += 2
                continue
            if a.startswith("-"):
                raise Deny("wsl %s" % a)
            break
        inner = args[i:]
        if not inner:
            raise Deny("wsl without a command")
        if inner[0] in ("bash", "sh") and len(inner) == 3 and inner[1] in ("-c", "-lc"):
            return self.check_shell(inner[2], "bash", cwd, depth + 1)
        for a in inner:                      # wsl hands the rest to a shell: no word may carry shell syntax
            if not SAFE_WORD.match(a):
                raise Deny("wsl argument with shell characters: %s" % a)
        if depth + 1 > MAX_DEPTH:
            raise Deny("nested too deep")
        self.check_argv(inner, "bash", {"cwd": cwd}, depth + 1)

    # ---- one tool call
    def check(self, tool, tool_input):
        """None when allowed, else the short reason"""
        a = self.a
        try:
            if not isinstance(tool, str) or not tool:
                raise Deny("no tool name")
            if not isinstance(tool_input, dict):
                tool_input = {}
            if tool in a.get("tools_always_allowed", []):
                return None
            if tool in a.get("tools_file_write", []):
                self.check_write(tool_input)
                return None
            if tool in a.get("tools_shell", {}):
                self.check_shell(tool_input.get("command"), a["tools_shell"][tool])
                return None
            raise Deny("tool %s" % tool)
        except Deny as e:
            return str(e) or "denied"
        except RecursionError:
            return "nested too deep"


def load_allow(path=None):
    with open(path or ALLOW_FILE, encoding="utf-8") as f:
        return json.load(f)


def is_main_session(inp):
    """a tool call made inside a subagent or teammate carries agent_id; the main session's does not"""
    return not (isinstance(inp, dict) and isinstance(inp.get("agent_id"), str) and inp.get("agent_id").strip())


def decide(inp, allow=None, resolve_real=None):
    """(allowed: bool, reason: str). Subagents are always allowed."""
    if not is_main_session(inp):
        return True, "subagent"
    if not isinstance(inp, dict):
        return False, "unreadable hook input"
    if allow is None:
        allow = load_allow()
    g = Guard(allow, inp.get("scratchpad_dir"), inp.get("cwd"), resolve_real)
    why = g.check(inp.get("tool_name"), inp.get("tool_input"))
    return (why is None), (why or "allowed")


def log_denial(inp, reason, path=None):
    ti = inp.get("tool_input") if isinstance(inp, dict) else None
    what = ""
    if isinstance(ti, dict):
        what = str(ti.get("command") or ti.get("file_path") or ti.get("notebook_path") or "")[:500]
    row = {"t": time.strftime("%Y-%m-%dT%H:%M:%S"), "tool": (inp or {}).get("tool_name") if isinstance(inp, dict) else None,
           "what": what, "reason": reason, "session": (inp or {}).get("session_id") if isinstance(inp, dict) else None}
    p = path or os.environ.get("MAINGUARD_LOG") or LOG_FILE
    try:
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "a", encoding="utf-8") as f:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
    except OSError:
        pass


def deny_output(reason, message=DEFAULT_MESSAGE):
    return {"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
                                   "permissionDecisionReason": "%s (denied: %s)" % (message, reason)}}


def main(argv):
    explain = "--explain" in argv
    raw = sys.stdin.read()
    try:
        inp = json.loads(raw)
    except Exception:
        inp = None
    if isinstance(inp, dict) and not is_main_session(inp):
        return 0                                   # a subagent: never checked, never blocked
    if os.path.exists(OFF_FILE):
        return 0                                   # switched off by the owner
    message = DEFAULT_MESSAGE
    try:
        allow = load_allow()
        message = allow.get("deny_message") or DEFAULT_MESSAGE
        ok, reason = decide(inp, allow)
    except Exception as e:                         # fail closed for the main session
        ok, reason = False, "guard error: %r" % (e,)
    if explain:
        print("ALLOW" if ok else "DENY: " + reason)
        return 0
    if ok:
        return 0                                   # silence: the normal permission flow goes on
    log_denial(inp if isinstance(inp, dict) else {}, reason)
    sys.stdout.write(json.dumps(deny_output(reason, message)))
    return 0


if __name__ == "__main__":
    try:
        code = main(sys.argv[1:])
    except BaseException as e:                     # never let a crash open the gate: exit 2 blocks the call
        sys.stderr.write(DEFAULT_MESSAGE + " (guard crashed: %r)\n" % (e,))
        code = 2
    sys.exit(code)
