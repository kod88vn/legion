#!/usr/bin/env python3
"""constitution.py - the one loader of CONSTITUTION.md for every prompt that is built by code or by a hook.

The owner's order (2026-11-15, PR42): every agent reads the whole constitution first, exactly once, from the one file,
loaded at run time. No copies, no summaries.

Library (used by tools/puppet/dispatch.py and tools/system/constcheck.py):
  text()                 the current text of CONSTITUTION.md
  with_constitution(b)   the constitution, then the prompt body b. Raises if b already holds it.
  require(prompt, what)  raises ConstitutionError unless `prompt` starts with the full text and holds it exactly once
  count_in(prompt)       how many times the full text occurs in a prompt

Hook (Claude Code, wired in .claude/settings.json):
  python constitution.py subagent-start
      SubagentStart hook. Subagents that load CLAUDE.md get the constitution through its import, so the hook stays
      silent for them. The agent types that start WITHOUT CLAUDE.md (NO_CLAUDE_MD below, and custom agents with
      omitClaudeMd) get the full text here as additionalContext, before their first prompt.
      A hook's additionalContext is capped at 10,000 characters by Claude Code (longer text is replaced by a file
      path and a 2,000-character preview). If the constitution outgrows the cap the hook refuses (exit 2) rather
      than hand over a shortened text, and constcheck.py fails.

Standard library only. Reads; changes nothing.
"""
import hashlib
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.normpath(os.path.join(HERE, "..", ".."))

# Claude Code: "A hook's additionalContext ... strings ... are capped at 10,000 characters" (docs: hooks, JSON output).
HOOK_CAP = 10000

# Built-in agent types that start without CLAUDE.md (docs: sub-agents, "What loads at startup": "The built-in
# Explore and Plan agents skip this"). Every other built-in and custom agent loads CLAUDE.md, and with it the import.
NO_CLAUDE_MD = ("Explore", "Plan")


class ConstitutionError(Exception):
    pass


def path():
    return os.environ.get("LEGION_CONSTITUTION") or os.path.join(REPO, "CONSTITUTION.md")


def normalise(t):
    """the text with one kind of line end, no trailing blanks, no leading or trailing empty lines"""
    t = t.replace("﻿", "").replace("\r\n", "\n").replace("\r", "\n")
    return "\n".join(line.rstrip() for line in t.split("\n")).strip("\n")


def text():
    """the full current text (normalised line ends). Raises ConstitutionError when the file is missing or empty."""
    try:
        with open(path(), encoding="utf-8") as f:
            t = normalise(f.read())
    except OSError as e:
        raise ConstitutionError("CONSTITUTION.md cannot be read: %s" % e)
    if len(t) < 200 or "Article 1" not in t:
        raise ConstitutionError("CONSTITUTION.md is empty or not the constitution: %s" % path())
    return t


def digest(t=None):
    return hashlib.sha256(normalise(text() if t is None else t).encode("utf-8")).hexdigest()


def count_in(prompt, const=None):
    return normalise(prompt).count(text() if const is None else normalise(const))


def require(prompt, what="prompt"):
    """the gate every sender calls just before it sends: full text, first, exactly once"""
    const = text()
    p = normalise(prompt)
    n = p.count(const)
    if n == 0:
        raise ConstitutionError("%s does not carry the full constitution: not sent" % what)
    if n > 1:
        raise ConstitutionError("%s carries the constitution %d times: not sent" % (what, n))
    if not p.startswith(const):
        raise ConstitutionError("%s does not start with the constitution: not sent" % what)
    return prompt


def with_constitution(body):
    """the constitution first, then the body (a role brief, a system prompt)"""
    const = text()
    if const in normalise(body or ""):
        raise ConstitutionError("the prompt body already holds the constitution; it is loaded once, here")
    return require(const + "\n\n" + (body or "").lstrip("\n"), "assembled prompt")


# ----------------------------------------------------------------- Claude Code subagent definitions
def agent_frontmatter(md_text):
    """{key: value} of the YAML frontmatter of an agent definition (flat keys only)"""
    m = re.match(r"﻿?---\r?\n(.*?)\r?\n---", md_text, re.S)
    out = {}
    if m:
        for line in m.group(1).splitlines():
            k, sep, v = line.partition(":")
            if sep and k.strip() and not k.startswith((" ", "\t", "#")):
                out[k.strip()] = v.strip().strip("\"'")
    return out


def agents_without_claude_md(agent_dirs=None):
    """names of custom agents that set omitClaudeMd: they start without CLAUDE.md, so the hook serves them"""
    names = []
    for d in agent_dirs if agent_dirs is not None else default_agent_dirs():
        for root, _dirs, files in os.walk(d):
            for fn in files:
                if not fn.endswith(".md"):
                    continue
                try:
                    with open(os.path.join(root, fn), encoding="utf-8") as f:
                        fm = agent_frontmatter(f.read())
                except OSError:
                    continue
                if fm.get("omitClaudeMd", "").lower() == "true" and fm.get("name"):
                    names.append(fm["name"])
    return names


def default_agent_dirs():
    return [os.path.join(REPO, ".claude", "agents"), os.path.join(os.path.dirname(REPO), ".claude", "agents"),
            os.path.join(os.path.expanduser("~"), ".claude", "agents")]


def hook_serves(agent_type, agent_dirs=None):
    """True when the SubagentStart hook must hand this agent type the constitution (it gets no CLAUDE.md)"""
    return agent_type in NO_CLAUDE_MD or agent_type in agents_without_claude_md(agent_dirs)


def subagent_start(inp, agent_dirs=None):
    """the hook's JSON output for one SubagentStart input, or None for silence"""
    if not hook_serves(str((inp or {}).get("agent_type") or ""), agent_dirs):
        return None
    const = text()
    if len(const) > HOOK_CAP:
        raise ConstitutionError("the constitution is %d characters, over the %d-character cap of a hook's "
                                "additionalContext: it would arrive shortened" % (len(const), HOOK_CAP))
    return {"hookSpecificOutput": {"hookEventName": "SubagentStart", "additionalContext": const}}


def main(argv):
    mode = argv[0] if argv else ""
    if mode == "subagent-start":
        try:
            inp = json.load(sys.stdin)
        except Exception:
            inp = {}
        try:
            out = subagent_start(inp)
        except ConstitutionError as e:
            sys.stderr.write("CONSTITUTION NOT LOADED for this subagent: %s. Tell the owner.\n" % e)
            return 2
        if out:
            sys.stdout.write(json.dumps(out))
        return 0
    if mode == "digest":
        print(digest())
        return 0
    if mode == "print":
        sys.stdout.write(text() + "\n")
        return 0
    sys.stderr.write(__doc__)
    return 2


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    sys.exit(main(sys.argv[1:]))
